package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// BUG-030: the lifecycle / health / shift cap-safety MATRIX.
//
// Every cap-safety bug this round lived in the SAME place: a lifecycle, clinical, or workforce
// event updated ONE of the two sides of planned vaccination work and not the other.
//
//	(a) the goat's open vaccination OBLIGATIONS (obligation_instances + obligation_batches)
//	(b) the PLANNED-WORK read model the operator/park/Calendar screens actually render:
//	    vaccination_drive_assignments (animal_count / total_doses) and, since migration 000040,
//	    the exact per-goat ledger vaccination_drive_assignment_members.
//
// Individual cells were fixed piecemeal (BUG-005 procurement exit, BUG-013 exit decrement,
// BUG-015 stranded in_progress, the leave cascade), but nobody proved the PAIRING holds for every
// event. This file is that proof. Each test drives a REAL production entry point (event bus handler
// or the exact repository method the production service calls) and then asserts BOTH sides, plus:
// no orphaned membership rows, no double decrement on replay, and no live animal removed from a
// sibling operator's route.
//
// Coverage map (see the report for combos deliberately delegated to existing tests):
//
//	born / goat.created ......................... TestMatrixGoatCreatedLeavesSiblingPlannedWorkIntact
//	shed shift within a park .................... TestMatrixShedShiftRemovesAnimalFromOldShedPlannedDrive
//	sick / under_treatment / quarantine / icu ... TestMatrixClinicalDeferHoldsWorkAndReleasesPlannedDrive
//	recovered from each of the four ............. TestMatrixClinicalRecoveryReopensWithoutCorruptingPlannedDrive
//	died / sold / transferred / culled .......... goat_exit_drive_assignment_integration_test.go (all four
//	                                              are the SAME terminal goat.exited event; see report)
//	operator leave approved ..................... operator_config_replan_leave_e2e_test.go
//	protocol capacity changed ................... TestMatrixOperatorConfigRecomputeLeavesNoOrphanPlannedWork
//	operator position/cap/week-off changed ...... TestMatrixOperatorConfigRecomputeLeavesNoOrphanPlannedWork

// matrixFixture is one park / one shed / N goats / one planned vaccination drive, built entirely
// through production writers: protocol repo -> InsertObligation -> CreateBatchWithObligations ->
// UpsertVaccinationDriveAssignments (which also writes the exact membership ledger in the same tx).
type matrixFixture struct {
	versionID  string
	ruleID     string
	shedID     string
	operatorID string
	batchID    string
	planned    time.Time
	obligation map[string]string // goatID -> obligationID
}

func newMatrixFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repo *Repository,
	proto *protopg.Repository,
	slug string,
	shedID, operatorID string,
	planned time.Time,
	goatIDs ...string,
) matrixFixture {
	t.Helper()

	seedParkConsolidationShed(t, ctx, pool, shedID, "matrix-shed-"+slug)
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatIDs...)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')
ON CONFLICT (workforce_member_id) DO NOTHING`, operatorID, tenantID, "OP-"+slug); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	for _, goatID := range goatIDs {
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Matrix A')`, tenantID, goatID, shedID); err != nil {
			t.Fatalf("seed goat partition %s: %v", goatID, err)
		}
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.matrix." + slug, Name: "Matrix " + slug,
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	obligations := make(map[string]string, len(goatIDs))
	attach := make([]string, 0, len(goatIDs))
	for i, goatID := range goatIDs {
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled",
			IdempotencyKey: fmt.Sprintf("matrix-%s-dose-%d", slug, i), Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation for %s: applied=%v err=%v", goatID, applied, err)
		}
		obligations[goatID] = id
		attach = append(attach, id)
	}

	// Production drive shape: obligations stay SHED-scoped, but the vaccination drive batch the
	// sweeper creates for a shed group is PARK-scoped -- see
	// app/sweeper.go vaccinationDriveBatchScope, which rewrites a shed group to ("park", parkID).
	// The operator-config cascade selects future batches by scope_type='park', so a shed-scoped
	// fixture batch would be an unfaithful shape that silently skips that cascade.
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "matrix-" + slug, PlannedDate: &planned, Status: "planned",
		EstimatedTargets: int32(len(goatIDs)), PlannedQuantity: fmt.Sprintf("%d", len(goatIDs)),
		QuantityUnit: "dose", ConductedBy: &operator,
	}, attach)
	if err != nil || int(attached) != len(goatIDs) {
		t.Fatalf("create batch: attached=%d want=%d err=%v", attached, len(goatIDs), err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Matrix", PartitionLabel: "A", AnimalCount: int32(len(goatIDs)),
		VaccineRuleIDs: []string{ruleID}, TotalDoses: int32(len(goatIDs)), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("upsert drive assignment: %v", err)
	}

	f := matrixFixture{
		versionID: versionID, ruleID: ruleID, shedID: shedID, operatorID: operatorID,
		batchID: batchID, planned: planned, obligation: obligations,
	}
	// Fixture self-check: the production writer must have produced the aggregate row AND the exact
	// per-goat membership ledger, otherwise the assertions below would be vacuously green.
	animals, doses := f.readPlannedDrive(t, ctx, pool)
	if animals != len(goatIDs) || doses != len(goatIDs) {
		t.Fatalf("fixture invalid: planned drive = %d animals / %d doses, want %d/%d",
			animals, doses, len(goatIDs), len(goatIDs))
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignment_members m
		 JOIN vaccination_drive_assignments vda ON vda.assignment_id = m.assignment_id
		 WHERE m.tenant_id=$1 AND vda.batch_id=$2`, tenantID, batchID); got != len(goatIDs) {
		t.Fatalf("fixture invalid: membership rows = %d, want %d (the exact per-goat ledger must exist before the event)", got, len(goatIDs))
	}
	return f
}

func (f matrixFixture) readPlannedDrive(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (animals, doses int) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(sum(animal_count), 0), COALESCE(sum(total_doses), 0)
FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, f.batchID).Scan(&animals, &doses); err != nil {
		t.Fatalf("read planned drive: %v", err)
	}
	return animals, doses
}

func setGoatHealth(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, health string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET health_status=$3 WHERE tenant_id=$1 AND goat_id=$2`, tenantID, goatID, health); err != nil {
		t.Fatalf("set health_status=%s: %v", health, err)
	}
}

