package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationCommandBoardKPIBucketsDisjointAtAnimalGrain is the regression for the
// KPI tiles double-counting a multi-dose animal.
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
func TestVaccinationCommandBoardKPIBucketsDisjointAtAnimalGrain(t *testing.T) {
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
