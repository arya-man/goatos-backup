package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// A ROUND-grain feed & water removal reaches the verifier as ONE ITEM PER PEN (maintainer
// decision 2026-09-05). The card is one evening's job, but a single clip stretched over four
// pens proves nothing and the verifier cannot tell which pen was actually emptied — so the
// review grain follows the evidence, exactly as weighing corrected its own fasting card on
// 2026-09-03.
func TestRoundRemovalEnqueuesOneVerifierItemPerPen(t *testing.T) {
	enq := &fakeEnqueuer{}
	handler := NewPCCarePendingVerificationHandler(enq, nil)
	payload, _ := json.Marshal(map[string]any{
		"task_id":               "removal-task-1",
		"category":              domain.CategoryFeedWaterRemoval,
		"park_id":               fastingPark,
		"planned_business_date": "2026-09-10",
		"row_version":           2,
		"operator_id":           testAssignee,
		"removal_pens": []map[string]any{
			{
				"removal_pen_id": "pen-a", "gated_task_id": "task-a", "pen_label": "Godel 1 - Part 3",
				"feed_proof_ref": "feed-a", "water_proof_ref": "water-a", "row_version": 3,
			},
			{
				"removal_pen_id": "pen-b", "gated_task_id": "task-b", "pen_label": "Castro 2",
				"feed_proof_ref": "feed-b", "water_proof_ref": "water-b", "row_version": 1,
			},
		},
	})

	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type:       "pc_care.task.pending_verification",
		TenantID:   testTenant,
		OccurredAt: time.Unix(0, 0),
		Payload:    payload,
	}); err != nil {
		t.Fatalf("pending verification: %v", err)
	}

	if enq.calls != 2 {
		t.Fatalf("verifier items = %d, want one per pen (2)", enq.calls)
	}
	// Each pen's key is that PEN's own row version, so a re-shot pen mints a fresh item while
	// the pens already approved are never re-queued.
	wantKeys := map[string]bool{"pc-care-removal-pen:pen-a:3": false, "pc-care-removal-pen:pen-b:1": false}
	for _, key := range enq.lastKeys {
		if _, ok := wantKeys[key]; !ok {
			t.Fatalf("unexpected idempotency key %q", key)
		}
		wantKeys[key] = true
	}
	for key, seen := range wantKeys {
		if !seen {
			t.Fatalf("missing idempotency key %q", key)
		}
	}
	// The LAST item is filed against its own pen and carries only that pen's two videos.
	if enq.last.RemovalPenID == "" {
		t.Fatal("a pen item must carry its removal pen id, or the verdict cannot reach the pen")
	}
	if len(enq.last.MediaRefs) != 2 {
		t.Fatalf("pen item media = %d refs, want exactly that pen's feed + water", len(enq.last.MediaRefs))
	}
	if enq.last.RemovalPenLabel == "" {
		t.Fatal("a pen item must carry its pen label; the verifier is owed which pen a clip proves")
	}
}

// Every other submit is untouched: one task, one item, no pen fan-out.
func TestOrdinarySubmitStillEnqueuesExactlyOneItem(t *testing.T) {
	enq := &fakeEnqueuer{}
	handler := NewPCCarePendingVerificationHandler(enq, nil)
	payload, _ := json.Marshal(map[string]any{
		"task_id":     testTask,
		"category":    domain.CategoryDeworming,
		"row_version": 4,
		"media_refs":  []map[string]string{{"proof_ref": "proof-1", "label": "RFID-1 · Video"}},
	})
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type: "pc_care.task.pending_verification", TenantID: testTenant, Payload: payload,
	}); err != nil {
		t.Fatalf("pending verification: %v", err)
	}
	if enq.calls != 1 {
		t.Fatalf("items = %d, want 1", enq.calls)
	}
	if enq.last.RemovalPenID != "" {
		t.Fatal("an ordinary task item must not be filed against a removal pen")
	}
}

// fakeRemovalVerdictStore records which write a verdict was routed to.
type fakeRemovalVerdictStore struct {
	approved []ports.ApplyRemovalPenVerdictParams
	bounced  []ports.ApplyRemovalPenVerdictParams
}

func (f *fakeRemovalVerdictStore) ApplyVerifiedRemovalPen(_ context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error) {
	f.approved = append(f.approved, p)
	return true, nil
}

func (f *fakeRemovalVerdictStore) BounceRemovalPenForRework(_ context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error) {
	f.bounced = append(f.bounced, p)
	return true, nil
}

