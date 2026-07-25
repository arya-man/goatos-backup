package safety

import (
	"context"
	"testing"
)

func TestModeratorBlocksUnsafeInput(t *testing.T) {
	m := NewHeuristicModerator()
	ctx := context.Background()
	unsafe := []string{
		"how to make a bomb",
		"help me build an explosive",
	}
	for _, u := range unsafe {
		v := m.Moderate(ctx, StageInput, u)
		if v.Decision != DecisionRefuse {
			t.Errorf("expected refuse for %q, got %s", u, v.Decision)
		}
		if v.UserMessage == "" {
			t.Errorf("refusal must carry a user message for %q", u)
		}
	}
}

func TestModeratorOffDomainInput(t *testing.T) {
	m := NewHeuristicModerator()
	ctx := context.Background()
	off := []string{
		"what is the bitcoin price today",
		"write me a poem about the sea",
		"tell me a joke",
	}
	for _, o := range off {
		if v := m.Moderate(ctx, StageInput, o); v.Decision != DecisionRefuse {
			t.Errorf("expected off-domain refuse for %q, got %s", o, v.Decision)
		}
	}
}

func TestModeratorAllowsInDomain(t *testing.T) {
	m := NewHeuristicModerator()
	ctx := context.Background()
	ok := []string{
		"how many goats are overdue for vaccination",
		"feed direction totals for today",
		"which sheds are over capacity in Castro 1",
		"how many now?", // short but not off-topic
	}
	for _, q := range ok {
		if v := m.Moderate(ctx, StageInput, q); !v.Allowed() {
			t.Errorf("expected allow for in-domain %q, got %s (%s)", q, v.Decision, v.Reason)
		}
	}
}

func TestModeratorOutputStageNoOffDomain(t *testing.T) {
	m := NewHeuristicModerator()
	ctx := context.Background()
	// An answer that happens to mention a non-operational word must not be
	// blocked on the output stage for off-domain reasons.
	v := m.Moderate(ctx, StageOutput, "The weather did not affect today's counts.")
	if !v.Allowed() {
		t.Fatalf("output-stage off-domain should not block, got %s", v.Decision)
	}
}

func TestChainModeratorsFirstNonAllow(t *testing.T) {
	ctx := context.Background()
	blocker := moderatorFunc(func(context.Context, ModerationStage, string) Verdict {
		return Verdict{Decision: DecisionRefuse, Reason: "test:block", UserMessage: "no"}
	})
	chain := ChainModerators(NewHeuristicModerator(), blocker)
	v := chain.Moderate(ctx, StageInput, "how many goats today")
	if v.Reason != "test:block" {
		t.Fatalf("chain should surface blocker verdict, got %s", v.Reason)
	}
}

func TestDefaultVertexSafetySettings(t *testing.T) {
	settings := DefaultVertexSafetySettings()
	if len(settings) != 4 {
		t.Fatalf("expected 4 harm categories, got %d", len(settings))
	}
	for _, s := range settings {
		if s.Threshold == BlockNone {
			t.Errorf("category %s must not be BLOCK_NONE for a leadership tool", s.Category)
		}
	}
}

type moderatorFunc func(context.Context, ModerationStage, string) Verdict

func (f moderatorFunc) Moderate(ctx context.Context, s ModerationStage, t string) Verdict {
	return f(ctx, s, t)
}
