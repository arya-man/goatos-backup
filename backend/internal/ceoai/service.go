// Package ceoai assembles the leadership assistant from its adapters. Build
// returns a ready HTTP handler for POST /ceo-ai/ask. It uses only the ports the
// caller supplies: with no Vertex provider it runs on the deterministic keyword
// planner (mode=fallback) so the endpoint is functional before Vertex/Cube land.
//
// Sibling adapters owned by other packages (cubeclient/MetricService,
// toolboxclient/Toolbox, sqlguard/SQLFallback, safety/Moderator,
// persistence/ConversationStore) are injected via Options — bridging their
// concrete types to the ports lives at the cmd wiring boundary.
package ceoai

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	ceohttp "github.com/vgoats/goatos/backend/internal/ceoai/adapters/http"
	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/keywordplanner"
	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/memcache"
	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/ratelimit"
	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Options carry the optional, sibling-owned ports plus tuning. Any nil port
// degrades gracefully (the assistant falls to the tiers that are wired).
type Options struct {
	// Provider is the model planner (Vertex). When nil, the deterministic
	// keyword planner is used for both primary and fallback (mode=fallback).
	Provider ports.AIProvider
	// Critic is the optional Gemini reviewer, gated by ReviewEnabled.
	Critic ports.Reviewer

	Metrics   ports.MetricService     // Cube (tier 1)
	ReadTools []ports.ToolExecutor    // in-process read services (tier 2)
	Toolbox   ports.Toolbox           // MCP Toolbox (tier 3)
	SQL       ports.SQLFallback       // sqlguard (tier 4)
	Moderator ports.Moderator         // safety
	Convo     ports.ConversationStore // persistence
	Memory    ports.MemoryStore
	Audit     ports.AuditSink
	Telemetry ports.Telemetry // observability metric facade (assistant_* OTel instruments)

	// Traces is the admin step-trace read port. When set, the assembled Service
	// also mounts GET /ceo-ai/admin/trace/{request_id}.
	Traces ceohttp.TraceReader

	// ConvStore / FeedbackStore back the thread + feedback surface (GET/POST
	// /ceo-ai/conversations*, POST /ceo-ai/messages/{id}/feedback). Starters
	// supplies the leadership starter questions for GET /ceo-ai/starters. A nil
	// store degrades that route to 503; a nil Starters uses the defaults. The
	// starters route (the leadership probe/launcher gate) works regardless.
	ConvStore     ceohttp.ConvStore
	FeedbackStore ceohttp.FeedbackStore
	Starters      ceohttp.StartersProvider

	Logger *slog.Logger
}

// Service is the assembled assistant. Handler is the JSON transport, Stream the
// SSE transport, and Router mounts both behind the single POST /ceo-ai/ask route
// (transport chosen by the request body's `stream` field).
type Service struct {
	Assistant  *app.Assistant
	Handler    *ceohttp.Handler
	Stream     *ceohttp.StreamHandler
	Router     *ceohttp.Router
	AdminTrace *ceohttp.AdminTraceHandler
	Convo      *ceohttp.ConversationHandler
}

// Register mounts the assistant on the protected mux (POST /ceo-ai/ask plus the
// thread/feedback/starters surface).
func (s *Service) Register(mux *http.ServeMux) {
	s.Router.Register(mux)
	if s.Convo != nil {
		s.Convo.Register(mux)
	}
}

// Build wires the assistant from env + options. It never returns an error: an
// absent Vertex provider is a supported (fallback) configuration.
func Build(opts Options) *Service {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	fallback := keywordplanner.New()
	provider := opts.Provider
	if provider == nil {
		provider = fallback
	}

	registry := app.NewRegistry(opts.Metrics, opts.Toolbox, opts.SQL)
	if len(opts.ReadTools) > 0 {
		registry.Register(opts.ReadTools...)
	}

	cfg := app.Config{
		MaxSteps:      envInt("MESHA_AI_MAX_STEPS", 6),
		WallClock:     25 * time.Second,
		ReviewEnabled: os.Getenv("MESHA_AI_REVIEW") == "1",
		ModelVersion:  envOr("MESHA_VERTEX_MODEL", "gemini-2.5-flash"),
		PromptVersion: "v1",
	}

	assistant := app.NewAssistant(cfg, app.Deps{
		Provider:  provider,
		Fallback:  fallback,
		Registry:  registry,
		Metrics:   opts.Metrics,
		Moderator: opts.Moderator,
		Convo:     opts.Convo,
		Memory:    opts.Memory,
		Cache:     memcache.New(60*time.Second, 512),
		Limiter:   ratelimit.New(30, time.Minute, 10_000),
		Budget:    app.NewInMemoryBudget(2_000_000, 200_000),
		Audit:     opts.Audit,
		Critic:    opts.Critic,
		Telemetry: opts.Telemetry,
		Semaphore: app.NewSemaphore(envInt("MESHA_AI_MAX_CONCURRENCY", 16)),
		Logger:    log,
	})

	handler := ceohttp.NewHandler(assistant, log)
	stream := ceohttp.NewStreamHandler(assistant, log)
	router := ceohttp.NewRouter(handler, stream)

	var adminTrace *ceohttp.AdminTraceHandler
	if opts.Traces != nil {
		adminTrace = ceohttp.NewAdminTraceHandler(opts.Traces, log)
		router = router.WithAdminTrace(adminTrace)
	}

	convo := ceohttp.NewConversationHandler(opts.ConvStore, opts.FeedbackStore, opts.Starters, log)

	return &Service{
		Assistant:  assistant,
		Handler:    handler,
		Stream:     stream,
		Router:     router,
		AdminTrace: adminTrace,
		Convo:      convo,
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
