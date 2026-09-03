package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type fakePenReconciliationVerdictRepo struct {
	applied []domain.PenReconciliationVerdictCommand
	bounced []domain.PenReconciliationVerdictCommand
}

func (f *fakePenReconciliationVerdictRepo) ApplyVerifiedPenReconciliation(
	_ context.Context, in domain.PenReconciliationVerdictCommand,
) error {
	f.applied = append(f.applied, in)
	return nil
}

func (f *fakePenReconciliationVerdictRepo) BouncePenReconciliationForRework(
	_ context.Context, in domain.PenReconciliationVerdictCommand,
) error {
	f.bounced = append(f.bounced, in)
	return nil
}

// TestPenReconciliationVerdictRouting pins that approve completes the card, rework bounces it
// with the verifier's reason, and verdicts for OTHER counts ref_types (shifting shares the
// module) pass through untouched.
func TestPenReconciliationVerdictRouting(t *testing.T) {
	repo := &fakePenReconciliationVerdictRepo{}
	handler := NewPenReconciliationVerificationHandler(repo, nil)

	approve := eventbus.Event{
		ID: "event-1", Type: EventVerificationVerdictApproved, TenantID: "tenant-1",
		OccurredAt: time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC),
		Payload:    []byte(`{"verified_by":"verifier-1","source":{"module":"counts","ref_type":"pen_reconciliation_card","ref_id":"card-1"}}`),
	}
	if err := handler.HandleEvent(context.Background(), approve); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if len(repo.applied) != 1 || repo.applied[0].CardID != "card-1" ||
		repo.applied[0].VerifiedBy != "verifier-1" || repo.applied[0].TenantID != "tenant-1" {
		t.Fatalf("applied = %+v", repo.applied)
	}

	rework := eventbus.Event{
		ID: "event-2", Type: EventVerificationVerdictRework, TenantID: "tenant-1",
		OccurredAt: time.Now(),
		Payload:    []byte(`{"verified_by":"verifier-1","reason":"animal not visible in the video","source":{"module":"counts","ref_type":"pen_reconciliation_card","ref_id":"card-2"}}`),
	}
	if err := handler.HandleEvent(context.Background(), rework); err != nil {
		t.Fatalf("rework: %v", err)
	}
	if len(repo.bounced) != 1 || repo.bounced[0].CardID != "card-2" ||
		repo.bounced[0].Reason != "animal not visible in the video" {
		t.Fatalf("bounced = %+v", repo.bounced)
	}

	// A shifting verdict shares module=counts and must NOT touch reconciliation cards.
	shifting := eventbus.Event{
		ID: "event-3", Type: EventVerificationVerdictApproved, TenantID: "tenant-1",
		Payload: []byte(`{"verified_by":"verifier-1","source":{"module":"counts","ref_type":"shifting_event","ref_id":"movement-1"}}`),
	}
	if err := handler.HandleEvent(context.Background(), shifting); err != nil {
		t.Fatalf("shifting verdict: %v", err)
	}
	if len(repo.applied) != 1 || len(repo.bounced) != 1 {
		t.Fatalf("shifting verdict leaked into reconciliation: applied=%d bounced=%d",
			len(repo.applied), len(repo.bounced))
	}
}
