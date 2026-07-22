package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// fakeStreamer is a scripted StreamingAsker for transport tests.
type fakeStreamer struct {
	tokens []string
	answer domain.Answer
	err    error

	stopped chan struct{}    // closed after the producer observes cancellation
	gate    chan struct{}    // blocks between tokens so a test can cancel mid-stream
	seen    *domain.Question // captured request
}

func (f *fakeStreamer) AskStream(ctx context.Context, q domain.Question, sink StreamTokenSink) (domain.Answer, error) {
	if f.seen != nil {
		*f.seen = q
	}
	for _, tok := range f.tokens {
		if f.gate != nil {
			select {
			case <-f.gate:
			case <-ctx.Done():
				if f.stopped != nil {
					close(f.stopped)
				}
				return domain.Answer{}, ctx.Err()
			}
		}
		if err := sink.Token(ctx, tok); err != nil {
			if f.stopped != nil {
				close(f.stopped)
			}
			return domain.Answer{}, err
		}
	}
	if f.err != nil {
		return domain.Answer{}, f.err
	}
	return f.answer, nil
}

func leadershipCtx(ctx context.Context) context.Context {
	ctx = httpmiddleware.WithTenantID(ctx, "tenant-1")
	ctx = httpmiddleware.WithActorID(ctx, "actor-1")
	return ctx
}

func postSSE(t *testing.T, h *StreamHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask?stream=1", strings.NewReader(body))
	req = req.WithContext(leadershipCtx(req.Context()))
	rec := httptest.NewRecorder()
	h.Stream(rec, req)
	return rec
}

func TestStreamCompletesWithTokensAndTerminalEvent(t *testing.T) {
	streamer := &fakeStreamer{
		tokens: []string{"Hello ", "world"},
		answer: domain.Answer{
			Answer:         "Hello world",
			Source:         "Mesha counts read model",
			Mode:           domain.ModePlanned,
			ConversationID: "conv-1",
			Citations:      []domain.Citation{{Surface: "counts"}},
		},
	}
	h := NewStreamHandler(streamer, nil)
	rec := postSSE(t, h, `{"question":"how many goats"}`)

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()

	var assembled strings.Builder
	for _, ev := range parseEventData(t, body, "token") {
		var p struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		mustJSON(t, ev, &p)
		if p.Type != "token" {
			t.Fatalf("token frame type = %q, want token", p.Type)
		}
		assembled.WriteString(p.Text)
	}
	if assembled.String() != "Hello world" {
		t.Fatalf("assembled = %q, want %q", assembled.String(), "Hello world")
	}

	done := parseEventData(t, body, "final")
	if len(done) != 1 {
		t.Fatalf("want exactly 1 final event, got %d", len(done))
	}
	var term struct {
		Type string `json:"type"`
		domain.Answer
	}
	mustJSON(t, done[0], &term)
	if term.Type != "final" {
		t.Fatalf("final frame type = %q, want final", term.Type)
	}
	if term.Source != "Mesha counts read model" || term.Mode != domain.ModePlanned {
		t.Fatalf("terminal metadata mismatch: %+v", term)
	}
	if term.ConversationID != "conv-1" {
		t.Fatalf("conversation_id = %q, want conv-1", term.ConversationID)
	}
	if len(term.Citations) != 1 || term.Citations[0].Surface != "counts" {
		t.Fatalf("citations mismatch: %+v", term.Citations)
	}
}

// TestNoTraceTokensLeak asserts the wire carries only allowed fields/events —
// never a step-trace / chain-of-thought / SQL key.
func TestNoTraceTokensLeak(t *testing.T) {
	streamer := &fakeStreamer{
		tokens: []string{"answer only"},
		answer: domain.Answer{Answer: "answer only", Source: "s", Mode: domain.ModeFallback, RequestID: "rid"},
	}
	h := NewStreamHandler(streamer, nil)
	body := strings.ToLower(postSSE(t, h, `{"question":"q"}`).Body.String())

	for _, banned := range []string{"chain_of_thought", "chain-of-thought", "step_trace", "steptrace", "tool_timeline", "reasoning", "\"sql\"", "select "} {
		if strings.Contains(body, banned) {
			t.Fatalf("stream leaked internal field %q: %s", banned, body)
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "event:") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			if name != "token" && name != "final" && name != "error" {
				t.Fatalf("unexpected event type %q", name)
			}
		}
	}
}

// TestDisconnectCancelsServerWork verifies client disconnect cancels the request
// context and the producer observes it (no orphaned work).
func TestDisconnectCancelsServerWork(t *testing.T) {
	stopped := make(chan struct{})
	gate := make(chan struct{})
	streamer := &fakeStreamer{tokens: []string{"a", "b", "c"}, gate: gate, stopped: stopped}
	h := NewStreamHandler(streamer, nil)

	ctx, cancel := context.WithCancel(leadershipCtx(context.Background()))
	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask?stream=1", strings.NewReader(`{"question":"q"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.Stream(rec, req)
		close(done)
	}()

	gate <- struct{}{} // let first token through
	cancel()           // simulate client disconnect

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not observe context cancellation")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after cancellation")
	}
}

