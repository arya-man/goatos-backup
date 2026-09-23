package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE OUT-OF-SAMPLE SWEEP. coveringSources has exactly one production consumer
// -- the "not tracked" refusal override at orchestrator.go -- and three rounds
// of this PR oscillated between nominating too much and nominating too little,
// each round passing because it was tuned against the question list the
// previous round filed.
//
// This file is the pin that a one-directional tune cannot pass. Both tables are
// scored against the REAL cards and the REAL tool catalogue, both are large,
// and neither may be edited to make a change pass: a question moves between the
// tables only when the CATALOGUE changes, and a question is added only when it
// is one a leader would really ask.
//
// Direction A (mustNominate): the farm records this, so a planner refusal that
// says "we do not track that" has to be re-checked.
//
// Direction B (mustRefuse): the farm records NOTHING that answers this, so the
// refusal is the honest answer and must ship. Overriding it either makes the
// model answer a loan question from a video-review reject rate, or -- with no
// model left in the loop -- returns orchestrator.go's hardcoded "The underlying
// records exist, so please rephrase it", a false factual claim about the farm's
// data asserted by the system itself.

// mustNominate: every one of these is answerable from the live catalogue, and
// `want` is a source that must appear among the nominations.
var sweepMustNominate = []struct {
	question string
	want     string
}{
	// Sales and money.
	{"who are our biggest buyers by revenue", "sales_buyer_summary"},
	{"how much revenue did we book", "sales_buyer_summary"},
	{"what is the outstanding amount per buyer", "sales_buyer_summary"},
	{"how many deals closed", "sales_buyer_summary"},
	{"what price per kg are we getting", "sales_deal_lines_closed"},
	{"what was the total weight kg we sold", "sales_deal_lines_closed"},
	{"which buyers are repeat buyers", "sales_buyer_summary"},
	{"what payment have we received", "sales_buyer_summary"},
	// Herd and animals.
	{"how many animals do we have by breed", "animal_current_scope"},
	{"what is the lifecycle status of our animals", "animal_current_scope"},
	{"how many animals by sex", "animal_current_scope"},
	{"what is the age days distribution of the herd", "counts_breakdown"},
	{"which animals are at which management stage", "animal_current_scope"},
	// Weighing and growth.
	{"what is the average weight of our animals", "weighing_latest_individual_weight"},
	{"what daily gain are the animals putting on", "growth_adg_pairs"},
	{"what gain kg did each animal record", "growth_adg_pairs"},
	{"what was the latest weight kg recorded per animal", "weighing_latest_individual_weight"},
	{"how many scans did we capture", "weighing_capture_activity"},
	{"how many animals are pending weighing verification", "weighing_verification_status"},
	// Feed.
	{"how much feed did we direct versus feed", "feed_adherence"},
	{"what is the variance kg on feed", "feed_adherence"},
	{"what quantity fed was recorded", "feed_completions_base"},
	{"which feed directions are blocked", "feed_direction_current"},
	// Vaccination.
	{"how many vaccine doses do we need to pick", "vaccination_dose_pickup"},
	{"how many animals are overdue for vaccination", "vaccination"},
	{"what is the utilization of each vaccinator", "vaccination_operator_status"},
	{"what is the daily capacity per operator", "vaccination_operator_status"},
	{"which vaccination obligations are still open", "vaccination_obligations_base"},
	{"what was the review status of the prearrival history", "vaccination_prearrival_history_review"},
	// Procurement.
	{"how many loads did we take in", "procurement_loads_base"},
	{"what is the expected count on each load", "procurement_loads_base"},
	{"what stage is the procurement pipeline at", "procurement_pipeline"},
	{"what health blockers were raised on a load", "source_entry_health_status"},
	{"what is the evidence status on source entry", "source_entry_health_status"},
	// Mortality.
	{"how many deaths did we have", "mortality_base"},
	{"how many kid deaths versus adult deaths", "mortality_base"},
	{"which cause is killing the most animals", "mortality_base"},
	{"which disease is hitting us hardest", "mortality_base"},
	{"what is the active population", "mortality_base"},
	// Inventory.
	{"which items are below reorder", "inventory_stock_position"},
	{"what is the stock on hand", "inventory_stock_position"},
	// Workforce and operations.
	{"how many workers have overdue work", "workforce_coverage_status"},
	{"what is the coverage status by role", "workforce_coverage_status"},
	{"which tasks are verified", "workforce_tasks_base"},
	{"how many exceptions are open by severity", "ops_exception_queue"},
	{"what is in the action center by area", "action_center_current"},
	{"which sop tasks are overdue", "sop_execution_status"},
	// Verification and integrity.
	{"how many videos were reviewed", "verifier_review_integrity"},
	{"what is the reject rate on reviews", "verifier_review_integrity"},
	{"how many items are pending in the verification queue", "verification_queue"},
	// Capacity, notifications, audit.
	{"which sheds are over capacity", "shed_capacity_current"},
	{"how many notifications failed", "notification_delivery_health"},
	{"what audit activity did we see by actor", "audit_activity_summary"},
	// Counts and movement.
	{"how many births and transfers out did we see", "counts_movement_daily"},
	{"how many approvals are pending", "counts_movement_daily"},
	{"how many kids do we have", "counts_breakdown"},
}

