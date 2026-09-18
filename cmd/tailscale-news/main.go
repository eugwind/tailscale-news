// Command tailscale-news aggregates Tailscale-related news from RSS/Atom feeds,
// blogs, release notes, and community sources, and serves them over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/eugwind/tailscale-news/internal/config"
	"github.com/eugwind/tailscale-news/internal/httpapi"
)

// Injected at link time by .github/skills/release-build/scripts/build-release.sh.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tailscale-news: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("tailscale-news", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print version information and exit")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *showVersion {
		fmt.Fprintf(stdout, "tailscale-news %s (commit %s, built %s)\n", version, commit, buildDate)
		return nil
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.InfoContext(ctx, "starting",
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("addr", cfg.Addr),
		slog.Duration("poll_interval", cfg.PollInterval),
		slog.Int("max_concurrency", cfg.MaxConcurrency),
	)

	handler := httpapi.NewHandler(logger, httpapi.BuildInfo{Version: version, Commit: commit})
	srv := httpapi.NewServer(cfg.Addr, handler)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.InfoContext(ctx, "shutdown signal received", slog.Duration("grace", cfg.ShutdownTimeout))
	}

	// ctx is already cancelled, so shutdown needs its own deadline.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		closeErr := srv.Close()
		return fmt.Errorf("graceful shutdown: %w", errors.Join(err, closeErr))
	}
	if err := <-serverErr; err != nil {
		return fmt.Errorf("http server: %w", err)
	}

	logger.InfoContext(shutdownCtx, "stopped cleanly")
	return nil
}
