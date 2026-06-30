// Package app holds the feed-direction application service.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/feed/ports"
)

var (
	ErrMissingRequiredField        = errors.New("feed: missing required field")
	ErrInvalidResolutionAction     = errors.New("feed: invalid counts projection exception resolution action")
	ErrInvalidIdempotencyKey       = errors.New("feed: invalid idempotency key")
	ErrInvalidResolutionReason     = errors.New("feed: invalid resolution reason")
	ErrInvalidResolutionRef        = errors.New("feed: invalid resolution ref")
	ErrInvalidLimit                = errors.New("feed: invalid limit")
	ErrInvalidCursor               = errors.New("feed: invalid cursor")
	ErrInvalidExceptionFilter      = errors.New("feed: invalid counts projection exception filter")
	ErrCountsResolverUnavailable   = errors.New("feed: counts projection exception resolver unavailable")
	ErrCountsListerUnavailable     = errors.New("feed: counts projection exception lister unavailable")
	ErrIdempotencyConflict         = errors.New("feed: idempotency key reused with different payload")
	ErrProjectionExceptionNotFound = errors.New("feed: counts projection exception not found")
	ErrProjectionExceptionClosed   = errors.New("feed: counts projection exception already closed")
)

const (
	minIdempotencyKeyLen   = 8
	maxIdempotencyKeyLen   = 200
	maxResolutionReasonLen = 2000
	maxResolutionRefLen    = 500
	defaultExceptionLimit  = int32(50)
	maxExceptionLimit      = int32(200)
)

type countsReadinessReader interface {
	Readiness(ctx context.Context, tenantID string) (countsdomain.Readiness, error)
}

type countsProjectionExceptionResolver interface {
	ResolveProjectionException(ctx context.Context, in countsdomain.ProjectionExceptionResolutionRequest) (countsdomain.ProjectionExceptionResolution, error)
}

type countsProjectionExceptionLister interface {
	ListProjectionExceptions(ctx context.Context, in countsdomain.ProjectionExceptionQuery) (countsdomain.ProjectionExceptionList, error)
}

// Service coordinates feed-direction use-cases over the repository boundary.
type Service struct {
	repo                      ports.Repository
	countsReadiness           countsReadinessReader
	countsExceptionResolution countsProjectionExceptionResolver
	countsExceptionList       countsProjectionExceptionLister
	now                       func() time.Time
}

// NewService constructs a Service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock overrides the service clock for deterministic tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) WithCountsReadiness(reader countsReadinessReader) *Service {
	s.countsReadiness = reader
	return s
}

func (s *Service) WithCountsProjectionExceptionResolver(resolver countsProjectionExceptionResolver) *Service {
	s.countsExceptionResolution = resolver
	return s
}

func (s *Service) WithCountsProjectionExceptionLister(lister countsProjectionExceptionLister) *Service {
	s.countsExceptionList = lister
	return s
}

// RecordDirection records a shed feed-direction execution (idempotent).
func (s *Service) RecordDirection(ctx context.Context, in domain.NewDirection) (string, bool, error) {
	return s.repo.RecordDirection(ctx, in)
}

// AcceptDirection accepts a recorded direction on verification, returning its verification context
// (idempotent: applied is false on replay).
func (s *Service) AcceptDirection(ctx context.Context, tenantID, completionID string, verifiedBy *string) (domain.AcceptedDirection, bool, error) {
	return s.repo.AcceptDirection(ctx, tenantID, completionID, verifiedBy)
}

// RejectDirection rejects a recorded direction (rework). applied is false on replay.
func (s *Service) RejectDirection(ctx context.Context, tenantID, completionID, reason string, verifiedBy *string) (bool, error) {
	return s.repo.RejectDirection(ctx, tenantID, completionID, reason, verifiedBy)
}

// ShedHistory returns a shed's feed history.
func (s *Service) ShedHistory(ctx context.Context, tenantID, shedID string, limit int32) ([]domain.DirectionHistoryItem, error) {
	return s.repo.ListDirectionsByShed(ctx, tenantID, shedID, limit)
}

// VerificationQueue returns directions awaiting review (earliest fed first).
func (s *Service) VerificationQueue(ctx context.Context, tenantID string, limit int32) ([]domain.RecordedDirection, error) {
	return s.repo.ListRecordedDirections(ctx, tenantID, limit)
}