// mustRefuse: the catalogue models NOTHING that answers these. Each is a real
// leadership question about a subject a goat farm could plausibly have and this
// one does not record. A nomination here is the loose failure.
var sweepMustRefuse = []string{
	// Finance the farm does not model.
	"what is the interest rate on our loan",
	"how much did we pay in loan interest",
	"what is our ebitda",
	"how much tax did we pay",
	"what is our payroll cost",
	"how much did electricity cost",
	"what is our diesel spend",
	"how much rent do we owe on the land",
	"what is the depreciation on our equipment",
	"what is our cash runway",
	// Insurance, legal, HR.
	"how many insurance claims did we file",
	"what is our insurance premium",
	"how many grievances were raised",
	"how many training hours did we deliver",
	"what is our accident frequency",
	// Milk and products the farm does not record.
	"what is the milk fat percentage by breed of cow",
	"how many litres of milk did we collect",
	"how much cheese did we produce",
	"what is our wool yield",
	"how much manure did we sell",
	// Agronomy and utilities.
	"how much rainfall did we get",
	"what is the soil ph in the paddock",
	"how much water did we pump",
	"what is the borewell level",
	"how much fodder did we grow ourselves",
	"what is the ambient temperature in the barn",
	// Operational subjects with no read model.
	"which sheds missed their milking session yesterday",
	"how many treatment sessions were missed yesterday",
	"how much colostrum did each shed dispense yesterday",
	"what is the pregnancy rate of our does",
	"how many embryo transfers did we do",
	"how many hoof trims were done",
	// Systems and marketing.
	"how many website visitors did we get",
	"what is our instagram engagement",
	"how many support tickets are open",
	"what is our app crash rate",
	"how many emails did the marketing team send",
	// Governance / people questions with no source.
	"who will plan and manage the shed",
	"what is the board meeting schedule",
	"how many investors have we onboarded",
}

// TestCoverageSweepNominatesEverythingTheCatalogueCarries is direction A.
func TestCoverageSweepNominatesEverythingTheCatalogueCarries(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	if len(catalog) < 10 || len(cards) < 25 {
		t.Fatalf("the live catalogue shrank to %d tools / %d cards; this sweep must score against the real one",
			len(catalog), len(cards))
	}
	var zero, wrong int
	for _, tc := range sweepMustNominate {
		covering := coveringSources(tc.question, cards, catalog)
		if len(covering) == 0 {
			zero++
			t.Errorf("ZERO-NOMINATION: %q nominated nothing — the planner's \"we don't track that\" refusal ships verbatim, and the catalogue carries %s",
				tc.question, tc.want)
			continue
		}
		if !namesSource(covering, tc.want) {
			wrong++
			t.Errorf("WRONG-SOURCE: %q nominated %v, expected it to include %s", tc.question, covering, tc.want)
		}
	}
	if zero+wrong > 0 {
		t.Logf("direction A: %d zero-nominations, %d wrong-source, of %d questions", zero, wrong, len(sweepMustNominate))
	}
}

// TestCoverageSweepRefusesEverythingTheCatalogueDoesNotCarry is direction B.
// It is the half that a tune for direction A must not be able to break.
func TestCoverageSweepRefusesEverythingTheCatalogueDoesNotCarry(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	if len(catalog) < 10 || len(cards) < 25 {
		t.Fatalf("the live catalogue shrank to %d tools / %d cards; this sweep must score against the real one",
			len(catalog), len(cards))
	}
	for _, question := range sweepMustRefuse {
		if covering := coveringSources(question, cards, catalog); len(covering) != 0 {
			t.Errorf("FALSE NOMINATION: %q nominated %v — the farm records none of this, so the refusal override either answers it from an unrelated column or asserts %q with no model in the loop",
				question, covering, "The underlying records exist")
		}
	}
}