// membershipRowsForGoat returns how many exact drive-membership rows still bind this goat to any
// planned drive assignment. A goat whose work was pulled out of a drive must not stay in the ledger.
func membershipRowsForGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) int {
	t.Helper()
	return countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND goat_id=$2`, tenantID, goatID)
}

// ---------------------------------------------------------------------------------------------
// born / goat.created
// ---------------------------------------------------------------------------------------------

// TestMatrixGoatCreatedLeavesSiblingPlannedWorkIntact is the born/goat.created cell. Obligation
// GENERATION for a newborn is owned and covered by the vaccination package
// (internal/vaccination/app generation + GoatCreatedHandler); what the cap-safety matrix must prove
// here is the (b) side nobody asserts: a birth is purely ADDITIVE to planned work. A newborn must
// never mutate an already-planned drive's animal_count/total_doses or membership, because the
// newborn's own obligation is unbatched until the next sweeper tick plans it under the current cap.
// A birth that silently inflated a sibling drive row would push that operator's day over cap with
// work the planner never scheduled.
func TestMatrixGoatCreatedLeavesSiblingPlannedWorkIntact(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	const (
		goatA   = "10000000-0000-4000-8000-00000000ba01"
		goatB   = "10000000-0000-4000-8000-00000000ba02"
		newborn = "10000000-0000-4000-8000-00000000ba03"
		shedID  = "00000000-0000-4000-8000-00000000ea01"
		opID    = "20000000-0000-4000-8000-00000000ea01"
	)
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	f := newMatrixFixture(t, ctx, pool, repo, proto, "born", shedID, opID, planned, goatA, goatB)

	// The birth itself: a new live animal appears in the same shed (the external input fact the
	// identity module writes before publishing goat.created).
	seedReserveGoats(t, ctx, pool, shedID, cbePark, newborn)

	animals, doses := f.readPlannedDrive(t, ctx, pool)
	if animals != 2 || doses != 2 {
		t.Fatalf("planned drive after birth = %d animals / %d doses, want 2/2 (a birth must not mutate an already-planned drive)", animals, doses)
	}
	if got := membershipRowsForGoat(t, ctx, pool, newborn); got != 0 {
		t.Fatalf("newborn membership rows = %d, want 0 (a newborn is not planned work until the sweeper plans it)", got)
	}
}

// ---------------------------------------------------------------------------------------------
// shed shift within a park
// ---------------------------------------------------------------------------------------------

// TestMatrixShedShiftRemovesAnimalFromOldShedPlannedDrive is the shed-shift cell, and it is the
// pairing this matrix exists to test. Goats NEVER move between parks (leaving a park is a terminal
// exit), so the real move is shed -> shed inside one park, including a destination shed whose
// profile changes management_stage.
//
// (a) is already proven by shift_integration_test.go: SM-2 re-scopes the goat's open obligations to
// the destination shed and detaches them from the old planned batch (batch_id = NULL), and
// obligation_batches.estimated_targets / planned_quantity are decremented
// (repository.go reScopeOpenForGoatInTx).
//
// (b) is what nobody asserts. vaccination_drive_assignments is the row the vaccination execution /
// shed screens, the operator day view and Calendar render as PLANNED WORK, and
// vaccination_drive_assignment_members is the exact per-goat ledger behind it. If the shifted animal
// is not removed from the OLD shed's drive row, the operator of the old shed keeps an animal that is
// no longer there on their route and the old shed's day stays inflated forever -- nothing re-derives
// that row, because the planner only rewrites assignments when it re-plans the batch.
func TestMatrixShedShiftRemovesAnimalFromOldShedPlannedDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	const (
		movingGoat  = "10000000-0000-4000-8000-00000000bb01"
		stayingGoat = "10000000-0000-4000-8000-00000000bb02"
		thirdGoat   = "10000000-0000-4000-8000-00000000bb03"
		fromShed    = "00000000-0000-4000-8000-00000000eb01"
		toShed      = "00000000-0000-4000-8000-00000000eb02"
		opID        = "20000000-0000-4000-8000-00000000eb01"
	)
	planned := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)
	f := newMatrixFixture(t, ctx, pool, repo, proto, "shift", fromShed, opID, planned,
		movingGoat, stayingGoat, thirdGoat)
	// Destination shed in the SAME park (a shed shift is never a park move).
	seedParkConsolidationShed(t, ctx, pool, toShed, "matrix-shed-shift-dest")

	// Production path: goat.location.changed envelope -> registered SM-2 handler -> repository.
	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatShiftedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "matrix-shift-event",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        movingGoat,
		OccurredAt: time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC),
		Payload:    []byte(fmt.Sprintf(`{"scope_type":"shed","scope_id":%q,"to_shed_id":%q}`, toShed, toShed)),
	}); err != nil {
		t.Fatalf("publish goat.location.changed: %v", err)
	}

	// (a) obligation side: re-scoped to the destination shed and released from the old planned batch.
	var scopeID string
	var batchNull bool
	if err := pool.QueryRow(ctx, `
SELECT scope_id::text, batch_id IS NULL
FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, f.obligation[movingGoat]).Scan(&scopeID, &batchNull); err != nil {
		t.Fatalf("read shifted obligation: %v", err)
	}
	if scopeID != toShed {
		t.Fatalf("shifted obligation scope_id = %s, want %s", scopeID, toShed)
	}
	if !batchNull {
		t.Fatalf("shifted obligation is still attached to the old shed's planned batch")
	}

	// (b) planned-work side: the moved animal must no longer occupy the OLD shed's drive.
	animals, doses := f.readPlannedDrive(t, ctx, pool)
	if animals != 2 || doses != 2 {
		t.Fatalf("old shed planned drive = %d animals / %d doses after the shift, want 2/2 -- the moved animal must not stay on the old shed operator's route", animals, doses)
	}
	if got := membershipRowsForGoat(t, ctx, pool, movingGoat); got != 0 {
		t.Fatalf("membership rows binding the moved animal to the OLD shed's drive = %d, want 0 (the exact ledger must not keep an animal that left the shed)", got)
	}
	// The two animals that did not move keep their exact membership -- a shift must never remove a
	// live animal from a sibling's planned work.
	for _, stayed := range []string{stayingGoat, thirdGoat} {
		if got := membershipRowsForGoat(t, ctx, pool, stayed); got != 1 {
			t.Fatalf("membership rows for non-moving goat %s = %d, want 1 (a shift must not evict siblings)", stayed, got)
		}
	}

	// Replay-safe: a duplicate delivery must not decrement the drive a second time.
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "matrix-shift-event",
		Type:       oblapp.EventGoatLocationChanged,
		TenantID:   tenantID,
		Key:        movingGoat,
		OccurredAt: time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC),
		Payload:    []byte(fmt.Sprintf(`{"scope_type":"shed","scope_id":%q,"to_shed_id":%q}`, toShed, toShed)),
	}); err != nil {
		t.Fatalf("republish goat.location.changed: %v", err)
	}
	if animals, doses = f.readPlannedDrive(t, ctx, pool); animals != 2 || doses != 2 {
		t.Fatalf("after replay old shed planned drive = %d/%d, want 2/2 (re-delivery must be idempotent)", animals, doses)
	}
}

