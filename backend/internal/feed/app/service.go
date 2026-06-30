// Package app holds the feed-direction application service.
package app

import (
	"context"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
	"github.com/vgoats/goatos/backend/internal/feed/ports"
)

type countsReadinessReader interface {
	Readiness(ctx context.Context, tenantID string) (countsdomain.Readiness, error)
}

// Service coordinates feed-direction use-cases over the repository boundary.
type Service struct {
	repo            ports.Repository
	countsReadiness countsReadinessReader
	now             func() time.Time
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
			g2.BlockerReason = "Counts/Shifting has open projection exceptions; Feed generation remains blocked."
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
		})
	}
	readiness.CountsShiftingSubgates = subgates
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
