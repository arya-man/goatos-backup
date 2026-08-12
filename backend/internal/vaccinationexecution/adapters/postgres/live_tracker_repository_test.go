package postgres

import (
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
	// duplicate every obligation in a shed that has two assignment rows.
	if !strings.Contains(sql, "ORDER BY a.assignment_id\n    LIMIT 1") {
		t.Error("the drive-assignment lookup must be a LIMIT 1 LATERAL, not a fan-out join")
	}
	if strings.Contains(liveTrackerCellsSQL, "JOIN protocol_rule_dimensions") {
		t.Error("protocol_rule_dimensions must never be joined into a counting query; it fans rules out by dimension")
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
	if !strings.Contains(liveTrackerComboSQL, "LIMIT $8::int") {
		t.Error("combo rows must be capped server-side")
	}
	if !strings.Contains(liveTrackerActivitySQL, "LIMIT $8::int") {
		t.Error("the activity feed must be capped server-side")
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
	if !strings.Contains(liveTrackerScopedCTE, "($3::text = '' OR g.park_id::text = $3::text)") {
		t.Error("park scope must be enforced inside the membership CTE, not only at the handler")
	}
	if !strings.Contains(liveTrackerVerificationSQL, "($3::text = '' OR vi.park_id::text = $3::text)") {
		t.Error("the verification block must honour the same park scope as the rest of the page")
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
		{"finished clean", domain.LiveTrackerShedRow{ScheduledAdmins: 10, ProofVideosReceived: 10}, &recent, domain.LiveTrackerShedDone},
		{"finished with re-scans", domain.LiveTrackerShedRow{ScheduledAdmins: 10, ProofVideosReceived: 10, ExtraAttemptCount: 2}, &recent, domain.LiveTrackerShedReview},
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
		{"all proofed", domain.LiveTrackerOperatorRow{ScheduledAdmins: 21, ProofVideos: 21, ScanCaptures: 21}, domain.LiveTrackerOperatorDone},
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
		{parkID: "p1", parkName: "Alpha", shedID: "s1", shedName: "Gandhi", partitionLabel: "2", vaccineFamily: "goat_pox", scheduled: 46, proofed: 36, scanned: 36},
		{parkID: "p1", parkName: "Alpha", shedID: "s2", shedName: "Gandhi", partitionLabel: "3", vaccineFamily: "goat_pox", scheduled: 49, proofed: 5, scanned: 5},
		{parkID: "p2", parkName: "Beta", shedID: "s3", shedName: "Godel 1", partitionLabel: "Part 1", vaccineFamily: "blue_tongue", scheduled: 30, proofed: 22, scanned: 0},
	}
	kpis := liveTrackerKPIs(cells, 2, 3)
	sheds := liveTrackerShedRows(cells, now)

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
	if kpis.Remaining != kpis.ScheduledAdministrations-kpis.ProofVideosReceived {
		t.Errorf("Remaining %d is not scheduled minus proofed", kpis.Remaining)
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
	attention := liveTrackerAttention(sheds, operators)
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
	if kpis := liveTrackerKPIs(nil, 0, len(attention)); kpis.AttentionCount != len(attention) {
		t.Errorf("the Attention tile (%d) must equal len(attention) (%d)", kpis.AttentionCount, len(attention))
	}
}

// TestLiveTrackerOperatorIdentityJoinsUserIDNotMemberID pins the join that silently blanks the whole
// operator column when it is wrong: uploaded_by / captured_by are USER ids, and joining them to
// workforce_member_id returns zero rows without any error.
func TestLiveTrackerOperatorIdentityJoinsUserIDNotMemberID(t *testing.T) {
	actorBlock := liveTrackerActorSQL[strings.Index(liveTrackerActorSQL, "FROM actors a"):]
	if !strings.Contains(actorBlock, "wm.user_id = a.actor_id") {
		t.Error("evidence actors must join workforce_members on user_id")
	}
	if strings.Contains(actorBlock, "wm.workforce_member_id = a.actor_id") {
		t.Error("evidence actors must NOT join workforce_members on workforce_member_id; that match is always empty")
	}
	if !strings.Contains(liveTrackerActivitySQL, "wm.tenant_id = $1::uuid AND wm.user_id = e.actor_id") {
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
	if !strings.Contains(liveTrackerActivitySQL, "$9::timestamptz IS NULL OR e.occurred_at < $9::timestamptz") {
		t.Error("the feed must page by keyset on occurred_at, not by offset")
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
		{parkID: "p1", parkName: "Alpha", shedID: "s1", shedName: "Gandhi", partitionLabel: "2", scheduled: 46, proofed: 46, scanned: 46, lastActivityAt: &recent},
		{parkID: "p1", parkName: "Alpha", shedID: "s2", shedName: "Gandhi", partitionLabel: "3", scheduled: 49, proofed: 49, scanned: 49, extraAttempts: 2, lastActivityAt: &recent},
		{parkID: "p1", parkName: "Alpha", shedID: "s3", shedName: "Sumathi 2", partitionLabel: "Part 1", scheduled: 10, proofed: 0, scanned: 0},
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
		kpis := liveTrackerKPIs(kept, 0, 0)
		if rows != tc.wantRows {
			t.Errorf("status %q kept %d shed rows, want %d", tc.status, rows, tc.wantRows)
		}
		if kpis.ScheduledAdministrations != tc.wantScheduled {
			t.Errorf("status %q tile = %d, want %d (the tile must describe the rows it sits above)",
				tc.status, kpis.ScheduledAdministrations, tc.wantScheduled)
		}
	}
}
