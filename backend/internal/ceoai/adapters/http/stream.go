package http

// This file owns the Server-Sent-Events (SSE) streaming transport for the
// leadership assistant ask endpoint (POST /api/ceo-ai/ask?stream=1). It is a
// pure transport boundary: it resolves the Actor from the SERVER SESSION (never
// user text), drives the app streaming port, and renders answer tokens + one
// terminal metadata event onto the wire. It does not plan, route, or compose.
//
// Hard invariants enforced here:
//   - Only synthesized ANSWER tokens are streamed. Step traces, chain-of-thought,
//     tool timelines, and planner reasoning are INTERNAL (audit / admin-only) and
//     have no path to the wire: the StreamingAsker port only exposes answer text
//     via the token sink plus the domain.Answer envelope (answer/source/mode/
//     request_id/conversation_id/citations).
//   - Client disconnect cancels the request context, which the app must honor to
//     stop upstream Vertex/Cube/DB work (no orphaned compute), and which the app
//     also uses to enforce max-steps / budget.
//   - Writes are serialized under a mutex because the heartbeat ticker and the
//     token producer both write the same ResponseWriter.
//   - When the ResponseWriter cannot flush (buffering proxy / test recorder),
//     the handler degrades to a single non-streaming JSON body.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

const (
	streamMaxQuestionBytes  = 1200
	streamMaxBodyBytes      = 8 << 10
	streamHeartbeatInterval = 15 * time.Second
)

// StreamTokenSink is how the app emits incremental answer text. Each call
// appends one increment of the synthesized answer. Implementations write to the
// SSE socket and block on back-pressure; a returned error (client gone) tells
// the app to stop. Only user-facing answer content may be passed — never trace.
//
// It is a type ALIAS of app.StreamSink (the app-owned port), so the transport's
// sink implementations (sseWriter/bufferedSink) structurally satisfy the app
// port and *app.Assistant satisfies StreamingAsker below — without the app
// importing this adapter (hexagonal dependency inversion, no import cycle).
type StreamTokenSink = app.StreamSink

// StreamingAsker is the app port the streaming transport depends on. The
// orchestrator (*app.Assistant) implements it: it plans, routes, executes, and
// composes, pushing answer tokens through sink and returning the final Answer.
// It MUST honor ctx cancellation and its own max-steps / budget limits.
type StreamingAsker interface {
	AskStream(ctx context.Context, q domain.Question, sink StreamTokenSink) (domain.Answer, error)
}

// Wire frames. The committed admin-web client keys events off a `type`
// discriminator inside each SSE `data:` JSON payload (it ignores `event:`
// lines), so every frame carries `type`. We ALSO emit a matching `event:` line
// for EventSource-style consumers — belt and suspenders, harmless to the
// type-based client.
type tokenFrame struct {
	Type string `json:"type"` // always "token"
	Text string `json:"text"`
}

// progressFrame is a coarse pre-answer status frame (planning / querying /
// synthesizing) so the client can render a progressive status line under the
// streaming placeholder. It carries ONLY a stable phase enum + a coarse route
// label — never chain-of-thought, reasoning, or step traces.
type progressFrame struct {
	Type  string `json:"type"` // always "progress"
	Phase string `json:"phase"`
	Label string `json:"label,omitempty"`
}

// finalFrame is the terminal metadata. The embedded domain.Answer inlines the
// allowed envelope fields (answer/source/mode/request_id/conversation_id/
// citations) alongside the `type` discriminator — nothing else.
type finalFrame struct {
	Type string `json:"type"` // always "final"
	domain.Answer
}

type errorFrame struct {
	Type    string `json:"type"` // always "error"
	Message string `json:"message"`
}

// StreamHandler renders a StreamingAsker over SSE.
type StreamHandler struct {
	assistant StreamingAsker
	log       *slog.Logger
	heartbeat time.Duration
}

// NewStreamHandler constructs the SSE streaming handler.
func NewStreamHandler(assistant StreamingAsker, log *slog.Logger) *StreamHandler {
	if log == nil {
		log = slog.Default()
	}
	return &StreamHandler{assistant: assistant, log: log, heartbeat: streamHeartbeatInterval}
}

// Stream handles POST /api/ceo-ai/ask?stream=1. Pre-flight failures (auth, bad
// body) return a normal JSON status before any SSE bytes are written; once the
// stream is committed, failures surface as an SSE error event.
func (h *StreamHandler) Stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteError(w, r, h.log, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"}, nil)
		return
	}

	ctx := r.Context()
	actor := BuildActor(
		httpmiddleware.TenantIDFromContext(ctx),
		httpmiddleware.ActorIDFromContext(ctx),
		httpmiddleware.LocaleTagFromContext(ctx),
		httpmiddleware.AuthGrantsFromContext(ctx),
	)
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil)
		return
	}

	var req askRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, streamMaxBodyBytes)).Decode(&req); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "invalid_json"}, err)
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, map[string]string{"error": "question_required"}, nil)
		return
	}
	if len(question) > streamMaxQuestionBytes {
		question = strings.TrimSpace(question[:streamMaxQuestionBytes])
	}

	q := domain.Question{
		Actor:          actor,
		ConversationID: strings.TrimSpace(req.ConversationID),
		Text:           question,
		AsOf:           biztime.BusinessDayStart(time.Now()),
	}
	requestID := httpmiddleware.RequestIDFromContext(ctx)

	// Find the real net/http Flusher THROUGH the middleware ResponseWriter
	// wrappers (metrics/status/panic recorders each implement Unwrap). A bare
	// w.(http.Flusher) assertion fails on the wrapped writer and silently
	// downgrades every SSE request to the buffered JSON fallback — which is
	// exactly what a live curl behind the auth chain hit. Walking Unwrap()
	// recovers the underlying flusher so tokens actually stream.
	flusher := flusherFromChain(w)
	if flusher == nil {
		h.serveBuffered(ctx, w, r, q, requestID)
		return
	}
	h.serveSSE(ctx, w, flusher, q, requestID)
}

