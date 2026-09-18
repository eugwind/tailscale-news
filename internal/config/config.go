// Package config loads and validates the aggregator's runtime configuration.
//
// Configuration comes from environment variables only and is parsed exactly once
// at startup. Every value has a usable default so the service runs with no
// environment set at all.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

// Defaults applied when the corresponding environment variable is unset or empty.
const (
	DefaultAddr            = ":8080"
	DefaultLogLevel        = slog.LevelInfo
	DefaultPollInterval    = 15 * time.Minute
	DefaultFetchTimeout    = 30 * time.Second
	DefaultMaxConcurrency  = 4
	DefaultShutdownTimeout = 15 * time.Second
	DefaultMaxItems        = 5000
)

// Config holds every runtime setting for the service.
type Config struct {
	// Addr is the TCP address the HTTP server listens on, for example ":8080".
	Addr string
	// LogLevel is the minimum level emitted by the structured logger.
	LogLevel slog.Level
	// PollInterval is how often every registered source is polled.
	PollInterval time.Duration
	// FetchTimeout bounds a single source fetch, including body read.
	FetchTimeout time.Duration
	// MaxConcurrency bounds how many sources are fetched in parallel.
	MaxConcurrency int
	// ShutdownTimeout bounds graceful shutdown before connections are forced closed.
	ShutdownTimeout time.Duration
	// MaxItems bounds how many stories the in-memory store retains.
	MaxItems int
}

// Load reads the configuration from getenv, which is normally [os.Getenv].
// Passing getenv explicitly keeps the loader testable without mutating the
// process environment.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:            DefaultAddr,
		LogLevel:        DefaultLogLevel,
		PollInterval:    DefaultPollInterval,
		FetchTimeout:    DefaultFetchTimeout,
		MaxConcurrency:  DefaultMaxConcurrency,
		ShutdownTimeout: DefaultShutdownTimeout,
		MaxItems:        DefaultMaxItems,
	}

	if v := getenv("TSNEWS_ADDR"); v != "" {
		cfg.Addr = v
	}

	if v := getenv("TSNEWS_LOG_LEVEL"); v != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("TSNEWS_LOG_LEVEL %q: %w", v, err)
		}
		cfg.LogLevel = level
	}

	durations := []struct {
		key    string
		target *time.Duration
	}{
		{"TSNEWS_POLL_INTERVAL", &cfg.PollInterval},
		{"TSNEWS_FETCH_TIMEOUT", &cfg.FetchTimeout},
		{"TSNEWS_SHUTDOWN_TIMEOUT", &cfg.ShutdownTimeout},
	}
	for _, d := range durations {
		v := getenv(d.key)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("%s %q: %w", d.key, v, err)
		}
		if parsed <= 0 {
			return Config{}, fmt.Errorf("%s %q: must be positive", d.key, v)
		}
		*d.target = parsed
	}

	if v := getenv("TSNEWS_MAX_CONCURRENCY"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("TSNEWS_MAX_CONCURRENCY %q: %w", v, err)
		}
		if parsed < 1 {
			return Config{}, fmt.Errorf("TSNEWS_MAX_CONCURRENCY %q: must be at least 1", v)
		}
		cfg.MaxConcurrency = parsed
	}

	if v := getenv("TSNEWS_MAX_ITEMS"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("TSNEWS_MAX_ITEMS %q: %w", v, err)
		}
		if parsed < 1 {
			return Config{}, fmt.Errorf("TSNEWS_MAX_ITEMS %q: must be at least 1", v)
		}
		cfg.MaxItems = parsed
	}

	return cfg, nil
}