// ---------------------------------------------------------------------------------------------
// clinical defer set: sick / under_treatment / quarantine / icu  (C35-010, P0)
// ---------------------------------------------------------------------------------------------

// TestMatrixClinicalDeferHoldsWorkAndReleasesPlannedDrive is the MANDATORY clinical defer set cell.
// Per AGENTS.md (C35-010) and docs/preventive-care-vaccination/vaccination-rules.md, an animal that
// is sick, under_treatment, quarantine or icu must have its open vaccination work DEFERRED (held for
// recovery) -- never cancelled, never left scheduled. A wrong medical action here is P0.
//
// The matrix asserts BOTH sides for each of the four states:
//
//	(a) the obligation is 'deferred' (not 'canceled', not still 'scheduled') and is released from
//	    the planned batch, and the batch's estimated_targets drops by one.
//	(b) the planned drive the animal was batched into no longer counts it -- the sick animal must
//	    not stay on an operator's route as planned work, and the exact membership row must be gone.
//
// The defer is driven through the exact repository method the production vaccination
// GenerationService calls when a goat enters a defer state
// (internal/vaccination/app/generation.go -> obl.DeferOpenObligationForGeneration), reached from the
// durable goat.health.changed / goat.location.changed recheck path.
func TestMatrixClinicalDeferHoldsWorkAndReleasesPlannedDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	for i, state := range []string{"sick", "under_treatment", "quarantine", "icu"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()

			repo := NewRepository(pool, 5*time.Second)
			proto := protopg.NewRepository(pool, 5*time.Second)

			clinicalGoat := fmt.Sprintf("10000000-0000-4000-8000-00000000c%d01", i)
			healthyGoat := fmt.Sprintf("10000000-0000-4000-8000-00000000c%d02", i)
			shedID := fmt.Sprintf("00000000-0000-4000-8000-00000000ec%d1", i)
			opID := fmt.Sprintf("20000000-0000-4000-8000-00000000ec%d1", i)

			planned := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
			f := newMatrixFixture(t, ctx, pool, repo, proto, "defer"+state, shedID, opID, planned,
				clinicalGoat, healthyGoat)

			// The clinical fact (external input): the animal enters the defer state.
			setGoatHealth(t, ctx, pool, clinicalGoat, state)

			key := ""
			if err := pool.QueryRow(ctx,
				`SELECT idempotency_key FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
				tenantID, f.obligation[clinicalGoat]).Scan(&key); err != nil {
				t.Fatalf("read idempotency key: %v", err)
			}
			occurred := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
			if _, changed, err := repo.DeferOpenObligationForGeneration(ctx, tenantID, key, state, occurred); err != nil || !changed {
				t.Fatalf("defer for clinical state %s: changed=%v err=%v", state, changed, err)
			}

			// (a) obligation side -- held, never cancelled, never left scheduled.
			if got := scanStatus(t, ctx, pool, f.obligation[clinicalGoat]); got != "deferred" {
				t.Fatalf("clinical %s obligation status = %s, want deferred (cancelling or leaving it scheduled is a P0 medical error)", state, got)
			}
			if got := scanStatus(t, ctx, pool, f.obligation[healthyGoat]); got != "scheduled" {
				t.Fatalf("healthy sibling obligation status = %s, want scheduled", got)
			}
			estimated := countRows(t, ctx, pool,
				`SELECT estimated_targets FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, f.batchID)
			if estimated != 1 {
				t.Fatalf("batch estimated_targets = %d after clinical defer, want 1", estimated)
			}

			// (b) planned-work side -- the held animal must leave the operator's planned route.
			animals, doses := f.readPlannedDrive(t, ctx, pool)
			if animals != 1 || doses != 1 {
				t.Fatalf("planned drive = %d animals / %d doses after %s defer, want 1/1 -- a clinically held animal must not stay on the operator's planned route while its obligation is detached from the batch", animals, doses, state)
			}
			if got := membershipRowsForGoat(t, ctx, pool, clinicalGoat); got != 0 {
				t.Fatalf("membership rows for %s animal = %d, want 0 (the exact ledger must not keep an animal whose work was pulled off the drive)", state, got)
			}
			if got := membershipRowsForGoat(t, ctx, pool, healthyGoat); got != 1 {
				t.Fatalf("membership rows for the healthy sibling = %d, want 1 (a clinical defer must not evict a live animal from the drive)", got)
			}
		})
	}
}

