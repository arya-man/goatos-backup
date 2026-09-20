package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// TestPurchaseLoadOpensAnAnimallessWorkflowKeyedOnTheLoad pins the opener's half of the contract
// (PROCUREMENT IS SOP-DRIVEN END TO END, 2026-09-20): a purchase load's intake workflow carries
// NO animal, is keyed on the load, runs in the load's own park, and a redelivered event opens
// nothing.
func TestPurchaseLoadOpensAnAnimallessWorkflowKeyedOnTheLoad(t *testing.T) {
	repo := &openRecorder{fakeRepo: newFakeRepo(), byRef: map[string]string{}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }
	payload, _ := json.Marshal(map[string]any{"load_id": "load-1", "park_id": "park-1"})
	ev := eventbus.Event{Type: EventAnimalPurchaseLoadRecorded, TenantID: "tenant", Key: "load-1", Payload: payload}
	if err := NewAnimalPurchaseLoadRecordedWorkflowHandler(svc).HandleEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(repo.opened) != 1 {
		t.Fatalf("opened %d workflows, want 1", len(repo.opened))
	}
	cmd := repo.opened[0]
	if cmd.TemplateKey != domain.TemplateKeyAnimalPurchaseIntake {
		t.Fatalf("template key = %q", cmd.TemplateKey)
	}
	if cmd.SubjectRefID == nil || *cmd.SubjectRefID != "load-1" {
		t.Fatalf("subject ref = %v", cmd.SubjectRefID)
	}
	if cmd.SubjectGoatID != "" {
		t.Fatalf("intake workflow must carry no animal, got %q", cmd.SubjectGoatID)
	}
	if cmd.ParkID == nil || *cmd.ParkID != "park-1" {
		t.Fatalf("park = %v", cmd.ParkID)
	}
	// Redelivery: the workflow is found by its subject ref and nothing is opened twice.
	if err := NewAnimalPurchaseLoadRecordedWorkflowHandler(svc).HandleEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}
	if len(repo.opened) != 1 {
		t.Fatalf("redelivery opened %d workflows, want 1", len(repo.opened))
	}
}

// TestDecisionStepWaitsForTheLastAnimal is the reason the step is the ENGINE's and not a tap: a
// load must never read "decided" while an animal is still waiting, and an empty load has decided
// nothing at all. The count is the producer's own, taken inside the decision transaction.
func TestDecisionStepWaitsForTheLastAnimal(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    int
	}{
		{"one animal still waiting", map[string]any{"load_id": "load-1", "load_pending": 3, "load_accepted": 1, "load_rejected": 0}, 0},
		{"nothing decided on an empty load", map[string]any{"load_id": "load-1", "load_pending": 0, "load_accepted": 0, "load_rejected": 0}, 0},
		{"last animal decided", map[string]any{"load_id": "load-1", "load_pending": 0, "load_accepted": 4, "load_rejected": 1}, 1},
		{"every animal rejected still finishes the step", map[string]any{"load_id": "load-1", "load_pending": 0, "load_accepted": 0, "load_rejected": 2}, 1},
		{"no load on the event", map[string]any{"load_pending": 0, "load_accepted": 1}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewService(repo, nil)
			svc.now = func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }
			payload, _ := json.Marshal(tc.payload)
			err := NewAnimalPurchaseDecidedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
				Type: EventAnimalPurchaseDecided, TenantID: "tenant", Key: "candidate-1", Payload: payload,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(repo.purchaseDecisionCompletions) != tc.want {
				t.Fatalf("completions = %v, want %d", repo.purchaseDecisionCompletions, tc.want)
			}
		})
	}
}

