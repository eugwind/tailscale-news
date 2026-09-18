package config

import (
	"log/slog"
	"testing"
	"time"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoad_Defaults_AllFieldsPopulated(t *testing.T) {
	t.Parallel()

	got, err := Load(envFrom(nil))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	want := Config{
		Addr:            DefaultAddr,
		LogLevel:        DefaultLogLevel,
		PollInterval:    DefaultPollInterval,
		FetchTimeout:    DefaultFetchTimeout,
		MaxConcurrency:  DefaultMaxConcurrency,
		ShutdownTimeout: DefaultShutdownTimeout,
	}
	if got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestLoad_ValidEnvironment_Overrides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want func(Config) Config
	}{
		{
			name: "address",
			env:  map[string]string{"TSNEWS_ADDR": "127.0.0.1:9000"},
			want: func(c Config) Config { c.Addr = "127.0.0.1:9000"; return c },
		},
		{
			name: "log level is case insensitive",
			env:  map[string]string{"TSNEWS_LOG_LEVEL": "debug"},
			want: func(c Config) Config { c.LogLevel = slog.LevelDebug; return c },
		},
		{
			name: "poll interval",
			env:  map[string]string{"TSNEWS_POLL_INTERVAL": "90s"},
			want: func(c Config) Config { c.PollInterval = 90 * time.Second; return c },
		},
		{
			name: "fetch timeout",
			env:  map[string]string{"TSNEWS_FETCH_TIMEOUT": "5s"},
			want: func(c Config) Config { c.FetchTimeout = 5 * time.Second; return c },
		},
		{
			name: "shutdown timeout",
			env:  map[string]string{"TSNEWS_SHUTDOWN_TIMEOUT": "1m"},
			want: func(c Config) Config { c.ShutdownTimeout = time.Minute; return c },
		},
		{
			name: "max concurrency",
			env:  map[string]string{"TSNEWS_MAX_CONCURRENCY": "16"},
			want: func(c Config) Config { c.MaxConcurrency = 16; return c },
		},
		{
			name: "empty values fall back to defaults",
			env:  map[string]string{"TSNEWS_ADDR": "", "TSNEWS_POLL_INTERVAL": ""},
			want: func(c Config) Config { return c },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defaults, err := Load(envFrom(nil))
			if err != nil {
				t.Fatalf("Load() defaults error = %v", err)
			}

			got, err := Load(envFrom(tt.env))
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if want := tt.want(defaults); got != want {
				t.Errorf("Load() = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoad_InvalidEnvironment_ReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"unknown log level", map[string]string{"TSNEWS_LOG_LEVEL": "chatty"}},
		{"unparseable duration", map[string]string{"TSNEWS_POLL_INTERVAL": "15 minutes"}},
		{"zero duration", map[string]string{"TSNEWS_FETCH_TIMEOUT": "0s"}},
		{"negative duration", map[string]string{"TSNEWS_SHUTDOWN_TIMEOUT": "-1s"}},
		{"non-numeric concurrency", map[string]string{"TSNEWS_MAX_CONCURRENCY": "many"}},
		{"zero concurrency", map[string]string{"TSNEWS_MAX_CONCURRENCY": "0"}},
		{"negative concurrency", map[string]string{"TSNEWS_MAX_CONCURRENCY": "-2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(envFrom(tt.env))
			if err == nil {
				t.Fatalf("Load() error = nil, want an error (got config %+v)", got)
			}
			if got != (Config{}) {
				t.Errorf("Load() = %+v, want the zero Config on error", got)
			}
		})
	}
}