func (s *Service) ListCountsProjectionExceptions(ctx context.Context, in domain.CountsProjectionExceptionQuery) (domain.CountsProjectionExceptionList, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = "open"
	}
	in.ParkID = trimOptional(in.ParkID)
	in.ShedID = trimOptional(in.ShedID)
	in.ExceptionType = trimOptionalLower(in.ExceptionType)
	in.Severity = trimOptionalLower(in.Severity)
	in.OwnerRef = trimOptional(in.OwnerRef)
	in.WorkState = trimOptionalLower(in.WorkState)
	if in.TenantID == "" {
		return domain.CountsProjectionExceptionList{}, ErrMissingRequiredField
	}
	if !oneOf(in.Status, "open", "resolved", "dismissed") ||
		(in.ParkID != nil && !countsdomain.IsUUIDString(*in.ParkID)) ||
		(in.ShedID != nil && !countsdomain.IsUUIDString(*in.ShedID)) ||
		(in.ExceptionType != nil && !oneOf(*in.ExceptionType, "missing_base_count", "missing_structured_impact", "unreported_shifting", "count_mismatch", "alias_conflict", "ration_context_unresolved", "destination_shortage", "unsafe_surplus", "query_plan_unproven")) ||
		(in.Severity != nil && !oneOf(*in.Severity, "warning", "blocking", "critical")) ||
		(in.WorkState != nil && !oneOf(*in.WorkState, "blocked", "owner_missing", "resolved", "dismissed")) {
		return domain.CountsProjectionExceptionList{}, ErrInvalidExceptionFilter
	}
	if in.Limit == 0 {
		in.Limit = defaultExceptionLimit
	}
	if in.Limit < 0 || in.Limit > maxExceptionLimit {
		return domain.CountsProjectionExceptionList{}, ErrInvalidLimit
	}
	var cursor *countsdomain.ProjectionExceptionCursor
	if in.Cursor != nil {
		raw := strings.TrimSpace(*in.Cursor)
		if raw != "" {
			decoded, err := countsdomain.DecodeProjectionExceptionCursor(raw)
			if err != nil {
				return domain.CountsProjectionExceptionList{}, ErrInvalidCursor
			}
			cursor = &decoded
		}
	}
	if s.countsExceptionList == nil {
		return domain.CountsProjectionExceptionList{}, ErrCountsListerUnavailable
	}
	out, err := s.countsExceptionList.ListProjectionExceptions(ctx, countsdomain.ProjectionExceptionQuery{
		TenantID: in.TenantID, Status: in.Status, ParkID: in.ParkID, ShedID: in.ShedID,
		ExceptionType: in.ExceptionType, Severity: in.Severity, OwnerRef: in.OwnerRef,
		WorkState: in.WorkState, Cursor: cursor, Limit: in.Limit,
	})
	if err != nil {
		return domain.CountsProjectionExceptionList{}, err
	}
	items := make([]domain.CountsProjectionException, 0, len(out.Items))
	for _, item := range out.Items {
		items = append(items, mapCountsProjectionException(item))
	}
	return domain.CountsProjectionExceptionList{Items: items, NextCursor: out.NextCursor}, nil
}

func (s *Service) ResolveCountsProjectionException(ctx context.Context, in domain.CountsProjectionExceptionResolutionCommand) (domain.CountsProjectionExceptionResolution, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ProjectionExceptionID = strings.TrimSpace(in.ProjectionExceptionID)
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.ResolutionReason = strings.TrimSpace(in.ResolutionReason)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.ResolutionRef != nil {
		ref := strings.TrimSpace(*in.ResolutionRef)
		in.ResolutionRef = ptrIfNotEmpty(ref)
	}
	if in.TenantID == "" || in.ProjectionExceptionID == "" || in.ActorID == "" ||
		in.ResolutionReason == "" || in.IdempotencyKey == "" {
		return domain.CountsProjectionExceptionResolution{}, ErrMissingRequiredField
	}
	if len(in.IdempotencyKey) < minIdempotencyKeyLen || len(in.IdempotencyKey) > maxIdempotencyKeyLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidIdempotencyKey
	}
	if len(in.ResolutionReason) > maxResolutionReasonLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionReason
	}
	if in.ResolutionRef != nil && len(*in.ResolutionRef) > maxResolutionRefLen {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionRef
	}
	if in.Action != "resolve" && in.Action != "dismiss" {
		return domain.CountsProjectionExceptionResolution{}, ErrInvalidResolutionAction
	}
	if s.countsExceptionResolution == nil {
		return domain.CountsProjectionExceptionResolution{}, ErrCountsResolverUnavailable
	}
	out, err := s.countsExceptionResolution.ResolveProjectionException(ctx, countsdomain.ProjectionExceptionResolutionRequest{
		TenantID:              in.TenantID,
		ProjectionExceptionID: in.ProjectionExceptionID,
		Action:                in.Action,
		ResolvedByRef:         in.ActorID,
		ResolutionReason:      in.ResolutionReason,
		ResolutionRef:         in.ResolutionRef,
		IdempotencyKey:        in.IdempotencyKey,
		RequestFingerprint:    countsProjectionExceptionResolutionFingerprint(in),
	})
	if err != nil {
		return domain.CountsProjectionExceptionResolution{}, mapCountsProjectionExceptionError(err)
	}
	return domain.CountsProjectionExceptionResolution{
		ResolutionID:          out.ProjectionExceptionResolutionID,
		ProjectionExceptionID: out.ProjectionExceptionID,
		Action:                out.Action,
		Status:                out.Status,
		WorkState:             out.WorkState,
		ResolvedByRef:         out.ResolvedByRef,
		ResolutionReason:      out.ResolutionReason,
		ResolutionRef:         out.ResolutionRef,
		ResolvedAt:            out.ResolvedAt,
		Replayed:              out.Replayed,
	}, nil
}