// TestFeedPurchaseWorkflowOpensAndItsTwoEngineStepsFollowTheirOwners pins the feed half: the
// purchase opens an animal-less workflow keyed on the load, the LEDGER's delivery write completes
// the arrival step, and an ACCEPTED toxin round completes the aflatoxin step. Neither engine step
// is reachable by a tap, which is the whole reason they are hooks: the fact each records already
// has an owner elsewhere, and a tap would let the two disagree.
func TestFeedPurchaseWorkflowOpensAndItsTwoEngineStepsFollowTheirOwners(t *testing.T) {
	repo := &openRecorder{fakeRepo: newFakeRepo(), byRef: map[string]string{}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }

	recorded, _ := json.Marshal(map[string]any{"feed_purchase_id": "load-9", "park_id": "park-2"})
	if err := NewFeedPurchaseRecordedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventFeedPurchaseRecorded, TenantID: "tenant", Payload: recorded,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.opened) != 1 || repo.opened[0].TemplateKey != domain.TemplateKeyFeedPurchaseIntake {
		t.Fatalf("opened = %+v", repo.opened)
	}
	if repo.opened[0].SubjectGoatID != "" {
		t.Fatalf("feed purchase workflow must carry no animal, got %q", repo.opened[0].SubjectGoatID)
	}

	reached, _ := json.Marshal(map[string]any{"feed_purchase_id": "load-9", "reached_on": "2026-09-24"})
	if err := NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventFeedPurchaseReached, TenantID: "tenant", Payload: reached,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.feedReachedCompletions) != 1 || repo.feedReachedCompletions[0] != "load-9" {
		t.Fatalf("arrival completions = %v", repo.feedReachedCompletions)
	}

	accepted, _ := json.Marshal(map[string]any{"feed_purchase_id": "load-9", "toxin_task_id": "task-1", "accepted_at": "2026-09-25T10:00:00Z"})
	if err := NewToxinTestAcceptedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventToxinTestAccepted, TenantID: "tenant", Payload: accepted,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.toxinStepCompletions) != 1 || repo.toxinStepCompletions[0] != "load-9" {
		t.Fatalf("aflatoxin completions = %v", repo.toxinStepCompletions)
	}

	// An event naming no load is ignored rather than guessed at: the aflatoxin step of SOME load
	// is not a thing the engine may pick.
	blank, _ := json.Marshal(map[string]any{"toxin_task_id": "task-2"})
	if err := NewToxinTestAcceptedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventToxinTestAccepted, TenantID: "tenant", Key: "task-2", Payload: blank,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.toxinStepCompletions) != 1 {
		t.Fatalf("a load-less event completed something: %v", repo.toxinStepCompletions)
	}
}

func TestFeedPurchaseRecordedReconcilesAlreadyReachedLoad(t *testing.T) {
	repo := &openRecorder{fakeRepo: newFakeRepo(), byRef: map[string]string{}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }

	// A same-transaction reached event can be claimed before the recorded event because the outbox
	// tie-breaker is a random UUID. When that happens the reached handler sees no workflow and acks
	// the event, so the recorded handler must heal the arrival step once it opens the workflow.
	reached, _ := json.Marshal(map[string]any{"feed_purchase_id": "load-10", "reached_on": "2026-09-24"})
	if err := NewFeedPurchaseReachedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventFeedPurchaseReached, TenantID: "tenant", Payload: reached,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.feedReachedCompletions) != 1 {
		t.Fatalf("first reached event completions = %v", repo.feedReachedCompletions)
	}

	repo.feedReachedCompletions = nil
	recorded, _ := json.Marshal(map[string]any{"feed_purchase_id": "load-10", "park_id": "park-2", "reached_on": "2026-09-24"})
	if err := NewFeedPurchaseRecordedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventFeedPurchaseRecorded, TenantID: "tenant", Payload: recorded,
	}); err != nil {
		t.Fatal(err)
	}
	if len(repo.opened) != 1 {
		t.Fatalf("opened = %+v", repo.opened)
	}
	if len(repo.feedReachedCompletions) != 1 || repo.feedReachedCompletions[0] != "load-10" {
		t.Fatalf("reconciled arrival completions = %v", repo.feedReachedCompletions)
	}
}
