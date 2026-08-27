// Package config loads Market Lens runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	AllowedOrigins  []string
	Environment     string
	StaticDir       string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		Port:            valueOrDefault("PORT", "8080"),
		DatabaseURL:     valueOrDefault("DATABASE_URL", "postgres://market_lens:market_lens@localhost:5432/market_lens?sslmode=disable"),
		AllowedOrigins:  splitCSV(valueOrDefault("ALLOWED_ORIGINS", "http://localhost:5173")),
		Environment:     valueOrDefault("ENV", "development"),
		StaticDir:       strings.TrimSpace(os.Getenv("STATIC_DIR")),
		ShutdownTimeout: 10 * time.Second,
	}
	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return cfg, nil
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
