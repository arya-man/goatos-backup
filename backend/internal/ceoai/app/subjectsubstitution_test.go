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
	if !strings.Contains(refusal, "source for milk feeding") || !strings.Contains(refusal, "animal_current_scope") {
		t.Errorf("refusal must say what it has no source for and which source ran: %q", refusal)
	}
	if strings.Contains(refusal, "in Goat OS") {
		t.Errorf("refusal must not claim the product does not track it: %q", refusal)
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

// coverageRead is the workforce read a leader's coverage question lands on: the
// view's own rows, one per park per role, with no filter -- the shape the
// planner produced live.
func coverageRead(t *testing.T) (reporting.SchemaCard, []domain.SubQuestion, []domain.ToolResult) {
	t.Helper()
	card, ok := reporting.CardByName("workforce_coverage_status")
	if !ok {
		t.Skip("the workforce-coverage card is not in the catalogue")
	}
	subs := []domain.SubQuestion{sqlSub(
		"SELECT park_label, role_label, owner_label, backup_label, coverage_status FROM ceo_ai." + card.Name)}
	results := []domain.ToolResult{{
		ToolName: "sql_fallback", SourceView: card.Name,
		Facts: []domain.Fact{
			{Label: "Feed Director", Value: "covered_by_backup", Scope: "Castro 1"},
			{Label: "Growth Director", Value: "present", Scope: "Castro 1"},
			{Label: "Health Officer", Value: "uncovered_absence", Scope: "Gandhi 2"},
			{Label: "Park Head", Value: "present", Scope: "Gandhi 2"},
		},
	}}
	return card, subs, results
}

// D2, measured live three runs out of three: "who is the backup for the Feed
// Director" was REFUSED with "I read workforce_coverage_status, which does not
// report it" -- while `role_label = 'Feed Director'` is a ROW of that view.
// `director` is nobody's column name, so the guard read it as a subject the
// farm does not record: it could see COLUMN names and not ROW VALUES.
func TestARoleNamedByItsRowValueIsNotRefusedAsUnrecorded(t *testing.T) {
	_, subs, results := coverageRead(t)
	for _, question := range []string{
		"who is the backup for the Feed Director",
		"is the Growth Director covered today",
		"who is covering for the Health Officer",
	} {
		if substituted, subject, view := subjectSubstitution(
			question, subs, results, reporting.Cards(), heldOutCatalogue()); substituted {
			t.Errorf("a role the view reports as a row was refused: %q -> %q from %s", question, subject, view)
		}
	}
}

// The SAME view, the SAME question shape, one word changed: "Feed Inspector"
// is not a role this view reports. Nothing narrowed on "inspector" and no row
// carries it, so the refusal stands. This is the pair that shows the fix
// widens the gate to the view's own ROW VOCABULARY and not to every word
// standing beside a modelled one -- "Feed Director" answers, "Feed Inspector"
// does not, and only the data separates them.
func TestARoleTheCoverageViewDoesNotReportIsStillRefused(t *testing.T) {
	_, subs, results := coverageRead(t)
	substituted, subject, view := subjectSubstitution(
		"who is the backup for the Feed Inspector",
		subs, results, reporting.Cards(), heldOutCatalogue())
	if !substituted {
		t.Fatal("a subject no row of the view carries was answered from it anyway")
	}
	if subject != "feed inspector" || view != "workforce_coverage_status" {
		t.Errorf("the refusal must name the subject and the source that ran, got %q from %s", subject, view)
	}
}

// THE CAPTION IS NOT DATA. A statement may write the subject into its own
// SELECT list -- `SELECT 'milk feeding' AS label, count(*) ...` -- and the fact
// it produces then echoes the word back. That is the model naming its own
// answer, not the view reporting the subject, and it must not talk its way past
// the guard. Same discipline entitysubstitution.go applies to an ear tag.
func TestASubjectWrittenOnlyIntoTheSelectListIsStillRefused(t *testing.T) {
	card, _, _ := scopeRead(t)
	subs := []domain.SubQuestion{sqlSub(
		"SELECT 'milk feeding' AS label, park_label, count(*) FROM ceo_ai." + card.Name + " GROUP BY 1, 2")}
	results := []domain.ToolResult{{
		ToolName: "sql_fallback", SourceView: card.Name,
		Facts: []domain.Fact{{Label: "milk feeding", Value: "24", Scope: "CBE"}},
	}}
	if substituted, _, _ := subjectSubstitution(
		"Kids on milk feeding per park today (head count)",
		subs, results, reporting.Cards(), heldOutCatalogue()); !substituted {
		t.Fatal("a caption the model wrote into its own projection excused the substitution")
	}
}

// NOR IS ANYWHERE ELSE IN THE MODEL'S OWN STATEMENT. The caption rule above
// guarded the projection only; the clearance beside it accepted the subject
// appearing ANYWHERE from the outermost FROM onwards, tested as a substring
// search. So mk-02 -- the exact held-out defect, `CBE 24, CPT 24` off the
// animal-scope view -- came back the moment the model added a comment, a
// tautology, an alias or an ordering key. Every shape below is that statement
// plus one forged token, and all of them must still refuse.
func TestAForgedMentionOfTheSubjectDoesNotExcuseTheWrongView(t *testing.T) {
	card, _, results := scopeRead(t)
	const head = "SELECT park_label, count(*) FROM ceo_ai.animal_current_scope"
	for _, tc := range []struct{ name, sql string }{
		{"a trailing line comment", head + " WHERE management_stage = 'K2' GROUP BY 1 -- milk feeding"},
		{"a block comment", head + " /* milk feeding */ WHERE management_stage = 'K2' GROUP BY 1"},
		{"a literal compared to another literal", head + " WHERE management_stage = 'K2' AND 'milk feeding' <> '' GROUP BY 1"},
		{"a table alias", "SELECT park_label, count(*) FROM ceo_ai.animal_current_scope milk_feeding WHERE management_stage = 'K2' GROUP BY 1"},
		{"an ordering key", head + " WHERE management_stage = 'K2' GROUP BY 1 ORDER BY 'milk feeding'"},
		{"a filter that narrows on the word", head + " WHERE management_stage ILIKE '%milk feeding%' GROUP BY 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subs := []domain.SubQuestion{sqlSub(tc.sql)}
			substituted, subject, view := subjectSubstitution(
				"Kids on milk feeding per park today (head count)",
				subs, results, reporting.Cards(), heldOutCatalogue())
			if !substituted {
				t.Fatalf("the model talked its way past the guard with its own statement text: %s", tc.sql)
			}
			if subject != "milk feeding" || view != card.Name {
				t.Errorf("the refusal must still name the subject and the source that ran, got %q from %s", subject, view)
			}
		})
	}
}

