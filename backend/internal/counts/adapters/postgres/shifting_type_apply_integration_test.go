package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Typed shifting rewrite -- APPLY-side proofs on the REAL counts + identity Postgres path
// (maintainer decisions 2026-08-20, docs/features/shifting/shifting-rewrite-tag-rules.md).
//
// The raise-side rulebook is proven in counts/domain and the HTTP handler tests. What only a real
// database can prove is the SECOND GATE: pen-tag adoption writes the destination's configured
// cohort in the same transaction as the relocation and fails the whole apply closed when the pen
// changed between approval and completion; and the health-only clinical stamp is derived from the
// STORED category, so a non-health movement carrying a clinical target still refuses at apply.

// typedShiftingEvent builds a raise-shaped event row carrying the typed rewrite's snapshot fields.
func typedShiftingEvent(key, category, targetStage, adoptPenTag string) domain.ShiftingEvent {
	event := shiftingEventForApproval(key)
	event.Category = category
	event.TargetManagementStage = targetStage
	event.ManagementStageMode = "keep_current"
	if targetStage != "" {
		event.ManagementStageMode = "destination_stage"
	}
	event.AdoptPenTag = adoptPenTag
	return event
}

func recordTypedShifting(
	t *testing.T, ctx context.Context, repo *Repository, key string, goatIDs []string, event domain.ShiftingEvent,
) (shiftingEventID, approvalRequestID string) {
	t.Helper()
	shiftingEventID, _, err := repo.RecordShiftingEvent(ctx, event)
	if err != nil {
		t.Fatalf("record typed shifting event: %v", err)
	}
	// The approved payload is what completion relocates: goat_ids must ride on it, exactly as the
	// raise handler stores them.
	payload, err := json.Marshal(map[string]any{
		"shifting_event_id":   shiftingEventID,
		"destination_park_id": countsPark,
		"destination_shed_id": countsShedB,
		"goat_ids":            goatIDs,
	})
	if err != nil {
		t.Fatalf("marshal typed shifting payload: %v", err)
	}
	req, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID:           countsTenant,
		RequestType:        domain.ApprovalRequestTypeShifting,
		Payload:            payload,
		ShiftingEventID:    &shiftingEventID,
		RaisedByUserID:     countsOperator,
		RaisedAt:           time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey:     "submit-" + key,
		RequestFingerprint: "submit-fp-" + key,
	})
	if err != nil {
		t.Fatalf("submit typed shifting approval: %v", err)
	}
	return shiftingEventID, req.ApprovalRequestID
}

// seedStageVocabulary upserts an active animal_stage_lookup row WITHOUT configuring any shed
// profile -- for stages (ICU) that must exist in the vocabulary while no pen is configured with them.
func seedStageVocabulary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stageCode string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES ($1::uuid, $2, $2, 1, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE SET status = 'active'`, countsTenant, stageCode); err != nil {
		t.Fatalf("seed stage vocabulary %q: %v", stageCode, err)
	}
}

// shedProfileStage reads the shed's configured cohort (” when none), the value the typed spacing
// adoption is supposed to write.
func shedProfileStage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID string) string {
	t.Helper()
	var stage string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(a.stage_code, '')
FROM locations shed
LEFT JOIN shed_profiles p ON p.tenant_id = shed.tenant_id AND p.location_id = shed.location_id
LEFT JOIN animal_stage_lookup a
       ON a.tenant_id = shed.tenant_id AND a.animal_stage_id = p.animal_stage_id AND a.status = 'active'
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid`, countsTenant, shedID).Scan(&stage); err != nil {
		t.Fatalf("read shed profile stage: %v", err)
	}
	return stage
}

