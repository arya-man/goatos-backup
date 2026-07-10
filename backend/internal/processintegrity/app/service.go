// Package app coordinates process-integrity read use-cases.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/processintegrity/ports"
)

type Service struct {
	repo ports.Repository
	now  func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().In(biztime.DefaultLocation()) }}
}

func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Service) ActionCenter(ctx context.Context, q domain.Query) (domain.ActionCenterResponse, error) {
	q = s.defaults(q)
	result, err := s.repo.ListRows(ctx, q)
	if err != nil {
		return domain.ActionCenterResponse{}, err
	}
	return domain.ActionCenterResponse{
		Source:            domain.SourceAPI,
		Items:             result.Rows,
		CountsByWorkState: result.CountsByWorkState,
		TotalCount:        result.TotalCount,
		NextCursor:        result.NextCursor,
	}, nil
}

func (s *Service) ProtocolAdherence(ctx context.Context, q domain.Query) (domain.ProtocolAdherenceResponse, error) {
	q = s.defaults(q)
	q.IncludeCompleted = true
	q.IncludeAdherenceSummary = true
	result, err := s.repo.ListRows(ctx, q)
	if err != nil {
		return domain.ProtocolAdherenceResponse{}, err
	}
	rows := make([]domain.AdherenceRow, 0, len(result.Rows))
	summary := result.AdherenceSummary
	for _, row := range result.Rows {
		rows = append(rows, domain.AdherenceRow{
			RowID:      row.RowID,
			Expected:   expectedText(row),
			Actual:     actualText(row),
			Gap:        gapText(row),
			Severity:   row.Severity,
			Owner:      row.Owner,
			NextAction: row.NextAction,
			Evidence:   row.Evidence,
			WorkState:  row.WorkState,
		})
	}
	if summary.ExpectedCount > 0 {
		summary.AdherencePercent = float64(summary.CompletedCount) / float64(summary.ExpectedCount) * 100
	}
	return domain.ProtocolAdherenceResponse{
		Source:     domain.SourceAPI,
		Summary:    summary,
		Rows:       rows,
		TotalCount: result.TotalCount,
		NextCursor: result.NextCursor,
	}, nil
}

func (s *Service) ControlTower(ctx context.Context, q domain.Query) (domain.ControlTowerResponse, error) {
	q = s.defaults(q)
	q.OnlyBrokenOrAtRisk = true
	q.IncludeCompleted = false
	if q.Limit <= 0 {
		q.Limit = 50
	}
	alertQuery := q
	result, err := s.repo.ListRows(ctx, alertQuery)
	if err != nil {
		return domain.ControlTowerResponse{}, err
	}
	summaryQuery := q
	summaryQuery.WorkState = nil
	summaryQuery.Severity = nil
	summaryQuery.OwnerID = nil
	summaryQuery.Offset = 0
	summaryQuery.Cursor = nil
	summaryQuery.Limit = 1
	summaryResult, err := s.repo.ListRows(ctx, summaryQuery)
	if err != nil {
		return domain.ControlTowerResponse{}, err
	}
	summary := domain.ControlTowerSummary{ProcessIntact: true}
	for _, c := range summaryResult.CountsByWorkState {
		switch c.WorkState {
		case domain.WorkStateRejected, domain.WorkStateBlocked, domain.WorkStateOwnerMissing:
			summary.CriticalCount += int(c.Count)
			summary.OpenGapCount += int(c.Count)
		case domain.WorkStateOverdue, domain.WorkStateMissed, domain.WorkStateProofPending, domain.WorkStateVerificationPending:
			summary.WarningCount += int(c.Count)
			summary.OpenGapCount += int(c.Count)
		}
		if c.WorkState == domain.WorkStateVerificationPending {
			summary.VerificationBacklog += int(c.Count)
		}
		if c.WorkState == domain.WorkStateOwnerMissing {
			summary.OwnerMissingCount += int(c.Count)
		}
		if c.WorkState == domain.WorkStateBlocked {
			summary.ConfigOrSOPBlockers += int(c.Count)
		}
	}
	summary.ProcessIntact = summary.OpenGapCount == 0
	alerts := make([]domain.ControlTowerAlert, 0, len(result.Rows))
	for _, row := range result.Rows {
		alerts = append(alerts, domain.ControlTowerAlert{
			RowID:        row.RowID,
			Severity:     row.Severity,
			WorkState:    row.WorkState,
			Title:        alertTitle(row),
			Detail:       alertDetail(row),
			ParkID:       row.ParkID,
			ParkName:     row.ParkName,
			ShedID:       row.ShedID,
			ShedName:     row.ShedName,
			DriveName:    row.DriveName,
			Owner:        row.Owner,
			NextAction:   row.NextAction,
			EvidenceLink: workflowLink(row),
			ObligationID: row.ObligationID,
		})
	}
	return domain.ControlTowerResponse{Source: domain.SourceAPI, Summary: summary, Alerts: alerts, TotalCount: result.TotalCount, NextCursor: result.NextCursor}, nil
}

