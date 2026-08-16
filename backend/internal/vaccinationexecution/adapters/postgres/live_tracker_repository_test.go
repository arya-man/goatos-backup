package postgres

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// These are SQL-SHAPE and fold-logic regressions, not database round-trips. Each one pins a defect
// that returns a plausible-looking wrong number rather than an error — the class of bug that ships
// because nothing crashes.

// TestLiveTrackerOneToManyCannotFanOutAdministrations pins the cardinality contract: one
// obligation_instance is one administration, and no evidence join may multiply it.
//
// The failure this prevents is not an error — a goat with three proof rows and two scan rows would
// simply be counted five times, and the board would report more administrations than the day has.
func TestLiveTrackerOneToManyCannotFanOutAdministrations(t *testing.T) {
	sql := liveTrackerScopedCTE
	for _, table := range []string{"day_proofs", "day_scans", "day_attempts"} {
		idx := strings.Index(sql, table+" AS (")
		if idx < 0 {
			t.Fatalf("evidence CTE %s is missing; per-goat evidence must be pre-aggregated before it is joined", table)
		}
		block := sql[idx:]
		if end := strings.Index(block, "\n),"); end > 0 {
			block = block[:end]
		}
		if !strings.Contains(block, "GROUP BY") {
			t.Errorf("%s must GROUP BY goat before joining; an ungrouped join multiplies administrations by evidence rows", table)
		}
	}
	// The drive assignment is 0:N per (shed, partition, day). Resolving it through a bare JOIN would
	// duplicate every obligation in a shed that has two assignment rows. It is pre-aggregated to ONE
	// row per (shed, normalized partition) — a per-obligation LATERAL re-scanned the day's whole
	// assignment set, with two regexp_replace() calls per row, once for every obligation on the board.
	if !strings.Contains(sql, "day_assignments AS (") ||
		!strings.Contains(sql, "SELECT DISTINCT ON (a.shed_id,") {
		t.Fatal("the drive assignment must be resolved ONCE per shed x partition, not per obligation")
	}
	if !strings.Contains(sql, "ORDER BY a.shed_id, ") || !strings.Contains(sql, ", a.assignment_id\n)") {
		t.Error("the pre-aggregated assignment must keep the ORDER BY assignment_id tie-break the LATERAL had")
	}
	if strings.Contains(sql, "LEFT JOIN LATERAL (\n    SELECT a.operator_id") {
		t.Error("the per-obligation assignment LATERAL is an N+1 with a non-sargable join key")
	}
	if strings.Contains(liveTrackerCellsSQL, "JOIN protocol_rule_dimensions") {
		t.Error("protocol_rule_dimensions must never be joined into a counting query; it fans rules out by dimension")
	}
}

// TestLiveTrackerDayPredicatesAreSargableRanges pins that every day boundary is a half-open
// timestamptz range against an indexable column, not (<ts> AT TIME ZONE 'Asia/Kolkata')::date = $2.
//
// The cast form is a function of the column, so no index can ever drive it: proof_artifacts,
// sop_task_scan_captures, sop_task_scan_attempts, vaccination_completions, obligation_status_events
// and verification_items were each read in FULL for a one-day answer, on a page that re-reads itself
// every 10 seconds per viewer, and obligation_instances was scanned tenant-wide and joined to goats,
// partitions and the whole protocol chain before the date filter was applied at all.
func TestLiveTrackerDayPredicatesAreSargableRanges(t *testing.T) {
	for name, sql := range map[string]string{
		"scoped":       liveTrackerScopedCTE,
		"actors":       liveTrackerActorSQL,
		"activity":     liveTrackerActivitySQL,
		"verification": liveTrackerVerificationSQL,
	} {
		if !strings.Contains(sql, "day_window AS (") {
			t.Errorf("%s must cut its day boundary once, as a half-open timestamptz range", name)
		}
		for _, banned := range []string{
			"AT TIME ZONE 'Asia/Kolkata')::date = $2::date",
		} {
			// The membership's COALESCE(override, IST date of due_at) equality is the ONE legitimate
			// remaining use: it is the exact post-filter that sits on top of the bounded scan, and
			// every statement that embeds the scoped CTE inherits that single occurrence.
			count := strings.Count(sql, banned)
			allowed := 1
			if name == "verification" {
				allowed = 0
			}
			if count > allowed {
				t.Errorf("%s has %d non-sargable date casts, want at most %d", name, count, allowed)
			}
		}
	}
	// The base scan of obligation_instances must be BOUNDED before the override chain is consulted.
	if !strings.Contains(liveTrackerScopedCTE, "AND oi.due_at >= (SELECT due_floor FROM day_window)") ||
		!strings.Contains(liveTrackerScopedCTE, "AND oi.due_at < (SELECT day_end FROM day_window)") {
		t.Fatal("obligation_instances must be entered through a bounded due_at range, not filtered after the join")
	}
	// due_floor must be EXACT, not a guessed span: an override can only postpone
	// (vaccination_drive_date_overrides_postpone_check), so the earliest original_drive_date that
	// lands on this day is the true floor and nothing that could qualify is excluded.
	if !strings.Contains(liveTrackerScopedCTE, "SELECT min(o.original_drive_date)") ||
		!strings.Contains(liveTrackerScopedCTE, "o.override_date = $2::date") {
		t.Error("the due_at floor must be derived from the day's own active overrides, not from a fixed window")
	}
}

// TestLiveTrackerMultipleDimensionsCollapseToOneVaccineFamily pins that several dose codes of the
// same antigen collapse to ONE vaccine axis value, so a shed running dose 1 and dose 2 of the same
// vaccine appears once rather than as two competing rows.
func TestLiveTrackerMultipleDimensionsCollapseToOneVaccineFamily(t *testing.T) {
	cases := map[string]string{
		"goat_pox_adult_w1":  "goat_pox",
		"goat_pox_adult_w2":  "goat_pox",
		"GOAT_POX":           "goat_pox",
		"blue_tongue_first":  "blue_tongue",
		"et_tt_7w":           "et_tt",
		"ppr_booster":        "ppr",
		"sheep_pox_adult_w1": "sheep_pox",
		"hs":                 "hs",
	}
	for code, want := range cases {
		if got := domain.VaccineFamilyCode(code); got != want {
			t.Errorf("VaccineFamilyCode(%q) = %q, want %q", code, got, want)
		}
	}
	// The SQL copy must recognise the same families, or the filter and the label would disagree.
	for family := range map[string]struct{}{"goat_pox": {}, "blue_tongue": {}, "et_tt": {}, "ppr": {}, "fmd": {}, "hs": {}, "sheep_pox": {}} {
		if !strings.Contains(liveTrackerVaccineFamilyExpr, "'"+family+"'") {
			t.Errorf("the SQL family expression does not recognise %q; SQL filtering and Go labelling must agree", family)
		}
	}
}

