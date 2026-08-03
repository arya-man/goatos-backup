package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationCommandBoardKPIOneToManyDosesFoldToOneAnimalAndStatusBucketsSumToTargets
// is the regression for the KPI tiles double-counting a multi-dose animal.
//
// It is the ONE-TO-MANY case (one animal, many obligations) and the STATUS-BUCKET
// partition case in a single fixture, because they are the same defect seen from two
// sides: the fan-out is what breaks the partition.
//
// targets has always been an ANIMAL count (COUNT DISTINCT target_id) while the four
// status tiles evaluated their priority chain on the OBLIGATION row. One goat with an
// accepted ET dose and a still-scheduled PPR dose therefore satisfied the verified
// predicate on one row and the scheduled predicate on another, and was counted by both
// tiles — four tiles summing to 2 sitting underneath a total of 1. Leadership could not
// reconcile the row, and "how many animals are still scheduled" was inflated by every
// animal that had already had a different dose accepted. Multi-dose is the normal case
// on a kid schedule, not a corner case.
//
// The chain is now folded per animal before it is applied, so the tiles partition
// targets: verified outranks scheduled (documented precedence in the KPI query).
func TestVaccinationCommandBoardKPIOneToManyDosesFoldToOneAnimalAndStatusBucketsSumToTargets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000c1"
	parkID := "70000000-0000-4000-8000-0000010000c1"
	shedID := "70000000-0000-4000-8000-0000020000c1"
	goatID := "70000000-0000-4000-8000-0000030000c1"
	protocolVersionID := "70000000-0000-4000-8000-0000060000c1"
	ruleAcceptedID := "70000000-0000-4000-8000-0000070000c1"
	ruleScheduledID := "70000000-0000-4000-8000-0000070000c2"
	oblAccepted := "70000000-0000-4000-8000-0000080000c1"
	oblScheduled := "70000000-0000-4000-8000-0000080000c2"

	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Park C1', 'park', NULL, 'active')`, parkID, tenantID)
	execProjectionSQL(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Shed C1', 'shed', $3, 'active')`, shedID, tenantID, parkID)
	custodianPartyID := "70000000-0000-4000-8000-00000a0000c1"
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian C1', 'active')`,
		custodianPartyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, custodianPartyID)
	protocolID := "70000000-0000-4000-8000-0000060000c0"
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_c1', 'Vaccination C1', 'vaccination', 'active')`,
		protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		protocolVersionID, tenantID, protocolID)
	execProjectionSQL(t, ctx, pool, "accepted rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w1', 'birth_age')`, ruleAcceptedID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "scheduled rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'ppr_adult', 'birth_age')`, ruleScheduledID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)
	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

	// Dose 1: given and accepted two days ago.
	execProjectionSQL(t, ctx, pool, "obligation accepted",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'completed', $7::timestamptz, 'kpi-grain-accepted-c1')`,
		oblAccepted, tenantID, protocolVersionID, goatID, shedID, ruleAcceptedID, asOf.Add(-2*24*time.Hour))
	execProjectionSQL(t, ctx, pool, "completion accepted",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ('70000000-0000-4000-8000-0000090000c1', $1, $2, $3, 'accepted', $4::timestamptz, $4::timestamptz, 'kpi-grain-completion-c1')`,
		tenantID, oblAccepted, goatID, asOf.Add(-2*24*time.Hour))

	// Dose 2: the SAME animal, still scheduled for a future business day.
	execProjectionSQL(t, ctx, pool, "obligation scheduled",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'scheduled', $7::timestamptz, 'kpi-grain-scheduled-c1')`,
		oblScheduled, tenantID, protocolVersionID, goatID, shedID, ruleScheduledID, asOf.Add(3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{
		TenantID: tenantID,
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.Targets != 1 {
		t.Fatalf("targets = %d, want 1 distinct animal", resp.KPIs.Targets)
	}
	if resp.KPIs.DosesVerified != 1 {
		t.Fatalf("doses_verified = %d, want 1", resp.KPIs.DosesVerified)
	}
	if resp.KPIs.ScheduledAhead != 0 {
		t.Fatalf("scheduled_ahead = %d, want 0; the animal is already counted as verified and must not appear twice", resp.KPIs.ScheduledAhead)
	}
	sum := resp.KPIs.DosesVerified + resp.KPIs.AwaitingVerification + resp.KPIs.OverdueNotGiven + resp.KPIs.ScheduledAhead
	if sum != resp.KPIs.Targets {
		t.Fatalf("tiles sum to %d but targets = %d; the four tiles must partition targets", sum, resp.KPIs.Targets)
	}
}

// TestVaccinationCommandBoardKPIParkScopeExcludesAnotherParksAnimals pins the SCOPE half of
// the same aggregate. The per-animal fold groups on target_id, and the park filter reaches
// the animal only through locations.parent_location_id (goat -> shed -> park). If that
// parenting is ever flattened or dropped, the fold silently starts counting the other park's
// animals into a park-scoped board -- and because the tiles would still sum to targets, the
// disjointness test above would keep passing while the number was wrong.
func TestVaccinationCommandBoardKPIParkScopeExcludesAnotherParksAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000d1"
	mineParkID := "70000000-0000-4000-8000-0000010000d1"
	otherParkID := "70000000-0000-4000-8000-0000010000d2"
	mineShedID := "70000000-0000-4000-8000-0000020000d1"
	otherShedID := "70000000-0000-4000-8000-0000020000d2"
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "d1")

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	seedCommandBoardPark(t, ctx, pool, tenantID, mineParkID, mineShedID, "Mine")
	seedCommandBoardPark(t, ctx, pool, tenantID, otherParkID, otherShedID, "Other")
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, mineShedID, "d1a", asOf.Add(3*24*time.Hour))
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, otherShedID, "d1b", asOf.Add(3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{
		TenantID: tenantID,
		ParkID:   &mineParkID,
		AsOf:     asOf,
	})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	if resp.KPIs.Targets != 1 {
		t.Fatalf("targets = %d, want 1; the other park's animal must not enter a park-scoped board", resp.KPIs.Targets)
	}

	all, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("unscoped VaccinationCommandBoard() error = %v", err)
	}
	if all.KPIs.Targets != 2 {
		t.Fatalf("unscoped targets = %d, want 2; the park filter must NARROW, not be the only thing that works", all.KPIs.Targets)
	}
}

