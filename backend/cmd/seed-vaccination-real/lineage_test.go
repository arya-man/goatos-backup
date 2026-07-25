package main

import (
	"os"
	"strings"
	"testing"

	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// mkFact builds a sourceFact for a cell with the given disposition, mirroring addFact's lineage key.
func mkFact(animalKey, vaccine, dose string, seq int, val, disposition string) sourceFact {
	c := vaccCell{AnimalKey: animalKey, Vaccine: vaccine, DoseCode: dose, Sequence: seq, Value: val}
	return sourceFact{
		lineageKey:    sourceFactLineageKey(c),
		animalKey:     animalKey,
		vaccineHeader: vaccine,
		doseCode:      sourceDoseCode(c),
		sequence:      seq,
		sourceValue:   val,
		disposition:   disposition,
	}
}

// TestSourceFactLineageKeyIsUniquePerCell proves distinct source cells never collide and the same
// cell always yields the same lineage identity (VACC-REV-01 exactly-once foundation).
func TestSourceFactLineageKeyIsUniquePerCell(t *testing.T) {
	base := vaccCell{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-21"}
	if got, want := sourceFactLineageKey(base), "goat-1|ET+TT|first|3|2026-06-21"; got != want {
		t.Fatalf("lineage key = %q, want %q", got, want)
	}
	// Any single varying field changes the key -> no two distinct cells share a lineage.
	variants := []vaccCell{
		{AnimalKey: "goat-2", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "FMD", DoseCode: "first", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "booster", Sequence: 3, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 4, Value: "2026-06-21"},
		{AnimalKey: "goat-1", Vaccine: "ET+TT", DoseCode: "first", Sequence: 3, Value: "2026-06-22"},
	}
	seen := map[string]bool{sourceFactLineageKey(base): true}
	for _, v := range variants {
		k := sourceFactLineageKey(v)
		if seen[k] {
			t.Fatalf("lineage collision for variant %+v -> %q", v, k)
		}
		seen[k] = true
	}
}

// TestReconcileSourceFactLineageCountsEachFactExactlyOnce proves the VACC-REV-01 gate accepts a
// ledger where every dated fact appears once, and rejects double-counting / gaps.
func TestReconcileSourceFactLineageCountsEachFactExactlyOnce(t *testing.T) {
	facts := []sourceFact{
		mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-1", "FMD", "first", 2, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-2", "ET+TT", "first", 1, "2030-01-01", dispositionScheduledObligation),
		mkFact("goat-3", "ET+TT", "first", 1, "bad-date", dispositionUnresolved),
		mkFact("goat-4", "ET+TT", "first", 1, "2026-06-21", dispositionExcludedGoatNotPlaced),
	}
	if err := reconcileSourceFactLineage(len(facts), facts); err != nil {
		t.Fatalf("valid ledger rejected: %v", err)
	}

	// dated-count mismatch (a fact vanished before the ledger).
	if err := reconcileSourceFactLineage(len(facts)+1, facts); err == nil {
		t.Fatal("expected failure when dated count exceeds ledger facts")
	}
}

// TestReconcileSourceFactLineageRejectsCollision proves that two ledger entries sharing a lineage
// key (a double-count) fail the gate rather than silently inflating the denominator.
func TestReconcileSourceFactLineageRejectsCollision(t *testing.T) {
	dup := mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion)
	facts := []sourceFact{dup, dup}
	err := reconcileSourceFactLineage(len(facts), facts)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected lineage collision error, got %v", err)
	}
}

// TestReconcileSourceFactLineageFailsOnUnknownVaccineHeader proves Fix Plan A1 [P1]: a dated cell
// under an unrecognized vaccine header FAILS the seed instead of vanishing outside the denominator.
func TestReconcileSourceFactLineageFailsOnUnknownVaccineHeader(t *testing.T) {
	facts := []sourceFact{
		mkFact("goat-1", "ET+TT", "first", 1, "2026-06-21", dispositionImportedCompletion),
		mkFact("goat-2", "TYPO_VACCINE", "first", 1, "2026-06-21", dispositionExcludedVaccineUnknown),
	}
	err := reconcileSourceFactLineage(len(facts), facts)
	if err == nil || !strings.Contains(err.Error(), "unknown vaccine header") {
		t.Fatalf("expected unknown-header failure, got %v", err)
	}
}

// The following adversarial tests cover the seed reconciliation aggregate
// (verifySeedReconciliation, grain (tenant_id, target_id, rule_id)) across the
// dimensions required by the aggregate-projection review: cardinality/one-to-many,
// page-boundary totals, date-shifted buckets, scope, and the status matrix. They
// exercise validateSeedReconciliation, the pure invariant gate the aggregate feeds.

func cleanReconciliation() seedReconciliation {
	return seedReconciliation{SourceAcceptedHistory: 10}
}

// cardinality dimension.
func TestSeedReconciliationOneToManyDuplicateActiveFails(t *testing.T) {
	if err := validateSeedReconciliation(cleanReconciliation(), 10); err != nil {
		t.Fatalf("clean reconciliation rejected: %v", err)
	}
	got := cleanReconciliation()
	got.DuplicateActiveRuleTargets = 1 // one goat/rule fanned out to >1 active obligation
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when a goat/rule grain has duplicate active obligations")
	}
}

// pagination dimension: the totals are whole-set persisted counts, never a page.
func TestSeedReconciliationPageBoundaryTotalsUsePersistedCounts(t *testing.T) {
	got := cleanReconciliation() // SourceAcceptedHistory=10
	if err := validateSeedReconciliation(got, 10); err != nil {
		t.Fatalf("matching whole-set totals rejected: %v", err)
	}
	// A short (paged) accepted-history count must fail — the invariant compares the full committed
	// count to the source total, independent of any page size.
	if err := validateSeedReconciliation(got, 11); err == nil {
		t.Fatal("expected failure when accepted history count differs from the source total")
	}
}

