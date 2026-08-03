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
