package worker

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HealthServer exposes the Cloud Run lifecycle/health endpoints for the kernel
// worker. The worker is a background loop with no request surface, but a Cloud
// Run SERVICE requires the container to listen on $PORT or its revision never
// becomes ready. This listener exists ONLY for Cloud Run startup/liveness
// probes and health monitoring — it never triggers stage work.
//
//	GET /livez  — process/supervisor alive. Always 200 while the process runs;
//	              a liveness-probe failure means the container is wedged and Cloud
//	              Run should restart it.
//	GET /readyz — ready to be counted as a live kernel: the supervisor has
//	              started AND the database is reachable AND we are not draining.
//	              Flips to 503 on SIGTERM so Cloud Run stops treating a
//	              shutting-down instance as healthy before the process exits.
type HealthServer struct {
	pool     *pgxpool.Pool
	logger   *slog.Logger
	srv      *http.Server
	ready    atomic.Bool
	draining atomic.Bool
}

// NewHealthServer builds a health server bound to addr (e.g. ":8080"). It does
// not start listening until Start is called.
func NewHealthServer(addr string, pool *pgxpool.Pool, logger *slog.Logger) *HealthServer {
	h := &HealthServer{pool: pool, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", h.handleLivez)
	mux.HandleFunc("/readyz", h.handleReadyz)
	h.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return h
}

// SetReady marks the worker ready (supervisor has started). Readiness still
// additionally requires a live DB and not-draining at probe time.
func (h *HealthServer) SetReady(ready bool) { h.ready.Store(ready) }

// BeginDraining flips readiness off for graceful SIGTERM shutdown: Cloud Run
// sees /readyz fail and stops counting this instance as a live kernel while the
// in-flight stage finishes and the process exits.
func (h *HealthServer) BeginDraining() { h.draining.Store(true) }

// Start begins serving in a background goroutine. A non-ErrServerClosed listen
// error is logged (the worker keeps running — a health-port bind failure must
// not silently mask the kernel, but on Cloud Run it will fail the startup probe
// and the revision will be rolled back).
func (h *HealthServer) Start() {
	go func() {
		if err := h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.logger.Error("kernel_worker_health_server_failed", "addr", h.srv.Addr, "err", err)
		}
	}()
}

// Shutdown gracefully stops the health server.
func (h *HealthServer) Shutdown(ctx context.Context) {
	if err := h.srv.Shutdown(ctx); err != nil {
		h.logger.Error("kernel_worker_health_server_shutdown_failed", "err", err)
	}
}

func (h *HealthServer) handleLivez(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *HealthServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if h.draining.Load() {
		http.Error(w, "draining", http.StatusServiceUnavailable)
		return
	}
	if !h.ready.Load() {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	// DB reachability. Bounded so a wedged DB cannot hang the probe.
	pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.pool.Ping(pingCtx); err != nil {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
