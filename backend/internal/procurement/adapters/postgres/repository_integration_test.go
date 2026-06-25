package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

const (
	testTenant         = "00000000-0000-4000-8000-000000000001"
	testSourceParty    = "00000000-0000-4000-8000-000000001101"
	testSourceLocation = "00000000-0000-4000-8000-000000003003"
	testPark           = "00000000-0000-4000-8000-000000003001"
	testShed           = "71000000-0000-4000-8000-000000000001"
	testOtherGoat      = "71000000-0000-4000-8000-0000000000ff"
)

func TestProcurementSourceEntryPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcurementCommon(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)

	t.Run("45-70 day source warmup is valid", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
		for _, days := range []int{45, 70} {
			load := createProcurementLoad(t, ctx, repo, "warmup-load-"+itoa(days), 1)
			end := start.Add(time.Duration(days) * 24 * time.Hour)
			goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
				TenantID:          testTenant,
				LoadID:            load.LoadID,
				SourceTag:         strPtr("WARMUP-" + itoa(days)),
				WarmupStartedAt:   &start,
				WarmupEndedAt:     &end,
				HoldingLocationID: strPtr(testSourceLocation),
				IdempotencyKey:    "warmup-goat-" + itoa(days),
			})
			if goat.WarmupDays == nil || *goat.WarmupDays != days {
				t.Fatalf("warmup days = %v, want %d", goat.WarmupDays, days)
			}
			var stayDays int
			if err := pool.QueryRow(ctx, `SELECT warmup_days FROM source_holding_stays WHERE tenant_id=$1 AND goat_id=$2`, testTenant, goat.GoatID).Scan(&stayDays); err != nil {
				t.Fatalf("read warmup stay: %v", err)
			}
			if stayDays != days {
				t.Fatalf("stay warmup days = %d, want %d", stayDays, days)
			}
		}
	})

	t.Run("source RFID conflict blocks identity without duplicate goat", func(t *testing.T) {
		seedExistingRFIDGoat(t, ctx, pool, testOtherGoat, "RFID-CONFLICT-1")
		before := countRows(t, ctx, pool, `SELECT count(*) FROM goats WHERE tenant_id=$1`, testTenant)
		load := createProcurementLoad(t, ctx, repo, "rfid-conflict-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceRFID:     strPtr("RFID-CONFLICT-1"),
			TemporaryID:    strPtr("TEMP-RFID-CONFLICT"),
			IdempotencyKey: "rfid-conflict-goat",
		})
		after := countRows(t, ctx, pool, `SELECT count(*) FROM goats WHERE tenant_id=$1`, testTenant)
		if after != before {
			t.Fatalf("goat count changed from %d to %d; duplicate RFID goat was created", before, after)
		}
		if goat.GoatID != testOtherGoat || goat.IdentityReview != "conflict" || goat.CurrentState != domain.GoatStatePreDispatchBlocked {
			t.Fatalf("rfid conflict row = %#v, want existing goat blocked with identity conflict", goat)
		}
	})

	t.Run("pending identity ownership health cannot accepted intake", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "pending-intake-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("PENDING-INTAKE"),
			IdempotencyKey: "pending-intake-goat",
		})
		proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000101", "pending-intake-proof")
		seedTransitProof(t, ctx, pool, load.LoadID, proofID)
		_, err := pool.Exec(ctx, `
UPDATE procurement_load_goats
SET current_state='arrival_accepted',
    selection_state='loaded',
    loaded_at=TIMESTAMPTZ '2026-02-01 10:00:00+00',
    arrived_at=TIMESTAMPTZ '2026-02-01 16:00:00+00',
    health_state='pending',
    identity_review_state='pending',
    ownership_state='shared_pending'
WHERE tenant_id=$1 AND load_id=$2 AND goat_id=$3`, testTenant, load.LoadID, goat.GoatID)
		if err != nil {
			t.Fatalf("force pending arrival row: %v", err)
		}
		_, err = repo.AcceptIntake(ctx, ports.AcceptIntake{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatIDs:        []string{goat.GoatID},
			ParkLocationID: testPark,
			ShedLocationID: testShed,
			AcceptedAt:     time.Date(2026, 2, 1, 17, 0, 0, 0, time.UTC),
			EntryDate:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			IdempotencyKey: "pending-intake-accept",
		})
		if !errors.Is(err, ports.ErrInvalidTransition) {
			t.Fatalf("AcceptIntake pending row error = %v, want ErrInvalidTransition", err)
		}
		assertNoPHCHandoff(t, ctx, pool, goat.GoatID)
	})

	t.Run("arrival extra unknown cannot accepted intake", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "arrival-extra-load", 1)
		_, err := repo.RecordArrivalReview(ctx, ports.ArrivalReview{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			ParkLocationID: testPark,
			ExpectedCount:  1,
			LoadedCount:    1,
			ArrivedCount:   2,
			ExtraCount:     1,
			Status:         "mismatch",
			ReviewedAt:     time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC),
			IdempotencyKey: "arrival-extra-review",
			Goats: []ports.ArrivalGoat{
				{TemporaryID: strPtr("UNKNOWN-EXTRA-1"), ArrivalState: "extra_unresolved"},
			},
		})
		if err != nil {
			t.Fatalf("RecordArrivalReview extra: %v", err)
		}
		_, err = repo.AcceptIntake(ctx, ports.AcceptIntake{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			ParkLocationID: testPark,
			ShedLocationID: testShed,
			AcceptedAt:     time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			EntryDate:      time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			IdempotencyKey: "arrival-extra-intake",
		})
		if !errors.Is(err, ports.ErrInvalidTransition) {
			t.Fatalf("AcceptIntake extra row error = %v, want ErrInvalidTransition", err)
		}
	})

	t.Run("dispatch with proof but zero eligible goats fails closed", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "dispatch-zero-eligible-load", 1)
		_ = addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("ZERO-ELIGIBLE"),
			IdentityState:  "clean",
			OwnershipState: "mesha_owned",
			IdempotencyKey: "dispatch-zero-eligible-goat",
		})
		proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000180", "dispatch-zero-eligible-proof")
		_, err := repo.DispatchLoad(ctx, ports.DispatchLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			ToLocationID:   testPark,
			ProofRefID:     &proofID,
			DispatchedAt:   time.Date(2026, 4, 2, 11, 0, 0, 0, time.UTC),
			IdempotencyKey: "dispatch-zero-eligible",
		})
		if !errors.Is(err, ports.ErrInvalidTransition) {
			t.Fatalf("DispatchLoad zero eligible error = %v, want ErrInvalidTransition", err)
		}
		handoffCount := countRows(t, ctx, pool, `SELECT count(*) FROM transit_handoffs WHERE tenant_id=$1 AND load_id=$2`, testTenant, load.LoadID)
		if handoffCount != 0 {
			t.Fatalf("transit handoffs = %d, want 0", handoffCount)
		}
		var status string
		if err := pool.QueryRow(ctx, `SELECT status FROM procurement_loads WHERE tenant_id=$1 AND load_id=$2`, testTenant, load.LoadID).Scan(&status); err != nil {
			t.Fatalf("read load status: %v", err)
		}
		if status == domain.LoadStatusInTransit {
			t.Fatalf("load status = %q, want not in_transit", status)
		}
	})

	t.Run("exception-only work rows include owner missing and exclude normal due", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "exception-only-work-load", 2)
		dueGoat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("WORK-DUE"),
			IdentityState:  "clean",
			OwnershipState: "mesha_owned",
			HealthState:    domain.HealthPassed,
			IdempotencyKey: "work-due-goat",
		})
		ownerMissingGoat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("WORK-OWNER-MISSING"),
			IdentityState:  "clean",
			OwnershipState: "shared_pending",
			HealthState:    domain.HealthPassed,
			IdempotencyKey: "work-owner-missing-goat",
		})
		result, err := repo.ListWorkRows(ctx, domain.WorkQuery{
			TenantID:      testTenant,
			Limit:         100,
			ExceptionOnly: true,
		})
		if err != nil {
			t.Fatalf("ListWorkRows(exception only): %v", err)
		}
		dueRowID := "load_goat:" + dueGoat.LoadGoatID
		ownerMissingRowID := "load_goat:" + ownerMissingGoat.LoadGoatID
		foundOwnerMissing := false
		for _, row := range result.Rows {
			if row.RowID == dueRowID {
				t.Fatalf("normal due row leaked into exception-only work rows: %#v", row)
			}
			if row.RowID == ownerMissingRowID {
				foundOwnerMissing = true
				if row.WorkState != "owner_missing" || row.WorkType != "ownership" {
					t.Fatalf("owner missing row = %#v", row)
				}
			}
			if !isProcurementExceptionRow(row) {
				t.Fatalf("non-exception row leaked into exception-only work rows: %#v", row)
			}
		}
		if !foundOwnerMissing {
			t.Fatalf("owner_missing row %s not returned in exception-only work rows: %#v", ownerMissingRowID, result.Rows)
		}
	})

	t.Run("pre-dispatch reject blocks active vaccination work", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "reject-before-truck-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("REJECT-BEFORE-TRUCK"),
			IdentityState:  "clean",
			OwnershipState: "mesha_owned",
			IdempotencyKey: "reject-before-truck-goat",
		})
		_, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatID:         goat.GoatID,
			HealthState:    domain.HealthPassed,
			CheckedAt:      time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
			IdempotencyKey: "reject-before-truck-health",
		})
		if err != nil {
			t.Fatalf("health pass: %v", err)
		}
		_, err = repo.RecordDecision(ctx, ports.Decision{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatID:         goat.GoatID,
			DecisionStage:  "pre_dispatch",
			DecisionType:   domain.DecisionRejected,
			Reason:         "rejected before truck loading",
			DecidedAt:      time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			IdempotencyKey: "reject-before-truck-decision",
		})
		if err != nil {
			t.Fatalf("pre-dispatch reject: %v", err)
		}
		assertNoPHCHandoff(t, ctx, pool, goat.GoatID)
		protocolVersionID, ruleID := seedVaccinationProtocol(t, ctx, pool, "reject-before-truck")
		err = insertActiveVaccinationObligation(ctx, pool, protocolVersionID, ruleID, goat.GoatID, "reject-before-truck-obligation")
		if err == nil || !strings.Contains(err.Error(), "vaccination_obligation_blocked_for_procurement_excluded_goat") {
			t.Fatalf("active vaccination obligation error = %v, want procurement exclusion guard", err)
		}
	})

	t.Run("accepted clean goat creates PHC handoff and workflow read model", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "accepted-clean-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			SourceTag:      strPtr("ACCEPTED-CLEAN"),
			IdentityState:  "clean",
			OwnershipState: "mesha_owned",
			IdempotencyKey: "accepted-clean-goat",
		})
		if _, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatID:         goat.GoatID,
			HealthState:    domain.HealthPassed,
			CheckedAt:      time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
			IdempotencyKey: "accepted-clean-health",
		}); err != nil {
			t.Fatalf("health pass: %v", err)
		}
		if _, err := repo.RecordDecision(ctx, ports.Decision{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatID:         goat.GoatID,
			DecisionStage:  "pre_dispatch",
			DecisionType:   domain.DecisionAccepted,
			DecidedAt:      time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			IdempotencyKey: "accepted-clean-decision",
		}); err != nil {
			t.Fatalf("pre-dispatch accept: %v", err)
		}
		proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000201", "accepted-clean-dispatch-proof")
		if _, err := repo.DispatchLoad(ctx, ports.DispatchLoad{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			ToLocationID:   testPark,
			ProofRefID:     &proofID,
			DispatchedAt:   time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
			IdempotencyKey: "accepted-clean-dispatch",
		}); err != nil {
			t.Fatalf("dispatch: %v", err)
		}
		if _, err := repo.RecordArrivalReview(ctx, ports.ArrivalReview{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			ParkLocationID: testPark,
			ExpectedCount:  1,
			LoadedCount:    1,
			ArrivedCount:   1,
			MatchedCount:   1,
			Status:         domain.DecisionAccepted,
			ReviewedAt:     time.Date(2026, 5, 1, 16, 0, 0, 0, time.UTC),
			IdempotencyKey: "accepted-clean-arrival",
			Goats:          []ports.ArrivalGoat{{GoatID: &goat.GoatID, ArrivalState: "accepted"}},
		}); err != nil {
			t.Fatalf("arrival accepted: %v", err)
		}
		handoffs, err := repo.AcceptIntake(ctx, ports.AcceptIntake{
			TenantID:       testTenant,
			LoadID:         load.LoadID,
			GoatIDs:        []string{goat.GoatID},
			ParkLocationID: testPark,
			ShedLocationID: testShed,
			AcceptedAt:     time.Date(2026, 5, 1, 17, 0, 0, 0, time.UTC),
			EntryDate:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			IdempotencyKey: "accepted-clean-intake",
		})
		if err != nil {
			t.Fatalf("AcceptIntake clean: %v", err)
		}
		if len(handoffs) != 1 || handoffs[0].GoatID != goat.GoatID {
			t.Fatalf("handoffs = %#v", handoffs)
		}
		rowID := "load_goat:" + goat.LoadGoatID
		row, found, err := repo.GetWorkRow(ctx, domain.WorkQuery{TenantID: testTenant, Limit: 1}, rowID)
		if err != nil || !found {
			t.Fatalf("GetWorkRow() found=%v err=%v", found, err)
		}
		if row.WorkState != "completed" || row.CompletedCount != 1 {
			t.Fatalf("work row = %#v, want completed intake", row)
		}
	})

	t.Run("pagination cursor works", func(t *testing.T) {
		ids := make([]string, 0, 3)
		for i := 0; i < 3; i++ {
			load := createProcurementLoad(t, ctx, repo, "pagination-load-"+itoa(i), 0)
			ids = append(ids, load.LoadID)
			_, err := pool.Exec(ctx, `
UPDATE procurement_loads
SET status='canceled', updated_at=$3::timestamptz
WHERE tenant_id=$1 AND load_id=$2`, testTenant, load.LoadID, time.Date(2026, 6, 1, 12-i, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("update pagination load: %v", err)
			}
		}
		first, err := repo.ListLoads(ctx, domain.LoadQuery{TenantID: testTenant, Status: "canceled", Limit: 2})
		if err != nil {
			t.Fatalf("ListLoads first: %v", err)
		}
		if len(first.Items) != 2 || first.NextCursor == nil {
			t.Fatalf("first page = %#v, want 2 items and cursor", first)
		}
		cursor, err := domain.DecodeLoadCursor(*first.NextCursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		second, err := repo.ListLoads(ctx, domain.LoadQuery{TenantID: testTenant, Status: "canceled", Limit: 2, Cursor: &cursor})
		if err != nil {
			t.Fatalf("ListLoads second: %v", err)
		}
		if len(second.Items) != 1 || second.NextCursor != nil {
			t.Fatalf("second page = %#v, want final single item", second)
		}
		_ = ids
	})
}

// TestProcurementIdempotentReplay proves item-3: every flagged write path honors the AGENTS.md write-path
// idempotency contract — an exact replay (same key + same payload) returns the original result and runs NO
// further side effects (row_version stays put), and a same-key/different-payload replay is rejected with
// ErrIdempotencyConflict without mutating state.
func TestProcurementIdempotentReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcurementCommon(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	loadGoatVersion := func(loadID, goatID string) int64 {
		t.Helper()
		var v int64
		if err := pool.QueryRow(ctx, `SELECT row_version FROM procurement_load_goats
			WHERE tenant_id = $1 AND load_id = $2 AND goat_id = $3`, testTenant, loadID, goatID).Scan(&v); err != nil {
			t.Fatalf("read load_goat row_version: %v", err)
		}
		return v
	}

	t.Run("RecordSourceHealth", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "idem-health-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID, SourceTag: strPtr("IDEM-HEALTH"),
			IdentityState: "clean", OwnershipState: "mesha_owned", IdempotencyKey: "idem-health-goat",
		})
		in := ports.SourceHealth{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID,
			HealthState: domain.HealthPassed, CheckedAt: time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC),
			IdempotencyKey: "idem-health",
		}
		first, err := repo.RecordSourceHealth(ctx, in)
		if err != nil {
			t.Fatalf("first RecordSourceHealth: %v", err)
		}
		v1 := loadGoatVersion(load.LoadID, goat.GoatID)

		replay, err := repo.RecordSourceHealth(ctx, in)
		if err != nil {
			t.Fatalf("replay RecordSourceHealth: %v", err)
		}
		if replay.HealthCheckID != first.HealthCheckID {
			t.Fatalf("replay returned different result: first=%s replay=%s", first.HealthCheckID, replay.HealthCheckID)
		}
		if v2 := loadGoatVersion(load.LoadID, goat.GoatID); v2 != v1 {
			t.Fatalf("replay mutated state: row_version %d -> %d", v1, v2)
		}

		bad := in
		bad.HealthState = domain.HealthFailed
		bad.Reason = "different payload"
		if _, err := repo.RecordSourceHealth(ctx, bad); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
		if v3 := loadGoatVersion(load.LoadID, goat.GoatID); v3 != v1 {
			t.Fatalf("conflict mutated state: row_version %d -> %d", v1, v3)
		}
	})

	t.Run("RecordDecision", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "idem-decision-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID, SourceTag: strPtr("IDEM-DECISION"),
			IdentityState: "clean", OwnershipState: "mesha_owned", IdempotencyKey: "idem-decision-goat",
		})
		if _, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, HealthState: domain.HealthPassed,
			CheckedAt: time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC), IdempotencyKey: "idem-decision-health",
		}); err != nil {
			t.Fatalf("health pass: %v", err)
		}
		in := ports.Decision{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID,
			DecisionStage: "pre_dispatch", DecisionType: domain.DecisionAccepted,
			DecidedAt: time.Date(2026, 4, 2, 10, 0, 0, 0, time.UTC), IdempotencyKey: "idem-decision",
		}
		first, err := repo.RecordDecision(ctx, in)
		if err != nil {
			t.Fatalf("first RecordDecision: %v", err)
		}
		v1 := loadGoatVersion(load.LoadID, goat.GoatID)

		// Without branch-first idempotency this replay would re-run the eligibility-guarded UPDATE, find the
		// goat already pre_dispatch_accepted, and wrongly fail with ErrInvalidTransition.
		replay, err := repo.RecordDecision(ctx, in)
		if err != nil {
			t.Fatalf("replay RecordDecision: %v", err)
		}
		if replay.DecisionID != first.DecisionID {
			t.Fatalf("replay returned different result: first=%s replay=%s", first.DecisionID, replay.DecisionID)
		}
		if v2 := loadGoatVersion(load.LoadID, goat.GoatID); v2 != v1 {
			t.Fatalf("replay mutated state: row_version %d -> %d", v1, v2)
		}

		bad := in
		bad.DecisionType = domain.DecisionRejected
		bad.Reason = "different payload"
		if _, err := repo.RecordDecision(ctx, bad); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
		if v3 := loadGoatVersion(load.LoadID, goat.GoatID); v3 != v1 {
			t.Fatalf("conflict mutated state: row_version %d -> %d", v1, v3)
		}
	})

	t.Run("RecordArrivalReview and AcceptIntake", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "idem-arrival-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID, SourceTag: strPtr("IDEM-ARRIVAL"),
			IdentityState: "clean", OwnershipState: "mesha_owned", IdempotencyKey: "idem-arrival-goat",
		})
		if _, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, HealthState: domain.HealthPassed,
			CheckedAt: time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC), IdempotencyKey: "idem-arrival-health",
		}); err != nil {
			t.Fatalf("health pass: %v", err)
		}
		if _, err := repo.RecordDecision(ctx, ports.Decision{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, DecisionStage: "pre_dispatch",
			DecisionType: domain.DecisionAccepted, DecidedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			IdempotencyKey: "idem-arrival-decision",
		}); err != nil {
			t.Fatalf("decision accept: %v", err)
		}
		proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000301", "idem-arrival-dispatch-proof")
		if _, err := repo.DispatchLoad(ctx, ports.DispatchLoad{
			TenantID: testTenant, LoadID: load.LoadID, ToLocationID: testPark, ProofRefID: &proofID,
			DispatchedAt: time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC), IdempotencyKey: "idem-arrival-dispatch",
		}); err != nil {
			t.Fatalf("dispatch: %v", err)
		}

		arrival := ports.ArrivalReview{
			TenantID: testTenant, LoadID: load.LoadID, ParkLocationID: testPark,
			ExpectedCount: 1, LoadedCount: 1, ArrivedCount: 1, MatchedCount: 1,
			Status: domain.DecisionAccepted, ReviewedAt: time.Date(2026, 5, 2, 16, 0, 0, 0, time.UTC),
			IdempotencyKey: "idem-arrival", Goats: []ports.ArrivalGoat{{GoatID: &goat.GoatID, ArrivalState: "accepted"}},
		}
		firstReview, err := repo.RecordArrivalReview(ctx, arrival)
		if err != nil {
			t.Fatalf("first RecordArrivalReview: %v", err)
		}
		v1 := loadGoatVersion(load.LoadID, goat.GoatID)
		replayReview, err := repo.RecordArrivalReview(ctx, arrival)
		if err != nil {
			t.Fatalf("replay RecordArrivalReview: %v", err)
		}
		if replayReview.ReviewID != firstReview.ReviewID || len(replayReview.Goats) != len(firstReview.Goats) {
			t.Fatalf("replay arrival mismatch: first=%s/%d replay=%s/%d", firstReview.ReviewID, len(firstReview.Goats), replayReview.ReviewID, len(replayReview.Goats))
		}
		if v2 := loadGoatVersion(load.LoadID, goat.GoatID); v2 != v1 {
			t.Fatalf("replay arrival mutated state: row_version %d -> %d", v1, v2)
		}
		badArrival := arrival
		badArrival.Status = domain.DecisionRejected
		badArrival.Goats = []ports.ArrivalGoat{{GoatID: &goat.GoatID, ArrivalState: "rejected"}}
		if _, err := repo.RecordArrivalReview(ctx, badArrival); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("arrival same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}

		intake := ports.AcceptIntake{
			TenantID: testTenant, LoadID: load.LoadID, GoatIDs: []string{goat.GoatID},
			ParkLocationID: testPark, ShedLocationID: testShed,
			AcceptedAt: time.Date(2026, 5, 2, 17, 0, 0, 0, time.UTC),
			EntryDate:  time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), IdempotencyKey: "idem-intake",
		}
		firstHandoffs, err := repo.AcceptIntake(ctx, intake)
		if err != nil {
			t.Fatalf("first AcceptIntake: %v", err)
		}
		v3 := loadGoatVersion(load.LoadID, goat.GoatID)
		// Without branch-first idempotency this replay would re-run the eligibility-guarded UPDATE, find the
		// goat already accepted_herd_intake, and wrongly fail with ErrInvalidTransition.
		replayHandoffs, err := repo.AcceptIntake(ctx, intake)
		if err != nil {
			t.Fatalf("replay AcceptIntake: %v", err)
		}
		if len(replayHandoffs) != len(firstHandoffs) || len(firstHandoffs) != 1 || replayHandoffs[0].HandoffID != firstHandoffs[0].HandoffID {
			t.Fatalf("replay intake mismatch: first=%#v replay=%#v", firstHandoffs, replayHandoffs)
		}
		if v4 := loadGoatVersion(load.LoadID, goat.GoatID); v4 != v3 {
			t.Fatalf("replay intake mutated state: row_version %d -> %d", v3, v4)
		}
		badIntake := intake
		badIntake.EntryDate = time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
		if _, err := repo.AcceptIntake(ctx, badIntake); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("intake same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
	})

	countGoats := func() int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM goats WHERE tenant_id = $1`, testTenant).Scan(&n); err != nil {
			t.Fatalf("count goats: %v", err)
		}
		return n
	}
	countLoads := func() int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM procurement_loads WHERE tenant_id = $1`, testTenant).Scan(&n); err != nil {
			t.Fatalf("count loads: %v", err)
		}
		return n
	}

	t.Run("CreateLoad", func(t *testing.T) {
		in := ports.CreateLoad{
			TenantID: testTenant, SourcePartyID: testSourceParty,
			SourceLocationID: strPtr(testSourceLocation), ExpectedCount: 5,
			IdempotencyKey: "idem-createload",
		}
		first, err := repo.CreateLoad(ctx, in)
		if err != nil {
			t.Fatalf("first CreateLoad: %v", err)
		}
		loadsAfterFirst := countLoads()

		replay, err := repo.CreateLoad(ctx, in)
		if err != nil {
			t.Fatalf("replay CreateLoad: %v", err)
		}
		if replay.LoadID != first.LoadID {
			t.Fatalf("replay returned different load: first=%s replay=%s", first.LoadID, replay.LoadID)
		}
		if replay.RowVersion != first.RowVersion {
			t.Fatalf("replay mutated load: row_version %d -> %d", first.RowVersion, replay.RowVersion)
		}
		if n := countLoads(); n != loadsAfterFirst {
			t.Fatalf("replay created a duplicate load: count %d -> %d", loadsAfterFirst, n)
		}

		bad := in
		bad.ExpectedCount = 9
		if _, err := repo.CreateLoad(ctx, bad); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
		if n := countLoads(); n != loadsAfterFirst {
			t.Fatalf("conflict created a load: count %d -> %d", loadsAfterFirst, n)
		}
	})

	t.Run("AddGoatToLoad does not duplicate goats on retry", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "idem-addgoat-load", 1)
		in := ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID,
			SourceTag: strPtr("IDEM-ADDGOAT"), TemporaryID: strPtr("TMP-ADDGOAT"),
			SelectionState: "candidate", CurrentState: domain.GoatStateSourceCandidate,
			IdentityState: "pending", OwnershipState: "pending", HealthState: "pending",
			ProofRefs: []byte("[]"), Metadata: []byte("{}"), IdempotencyKey: "idem-addgoat",
		}
		goatsBefore := countGoats()
		first, err := repo.AddGoatToLoad(ctx, in)
		if err != nil {
			t.Fatalf("first AddGoatToLoad: %v", err)
		}
		goatsAfterFirst := countGoats()
		if goatsAfterFirst != goatsBefore+1 {
			t.Fatalf("first add did not create exactly one goat: %d -> %d", goatsBefore, goatsAfterFirst)
		}
		v1 := loadGoatVersion(load.LoadID, first.GoatID)

		// The dup-goat bug: without a reserve guard this retry inserts a brand-new goats row (the ON CONFLICT
		// on procurement_load_goats is keyed by goat_id, so a fresh goat never conflicts) -> duplicate goat.
		replay, err := repo.AddGoatToLoad(ctx, in)
		if err != nil {
			t.Fatalf("replay AddGoatToLoad: %v", err)
		}
		if replay.LoadGoatID != first.LoadGoatID || replay.GoatID != first.GoatID {
			t.Fatalf("replay returned different row: first=%s/%s replay=%s/%s",
				first.LoadGoatID, first.GoatID, replay.LoadGoatID, replay.GoatID)
		}
		if n := countGoats(); n != goatsAfterFirst {
			t.Fatalf("replay created a duplicate goat: count %d -> %d", goatsAfterFirst, n)
		}
		if v2 := loadGoatVersion(load.LoadID, first.GoatID); v2 != v1 {
			t.Fatalf("replay mutated state: row_version %d -> %d", v1, v2)
		}

		bad := in
		bad.SourceTag = strPtr("IDEM-ADDGOAT-DIFFERENT")
		if _, err := repo.AddGoatToLoad(ctx, bad); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
		if n := countGoats(); n != goatsAfterFirst {
			t.Fatalf("conflict created a goat: count %d -> %d", goatsAfterFirst, n)
		}
	})

	t.Run("server-defaulted timestamp jitter does not break exact replay", func(t *testing.T) {
		// The service fills an omitted CheckedAt/DecidedAt/etc. with now() (service.go), so a retry of the
		// same logical request arrives with a LATER timestamp. Those fields are excluded from the fingerprint,
		// so the replay must return the original result rather than falsely conflicting.
		load := createProcurementLoad(t, ctx, repo, "idem-ts-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID, SourceTag: strPtr("IDEM-TS"),
			IdentityState: "clean", OwnershipState: "mesha_owned", IdempotencyKey: "idem-ts-goat",
		})
		base := ports.SourceHealth{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID,
			HealthState: domain.HealthPassed, CheckedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
			IdempotencyKey: "idem-ts-health",
		}
		first, err := repo.RecordSourceHealth(ctx, base)
		if err != nil {
			t.Fatalf("first RecordSourceHealth: %v", err)
		}
		jittered := base
		jittered.CheckedAt = base.CheckedAt.Add(97 * time.Minute) // same logical request, server-defaulted later
		replay, err := repo.RecordSourceHealth(ctx, jittered)
		if err != nil {
			t.Fatalf("replay with server-jittered timestamp must not conflict: %v", err)
		}
		if replay.HealthCheckID != first.HealthCheckID {
			t.Fatalf("replay returned different result: first=%s replay=%s", first.HealthCheckID, replay.HealthCheckID)
		}
	})

	t.Run("DispatchLoad", func(t *testing.T) {
		load := createProcurementLoad(t, ctx, repo, "idem-dispatch-load", 1)
		goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
			TenantID: testTenant, LoadID: load.LoadID, SourceTag: strPtr("IDEM-DISPATCH"),
			IdentityState: "clean", OwnershipState: "mesha_owned", IdempotencyKey: "idem-dispatch-goat",
		})
		if _, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, HealthState: domain.HealthPassed,
			CheckedAt: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC), IdempotencyKey: "idem-dispatch-health",
		}); err != nil {
			t.Fatalf("health pass: %v", err)
		}
		if _, err := repo.RecordDecision(ctx, ports.Decision{
			TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, DecisionStage: "pre_dispatch",
			DecisionType: domain.DecisionAccepted, DecidedAt: time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC),
			IdempotencyKey: "idem-dispatch-decision",
		}); err != nil {
			t.Fatalf("decision accept: %v", err)
		}
		proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000401", "idem-dispatch-proof")
		in := ports.DispatchLoad{
			TenantID: testTenant, LoadID: load.LoadID, ToLocationID: testPark, ProofRefID: &proofID,
			DispatchedAt: time.Date(2026, 6, 3, 11, 0, 0, 0, time.UTC), IdempotencyKey: "idem-dispatch",
		}
		first, err := repo.DispatchLoad(ctx, in)
		if err != nil {
			t.Fatalf("first DispatchLoad: %v", err)
		}
		v1 := loadGoatVersion(load.LoadID, goat.GoatID)

		// Exact replay (even with a server-jittered dispatched_at) returns the original handoff, no side effects.
		replayIn := in
		replayIn.DispatchedAt = in.DispatchedAt.Add(40 * time.Minute)
		replay, err := repo.DispatchLoad(ctx, replayIn)
		if err != nil {
			t.Fatalf("replay DispatchLoad: %v", err)
		}
		if replay.HandoffID != first.HandoffID {
			t.Fatalf("replay returned different handoff: first=%s replay=%s", first.HandoffID, replay.HandoffID)
		}
		if v2 := loadGoatVersion(load.LoadID, goat.GoatID); v2 != v1 {
			t.Fatalf("replay mutated state: row_version %d -> %d", v1, v2)
		}

		// Same key, different payload (different proof) must be rejected without mutating state.
		proof2 := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000402", "idem-dispatch-proof-2")
		bad := in
		bad.ProofRefID = &proof2
		if _, err := repo.DispatchLoad(ctx, bad); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("same-key different-payload: err = %v, want ErrIdempotencyConflict", err)
		}
		if v3 := loadGoatVersion(load.LoadID, goat.GoatID); v3 != v1 {
			t.Fatalf("conflict mutated state: row_version %d -> %d", v1, v3)
		}
	})
}

