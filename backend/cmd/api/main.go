package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vgoats/goatos/backend/internal/bootstrap"
	"github.com/vgoats/goatos/backend/internal/platform/buildinfo"
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
	// observability.New (not the deprecated platform/logger shim, and never
	// slog.New directly - see platform/observability's package doc and
	// tools/agent-hooks/check-boundaries.sh's slog.New guard) is the
	// canonical process logger. Version stamps buildinfo.Current() onto every
	// log line so a stale-binary drift error (internal/platform/migrationguard,
	// wired in bootstrap.NewAPI) can be traced back to the exact build that
	// refused to start.
	log := observability.New(observability.Config{
		Service: "api",
		Version: buildinfo.Current(),
		Level:   os.Getenv("GOATOS_LOG_LEVEL"),
	})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := assertLocalOriginMainStack(); err != nil {
		log.Error("local_stack_guard", slog.String("error", err.Error()))
		return err
	}

	shutdownTelemetry, err := observability.SetupTelemetry(ctx, observability.Config{Service: "api", Version: buildinfo.Current()})
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

func assertLocalOriginMainStack() error {
	if os.Getenv("GOATOS_ENV") != "local" || os.Getenv("GOATOS_ALLOW_STALE_LOCAL_STACK") == "1" {
		return nil
	}
	if !isSharedLocalAPIAddress(os.Getenv("GOATOS_HTTP_ADDR")) {
		return nil
	}
	repoRoot, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		if runningInContainer() {
			return nil
		}
		return err
	}
	remote, err := git("remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if !strings.Contains(remote, "github.com/vgoats/goatos") {
		return errors.New("backend local stack origin is not github.com/vgoats/goatos")
	}
	if os.Getenv("GOATOS_ORIGIN_MAIN_PREVERIFIED") != "1" {
		if err := exec.Command("git", "-C", repoRoot, "fetch", "--quiet", "origin", "main").Run(); err != nil {
			return errors.New("backend shared local stack could not fetch origin/main")
		}
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	originMain, err := git("rev-parse", "refs/remotes/origin/main")
	if err != nil {
		return err
	}
	if head != originMain {
		return errors.New("backend local stack HEAD " + shortSHA(head) + " is not origin/main " + shortSHA(originMain))
	}
	status, err := git("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if status != "" {
		return errors.New("backend local stack has modified tracked files; serve a clean origin/main checkout")
	}
	return nil
}

func isSharedLocalAPIAddress(addr string) bool {
	if strings.TrimSpace(addr) == "" {
		return true
	}
	_, port, err := net.SplitHostPort(addr)
	return err == nil && port == "8080"
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

func shortSHA(sha string) string {
	if len(sha) < 12 {
		return sha
	}
	return sha[:12]
}