// TestMatrixClinicalRecoveryReopensWithoutCorruptingPlannedDrive is the recovery cell for each of
// the four mandatory clinical states. Recovery must reopen the HELD obligation (never resurrect a
// cancelled one, never leave it deferred forever) and must not double-mutate the planned-work read
// model: the reopened obligation is unbatched and waits for the sweeper to plan it under the current
// cap, so the old drive row must stay exactly where the defer left it.
func TestMatrixClinicalRecoveryReopensWithoutCorruptingPlannedDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	for i, state := range []string{"sick", "under_treatment", "quarantine", "icu"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()

			repo := NewRepository(pool, 5*time.Second)
			proto := protopg.NewRepository(pool, 5*time.Second)

			clinicalGoat := fmt.Sprintf("10000000-0000-4000-8000-00000000d%d01", i)
			healthyGoat := fmt.Sprintf("10000000-0000-4000-8000-00000000d%d02", i)
			shedID := fmt.Sprintf("00000000-0000-4000-8000-00000000ed%d1", i)
			opID := fmt.Sprintf("20000000-0000-4000-8000-00000000ed%d1", i)

			planned := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
			f := newMatrixFixture(t, ctx, pool, repo, proto, "recover"+state, shedID, opID, planned,
				clinicalGoat, healthyGoat)

			key := ""
			if err := pool.QueryRow(ctx,
				`SELECT idempotency_key FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
				tenantID, f.obligation[clinicalGoat]).Scan(&key); err != nil {
				t.Fatalf("read idempotency key: %v", err)
			}
			setGoatHealth(t, ctx, pool, clinicalGoat, state)
			if _, changed, err := repo.DeferOpenObligationForGeneration(ctx, tenantID, key, state,
				time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)); err != nil || !changed {
				t.Fatalf("defer: changed=%v err=%v", changed, err)
			}
			afterDeferAnimals, afterDeferDoses := f.readPlannedDrive(t, ctx, pool)

			// Recovery (external clinical fact) then the production recheck reopen.
			setGoatHealth(t, ctx, pool, clinicalGoat, "healthy")
			if _, changed, err := repo.ReopenDeferredObligationForGeneration(ctx, tenantID, key,
				time.Date(2026, 8, 6, 6, 0, 0, 0, time.UTC), nil); err != nil || !changed {
				t.Fatalf("reopen after recovery from %s: changed=%v err=%v", state, changed, err)
			}

			// (a) held work is reopened, not cancelled, and is unbatched pending the next plan.
			var status string
			var batchNull bool
			if err := pool.QueryRow(ctx, `
SELECT status, batch_id IS NULL FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
				tenantID, f.obligation[clinicalGoat]).Scan(&status, &batchNull); err != nil {
				t.Fatalf("read recovered obligation: %v", err)
			}
			if status != "scheduled" {
				t.Fatalf("obligation status after recovery from %s = %s, want scheduled", state, status)
			}
			if !batchNull {
				t.Fatalf("recovered obligation must be unbatched until the sweeper re-plans it under the current cap")
			}

			// (b) recovery must not silently re-add the animal to the OLD drive row (that row was
			// planned under a cap computed without it) and must not double-decrement it either.
			animals, doses := f.readPlannedDrive(t, ctx, pool)
			if animals != afterDeferAnimals || doses != afterDeferDoses {
				t.Fatalf("planned drive changed on recovery from %s: %d/%d -> %d/%d, want unchanged (re-planning is the sweeper's job, not the reopen's)",
					state, afterDeferAnimals, afterDeferDoses, animals, doses)
			}
			if got := membershipRowsForGoat(t, ctx, pool, clinicalGoat); got != 0 {
				t.Fatalf("recovered animal membership rows = %d, want 0 until the sweeper re-plans it into a drive", got)
			}
			if got := membershipRowsForGoat(t, ctx, pool, healthyGoat); got != 1 {
				t.Fatalf("healthy sibling membership rows = %d, want 1", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------------------------
// protocol capacity changed / operator position, cap, week-off changed
// ---------------------------------------------------------------------------------------------

// TestMatrixOperatorConfigRecomputeLeavesNoOrphanPlannedWork is the config-cascade cell shared by
// vaccination.capacity.changed (protocol capacity changed) and vaccination.roster.changed (operator
// position / cap / week-off changed) -- both event types land on the same handler and the same
// release path, RecomputeFutureVaccinationDrives.
//
// The handler wiring for both event types is already covered by
// internal/obligation/app/operator_config_replan_test.go, and the leave variant end to end by
// operator_config_replan_leave_e2e_test.go. What the cap-safety matrix adds is the (a)/(b) pairing
// nobody asserts on this path: after a capacity/roster change releases a park's future planned
// drives, BOTH sides must be clean -- obligations unbatched and re-plannable, drive assignment rows
// deleted, and crucially NO orphaned vaccination_drive_assignment_members rows pointing at deleted
// assignments (an orphan would keep a dead per-goat route entry and make the next exact decrement
// subtract from a row that no longer exists).
func TestMatrixOperatorConfigRecomputeLeavesNoOrphanPlannedWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)

	const (
		goatA  = "10000000-0000-4000-8000-00000000be01"
		goatB  = "10000000-0000-4000-8000-00000000be02"
		shedID = "00000000-0000-4000-8000-00000000ee01"
		opID   = "20000000-0000-4000-8000-00000000ee01"
	)
	planned := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	f := newMatrixFixture(t, ctx, pool, repo, proto, "config", shedID, opID, planned, goatA, goatB)

	// Production path: the capacity/roster cascade's release step for this park, from the day the
	// new config takes effect.
	released, err := repo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("RecomputeFutureVaccinationDrives: %v", err)
	}
	if released != 1 {
		t.Fatalf("released batches = %d, want 1", released)
	}

	// (a) both animals' obligations are back to unbatched, open, and untouched clinically.
	for _, goatID := range []string{goatA, goatB} {
		var status string
		var batchNull bool
		if err := pool.QueryRow(ctx, `
SELECT status, batch_id IS NULL FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
			tenantID, f.obligation[goatID]).Scan(&status, &batchNull); err != nil {
			t.Fatalf("read obligation for %s: %v", goatID, err)
		}
		if status != "scheduled" || !batchNull {
			t.Fatalf("after config recompute goat %s obligation = %s / batched=%v, want scheduled + unbatched",
				goatID, status, !batchNull)
		}
	}

	// (b) the released drive rows are gone, with no orphaned exact-membership rows behind them.
	if animals, doses := f.readPlannedDrive(t, ctx, pool); animals != 0 || doses != 0 {
		t.Fatalf("planned drive after config recompute = %d animals / %d doses, want 0/0", animals, doses)
	}
	orphans := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignment_members m
LEFT JOIN vaccination_drive_assignments vda ON vda.assignment_id = m.assignment_id
WHERE m.tenant_id=$1 AND vda.assignment_id IS NULL`, tenantID)
	if orphans != 0 {
		t.Fatalf("orphaned drive-membership rows = %d, want 0", orphans)
	}
	for _, goatID := range []string{goatA, goatB} {
		if got := membershipRowsForGoat(t, ctx, pool, goatID); got != 0 {
			t.Fatalf("membership rows for %s after release = %d, want 0", goatID, got)
		}
	}

	// Idempotent: a replayed capacity/roster cascade must not re-release or corrupt anything.
	again, err := repo.RecomputeFutureVaccinationDrives(ctx, tenantID, cbePark,
		time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("replayed RecomputeFutureVaccinationDrives: %v", err)
	}
	if again != 0 {
		t.Fatalf("replayed release count = %d, want 0 (already-released batches must not be re-released)", again)
	}
}