// A SPACING apply into an empty unconfigured shed adopts the group's tag onto the shed's
// configuration in the SAME transaction as the relocation, while every animal keeps its own stage.
func TestTypedSpacingApplyAdoptsPenTagOntoEmptyDestination(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000ad01"
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K2")
	seedStageVocabulary(t, ctx, pool, "K2")
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("destination shed already configured with %q before the test, want unconfigured", got)
	}

	eventID, approvalID := recordTypedShifting(t, ctx, repo, "typed-adopt",
		[]string{goatID}, typedShiftingEvent("typed-adopt", "spacing", "", "K2"))
	if _, _, err := approveShifting(repo, ctx, "typed-adopt", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	// Approval alone configures nothing: the pen adopts its tag when the movement APPLIES.
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "" {
		t.Fatalf("destination configured %q after approval alone, want unconfigured until apply", got)
	}

	completed, _, err := submitShiftingForVerification(repo, ctx, "typed-adopt", eventID, "")
	if err != nil {
		t.Fatalf("operator completion: %v", err)
	}
	if completed.EventStatus != domain.ShiftingEventStatusApplied {
		t.Fatalf("completion status=%q, want applied", completed.EventStatus)
	}
	// The DB round-trip is the assertion: the destination shed's configured cohort IS the adopted
	// tag, the animal moved, and its own stage is untouched (spacing never restamps animals).
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "K2" {
		t.Fatalf("destination configured stage=%q after apply, want K2 adopted from the group", got)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedB {
		t.Fatalf("goat shed=%s, want destination %s", got, countsShedB)
	}
	if got := goatStage(t, ctx, pool, goatID); got != "K2" {
		t.Fatalf("goat stage=%q after spacing, want K2 preserved", got)
	}
}

// The raise promised an EMPTY-or-matching pen. When the pen was configured DIFFERENTLY between
// approval and completion, the whole apply fails closed: no relocation, no completion stamps, no
// pen write -- the movement stays authorized for a human to re-raise.
func TestTypedSpacingApplyFailsClosedWhenPenChangedSinceApproval(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000ad02"
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K2")
	seedStageVocabulary(t, ctx, pool, "K2")

	eventID, approvalID := recordTypedShifting(t, ctx, repo, "typed-pen-changed",
		[]string{goatID}, typedShiftingEvent("typed-pen-changed", "spacing", "", "K2"))
	if _, _, err := approveShifting(repo, ctx, "typed-pen-changed", approvalID, eventID, []string{goatID}); err != nil {
		t.Fatalf("park-head approval: %v", err)
	}
	// Between approval and completion somebody configures the destination for a DIFFERENT cohort.
	seedShedProfile(t, ctx, pool, countsShedB, "K3")

	_, _, err := submitShiftingForVerification(repo, ctx, "typed-pen-changed", eventID, "")
	if !errors.Is(err, identityports.ErrDestinationPenChanged) {
		t.Fatalf("completion err=%v, want ErrDestinationPenChanged", err)
	}
	if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
		t.Fatalf("goat shed=%s after a failed apply, want untouched source %s", got, countsShedA)
	}
	if got := shedProfileStage(t, ctx, pool, countsShedB); got != "K3" {
		t.Fatalf("destination configured stage=%q, want the K3 the pen was given, never overwritten", got)
	}
	if got := shiftingEventStatus(t, ctx, pool, eventID); got != domain.ShiftingEventStatusAuthorized {
		t.Fatalf("event_status=%q after a failed apply, want still authorized", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid AND completed_at IS NOT NULL`,
		countsTenant, eventID); got != 0 {
		t.Fatalf("failed apply left %d completion stamp(s), want 0 -- the transaction must roll back whole", got)
	}
}

// A HEALTH movement stamps a clinical destination tag at apply (the one type allowed to), and the
// permission derives from the STORED category: an identical movement stored as growth refuses the
// same clinical target with ErrClinicalDestinationTag and applies nothing. Mutation target: making
// the apply path pass AllowClinical unconditionally turns the adversarial half red.
func TestTypedHealthApplyStampsClinicalStateAndOtherTypesRefuse(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)
	seedStageVocabulary(t, ctx, pool, "ICU")

	t.Run("health stamps ICU", func(t *testing.T) {
		goatID := "00000000-0000-4000-8000-00000000ad03"
		seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K1")
		eventID, approvalID := recordTypedShifting(t, ctx, repo, "typed-health",
			[]string{goatID}, typedShiftingEvent("typed-health", "health", "ICU", ""))
		if _, _, err := approveShifting(repo, ctx, "typed-health", approvalID, eventID, []string{goatID}); err != nil {
			t.Fatalf("park-head approval: %v", err)
		}
		completed, _, err := submitShiftingForVerification(repo, ctx, "typed-health", eventID, "")
		if err != nil {
			t.Fatalf("health completion: %v", err)
		}
		if completed.EventStatus != domain.ShiftingEventStatusApplied {
			t.Fatalf("completion status=%q, want applied", completed.EventStatus)
		}
		if got := goatStage(t, ctx, pool, goatID); got != "ICU" {
			t.Fatalf("goat stage=%q after a health shift into ICU, want ICU -- the move IS the clinical call", got)
		}
	})

	t.Run("growth with the same clinical target refuses at apply", func(t *testing.T) {
		goatID := "00000000-0000-4000-8000-00000000ad04"
		seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "K1")
		eventID, approvalID := recordTypedShifting(t, ctx, repo, "typed-growth-icu",
			[]string{goatID}, typedShiftingEvent("typed-growth-icu", "growth", "ICU", ""))
		if _, _, err := approveShifting(repo, ctx, "typed-growth-icu", approvalID, eventID, []string{goatID}); err != nil {
			t.Fatalf("park-head approval: %v", err)
		}
		_, _, err := submitShiftingForVerification(repo, ctx, "typed-growth-icu", eventID, "")
		if !errors.Is(err, identityports.ErrClinicalDestinationTag) {
			t.Fatalf("growth completion with clinical target err=%v, want ErrClinicalDestinationTag", err)
		}
		if got := goatShed(t, ctx, pool, goatID); got != countsShedA {
			t.Fatalf("goat shed=%s after refused apply, want untouched %s", got, countsShedA)
		}
		if got := goatStage(t, ctx, pool, goatID); got != "K1" {
			t.Fatalf("goat stage=%q after refused apply, want untouched K1", got)
		}
	})
}