// TestBothSweepDirectionsAreScoredTogether is the guard on the guard: a future
// change that deletes or empties either table, or that scores one direction
// against a stub catalogue, fails here rather than passing quietly.
func TestBothSweepDirectionsAreScoredTogether(t *testing.T) {
	if len(sweepMustNominate) < 50 {
		t.Fatalf("direction A shrank to %d questions; the sweep is the pin, not a sample", len(sweepMustNominate))
	}
	if len(sweepMustRefuse) < 35 {
		t.Fatalf("direction B shrank to %d questions; a one-directional fix must not be able to pass", len(sweepMustRefuse))
	}
	seen := map[string]bool{}
	for _, tc := range sweepMustNominate {
		if seen[tc.question] {
			t.Errorf("duplicate question %q inflates the count without widening the sweep", tc.question)
		}
		seen[tc.question] = true
	}
	for _, q := range sweepMustRefuse {
		if seen[q] {
			t.Errorf("%q is in BOTH directions", q)
		}
		seen[q] = true
	}
}

// THE RESIDUAL, RECORDED RATHER THAN QUIETLY DROPPED FROM DIRECTION B. A
// question that NAMES a noun the catalogue genuinely models, and asks about an
// activity it does not, still nominates that noun's source: "how many animals
// were dewormed" reaches the animal views on `animal`, "what is the semen straw
// inventory" reaches inventory_stock_position on `inventory`, "what is the
// staff attrition rate" reaches vaccination_operator_status on staff->operator.
//
// It is a WEAKER failure than the blocker this file exists for, and knowingly
// accepted: the nomination names a source that really does carry the question's
// own noun, so the re-planned read is at least about the thing asked after —
// unlike "interest rate on our loan" landing on a video-review reject rate,
// where nothing in the question is modelled anywhere. Closing it needs the
// source to say what it does NOT carry, which no card does today.
//
// The assertion is that the nomination stays ON THE NOUN'S OWN SOURCE. If one
// of these ever reaches an unrelated view, that is the blocker class returning.
func TestAModelledNounStillNominatesItsOwnSourceAndNoOther(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, tc := range []struct{ question, want string }{
		{"how many animals were dewormed", "animal"},
		{"what is the semen straw inventory", "inventory_stock_position"},
		{"what is the staff attrition rate", "vaccination_operator_status"},
	} {
		covering := coveringSources(tc.question, cards, catalog)
		if len(covering) == 0 {
			t.Logf("RESIDUAL CLOSED: %q now nominates nothing — move it into sweepMustRefuse", tc.question)
			continue
		}
		if !namesSource(covering, tc.want) {
			t.Errorf("%q nominated %v, which does not carry the question's own noun — this is the unrelated-source blocker class",
				tc.question, covering)
		}
	}
}

// RECORDED GAPS, NOT HIDDEN ONES. These are questions the catalogue does carry
// an answer for but that coverage still cannot reach, because the leader's word
// and the schema's word are different words and this file keeps no measure
// synonym list ("cash" vs `outstanding_rupees`, "truck" vs `load`, "invoice" vs
// `deal`). They are recorded as a test so that closing one is visible: the
// sanctioned way to close it is to give the CARD the farm's word, never to
// loosen the matcher — which is exactly what re-opens direction B.
func TestRecordedVocabularyGapsAreStillGaps(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, q := range []string{
		"how much cash is still to be collected",
		"are any invoices unpaid beyond thirty days",
		"how many trucks came in this week",
		"what is the male to female ratio",
	} {
		if covering := coveringSources(q, cards, catalog); len(covering) != 0 {
			t.Logf("GAP CLOSED: %q now nominates %v — move it into sweepMustNominate", q, covering)
		}
	}
	// The gap is a vocabulary gap, not a modelling one: the schema's own word
	// for each of these reaches its source.
	for _, tc := range []struct{ question, want string }{
		{"how much is outstanding from our buyers", "sales_buyer_summary"},
		{"how many loads came in this week", "procurement_loads_base"},
		{"how many animals by sex", "animal_current_scope"},
	} {
		covering := coveringSources(tc.question, cards, catalog)
		if !namesSource(covering, tc.want) {
			t.Errorf("the schema's OWN word stopped reaching its source: %q nominated %v, want %s",
				tc.question, covering, tc.want)
		}
	}
}
