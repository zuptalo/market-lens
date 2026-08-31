package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type AuthConfig struct {
	Secret                 string
	SecureCookies          bool
	OwnerIdleTimeout       time.Duration
	SessionAbsoluteTimeout time.Duration
	SetupTTL               time.Duration
	MemberCodeTTL          time.Duration
	MemberTempBlock        time.Duration
	AppBaseURL             string
}

// resolveAppBaseURL determines the absolute origin used to build emailed account links.
// Operators may set it explicitly; otherwise the first allowed origin is the only safe
// inference, because guessing a host would send people to somewhere they never configured.
func resolveAppBaseURL(allowedOrigins []string) (string, error) {
	value := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if value == "" {
		if len(allowedOrigins) == 0 {
			return "", fmt.Errorf("APP_BASE_URL is required when ALLOWED_ORIGINS is empty")
		}
		value = allowedOrigins[0]
	}
	value = strings.TrimRight(value, "/")
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("APP_BASE_URL must be an absolute http or https origin")
	}
	return value, nil
}

func loadAuth(environment string) (AuthConfig, error) {
	secureCookies, err := strconv.ParseBool(valueOrDefault("AUTH_SECURE_COOKIES", "true"))
	if err != nil {
		return AuthConfig{}, fmt.Errorf("AUTH_SECURE_COOKIES must be true or false")
	}

	ownerIdleTimeout, err := positiveDuration("AUTH_OWNER_IDLE_TIMEOUT", "8h")
	if err != nil {
		return AuthConfig{}, err
	}
	sessionAbsoluteTimeout, err := positiveDuration("AUTH_SESSION_ABSOLUTE_TIMEOUT", "720h")
	if err != nil {
		return AuthConfig{}, err
	}
	if sessionAbsoluteTimeout < ownerIdleTimeout {
		return AuthConfig{}, fmt.Errorf("AUTH_SESSION_ABSOLUTE_TIMEOUT must not be shorter than AUTH_OWNER_IDLE_TIMEOUT")
	}
	setupTTL, err := positiveDuration("AUTH_SETUP_TTL", "15m")
	if err != nil {
		return AuthConfig{}, err
	}
	memberCodeTTL, err := positiveDuration("AUTH_MEMBER_CODE_TTL", "10m")
	if err != nil {
		return AuthConfig{}, err
	}
	memberTempBlock, err := positiveDuration("AUTH_MEMBER_TEMP_BLOCK", "15m")
	if err != nil {
		return AuthConfig{}, err
	}

	appBaseURL, err := resolveAppBaseURL(splitCSV(valueOrDefault("ALLOWED_ORIGINS", "http://localhost:5173")))
	if err != nil {
		return AuthConfig{}, err
	}

	secret := strings.TrimSpace(os.Getenv("AUTH_SECRET"))
	if secret != "" && len([]byte(secret)) < 32 {
		return AuthConfig{}, fmt.Errorf("AUTH_SECRET must contain at least 32 bytes")
	}
	production := environment == "production" || environment == "prod"
	if production && secret == "" {
		return AuthConfig{}, fmt.Errorf("AUTH_SECRET is required in production")
	}
	if production && !secureCookies {
		return AuthConfig{}, fmt.Errorf("AUTH_SECURE_COOKIES must be true in production")
	}

	return AuthConfig{
		Secret:                 secret,
		SecureCookies:          secureCookies,
		OwnerIdleTimeout:       ownerIdleTimeout,
		SessionAbsoluteTimeout: sessionAbsoluteTimeout,
		SetupTTL:               setupTTL,
		MemberCodeTTL:          memberCodeTTL,
		MemberTempBlock:        memberTempBlock,
		AppBaseURL:             appBaseURL,
	}, nil
}

func positiveDuration(key, fallback string) (time.Duration, error) {
	duration, err := time.ParseDuration(valueOrDefault(key, fallback))
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return duration, nil
}