// THE OBVIOUS SQL FOR A QUESTION ABOUT ONE ROLE IS NOT EVIDENCE THAT THE ROLE
// EXISTS, and this is the pair that says why the narrowing arm was deleted
// rather than tightened. `WHERE role_label ILIKE '%Feed Inspector%'` matching
// NO ROWS is the view saying it does not report that role -- and the old arm
// read it as a yes. It is exactly what the model wrote for `Feed Director`, so
// the guard cleared itself in precisely the case it was built to catch.
//
// Feed Director still answers, on the same narrowed statement, because a ROW
// comes back carrying it. Only the data separates them, which is the property
// the Director/Inspector pair has always been here to hold.
func TestNarrowingOnARoleNoRowCarriesIsStillRefused(t *testing.T) {
	card, _, results := coverageRead(t)
	narrowed := func(role string) []domain.SubQuestion {
		return []domain.SubQuestion{sqlSub(
			"SELECT role_label, backup_label FROM ceo_ai." + card.Name +
				" WHERE role_label ILIKE '%" + role + "%'")}
	}
	substituted, subject, _ := subjectSubstitution(
		"who is the backup for the Feed Inspector",
		narrowed("Feed Inspector"), results, reporting.Cards(), heldOutCatalogue())
	if !substituted {
		t.Fatal("a role no row of the view carries was cleared by the model's own WHERE clause")
	}
	if subject != "feed inspector" {
		t.Errorf("the refusal must name the subject, got %q", subject)
	}
	if substituted, _, _ := subjectSubstitution(
		"who is the backup for the Feed Director",
		narrowed("Feed Director"), results, reporting.Cards(), heldOutCatalogue()); substituted {
		t.Error("a role the view reports as a row was refused on the same narrowed statement shape")
	}
}
