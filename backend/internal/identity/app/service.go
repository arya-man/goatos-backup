package app

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

var displayIDPattern = regexp.MustCompile(`^G-[0-9]{6,}$`)

const maxMergeRedirectHops = 16

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetGoatPassport(ctx context.Context, tenantID, lookup string, traceID string) (*domain.GoatPassportResult, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	lookup = strings.TrimSpace(lookup)
	if lookup == "" {
		return nil, BadRequest("invalid_goat_lookup", "goat_id or display_id is required")
	}

	var goat *domain.GoatPassport
	var err error
	if displayIDPattern.MatchString(lookup) {
		goat, err = s.repo.GetGoatByDisplayID(ctx, tenantID, lookup)
	} else {
		goat, err = s.repo.GetGoatByID(ctx, tenantID, lookup)
	}
	if err != nil {
		return nil, mapRepoErr(err)
	}

	originalID := goat.GoatID
	warnings := ensureWarnings(goat.Summary.Warnings)
	visited := map[string]struct{}{}
	for hop := 0; goat.IdentityState == "merged"; hop++ {
		if hop >= maxMergeRedirectHops {
			return nil, Internal("merge redirect chain exceeded maximum depth")
		}
		if _, ok := visited[goat.GoatID]; ok {
			return nil, Internal("merge redirect cycle detected")
		}
		visited[goat.GoatID] = struct{}{}
		if goat.MergedIntoGoatID == nil {
			return nil, Internal("merged goat is missing survivor redirect")
		}
		redirectID := *goat.MergedIntoGoatID
		warnings = append(warnings, domain.Warning{
			Code:           "merged_redirect",
			Message:        "Goat identity has been merged; returning survivor passport.",
			OriginalGoatID: &originalID,
			RedirectGoatID: &redirectID,
		})
		next, err := s.repo.GetGoatByID(ctx, tenantID, redirectID)
		if err != nil {
			return nil, mapRepoErr(err)
		}
		goat = next
	}

	goat.Identifiers = ensureIdentifiers(goat.Identifiers)
	goat.EvidenceRefs = ensureEvidence(goat.EvidenceRefs)
	goat.Summary.Warnings = ensureWarnings(goat.Summary.Warnings)
	return &domain.GoatPassportResult{Goat: *goat, Warnings: warnings, TraceID: traceID}, nil
}