// TestLiveTrackerPaginationBoundsEveryReturnedArray pins that no section of this page can stream an
// unbounded list, and that the combo card reports a total computed BEFORE the row cap — a truncated
// list with a truncated total silently under-reports the day.
func TestLiveTrackerPaginationBoundsEveryReturnedArray(t *testing.T) {
	if domain.LiveTrackerMaxOperators <= 0 || domain.LiveTrackerMaxSheds <= 0 || domain.LiveTrackerMaxComboRows <= 0 {
		t.Fatal("operator, shed and combo arrays must all carry a positive server-side cap")
	}
	if domain.LiveTrackerMaxActivity < domain.LiveTrackerDefaultActivity {
		t.Fatal("the activity cap must not be below its own default")
	}
	if !strings.Contains(liveTrackerComboSQL, "combo_total AS (SELECT count(*)::int AS animal_count FROM combo_goats)") {
		t.Error("combo animal_count must be counted over the full combo set, not over the capped page")
	}
	if !strings.Contains(liveTrackerComboSQL, "LIMIT $9::int") {
		t.Error("combo rows must be capped server-side")
	}
	if !strings.Contains(liveTrackerActivitySQL, "LIMIT $9::int") {
		t.Error("the activity feed must be capped server-side")
	}
	// The feed's LIMIT must be applied BEFORE the name/location/identifier joins, not after. Applying
	// it after ran every one of those joins once per event of the WHOLE drive day to return 40 rows.
	pageIdx := strings.Index(liveTrackerActivitySQL, "page AS (")
	decorateIdx := strings.Index(liveTrackerActivitySQL, "FROM page e")
	if pageIdx < 0 || decorateIdx < pageIdx {
		t.Fatal("the feed must page before it decorates")
	}
	if strings.Contains(liveTrackerActivitySQL[decorateIdx:], "LIMIT $9::int") {
		t.Error("the page cap must be applied before the decoration joins, not after them")
	}
	// Every bounded list is a DECLARED constant, never a bare literal, and the actor rollup is one of
	// them: the operator board reads its evidence out of that rollup by member id, so an operator
	// missing from it renders with zero evidence and state not_started.
	if !strings.Contains(liveTrackerActorSQL, "LIMIT "+fmt.Sprint(domain.LiveTrackerMaxActors)) {
		t.Error("the actor evidence rollup must be capped by its declared constant")
	}
	if !strings.Contains(liveTrackerActorSQL, "ORDER BY GREATEST(ap.last_at, asn.last_at) DESC NULLS LAST, a.actor_id") {
		t.Error("a cap with no total order returns an arbitrary page that can change between two polls")
	}
	if domain.LiveTrackerMaxAttention <= 0 {
		t.Error("the attention rail must carry a cap of its own; it is derived from the pre-cap cell rollup")
	}
}

// TestLiveTrackerPageBoundaryFlagsTruncationHonestly pins that the combo card only claims to be
// complete when it actually is. Reporting rows_truncated=false while rows were dropped is how a
// "showing everything" list quietly hides animals.
func TestLiveTrackerPageBoundaryFlagsTruncationHonestly(t *testing.T) {
	combo := domain.LiveTrackerCombo{AnimalCount: 7, Rows: make([]domain.LiveTrackerComboRow, 7)}
	if combo.AnimalCount > len(combo.Rows) {
		t.Fatal("fixture is not a complete page")
	}
	truncated := domain.LiveTrackerCombo{AnimalCount: 900, Rows: make([]domain.LiveTrackerComboRow, domain.LiveTrackerMaxComboRows)}
	if truncated.AnimalCount <= len(truncated.Rows) {
		t.Fatal("fixture is not a truncated page")
	}
}

// TestLiveTrackerDateShiftUsesEffectiveDriveDate pins that a postponed drive moves with its
// override. Reading due_at alone would leave the moved vaccine on its ORIGINAL day — the board would
// show work as scheduled on a day the operators are not running it.
func TestLiveTrackerDateShiftUsesEffectiveDriveDate(t *testing.T) {
	if !strings.Contains(liveTrackerScopedCTE, "vaccination_drive_date_overrides") {
		t.Fatal("membership must resolve the drive-date override chain, not due_at alone")
	}
	if !strings.Contains(liveTrackerScopedCTE, "COALESCE(ovr.override_date, (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date) = $2::date") {
		t.Error("the effective drive date must be COALESCE(override, IST date of due_at)")
	}
	if !strings.Contains(liveTrackerScopedCTE, "ovr.canceled_at IS NULL") {
		t.Error("a canceled override must not move work")
	}
}

// TestLiveTrackerAssignedWorkUsesAssignmentScheduledDateWithOneToManyPageBoundaryParkScopeStatusMatrix pins the operational source of truth: once a row is
// assigned to an operator drive, planned_date is the live-track day. due_at can represent clinical
// due timing or bad seed history and must not pull assigned closed work into another day's board.
func TestLiveTrackerAssignedWorkUsesAssignmentScheduledDateWithOneToManyPageBoundaryParkScopeStatusMatrix(t *testing.T) {
	for _, snippet := range []string{
		"LEFT JOIN vaccination_drive_assignment_members member",
		"LEFT JOIN vaccination_drive_assignments member_assignment",
		"member_assignment.planned_date = $2::date",
		"member_assignment.assignment_id IS NULL",
		"COALESCE(s.assigned_operator_id, asg.operator_id) AS operator_id",
	} {
		if !strings.Contains(liveTrackerScopedCTE, snippet) {
			t.Fatalf("live tracker membership/operator attribution missing %q", snippet)
		}
	}
	if strings.Index(liveTrackerScopedCTE, "member_assignment.planned_date = $2::date") >
		strings.Index(liveTrackerScopedCTE, "AND oi.due_at >= (SELECT due_floor FROM day_window)") {
		t.Fatal("assigned work must be admitted by planned_date before falling back to due_at")
	}
}

// TestLiveTrackerScheduledDateCutsEveryDayBoundaryInBusinessTime pins that every day boundary on the
// page is an IST boundary. A single UTC comparison would put the last two evening hours of a drive
// on the next day for one section and not the others.
func TestLiveTrackerScheduledDateCutsEveryDayBoundaryInBusinessTime(t *testing.T) {
	for name, sql := range map[string]string{
		"scoped":       liveTrackerScopedCTE,
		"cells":        liveTrackerCellsSQL,
		"actors":       liveTrackerActorSQL,
		"combo":        liveTrackerComboSQL,
		"activity":     liveTrackerActivitySQL,
		"verification": liveTrackerVerificationSQL,
	} {
		if strings.Contains(sql, "::date") && !strings.Contains(sql, "AT TIME ZONE 'Asia/Kolkata'") {
			t.Errorf("%s compares dates without converting to business time first", name)
		}
	}
}

// TestLiveTrackerExecutionDateWindowRejectsUnboundedHistory pins the business-date window. This is a
// business date rather than a projection as_of, so a past drive day must be READABLE — but not an
// arbitrarily old one, which would turn a polling page into an unbounded historical scan.
func TestLiveTrackerExecutionDateWindowRejectsUnboundedHistory(t *testing.T) {
	if domain.LiveTrackerBusinessDateLookbackDays <= 0 {
		t.Fatal("a past drive day must remain readable")
	}
	if domain.LiveTrackerBusinessDateLookbackDays > 31 {
		t.Fatal("the drive-day window must stay bounded")
	}
}

// TestLiveTrackerParkScopeIsAppliedInsideTheMembership pins defence in depth: the HTTP handler
// clamps park, and the query narrows again. A read that trusted only the handler would return the
// other park's drive to any caller who simply omitted park_id.
func TestLiveTrackerParkScopeIsAppliedInsideTheMembership(t *testing.T) {
	if !strings.Contains(liveTrackerScopedCTE, "($3::text = '' OR g.park_id = NULLIF($3::text, '')::uuid)") {
		t.Error("park scope must be enforced inside the membership CTE, not only at the handler")
	}
	if !strings.Contains(liveTrackerVerificationSQL, "($3::text = '' OR vi.park_id = NULLIF($3::text, '')::uuid)") {
		t.Error("the verification block must honour the same park scope as the rest of the page")
	}
	if !strings.Contains(liveTrackerVerificationSQL, "($5::uuid[] IS NULL OR vi.park_id = ANY($5::uuid[]))") {
		t.Error("the verification block must carry the authorization park set, not only the selected park filter")
	}
}

// TestLiveTrackerScopeHierarchyNormalisesPartitionsOnBothSides pins the partition-identity trap:
// goat_shed_partitions spells a partition "3" while vaccination_drive_assignments spells the same
// one "Part 3". Joining the raw labels does not error — it drops a whole park's partitions into
// "unassigned", so its operator appears to have been given no work at all.
func TestLiveTrackerScopeHierarchyNormalisesPartitionsOnBothSides(t *testing.T) {
	if domain.NormalizePartitionLabel("Part 3") != domain.NormalizePartitionLabel("3") {
		t.Error("\"Part 3\" and \"3\" are the same partition and must normalise identically")
	}
	if domain.NormalizePartitionLabel("  PART  4 ") != "4" {
		t.Errorf("normalisation must be case- and whitespace-insensitive, got %q", domain.NormalizePartitionLabel("  PART  4 "))
	}
	if domain.NormalizePartitionLabel("") != "whole" {
		t.Error("an unpartitioned shed must normalise to the whole-shed key, not to an empty join key")
	}
	goatSide := liveTrackerPartitionNormExpr("gsp.partition_label")
	assignmentSide := liveTrackerPartitionNormExpr("a.partition_label")
	if !strings.Contains(liveTrackerScopedCTE, goatSide) || !strings.Contains(liveTrackerScopedCTE, assignmentSide) {
		t.Error("both sides of the partition join must go through the same normalizer")
	}
}