// TestBufferedFallbackWhenNoFlusher exercises the non-streaming degrade using a
// ResponseWriter that does not implement http.Flusher.
func TestBufferedFallbackWhenNoFlusher(t *testing.T) {
	streamer := &fakeStreamer{
		tokens: []string{"one ", "two"},
		answer: domain.Answer{Source: "s", Mode: domain.ModePlanned, ConversationID: "c"},
	}
	h := NewStreamHandler(streamer, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask", strings.NewReader(`{"question":"q"}`))
	req = req.WithContext(leadershipCtx(req.Context()))
	w := &noFlushWriter{header: http.Header{}}
	h.Stream(w, req)

	if w.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.status)
	}
	var payload domain.Answer
	mustJSON(t, w.body.String(), &payload)
	if payload.Answer != "one two" {
		t.Fatalf("answer = %q, want %q (backfilled from sink)", payload.Answer, "one two")
	}
	if payload.Source != "s" || payload.ConversationID != "c" {
		t.Fatalf("terminal metadata missing in buffered fallback: %+v", payload)
	}
}

func TestRejectsUnauthorizedAndBadRequests(t *testing.T) {
	h := NewStreamHandler(&fakeStreamer{}, nil)

	// Missing tenant/actor context => 401 before any streaming.
	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask", strings.NewReader(`{"question":"q"}`))
	rec := httptest.NewRecorder()
	h.Stream(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-actor status = %d, want 401", rec.Code)
	}

	if rec := postSSE(t, h, `{"question":"   "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty question status = %d, want 400", rec.Code)
	}
	if rec := postSSE(t, h, `not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json status = %d, want 400", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ceo-ai/ask", nil)
	req = req.WithContext(leadershipCtx(req.Context()))
	rec = httptest.NewRecorder()
	h.Stream(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}
}

func TestProducerErrorEmitsSafeErrorEvent(t *testing.T) {
	streamer := &fakeStreamer{err: errBoom{}}
	h := NewStreamHandler(streamer, nil)
	body := postSSE(t, h, `{"question":"q"}`).Body.String()
	if errs := parseEventData(t, body, "error"); len(errs) != 1 {
		t.Fatalf("want 1 error event, got %d: %s", len(errs), body)
	}
	if strings.Contains(body, "boom") {
		t.Fatalf("internal error detail leaked to wire: %s", body)
	}
	if !strings.Contains(body, "assistant_unavailable") {
		t.Fatalf("expected safe error code, got: %s", body)
	}
}

func TestForbiddenAppErrorMapsToBufferedStatus(t *testing.T) {
	streamer := &fakeStreamer{err: app.ErrForbidden}
	h := NewStreamHandler(streamer, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask", strings.NewReader(`{"question":"q"}`))
	req = req.WithContext(leadershipCtx(req.Context()))
	w := &noFlushWriter{header: http.Header{}}
	h.Stream(w, req)
	if w.status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.status)
	}
}

func TestActorAndQuestionResolvedFromSession(t *testing.T) {
	var seen domain.Question
	streamer := &fakeStreamer{tokens: []string{"x"}, answer: domain.Answer{Source: "s", Mode: domain.ModePlanned}, seen: &seen}
	h := NewStreamHandler(streamer, nil)
	postSSE(t, h, `{"question":"  how many goats  ","conversation_id":"c9"}`)
	if seen.Actor.TenantID != "tenant-1" || seen.Actor.UserID != "actor-1" {
		t.Fatalf("actor not from session: %+v", seen.Actor)
	}
	if seen.Text != "how many goats" {
		t.Fatalf("question text = %q, want trimmed", seen.Text)
	}
	if seen.ConversationID != "c9" {
		t.Fatalf("conversation id = %q, want c9", seen.ConversationID)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom internal detail" }

// --- helpers ---

func parseEventData(t *testing.T, body, event string) []string {
	t.Helper()
	var out []string
	lines := strings.Split(body, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "event: "+event {
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "data: ") {
				out = append(out, strings.TrimPrefix(lines[i+1], "data: "))
			}
		}
	}
	return out
}

func mustJSON(t *testing.T, s string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("json unmarshal %q: %v", s, err)
	}
}

// noFlushWriter is an http.ResponseWriter that does NOT implement http.Flusher.
type noFlushWriter struct {
	header http.Header
	body   strings.Builder
	status int
	mu     sync.Mutex
}

func (n *noFlushWriter) Header() http.Header { return n.header }
func (n *noFlushWriter) WriteHeader(code int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.status == 0 {
		n.status = code
	}
}
func (n *noFlushWriter) Write(b []byte) (int, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.status == 0 {
		n.status = http.StatusOK
	}
	return n.body.Write(b)
}

var _ io.Writer = (*noFlushWriter)(nil)
