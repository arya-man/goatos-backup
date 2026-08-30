package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationCommandBoardCohortMatrixReconcilesWithKPIsStatusBucketsCrossSurfaceParityParkScope
// pins the CEO command board's cohort matrix to the SAME submission truth its own KPI row reads.
//
// Live defect (two parks, 20 animals each, every animal vaccinated and submitted, none verified):
//
//	KPI row:        awaiting_verification = 40
//	cohort matrix:  pendingCount 12 (F) + 8 (M) per park = 40 "pending", verifiedCount 0
//
// The cohort matrix was byte-identical BEFORE and AFTER a full 20-animal submission, and identical
// to a park that had been submitted since the previous day. A CEO reading the page sees
// "40 awaiting verification" in the KPI row and "40 pending" in the matrix directly below it, with
// no column that reconciles the two -- and cannot tell that all 40 animals have in fact been
// vaccinated.
//
// The query was conformant to its written contract
// (docs/architecture/operational-read-model-contract.md: pending_count =
// COUNT(DISTINCT obligation_id WHERE status IN ('scheduled','due'))) because obligation status
// advances only on VERIFICATION, not on submission. The DEFINITION was the defect: "pending"
// silently fused two opposite operational states -- field work not done, and field work done and
// awaiting a verifier.
//
// The matrix now carries a submission dimension. THREE disjoint buckets per cell:
//
//	pendingCount   -- open obligation, no completion recorded: the operator still owes this work
//	submittedCount -- completion recorded, not yet verifier-accepted: field work DONE, review owed
//	verifiedCount  -- verifier-accepted
//
// StatusBuckets / CrossSurfaceParity / ParkScope / OneToMany.
func TestVaccinationCommandBoardCohortMatrixReconcilesWithKPIsStatusBucketsCrossSurfaceParityParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Kolkata) error = %v", err)
	}

	const (
		tenantID          = "00000000-0000-4000-8000-0000000000e1"
		parkCBE           = "70000000-0000-4000-8000-0000010000e1"
		parkCPT           = "70000000-0000-4000-8000-0000010000e2"
		shedCBE           = "70000000-0000-4000-8000-0000020000e1"
		shedCPT           = "70000000-0000-4000-8000-0000020000e2"
		protocolID        = "70000000-0000-4000-8000-0000060000e0"
		protocolVersionID = "70000000-0000-4000-8000-0000060000e1"
		ruleID            = "70000000-0000-4000-8000-0000070000e1"
		batchCBE          = "70000000-0000-4000-8000-0000040000e1"
		batchCPT          = "70000000-0000-4000-8000-0000040000e2"
		custodianPartyID  = "70000000-0000-4000-8000-0000090000e1"
		herdPerPark       = 20
		femalePerPark     = 12
	)

	// Fixed business dates, IST. Both drives planned 2026-08-02 in a window running to 2026-08-05;
	// the board is read on business day 2026-08-03. No now()±N anywhere.
	plannedDate := time.Date(2026, 8, 2, 0, 0, 0, 0, ist)
	asOf := time.Date(2026, 8, 3, 11, 0, 0, 0, ist)
	administeredAt := time.Date(2026, 8, 2, 15, 0, 0, 0, ist)
	windowEnd := time.Date(2026, 8, 5, 23, 59, 59, 0, ist)

	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org E', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian E', 'active')`,
		custodianPartyID)
	for _, p := range []struct{ id, name string }{{parkCBE, "CBE-QA"}, {parkCPT, "CPT-QA"}} {
		execProjectionSQL(t, ctx, pool, "park "+p.name,
			`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
			 VALUES ($1, $2, $3, 'park', NULL, 'active')`, p.id, tenantID, p.name)
	}
	for _, s := range []struct{ id, name, park string }{{shedCBE, "Shed CBE", parkCBE}, {shedCPT, "Shed CPT", parkCPT}} {
		execProjectionSQL(t, ctx, pool, "shed "+s.name,
			`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
			 VALUES ($1, $2, $3, 'shed', $4, 'active')`, s.id, tenantID, s.name, s.park)
	}

	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_e', 'Vaccination E', 'vaccination', 'active')`, protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`, protocolVersionID, tenantID, protocolID)
	execProjectionSQL(t, ctx, pool, "rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'ppr_adult', 'birth_age')`, ruleID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)

	for _, b := range []struct{ id, name, park string }{{batchCBE, "cbe", parkCBE}, {batchCPT, "cpt", parkCPT}} {
		execProjectionSQL(t, ctx, pool, "drive batch "+b.name,
			`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id,
			   planned_date, window_start, window_end, status)
			 VALUES ($1, $2, $3, 'park', $4, $5::date, $5::timestamptz, $6::timestamptz, 'in_progress')`,
			b.id, tenantID, protocolVersionID, b.park, plannedDate, windowEnd)
	}

	// BOTH parks fully submitted: every animal scanned, a 'recorded' completion on every
	// obligation, verified_at NULL everywhere. Obligations sit in the sweeper's post-due 'due'
	// state -- obligation status does NOT advance on submission, which is exactly why the matrix
	// could not see the work.
	seedHerd := func(parkLabel, shedID, batchID string, offset int) {
		for i := 0; i < herdPerPark; i++ {
			goatID := fmt.Sprintf("70000000-0000-4000-8000-0000300%05d", offset+i)
			oblID := fmt.Sprintf("70000000-0000-4000-8000-0000800%05d", offset+i)
			sex := "female"
			if i >= femalePerPark {
				sex = "male"
			}
			execProjectionSQL(t, ctx, pool, "goat "+parkLabel,
				`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
				 VALUES ($1, $2, $3, 'alive', 'Non-Pregnant', $4, $5, '2024-01-01')`,
				goatID, tenantID, sex, shedID, custodianPartyID)
			execProjectionSQL(t, ctx, pool, "obligation "+parkLabel,
				`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
				   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
				 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'due', $7::timestamptz, $8, $9)`,
				oblID, tenantID, protocolVersionID, goatID, shedID, ruleID, plannedDate, batchID,
				fmt.Sprintf("obl-e-%s-%d", parkLabel, i))
			execProjectionSQL(t, ctx, pool, "completion "+parkLabel,
				`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
				 VALUES ($1, $2, $3, $4, 'recorded', $5::timestamptz, NULL, $6)`,
				fmt.Sprintf("70000000-0000-4000-8000-0000900%05d", offset+i), tenantID, oblID, goatID, administeredAt,
				fmt.Sprintf("comp-e-%s-%d", parkLabel, i))
		}
	}
	seedHerd("cbe", shedCBE, batchCBE, 201)
	seedHerd("cpt", shedCPT, batchCPT, 301)

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	k := resp.KPIs
	t.Logf("KPIs: targets=%d verified=%d awaiting=%d overdue=%d scheduledAhead=%d",
		k.Targets, k.DosesVerified, k.AwaitingVerification, k.OverdueNotGiven, k.ScheduledAhead)

	if k.Targets != 2*herdPerPark || k.AwaitingVerification != 2*herdPerPark {
		t.Fatalf("fixture did not reproduce the live state: targets=%d awaiting=%d, want %d/%d",
			k.Targets, k.AwaitingVerification, 2*herdPerPark, 2*herdPerPark)
	}

	pending, submitted, verified := 0, 0, 0
	cohortMatrix := cohortMatrixCells(t, ctx, pool, tenantID, asOf)
	for _, cell := range cohortMatrix {
		t.Logf("cohort cell park=%s stage=%s sex=%s vaccine=%s animals=%d pending=%d submitted=%d verified=%d",
			cell.Cohort.ParkName, cell.Cohort.ManagementStage, cell.Cohort.Sex, cell.VaccineLabel,
			cell.Cohort.AnimalCount, cell.PendingCount, cell.SubmittedCount, cell.VerifiedCount)
		pending += cell.PendingCount
		submitted += cell.SubmittedCount
		verified += cell.VerifiedCount
	}

	// (a) The matrix must SEE the submission. Before the fix this bucket did not exist and the
	// same 40 animals were reported as "pending".
	if submitted != 2*herdPerPark {
		t.Errorf("cohort matrix submitted total = %d, want %d -- every animal on both parks has a "+
			"recorded, unverified completion, so the matrix must show the field work as DONE-awaiting-review, "+
			"not as work the operator still owes", submitted, 2*herdPerPark)
	}

	// (b) The matrix must RECONCILE with the KPI row printed directly above it. Both parks are
	// fully submitted, so nothing is still owed by an operator.
	if pending != 0 {
		t.Errorf("cohort matrix pending total = %d while the KPI row says awaiting_verification = %d and "+
			"overdue_not_given = %d. A CEO reading \"%d pending\" below \"%d awaiting verification\" cannot tell "+
			"that all %d animals have actually been vaccinated -- pending must mean field work NOT DONE",
			pending, k.AwaitingVerification, k.OverdueNotGiven, pending, k.AwaitingVerification, 2*herdPerPark)
	}
	if submitted != k.AwaitingVerification {
		t.Errorf("cohort matrix submitted total = %d but KPI awaiting_verification = %d -- one page, two answers",
			submitted, k.AwaitingVerification)
	}
	if verified != k.DosesVerified {
		t.Errorf("cohort matrix verified total = %d but KPI doses_verified = %d", verified, k.DosesVerified)
	}
	if pending != k.OverdueNotGiven+k.ScheduledAhead {
		t.Errorf("cohort matrix pending total = %d but KPI overdue_not_given + scheduled_ahead = %d",
			pending, k.OverdueNotGiven+k.ScheduledAhead)
	}

	// (c) The three buckets are disjoint and never exceed the obligation total in the cell.
	for _, cell := range cohortMatrix {
		if cell.PendingCount+cell.SubmittedCount+cell.VerifiedCount > cell.Cohort.AnimalCount {
			t.Errorf("cohort cell %s/%s/%s/%s: pending %d + submitted %d + verified %d exceeds animal count %d -- "+
				"the buckets must be disjoint",
				cell.Cohort.ParkName, cell.Cohort.ManagementStage, cell.Cohort.Sex, cell.VaccineLabel,
				cell.PendingCount, cell.SubmittedCount, cell.VerifiedCount, cell.Cohort.AnimalCount)
		}
	}
}