// TestVaccinationCommandBoardKPIExecutionDateWindowExcludesOutOfWindowDoses pins the DATE
// half. scheduled_ahead is bounded by a forward window from as_of, so an obligation due far
// outside it must not be counted -- otherwise the tile answers "everything ever scheduled"
// while presenting itself as the near-term workload, and it would grow without bound as the
// protocol generates future doses.
func TestVaccinationCommandBoardKPIExecutionDateWindowExcludesOutOfWindowDoses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000e1"
	parkID := "70000000-0000-4000-8000-0000010000e1"
	shedID := "70000000-0000-4000-8000-0000020000e1"
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "e1")

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Window")
	// In window: a few days out. Out of window: a year out.
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "e1near", asOf.Add(3*24*time.Hour))
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "e1far", asOf.Add(365*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	if resp.KPIs.ScheduledAhead != 1 {
		t.Fatalf("scheduled_ahead = %d, want 1; only the in-window dose belongs to the near-term tile", resp.KPIs.ScheduledAhead)
	}
}

// --- shared fixture helpers for the scope and date cases above. Deliberately minimal: they
// seed only what the KPI aggregate actually reads, so a failure points at the aggregate
// rather than at fixture drift.

func seedCommandBoardProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, suffix string) (protocolVersionID, ruleID string) {
	t.Helper()
	protocolID := "70000000-0000-4000-8000-00000600" + suffix + "0"
	protocolVersionID = "70000000-0000-4000-8000-00000600" + suffix + "1"
	ruleID = "70000000-0000-4000-8000-00000700" + suffix + "1"
	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org', 'active')
		 ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_`+suffix+`', 'Vaccination `+suffix+`', 'vaccination', 'active')`,
		protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		protocolVersionID, tenantID, protocolID)
	execProjectionSQL(t, ctx, pool, "rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'ppr_adult', 'birth_age')`, ruleID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)
	return protocolVersionID, ruleID
}

func seedCommandBoardPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, parkID, shedID, label string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'park', NULL, 'active')`, parkID, tenantID, "Park "+label)
	execProjectionSQL(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'shed', $4, 'active')`, shedID, tenantID, "Shed "+label, parkID)
}