// flusherFromChain returns the first http.Flusher reachable by unwrapping the
// ResponseWriter chain (Go 1.20+ Unwrap convention), or nil if none supports it.
func flusherFromChain(w http.ResponseWriter) http.Flusher {
	for {
		if f, ok := w.(http.Flusher); ok {
			return f
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		next := u.Unwrap()
		if next == nil || next == w {
			return nil
		}
		w = next
	}
}

func (h *StreamHandler) serveSSE(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	q domain.Question,
	requestID string,
) {
	setStreamHeaders(w)
	w.WriteHeader(http.StatusOK)

	sw := &sseWriter{w: w, flusher: flusher}
	// Open comment forces the head + first flush so the client's reader resolves
	// promptly and intermediaries commit the connection.
	_ = sw.comment("open")

	hb := h.heartbeat
	if hb <= 0 {
		hb = streamHeartbeatInterval
	}
	stopHeartbeat := startHeartbeat(ctx, sw, hb)
	defer stopHeartbeat()

	ans, err := h.assistant.AskStream(ctx, q, sw)
	stopHeartbeat()

	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			h.log.InfoContext(ctx, "ceoai stream canceled by client", "request_id", requestID)
			return
		}
		h.log.ErrorContext(ctx, "ceoai stream failed", "request_id", requestID, "error", err)
		// Never leak internal detail (could carry SQL/trace). Emit a stable,
		// user-safe error code mapped from the app error taxonomy.
		_ = sw.event("error", errorFrame{Type: "error", Message: streamErrorCode(err)})
		return
	}

	if ans.RequestID == "" {
		ans.RequestID = requestID
	}
	if ans.ConversationID == "" {
		ans.ConversationID = q.ConversationID
	}
	_ = sw.event("final", finalFrame{Type: "final", Answer: ans})
}

// serveBuffered is the non-streaming degrade path used when the writer cannot
// flush. It collects the full answer and writes one JSON envelope.
func (h *StreamHandler) serveBuffered(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	q domain.Question,
	requestID string,
) {
	buf := &bufferedSink{}
	ans, err := h.assistant.AskStream(ctx, q, buf)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		status, code := streamErrorStatus(err)
		httpresponse.WriteError(w, r, h.log, status, map[string]string{"error": code}, err)
		return
	}
	if ans.RequestID == "" {
		ans.RequestID = requestID
	}
	if ans.ConversationID == "" {
		ans.ConversationID = q.ConversationID
	}
	// If the app streamed text through the sink but left Answer empty, backfill
	// the assembled text so buffered callers still receive the answer body.
	if ans.Answer == "" {
		ans.Answer = buf.String()
	}
	httpresponse.WriteJSON(w, http.StatusOK, ans)
}

// startHeartbeat emits keep-alive comments until ctx is done or stop is called.
// stop is idempotent and safe to call from the request goroutine.
func startHeartbeat(ctx context.Context, sw *sseWriter, interval time.Duration) func() {
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if err := sw.comment("keepalive"); err != nil {
					return
				}
			}
		}
	}()
	return stop
}

// sseWriter serializes SSE writes across the producer and heartbeat goroutines.
type sseWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

// Token implements StreamTokenSink by emitting a token event.
func (s *sseWriter) Token(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.event("token", tokenFrame{Type: "token", Text: token})
}

// Progress implements app.ProgressSink by emitting a coarse status frame. It is
// best-effort: a flush error (client gone) is surfaced so the app can stop.
func (s *sseWriter) Progress(ctx context.Context, phase, label string) error {
	if phase == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.event("progress", progressFrame{Type: "progress", Phase: phase, Label: label})
}

// event writes a named SSE event with a single-line JSON data payload + flush.
func (s *sseWriter) event(name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.w, "event: "+name+"\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(s.w, "data: "); err != nil {
		return err
	}
	if _, err := s.w.Write(data); err != nil {
		return err
	}
	if _, err := io.WriteString(s.w, "\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// comment writes an SSE comment line (ignored by clients) + flush.
func (s *sseWriter) comment(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := io.WriteString(s.w, ": "+text+"\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// bufferedSink accumulates tokens for the non-streaming fallback.
type bufferedSink struct {
	mu  sync.Mutex
	buf []byte
}

func (b *bufferedSink) Token(_ context.Context, token string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, token...)
	return nil
}

func (b *bufferedSink) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func setStreamHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	// Disable proxy buffering (nginx / Cloud Run ingress) so tokens flush live.
	h.Set("X-Accel-Buffering", "no")
}

// streamErrorCode maps an app error to a stable, user-safe SSE error code.
func streamErrorCode(err error) string {
	switch {
	case errors.Is(err, app.ErrForbidden):
		return "leadership_required"
	case errors.Is(err, app.ErrEmptyQuestion):
		return "question_required"
	default:
		return "assistant_unavailable"
	}
}

// streamErrorStatus maps an app error to a JSON status + code for the buffered
// (non-streaming) degrade path.
func streamErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, app.ErrForbidden):
		return http.StatusForbidden, "leadership_required"
	case errors.Is(err, app.ErrEmptyQuestion):
		return http.StatusBadRequest, "question_required"
	default:
		return http.StatusBadGateway, "assistant_unavailable"
	}
}
