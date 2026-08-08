package verificationbridge

import (
	"context"
	"testing"
	"time"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type recordingVerificationCreator struct {
	items []verificationdomain.CreateItem
}

func (r *recordingVerificationCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	r.items = append(r.items, in)
	return verificationdomain.CreateItemResult{}, nil
}

func TestDistributionVerificationCarriesPartitionIdentity(t *testing.T) {
	recorder := &recordingVerificationCreator{}
	enqueuer := New(recorder)

	err := enqueuer.EnqueueFeedDistributionVerification(context.Background(), feeddirectionapp.FeedDistributionVerificationEnqueueRequest{
		TenantID:             "tenant-1",
		CompletionID:         "completion-1",
		ParkID:               "park-1",
		ShedID:               "castro",
		PartitionLabel:       "2",
		SessionNo:            1,
		Workflow:             "normal",
		TargetDate:           time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC),
		DistributionProofRef: "distribution-proof",
		WaterProofRef:        "water-proof",
		OperatorID:           "operator-1",
		CapturedAt:           time.Date(2026, 8, 8, 7, 30, 0, 0, time.UTC),
		IdempotencyKey:       "feed-distribution-verification:completion-1:1",
	})
	if err != nil {
		t.Fatalf("EnqueueFeedDistributionVerification: %v", err)
	}
	if len(recorder.items) != 1 {
		t.Fatalf("CreateItem calls = %d, want 1", len(recorder.items))
	}

	item := recorder.items[0]
	if item.PartitionLabel == nil || *item.PartitionLabel != "2" {
		t.Fatalf("PartitionLabel = %v, want 2", item.PartitionLabel)
	}
	if item.SubjectLabel == nil || *item.SubjectLabel != "Session 1 · castro · Pen 2" {
		t.Fatalf("SubjectLabel = %v, want partition-aware verifier label", item.SubjectLabel)
	}
}
