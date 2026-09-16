package verificationbridge

import (
	"context"
	"testing"

	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type recordingWastageVerification struct{ recordingVerificationCreator }

func (r *recordingWastageVerification) RelabelItemBySource(context.Context, string, string, string, string, string) (int, error) {
	return 0, nil
}

// Every feed stage's bridge maps the card's captures onto the verifier item POSITIONALLY: a blank
// ref is dropped TOGETHER with its title, kinds are normalized (an unjudged either is unknown), and
// the answers become context rows. Dropping the ref alone would shift every later title by one.
func TestFeedBridgesMapMediaMetaPositionallyForAllFourStages(t *testing.T) {
	refs := []string{"r-bag", "", "r-clip"}
	meta := []feeddirectionports.ProofMeta{{Label: " Bag ", Kind: "photo"}, {Label: "Lost", Kind: "video"}, {Label: "Crew clip", Kind: "either"}}
	answers := []authored.AnswerRow{{Title: "Trough clean?", Value: "Yes"}}
	ctx := context.Background()

	recorder := &recordingVerificationCreator{}
	wastage := &recordingWastageVerification{}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(New(recorder).EnqueueFeedDistributionVerification(ctx, feeddirectionapp.FeedDistributionVerificationEnqueueRequest{
		TenantID: "t", CompletionID: "c", SessionNo: 1, MediaRefs: refs, MediaMeta: meta, ContextRows: answers, IdempotencyKey: "k1",
	}))
	must(NewPacking(recorder).EnqueueFeedPackingVerification(ctx, feeddirectionapp.FeedPackingVerificationEnqueueRequest{
		TenantID: "t", CompletionID: "c", SessionNo: 1, MediaRefs: refs, MediaMeta: meta, ContextRows: answers, IdempotencyKey: "k2",
	}))
	must(NewTransport(recorder).EnqueueFeedTransportVerification(ctx, feeddirectionapp.FeedTransportVerificationEnqueueRequest{
		TenantID: "t", AttemptID: "a", MediaRefs: refs, MediaMeta: meta, AnswerRows: answers, IdempotencyKey: "k3",
	}))
	must(NewWastage(wastage).EnqueueFeedWastageVerification(ctx, feeddirectionapp.FeedWastageVerificationEnqueueRequest{
		TenantID: "t", CompletionID: "c", MediaRefs: refs, MediaMeta: meta, AnswerRows: answers, IdempotencyKey: "k4",
	}))
	items := append(append([]verificationdomain.CreateItem(nil), recorder.items...), wastage.items...)
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	for i, item := range items {
		if len(item.MediaRefs) != 2 || item.MediaRefs[0] != "r-bag" || item.MediaRefs[1] != "r-clip" {
			t.Fatalf("item %d refs = %v", i, item.MediaRefs)
		}
		want := []verificationdomain.MediaMeta{{Label: "Bag", Kind: "photo"}, {Label: "Crew clip", Kind: ""}}
		if len(item.MediaMeta) != 2 || item.MediaMeta[0] != want[0] || item.MediaMeta[1] != want[1] {
			t.Fatalf("item %d meta = %+v, want %+v", i, item.MediaMeta, want)
		}
		found := false
		for _, row := range item.ContextRows {
			if row.Label == "Trough clean?" && row.Value == "Yes" {
				found = true
			}
		}
		if !found {
			t.Fatalf("item %d context rows = %+v, want the answer", i, item.ContextRows)
		}
	}

	// A legacy request (fixed refs, no card list) carries NO meta: the queue falls back to the
	// category registry, exactly as before cards existed.
	legacy := &recordingVerificationCreator{}
	must(New(legacy).EnqueueFeedDistributionVerification(ctx, feeddirectionapp.FeedDistributionVerificationEnqueueRequest{
		TenantID: "t", CompletionID: "c", SessionNo: 1, DistributionProofRef: "d", WaterProofRef: "w", IdempotencyKey: "k5",
	}))
	if len(legacy.items[0].MediaMeta) != 0 {
		t.Fatalf("legacy meta = %+v, want none", legacy.items[0].MediaMeta)
	}
}
