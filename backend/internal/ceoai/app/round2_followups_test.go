package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// EVERY RESULT SWAP IS GATED. postReadHonesty judges whatever answered, so a
// path that REPLACES a result after the gates have run puts a number in front
// of a leader that no gate ever saw. The post-review retryFailedResults is the
// one such path: it swaps a failed/empty result in place, at a different tier,
// after both gates.
//
// It cannot be reached end to end today, and that is precisely why this test is
// structural rather than behavioural: no entry in fallbackAliases defines a
// `sql` tier, so every replacement lands on a route with no SourceView and is
// unjudgeable anyway. The gate is therefore currently harmless BY ACCIDENT --
// add one alias, or start setting SourceView on Toolbox results (the planned
// follow-up), and the accident stops holding. This test pins the rule instead
// of the accident.
//
// Mutation: delete the postReadHonesty call inside the retryFailedResults
// branch and this goes red.
func TestThePostReviewRetrySwapIsGatedByTheHonestyChecks(t *testing.T) {
	src, err := os.ReadFile("orchestrator.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	// ANCHORED ON THE FUNCTION, NOT ON THE `if a.` SPELLING, AND IT FAILS RATHER
	// THAN SKIPS. The first version looked for the literal "if a.retryFailedResults("
	// and called t.Skip when it was absent -- so reformatting the call to
	// `if ok := a.retryFailedResults(...); ok {` AND deleting the gate together
	// reported PASS. A routine refactor plus a dropped gate was invisible, which
	// is the one thing a structural test must not allow. While the function
	// exists in this file, its gate must be provable.
	const call = "retryFailedResults("
	at := strings.Index(text, call)
	if at < 0 {
		t.Fatal("retryFailedResults is no longer called from orchestrator.go — if the post-review " +
			"retry was removed, delete this test; if it MOVED, re-anchor it, because an ungated " +
			"result swap after postReadHonesty puts a number in front of a leader that no gate saw")
	}
	// The branch body, up to its closing brace at the same indentation.
	rest := text[at:]
	end := strings.Index(rest, "\n\t\t}\n")
	if end < 0 {
		t.Fatal("could not delimit the retry branch")
	}
	branch := rest[:end]
	if !strings.Contains(branch, "postReadHonesty(") {
		t.Fatal("retryFailedResults replaces a result AFTER the honesty gates ran, " +
			"and the branch does not re-run them — a number no gate saw can reach the leader")
	}
	if strings.Index(branch, "postReadHonesty(") > strings.Index(branch, "comp.composeFor(") {
		t.Fatal("the gate must run BEFORE the swapped results are composed")
	}
}

// A QUESTION REDUCED TO JUNK MUST NOT BE REFUSED ON THE JUNK. measureTerms
// strips the DIMENSION vocabulary, so a question whose subject IS a dimension
// ("which diseases are most common", "how many loads arrived yesterday") was
// left holding only its adjective or its verb -- and, given a read that named a
// card, the leader was told "We don't track common, most in Goat OS".
// derivationWords is now subtracted in measureTerms, as namesASubject already
// did, and carries the two observed words.
//
// Mutation: drop the derivationWords subtraction in measureTerms and this goes
// red on both questions.
func TestAQuestionLeftHoldingOnlyItsArithmeticWordsIsNotRefused(t *testing.T) {
	cards, catalog := reporting.Cards(), heldOutCatalogue()
	for _, question := range []string{
		"which diseases are most common",
		"how many loads arrived yesterday",
	} {
		if unmodelled, terms := measureUnmodelled(question, cards, catalog); unmodelled {
			t.Errorf("%q would be refused as %v — those are arithmetic/event words, not a subject the farm could record",
				question, terms)
		}
	}
	// And the gate still fires on a real unmodelled SUBJECT beside the same
	// shape of question, so this did not defang it.
	if unmodelled, terms := measureUnmodelled(
		"how much colostrum did each shed dispense yesterday", cards, catalog); !unmodelled {
		t.Errorf("a genuinely unmodelled subject stopped being refused; terms=%v", terms)
	}
}

// AFTER THE Synthetic CHANGE, Complete is correctly false more often -- so a
// single-sub plan whose read LEGITIMATELY found nothing now takes the extra
// retry and the strict re-render it did not take before. "Nothing found" is an
// honest answer and must stay one: the reply says so plainly, and never claims
// the read failed or that the subject is untracked.
func TestASingleSubReadThatFoundNothingStillAnswersHonestly(t *testing.T) {
	sqlFB := &fakeSQLFallback{result: domain.ToolResult{SourceView: "animal_current_scope"}}
	reg := NewRegistry(nil, nil, sqlFB)
	const empty = "SELECT park_label AS label, CAST(count(*) AS text) AS value, park_label AS scope " +
		"FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' GROUP BY park_label LIMIT 10"
	prov := &fakeProvider{byModel: true, plan: modelSQLPlan(empty,
		domain.AnswerSpec{Measure: "animals", Dimensions: []string{"park"}})}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: reg})

	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(),
		Text: "how many animals are in each park", AsOf: time.Date(2026, 9, 18, 9, 0, 0, 0, istZone)})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode == domain.ModeRefused {
		t.Fatalf("an empty read was turned into a refusal: %q", ans.Answer)
	}
	low := strings.ToLower(ans.Answer)
	if strings.Contains(low, "couldn't read") || strings.Contains(low, "could not read") {
		t.Fatalf("an empty read was reported as a failed read: %q", ans.Answer)
	}
	if strings.Contains(low, "don't have a source for") {
		t.Fatalf("an empty read was reported as a subject with no source: %q", ans.Answer)
	}
	if strings.TrimSpace(ans.Answer) == "" {
		t.Fatal("an empty read produced an empty answer")
	}
}
