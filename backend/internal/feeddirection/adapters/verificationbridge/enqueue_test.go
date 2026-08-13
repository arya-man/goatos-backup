package verificationbridge

import (
	"context"
	"strings"
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
	// The subject is the SESSION and nothing else. It used to read
	// "Session 1 · castro · Pen 2" -- the shed ID and a duplicate of the pen, which this fixture
	// made look harmless by naming the shed "castro". On real data that middle field is a UUID, and
	// a verifier's card read "Session 2 · 62241795-628e-58ef-9591-aa384fb0f0f7 · Pen 1" (reported
	// 2026-08-09). The old expectation is why it survived review: a fixture asserting a shape the
	// farm does not have is a defect even while it passes.
	//
	// Location rides on ShedID/PartitionLabel (asserted above) and is composed ONCE at the wire
	// boundary by oploc.Display(), which is what the card renders.
	if item.SubjectLabel == nil || *item.SubjectLabel != "Session 1" {
		t.Fatalf("SubjectLabel = %v, want just the session", item.SubjectLabel)
	}
	if strings.Contains(*item.SubjectLabel, "castro") || strings.Contains(*item.SubjectLabel, "Pen ") {
		t.Errorf("SubjectLabel = %q leaks the shed id or duplicates the pen", *item.SubjectLabel)
	}
}