func (s *Service) WorkflowDrilldown(ctx context.Context, q domain.Query, rowID string) (domain.WorkflowDrilldownResponse, bool, error) {
	if strings.TrimSpace(rowID) == "" {
		return domain.WorkflowDrilldownResponse{}, false, nil
	}
	q = s.defaults(q)
	q.IncludeCompleted = true
	row, found, err := s.repo.GetRow(ctx, q, rowID)
	if err != nil || !found {
		return domain.WorkflowDrilldownResponse{}, found, err
	}
	if row.Category == domain.CategoryFeedDirection {
		return domain.WorkflowDrilldownResponse{Source: domain.SourceAPI, Row: row, Nodes: feedDirectionExceptionNodes(row)}, true, nil
	}
	nodes := []domain.WorkflowNode{
		{Key: "config_published", Label: "Config published", State: publishedState(row), Timestamp: nil, Owner: row.Owner.EscalationOwnerName},
		{Key: "obligation_generated", Label: "Obligation generated", State: stateFromBool(true, "generated", "missing"), Timestamp: &row.DueAt},
		{Key: "batch_opened", Label: "Batch / drive opened", State: nullableState(row.BatchStatus, "not_started"), Timestamp: row.WindowStart, Owner: row.Owner.OperatorName},
		{Key: "sop_task", Label: "SOP task", State: string(row.SOPTaskState), Timestamp: nil, Owner: row.Owner.OperatorName},
		{Key: "proof_uploaded", Label: "Proof uploaded", State: string(row.ProofState), Timestamp: row.Evidence.LatestEvidenceAt, Evidence: firstProof(row)},
		{Key: "verification", Label: "Verification", State: string(row.VerificationState), Timestamp: row.Evidence.LatestEvidenceAt, Actor: row.Owner.VerifierName, Blocker: row.Evidence.LatestRejectionReason},
		{Key: "completion", Label: "Completion", State: completionNodeState(row), Timestamp: nil, Evidence: row.CompletionID},
		{Key: "next_due", Label: "Booster / next dose basis", State: nextDueState(row), Timestamp: nil},
	}
	return domain.WorkflowDrilldownResponse{Source: domain.SourceAPI, Row: row, Nodes: nodes}, true, nil
}

func workflowLink(row domain.Row) string {
	switch row.Category {
	case domain.CategoryFeedDirection:
		return "/workflows/" + row.RowID + "?category=feed_direction"
	default:
		return "/workflows/" + row.RowID
	}
}

func feedDirectionExceptionNodes(row domain.Row) []domain.WorkflowNode {
	return []domain.WorkflowNode{
		{Key: "counts_projection", Label: "Counts/Shifting projection", State: stateFromBool(row.ProcessKey != "", "projected", "missing"), Timestamp: &row.DueAt},
		{Key: "exception_open", Label: "Exception opened", State: string(row.WorkState), Timestamp: row.Evidence.LatestEvidenceAt, Evidence: row.Evidence.AuditRef},
		{Key: "owner_review", Label: "Owner review", State: string(row.OwnerState), Timestamp: nil, Owner: row.Owner.EscalationOwnerName, Blocker: row.BlockerReason},
		{Key: "resolution", Label: "Resolution", State: completionNodeState(row), Timestamp: nil, Evidence: row.CompletionID},
	}
}

func (s *Service) defaults(q domain.Query) domain.Query {
	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = s.now()
	}
	q.AsOf = asOf
	if q.DueBefore.IsZero() {
		q.DueBefore = asOf.Add(30 * 24 * time.Hour)
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	return q
}

func expectedText(row domain.Row) string {
	return fmt.Sprintf("%s %s: %d due by %s", row.ProtocolName, row.DoseCode, row.ExpectedCount, biztime.BusinessDate(row.DueAt))
}

func actualText(row domain.Row) string {
	switch row.WorkState {
	case domain.WorkStateCompleted:
		return fmt.Sprintf("%d completed and verified", row.CompletedCount)
	case domain.WorkStateVerificationPending:
		return fmt.Sprintf("%d proof record(s) awaiting verification", row.ProofCount)
	case domain.WorkStateRejected:
		return fmt.Sprintf("%d rejected proof/completion record(s)", row.RejectedCount)
	case domain.WorkStateDeferred:
		return fmt.Sprintf("%d deferred/explained", max(row.DeferredCount, 1))
	case domain.WorkStateMissed:
		return "missed deadline"
	default:
		if row.CompletedCount > 0 {
			return fmt.Sprintf("%d completed; %d still open", row.CompletedCount, row.ExpectedCount-row.CompletedCount)
		}
		return "not completed"
	}
}

func gapText(row domain.Row) string {
	if row.WorkState == domain.WorkStateDeferred && row.GapType != "" {
		return row.GapType
	}
	if row.ProcessIntact {
		return "none"
	}
	if row.GapType != "" {
		return row.GapType
	}
	return string(row.WorkState)
}

func alertTitle(row domain.Row) string {
	if row.DriveName != nil && *row.DriveName != "" {
		return *row.DriveName
	}
	return strings.TrimSpace(row.ProtocolName + " " + row.DoseCode)
}

func alertDetail(row domain.Row) string {
	base := fmt.Sprintf("%s / %s", row.ParkName, row.ShedName)
	if row.BlockerReason != nil && *row.BlockerReason != "" {
		return base + ": " + *row.BlockerReason
	}
	return base + ": " + row.GapType
}

func publishedState(row domain.Row) string {
	if row.ProtocolVersionID != "" {
		return "published"
	}
	return "missing"
}

func nullableState(v *string, fallback string) string {
	if v == nil || *v == "" {
		return fallback
	}
	return *v
}

func stateFromBool(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func firstProof(row domain.Row) *string {
	if len(row.Evidence.ProofIDs) == 0 {
		return nil
	}
	return &row.Evidence.ProofIDs[0]
}

func completionNodeState(row domain.Row) string {
	if row.CompletionState != nil && *row.CompletionState != "" {
		return *row.CompletionState
	}
	if row.CompletedCount > 0 {
		return "completed"
	}
	return "open"
}

func nextDueState(row domain.Row) string {
	if row.WorkState == domain.WorkStateCompleted {
		return "ready_from_accepted_completion"
	}
	return "waiting_for_accepted_completion"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