// seedCommandBoardScheduledAnimal creates one goat in the shed with ONE scheduled obligation
// due at dueAt. suffix must be unique per animal within a test.
func seedCommandBoardScheduledAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time) {
	t.Helper()
	partyID := uuidFromSuffix("0a", suffix)
	goatID := uuidFromSuffix("03", suffix)
	oblID := uuidFromSuffix("08", suffix)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)
	execProjectionSQL(t, ctx, pool, "obligation scheduled",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'scheduled', $7::timestamptz, $8)`,
		oblID, tenantID, protocolVersionID, goatID, shedID, ruleID, dueAt, "kpi-"+suffix)
}

// uuidFromSuffix builds a deterministic, collision-free uuid from a short test suffix so the
// helpers never need callers to hand-write ids.
func uuidFromSuffix(group, suffix string) string {
	h := 0
	for _, r := range suffix {
		h = h*31 + int(r)
	}
	return fmt.Sprintf("70000000-0000-4000-8000-0000%s0000%03x", group, h%0xfff)
}

// TestVaccinationCommandBoardKPIStatusBucketsEveryStatusClosedWithoutDoseReconcilesTargets is the
// regression for the tiles summing to LESS than the total they sit under.
//
// targets is COUNT(DISTINCT target_id) over the drive's animals, so an animal stays in the total
// even after its work is called off. But an animal whose every obligation closed with no
// completion against it ('canceled' here, and equally 'waived'/'superseded') satisfies none of the
// four original predicates: it is not open, so it is neither overdue nor scheduled, and nothing
// was ever recorded, so it is neither awaiting nor verified. A 100-animal drive with 3 withdrawn
// animals therefore read "Total 100" over tiles summing to 97, and the reader could not tell that
// hole apart from a display bug or missing data.
//
// The fixture is the smallest shape that reproduces it: one animal still scheduled, one animal
// cancelled. Before the fix the four tiles summed to 1 under a total of 2.
func TestVaccinationCommandBoardKPIStatusBucketsEveryStatusClosedWithoutDoseReconcilesTargets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f1"
	parkID := "70000000-0000-4000-8000-0000010000f1"
	shedID := "70000000-0000-4000-8000-0000020000f1"
	protocolID := "70000000-0000-4000-8000-0000060000f0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000f1"
	ruleID := "70000000-0000-4000-8000-0000070000f1"
	partyID := "70000000-0000-4000-8000-00000a0000f1"
	scheduledGoat := "70000000-0000-4000-8000-0000030000f1"
	cancelledGoat := "70000000-0000-4000-8000-0000030000f2"

	seedKPIFixtureBase(t, ctx, pool, tenantID, parkID, shedID, protocolID, protocolVersionID, ruleID, partyID, "f1")

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, scheduledGoat)
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, cancelledGoat)

	execProjectionSQL(t, ctx, pool, "obligation still scheduled",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ('70000000-0000-4000-8000-0000080000f1', $1, $2, $3, 'goat', 'shed', $4, $5, 'scheduled', $6::timestamptz, 'kpi-closed-scheduled-f1')`,
		tenantID, protocolVersionID, scheduledGoat, shedID, ruleID, asOf.Add(3*24*time.Hour))

	// Called off after planning: closed status, and deliberately NO vaccination_completions row.
	execProjectionSQL(t, ctx, pool, "obligation cancelled without dose",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ('70000000-0000-4000-8000-0000080000f2', $1, $2, $3, 'goat', 'shed', $4, $5, 'canceled', $6::timestamptz, 'kpi-closed-canceled-f1')`,
		tenantID, protocolVersionID, cancelledGoat, shedID, ruleID, asOf.Add(-3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.Targets != 2 {
		t.Fatalf("targets = %d, want 2; a called-off animal is still on the drive's roster", resp.KPIs.Targets)
	}
	if resp.KPIs.ClosedWithoutDose != 1 {
		t.Fatalf("closed_without_dose = %d, want 1; the cancelled animal must be NAMED, not left as an unexplained hole", resp.KPIs.ClosedWithoutDose)
	}
	if resp.KPIs.ScheduledAhead != 1 {
		t.Fatalf("scheduled_ahead = %d, want 1; the cancelled animal must not leak into outstanding work", resp.KPIs.ScheduledAhead)
	}
	if resp.KPIs.OverdueNotGiven != 0 {
		t.Fatalf("overdue_not_given = %d, want 0; a closed obligation is not outstanding no matter how far past its due date it is", resp.KPIs.OverdueNotGiven)
	}
	sum := resp.KPIs.DosesVerified + resp.KPIs.AwaitingVerification + resp.KPIs.OverdueNotGiven +
		resp.KPIs.ScheduledAhead + resp.KPIs.ClosedWithoutDose
	if sum != resp.KPIs.Targets {
		t.Fatalf("tiles sum to %d but targets = %d; the five tiles must be an EXHAUSTIVE partition of targets, not a subset of it", sum, resp.KPIs.Targets)
	}
}

// TestVaccinationCommandBoardDriveOptionsPaginationPageBoundaryMultiPageReportsTruncation pins the
// bound's honesty rather than its size.
//
// The picker has always been bounded, and must stay bounded. What it did not do was SAY so: rows
// past the bound were dropped silently, so a drive that is genuinely scheduled looked exactly like
// a drive that was never planned, and a user going to find it concluded the work did not exist.
// The row grain is (batch, park), so a drive running in two parks spends two slots and a two-park
// programme reaches the bound at half as many drives as anyone would predict — the bound is hit in
// practice, not in theory.
//
// The limit is driven down to 1 instead of seeding 200 drives: the boundary behaviour is what is
// under test, and a fixture slow enough to be skipped proves nothing.
func TestVaccinationCommandBoardDriveOptionsPaginationPageBoundaryMultiPageReportsTruncation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f2"
	parkID := "70000000-0000-4000-8000-0000010000f3"
	shedID := "70000000-0000-4000-8000-0000020000f3"
	protocolID := "70000000-0000-4000-8000-0000060000f4"
	protocolVersionID := "70000000-0000-4000-8000-0000060000f5"
	ruleID := "70000000-0000-4000-8000-0000070000f5"
	partyID := "70000000-0000-4000-8000-00000a0000f5"
	goatID := "70000000-0000-4000-8000-0000030000f5"

	seedKPIFixtureBase(t, ctx, pool, tenantID, parkID, shedID, protocolID, protocolVersionID, ruleID, partyID, "f2")
	seedKPIGoat(t, ctx, pool, tenantID, shedID, partyID, goatID)

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	// Two drives, newest first by planned date. With the limit at 1 only the newer is offered.
	for i, drive := range []struct {
		batchID     string
		obligation  string
		plannedDate string
	}{
		{"70000000-0000-4000-8000-0000090000f1", "70000000-0000-4000-8000-0000080000f5", "2026-08-10"},
		{"70000000-0000-4000-8000-0000090000f2", "70000000-0000-4000-8000-0000080000f6", "2026-07-20"},
	} {
		execProjectionSQL(t, ctx, pool, "obligation batch "+drive.batchID,
			`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, window_start, window_end)
			 VALUES ($1, $2, $3, 'shed', $4, 'planned', $5::date, $5::timestamptz, $5::timestamptz)`,
			drive.batchID, tenantID, protocolVersionID, shedID, drive.plannedDate)
		execProjectionSQL(t, ctx, pool, "drive obligation "+drive.batchID,
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, batch_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, $5, 'goat', 'shed', $6, $7, 'scheduled', $8::timestamptz, $9)`,
			drive.obligation, tenantID, protocolVersionID, drive.batchID, goatID, shedID, ruleID,
			asOf.Add(time.Duration(i+1)*24*time.Hour), fmt.Sprintf("drive-options-f2-%d", i))
	}

	repo := NewRepository(pool, 5*time.Second)

	repo.driveOptionsLimit = 1
	bounded, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("bounded VaccinationCommandBoard() error = %v", err)
	}
	if len(bounded.DriveOptions) != 1 {
		t.Fatalf("drive options = %d, want 1; the limit+1 probe row must never be rendered", len(bounded.DriveOptions))
	}
	if !bounded.DriveOptionsTruncated {
		t.Fatalf("driveOptionsTruncated = false with a dropped drive; silent truncation is what makes a scheduled drive look unplanned")
	}
	if bounded.DriveOptions[0].DriveBatchID != "70000000-0000-4000-8000-0000090000f1" {
		t.Fatalf("kept drive = %s, want the newest planned date first", bounded.DriveOptions[0].DriveBatchID)
	}

	repo.driveOptionsLimit = 10
	full, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("unbounded-enough VaccinationCommandBoard() error = %v", err)
	}
	if len(full.DriveOptions) != 2 {
		t.Fatalf("drive options = %d, want 2; the bound must NARROW the list, not be the only thing that works", len(full.DriveOptions))
	}
	if full.DriveOptionsTruncated {
		t.Fatalf("driveOptionsTruncated = true with room to spare; a false overflow signal trains readers to ignore the real one")
	}
}

// seedKPIFixtureBase seeds the tenant/park/shed/party/protocol scaffolding the KPI and drive-option
// aggregates read. Ids are passed in rather than derived, because the derived-id helper above
// builds malformed uuids for some suffixes and these cases must fail on the aggregate, not on the
// fixture.
func seedKPIFixtureBase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, parkID, shedID, protocolID, protocolVersionID, ruleID, partyID, suffix string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'park', NULL, 'active')`, parkID, tenantID, "Park "+suffix)
	execProjectionSQL(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'shed', $4, 'active')`, shedID, tenantID, "Shed "+suffix, parkID)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, $3, $4, 'vaccination', 'active')`,
		protocolID, tenantID, "vaccination_"+suffix, "Vaccination "+suffix)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		protocolVersionID, tenantID, protocolID)
	execProjectionSQL(t, ctx, pool, "rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'ppr_adult', 'birth_age')`, ruleID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)
}

func seedKPIGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID, partyID, goatID string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)
}