func (s *Service) SearchGoats(ctx context.Context, params ports.SearchGoatsParams, traceID string) (*domain.GoatSearchResult, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	if params.Limit < 1 || params.Limit > 100 {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 100")
	}
	if err := validateOptionalUUID("goat_id", params.GoatID); err != nil {
		return nil, err
	}
	if err := validateOptionalUUID("farm_id", params.FarmID); err != nil {
		return nil, err
	}
	if err := validateOptionalUUID("park_id", params.ParkID); err != nil {
		return nil, err
	}
	if err := validateOptionalUUID("location_id", params.LocationID); err != nil {
		return nil, err
	}
	items, next, err := s.repo.SearchGoats(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	for i := range items {
		items[i].Warnings = ensureWarnings(items[i].Warnings)
	}
	return &domain.GoatSearchResult{Items: items, NextCursor: next, TraceID: traceID}, nil
}

func (s *Service) ResolveIdentifier(ctx context.Context, params ports.ResolveIdentifierParams, traceID string) (*domain.ResolveIdentifierResult, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.IdentifierType = strings.TrimSpace(params.IdentifierType)
	params.NormalizedValue = normalizeIdentifier(params.IdentifierType, params.NormalizedValue)
	if params.IdentifierType == "" || params.NormalizedValue == "" {
		return nil, BadRequest("invalid_identifier", "identifier type and value are required")
	}

	matches, err := s.repo.FindIdentifierMatches(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	active := make([]domain.IdentifierMatch, 0)
	history := make([]domain.IdentifierMatch, 0)
	for _, match := range matches {
		if match.Identifier.Status == "active" {
			active = append(active, match)
			continue
		}
		if match.Identifier.Status == "retired" || match.Identifier.Status == "disputed" {
			history = append(history, match)
		}
	}

	result := &domain.ResolveIdentifierResult{
		ResolutionState: domain.ResolutionNoMatch,
		CandidateGoats:  []domain.GoatSummary{},
		Warnings:        []domain.Warning{},
		TraceID:         traceID,
	}

	if len(active) == 0 {
		if len(history) > 0 {
			result.ResolutionState = domain.ResolutionNeedsReview
			result.CandidateGoats = summariesFromMatches(history)
			result.Warnings = append(result.Warnings, domain.Warning{
				Code:    "identifier_history_only",
				Message: "Only retired or disputed identifier history matched; review evidence before linking.",
			})
		}
		return result, nil
	}

	if len(active) > 1 {
		result.ResolutionState = domain.ResolutionMultipleMatch
		result.CandidateGoats = summariesFromMatches(active)
		if scopeKey, ok := sameScopeKey(active); ok {
			conflictID, err := s.repo.FindOpenConflictForIdentifier(ctx, params.TenantID, params.IdentifierType, params.NormalizedValue, scopeKey)
			if err != nil {
				return nil, mapRepoErr(err)
			}
			result.ConflictID = conflictID
		}
		return result, nil
	}

	match := active[0]
	if len(history) > 0 {
		result.Warnings = append(result.Warnings, domain.Warning{
			Code:    "identifier_history_present",
			Message: "Retired or disputed history exists for this identifier.",
		})
	}

	if match.Goat.IdentityState == "merged" {
		passport, err := s.GetGoatPassport(ctx, params.TenantID, match.Goat.GoatID, traceID)
		if err != nil {
			return nil, err
		}
		redirectID := passport.Goat.GoatID
		originalID := match.Goat.GoatID
		result.ResolutionState = domain.ResolutionMergedRedirect
		result.GoatSummary = &passport.Goat.Summary
		result.RedirectGoatID = &redirectID
		result.Warnings = append(result.Warnings, domain.Warning{
			Code:           "merged_redirect",
			Message:        "Identifier matched a merged goat; returning survivor.",
			OriginalGoatID: &originalID,
			RedirectGoatID: &redirectID,
		})
		return result, nil
	}

	result.ResolutionState = domain.ResolutionSingleMatch
	result.GoatSummary = &match.Goat
	return result, nil
}

func (s *Service) GetGoatTimeline(ctx context.Context, params ports.GetGoatTimelineParams, traceID string) (*domain.GoatTimelineResponse, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.GoatID = strings.TrimSpace(params.GoatID)
	if !uuidPattern.MatchString(params.GoatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	if params.Limit < 1 || params.Limit > 100 {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 100")
	}
	items, next, err := s.repo.GetGoatTimeline(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if items == nil {
		items = []domain.GoatTimelineEvent{}
	}
	return &domain.GoatTimelineResponse{Items: items, NextCursor: next, TraceID: traceID}, nil
}

func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	return nil
}

func normalizeIdentifier(identifierType, value string) string {
	value = strings.TrimSpace(value)
	switch identifierType {
	case "rfid":
		return strings.ToUpper(value)
	default:
		return value
	}
}

func summariesFromMatches(matches []domain.IdentifierMatch) []domain.GoatSummary {
	summaries := make([]domain.GoatSummary, 0, len(matches))
	seen := map[string]struct{}{}
	for _, match := range matches {
		if _, ok := seen[match.Goat.GoatID]; ok {
			continue
		}
		match.Goat.Warnings = ensureWarnings(match.Goat.Warnings)
		summaries = append(summaries, match.Goat)
		seen[match.Goat.GoatID] = struct{}{}
	}
	return summaries
}

func sameScopeKey(matches []domain.IdentifierMatch) (string, bool) {
	if len(matches) == 0 {
		return "", false
	}
	scope := matches[0].Identifier.ScopeKey
	if strings.TrimSpace(scope) == "" {
		return "", false
	}
	for _, match := range matches[1:] {
		if match.Identifier.ScopeKey != scope {
			return "", false
		}
	}
	return scope, true
}

func ensureWarnings(in []domain.Warning) []domain.Warning {
	if in == nil {
		return []domain.Warning{}
	}
	return in
}

func ensureIdentifiers(in []domain.GoatIdentifier) []domain.GoatIdentifier {
	if in == nil {
		return []domain.GoatIdentifier{}
	}
	return in
}

func ensureEvidence(in []domain.EvidenceRef) []domain.EvidenceRef {
	if in == nil {
		return []domain.EvidenceRef{}
	}
	return in
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrNotFound) {
		return NotFound("resource is missing or outside scope")
	}
	if errors.Is(err, ports.ErrIdempotencyConflict) {
		return Conflict("idempotency_conflict", "Idempotency-Key was reused with a different request body")
	}
	if errors.Is(err, ports.ErrIdempotencyPending) {
		return Conflict("idempotency_pending", "Idempotency-Key is already processing")
	}
	if errors.Is(err, ports.ErrWriteConflict) {
		return Conflict("write_conflict", "identity write cannot be applied with the supplied state or row_version")
	}
	if errors.Is(err, ports.ErrInvalidCursor) {
		return BadRequest("invalid_cursor", "cursor is not valid for this list endpoint")
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal("identity repository error")
}
