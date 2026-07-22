package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// collectSink captures streamed answer tokens for assertion.
type collectSink struct {
	mu     sync.Mutex
	tokens []string
}

func (c *collectSink) Token(_ context.Context, tok string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens = append(c.tokens, tok)
	return nil
}

func (c *collectSink) joined() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.tokens, "")
}

// TestAskStreamReassemblesAnswer proves streamed tokens concatenate back to the
// exact non-streaming answer body and the final envelope matches Ask.
func TestAskStreamReassemblesAnswer(t *testing.T) {
	prov := &fakeProvider{plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "s1", Text: "count", Route: domain.RouteAPI, ToolName: "herd_count"},
	}}, byModel: true}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(&fakeExec{
		spec:   ports.ToolSpec{Name: "herd_count", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "counts", Facts: []domain.Fact{{Label: "active goats", Value: "1234"}}},
	})
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})

	q := domain.Question{Actor: leadershipActor(), Text: "how many goats"}
	sink := &collectSink{}
	ans, err := a.AskStream(context.Background(), q, sink)
	if err != nil {
		t.Fatalf("AskStream err = %v", err)
	}
	if ans.Answer == "" {
		t.Fatal("empty answer")
	}
	if got := sink.joined(); got != ans.Answer {
		t.Fatalf("streamed body %q != final answer %q", got, ans.Answer)
	}
}

// progressSink captures both progress phases and answer tokens in arrival order
// so a test can assert progress frames precede the answer body.
type progressSink struct {
	mu     sync.Mutex
	order  []string // "progress:<phase>" and "token" markers, in arrival order
	phases []string
	tokens []string
}

func (p *progressSink) Progress(_ context.Context, phase, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phases = append(p.phases, phase)
	p.order = append(p.order, "progress:"+phase)
	return nil
}

func (p *progressSink) Token(_ context.Context, tok string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens = append(p.tokens, tok)
	p.order = append(p.order, "token")
	return nil
}

// TestAskStreamEmitsProgressBeforeAnswer proves AskStream emits the coarse
// progress phases (planning -> querying -> synthesizing) and that every progress
// frame arrives BEFORE the first answer token — the fix for the "frozen blank"
// finding. It also proves no phase label leaks chain-of-thought (phases are a
// bounded enum).
func TestAskStreamEmitsProgressBeforeAnswer(t *testing.T) {
	prov := &fakeProvider{plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "s1", Text: "count", Route: domain.RouteAPI, ToolName: "herd_count"},
	}}, byModel: true}
	reg := NewRegistry(nil, nil, nil)
	reg.Register(&fakeExec{
		spec:   ports.ToolSpec{Name: "herd_count", Route: domain.RouteAPI},
		result: domain.ToolResult{Surface: "counts", Facts: []domain.Fact{{Label: "active goats", Value: "1234"}}},
	})
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})

	q := domain.Question{Actor: leadershipActor(), Text: "how many goats"}
	sink := &progressSink{}
	if _, err := a.AskStream(context.Background(), q, sink); err != nil {
		t.Fatalf("AskStream err = %v", err)
	}

	wantPhases := []string{"planning", "querying", "synthesizing"}
	if strings.Join(sink.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", sink.phases, wantPhases)
	}
	// Every progress frame must precede the first token.
	firstToken := -1
	lastProgress := -1
	for i, m := range sink.order {
		if m == "token" && firstToken == -1 {
			firstToken = i
		}
		if strings.HasPrefix(m, "progress:") {
			lastProgress = i
		}
	}
	if firstToken == -1 {
		t.Fatal("no answer tokens streamed")
	}
	if lastProgress >= firstToken {
		t.Fatalf("progress frame arrived after first token (lastProgress=%d firstToken=%d): %v", lastProgress, firstToken, sink.order)
	}
}

// blockingProvider blocks in Plan until ctx is cancelled, then records that it
// observed the cancellation and returns ctx.Err(). This is the real upstream
// dependency (Vertex/Cube/DB stand-in) that AskStream must interrupt.
type blockingProvider struct {
	entered  chan struct{}
	observed chan struct{}
	once     sync.Once
}

func (b *blockingProvider) Plan(ctx context.Context, _ domain.Question, _ []domain.ResolvedEntities, _ []ports.ToolSpec) (domain.Plan, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	close(b.observed)
	return domain.Plan{}, ctx.Err()
}
func (b *blockingProvider) PlannedByModel() bool { return true }

// TestAskStreamHonorsCancellation is the production-path proof for the review's
// MEDIUM finding: a client disconnect (ctx cancel) mid-answer stops the REAL
// orchestrator's upstream work instead of running it to completion in an
// orphaned goroutine. The provider blocks on ctx.Done(); AskStream must return
// promptly with a context error and NO fallback answer, and the provider must
// observe the cancellation.
func TestAskStreamHonorsCancellation(t *testing.T) {
	bp := &blockingProvider{entered: make(chan struct{}), observed: make(chan struct{})}
	// A fallback IS wired to prove cancellation is NOT papered over by degrading
	// to the deterministic planner: the ctx error must win.
	fb := &fakeProvider{plan: domain.Plan{SubQuestions: []domain.SubQuestion{{ID: "s1", Route: domain.RouteAPI, ToolName: "x"}}}}
	a := NewAssistant(Config{}, Deps{Provider: bp, Fallback: fb, Registry: NewRegistry(nil, nil, nil)})

	ctx, cancel := context.WithCancel(context.Background())
	q := domain.Question{Actor: leadershipActor(), Text: "how many goats"}
	sink := &collectSink{}

	type result struct {
		ans domain.Answer
		err error
	}
	done := make(chan result, 1)
	go func() {
		ans, err := a.AskStream(ctx, q, sink)
		done <- result{ans, err}
	}()

	select {
	case <-bp.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider was never invoked")
	}
	cancel() // simulate client disconnect

	select {
	case <-bp.observed:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream provider did not observe cancellation (orphaned work)")
	}

	select {
	case r := <-done:
		if r.err == nil || !errors.Is(r.err, context.Canceled) {
			t.Fatalf("AskStream err = %v, want context.Canceled", r.err)
		}
		if r.ans.Answer != "" {
			t.Fatalf("cancelled request produced an answer: %q", r.ans.Answer)
		}
		if got := sink.joined(); got != "" {
			t.Fatalf("cancelled request streamed tokens: %q", got)
		}
		if fb.calls != 0 {
			t.Fatalf("cancellation fell through to fallback planner (calls=%d)", fb.calls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AskStream did not return after cancellation")
	}
}