// Readiness returns the Feed Direction build/runtime readiness contract. Until
// Counts/Shifting exposes source-backed projection readiness, this deliberately
// fails closed so no caller can treat Feed generation as safe.
func (s *Service) Readiness(ctx context.Context, tenantID string) (domain.Readiness, error) {
	checkedAt := s.clock().UTC()
	readiness := domain.Readiness{
		TenantID:          tenantID,
		Status:            domain.ReadinessBlocked,
		CurrentGate:       "G2",
		GenerationAllowed: false,
		NextAction: "Close G2 Counts/Shifting projection before generation: " +
			"source-backed Base Count anchors, ShiftingEvent ledger, one-day projection worker, " +
			"ration-context resolver, idempotency/replay proof, exceptions, observability, " +
			"bounded read models, API/UI, and E2E evidence must all pass.",
		SourcePriority:           "Feed, Shiftings and Count.docx is primary business truth; workbooks and legacy Apps Script are evidence only.",
		Gates:                    feedReadinessGates(checkedAt),
		CountsShiftingSubgates:   countsShiftingSubgates(checkedAt),
		SafetyInvariants:         feedSafetyInvariants(checkedAt),
		GeneratedDirectionsState: "disabled_until_g2_ready",
	}
	if s.countsReadiness != nil {
		countsReadiness, err := s.countsReadiness.Readiness(ctx, tenantID)
		if err != nil {
			readiness.Gates[1].BlockerReason = "Counts/Shifting readiness provider failed; Feed remains blocked."
		} else {
			applyCountsReadiness(&readiness, countsReadiness)
		}
	}
	return readiness, nil
}

func mapCountsProjectionException(in countsdomain.ProjectionException) domain.CountsProjectionException {
	evidence := json.RawMessage([]byte(`{}`))
	if len(in.EvidenceJSON) > 0 {
		evidence = append(json.RawMessage(nil), in.EvidenceJSON...)
	}
	return domain.CountsProjectionException{
		ProjectionExceptionID: in.ProjectionExceptionID,
		ProjectionSnapshotID:  in.ProjectionSnapshotID,
		ExceptionType:         in.ExceptionType,
		SourceKey:             in.SourceKey,
		GrainKey:              in.GrainKey,
		ParkID:                in.ParkID,
		ShedID:                in.ShedID,
		BreedKey:              in.BreedKey,
		StageTag:              in.StageTag,
		Severity:              in.Severity,
		Status:                in.Status,
		OwnerRef:              in.OwnerRef,
		WorkType:              in.WorkType,
		WorkState:             in.WorkState,
		DueAt:                 in.DueAt,
		NextAction:            in.NextAction,
		EvidenceLink:          in.EvidenceLink,
		BlockerReason:         in.BlockerReason,
		EvidenceJSON:          evidence,
		ResolutionID:          in.ResolutionID,
		ResolvedByRef:         in.ResolvedByRef,
		ResolutionReason:      in.ResolutionReason,
		ResolutionRef:         in.ResolutionRef,
		ResolvedAt:            in.ResolvedAt,
		CreatedAt:             in.CreatedAt,
		UpdatedAt:             in.UpdatedAt,
	}
}