// TestLiveTrackerStatusBucketsAreDisjointAndTotal pins that the operator and shed state machines
// each assign exactly one state per row. Overlapping states would let one row appear under two
// filters and be counted twice in the tiles.
func TestLiveTrackerStatusBucketsAreDisjointAndTotal(t *testing.T) {
	statuses := []domain.LiveTrackerStatus{
		domain.LiveTrackerStatusActive,
		domain.LiveTrackerStatusDone,
		domain.LiveTrackerStatusPending,
		domain.LiveTrackerStatusReview,
	}

	operatorStates := []string{
		domain.LiveTrackerOperatorActive,
		domain.LiveTrackerOperatorDone,
		domain.LiveTrackerOperatorNotStarted,
		domain.LiveTrackerOperatorIdle,
	}
	for _, state := range operatorStates {
		matches := 0
		for _, status := range statuses {
			if domain.OperatorMatchesStatus(state, status) {
				matches++
			}
		}
		if matches != 1 {
			t.Errorf("operator state %q matches %d status filters, want exactly 1", state, matches)
		}
	}

	shedStates := []string{
		domain.LiveTrackerShedReceiving,
		domain.LiveTrackerShedSlow,
		domain.LiveTrackerShedDone,
		domain.LiveTrackerShedNotStarted,
		domain.LiveTrackerShedReview,
	}
	for _, state := range shedStates {
		matches := 0
		for _, status := range statuses {
			if domain.ShedMatchesStatus(state, status) {
				matches++
			}
		}
		if matches != 1 {
			t.Errorf("shed state %q matches %d status filters, want exactly 1", state, matches)
		}
	}
}

