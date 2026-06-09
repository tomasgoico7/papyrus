package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port           string
	Environment    string
	AIServiceURL   string
	AIServiceToken string
	JWKSURL        string
	AllowedOrigins []string
	RateLimitRPM   int
	MaxUploadBytes int64
	RequestTimeout time.Duration
}

func (c Config) IsProduction() bool {
	return c.Environment == "production"
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	supabaseURL, err := requiredString("SUPABASE_URL")
	if err != nil {
		return nil, err
	}

	rateLimit, err := intWithDefault("RATE_LIMIT_RPM", 20)
	if err != nil {
		return nil, err
	}

	maxUploadMB, err := intWithDefault("MAX_UPLOAD_MB", 5)
	if err != nil {
		return nil, err
	}

	timeoutSeconds, err := intWithDefault("REQUEST_TIMEOUT_SECONDS", 60)
	if err != nil {
		return nil, err
	}

	return &Config{
		Port:           stringWithDefault("PORT", "8080"),
		Environment:    stringWithDefault("ENVIRONMENT", "development"),
		AIServiceURL:   stringWithDefault("AI_SERVICE_URL", "http://localhost:8000"),
		AIServiceToken: strings.TrimSpace(os.Getenv("INTERNAL_API_KEY")),
		JWKSURL:        strings.TrimRight(supabaseURL, "/") + "/auth/v1/.well-known/jwks.json",
		AllowedOrigins: splitOrigins(stringWithDefault("ALLOWED_ORIGINS", "http://localhost:3000")),
		RateLimitRPM:   rateLimit,
		MaxUploadBytes: int64(maxUploadMB) * 1024 * 1024,
		RequestTimeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

func requiredString(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing required environment variable %q", key)
	}
	return value, nil
}

func stringWithDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func intWithDefault(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("environment variable %q must be an integer: %w", key, err)
	}
	return value, nil
}

func splitOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}
