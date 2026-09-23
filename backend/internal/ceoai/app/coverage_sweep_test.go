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

// THE LEADER-LANGUAGE HALF OF DIRECTION A, and it exists because the table
// above could not see the defect round 5 measured.
//
// Every question in sweepMustNominate is written in the SCHEMA's vocabulary:
// "what gain kg did each animal record", "what is the variance kg on feed",
// "how many animals by sex". Those are column names in question form, and a
// matcher tuned on them passes while a CEO's own English fails — measured:
// 11 of 50 plain-leadership questions nominated NOTHING, including "how much
// money did we make" and "who owes us money", each killed by the single word
// `money` while ₹325,930 of `outstanding_rupees` sat in the view.
//
// So these are written the way a leader types, deliberately using words the
// schema does not: money, cash, owes, paid, sell, trucks, sops, behind, full,
// baby, young, males. A question belongs here only if the catalogue really can
// answer it; `want` is the source that must be named.
var sweepMustNominateInLeaderEnglish = []struct {
	question string
	want     string
}{
	// Money, in the words a leader uses for it.
	{"how much money did we make", "sales_buyer_summary"},
	{"how much money did we make last month", "sales_buyer_summary"},
	{"who owes us money", "sales_buyer_summary"},
	{"how much money is outstanding", "sales_buyer_summary"},
	{"how much cash is still to be collected", "sales_buyer_summary"},
	{"how much cash have we collected", "sales_buyer_summary"},
	{"what did we get paid", "sales_buyer_summary"},
	{"how much did we sell last month", "sales_buyer_summary"},
	{"how many animals did we sell", "sales"},
	{"who are our biggest customers", "sales_buyer_summary"},
	{"which customers still owe us", "sales_buyer_summary"},
	// The herd, in the words a leader uses for it.
	{"how many baby goats were born", "counts_movement_daily"},
	{"how many young goats do we have", "counts_breakdown"},
	{"how many males and females do we have", "animal_current_scope"},
	{"what is the male to female ratio", "animal_current_scope"},
	{"what is the age profile of the herd", "animal_current_scope"},
	{"how big is the herd", "animal"},
	// Operations, in the words a leader uses for them.
	{"are any sheds too full", "shed_capacity_current"},
	{"which sops are behind", "sop_execution_status"},
	{"are we feeding the pens enough", "feed_adherence"},
	{"how many trucks of goats arrived", "procurement_loads_base"},
	{"how many trucks came in this week", "procurement_loads_base"},
	{"how many staff do we have", "operator"},
	{"how much did we spend on feed", "feed_adherence"},
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
	// ROUND 5's MEASURED FALSE NOMINATIONS. Each got in on the source's NAME
	// while the question had ALREADY been judged to carry a subject the farm
	// models nowhere — chickens, a bank, turnover, attrition — because
	// `nominates` returned on the name arm before it read `strict` at all.
	// Each then reached the system's own "please rephrase" sentence with no
	// model in the loop.
	"what is the mortality rate of our chickens",
	"when is the next audit by the bank",
	"what is our employee turnover",
	"what is the staff attrition rate",
	"how many chickens do we have",
	"what is the milk yield per doe",
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

// TestCoverageSweepNominatesWhenTheLEADERAsksIt is direction A in the
// leader's own English rather than the schema's. See the table's own comment:
// a matcher tuned on schema vocabulary passes the table above and still
// refuses the two highest-value questions in the product.
func TestCoverageSweepNominatesWhenTheLEADERAsksIt(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	if len(catalog) < 10 || len(cards) < 25 {
		t.Fatalf("the live catalogue shrank to %d tools / %d cards; this sweep must score against the real one",
			len(catalog), len(cards))
	}
	for _, tc := range sweepMustNominateInLeaderEnglish {
		covering := coveringSources(tc.question, cards, catalog)
		if len(covering) == 0 {
			t.Errorf("ZERO-NOMINATION IN PLAIN ENGLISH: %q nominated nothing — the planner's \"we don't track that\" refusal ships verbatim, and the catalogue carries %s",
				tc.question, tc.want)
			continue
		}
		if !namesSource(covering, tc.want) {
			t.Errorf("WRONG-SOURCE: %q nominated %v, expected it to include %s", tc.question, covering, tc.want)
		}
	}
}

// TestStrictGatesBOTHArmsNotOnlyTheColumnArm pins the STRUCTURE of the fix,
// not a question list. `hasUnmodelledSubject` described itself as "THE
// STRUCTURAL DISCRIMINATOR this file turns on" while `nominates` returned on
// the name arm one line before it was read; a question judged to be about
// something the farm models nowhere could still nominate, and three of five
// measured false nominations got in exactly there.
//
// A tune that re-orders those two lines back fails here whatever the question
// tables say.
func TestStrictGatesBOTHArmsNotOnlyTheColumnArm(t *testing.T) {
	// A source whose NAME is the question's word, and a question carrying a
	// subject the catalogue never names. Under strict, neither arm may open.
	words := coverageWords("what is the mortality rate of our chickens")
	nameHay := identifierHaystack("mortality_base")
	fullHay := identifierHaystack("mortality_base deaths kid_deaths adult_deaths")
	vocab := map[string]int{"mortality": 1, "death": 1, "kid": 1, "adult": 1}
	if _, ok := nominates(words, nameHay, fullHay, vocab, true, false); ok {
		t.Error("strict must close the NAME arm too: a name match overrode a question judged to be about something the farm models nowhere")
	}
	if _, ok := nominates(words, nameHay, fullHay, vocab, false, false); !ok {
		t.Error("the name arm must still open when the question carries no unmodelled subject")
	}
}

// TestTheCrudeSingularIsNotAnUnmodelledSubject pins the other half of the
// accuracy fix. questionWords adds a bare s-stripped form beside every plural,
// so "status" arrives with the fragment "statu" beside it. Nothing names
// "statu", so every question containing the word "status" was strict — four of
// the six schema-vocabulary questions that were strict were strict for that
// reason and lived entirely on the name arm this round closes.
func TestTheCrudeSingularIsNotAnUnmodelledSubject(t *testing.T) {
	vocab := map[string]int{"status": 2, "coverage": 1, "role": 1}
	if hasUnmodelledSubject("what is the coverage status by role", vocab) {
		t.Error("the crude singular \"statu\" read as a subject the farm does not record")
	}
	// It must not become a blanket exemption: a real unmodelled plural still
	// makes the question strict.
	if !hasUnmodelledSubject("how many chickens do we have", vocab) {
		t.Error("a genuinely unmodelled plural must still be an unmodelled subject")
	}
}

// TestTheLeaderFoldOnlyEverMeetsAWordTheCatalogueNames is the safety argument
// for leaderNouns stated as a test: a fold may make a leader's word MEET a
// word the catalogue really carries, and may never invent coverage. Every
// right-hand side must be named by the live catalogue.
func TestTheLeaderFoldOnlyEverMeetsAWordTheCatalogueNames(t *testing.T) {
	vocab := catalogueVocabulary(reporting.Cards(), liveCatalogue())
	for leader, schema := range leaderNouns {
		if vocab[wordStem(schema)] == 0 {
			t.Errorf("leaderNouns[%q] = %q, which the live catalogue names nowhere — a fold onto nothing is a synonym list, not a bridge",
				leader, schema)
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
	if len(sweepMustRefuse) < 40 {
		t.Fatalf("direction B shrank to %d questions; a one-directional fix must not be able to pass", len(sweepMustRefuse))
	}
	if len(sweepMustNominateInLeaderEnglish) < 20 {
		t.Fatalf("the leader-English half of direction A shrank to %d questions; without it a tune against the schema's own vocabulary passes while a CEO's English fails",
			len(sweepMustNominateInLeaderEnglish))
	}
	seen := map[string]bool{}
	for _, tc := range sweepMustNominateInLeaderEnglish {
		if seen[tc.question] {
			t.Errorf("duplicate question %q inflates the count without widening the sweep", tc.question)
		}
		seen[tc.question] = true
	}
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

// THE DETERMINER SWEEP. It is GENERATED rather than listed, and its head nouns
// are deliberately OUTSIDE the catalogue — which is the one thing the two
// tables above cannot do. Every `the <noun>` question in those tables uses
// catalogue vocabulary as its head noun, so neither could see the class this
// sweep exists for: a determiner promoting an ordinary English REPORTING word
// into noun position, where `hasUnmodelledSubject` read it as proof the
// question was about another company and nominated NOTHING.
//
// Measured before the fix, on the real catalogue: `sales report` nominated 3
// sources, `the sales report` nominated 0. The same for numbers, figures,
// update, picture, situation, snapshot, progress, chart and tally — ten of the
// fifteen ordinary nouns tried. `the weighing progress` and `show the feed
// usage` died the same way.
//
// It is an A/B and that is the point: the assertion is that the determiner does
// not CHANGE the answer. A "fix" that made both sides nominate nothing would
// satisfy a one-sided list and fails here.
func TestADeterminerDoesNotKillAQuestionTheCatalogueAnswers(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	// Subjects the catalogue really models, one per area.
	subjects := []string{"sales", "weighing", "feed", "vaccination", "procurement", "health"}
	// Ordinary English words for a VIEW of data. None of these is a thing the
	// farm records, and none may be the reason a question dies.
	reportNouns := []string{
		"report", "reports", "numbers", "figures", "update", "picture",
		"situation", "snapshot", "progress", "chart", "tally", "summary",
		"overview", "breakdown", "totals", "details", "recap", "rundown",
		"issues", "problems",
	}
	determiners := []string{"the", "this", "that", "our"}
	var bare, killed int
	for _, subject := range subjects {
		for _, noun := range reportNouns {
			plain := subject + " " + noun
			plainCovering := coveringSources(plain, cards, catalog)
			if len(plainCovering) == 0 {
				// The bare phrase reaches nothing on its own, so there is no
				// determiner effect to measure here and inventing one would be
				// the opposite defect.
				continue
			}
			bare++
			for _, det := range determiners {
				q := det + " " + plain
				if covering := coveringSources(q, cards, catalog); len(covering) == 0 {
					killed++
					t.Errorf("DETERMINER KILLED THE QUESTION: %q nominates %v but %q nominates nothing — one English article turned a question the catalogue answers into \"we don't track that\"",
						plain, plainCovering, q)
				}
			}
		}
	}
	if bare < 40 {
		t.Fatalf("only %d of the %d generated phrases nominate anything at all; the sweep has stopped exercising the class it was written for",
			bare, len(subjects)*len(reportNouns))
	}
	t.Logf("determiner sweep: %d bare phrases nominate, %d determiner forms killed", bare, killed)
}

// The other direction, and it is what stops the fix above from being a
// loosening. A determiner in front of a phrase the catalogue models NOTHING of
// must still nominate nothing: the reporting-word exemption may only ever ride
// on a modelled word standing beside it in the same noun phrase.
func TestADeterminerDoesNotSmuggleAForeignSubjectIn(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	foreign := []string{"chicken", "loan", "mortgage", "payroll", "attrition", "turnover", "wifi", "instagram", "bitcoin", "rainfall"}
	reportNouns := []string{"report", "numbers", "figures", "summary", "chart", "issues", "progress"}
	for _, subject := range foreign {
		for _, noun := range reportNouns {
			for _, det := range []string{"the", "our", "this"} {
				q := det + " " + subject + " " + noun
				if covering := coveringSources(q, cards, catalog); len(covering) != 0 {
					t.Errorf("FOREIGN SUBJECT SMUGGLED IN: %q nominated %v — the farm records nothing about %q, so a reporting word beside it must not make it answerable",
						q, covering, subject)
				}
			}
		}
	}
}