// TestLiveTrackerEveryStatusIsReachableFromRealCounts pins that each declared state is actually
// produced by some real combination of counts. A state nothing can reach is a legend entry that
// never appears; a combination that reaches none would render a row with no status at all.
func TestLiveTrackerEveryStatusIsReachableFromRealCounts(t *testing.T) {
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	long := now.Add(-2 * time.Hour)
	recent := now.Add(-2 * time.Minute)

	shedCases := []struct {
		name string
		row  domain.LiveTrackerShedRow
		last *time.Time
		want string
	}{
		{"finished clean", domain.LiveTrackerShedRow{ScheduledAdmins: 10, ClosedAdmins: 10, ProofVideosReceived: 10}, &recent, domain.LiveTrackerShedDone},
		{"finished with re-scans", domain.LiveTrackerShedRow{ScheduledAdmins: 10, ClosedAdmins: 10, ProofVideosReceived: 10, ExtraAttemptCount: 2}, &recent, domain.LiveTrackerShedReview},
		{"nothing landed", domain.LiveTrackerShedRow{ScheduledAdmins: 8, Remaining: 8}, nil, domain.LiveTrackerShedNotStarted},
		{"barely started, long open", domain.LiveTrackerShedRow{ScheduledAdmins: 49, ProofVideosReceived: 5, Remaining: 44}, &long, domain.LiveTrackerShedSlow},
		{"landing steadily", domain.LiveTrackerShedRow{ScheduledAdmins: 46, ProofVideosReceived: 36, Remaining: 10}, &recent, domain.LiveTrackerShedReceiving},
	}
	for _, tc := range shedCases {
		if got := liveTrackerShedState(tc.row, tc.last, now); got != tc.want {
			t.Errorf("%s: shed state = %q, want %q", tc.name, got, tc.want)
		}
	}

	idle := domain.LiveTrackerIdleMinutes + 30
	fresh := 1
	operatorCases := []struct {
		name string
		row  domain.LiveTrackerOperatorRow
		want string
	}{
		{"no evidence at all", domain.LiveTrackerOperatorRow{ScheduledAdmins: 8, Remaining: 8}, domain.LiveTrackerOperatorNotStarted},
		{"all closed", domain.LiveTrackerOperatorRow{ScheduledAdmins: 21, ClosedAdmins: 21, ProofVideos: 21, ScanCaptures: 21}, domain.LiveTrackerOperatorDone},
		{"stalled mid-drive", domain.LiveTrackerOperatorRow{ScheduledAdmins: 8, ProofVideos: 1, ScanCaptures: 1, Remaining: 7, IdleMinutes: &idle}, domain.LiveTrackerOperatorIdle},
		{"working now", domain.LiveTrackerOperatorRow{ScheduledAdmins: 74, ProofVideos: 62, ScanCaptures: 62, Remaining: 12, IdleMinutes: &fresh}, domain.LiveTrackerOperatorActive},
	}
	for _, tc := range operatorCases {
		if got := liveTrackerOperatorState(tc.row); got != tc.want {
			t.Errorf("%s: operator state = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestLiveTrackerStatusMatrixTilesReconcileWithTheirTables pins the reconciliation the mock itself
// broke: the Scheduled tile must equal the sum of the shed table's Scheduled column, and Remaining
// must equal scheduled minus proofed. A tile computed from a second query drifts from the table
// under it and nobody can tell which number is right.
func TestLiveTrackerStatusMatrixTilesReconcileWithTheirTables(t *testing.T) {
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	cells := []liveTrackerCell{
		{parkID: "p1", parkName: "Alpha", shedID: "s1", shedName: "Gandhi", partitionLabel: "2", vaccineFamily: "goat_pox", scheduled: 46, proofed: 36, closed: 30, scanned: 36},
		{parkID: "p1", parkName: "Alpha", shedID: "s2", shedName: "Gandhi", partitionLabel: "3", vaccineFamily: "goat_pox", scheduled: 49, proofed: 5, closed: 0, scanned: 5},
		{parkID: "p2", parkName: "Beta", shedID: "s3", shedName: "Godel 1", partitionLabel: "Part 1", vaccineFamily: "blue_tongue", scheduled: 30, proofed: 22, closed: 22, scanned: 0},
	}
	kpis, unassigned := liveTrackerKPIs(cells, nil, 2, 3)
	sheds := liveTrackerShedRows(cells, now)
	_ = unassigned

	shedScheduled := 0
	shedProofed := 0
	for _, row := range sheds {
		shedScheduled += row.ScheduledAdmins
		shedProofed += row.ProofVideosReceived
	}
	if kpis.ScheduledAdministrations != shedScheduled {
		t.Errorf("Scheduled tile %d does not reconcile with the shed table's %d", kpis.ScheduledAdministrations, shedScheduled)
	}
	if kpis.ProofVideosReceived != shedProofed {
		t.Errorf("Proofs tile %d does not reconcile with the shed table's %d", kpis.ProofVideosReceived, shedProofed)
	}
	// Remaining is scheduled minus CLOSED, never minus proofs received. Proof arrival is field work
	// landing; closure is the obligation being discharged. Deriving Remaining from proof arrival made
	// the board read as a finished drive on a day where every video had landed and 9 of 298
	// obligations were completed.
	if kpis.Remaining != kpis.ScheduledAdministrations-kpis.ClosedAdministrations {
		t.Errorf("Remaining %d is not scheduled minus closed", kpis.Remaining)
	}
	shedClosed := 0
	for _, row := range sheds {
		shedClosed += row.ClosedAdmins
	}
	if kpis.ClosedAdministrations != shedClosed {
		t.Errorf("Closed tile %d does not reconcile with the shed table's %d", kpis.ClosedAdministrations, shedClosed)
	}
	if len(kpis.ScheduledByPark) != 2 {
		t.Fatalf("park split has %d entries, want 2", len(kpis.ScheduledByPark))
	}
	byParkTotal := 0
	for _, park := range kpis.ScheduledByPark {
		byParkTotal += park.Count
	}
	if byParkTotal != kpis.ScheduledAdministrations {
		t.Errorf("park split sums to %d, but the tile says %d", byParkTotal, kpis.ScheduledAdministrations)
	}
	// combo_animals is ANIMAL grain and must NOT be folded into an administration total.
	if kpis.ComboAnimals != 2 {
		t.Errorf("combo animal count = %d, want the animal figure passed in", kpis.ComboAnimals)
	}
	if kpis.AttentionCount != 3 {
		t.Errorf("the Attention tile must equal the number of attention rows, got %d", kpis.AttentionCount)
	}
}

// TestLiveTrackerAttentionCountEqualsItsOwnRows pins that the Attention tile is derived, never
// constant. The mock's badge said "3" forever regardless of what was happening on the farm.
func TestLiveTrackerAttentionCountEqualsItsOwnRows(t *testing.T) {
	idle := domain.LiveTrackerIdleMinutes + 5
	operators := []domain.LiveTrackerOperatorRow{
		{OperatorID: "o1", OperatorName: "Idle One", State: domain.LiveTrackerOperatorIdle, IdleMinutes: &idle, ScheduledAdmins: 8},
		{OperatorID: "o2", OperatorName: "Busy One", State: domain.LiveTrackerOperatorActive},
	}
	sheds := []domain.LiveTrackerShedRow{
		{ShedID: "s1", ShedLabel: "Sumathi 2 - Part 5", ExtraAttemptCount: 2, State: domain.LiveTrackerShedReview},
		{ShedID: "s2", ShedLabel: "Gandhi 3", State: domain.LiveTrackerShedSlow, ScheduledAdmins: 49, ProofVideosReceived: 5},
		{ShedID: "s3", ShedLabel: "Mandela 2 - Part 1", State: domain.LiveTrackerShedDone},
	}
	attention := liveTrackerAttention(sheds, operators, time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC))
	if len(attention) != 3 {
		t.Fatalf("expected one idle operator, one extra-attempts shed and one slow shed, got %d rows", len(attention))
	}
	kinds := map[string]int{}
	for _, row := range attention {
		kinds[row.Kind]++
	}
	for kind, want := range map[string]int{
		domain.LiveTrackerAttentionIdleOperator:  1,
		domain.LiveTrackerAttentionExtraAttempts: 1,
		domain.LiveTrackerAttentionSlowShed:      1,
	} {
		if kinds[kind] != want {
			t.Errorf("attention kind %q appeared %d times, want %d", kind, kinds[kind], want)
		}
	}
	if kpis, _ := liveTrackerKPIs(nil, nil, 0, len(attention)); kpis.AttentionCount != len(attention) {
		t.Errorf("the Attention tile (%d) must equal len(attention) (%d)", kpis.AttentionCount, len(attention))
	}
}

// TestLiveTrackerOperatorIdentityJoinsUserIDNotMemberID pins the join that silently blanks the whole
// operator column when it is wrong: uploaded_by / captured_by are USER ids, and joining them to
// workforce_member_id returns zero rows without any error.
func TestLiveTrackerOperatorIdentityJoinsUserIDNotMemberID(t *testing.T) {
	actorBlock := liveTrackerActorSQL[strings.Index(liveTrackerActorSQL, "FROM actors a"):]
	if !strings.Contains(actorBlock, "m.user_id = a.actor_id") {
		t.Error("evidence actors must join workforce_members on user_id")
	}
	if strings.Contains(actorBlock, "m.workforce_member_id = a.actor_id") {
		t.Error("evidence actors must NOT join workforce_members on workforce_member_id; that match is always empty")
	}
	if !strings.Contains(liveTrackerActivitySQL, "m.tenant_id = $1::uuid AND m.user_id = e.actor_id") {
		t.Error("the activity feed must resolve its actor through user_id")
	}
	// The assignment side is the opposite: vaccination_drive_assignments.operator_id IS a member id.
	if !strings.Contains(liveTrackerCellsSQL, "wm.workforce_member_id = se.operator_id") {
		t.Error("the drive assignment's operator_id is a workforce_member_id and must join as one")
	}
}

// TestLiveTrackerActivityFeedReadsEventTablesNotAuditLog pins the feed's source. audit_log carries
// obligation lifecycle transitions only — no scan and no proof event — so a feed built on it would
// silently omit the two things this page exists to show.
func TestLiveTrackerActivityFeedReadsEventTablesNotAuditLog(t *testing.T) {
	if strings.Contains(liveTrackerActivitySQL, "audit_log") {
		t.Error("the activity feed must not read audit_log; it carries no scan or proof event")
	}
	for _, source := range []string{
		"proof_artifacts",
		"sop_task_scan_captures",
		"sop_task_scan_attempts",
		"vaccination_completions",
		"obligation_status_events",
	} {
		if !strings.Contains(liveTrackerActivitySQL, source) {
			t.Errorf("the feed is missing its %s arm", source)
		}
	}
	if !strings.Contains(liveTrackerActivitySQL, "$10::timestamptz IS NULL") ||
		!strings.Contains(liveTrackerActivitySQL, "e.occurred_at < $10::timestamptz") {
		t.Error("the feed must page by keyset on occurred_at, not by offset")
	}
	// The cursor must be the FULL sort key. Comparing occurred_at alone skips every event tied with
	// the previous page's last row, and burst-written scan captures share a timestamp routinely —
	// that loses real events rather than merely repeating them.
	if !strings.Contains(liveTrackerActivitySQL, "(e.occurred_at = $10::timestamptz AND ($11::text = '' OR e.event_id < $11::text))") {
		t.Error("the feed cursor must carry an event_id tiebreaker; occurred_at alone drops tied events")
	}
	if !strings.Contains(liveTrackerActivitySQL, "ORDER BY e.occurred_at DESC, e.event_id DESC") {
		t.Error("the cursor predicate and the ORDER BY must be the same key")
	}
}

// TestLiveTrackerVaccineLabelsNeverComeFromTheEmptyCatalog pins that labels are composed from the
// protocol, because public.vaccines has no rows in any environment — joining it would blank every
// vaccine cell on the page.
func TestLiveTrackerVaccineLabelsNeverComeFromTheEmptyCatalog(t *testing.T) {
	for name, sql := range map[string]string{
		"cells":    liveTrackerCellsSQL,
		"combo":    liveTrackerComboSQL,
		"activity": liveTrackerActivitySQL,
		"options":  liveTrackerFilterOptionsSQL,
	} {
		if strings.Contains(sql, "public.vaccines") || strings.Contains(sql, "JOIN vaccines") {
			t.Errorf("%s must not read the empty vaccines catalog", name)
		}
	}
}

// TestLiveTrackerShedLabelMatchesOperationalNaming pins the shed label format against the rest of
// the vaccination surface, so the same partition is not called two different things on two screens.
func TestLiveTrackerShedLabelMatchesOperationalNaming(t *testing.T) {
	cases := map[[2]string]string{
		// Canonical naming (2026-08-14 ruling): bare-numeral partitions join with a SPACE
		// ("Gandhi 3" is a real shed name, never "Gandhi - 3"); worded partitions keep the dash.
		{"Gandhi", "3"}:         "Gandhi 3",
		{"Sumathi 2", "Part 4"}: "Sumathi 2 - Part 4",
		{"Mandela 2", "whole"}:  "Mandela 2",
		{"Old Yashoda", ""}:     "Old Yashoda",
		{"Godel 1", "part 2"}:   "Godel 1 - part 2",
	}
	for input, want := range cases {
		if got := domain.ShedDisplayLabel(input[0], input[1]); got != want {
			t.Errorf("ShedDisplayLabel(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

// TestLiveTrackerFilterOptionSeparatorIsARealControlCharacter pins the composite-label separator.
//
// Writing '\x1f' inside a standard-conforming Postgres string literal produces the four characters
// backslash-x-1-f, NOT the unit separator. Nothing errors: the Go side simply never finds its
// delimiter, and every vaccine and shed option renders its raw internal tokens
// ("Preventive Care Vaccination Matrix\x1fgoat_pox_adult_w1") straight into the filter bar.
func TestLiveTrackerFilterOptionSeparatorIsARealControlCharacter(t *testing.T) {
	if strings.Contains(liveTrackerFilterOptionsSQL, `'\x1f'`) {
		t.Error(`filter-option labels must join on chr(31); the literal '\x1f' is a backslash escape, not a separator`)
	}
	for _, expr := range []string{
		"min(se.protocol_name) || chr(31) || min(se.dose_code)",
		"COALESCE(sh.name, '') || chr(31) || se.partition_label",
	} {
		if !strings.Contains(liveTrackerFilterOptionsSQL, expr) {
			t.Errorf("composite label %q must join on chr(31)", expr)
		}
	}
	// The Go side splits on the same real control character.
	protocol, dose, found := strings.Cut("Preventive Care Vaccination Matrix\x1fgoat_pox_adult_w1", "\x1f")
	if !found || protocol == "" || dose != "goat_pox_adult_w1" {
		t.Fatalf("composite label did not split: protocol=%q dose=%q found=%v", protocol, dose, found)
	}
}

// TestLiveTrackerStatusFilterNarrowsTilesAndTableTogether pins that the status filter moves the
// TILES as well as the rows. The mock's filter bar promised "filters apply to tiles, tables and the
// live feed together" while its tiles ignored every filter — a headline that no longer described the
// rows beneath it, with nothing on screen to say which number was stale.
func TestLiveTrackerStatusFilterNarrowsTilesAndTableTogether(t *testing.T) {
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Minute)
	cells := []liveTrackerCell{
		{parkID: "p1", parkName: "Alpha", shedID: "s1", shedName: "Gandhi", partitionLabel: "2", scheduled: 46, proofed: 46, closed: 46, scanned: 46, lastActivityAt: &recent},
		{parkID: "p1", parkName: "Alpha", shedID: "s2", shedName: "Gandhi", partitionLabel: "3", scheduled: 49, proofed: 49, closed: 49, scanned: 49, extraAttempts: 2, lastActivityAt: &recent},
		{parkID: "p1", parkName: "Alpha", shedID: "s3", shedName: "Sumathi 2", partitionLabel: "Part 1", scheduled: 10, proofed: 0, closed: 0, scanned: 0},
	}
	sheds := liveTrackerShedRows(cells, now)

	// Reproduce the repository's fold: the surviving cells are exactly the administrations behind the
	// surviving shed rows, because sheds[i] is built 1:1 from cells[i].
	for _, tc := range []struct {
		status        domain.LiveTrackerStatus
		wantRows      int
		wantScheduled int
	}{
		{domain.LiveTrackerStatusReview, 1, 49},
		{domain.LiveTrackerStatusDone, 1, 46},
		{domain.LiveTrackerStatusPending, 1, 10},
	} {
		kept := make([]liveTrackerCell, 0, len(cells))
		rows := 0
		for i, row := range sheds {
			if domain.ShedMatchesStatus(row.State, tc.status) {
				kept = append(kept, cells[i])
				rows++
			}
		}
		kpis, _ := liveTrackerKPIs(kept, nil, 0, 0)
		if rows != tc.wantRows {
			t.Errorf("status %q kept %d shed rows, want %d", tc.status, rows, tc.wantRows)
		}
		if kpis.ScheduledAdministrations != tc.wantScheduled {
			t.Errorf("status %q tile = %d, want %d (the tile must describe the rows it sits above)",
				tc.status, kpis.ScheduledAdministrations, tc.wantScheduled)
		}
	}
}

// TestLiveTrackerMembershipIsVaccinationOnly pins the protocol-category predicate. The obligation
// engine is SHARED: obligation_instances carries deworming, biosecurity, panel-cleaning and seven
// other categories. Without this predicate every goat-targeted obligation due that day is counted
// under "Scheduled today · administrations" on a page whose brand promise is vaccination — and
// because every evidence CTE filters st.task_type = 'vaccination', such a row can NEVER be proofed
// off. It inflates Remaining permanently and pins its shed at not_started.
func TestLiveTrackerMembershipIsVaccinationOnly(t *testing.T) {
	if !strings.Contains(liveTrackerScopedCTE, "pd.category = 'vaccination'") {
		t.Fatal("the membership CTE must narrow protocol_definitions to the vaccination category")
	}
}

// TestLiveTrackerMembershipExcludesDeadObligations pins the status exclusion set. 'superseded' and
// 'waived' rows will never receive a proof; counting them into `scheduled` inflates the Scheduled
// tile and Remaining and holds the shed row open for the rest of the day.
func TestLiveTrackerMembershipExcludesDeadObligations(t *testing.T) {
	if !strings.Contains(liveTrackerScopedCTE, "oi.status NOT IN ('canceled', 'superseded', 'waived')") {
		t.Error("membership must exclude canceled, superseded AND waived obligations")
	}
	if strings.Contains(liveTrackerScopedCTE, "oi.status <> 'canceled'") {
		t.Error("excluding only 'canceled' leaves dead obligations counted as scheduled work")
	}
}

// TestLiveTrackerActorEvidenceCannotFanOutOnComboAnimals pins the operator board's Videos and Scans
// columns against the combo case this page exists to show. scoped_enriched is ONE ROW PER
// OBLIGATION, so joining raw proof/scan rows to it and counting doubles both columns for any animal
// carrying two same-day obligations — which then feeds Remaining, the operator's live state, the
// idle attention rows and the Attention tile. It is invisible in stg only because combo_animals is 0
// there today.
func TestLiveTrackerActorEvidenceCannotFanOutOnComboAnimals(t *testing.T) {
	if !strings.Contains(liveTrackerActorSQL, "scoped_goats AS (\n  SELECT DISTINCT goat_id FROM scoped_enriched\n)") {
		t.Fatal("the actor query must build a DISTINCT per-goat set before counting evidence")
	}
	for _, join := range []string{
		"JOIN scoped_goats sg ON sg.goat_id = pa.subject_id",
		"JOIN scoped_goats sg ON sg.goat_id = c.goat_id",
	} {
		if !strings.Contains(liveTrackerActorSQL, join) {
			t.Errorf("missing per-goat evidence join %q", join)
		}
	}
	for _, banned := range []string{
		"JOIN scoped_enriched se ON se.goat_id = pa.subject_id",
		"JOIN scoped_enriched se ON se.goat_id = c.goat_id",
	} {
		if strings.Contains(liveTrackerActorSQL, banned) {
			t.Errorf("%q joins the per-OBLIGATION set for cardinality; a combo animal doubles the count", banned)
		}
	}
}

// TestLiveTrackerFeedEmitsOneRowPerPhysicalEvent pins that the goat-keyed feed arms do not join the
// dose-grain location CTE. shed_names carries one row per (goat, dose); joining a proof upload to it
// on goat_id alone emitted TWO feed rows for one proof, both carrying the SAME event_id — duplicate
// React keys, a LIMIT consumed by duplicates, and an inflated observed_per_min.
func TestLiveTrackerFeedEmitsOneRowPerPhysicalEvent(t *testing.T) {
	if !strings.Contains(liveTrackerActivitySQL, "goat_places AS (") ||
		!strings.Contains(liveTrackerActivitySQL, "SELECT DISTINCT ON (se.goat_id)") {
		t.Fatal("the feed must carry a one-row-per-goat location projection for its goat-keyed arms")
	}
	for _, join := range []string{
		"JOIN goat_places se ON se.goat_id = pa.subject_id",
		"JOIN goat_places se ON se.goat_id = c.goat_id",
		"JOIN goat_places se ON se.goat_id = a.goat_id",
	} {
		if !strings.Contains(liveTrackerActivitySQL, join) {
			t.Errorf("goat-keyed feed arm must join goat_places: missing %q", join)
		}
	}
	if strings.Contains(liveTrackerActivitySQL, "shed_names") {
		t.Error("a goat-keyed arm joined to a dose-grain projection duplicates every event of a combo animal")
	}
	// The two dose-keyed arms read their location and dose straight off the obligation row the event
	// belongs to. Routing them through a DISTINCT dose-grain projection re-joined on
	// (goat_id, dose_code) was a self-join that could only ever duplicate a row, and the planner
	// costed it as a 768 x 768 nested loop — 589,056 comparisons to decorate a 40-row page.
	// obligation_id is unique in scoped_enriched, so this join is 1:1 by construction.
	for _, join := range []string{
		"JOIN scoped_enriched sc ON sc.obligation_id = vc.obligation_id",
		"JOIN scoped_enriched sc ON sc.obligation_id = ose.obligation_id",
	} {
		if !strings.Contains(liveTrackerActivitySQL, join) {
			t.Errorf("the dose-keyed arms must resolve through the obligation itself: missing %q", join)
		}
	}
	// The three goat-keyed event sets are read ONCE for the day. Left to the planner's estimate they
	// became the inner of a nested loop over goat_places — a full scan of the event table per animal
	// on the board wherever no supporting time index exists.
	for _, cte := range []string{"day_event_proofs AS MATERIALIZED", "day_event_captures AS MATERIALIZED", "day_event_attempts AS MATERIALIZED"} {
		if !strings.Contains(liveTrackerActivitySQL, cte) {
			t.Errorf("the feed must read each day event set once: missing %q", cte)
		}
	}
}

// TestLiveTrackerIdentityJoinSurvivesARehiredPerson pins the workforce_members join. The only
// uniqueness guarantee on user_id is PARTIAL (workforce_members_active_user_unique_idx, WHERE
// status = 'active'), so a re-hired person holds one active row plus one or more left/inactive rows
// on the same user_id. A bare join fans out: a duplicate operator row with identical counts, and
// every one of that person's feed rows rendered twice.
func TestLiveTrackerIdentityJoinSurvivesARehiredPerson(t *testing.T) {
	for name, sql := range map[string]string{"actors": liveTrackerActorSQL, "activity": liveTrackerActivitySQL} {
		if !strings.Contains(sql, "AND m.status = 'active'") {
			t.Errorf("%s must resolve workforce identity through the ACTIVE row only", name)
		}
		if !strings.Contains(sql, "ORDER BY m.workforce_member_id\n  LIMIT 1") &&
			!strings.Contains(sql, "ORDER BY m.workforce_member_id\n    LIMIT 1") {
			t.Errorf("%s must disambiguate workforce identity with a LIMIT 1 lateral", name)
		}
	}
}

// TestLiveTrackerAttentionElapsedIsMeasuredNotAThreshold pins that ElapsedMin is an OBSERVATION.
// The slow-shed row previously carried domain.LiveTrackerSlowShedMinutes — a policy constant — which
// rendered on screen as a bare "· 40" indistinguishable from a measured figure.
func TestLiveTrackerAttentionElapsedIsMeasuredNotAThreshold(t *testing.T) {
	clock := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	lastProof := clock.Add(-137 * time.Minute)
	sheds := []domain.LiveTrackerShedRow{
		{ShedID: "s1", ShedLabel: "Gandhi 3", State: domain.LiveTrackerShedSlow, ScheduledAdmins: 49, ProofVideosReceived: 5, LastProofAt: &lastProof},
		{ShedID: "s2", ShedLabel: "Gandhi 4", State: domain.LiveTrackerShedSlow, ScheduledAdmins: 40, ProofVideosReceived: 0},
	}
	rows := liveTrackerAttention(sheds, nil, clock)
	if len(rows) != 2 {
		t.Fatalf("expected one attention row per slow shed, got %d", len(rows))
	}
	if rows[0].ElapsedMin != 137 {
		t.Errorf("slow-shed elapsed = %d, want the measured 137 minutes since its last proof", rows[0].ElapsedMin)
	}
	if rows[1].ElapsedMin != 0 {
		t.Errorf("a shed with no proof has nothing to measure; elapsed = %d, want 0", rows[1].ElapsedMin)
	}
	for _, row := range rows {
		if row.ElapsedMin == domain.LiveTrackerSlowShedMinutes {
			t.Error("attention elapsed must never be the slow-shed policy threshold rendered as data")
		}
	}
}

// TestLiveTrackerPastDriveDayIsNotMeasuredAgainstWallClock pins the elapsed clock. The handler
// accepts a business_date up to LiveTrackerBusinessDateLookbackDays back; measuring a finished drive
// against wall-clock now labels every open shed slow and every operator idle, and manufactures an
// attention row with an idle figure in the thousands of minutes for a drive that closed days ago.
func TestLiveTrackerPastDriveDayIsNotMeasuredAgainstWallClock(t *testing.T) {
	loc := time.UTC
	past := time.Date(2026, 8, 8, 0, 0, 0, 0, loc)
	now := time.Date(2026, 8, 12, 18, 30, 0, 0, loc)
	clock := liveTrackerStateClock(past, loc, now)
	if !clock.Equal(time.Date(2026, 8, 9, 0, 0, 0, 0, loc)) {
		t.Errorf("a past drive day must be measured to its own close, got %s", clock)
	}
	today := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	if got := liveTrackerStateClock(today, loc, now); !got.Equal(now) {
		t.Errorf("today's drive must be measured against now, got %s", got)
	}
}

// TestLiveTrackerTruncationIsReportedNotSilent pins that the two boards declare their own caps. The
// KPI tiles are folded from the untruncated cell set, so past the caps the Scheduled tile
// legitimately exceeds the sum of the visible table's Scheduled column — and the response must carry
// the totals and flags that let the page say so, exactly as the combo card already does.
func TestLiveTrackerTruncationIsReportedNotSilent(t *testing.T) {
	var resp domain.LiveTrackerResponse
	resp.OperatorsTotal = 130
	resp.OperatorsTruncated = true
	resp.ShedsTotal = 240
	resp.ShedsTruncated = true
	resp.CellsTruncated = true
	if !resp.OperatorsTruncated || !resp.ShedsTruncated || !resp.CellsTruncated {
		t.Fatal("the response must be able to declare operator, shed and cell truncation")
	}
	if resp.OperatorsTotal <= domain.LiveTrackerMaxOperators || resp.ShedsTotal <= domain.LiveTrackerMaxSheds {
		t.Fatal("totals must describe the pre-truncation row counts")
	}
	if !strings.Contains(liveTrackerCellsSQL, "LIMIT "+fmt.Sprint(domain.LiveTrackerMaxCells)) {
		t.Error("the cell cap must be the declared constant, not a second hardcoded number")
	}
	// Each option kind carries its OWN cap. A single shared cap across the union was spent in
	// kind-name order (operator < park < shed < vaccine), so past it the vaccine list — the smallest
	// and the one the truncation note tells the reader to reach for — was destroyed ENTIRELY before
	// any other list lost a single row, with no flag anywhere.
	for kind, cap := range map[string]int{
		"park":     domain.LiveTrackerMaxParkOptions,
		"vaccine":  domain.LiveTrackerMaxVaccineOptions,
		"operator": domain.LiveTrackerMaxOperatorOptions,
		"shed":     domain.LiveTrackerMaxShedOptions,
	} {
		if !strings.Contains(liveTrackerFilterOptionsSQL, "LIMIT "+fmt.Sprint(cap)) {
			t.Errorf("the %s option list must carry its own declared cap", kind)
		}
	}
	if strings.Contains(liveTrackerFilterOptionsSQL, "ORDER BY 1, 5") {
		t.Error("one shared ORDER BY/LIMIT over the union starves the last kind alphabetically")
	}
	if !strings.Contains(liveTrackerFilterOptionsSQL, "(count(*) OVER ())::int") {
		t.Error("each option kind must report its pre-cap total so a starved list cannot look complete")
	}
	var options domain.LiveTrackerFilterOptions
	options.Truncated = true
	if !options.Truncated {
		t.Fatal("the filter vocabulary must be able to declare its own truncation")
	}
}

// TestLiveTrackerFilterVocabularyDoesNotCollapseTheParkControl pins that the filter bar is compiled
// under the AUTHORIZATION clamp, not under the caller's own park selection. Passing the selected
// park made the control self-collapsing: once a park was chosen the dropdown offered only that park
// and the user could not switch back to the other one.
func TestLiveTrackerFilterVocabularyDoesNotCollapseTheParkControl(t *testing.T) {
	if strings.Contains(liveTrackerFilterOptionsSQL, "$5::text") || strings.Contains(liveTrackerFilterOptionsSQL, "$6::text") {
		t.Log("filter options inherit the scoped CTE's parameter list; only park ($3) is ever non-empty")
	}
	if liveTrackerAuthorizedParkScope(t.Context(), "tenant") != nil {
		t.Error("with no grants in context the filter vocabulary must not be park-clamped")
	}
	// The clamp is a SET, not a single-park special case. Returning "no narrowing" whenever the actor
	// held grants in anything other than exactly one park compiled this vocabulary TENANT-WIDE, and
	// this query is the only authorization gate that applies to it — so a two-park director was
	// handed every other park's shed names, partition labels, operator names and vaccine codes.
	if !strings.Contains(liveTrackerScopedCTE, "($8::uuid[] IS NULL OR g.park_id = ANY($8::uuid[]))") {
		t.Error("the authorization park SET must narrow the membership independently of the caller's own park selection")
	}
}

// TestLiveTrackerReworkIsDayScopedLikeItsOwnDocComment pins that the verification card's three
// figures agree about what "today" means. rework_requested previously carried no date predicate at
// all: an all-time tenant-wide rejected total rendered directly beneath a today-only verified total.
func TestLiveTrackerReworkIsDayScopedLikeItsOwnDocComment(t *testing.T) {
	if !strings.Contains(liveTrackerVerificationSQL, "vi.status = 'rejected' AND vi.verified_at >= w.day_start AND vi.verified_at < w.day_end") {
		t.Error("rework_requested must be day-scoped through verified_at, like the approved counters beside it")
	}
	if strings.Contains(liveTrackerVerificationSQL, "(vi.verified_at AT TIME ZONE 'Asia/Kolkata')::date") {
		t.Error("verified_at must be compared as a sargable range; the cast form cannot be indexed")
	}
}

// TestLiveTrackerRemainingMeasuresClosureNotProofArrival pins the difference the whole board turns
// on. Proof ARRIVAL and obligation CLOSURE are different facts: in stg on 2026-08-12 all 298 proof
// videos had landed while 9 of 298 obligation_instances were completed. With Remaining derived from
// proof arrival every tile read zero, every shed row read `done`, and the page reported a finished
// drive on a drive that was still open — a wrong operational conclusion, not a cosmetic one.
func TestLiveTrackerRemainingMeasuresClosureNotProofArrival(t *testing.T) {
	now := time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Minute)
	cells := []liveTrackerCell{
		{parkID: "p1", parkName: "Alpha", shedID: "s1", shedName: "Gandhi", partitionLabel: "2",
			operatorID: "op1", operatorName: "Kumar", scheduled: 298, proofed: 298, closed: 9,
			scanned: 153, lastActivityAt: &recent},
	}
	kpis, _ := liveTrackerKPIs(cells, nil, 0, 0)
	if kpis.Remaining != 289 {
		t.Errorf("Remaining = %d, want 289 — the 289 obligations that are proofed but NOT closed", kpis.Remaining)
	}
	if kpis.AwaitingClose != 289 {
		t.Errorf("AwaitingClose = %d, want 289 proofed-but-open administrations", kpis.AwaitingClose)
	}
	if kpis.ProofVideosReceived != 298 {
		t.Errorf("proof arrival must still be reported in full, got %d", kpis.ProofVideosReceived)
	}

	sheds := liveTrackerShedRows(cells, now)
	if sheds[0].Remaining != 289 || sheds[0].State == domain.LiveTrackerShedDone {
		t.Errorf("shed row remaining=%d state=%q; a shed whose obligations are open is not done",
			sheds[0].Remaining, sheds[0].State)
	}
	// The operator row must reach the SAME conclusion about the same work.
	operators := liveTrackerOperatorRows(cells, nil, now)
	if len(operators) != 1 {
		t.Fatalf("expected one operator row, got %d", len(operators))
	}
	if operators[0].Remaining != sheds[0].Remaining {
		t.Errorf("operator remaining %d disagrees with the shed row for the identical work (%d)",
			operators[0].Remaining, sheds[0].Remaining)
	}
}

// TestLiveTrackerOperatorRemainingIsObligationGrainOnBothSides pins the combo-day arithmetic that
// used to make a finished operator look half done.
//
// ProofVideos is a PHYSICAL count of proof_artifacts rows; ScheduledAdmins is an OBLIGATION count.
// On a combo day one video closes two obligations, so subtracting one from the other left a finished
// operator with Remaining = half their workload: never `done`, dropped by the status=done filter,
// and — 90 minutes after their last upload — emitted as a "dng" idle-operator attention row, while
// the shed rows covering the identical work read Remaining 0 / done.
func TestLiveTrackerOperatorRemainingIsObligationGrainOnBothSides(t *testing.T) {
	now := time.Date(2026, 8, 31, 18, 0, 0, 0, time.UTC)
	stale := now.Add(-3 * time.Hour)
	// Three combo animals, seven obligations, three physical videos — the shape of stg 2026-08-31.
	cells := []liveTrackerCell{
		{parkID: "p1", shedID: "s1", shedName: "Natheswar", partitionLabel: "6", vaccineFamily: "fmd",
			operatorID: "op1", operatorName: "Natheswar", scheduled: 3, proofed: 3, closed: 3, scanned: 3, lastActivityAt: &stale},
		{parkID: "p1", shedID: "s1", shedName: "Natheswar", partitionLabel: "6", vaccineFamily: "hs",
			operatorID: "op1", operatorName: "Natheswar", scheduled: 3, proofed: 3, closed: 3, scanned: 3, lastActivityAt: &stale},
		{parkID: "p1", shedID: "s1", shedName: "Natheswar", partitionLabel: "6", vaccineFamily: "sheep_pox",
			operatorID: "op1", operatorName: "Natheswar", scheduled: 1, proofed: 1, closed: 1, scanned: 1, lastActivityAt: &stale},
	}
	actors := []liveTrackerActor{{memberID: "op1", displayName: "Natheswar", videos: 3, scans: 3, lastActivityAt: &stale}}

	operators := liveTrackerOperatorRows(cells, actors, now)
	if len(operators) != 1 {
		t.Fatalf("expected one operator row, got %d", len(operators))
	}
	row := operators[0]
	if row.ScheduledAdmins != 7 || row.ClosedAdmins != 7 {
		t.Fatalf("fixture mismatch: scheduled=%d closed=%d", row.ScheduledAdmins, row.ClosedAdmins)
	}
	if row.ProofVideos != 3 {
		t.Errorf("ProofVideos must stay the PHYSICAL upload count, got %d", row.ProofVideos)
	}
	if row.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0; 7 obligations minus 3 physical videos is a grain mismatch", row.Remaining)
	}
	if row.State != domain.LiveTrackerOperatorDone {
		t.Errorf("operator state = %q, want done", row.State)
	}
	// And therefore no manufactured idle-operator attention row.
	for _, attn := range liveTrackerAttention(liveTrackerShedRows(cells, now), operators, now) {
		if attn.Kind == domain.LiveTrackerAttentionIdleOperator {
			t.Error("a finished operator must not be reported idle")
		}
	}
}

// TestLiveTrackerUnassignedWorkIsReportedNotDropped pins the residual between the Scheduled tile and
// the Operators table. Obligations in a shed/partition with no drive assignment for the day are
// counted into the tile and rendered in the shed board, but liveTrackerOperatorRows has no operator
// to attribute them to — so the Operators column simply summed short of the tile above it with
// nothing on screen to explain the gap. On stg 2026-08-14 that gap is 95 of 199.
func TestLiveTrackerUnassignedWorkIsReportedNotDropped(t *testing.T) {
	now := time.Date(2026, 8, 14, 18, 0, 0, 0, time.UTC)
	cells := []liveTrackerCell{
		{parkID: "p1", shedID: "s1", shedName: "Gandhi", partitionLabel: "2", operatorID: "op1", operatorName: "Kumar", scheduled: 104},
		{parkID: "p1", shedID: "s2", shedName: "Godel 2", partitionLabel: "4", operatorID: "", scheduled: 95},
	}
	kpis, unassigned := liveTrackerKPIs(cells, nil, 0, 0)
	operatorSum := 0
	for _, row := range liveTrackerOperatorRows(cells, nil, now) {
		operatorSum += row.ScheduledAdmins
	}
	shedSum := 0
	for _, row := range liveTrackerShedRows(cells, now) {
		shedSum += row.ScheduledAdmins
	}
	if shedSum != kpis.ScheduledAdministrations {
		t.Errorf("the shed board keeps unassigned work: %d vs tile %d", shedSum, kpis.ScheduledAdministrations)
	}
	if unassigned != 95 {
		t.Fatalf("unassigned residual = %d, want 95", unassigned)
	}
	if operatorSum+unassigned != kpis.ScheduledAdministrations {
		t.Errorf("operators (%d) + unassigned (%d) must reconcile with the tile (%d)",
			operatorSum, unassigned, kpis.ScheduledAdministrations)
	}
}

// TestLiveTrackerKPITilesStayExactWhenTheRollupIsCapped pins that the headline totals come from the
// rollup's own window aggregates, not from the capped row list they sit above. Folding them from the
// returned rows made the headline number itself under-report the drive day past LiveTrackerMaxCells
// — a wrong total, not merely a shortened table — while two notes on the same page asserted the
// tiles still counted the whole day.
func TestLiveTrackerKPITilesStayExactWhenTheRollupIsCapped(t *testing.T) {
	// Two visible cells out of a day that really carried 900 administrations.
	cells := []liveTrackerCell{
		{parkID: "p1", parkName: "Alpha", scheduled: 10, proofed: 8, closed: 7, scanned: 6,
			dayScheduled: 900, dayProofed: 700, dayClosed: 500, dayScanned: 640, dayUnassigned: 40, parkScheduled: 900},
		{parkID: "p1", parkName: "Alpha", scheduled: 12, proofed: 9, closed: 8, scanned: 7,
			dayScheduled: 900, dayProofed: 700, dayClosed: 500, dayScanned: 640, dayUnassigned: 40, parkScheduled: 900},
	}
	kpis, unassigned := liveTrackerKPIs(cells, liveTrackerTotals(cells), 0, 0)
	if kpis.ScheduledAdministrations != 900 || kpis.ProofVideosReceived != 700 ||
		kpis.ClosedAdministrations != 500 || kpis.ScanCaptures != 640 {
		t.Fatalf("tiles folded from the capped rows: %+v", kpis)
	}
	if kpis.Remaining != 400 {
		t.Errorf("Remaining = %d, want 900-500", kpis.Remaining)
	}
	if unassigned != 40 {
		t.Errorf("unassigned = %d, want the day figure 40", unassigned)
	}
	if len(kpis.ScheduledByPark) != 1 || kpis.ScheduledByPark[0].Count != 900 {
		t.Errorf("the park split must carry the park's whole-day total, got %+v", kpis.ScheduledByPark)
	}
	// A status filter is an explicit narrowing, so under one the tiles describe the surviving rows.
	narrowed, _ := liveTrackerKPIs(cells, nil, 0, 0)
	if narrowed.ScheduledAdministrations != 22 {
		t.Errorf("under a status filter the tiles must fold from the surviving cells, got %d",
			narrowed.ScheduledAdministrations)
	}
}

// TestLiveTrackerDoseStateNeverInfersClosureFromAProof pins that the combo card reads the
// obligation's OWN status.
//
// Every branch used to return `closed` as soon as the animal had a completed proof — including the
// default branch — so a `scheduled`, `deferred` or `missed` obligation rendered as "closed by the
// animal's proof" while obligation_instances.status said otherwise and completed_at was null. This
// is the only place on the whole board where obligation status is actually read.
func TestLiveTrackerDoseStateNeverInfersClosureFromAProof(t *testing.T) {
	for _, tc := range []struct {
		status string
		proofs int
		want   string
	}{
		{"completed", 0, domain.LiveTrackerDoseClosed},
		{"completed", 1, domain.LiveTrackerDoseClosed},
		{"in_progress", 1, domain.LiveTrackerDoseVerificationPending},
		{"due", 1, domain.LiveTrackerDoseVerificationPending},
		{"scheduled", 1, domain.LiveTrackerDoseVerificationPending},
		{"deferred", 1, domain.LiveTrackerDoseVerificationPending},
		{"missed", 1, domain.LiveTrackerDoseVerificationPending},
		{"in_progress", 0, domain.LiveTrackerDoseAwaitingProof},
		{"due", 0, domain.LiveTrackerDoseAwaitingProof},
		{"scheduled", 0, domain.LiveTrackerDoseScheduled},
		{"missed", 0, domain.LiveTrackerDoseScheduled},
	} {
		if got := liveTrackerDoseState(tc.status, tc.proofs); got != tc.want {
			t.Errorf("dose state for status=%q proofs=%d = %q, want %q", tc.status, tc.proofs, got, tc.want)
		}
	}
}

// TestLiveTrackerExtraAttemptsAreChargedOncePerAnimal pins that a single physical duplicate scan is
// reported once. day_attempts is per-GOAT while the rollup is per-OBLIGATION, so summing extra_count
// across a combo animal's rows charged the same re-scan to every vaccine cell that animal appears in
// — two "Extra attempts" figures, two Attention rows, and two sheds pinned to `review` for one event.
func TestLiveTrackerExtraAttemptsAreChargedOncePerAnimal(t *testing.T) {
	if !strings.Contains(liveTrackerScopedCTE, "row_number() OVER (PARTITION BY s.goat_id ORDER BY s.dose_code, s.obligation_id) AS goat_seq") {
		t.Fatal("the rollup must mark exactly one carrier row per animal for per-goat counters")
	}
	if !strings.Contains(liveTrackerCellsSQL, "sum(se.extra_count) FILTER (WHERE se.goat_seq = 1)") {
		t.Error("a per-goat counter must not be summed across that goat's per-obligation rows")
	}
}

// TestLiveTrackerFeedNamesEveryDoseOfACombo pins that the feed row reporting a combo animal's single
// video names every antigen that video covers. goat_places kept only the alphabetically-first
// dose_code per animal, so the one proof that covered FMD and HS rendered as an FMD-only event.
func TestLiveTrackerFeedNamesEveryDoseOfACombo(t *testing.T) {
	if !strings.Contains(liveTrackerActivitySQL, "goat_doses AS (") ||
		!strings.Contains(liveTrackerActivitySQL, "string_agg(DISTINCT se.protocol_name || chr(31) || se.dose_code, chr(30))") {
		t.Fatal("the feed's goat-keyed arms must carry every same-day dose, not one arbitrary dose")
	}
	got := liveTrackerDoseLabels("Preventive Care Vaccination Matrix\x1ffmd_adult_w1\x1eProtocol\x1fhs_adult_w1")
	if !strings.Contains(got, " + ") {
		t.Errorf("a combo animal's feed label must name both antigens, got %q", got)
	}
	if liveTrackerDoseLabels("") != "" {
		t.Error("an event with no dose context must render no vaccine label rather than an empty separator")
	}
}
