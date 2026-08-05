package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
)

const (
	sourceLabelStatusResolved       = "resolved"
	sourceLabelStatusReviewRequired = "review_required"
	sourceLabelStatusConflict       = "conflict"
)

type ResolveSourceLabelInput struct {
	TenantID      string
	ActorID       string
	TraceID       string
	SourceContext string
	SourceLabel   string
	EvidenceJSON  []byte
	EvidenceHash  string
}

func (s *Service) ResolveSourceLabel(ctx context.Context, input ResolveSourceLabelInput) (*domain.SourceLabelResolution, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	sourceContext := strings.TrimSpace(input.SourceContext)
	if !allowedAliasSourceContexts[sourceContext] {
		return nil, BadRequest("invalid_source_context", "source_context is not supported")
	}
	sourceLabel := strings.TrimSpace(input.SourceLabel)
	if sourceLabel == "" || len(sourceLabel) > 300 {
		return nil, BadRequest("invalid_source_label", "source_label must be between 1 and 300 characters")
	}
	normalized := NormalizeSourceLabel(sourceLabel)
	if normalized == "" {
		return nil, BadRequest("invalid_source_label", "source_label must contain searchable text")
	}
	evidenceJSON, err := sourceLabelEvidenceJSON(sourceContext, sourceLabel, normalized, input.EvidenceJSON)
	if err != nil {
		slog.ErrorContext(ctx, "resolve source label: evidence json processing failed", slog.String("tenant_id", tenantID), slog.String("source_context", sourceContext), slog.Any("error", err))
		return nil, BadRequest("invalid_evidence", "evidence must be a JSON object")
	}
	evidenceHash := strings.TrimSpace(input.EvidenceHash)
	if evidenceHash == "" {
		evidenceHash = sourceLabelEvidenceHash(tenantID, sourceContext, normalized, evidenceJSON)
	}
	if len(evidenceHash) > 128 {
		return nil, BadRequest("invalid_evidence_hash", "evidence_hash must be at most 128 characters")
	}

	matches, err := s.repo.FindActiveAliases(ctx, ports.SourceLabelQuery{
		TenantID:              tenantID,
		SourceContext:         sourceContext,
		NormalizedSourceLabel: normalized,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if len(matches) == 1 {
		location := matches[0].Location
		return &domain.SourceLabelResolution{
			SourceContext:         sourceContext,
			SourceLabel:           sourceLabel,
			NormalizedSourceLabel: normalized,
			Status:                sourceLabelStatusResolved,
			Location:              &location,
			CandidateLocationIDs:  []string{location.LocationID},
			TraceID:               input.TraceID,
		}, nil
	}

	candidateIDs := make([]string, 0, len(matches))
	for _, match := range matches {
		candidateIDs = append(candidateIDs, match.Location.LocationID)
	}
	reviewType := "unknown_alias"
	status := sourceLabelStatusReviewRequired
	if len(matches) > 1 {
		reviewType = "alias_conflict"
		status = sourceLabelStatusConflict
	}
	result, err := s.repo.OpenLocationReviewItem(ctx, ports.OpenLocationReviewItemCommand{
		TenantID:              tenantID,
		ActorID:               strings.TrimSpace(input.ActorID),
		TraceID:               input.TraceID,
		ReviewType:            reviewType,
		SourceContext:         sourceContext,
		SourceLabel:           sourceLabel,
		NormalizedSourceLabel: normalized,
		CandidateLocationIDs:  candidateIDs,
		EvidenceJSON:          evidenceJSON,
		EvidenceHash:          evidenceHash,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	reviewItem := result.ReviewItem
	return &domain.SourceLabelResolution{
		SourceContext:         sourceContext,
		SourceLabel:           sourceLabel,
		NormalizedSourceLabel: normalized,
		Status:                status,
		ReviewItem:            &reviewItem,
		CandidateLocationIDs:  candidateIDs,
		TraceID:               input.TraceID,
	}, nil
}

func NormalizeSourceLabel(label string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(label)), " "))
}

func sourceLabelEvidenceJSON(sourceContext, sourceLabel, normalized string, raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		return json.Marshal(decoded)
	}
	return json.Marshal(map[string]any{
		"source_context":          sourceContext,
		"source_label":            sourceLabel,
		"normalized_source_label": normalized,
	})
}

func sourceLabelEvidenceHash(tenantID, sourceContext, normalized string, evidenceJSON []byte) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{tenantID, sourceContext, normalized, string(evidenceJSON)}, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
