package proof

import (
	"context"
	"strconv"
	"strings"

	fdports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// PenSessionCaptureKey rebuilds the key the PHONE stamps on every feed proof upload as
// `metadata.client_task_key`, so the server can find a pen-session's proofs whoever shot them.
//
// It MIRRORS `feedCaptureGroupKey` + `partitionMatchToken` in
// apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedCaptureGroupKey.kt.
// The two must change together: this is a string-equality lookup, so any drift silently returns NO
// matches and the multi-operator flow degrades to "nobody captured anything" instead of failing
// loudly. TestPenSessionCaptureKeyMatchesTheClientToken pins the format against values observed on
// STG.
//
// This is deliberately NOT domain.PartitionMatchKey. That normalizes a partition for the SERVER's
// own keys; this mirrors the CLIENT's token, a different function that merely looks similar.
func PenSessionCaptureKey(q fdports.PenSessionCaptureQuery) string {
	return strings.Join([]string{
		"feed-dist",
		q.TargetDate.Format("2006-01-02"),
		strings.TrimSpace(q.ShedID),
		clientPartitionToken(q.PartitionLabel),
		strconv.Itoa(int(q.SessionNo)),
		strings.TrimSpace(q.Workflow),
	}, ":")
}

// clientPartitionToken mirrors the phone's partitionMatchToken: trimmed, lowercased, internal
// whitespace collapsed, blank -> "whole". "whole" is a MATCHING key and never user copy.
func clientPartitionToken(label string) string {
	normalized := strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(label))), " ")
	if normalized == "" {
		return "whole"
	}
	return normalized
}

// ListPenSessionCaptures reads one pen-session's completed proof uploads through the proof module's
// own query, which already filters on tenant + shed scope + client_task_key + upload_state.
//
// The feed module deliberately does NOT touch proof_artifacts itself: the table belongs to the proof
// module, and this adapter is the boundary. Results arrive newest-first, so the first row seen for a
// slot is that slot's current proof.
func (v *Validator) ListPenSessionCaptures(
	ctx context.Context, q fdports.PenSessionCaptureQuery,
) ([]fdports.CapturedProofSlot, error) {
	shedID := strings.TrimSpace(q.ShedID)
	if strings.TrimSpace(q.TenantID) == "" || shedID == "" {
		return nil, nil
	}
	artifacts, err := v.repo.ListUploadedProofs(ctx, proofdomain.ListUploadedProofsQuery{
		TenantID:      strings.TrimSpace(q.TenantID),
		ScopeType:     "shed",
		ScopeID:       shedID,
		ClientTaskKey: PenSessionCaptureKey(q),
		Limit:         penSessionCaptureLimit,
		// The proof module authorizes the shed against these. Never AllAuthorizedParks: that would
		// disable the check the handler's clamp is only the first half of.
		AuthorizedParkIDs: q.AuthorizedParkIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make([]fdports.CapturedProofSlot, 0, 3)
	seen := make(map[string]struct{}, 3)
	for _, artifact := range artifacts {
		fieldKey := metadataString(artifact.Metadata, "field_key")
		if fieldKey == "" {
			continue
		}
		// Newest-first: the first row for a slot is the live one. A slot re-captured several times
		// must report ONE proof, never a duplicate row per take.
		if _, done := seen[fieldKey]; done {
			continue
		}
		seen[fieldKey] = struct{}{}
		capturedAt := artifact.CreatedAt
		if artifact.UploadedAt != nil {
			capturedAt = *artifact.UploadedAt
		}
		out = append(out, fdports.CapturedProofSlot{
			FieldKey:   fieldKey,
			ProofID:    artifact.ProofID,
			CapturedAt: capturedAt,
			MimeType:   artifact.MimeType,
		})
	}
	return out, nil
}

// penSessionCaptureLimit bounds the read. A pen-session has three slots; the allowance covers
// re-captures that are still on file without ever becoming an unbounded scan.
const penSessionCaptureLimit = 30

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