func seedProcurementCommon(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1, $2, 'shed', 'PROC_TEST_SHED', 'Procurement Test Shed', $3, 'active')
ON CONFLICT (tenant_id, location_code) DO NOTHING`, testShed, testTenant, testPark)
	if err != nil {
		t.Fatalf("seed shed: %v", err)
	}
}

func createProcurementLoad(t *testing.T, ctx context.Context, repo *Repository, key string, expected int) domain.Load {
	t.Helper()
	load, err := repo.CreateLoad(ctx, ports.CreateLoad{
		TenantID:         testTenant,
		SourcePartyID:    testSourceParty,
		SourceLocationID: strPtr(testSourceLocation),
		ExpectedCount:    expected,
		IdempotencyKey:   key,
	})
	if err != nil {
		t.Fatalf("CreateLoad(%s): %v", key, err)
	}
	return load
}

func addProcurementGoat(t *testing.T, ctx context.Context, repo *Repository, loadID string, in ports.AddGoatToLoad) domain.LoadGoat {
	t.Helper()
	in.TenantID = testTenant
	in.LoadID = loadID
	if in.SelectionState == "" {
		in.SelectionState = "candidate"
	}
	if in.CurrentState == "" {
		in.CurrentState = domain.GoatStateSourceCandidate
		if in.WarmupStartedAt != nil {
			in.CurrentState = domain.GoatStateSourceWarmup
		}
	}
	if in.IdentityState == "" {
		in.IdentityState = "pending"
	}
	if in.OwnershipState == "" {
		in.OwnershipState = "pending"
	}
	if in.HealthState == "" {
		in.HealthState = "pending"
	}
	if in.WarmupDays == nil && in.WarmupStartedAt != nil && in.WarmupEndedAt != nil {
		days := int(in.WarmupEndedAt.Sub(*in.WarmupStartedAt).Hours() / 24)
		in.WarmupDays = &days
	}
	if len(in.ProofRefs) == 0 {
		in.ProofRefs = []byte("[]")
	}
	if len(in.Metadata) == 0 {
		in.Metadata = []byte("{}")
	}
	goat, err := repo.AddGoatToLoad(ctx, in)
	if err != nil {
		t.Fatalf("AddGoatToLoad(%s): %v", in.IdempotencyKey, err)
	}
	return goat
}

func seedExistingRFIDGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, rfid string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id)
VALUES ($1, $2, 'alive', 'clean', '00000000-0000-4000-8000-000000001001', $3, $3)
ON CONFLICT (goat_id) DO NOTHING`, goatID, testTenant, testPark)
	if err != nil {
		t.Fatalf("seed existing goat: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key,
  is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence
) VALUES (
  $1, $2, 'rfid', $3, $4, 'global', true, 'active', now(), 'test', $5, 'test', 1
)
ON CONFLICT DO NOTHING`, testTenant, goatID, rfid, normalizeIdentifier(rfid), "goat:"+goatID)
	if err != nil {
		t.Fatalf("seed existing RFID: %v", err)
	}
}

func insertProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proofID, objectKey string) string {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO proof_artifacts (
  proof_id, tenant_id, storage_provider, object_key, upload_state,
  scope_type, scope_id, subject_type, proof_type, content_hash, mime_type, size_bytes
) VALUES (
  $1, $2, 'local', $3, 'completed', 'tenant', $2, 'other', 'photo', 'hash-' || $3, 'image/jpeg', 100
)`, proofID, testTenant, objectKey)
	if err != nil {
		t.Fatalf("insert proof: %v", err)
	}
	return proofID
}