func mapCountsProjectionExceptionError(err error) error {
	switch {
	case errors.Is(err, countsports.ErrIdempotencyConflict):
		return ErrIdempotencyConflict
	case errors.Is(err, countsports.ErrProjectionExceptionNotFound):
		return ErrProjectionExceptionNotFound
	case errors.Is(err, countsports.ErrProjectionExceptionClosed):
		return ErrProjectionExceptionClosed
	default:
		return err
	}
}

func countsProjectionExceptionResolutionFingerprint(in domain.CountsProjectionExceptionResolutionCommand) string {
	ref := ""
	if in.ResolutionRef != nil {
		ref = *in.ResolutionRef
	}
	parts := []string{in.TenantID, in.ProjectionExceptionID, in.Action, in.ActorID, in.ResolutionReason, ref}
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ptrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func trimOptional(v *string) *string {
	if v == nil {
		return nil
	}
	return ptrIfNotEmpty(strings.TrimSpace(*v))
}

func trimOptionalLower(v *string) *string {
	if v == nil {
		return nil
	}
	return ptrIfNotEmpty(strings.ToLower(strings.TrimSpace(*v)))
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func (s *Service) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

func feedReadinessGates(checkedAt time.Time) []domain.ReadinessGate {
	gates := []domain.ReadinessGate{
		readyGate("G1", 1, "Mock anatomy and source-backed build lane reopened", "Product/engineering", "docs/feed-direction/BUILD-TO-DONE-GOAL.md"),
		blockedGate("G2", 2, "Counts/Shifting projection", "Counts/Shifting + Feed Direction", "docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md", "Counts/Shifting storage is not enough: Feed stays blocked until source-backed Base Count anchors and the ShiftingEvent ledger are projected into immutable one-day shed/cohort snapshots with ration-context resolution, owner-visible exceptions, observability, and query-plan evidence."),
		pendingGate("G3", 3, "Clock and legacy-trigger cutover sign-off", "Feed Director + engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Must follow docx clocks; legacy triggers remain retain/retire/replace evidence only."),
		pendingGate("G4", 4, "Ration approval and provenance", "Feed Director + protocol owner", "docs/feed-direction/TRD.md", "Workbook/KT parameters must become reviewed typed protocol/config rows before generation can depend on them."),
		pendingGate("G5", 5, "Eligibility, stage-tag, pregnancy, and session-slot policy", "Feed Director + protocol owner", "docs/feed-direction/BUILD-TO-DONE-GOAL.md", "Warm-up, pregnancy/lactation, breed/tag/age/session rules need reviewed effective-dated policy."),
		pendingGate("G6", 6, "Quantity and precision boundary", "Inventory + Feed Direction", "docs/feed-direction/TRD.md", "As-fed output, inventory base units, and SQL numeric precision must be closed before publishing quantities."),
		pendingGate("G7", 7, "Generation, Diff, and stage model", "Feed Direction", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Generation rows and durable stage_kind cannot be built as runtime truth until G2-G6 are closed or fail-closed."),
		pendingGate("G8", 8, "Transport map/list provider", "Feed Direction + transport", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Transport consolidation needs approved map/checklist behavior."),
		pendingGate("G9", 9, "Packing, wastage, and typed rework thresholds", "Feed Direction + operations", "docs/feed-direction/BUILD-TO-DONE-GOAL.md", "Overpack, moist/stale leftover, refusal-to-eat, sickness risk, and variance thresholds need typed exception policy."),
		pendingGate("G10", 10, "Slack/App Script security and cutover", "Security + engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Slack bridge stays disabled unless API-only security closeout is approved."),
		pendingGate("G11", 11, "Reminder and escalation policy", "Operations + engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Stage reminders/escalations wait for stage model."),
		pendingGate("G12", 12, "Notification delivery", "Operations + engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Notifications wait for approved stage/event contracts."),
		pendingGate("G13", 13, "Missed/recovery/rework path", "Operations + engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Recovery semantics wait for durable stage states and exception policies."),
		pendingGate("G14", 14, "Audit and observability", "Engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Generation and workers need audit/outbox/metric coverage."),
		pendingGate("G15", 15, "Command-lens field mapping", "Product + engineering", "docs/feed-direction/BUILD-TO-DONE-GOAL.md", "UI lenses wait for backend-owned read models."),
		pendingGate("G16", 16, "Bounded APIs and cursor read models", "Engineering", "docs/feed-direction/DEPENDENCY-CLOSURE-TRD.md", "Hot paths require bounded indexes/cursors and plan checks."),
		pendingGate("G17", 17, "Local E2E, visual proof, and final review", "Engineering + review agents", "docs/feed-direction/BUILD-TO-DONE-GOAL.md", "Do not call done until Postgres/API/client/UI/E2E/source-parity proof passes."),
	}
	for i := range gates {
		gates[i].LastCheckedAt = checkedAt
	}
	return gates
}

func readyGate(id string, sequence int, name, owner, evidenceRef string) domain.ReadinessGate {
	return domain.ReadinessGate{
		ID: id, Sequence: sequence, Name: name, Status: domain.ReadinessReady,
		Owner: owner, EvidenceRef: evidenceRef, AllowsBuild: true, AllowsGenerate: false,
	}
}

func blockedGate(id string, sequence int, name, owner, evidenceRef, blocker string) domain.ReadinessGate {
	return domain.ReadinessGate{
		ID: id, Sequence: sequence, Name: name, Status: domain.ReadinessBlocked,
		Owner: owner, EvidenceRef: evidenceRef, BlockerReason: blocker, AllowsBuild: true, AllowsGenerate: false,
	}
}

func pendingGate(id string, sequence int, name, owner, evidenceRef, blocker string) domain.ReadinessGate {
	return domain.ReadinessGate{
		ID: id, Sequence: sequence, Name: name, Status: domain.ReadinessPending,
		Owner: owner, EvidenceRef: evidenceRef, BlockerReason: blocker, AllowsBuild: false, AllowsGenerate: false,
	}
}

func countsShiftingSubgates(checkedAt time.Time) []domain.CountsShiftingSubgate {
	items := []struct {
		id      string
		blocker string
	}{
		{"CSG1", "Physical Base Count import/adoption and discrepancy workflow are not proven."},
		{"CSG2", "Source-backed ShiftingEvent ingestion and structured source/destination/cohort ledger are not proven."},
		{"CSG3", "Structured cohort/stage impact is not proven for every movement."},
		{"CSG4", "Realized count_as_of and one-day projected_count_for horizon split are not proven."},
		{"CSG5", "Immediate physical Base Count adoption plus discrepancy investigation is not proven."},
		{"CSG6", "Unreported-shifting and count-mismatch detection are not proven."},
		{"CSG7", "Owner-approved breed/stage alias mapping coverage is not complete."},
		{"CSG8", "Idempotency/replay across ingestion, projection, and source replay is not proven."},
		{"CSG9", "Feed projection API over bounded immutable rows is not proven."},
		{"CSG10", "Scale, observability, source parity, and seeded E2E are not proven."},
	}
	subgates := make([]domain.CountsShiftingSubgate, 0, len(items))
	for i, item := range items {
		subgates = append(subgates, domain.CountsShiftingSubgate{
			ID: item.id, Sequence: i + 1, Name: csgName(item.id), Status: domain.ReadinessBlocked,
			Owner: "Counts/Shifting + Feed Direction", EvidenceRef: "docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md",
			BlockerReason: item.blocker, LastCheckedAt: checkedAt, AllowsGenerate: false,
		})
	}
	return subgates
}

func applyCountsReadiness(readiness *domain.Readiness, counts countsdomain.Readiness) {
	if len(readiness.Gates) >= 2 {
		g2 := &readiness.Gates[1]
		g2.Status = feedReadinessStatus(counts.Status)
		g2.AllowsGenerate = false
		if counts.Status == countsdomain.ReadinessReady {
			g2.BlockerReason = ""
			readiness.CurrentGate = "G3"
		} else if counts.OpenExceptionCount > 0 {
			g2.BlockerReason = countsReadinessBlockerSummary(counts, "Counts/Shifting has open projection exceptions; Feed generation remains blocked.")
		} else {
			g2.BlockerReason = countsReadinessBlockerSummary(counts, "Counts/Shifting subgates are not ready; Feed generation remains blocked.")
		}
	}
	if len(counts.Subgates) == 0 {
		return
	}
	subgates := make([]domain.CountsShiftingSubgate, 0, len(counts.Subgates))
	for i, subgate := range counts.Subgates {
		subgates = append(subgates, domain.CountsShiftingSubgate{
			ID: subgate.ID, Sequence: i + 1, Name: csgName(subgate.ID),
			Status: feedReadinessStatus(subgate.Status), Owner: subgate.Owner,
			EvidenceRef: subgate.EvidenceRef, BlockerReason: subgate.BlockerReason,
			LastCheckedAt: subgate.LastCheckedAt, AllowsGenerate: false,
			RecentEvidence: countsRecentEvidence(subgate.RecentEvidence),
		})
	}
	readiness.CountsShiftingSubgates = subgates
}

func countsRecentEvidence(in []countsdomain.ReadinessEvidence) []domain.ReadinessEvidence {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.ReadinessEvidence, 0, len(in))
	for _, evidence := range in {
		out = append(out, domain.ReadinessEvidence{
			Status:            feedReadinessStatus(evidence.Status),
			EvidenceRef:       evidence.EvidenceRef,
			BlockerReason:     evidence.BlockerReason,
			ImplementationRef: evidence.ImplementationRef,
			RecordedAt:        evidence.RecordedAt,
		})
	}
	return out
}

func countsReadinessBlockerSummary(counts countsdomain.Readiness, prefix string) string {
	const maxDetails = 4
	details := make([]string, 0, maxDetails)
	for _, subgate := range counts.Subgates {
		if subgate.Status == countsdomain.ReadinessReady {
			continue
		}
		detail := subgate.ID + "=" + string(subgate.Status)
		if reason := strings.TrimSpace(subgate.BlockerReason); reason != "" {
			detail += " (" + reason + ")"
		}
		details = append(details, detail)
		if len(details) == maxDetails {
			break
		}
	}
	if len(details) == 0 {
		return prefix
	}
	remaining := nonReadySubgateCount(counts.Subgates) - len(details)
	summary := prefix + " Non-ready Counts/Shifting subgates: " + strings.Join(details, "; ")
	if remaining > 0 {
		summary += fmt.Sprintf("; +%d more", remaining)
	}
	return summary
}

func nonReadySubgateCount(subgates []countsdomain.ReadinessSubgate) int {
	count := 0
	for _, subgate := range subgates {
		if subgate.Status != countsdomain.ReadinessReady {
			count++
		}
	}
	return count
}

func feedReadinessStatus(status countsdomain.ReadinessStatus) domain.ReadinessStatus {
	switch status {
	case countsdomain.ReadinessReady:
		return domain.ReadinessReady
	case countsdomain.ReadinessPending:
		return domain.ReadinessPending
	default:
		return domain.ReadinessBlocked
	}
}

func csgName(id string) string {
	switch id {
	case "CSG1":
		return "Base Count anchor"
	case "CSG2":
		return "ShiftingEvent ledger"
	case "CSG3":
		return "Structured impacts"
	case "CSG4":
		return "Horizon split"
	case "CSG5":
		return "Base Count adoption"
	case "CSG6":
		return "Unreported-shifting detection"
	case "CSG7":
		return "Alias normalization"
	case "CSG8":
		return "Idempotency and replay"
	case "CSG9":
		return "Projection API"
	case "CSG10":
		return "Scale and observability proof"
	default:
		return id
	}
}

func feedSafetyInvariants(checkedAt time.Time) []domain.SafetyInvariant {
	items := []struct {
		key     string
		owner   string
		blocker string
	}{
		{
			key:   "shifted_pregnant_destination_recompute",
			owner: "Feed Director + Counts/Shifting + Protocol",
			blocker: "Pregnant/lactating/warm-up animals shifted into a destination shed must re-resolve pregnancy, " +
				"lactation, warm-up, age/stage, breed alias, shed tag, and ration context before any Feed direction can be generated.",
		},
		{
			key:     "destination_shed_shortage_fail_closed",
			owner:   "Feed Direction + operations",
			blocker: "If destination-shed recalculation shows shortage, stale context, or missing reviewed ration context, generation is blocked and escalated instead of averaging normal shed feed.",
		},
		{
			key:     "overfeed_wastage_moist_feed_exception",
			owner:   "Feed Direction + operations",
			blocker: "Overpacking, unsafe surplus, moist/stale leftover feed, refusal-to-eat, or sickness-risk signals must create typed exception/rework and cannot be treated as harmless surplus.",
		},
		{
			key:     "bounded_projection_no_full_herd_scan",
			owner:   "Engineering",
			blocker: "Feed readiness must come from bounded shed/cohort projection snapshots and indexed read models, not per-request full-herd scans.",
		},
	}
	invariants := make([]domain.SafetyInvariant, 0, len(items))
	for _, item := range items {
		invariants = append(invariants, domain.SafetyInvariant{
			Key: item.key, Status: domain.ReadinessBlocked, Owner: item.owner,
			EvidenceRef: "docs/feed-direction/BUILD-TO-DONE-GOAL.md", BlockerReason: item.blocker,
			LastCheckedAt: checkedAt, AllowsGenerate: false,
		})
	}
	return invariants
}
