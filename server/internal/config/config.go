// Package config loads Market Lens runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type MarketDataConfig struct {
	Provider        string
	APIToken        string
	ScheduleEnabled bool
	// ReobserveSessions is how many recent trading sessions the routine daily pass re-asks the
	// source about, so a close restated after the fact is noticed. Sessions, not calendar days:
	// the exchanges keep different holiday calendars.
	ReobserveSessions int
	DailyHour         int
	DailyMinute       int
	DailyLocation     *time.Location
	RequestTimeout    time.Duration
	MaxRetries        int
	Workers           int
}

type Config struct {
	Port                string
	DatabaseURL         string
	AllowedOrigins      []string
	Environment         string
	StaticDir           string
	ShutdownTimeout     time.Duration
	MarketData          MarketDataConfig
	Auth                AuthConfig
	ExternalCredentials ExternalCredentialConfig
}

func Load() (Config, error) {
	marketData, err := loadMarketData()
	if err != nil {
		return Config{}, err
	}
	environment := valueOrDefault("ENV", "development")
	auth, err := loadAuth(environment)
	if err != nil {
		return Config{}, err
	}
	externalCredentials, err := loadExternalCredentials(environment)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:                valueOrDefault("PORT", "8080"),
		DatabaseURL:         valueOrDefault("DATABASE_URL", "postgres://market_lens:market_lens@localhost:5432/market_lens?sslmode=disable"),
		AllowedOrigins:      splitCSV(valueOrDefault("ALLOWED_ORIGINS", "http://localhost:5173")),
		Environment:         environment,
		StaticDir:           strings.TrimSpace(os.Getenv("STATIC_DIR")),
		ShutdownTimeout:     10 * time.Second,
		MarketData:          marketData,
		Auth:                auth,
		ExternalCredentials: externalCredentials,
	}
	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return cfg, nil
}

func loadMarketData() (MarketDataConfig, error) {
	scheduleEnabled, err := strconv.ParseBool(valueOrDefault("MARKET_DATA_SCHEDULE_ENABLED", "false"))
	if err != nil {
		return MarketDataConfig{}, fmt.Errorf("MARKET_DATA_SCHEDULE_ENABLED must be true or false")
	}

	dailyTime, err := time.Parse("15:04", valueOrDefault("MARKET_DATA_DAILY_TIME", "20:00"))
	if err != nil {
		return MarketDataConfig{}, fmt.Errorf("MARKET_DATA_DAILY_TIME must use HH:MM")
	}
	dailyLocation, err := time.LoadLocation(valueOrDefault("MARKET_DATA_DAILY_TIMEZONE", "Europe/Stockholm"))
	if err != nil {
		return MarketDataConfig{}, fmt.Errorf("MARKET_DATA_DAILY_TIMEZONE must be an IANA time zone")
	}

	requestTimeout, err := time.ParseDuration(valueOrDefault("MARKET_DATA_REQUEST_TIMEOUT", "30s"))
	if err != nil || requestTimeout <= 0 {
		return MarketDataConfig{}, fmt.Errorf("MARKET_DATA_REQUEST_TIMEOUT must be a positive duration")
	}
	maxRetries, err := boundedInt("MARKET_DATA_MAX_RETRIES", 3, 0, 10)
	if err != nil {
		return MarketDataConfig{}, err
	}
	workers, err := boundedInt("MARKET_DATA_WORKERS", 4, 1, 16)
	if err != nil {
		return MarketDataConfig{}, err
	}
	// One session is the behaviour before feature 016. Sixty is about three months, past which
	// an operator wanting history should run a backfill and decide its scope deliberately.
	// Refused rather than clamped: somebody who sets 500 believing they have covered a quarter
	// must find out, not quietly get three months.
	reobserveSessions, err := boundedInt("MARKET_DATA_REOBSERVE_SESSIONS", 5, 1, 60)
	if err != nil {
		return MarketDataConfig{}, err
	}

	provider := valueOrDefault("MARKET_DATA_PROVIDER", "eodhd")
	if provider == "" {
		return MarketDataConfig{}, fmt.Errorf("MARKET_DATA_PROVIDER must not be empty")
	}

	return MarketDataConfig{
		Provider:          provider,
		APIToken:          strings.TrimSpace(os.Getenv("EODHD_API_TOKEN")),
		ScheduleEnabled:   scheduleEnabled,
		ReobserveSessions: reobserveSessions,
		DailyHour:         dailyTime.Hour(),
		DailyMinute:       dailyTime.Minute(),
		DailyLocation:     dailyLocation,
		RequestTimeout:    requestTimeout,
		MaxRetries:        maxRetries,
		Workers:           workers,
	}, nil
}

func boundedInt(key string, fallback, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(valueOrDefault(key, strconv.Itoa(fallback)))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", key, minimum, maximum)
	}
	return value, nil
}

func (c Config) IsProduction() bool {
	return c.Environment == "production" || c.Environment == "prod"
}

func valueOrDefault(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
