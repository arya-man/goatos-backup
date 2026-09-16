package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Each feed verdict handler hands the store the capture the verdict judged (source.evidence_id), so
// a re-delivered or late verdict for an earlier submission cannot move the crew's re-shot work
// (see TestAStaleVerdictNeverAppliesToTheResubmittedWork for the store half).

type evidencePackingStore struct {
	fakePackingStore
	apply  ports.ApplyPackingParams
	bounce ports.BouncePackingParams
}

func (s *evidencePackingStore) ApplyVerifiedPacking(_ context.Context, p ports.ApplyPackingParams) (bool, error) {
	s.apply = p
	return true, nil
}
func (s *evidencePackingStore) BouncePackingForRework(_ context.Context, p ports.BouncePackingParams) (bool, error) {
	s.bounce = p
	return true, nil
}

type evidenceDistributionStore struct {
	fakeDistributionStore
	apply ports.ApplyDistributionParams
}

func (s *evidenceDistributionStore) ApplyVerifiedDistribution(_ context.Context, p ports.ApplyDistributionParams) (bool, error) {
	s.apply = p
	return true, nil
}

type evidenceWastageStore struct {
	fakeWastageStore
	bounce ports.BounceWastageParams
}

func (s *evidenceWastageStore) BounceWastageForRework(_ context.Context, p ports.BounceWastageParams) (bool, error) {
	s.bounce = p
	return true, nil
}

func verdictEvent(t *testing.T, eventType, refType string) eventbus.Event {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"reason": "blurry", "verified_by": "v-1",
		"source": map[string]any{"module": domain.VerificationModuleFeed, "ref_type": refType, "ref_id": "c-1", "evidence_id": "proof-judged"},
	})
	return eventbus.Event{ID: "e-1", Type: eventType, TenantID: testTenant, Payload: payload}
}

func TestFeedVerdictHandlersThreadTheJudgedEvidence(t *testing.T) {
	ctx := context.Background()
	packing := &evidencePackingStore{}
	h := NewFeedPackingVerificationHandler(packing, nil)
	if err := h.HandleEvent(ctx, verdictEvent(t, eventVerificationVerdictApproved, domain.VerificationRefTypePacking)); err != nil {
		t.Fatal(err)
	}
	if err := h.HandleEvent(ctx, verdictEvent(t, eventVerificationVerdictRework, domain.VerificationRefTypePacking)); err != nil {
		t.Fatal(err)
	}
	if packing.apply.EvidenceID != "proof-judged" || packing.bounce.EvidenceID != "proof-judged" {
		t.Fatalf("packing verdict evidence = apply %q / bounce %q, want proof-judged", packing.apply.EvidenceID, packing.bounce.EvidenceID)
	}
	dist := &evidenceDistributionStore{}
	if err := NewFeedDistributionVerificationHandler(dist, nil).HandleEvent(ctx, verdictEvent(t, eventVerificationVerdictApproved, domain.VerificationRefTypeFeed)); err != nil {
		t.Fatal(err)
	}
	if dist.apply.EvidenceID != "proof-judged" {
		t.Fatalf("distribution approve evidence = %q", dist.apply.EvidenceID)
	}
	wastage := &evidenceWastageStore{}
	if err := NewFeedWastageVerificationHandler(wastage, nil).HandleEvent(ctx, verdictEvent(t, eventVerificationVerdictRework, domain.VerificationRefTypeWastage)); err != nil {
		t.Fatal(err)
	}
	if wastage.bounce.EvidenceID != "proof-judged" {
		t.Fatalf("wastage rework evidence = %q", wastage.bounce.EvidenceID)
	}
}
