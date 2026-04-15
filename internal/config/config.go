package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	DatabaseURL  string
	CodexHome    string
	HTTPAddr     string
	PollInterval time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		HTTPAddr:     envOrDefault("HTTP_ADDR", ":8080"),
		PollInterval: 5 * time.Minute,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if raw := os.Getenv("POLL_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("parse POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}

	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		cfg.CodexHome = codexHome
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, fmt.Errorf("resolve user home: %w", err)
		}
		cfg.CodexHome = filepath.Join(home, ".codex")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
