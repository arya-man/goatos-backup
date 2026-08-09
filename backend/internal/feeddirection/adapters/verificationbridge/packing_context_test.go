package verificationbridge

import (
	"context"
	"testing"
	"time"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// A feed packing item must tell the verifier what the pen was EXPECTED to be packed with.
//
// Before this, the item carried the shed, pen, session, operator and video and NOTHING about the
// feed, so the verifier could confirm a video existed but not that the right feed was packed in the
// right amount (STG 2026-08-09). These assertions are on the values reaching CreateItem because a
// "does the field exist" check passed throughout that outage.

type capturingCreator struct{ last verificationdomain.CreateItem }

func (c *capturingCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	c.last = in
	return verificationdomain.CreateItemResult{}, nil
}

func enqueuePacking(t *testing.T, in feeddirectionapp.FeedPackingVerificationEnqueueRequest) verificationdomain.CreateItem {
	t.Helper()
	creator := &capturingCreator{}
	if err := NewPacking(creator).EnqueueFeedPackingVerification(context.Background(), in); err != nil {
		t.Fatalf("EnqueueFeedPackingVerification() error = %v", err)
	}
	return creator.last
}

func basePackingRequest() feeddirectionapp.FeedPackingVerificationEnqueueRequest {
	return feeddirectionapp.FeedPackingVerificationEnqueueRequest{
		TenantID:        "11111111-1111-4111-8111-111111111111",
		CompletionID:    "22222222-2222-4222-8222-222222222222",
		ParkID:          "33333333-3333-4333-8333-333333333333",
		ShedID:          "44444444-4444-4444-8444-444444444444",
		ShedName:        "Mandela 1",
		PartitionLabel:  "Part 2",
		SessionNo:       1,
		Workflow:        "normal",
		TargetDate:      time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		PackingProofRef: "55555555-5555-4555-8555-555555555555",
		IdempotencyKey:  "feed-packing-verification:22222222-2222-4222-8222-222222222222:1",
	}
}

func TestPackingItemCarriesTheExpectedRation(t *testing.T) {
	req := basePackingRequest()
	req.RationSummary = "Maize 12.5 kg · Soya 4 kg"
	req.HeadCountSummary = "38"

	got := enqueuePacking(t, req)

	if len(got.ContextRows) != 2 {
		t.Fatalf("context rows = %+v, want the ration and the head count", got.ContextRows)
	}
	// Order matters: the ration is what the verifier checks the video against, so it leads.
	if got.ContextRows[0].Label != "Expected ration" || got.ContextRows[0].Value != "Maize 12.5 kg · Soya 4 kg" {
		t.Errorf("row 0 = %+v, want the expected ration first", got.ContextRows[0])
	}
	if got.ContextRows[1].Value != "38" {
		t.Errorf("row 1 = %+v, want the pen's head count", got.ContextRows[1])
	}
}

// An unreadable sheet must yield NO row rather than a placeholder: "Expected ration: —" states that
// nothing was expected, which is a different and wronger claim than saying nothing.
func TestPackingItemOmitsUnknownExpectationRatherThanFakingIt(t *testing.T) {
	got := enqueuePacking(t, basePackingRequest())

	if len(got.ContextRows) != 0 {
		t.Errorf("context rows = %+v, want none when the issued sheet could not be read", got.ContextRows)
	}
}

// The pen must ride on its OWN field, not only inside the display label. Packing left this column
// NULL, so anything filtering or grouping by pen missed every packing item.
func TestPackingItemCarriesPartitionAsAField(t *testing.T) {
	got := enqueuePacking(t, basePackingRequest())

	if got.PartitionLabel == nil || *got.PartitionLabel != "Part 2" {
		t.Fatalf("PartitionLabel = %v, want the pen as its own field", got.PartitionLabel)
	}
	if got.SubjectLabel == nil || *got.SubjectLabel != "Session 1 · Mandela 1 - Part 2" {
		t.Errorf("SubjectLabel = %v, want session and operational location", got.SubjectLabel)
	}
}
