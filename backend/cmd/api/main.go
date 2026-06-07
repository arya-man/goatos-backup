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
)

func main() {
	log := logger.New(os.Getenv("GOATOS_LOG_LEVEL"))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	api, err := bootstrap.NewAPI(ctx, bootstrap.ConfigFromEnv(), log)
	if err != nil {
		log.Error("bootstrap api", slog.String("error", err.Error()))
		os.Exit(1)
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
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := api.Server.Shutdown(shutdownCtx); err != nil {
		log.Error("api_shutdown_error", slog.String("error", err.Error()))
		os.Exit(1)
	}
	log.Info("api_stopped")
}
