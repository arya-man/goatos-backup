package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// The verdict payload is the ONLY channel a producing module has for learning which
// proof a verifier decided against. Weighing's store has always carried a
// stale-evidence guard, but this payload never named the evidence, so every
// production verdict arrived with an empty id and the guard took its
// backward-compatibility skip: a verdict rendered against an older video was applied
// to whatever video had replaced it. The item's media_refs are frozen at creation
// (a re-shoot withdraws the item and raises a new one), so media_refs[0] IS the proof
// the verifier saw.
func TestVerificationVerdictPayloadNamesTheReviewedEvidence(t *testing.T) {
	proofID := "00000000-0000-4000-8000-0000000007a1"
	payload := verificationVerdictPayload(domain.Item{
		TenantID:  "00000000-0000-4000-8000-000000000001",
		ItemID:    "00000000-0000-4000-8000-000000000002",
		Status:    domain.StatusApproved,
		MediaRefs: []string{proofID, "00000000-0000-4000-8000-0000000007a2"},
	})

	source, ok := payload["source"].(map[string]any)
	if !ok {
		t.Fatalf("payload source has type %T, want map", payload["source"])
	}
	got, ok := source["evidence_id"].(string)
	if !ok {
		t.Fatalf("payload carries no source.evidence_id; the consumer's stale-evidence guard cannot fire without it")
	}
	if got != proofID {
		t.Fatalf("source.evidence_id = %q, want the PRIMARY proof %q", got, proofID)
	}
}

// An item raised with no media at all must publish an empty evidence id rather than
// a placeholder: empty already means "no evidence to compare" to the consumer, while
// a sentinel would read as a mismatch against every record and reject a verdict that
// is perfectly valid.
func TestVerificationVerdictPayloadEvidenceIDEmptyWithoutMedia(t *testing.T) {
	payload := verificationVerdictPayload(domain.Item{Status: domain.StatusRejected})
	source := payload["source"].(map[string]any)
	if got := source["evidence_id"]; got != "" {
		t.Fatalf("source.evidence_id = %v, want empty for a media-less item", got)
	}
}
