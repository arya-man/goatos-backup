package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// A card's date columns are NOT interchangeable. Letting a period ride any of
// them, then stamping the answer with the period the leader asked for, reports
// a figure read over a date nobody named — and the reader cannot see the swap.
// An alternate date column is opt-in PER QUESTION: only when the question's own
// words name that date.
func TestAPeriodRidesASecondDateColumnOnlyWhenTheQuestionNamesThatDate(t *testing.T) {
	card, ok := reporting.CardByName("procurement_loads_base")
	if !ok {
		t.Skip("card not in the catalogue")
	}
	alts := card.AlternateDateColumns()
	if len(alts) == 0 {
		t.Skip("card declares no alternate date column")
	}
	alt := alts[0]

	named := narrowAlternates(card, []string{alt}).(sqlguard.AlternateDateColumnsCard)
	if got := named.AlternateDateColumns(); len(got) != 1 || !strings.EqualFold(got[0], alt) {
		t.Errorf("a question naming %s must be allowed to bind it, got %v", alt, got)
	}

	silent := narrowAlternates(card, nil).(sqlguard.AlternateDateColumnsCard)
	if got := silent.AlternateDateColumns(); len(got) != 0 {
		t.Errorf("a question naming no date must bind the card's own business day only, got %v", got)
	}

	other := narrowAlternates(card, []string{"some_other_date"}).(sqlguard.AlternateDateColumnsCard)
	if got := other.AlternateDateColumns(); len(got) != 0 {
		t.Errorf("an allowance for a column this card does not declare must not widen it, got %v", got)
	}
}

// The allowance is derived from the COLUMN NAME against the question's words,
// so a column added tomorrow participates with no list to maintain.
func TestTheAllowanceIsDerivedFromTheQuestionsOwnWords(t *testing.T) {
	purchased := questionNamedDateColumns("how many animals did we purchase last quarter")
	if len(purchased) == 0 {
		t.Fatalf("a question about purchasing named no purchase date column")
	}
	for _, col := range purchased {
		if !strings.Contains(strings.ToLower(col), "purchase") {
			t.Errorf("unrelated column %q nominated by a purchase question", col)
		}
	}
	if named := questionNamedDateColumns("how many animals do we have"); len(named) != 0 {
		t.Errorf("a question naming no date nominated %v", named)
	}
}

// The server writes the allowance onto the sub-question; the planner cannot.
func TestTheAlternateDateAllowanceIsServerInjected(t *testing.T) {
	subs := []domain.SubQuestion{{
		ID: "s1", Route: domain.RouteSQL,
		// A plan that tried to grant itself the allowance is overwritten.
		Params: map[string]any{paramWindowAltCols: "verified_at"},
	}}
	w, _ := ResolveWindow("last quarter", trNow, nil)
	injectWindow(subs, w, "how many animals did we purchase last quarter")
	got, _ := subs[0].Params[paramWindowAltCols].(string)
	if strings.Contains(got, "verified_at") {
		t.Errorf("the plan's own allowance survived injection: %q", got)
	}
	if !strings.Contains(got, "purchase") {
		t.Errorf("the server allowance for a purchase question is missing: %q", got)
	}
}
