package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// "No rows" is not "not modelled". The held-out judge caught the assistant
// telling the CEO the farm does not track weight history and does not track
// sales payment, when the reads for both exist and are merely EMPTY — an answer
// that is wrong today and still wrong the day the first row lands, because
// nothing about it depends on the data. Before such a refusal is passed on, the
// catalogue is asked the same question.
func TestARefusalIsRecheckedAgainstTheReadsTheCatalogueOffers(t *testing.T) {
	catalog := []ports.ToolSpec{{
		Name:        "sales_overview",
		Route:       domain.RouteAPI,
		Description: "Sales overview for closed deals: sold animals, sheep/goat split, revenue, deals, monthly sales",
		Params:      []string{"farm", "month"},
	}}
	for _, question := range []string{
		"what weight did we record for each animal weighed this month",
		"what were our sales revenue and deals last month",
	} {
		covering := coveringSources(question, reporting.Cards(), catalog)
		if len(covering) == 0 {
			t.Errorf("no source nominated for %q — the refusal would stand unchecked", question)
			continue
		}
		feedback := coverageFeedback(covering)
		if !strings.Contains(strings.ToLower(feedback), "no rows yet") {
			t.Errorf("feedback does not tell the planner an empty source is still an answer: %q", feedback)
		}
	}
}

// Only a refusal that CLAIMS the data is not recorded is re-checked. A scope,
// safety or genuinely-unanswerable refusal must stand as written.
func TestOnlyANotTrackedClaimIsRecheckedAgainstTheCatalogue(t *testing.T) {
	for _, r := range []string{
		"The system does not track individual animal weight history.",
		"Buyer payment state isn't tracked in the system.",
		"That cannot be answered from the available data.",
	} {
		if !notTrackedRefusal(r) {
			t.Errorf("should be re-checked: %q", r)
		}
	}
	for _, r := range []string{
		"I can only answer for your own organization.",
		"I can't help with that request.",
		"I'm read-only and can't change any records.",
	} {
		if notTrackedRefusal(r) {
			t.Errorf("must stand as written, not re-planned: %q", r)
		}
	}
}

// A question about something the catalogue genuinely does not carry nominates
// nothing, so its refusal is passed through unchanged.
func TestAQuestionNoReadCoversStillRefuses(t *testing.T) {
	if covering := coveringSources("what is the milk fat percentage by breed of cow", reporting.Cards(), nil); len(covering) > 0 {
		t.Errorf("nominated %v for a question nothing in the catalogue answers", covering)
	}
}
