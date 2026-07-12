package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vgoats/goatos/backend/internal/bootstrap"
	"github.com/vgoats/goatos/backend/internal/platform/logger"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run holds every defer (telemetry flush, api.Close, signal-context stop) so
// they execute before main's single os.Exit. Calling os.Exit directly from
// main bypasses all deferred functions - including the telemetry shutdown
// that flushes batched spans/metrics - which previously meant a bootstrap,
// ListenAndServe, or Shutdown failure silently dropped observability data
// exactly when it mattered most (a crash). See the worker mains
// (cmd/outbox-relay, cmd/obligation-sweeper, ...) for the same run(ctx) error
// idiom.
func run() error {
	log := logger.New(os.Getenv("GOATOS_LOG_LEVEL"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := observability.SetupTelemetry(ctx, observability.Config{Service: "api"})
	if err != nil {
		log.Error("setup_telemetry", slog.String("error", err.Error()))
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			log.Error("shutdown_telemetry", slog.String("error", err.Error()))
		}
	}()

	api, err := bootstrap.NewAPI(ctx, bootstrap.ConfigFromEnv(), log)
	if err != nil {
		log.Error("bootstrap api", slog.String("error", err.Error()))
		return err
	}
	defer api.Close()

	errCh := make(chan error, 1)
	go func() {
		log.Info("api_starting", slog.String("addr", api.Server.Addr))
		errCh <- api.Server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("api_server_error", slog.String("error", err.Error()))
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.Server.Shutdown(shutdownCtx); err != nil {
		log.Error("api_shutdown_error", slog.String("error", err.Error()))
		return err
	}
	log.Info("api_stopped")
	return nil
}