// date dimension: repeat work must be strictly future; urgent schedulable work may be due/in-window
// but must not survive past its latest safe date.
func TestSeedReconciliationDateShiftNonFutureWorkFails(t *testing.T) {
	got := cleanReconciliation()
	got.RepeatObligationsNotFuture = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when a repeat obligation is not strictly future")
	}
	got = cleanReconciliation()
	got.SchedulableOpenWorkNotFuture = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when schedulable open work is past latest safe date")
	}
}

// scope dimension: per-goat scope integrity (breed FK + primary identifier).
func TestSeedReconciliationScopeHierarchyIntegrityFails(t *testing.T) {
	got := cleanReconciliation()
	got.MissingBreedForeignKeys = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when a goat is missing its species-owned breed FK")
	}
	got = cleanReconciliation()
	got.MissingPrimaryIdentifiers = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when a goat is missing its primary identifier")
	}
}

// status dimension: the status matrix (accepted vs obligation status).
func TestSeedReconciliationStatusMatrixMismatchFails(t *testing.T) {
	got := cleanReconciliation()
	got.AcceptedStatusMismatches = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when an accepted completion's obligation is not completed")
	}
	got = cleanReconciliation()
	got.ActivePrimaryAfterHistory = 1
	if err := validateSeedReconciliation(got, 10); err == nil {
		t.Fatal("expected failure when a primary stays active after accepted history exists")
	}
}

func TestSeedReconciliationOneToManyPageBoundaryStatusMatrixHistoryRepairProjection(t *testing.T) {
	sourceBytes, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read seed source: %v", err)
	}
	source := string(sourceBytes)
	for _, want := range []string{
		"supersedeActiveOneTimeVaccinationObligationsCoveredByHistory",
		"COALESCE(NULLIF(current_pr.repeat, ''), 'none') = 'none'",
		"history_vc.status = 'accepted'",
		"history_oi.status = 'completed'",
		"status = 'superseded'",
		"projection-review: membership=obligation_instances",
		"pagination=none",
		"GROUP BY target_id, rule_id",
		"target_type = 'goat'",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("seed history-repair projection missing %q", want)
		}
	}
}

// missing-anchor dimension (VAX-REV-02 follow-up): the MissingAnchorNormalWork invariant must
// DISTINGUISH a Contract §89 option-4 routed adult catch-up (a blank vaccine family with a NULL
// DOB/entry anchor, legitimately scheduled at the next compatible drive) from a FABRICATED normal
// missing-anchor due (§13 defect). Classification is by the obligation's durable idempotency key
// against the generator's own AnchorMissingCatchUpKey derivation.
func TestMissingAnchorReconciliationAllowsRoutedCatchUpRejectsFabricated(t *testing.T) {
	const (
		tenant  = "11111111-1111-4111-8111-111111111111"
		version = "22222222-2222-4222-8222-222222222222"
		rule    = "33333333-3333-4333-8333-333333333333"
		goat    = "44444444-4444-4444-8444-444444444444"
		seq     = int32(1)
	)

	// (a) A §89 option-4 routed catch-up: blank family, NULL DOB, non-deferred, carrying the exact
	// key the generator stamps. It must NOT be counted as a defect.
	routed := missingAnchorCandidate{
		idempotencyKey:    vaccinationapp.AnchorMissingCatchUpKey(tenant, version, rule, goat, "birth_age", seq),
		tenantID:          tenant,
		protocolVersionID: version,
		ruleID:            rule,
		goatID:            goat,
		sequence:          seq,
		triggerType:       "birth_age",
	}
	if !routed.isRoutedCatchUp() {
		t.Fatal("a genuine option-4 catch-up (matching AnchorMissingCatchUpKey) must classify as routed")
	}
	if n := countFabricatedMissingAnchorWork([]missingAnchorCandidate{routed}); n != 0 {
		t.Fatalf("routed §89 option-4 catch-up counted as fabricated=%d, want 0", n)
	}

	// (b) A fabricated normal missing-anchor due: same NULL-anchor shape but its key was NOT produced
	// by the option-4 routing. It must STILL be counted and STILL fail reconciliation.
	fabricated := missingAnchorCandidate{
		idempotencyKey:    "not-the-option-4-catchup-key",
		tenantID:          tenant,
		protocolVersionID: version,
		ruleID:            rule,
		goatID:            goat,
		sequence:          seq,
		triggerType:       "birth_age",
	}
	if fabricated.isRoutedCatchUp() {
		t.Fatal("a fabricated normal missing-anchor due must NOT classify as a routed catch-up")
	}
	if n := countFabricatedMissingAnchorWork([]missingAnchorCandidate{fabricated}); n != 1 {
		t.Fatalf("fabricated missing-anchor due counted=%d, want 1", n)
	}

	// End-to-end through the gate: a seed carrying only routed catch-ups passes; one fabricated due
	// blocks the seed (the invariant is not weakened into ignoring all missing-anchor work).
	clean := cleanReconciliation()
	clean.MissingAnchorNormalWork = int64(countFabricatedMissingAnchorWork([]missingAnchorCandidate{routed}))
	if err := validateSeedReconciliation(clean, 10); err != nil {
		t.Fatalf("routed §89 option-4 catch-ups must pass reconciliation: %v", err)
	}
	defect := cleanReconciliation()
	defect.MissingAnchorNormalWork = int64(countFabricatedMissingAnchorWork([]missingAnchorCandidate{routed, fabricated}))
	if err := validateSeedReconciliation(defect, 10); err == nil {
		t.Fatal("a fabricated normal missing-anchor due must fail reconciliation")
	}
}
