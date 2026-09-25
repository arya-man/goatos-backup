package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// openRecorder records what the service asked the repository to open. The fake repo compiles
// only the legacy code templates, so this test pins the SERVICE's part of the contract: a sale
// workflow is subject-keyed (no animal) and the two consumers reach the right calls.
type openRecorder struct {
	*fakeRepo
	opened []ports.OpenWorkflowCommand
	byRef  map[string]string
}

func (r *openRecorder) OpenWorkflow(_ context.Context, cmd ports.OpenWorkflowCommand) (bool, error) {
	r.opened = append(r.opened, cmd)
	r.byRef[cmd.TemplateKey+"|"+*cmd.SubjectRefID] = "wf-" + *cmd.SubjectRefID
	return true, nil
}

func (r *openRecorder) WorkflowIDBySubjectRef(_ context.Context, _, templateKey, ref string) (string, error) {
	if id, ok := r.byRef[templateKey+"|"+ref]; ok {
		return id, nil
	}
	return "", domain.ErrNotFound
}

// TestSaleRecordedOpensAGoatlessWorkflowAndTheConfirmCompletesTheTagStep: the found-live gap
// (2026-09-19, throwaway stack) was the service refusing a sale open with
// ErrMissingRequiredField because only general runs were allowed to carry no animal.
func TestSaleRecordedOpensAGoatlessWorkflowAndTheConfirmCompletesTheTagStep(t *testing.T) {
	repo := &openRecorder{fakeRepo: newFakeRepo(), byRef: map[string]string{}}
	svc := NewService(repo, nil)
	svc.now = func() time.Time { return time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC) }
	payload, _ := json.Marshal(map[string]any{"sales_deal_id": "deal-1", "park_id": "park-1"})
	err := NewSaleRecordedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
		Type: EventSalesDealRecorded, TenantID: "tenant", Key: "deal-1", Payload: payload, OccurredAt: svc.now(),
	})
	if err != nil {
		t.Fatalf("recorded handler: %v", err)
	}
	if len(repo.opened) != 1 || repo.opened[0].TemplateKey != domain.TemplateKeySalesDeal || repo.opened[0].SubjectGoatID != "" ||
		repo.opened[0].SubjectRefID == nil || *repo.opened[0].SubjectRefID != "deal-1" || repo.opened[0].ParkID == nil || *repo.opened[0].ParkID != "park-1" {
		t.Fatalf("open command = %+v", repo.opened)
	}
	// A redelivery finds the workflow by its subject ref and opens nothing.
	if err := NewSaleRecordedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{Type: EventSalesDealRecorded, TenantID: "tenant", Key: "deal-1", Payload: payload}); err != nil || len(repo.opened) != 1 {
		t.Fatalf("redelivery: err=%v opened=%d", err, len(repo.opened))
	}
	// A sale open with no deal is still refused.
	if _, err := svc.OpenSubjectWorkflow(context.Background(), OpenSubjectWorkflowInput{TenantID: "tenant", TemplateKey: domain.TemplateKeySalesDeal}); !errors.Is(err, domain.ErrMissingRequiredField) {
		t.Fatalf("no deal must be refused, got %v", err)
	}
	// The allocation confirm completes the tag step for that deal.
	confirm, _ := json.Marshal(map[string]any{"sales_deal_id": "deal-1", "allocated_at": "2026-09-19T10:00:00Z"})
	if err := NewSaleAllocatedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{Type: EventGoatSaleAllocated, TenantID: "tenant", Key: "deal-1", Payload: confirm}); err != nil {
		t.Fatalf("allocated handler: %v", err)
	}
	if len(repo.saleTagCompletions) != 1 || repo.saleTagCompletions[0] != "deal-1" {
		t.Fatalf("tag completions = %v", repo.saleTagCompletions)
	}
}

// TestSaleRecordedCarriesWhetherTheSaleHasAnimals (maintainer decision 2026-09-25): the opener
// passes the event's has_live_animals to the compile so a manure / feed sale opens without its
// tag step; an event written before the key existed opens with every step (nil = true); a sale
// recorded already failed opens nothing.
func TestSaleRecordedCarriesWhetherTheSaleHasAnimals(t *testing.T) {
	open := func(payload map[string]any) []ports.OpenWorkflowCommand {
		t.Helper()
		repo := &openRecorder{fakeRepo: newFakeRepo(), byRef: map[string]string{}}
		svc := NewService(repo, nil)
		raw, _ := json.Marshal(payload)
		if err := NewSaleRecordedWorkflowHandler(svc).HandleEvent(context.Background(), eventbus.Event{
			Type: EventSalesDealRecorded, TenantID: "tenant", Key: "deal-1", Payload: raw, OccurredAt: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("recorded handler: %v", err)
		}
		return repo.opened
	}
	manure := open(map[string]any{"sales_deal_id": "deal-1", "status": "Deal Closed", "has_live_animals": false})
	if len(manure) != 1 || manure[0].SaleHasAnimals == nil || *manure[0].SaleHasAnimals {
		t.Fatalf("manure sale must open with SaleHasAnimals=false, got %+v", manure)
	}
	animals := open(map[string]any{"sales_deal_id": "deal-1", "status": "Deal Closed", "has_live_animals": true})
	if len(animals) != 1 || animals[0].SaleHasAnimals == nil || !*animals[0].SaleHasAnimals {
		t.Fatalf("animal sale must open with SaleHasAnimals=true, got %+v", animals)
	}
	legacy := open(map[string]any{"sales_deal_id": "deal-1", "status": "Deal Closed"})
	if len(legacy) != 1 || legacy[0].SaleHasAnimals != nil {
		t.Fatalf("an event without has_live_animals must open as before (nil = every step), got %+v", legacy)
	}
	if failed := open(map[string]any{"sales_deal_id": "deal-1", "status": "Deal Failed", "has_live_animals": true}); len(failed) != 0 {
		t.Fatalf("a sale recorded already failed must open nothing, got %+v", failed)
	}
}

// TestDealFailedCancelsTheSaleWorkflow (maintainer decision 2026-09-25): a deal marked Deal Failed
// cancels its workflow; every other status change leaves it alone.
func TestDealFailedCancelsTheSaleWorkflow(t *testing.T) {
	repo := newFakeRepo()
	h := NewSaleStatusChangedWorkflowHandler(NewService(repo, nil))
	send := func(status string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"sales_deal_id": "deal-9", "previous_status": "Deal Closed", "status": status})
		if err := h.HandleEvent(context.Background(), eventbus.Event{Type: EventSalesDealStatusChanged, TenantID: "tenant", Key: "deal-9", Payload: raw}); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
	}
	send("In Discussion")
	send("Advance Paid")
	if len(repo.saleCancellations) != 0 {
		t.Fatalf("a non-failed status must not cancel, got %v", repo.saleCancellations)
	}
	send("Deal Failed")
	if len(repo.saleCancellations) != 1 || repo.saleCancellations[0] != "deal-9" {
		t.Fatalf("Deal Failed must cancel the deal's workflow, got %v", repo.saleCancellations)
	}
}
