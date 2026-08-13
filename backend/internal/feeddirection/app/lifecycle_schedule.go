package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// AdvanceScheduledLifecycle advances every configured park/workflow for the
// next feed day through the durable issue -> amend -> lock lifecycle. The
// persisted issue row is the retry cursor: a restarted worker resumes from the
// stored state and never repeats a completed transition.
func (s *Service) AdvanceScheduledLifecycle(ctx context.Context, tenantID string, asOf time.Time) ([]LifecycleReport, error) {
	if s.issues == nil || s.schedule == nil {
		return nil, fmt.Errorf("feeddirection: issue store and schedule reader are required for scheduled lifecycle")
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("feeddirection: tenant id is required for scheduled lifecycle")
	}
	if asOf.IsZero() {
		asOf = s.now()
	}
	asOf = asOf.In(biztime.DefaultLocation())
	feedDay := biztime.BusinessDate(biztime.BusinessDayStart(asOf).AddDate(0, 0, 1))

	parks, err := s.schedule.ListScheduledParks(ctx, tenantID, asOf)
	if err != nil {
		return nil, err
	}
	reports := make([]LifecycleReport, 0)
	for _, parkID := range parks {
		clocks, err := s.schedule.ListScheduleClocks(ctx, tenantID, parkID, asOf)
		if err != nil {
			return nil, err
		}
		for _, clock := range clocks {
			workflowReports, err := s.advanceScheduledWorkflow(ctx, tenantID, parkID, feedDay, clock, asOf)
			if err != nil {
				return nil, fmt.Errorf("feeddirection: advance park=%s workflow=%s: %w", parkID, clock.Workflow, err)
			}
			reports = append(reports, workflowReports...)
		}
	}
	return reports, nil
}

func (s *Service) advanceScheduledWorkflow(
	ctx context.Context,
	tenantID string,
	parkID string,
	feedDay string,
	clock domain.WorkflowClock,
	asOf time.Time,
) ([]LifecycleReport, error) {
	issueAt, err := clock.ExpectedIssueInstant(feedDay)
	if err != nil {
		return nil, err
	}
	if asOf.Before(issueAt) {
		return nil, nil
	}

	headers, err := s.issues.LoadIssueHeaders(ctx, tenantID, parkID, feedDay, clock.Workflow)
	if err != nil {
		return nil, err
	}
	if len(headers) > 1 {
		return nil, fmt.Errorf("expected at most one issue header, got %d", len(headers))
	}

	req := IssueRequest{TenantID: tenantID, ParkID: parkID, Workflow: clock.Workflow, AsOf: asOf}
	reports := make([]LifecycleReport, 0, 3)
	state := ""
	if len(headers) == 1 {
		state = headers[0].State
	}
	if state == domain.IssueStateLocked {
		return reports, nil
	}

	if state == "" {
		report, err := s.IssueDirection(ctx, req)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
		state = report.Header.State
	}

	correctionAt, err := clock.ExpectedCorrectionInstant(feedDay)
	if err != nil {
		return nil, err
	}
	if !asOf.Before(correctionAt) && state == domain.IssueStateIssued {
		report, err := s.AmendDirection(ctx, req)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
		state = report.Header.State
	}

	transportAt, err := clock.ExpectedTransportInstant(feedDay)
	if err != nil {
		return nil, err
	}
	if transportAt != nil && !asOf.Before(*transportAt) && state != domain.IssueStateLocked {
		report, err := s.LockDirection(ctx, req)
		if err != nil {
			return nil, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}
