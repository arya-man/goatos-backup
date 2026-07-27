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
	verificationports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

type Service struct {
	repo  ports.Repository
	now   func() time.Time
	media verificationports.MediaResolver
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

func (s *Service) WithMediaResolver(media verificationports.MediaResolver) *Service {
	s.media = media
	return s
}

func (s *Service) ActionCenter(ctx context.Context, q domain.Query) (domain.ActionCenterResponse, error) {
	q = s.defaults(q)
	result, err := s.repo.ListRows(ctx, q)
	if err != nil {
		return domain.ActionCenterResponse{}, err
	}
	result.Rows = s.withEvidenceMedia(ctx, q.TenantID, result.Rows)
	return domain.ActionCenterResponse{
		Source:            domain.SourceAPI,
		Items:             result.Rows,
		CountsByWorkState: result.CountsByWorkState,
		TotalCount:        result.TotalCount,
		NextCursor:        result.NextCursor,
		Projection:        result.Projection,
	}, nil
}

func (s *Service) ActionCenterCounts(ctx context.Context, q domain.Query) (domain.ActionCenterCountsResponse, error) {
	q = s.defaults(q)
	result, err := s.repo.ListRows(ctx, q)
	if err != nil {
		return domain.ActionCenterCountsResponse{}, err
	}
	return domain.ActionCenterCountsResponse{
		Source:            domain.SourceAPI,
		CountsByWorkState: result.CountsByWorkState,
		TotalCount:        result.TotalCount,
		Projection:        result.Projection,
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
	result.Rows = s.withEvidenceMedia(ctx, q.TenantID, result.Rows)
	rows := make([]domain.AdherenceRow, 0, len(result.Rows))
	summary := result.AdherenceSummary
	for _, row := range result.Rows {
		rows = append(rows, domain.AdherenceRow{
			RowID:                   row.RowID,
			Expected:                expectedText(row),
			Actual:                  actualText(row),
			Gap:                     gapText(row),
			Severity:                row.Severity,
			Owner:                   row.Owner,
			NextAction:              row.NextAction,
			Evidence:                row.Evidence,
			WorkState:               row.WorkState,
			DriveCapacityState:      row.DriveCapacityState,
			DriveAnimalsRequired:    row.DriveAnimalsRequired,
			DriveAnimalsAssigned:    row.DriveAnimalsAssigned,
			DriveOperatorCap:        row.DriveOperatorCap,
			DriveAvailableOperators: row.DriveAvailableOperators,
			DriveLatestSafeDate:     row.DriveLatestSafeDate,
			DriveMedicalDeferReason: row.DriveMedicalDeferReason,
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
		Projection: result.Projection,
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
	result.Rows = s.withEvidenceMedia(ctx, q.TenantID, result.Rows)

	summaryCounts := result.CountsByWorkState
	if !controlTowerCanReuseAlertCounts(q) {
		summaryQuery := q
		summaryQuery.WorkState = nil
		summaryQuery.Severity = nil
		summaryQuery.OwnerID = nil
		summaryQuery.Cursor = nil
		summaryQuery.Limit = 1
		summaryCounts, err = s.repo.CountByWorkState(ctx, summaryQuery)
		if err != nil {
			return domain.ControlTowerResponse{}, err
		}
	}
	summary := domain.ControlTowerSummary{ProcessIntact: true}
	for _, c := range summaryCounts {
		switch c.WorkState {
		case domain.WorkStateRejected, domain.WorkStateBlocked:
			summary.CriticalCount += int(c.Count)
			summary.OpenGapCount += int(c.Count)
		case domain.WorkStateOverdue, domain.WorkStateMissed, domain.WorkStateProofPending, domain.WorkStateVerificationPending:
			summary.WarningCount += int(c.Count)
			summary.OpenGapCount += int(c.Count)
		}
		if c.WorkState == domain.WorkStateVerificationPending {
			summary.VerificationBacklog += int(c.Count)
		}
		if c.WorkState == domain.WorkStateBlocked {
			summary.ConfigOrSOPBlockers += int(c.Count)
		}
	}
	summary.ProcessIntact = summary.OpenGapCount == 0
	alerts := make([]domain.ControlTowerAlert, 0, len(result.Rows))
	for _, row := range result.Rows {
		alerts = append(alerts, domain.ControlTowerAlert{
			RowID:                   row.RowID,
			Severity:                row.Severity,
			WorkState:               row.WorkState,
			Title:                   alertTitle(row),
			Detail:                  alertDetail(row),
			ParkID:                  row.ParkID,
			ParkName:                row.ParkName,
			ShedID:                  row.ShedID,
			ShedName:                row.ShedName,
			DriveName:               row.DriveName,
			Owner:                   row.Owner,
			NextAction:              row.NextAction,
			EvidenceLink:            workflowLink(row),
			DriveCapacityState:      row.DriveCapacityState,
			DriveAnimalsRequired:    row.DriveAnimalsRequired,
			DriveAnimalsAssigned:    row.DriveAnimalsAssigned,
			DriveOperatorCap:        row.DriveOperatorCap,
			DriveAvailableOperators: row.DriveAvailableOperators,
			DriveLatestSafeDate:     row.DriveLatestSafeDate,
			DriveMedicalDeferReason: row.DriveMedicalDeferReason,
			ObligationID:            row.ObligationID,
		})
	}
	return domain.ControlTowerResponse{Source: domain.SourceAPI, Summary: summary, Alerts: alerts, TotalCount: result.TotalCount, NextCursor: result.NextCursor, Projection: result.Projection}, nil
}

func controlTowerCanReuseAlertCounts(q domain.Query) bool {
	return q.WorkState == nil && q.Severity == nil && q.OwnerID == nil
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
	row = s.withEvidenceMedia(ctx, q.TenantID, []domain.Row{row})[0]
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

func (s *Service) withEvidenceMedia(ctx context.Context, tenantID string, rows []domain.Row) []domain.Row {
	if s.media == nil || tenantID == "" || len(rows) == 0 {
		return rows
	}
	proofIDs := make([]string, 0)
	seen := map[string]struct{}{}
	for _, row := range rows {
		for _, id := range row.Evidence.ProofIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			proofIDs = append(proofIDs, id)
		}
	}
	if len(proofIDs) == 0 {
		return rows
	}
	resolved, err := s.media.ResolveMedia(ctx, tenantID, proofIDs)
	if err != nil {
		msg := "proof media lookup failed"
		for i := range rows {
			if len(rows[i].Evidence.ProofIDs) > 0 {
				rows[i].Evidence.MediaResolutionError = &msg
			}
		}
		return rows
	}
	byID := make(map[string]domain.MediaItem, len(resolved))
	for _, item := range resolved {
		byID[item.ProofID] = domain.MediaItem{
			ProofID:     item.ProofID,
			DownloadURL: item.DownloadURL,
			MimeType:    item.MimeType,
			DurationMS:  item.DurationMS,
		}
	}
	for i := range rows {
		if len(rows[i].Evidence.ProofIDs) == 0 {
			continue
		}
		media := make([]domain.MediaItem, 0, len(rows[i].Evidence.ProofIDs))
		for _, id := range rows[i].Evidence.ProofIDs {
			if item, ok := byID[id]; ok {
				media = append(media, item)
			}
		}
		if len(media) > 0 {
			rows[i].Evidence.Media = media
		}
	}
	return rows
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
	if q.WorkState != nil && *q.WorkState == domain.WorkStateCompleted {
		q.IncludeCompleted = true
	}
	return q
}

func expectedText(row domain.Row) string {
	if row.DriveCapacityState == domain.DriveCapacityStateOverCapRequired {
		return fmt.Sprintf("%s %s: %d animals must finish by %s", row.ProtocolName, row.DoseCode, max(row.DriveAnimalsRequired, row.ExpectedCount), latestSafeOrDue(row))
	}
	return fmt.Sprintf("%s %s: %d due by %s", row.ProtocolName, row.DoseCode, row.ExpectedCount, biztime.BusinessDate(row.DueAt))
}

func actualText(row domain.Row) string {
	switch row.DriveCapacityState {
	case domain.DriveCapacityStateOverCapRequired:
		slots := row.DriveAvailableOperators * row.DriveOperatorCap
		if slots > 0 && row.DriveAnimalsAssigned <= slots {
			return fmt.Sprintf("%d assigned within %d planned operator slots", row.DriveAnimalsAssigned, slots)
		}
		if slots > 0 {
			return fmt.Sprintf("%d assigned against %d planned operator slots; add capacity or split the drive", row.DriveAnimalsAssigned, slots)
		}
		return fmt.Sprintf("%d assigned with no planned operator capacity", row.DriveAnimalsAssigned)
	case domain.DriveCapacityStateMedicalDefer:
		if row.DriveMedicalDeferReason != nil && *row.DriveMedicalDeferReason != "" {
			return "medically deferred: " + *row.DriveMedicalDeferReason
		}
		return "medically deferred"
	case domain.DriveCapacityStateTerminalAnimalClosed:
		if row.DriveMedicalDeferReason != nil && *row.DriveMedicalDeferReason != "" {
			return "closed terminal animal: " + *row.DriveMedicalDeferReason
		}
		return "closed terminal animal"
	}
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
	switch row.DriveCapacityState {
	case domain.DriveCapacityStateOverCapRequired:
		slots := row.DriveAvailableOperators * row.DriveOperatorCap
		if slots > 0 && row.DriveAnimalsAssigned <= slots {
			return "none"
		}
		return "capacity_shortfall"
	case domain.DriveCapacityStateMedicalDefer:
		return "medical_defer"
	case domain.DriveCapacityStateTerminalAnimalClosed:
		return "terminal_animal_closed"
	}
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
		if title := humanAlertTitle(*row.DriveName); title != "" {
			return title
		}
	}
	return humanAlertTitle(strings.TrimSpace(row.ProtocolName + " " + row.DoseCode))
}

func humanAlertTitle(raw string) string {
	title := strings.TrimSpace(raw)
	title = strings.TrimPrefix(title, "Preventive Care Vaccination Matrix -")
	title = strings.TrimPrefix(title, "Preventive Care Vaccination Matrix")
	return strings.TrimSpace(title)
}

func alertDetail(row domain.Row) string {
	base := fmt.Sprintf("%s / %s", row.ParkName, row.ShedName)
	switch row.DriveCapacityState {
	case domain.DriveCapacityStateOverCapRequired:
		return fmt.Sprintf("%s: %d animals assigned over %d operator slots; latest safe %s", base, row.DriveAnimalsAssigned, row.DriveAvailableOperators*row.DriveOperatorCap, latestSafeOrDue(row))
	case domain.DriveCapacityStateMedicalDefer, domain.DriveCapacityStateTerminalAnimalClosed:
		if row.DriveMedicalDeferReason != nil && *row.DriveMedicalDeferReason != "" {
			return base + ": " + *row.DriveMedicalDeferReason
		}
		return base + ": " + string(row.DriveCapacityState)
	}
	if row.BlockerReason != nil && *row.BlockerReason != "" {
		return base + ": " + *row.BlockerReason
	}
	return base + ": " + row.GapType
}

func latestSafeOrDue(row domain.Row) string {
	if row.DriveLatestSafeDate != nil && !row.DriveLatestSafeDate.IsZero() {
		return biztime.BusinessDate(*row.DriveLatestSafeDate)
	}
	return biztime.BusinessDate(row.DueAt)
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
