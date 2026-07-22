package app

// AskStream is the streaming entry point for the leadership assistant. It runs
// the SAME read-only orchestration pipeline as Ask (plan -> decompose ->
// Cube-first tool execution -> synthesize ONE grounded answer, behind the
// bounded step loop + runtime review) and then emits the synthesized ANSWER
// text progressively through the sink for a ChatGPT-class render.
//
// Root-cause streaming invariants (no band-aid):
//   - Only synthesized answer text reaches the sink. Step traces / chain-of-
//     thought / tool timelines never touch it — they stay INTERNAL (audit +
//     admin-only), exactly as with Ask. The transport (adapters/http) also
//     enforces this; the app never hands the sink anything but answer tokens.
//   - Client disconnect cancels ctx. Ask threads ctx into the planner, Cube,
//     Toolbox, SQL, and DB ports; on cancellation the pipeline stops upstream
//     work and returns ctx.Err() rather than continuing an orphaned query
//     (proven by app.TestAskStreamHonorsCancellation against a blocking
//     provider). AskStream propagates that error instead of streaming.
//   - The Answer envelope returned is byte-identical to Ask's for the same
//     input; streaming only changes delivery, never content.

import (
	"context"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// StreamSink is how the orchestrator emits incremental answer text to the
// transport. Each call appends one increment of the synthesized answer. The
// transport (SSE writer) implements it and blocks on back-pressure; a returned
// error (client gone) tells the app to stop. Only user-facing answer content is
// ever passed — never a step trace. Defining the port here (app-owned) keeps
// the dependency inversion clean: the HTTP adapter aliases this type so
// *Assistant structurally satisfies the transport's StreamingAsker port without
// the app importing the adapter.
type StreamSink interface {
	Token(ctx context.Context, token string) error
}

// AskStream produces one grounded Answer and streams its answer body through
// sink. It reuses Ask for the full pipeline (identical routing, review, audit,
// caching, persistence) so streaming can never diverge from the non-streaming
// contract, then chunks the composed answer into tokens.
func (a *Assistant) AskStream(ctx context.Context, q domain.Question, sink StreamSink) (domain.Answer, error) {
	if err := ctx.Err(); err != nil {
		return domain.Answer{}, err
	}

	ans, err := a.Ask(ctx, q)
	if err != nil {
		return domain.Answer{}, err
	}
	// A cancellation observed after Ask returned (e.g. Ask degraded gracefully
	// but the client is already gone) must not be rendered as a real answer.
	if err := ctx.Err(); err != nil {
		return domain.Answer{}, err
	}

	for _, tok := range streamTokens(ans.Answer) {
		if err := ctx.Err(); err != nil {
			return domain.Answer{}, err
		}
		// scale-guard:ignore: SSE token emission to the client stream sink, not a DB/port fan-out; tokens are the already-composed answer body, bounded by answer length.
		if err := sink.Token(ctx, tok); err != nil {
			return domain.Answer{}, err
		}
	}
	return ans, nil
}

// streamTokens splits an answer into progressive chunks whose concatenation is
// byte-identical to the input (each chunk keeps its trailing whitespace). This
// gives a word-at-a-time render without altering the final text.
func streamTokens(text string) []string {
	if text == "" {
		return nil
	}
	var tokens []string
	var b strings.Builder
	for _, r := range text {
		b.WriteRune(r)
		if r == ' ' || r == '\n' {
			tokens = append(tokens, b.String())
			b.Reset()
		}
	}
	if b.Len() > 0 {
		tokens = append(tokens, b.String())
	}
	return tokens
}
