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
// slot is that slot's current proof. Display names are enriched from workforce_members when the pool
// is wired; if wiring is missing or a lookup fails, CapturedByName is left empty.
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

	// Collect unique uploader IDs for a single batch lookup.
	uploaderIDs := make([]string, 0)
	uploaderIDSet := make(map[string]struct{})
	for _, artifact := range artifacts {
		if artifact.UploadedBy != nil && *artifact.UploadedBy != "" {
			uploaderID := *artifact.UploadedBy
			if _, seen := uploaderIDSet[uploaderID]; !seen {
				uploaderIDs = append(uploaderIDs, uploaderID)
				uploaderIDSet[uploaderID] = struct{}{}
			}
		}
	}

	// Look up display names in batch (only if pool is wired). Failures are non-fatal; empty names are acceptable.
	displayNames := make(map[string]string)
	if v.pool != nil && len(uploaderIDs) > 0 {
		displayNames = v.lookupWorkforceDisplayNames(ctx, q.TenantID, uploaderIDs)
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

		capturedByName := ""
		if artifact.UploadedBy != nil && *artifact.UploadedBy != "" {
			capturedByName = displayNames[*artifact.UploadedBy]
		}

		out = append(out, fdports.CapturedProofSlot{
			FieldKey:       fieldKey,
			ProofID:        artifact.ProofID,
			CapturedAt:     capturedAt,
			MimeType:       artifact.MimeType,
			CapturedByName: capturedByName,
		})
	}
	return out, nil
}

// lookupWorkforceDisplayNames queries workforce_members for display_name by user_id.
// Returns a map of user_id -> display_name. Non-fatal on errors; missing entries are returned as empty strings.
func (v *Validator) lookupWorkforceDisplayNames(ctx context.Context, tenantID string, userIDs []string) map[string]string {
	if v.pool == nil || len(userIDs) == 0 {
		return make(map[string]string)
	}

	result := make(map[string]string)
	// Best-effort lookup; timeout or pool errors are silently ignored so the proof slot is still
	// returned, just without a display name.
	rows, err := v.pool.Query(ctx, `
		SELECT user_id::text, display_name
		FROM workforce_members
		WHERE tenant_id = $1::uuid
		  AND user_id = ANY($2::uuid[])
	`, tenantID, userIDs)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var userID, displayName string
		if err := rows.Scan(&userID, &displayName); err != nil {
			continue
		}
		result[userID] = strings.TrimSpace(displayName)
	}
	return result
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

// DescribeProofUploads resolves the proof ids a COMPLETION row names back to their provenance —
// who uploaded each and when — through the proof module's own batched read.
//
// It is the leadership execution table's detail source (maintainer decision 2026-08-26). A
// pen-session's three references are looked up in ONE GetProofsByIDs call and the uploader display
// names in ONE workforce lookup, however many pen-sessions the day holds: the caller passes every
// row's references at once precisely so this never becomes a per-row round trip.
//
// Missing is missing: an id that does not resolve is absent from the map rather than mapped to a
// zero value, so the caller reports the slot as unproved instead of as a proof with no uploader.
func (v *Validator) DescribeProofUploads(
	ctx context.Context, tenantID string, proofIDs []string,
) (map[string]fdports.ProofUpload, error) {
	tenantID = strings.TrimSpace(tenantID)
	unique := make([]string, 0, len(proofIDs))
	seen := make(map[string]struct{}, len(proofIDs))
	for _, raw := range proofIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, done := seen[id]; done {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if tenantID == "" || len(unique) == 0 {
		return map[string]fdports.ProofUpload{}, nil
	}

	artifacts, err := v.repo.GetProofsByIDs(ctx, tenantID, unique)
	if err != nil {
		return nil, err
	}

	uploaderIDs := make([]string, 0, len(artifacts))
	uploaderSeen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.UploadedBy == nil || *artifact.UploadedBy == "" {
			continue
		}
		if _, done := uploaderSeen[*artifact.UploadedBy]; done {
			continue
		}
		uploaderSeen[*artifact.UploadedBy] = struct{}{}
		uploaderIDs = append(uploaderIDs, *artifact.UploadedBy)
	}
	displayNames := map[string]string{}
	if v.pool != nil && len(uploaderIDs) > 0 {
		displayNames = v.lookupWorkforceDisplayNames(ctx, tenantID, uploaderIDs)
	}

	out := make(map[string]fdports.ProofUpload, len(artifacts))
	for proofID, artifact := range artifacts {
		if artifact.TenantID != tenantID {
			continue
		}
		uploadedAt := artifact.CreatedAt
		if artifact.UploadedAt != nil {
			uploadedAt = *artifact.UploadedAt
		}
		name := ""
		if artifact.UploadedBy != nil && *artifact.UploadedBy != "" {
			name = displayNames[*artifact.UploadedBy]
		}
		out[proofID] = fdports.ProofUpload{
			ProofID:        proofID,
			UploadedAt:     uploadedAt,
			UploadedByName: name,
			MimeType:       artifact.MimeType,
		}
	}
	return out, nil
}

var _ fdports.ProofUploadDescriber = (*Validator)(nil)