// fakeTaskVerdictStore records task-grain verdicts, so a test can prove a PEN verdict never
// lands on a TASK.
type fakeTaskVerdictStore struct {
	applied int
	bounced int
}

func (f *fakeTaskVerdictStore) ApplyVerifiedTask(_ context.Context, _ ports.ApplyVerifiedTaskParams) (bool, error) {
	f.applied++
	return true, nil
}

func (f *fakeTaskVerdictStore) BounceTaskForRework(_ context.Context, _ ports.BounceTaskParams) (bool, error) {
	f.bounced++
	return true, nil
}

func verdictEvent(t *testing.T, eventType, refType, refID, reason string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"verified_by": "verifier-1",
		"reason":      reason,
		"source":      map[string]string{"module": domain.VerificationModulePCCare, "ref_type": refType, "ref_id": refID},
	})
	if err != nil {
		t.Fatalf("marshal verdict: %v", err)
	}
	return eventbus.Event{ID: "verdict-event-1", Type: eventType, TenantID: testTenant, Payload: payload}
}

// A pen's verdict lands on THAT PEN's evidence row, never on a task. Routing on ref_type is
// what keeps one item's approve from being applied to another item's record.
func TestRemovalPenVerdictLandsOnThePenAndNeverOnATask(t *testing.T) {
	tasks := &fakeTaskVerdictStore{}
	removals := &fakeRemovalVerdictStore{}
	handler := NewPCCareVerificationHandler(tasks, nil).WithRemovalStore(removals)

	if err := handler.HandleEvent(context.Background(),
		verdictEvent(t, "verification.verdict.approved", domain.VerificationRefTypeRemovalPen, "pen-a", "")); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := handler.HandleEvent(context.Background(),
		verdictEvent(t, "verification.verdict.rework", domain.VerificationRefTypeRemovalPen, "pen-b", "water not shown")); err != nil {
		t.Fatalf("rework: %v", err)
	}

	if len(removals.approved) != 1 || removals.approved[0].RemovalPenID != "pen-a" {
		t.Fatalf("approved = %+v, want exactly pen-a", removals.approved)
	}
	if len(removals.bounced) != 1 || removals.bounced[0].RemovalPenID != "pen-b" {
		t.Fatalf("bounced = %+v, want exactly pen-b", removals.bounced)
	}
	if removals.bounced[0].Reason != "water not shown" {
		t.Fatalf("rework reason = %q, want the verifier's words carried verbatim", removals.bounced[0].Reason)
	}
	if tasks.applied != 0 || tasks.bounced != 0 {
		t.Fatalf("a pen verdict reached the TASK writes: applied=%d bounced=%d", tasks.applied, tasks.bounced)
	}
}

// The mirror: a task verdict must never be applied to a pen.
func TestTaskVerdictNeverLandsOnARemovalPen(t *testing.T) {
	tasks := &fakeTaskVerdictStore{}
	removals := &fakeRemovalVerdictStore{}
	handler := NewPCCareVerificationHandler(tasks, nil).WithRemovalStore(removals)

	if err := handler.HandleEvent(context.Background(),
		verdictEvent(t, "verification.verdict.approved", domain.VerificationRefTypeTask, testTask, "")); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if tasks.applied != 1 {
		t.Fatalf("task applies = %d, want 1", tasks.applied)
	}
	if len(removals.approved) != 0 || len(removals.bounced) != 0 {
		t.Fatalf("a task verdict reached the PEN writes: %+v %+v", removals.approved, removals.bounced)
	}
}

// Another module's verdict is ignored, pen ref type or not.
func TestForeignModuleVerdictIsIgnored(t *testing.T) {
	tasks := &fakeTaskVerdictStore{}
	removals := &fakeRemovalVerdictStore{}
	handler := NewPCCareVerificationHandler(tasks, nil).WithRemovalStore(removals)

	payload, _ := json.Marshal(map[string]any{
		"verified_by": "verifier-1",
		"source":      map[string]string{"module": "weighing", "ref_type": domain.VerificationRefTypeRemovalPen, "ref_id": "pen-a"},
	})
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		Type: "verification.verdict.approved", TenantID: testTenant, Payload: payload,
	}); err != nil {
		t.Fatalf("foreign verdict: %v", err)
	}
	if tasks.applied != 0 || len(removals.approved) != 0 {
		t.Fatal("a weighing verdict was applied to pc_care state")
	}
}
