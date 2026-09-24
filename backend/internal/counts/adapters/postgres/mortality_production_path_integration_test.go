package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// THE MORTALITY RATE ON THE PRODUCTION WRITE PATHS (maintainer decision 2026-09-24). The other
// mortality tests write the history rows by hand; this one lets the code the app runs write
// them, so a drift between what the writers record and what the read expects fails here:
//
//   - the death and the sales go through identity's ExitGoat, the write behind Record death and
//     the sale-exit;
//   - the shift goes through Counts' record -> Park Head approval -> operator completion, which
//     relocates the animals, stamps the destination pen's stage, and writes goat.stage_changed
//     and goat_location_history the way it does in the field.
//
// Only the INPUT facts are seeded: the animals, their pen and stage, and the destination pen's
// configured stage. Every history row the rate is read from is produced by production code.
//
// F2 holds 30 animals in CPT Shed 1. One dies, five are sold, fifteen are shifted into a pen
// configured as Fattening. The old rule read F2 as 1 death over the 9 still standing there
// (11.1%); the rule is 1 of the 30 that were there (3.3%), and CPT Shed 1 the same.
func TestMortalityRateOnTheProductionWritePaths(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	identity := identitypg.NewRepository(pool, 10*time.Second)

	goats := make([]string, 30)
	for i := range goats {
		goats[i] = fmt.Sprintf("00000000-0000-4000-8000-0000000e%04d", i)
		seedApprovalGoatWithStage(t, ctx, pool, goats[i], countsShedA, "F2-Female")
	}
	seedShedProfile(t, ctx, pool, countsShedB, "Fattening")

	now := time.Now().In(biztime.DefaultLocation())
	exit := func(goatID, lifecycle, reason, key string) {
		t.Helper()
		var rowVersion int
		if err := pool.QueryRow(ctx, `SELECT row_version FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
			countsTenant, goatID).Scan(&rowVersion); err != nil {
			t.Fatalf("row version %s: %v", goatID, err)
		}
		if _, err := identity.ExitGoat(ctx, identityports.ExitGoatCommand{
			TenantID:             countsTenant,
			ActorID:              countsOperator,
			ClientIdempotencyKey: key,
			StoredIdempotencyKey: key,
			IdempotencyScope:     "goat.exit",
			RequestHash:          "hash-" + key,
			GoatID:               goatID,
			LifecycleStatus:      lifecycle,
			ExitReason:           reason,
			Reason:               "mortality production-path test",
			OccurredAt:           now,
			RowVersion:           rowVersion,
			GuardrailApproved:    true,
		}); err != nil {
			t.Fatalf("exit %s as %s: %v", goatID, reason, err)
		}
	}
	exit(goats[0], "dead", "died", "mort-e2e-death")
	for i := 1; i <= 5; i++ {
		exit(goats[i], "sold", "sold", fmt.Sprintf("mort-e2e-sale-%d", i))
	}

	// The raise snapshots the destination stage the completion stamps (the service resolves it
	// from the destination pen at raise time); this repository-level raise sets it the same way.
	moving := goats[6:21]
	event := shiftingEventForApproval("mort-e2e-shift")
	event.TargetManagementStage = "Fattening"
	shiftingEventID, _, err := repo.RecordShiftingEvent(ctx, event)
	if err != nil {
		t.Fatalf("record shifting event: %v", err)
	}
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   shiftingEventID,
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"goat_ids":            moving,
	})
	if err != nil {
		t.Fatalf("marshal shifting payload: %v", err)
	}
	approval, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &shiftingEventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           now,
		IdempotencyKey:     "submit-mort-e2e-shift",
		RequestFingerprint: "submit-fp-mort-e2e-shift",
	})
	if err != nil {
		t.Fatalf("submit shifting approval: %v", err)
	}
	approvalRequestID := approval.ApprovalRequestID
	if _, _, err := approveShifting(repo, ctx, "mort-e2e-shift", approvalRequestID, shiftingEventID, moving); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, _, err := completeShifting(repo, ctx, "mort-e2e-shift", shiftingEventID); err != nil {
		t.Fatalf("complete shifting: %v", err)
	}
	// The writers really did write the history the read depends on.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM goat_identity_events WHERE tenant_id = $1::uuid AND event_type = 'goat.stage_changed'`, countsTenant); got != 15 {
		t.Fatalf("stage_changed events=%d, want 15 from the completed shift", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM goat_location_history WHERE tenant_id = $1::uuid AND from_location_id = $2::uuid`, countsTenant, countsShedA); got != 15 {
		t.Fatalf("location history rows out of CPT Shed 1=%d, want 15 from the completed shift", got)
	}

	today := now.Format("2006-01-02")
	mort, err := repo.GetMortality(ctx, domain.MortalityQuery{TenantID: countsTenant, FromDate: monthsBack(0), ToDate: today})
	if err != nil {
		t.Fatalf("mortality: %v", err)
	}
	if mort.Totals.Deaths != 1 || mort.Totals.Animals != 30 {
		t.Fatalf("totals %+v, want 1 death of 30", mort.Totals)
	}
	f2, ok := stageBucket(mort, "F2-Female")
	if !ok || f2.Deaths != 1 || f2.Animals != 30 || f2.RatePct == nil || *f2.RatePct != 3.3 {
		t.Fatalf("F2-Female %+v, want 1 death of 30 = 3.3%% (the old rule read 1 of 9)", f2)
	}
	fat, ok := stageBucket(mort, "Fattening")
	if !ok || fat.Deaths != 0 || fat.Animals != 15 {
		t.Fatalf("Fattening %+v, want 0 deaths of the 15 shifted in", fat)
	}
	pen, ok := penBucket(mort, "CPT Shed 1")
	if !ok || pen.Deaths != 1 || pen.Animals != 30 || pen.RatePct == nil || *pen.RatePct != 3.3 {
		t.Fatalf("CPT Shed 1 %+v in %+v, want 1 death of 30 = 3.3%%", pen, mort.Pen)
	}
	assertDeathsNeverExceedAnimals(t, mort)
}
