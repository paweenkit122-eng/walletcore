// Package config loads application configuration from environment variables.
// In development, values can come from a local .env file (see .env.example);
// in containers/production they come from the real environment.
package config

import (
	"os"
	"time"
)

// Config holds all runtime configuration for the service.
type Config struct {
	AppEnv       string        // "development" | "production"
	HTTPPort     string        // port the HTTP server listens on
	DatabaseURL  string        // Postgres DSN
	ReadTimeout  time.Duration // HTTP server read timeout
	WriteTimeout time.Duration // HTTP server write timeout
}

// Load reads configuration from the environment, applying safe defaults
// so the service can boot locally with zero config.
func Load() Config {
	return Config{
		AppEnv:       getEnv("APP_ENV", "development"),
		HTTPPort:     getEnv("HTTP_PORT", "8080"),
		DatabaseURL:  getEnv("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/walletcore?sslmode=disable"),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// getEnv returns the value of key, or fallback when it is unset or empty.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
