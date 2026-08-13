package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// goneObjectMedia stands for the real production failure: the proof ROWS all still exist, so every
// media_ref resolves a signed link exactly as before, but the stored objects behind them are gone.
// Before this gate existed, RecordVerdict asked only the link-resolving question, so an approve —
// the single irreversible action in this app, with no un-approve endpoint anywhere — succeeded
// against evidence that did not exist.
type goneObjectMedia struct{}

func (goneObjectMedia) ResolveMedia(_ context.Context, _ string, proofIDs []string) ([]domain.MediaItem, error) {
	out := make([]domain.MediaItem, 0, len(proofIDs))
	for _, id := range proofIDs {
		out = append(out, domain.MediaItem{ProofID: id, DownloadURL: "https://signed.example/" + id})
	}
	return out, nil
}

func (goneObjectMedia) EnsureEvidenceAvailable(_ context.Context, _ string, _ []string) error {
	return ports.ErrEvidenceMissing
}

// uncheckableMedia resolves links but cannot complete the existence check (storage transport
// fault). That is NOT proof of absence and must be reported differently from a missing object.
type uncheckableMedia struct{}

func (uncheckableMedia) ResolveMedia(_ context.Context, _ string, proofIDs []string) ([]domain.MediaItem, error) {
	return goneObjectMedia{}.ResolveMedia(context.Background(), "", proofIDs)
}

func (uncheckableMedia) EnsureEvidenceAvailable(_ context.Context, _ string, _ []string) error {
	return errors.New("storage unreachable")
}

func seedPendingItem(t *testing.T, svc *Service, key string) domain.Item {
	t.Helper()
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1", "proof-2"}, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	return result.Item
}

func serviceWithMedia(t *testing.T, media ports.MediaResolver) *Service {
	t.Helper()
	svc := NewService(newFakeRepo(), media)
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
	}); err != nil {
		t.Fatalf("RegisterCategory: %v", err)
	}
	return svc
}

// The gate: a missing proof OBJECT must refuse the approve, terminally.
func TestApproveIsRefusedWhenProofObjectIsMissing(t *testing.T) {
	svc := serviceWithMedia(t, goneObjectMedia{})
	item := seedPendingItem(t, svc, "gate-key-1")

	got, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("approve with a missing proof object err = %v (item status %q), want a refusal", err, got.Status)
	}
	if appErr.Code != "evidence_missing" {
		t.Fatalf("code = %q, want evidence_missing", appErr.Code)
	}
	if appErr.HTTPStatus != 422 {
		t.Fatalf("status = %d, want 422", appErr.HTTPStatus)
	}
	if appErr.Retryable {
		t.Fatal("evidence_missing marked retryable; a deleted object cannot reappear on retry")
	}
	if got.Status == domain.StatusApproved {
		t.Fatal("item was approved against evidence that does not exist")
	}

	// And the refusal must be durable, not a one-shot: the item is still pending afterwards.
	stored, err := svc.GetItem(context.Background(), testTenant, item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if stored.Status != domain.StatusPending {
		t.Fatalf("stored status = %q, want pending", stored.Status)
	}
}

// Do not strand her: with the object gone, sending the work back for rework is the ONLY correct
// move left, so reject must still go through.
func TestRejectStillWorksWhenProofObjectIsMissing(t *testing.T) {
	svc := serviceWithMedia(t, goneObjectMedia{})
	item := seedPendingItem(t, svc, "gate-key-2")

	got, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: testTenant, RowVersion: 1, Reason: "proof video is not there, please record it again",
	})
	if err != nil {
		t.Fatalf("reject with a missing proof object: %v — the verifier is stranded", err)
	}
	if got.Status != domain.StatusRejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

// Reject keeps working in the normal case too, and its mandatory reason is unchanged.
func TestRejectWorksWhenProofObjectIsPresentAndStillRequiresAReason(t *testing.T) {
	svc, _ := newTestService()
	item := seedPendingItem(t, svc, "gate-key-3")

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: testTenant, RowVersion: 1,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "reason_required" {
		t.Fatalf("reason-less reject err = %v, want reason_required", err)
	}

	got, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: testTenant, RowVersion: 1, Reason: "goat was not held properly",
	})
	if err != nil {
		t.Fatalf("reject with present evidence: %v", err)
	}
	if got.Status != domain.StatusRejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

// The gate must not become a wall: with every object present, approve still succeeds.
func TestApproveStillSucceedsWhenProofObjectsArePresent(t *testing.T) {
	svc, _ := newTestService()
	item := seedPendingItem(t, svc, "gate-key-4")

	got, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	if err != nil {
		t.Fatalf("approve with present evidence: %v", err)
	}
	if got.Status != domain.StatusApproved {
		t.Fatalf("status = %q, want approved", got.Status)
	}
}

// "We could not check" is not "the video is gone": different code, retryable, and it must still
// refuse the irreversible action.
func TestApproveIsRefusedRetryablyWhenTheCheckItselfFails(t *testing.T) {
	svc := serviceWithMedia(t, uncheckableMedia{})
	item := seedPendingItem(t, svc, "gate-key-5")

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "evidence_check_failed" {
		t.Fatalf("err = %v, want evidence_check_failed", err)
	}
	if !appErr.Retryable {
		t.Fatal("evidence_check_failed must be retryable — the check failed, the video may be fine")
	}

	// Rework must remain open on this path too.
	got, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: testTenant, RowVersion: 1, Reason: "cannot open the proof video",
	})
	if err != nil {
		t.Fatalf("reject when the evidence check failed: %v", err)
	}
	if got.Status != domain.StatusRejected {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

// The visible copy must be farm language: no HTTP codes, no internal jargon, and it must tell her
// what to do instead.
func TestEvidenceRefusalCopyIsFarmLanguageAndOffersRework(t *testing.T) {
	for _, err := range []*Error{evidenceMissingErr(), evidenceUncheckableErr()} {
		msg := err.Message
		for _, banned := range []string{"422", "410", "HTTP", "proof_object_missing", "media_ref", "stat", "storage", "null", "object"} {
			if containsFold(msg, banned) {
				t.Fatalf("message %q contains internal word %q", msg, banned)
			}
		}
		if !containsFold(msg, "rework") {
			t.Fatalf("message %q does not tell the verifier she can send it back for rework", msg)
		}
	}
}

func containsFold(haystack, needle string) bool {
	h, n := []rune(lowerASCII(haystack)), []rune(lowerASCII(needle))
	if len(n) == 0 || len(n) > len(h) {
		return false
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j := range n {
			if h[i+j] != n[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func lowerASCII(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'A' && r <= 'Z' {
			out[i] = r + 32
		}
	}
	return string(out)
}
