package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// The catalogue the live assistant runs with: a counts read that knows kids and
// adults, and feed reads that know feeding. NOTHING here knows milk.
func heldOutCatalogue() []ports.ToolSpec {
	return []ports.ToolSpec{
		{
			Name: "counts_breakdown", Route: domain.RouteAPI,
			Description: "Live herd total, kids/adults age bands and a per-pen breakdown by stage, breed and sex.",
			Params:      []string{"park_label", "shed_id", "stage", "breed", "sex"},
		},
		{
			Name: "feed_direction_today", Route: domain.RouteAPI,
			Description: "Feed direction for today by pen",
			Params:      []string{"park_label"},
		},
	}
}

func scopeRead(t *testing.T) (reporting.SchemaCard, []domain.SubQuestion, []domain.ToolResult) {
	t.Helper()
	card, ok := reporting.CardByName("animal_current_scope")
	if !ok {
		t.Skip("the animal-scope card is not in the catalogue")
	}
	subs := []domain.SubQuestion{sqlSub(
		"SELECT park_label, count(*) FROM ceo_ai.animal_current_scope WHERE management_stage = 'K2' GROUP BY 1")}
	results := []domain.ToolResult{{
		ToolName: "sql_fallback", SourceView: card.Name,
		Facts: []domain.Fact{{Label: "CBE", Value: "24"}, {Label: "CPT", Value: "24"}},
	}}
	return card, subs, results
}

// mk-02, the held-out failure this exists for: "Kids on milk feeding per park
// today (head count)" was answered "CBE 24, CPT 24" -- the 48-goat park split
// of the animal-scope view, which reports no milk and no feeding.
func TestAMilkQuestionAnsweredFromTheAnimalScopeViewIsRefused(t *testing.T) {
	_, subs, results := scopeRead(t)
	substituted, subject, view := subjectSubstitution(
		"Kids on milk feeding per park today (head count)",
		subs, results, reporting.Cards(), heldOutCatalogue())
	if !substituted {
		t.Fatal("a milk-feeding question was answered from the animal-scope view and not refused")
	}
	if subject != "milk feeding" {
		t.Errorf("the refusal must name the subject the farm does not record, got %q", subject)
	}
	if view != "animal_current_scope" {
		t.Errorf("the refusal must name the source that actually ran, got %q", view)
	}
	refusal := substitutedSubjectRefusal(subject, view)
	if !strings.Contains(refusal, "don't track milk feeding") || !strings.Contains(refusal, "animal_current_scope") {
		t.Errorf("refusal must say what is untracked and which source ran: %q", refusal)
	}
}

// ct-05, the held-out answer that is CORRECT from that SAME view with almost
// the same SQL: "how many kids vs adults are there in each park". The only
// thing separating it from mk-02 is the word "milk", so a rule that cannot tell
// them apart is not shippable -- the previous card-models-nothing attempt
// flagged this one and was withdrawn.
func TestKidsVersusAdultsPerParkStillAnswersFromTheSameView(t *testing.T) {
	_, subs, results := scopeRead(t)
	for _, question := range []string{
		"how many kids vs adults are there in each park",
		"What's the current headcount per park?",
		"Break down live animals by breed and sex",
	} {
		if substituted, subject, view := subjectSubstitution(
			question, subs, results, reporting.Cards(), heldOutCatalogue()); substituted {
			t.Errorf("a correct answer was refused: %q -> %q from %s", question, subject, view)
		}
	}
}

// The read that ran is ON the question's subject whenever the card itself
// models one of the two words -- "crowded sheds" over the shed-capacity view,
// "preventive care tasks" over a task view. An unmodelled adjective beside a
// word the card DOES carry proves nothing, and refusing on it would refuse
// every question carrying one.
func TestAReadOverASourceThatModelsTheSubjectIsNeverRefused(t *testing.T) {
	card, ok := reporting.CardByName("shed_capacity_current")
	if !ok {
		t.Skip("the shed-capacity card is not in the catalogue")
	}
	subs := []domain.SubQuestion{sqlSub("SELECT shed_label, animals, capacity FROM ceo_ai." + card.Name)}
	results := []domain.ToolResult{{ToolName: "sql_fallback", SourceView: card.Name, Facts: oneFact()}}
	if substituted, subject, _ := subjectSubstitution(
		"Top 5 most crowded sheds relative to capacity",
		subs, results, reporting.Cards(), heldOutCatalogue()); substituted {
		t.Errorf("the shed-capacity view was refused its own subject: %q", subject)
	}
}

// An EMPTY read answered "No records found" substituted nothing: no number was
// presented, so there is no neighbour's figure to mistake for the answer. This
// is what keeps the questions whose real sources are simply empty answering as
// they do today.
func TestAnEmptyReadIsNeverAccusedOfSubstitution(t *testing.T) {
	_, subs, results := scopeRead(t)
	results[0].Facts = nil
	if substituted, subject, _ := subjectSubstitution(
		"Kids on milk feeding per park today (head count)",
		subs, results, reporting.Cards(), heldOutCatalogue()); substituted {
		t.Errorf("a read that returned no rows was refused as a substitution: %q", subject)
	}
}

// With no tool catalogue in hand the views are half the evidence, and "modelled
// by nothing" cannot be judged -- the same fail-open measureUnmodelled takes.
func TestWithNoToolCatalogueNothingIsJudgedUnmodelled(t *testing.T) {
	_, subs, results := scopeRead(t)
	if substituted, _, _ := subjectSubstitution(
		"Kids on milk feeding per park today (head count)",
		subs, results, reporting.Cards(), nil); substituted {
		t.Error("a subject was called untracked while half the catalogue was missing")
	}
}

// A question asks for ARITHMETIC with words no schema models -- "biggest",
// "percentage", "compared". They name how much, never what, so they must never
// look like an untracked subject.
func TestArithmeticWordsAreNeverMistakenForAnUntrackedSubject(t *testing.T) {
	_, subs, results := scopeRead(t)
	for _, question := range []string{
		"What's the biggest shortfall between directed and fed feed",
		"Deaths this month compared to last month, by park",
		"What's our daily feed adherence percentage for the last 14 days?",
	} {
		if substituted, subject, _ := subjectSubstitution(
			question, subs, results, reporting.Cards(), heldOutCatalogue()); substituted {
			t.Errorf("%q was refused over the arithmetic word %q", question, subject)
		}
	}
}