func seedTransitProof(t *testing.T, ctx context.Context, pool *pgxpool.Pool, loadID, proofID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO transit_handoffs (
  tenant_id, load_id, to_location_id, loaded_count, dispatched_at, proof_ref_id,
  discrepancy_state, status, idempotency_key
) VALUES (
  $1, $2, $3, 1, TIMESTAMPTZ '2026-02-01 10:00:00+00', $4, 'none', 'in_transit', $5
)`, testTenant, loadID, testPark, proofID, "direct-transit-"+loadID)
	if err != nil {
		t.Fatalf("seed transit proof: %v", err)
	}
}

func seedVaccinationProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string) (string, string) {
	t.Helper()
	protocolID := "72000000-0000-4000-8000-000000000001"
	versionID := "73000000-0000-4000-8000-000000000001"
	ruleID := "74000000-0000-4000-8000-000000000001"
	_, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1, $2, $3, 'Procurement Guard Vaccine', 'vaccination', 'active')
ON CONFLICT (tenant_id, code) DO NOTHING`, protocolID, testTenant, "vaccination.procurement_guard_"+strings.ReplaceAll(suffix, "-", "_"))
	if err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy)
VALUES ($1, $2, $3, 'tenant', 1, 'published', DATE '2026-01-01', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id) DO NOTHING`, versionID, testTenant, protocolID)
	if err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, repeat, catch_up, eligibility_json, proof_policy)
VALUES ($1, $2, $3, 'primary', 1, 'post_arrival', 'none', 'phc_approval', '{}'::jsonb, '{}'::jsonb)
ON CONFLICT (tenant_id, rule_id) DO NOTHING`, ruleID, testTenant, versionID)
	if err != nil {
		t.Fatalf("seed protocol rule: %v", err)
	}
	return versionID, ruleID
}

func insertActiveVaccinationObligation(ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, goatID, key string) error {
	_, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key
) VALUES (
  $1, $2, $3, 'goat', $4, 'park', $5, TIMESTAMPTZ '2026-07-01 00:00:00+00', 'scheduled', $6
)`, testTenant, versionID, ruleID, goatID, testPark, key)
	return err
}

func assertNoPHCHandoff(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM procurement_phc_handoffs WHERE tenant_id=$1 AND goat_id=$2`, testTenant, goatID); got != 0 {
		t.Fatalf("PHC handoffs for goat %s = %d, want 0", goatID, got)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func isProcurementExceptionRow(row domain.WorkRow) bool {
	switch row.WorkState {
	case "blocked", "owner_missing", "overdue":
		return true
	case "proof_pending":
		return row.WorkType == "dispatch_proof"
	case "rejected", "deferred":
		return row.Severity == "at_risk" || row.Severity == "critical" || row.Severity == "broken"
	default:
		return false
	}
}

func strPtr(v string) *string {
	return &v
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
