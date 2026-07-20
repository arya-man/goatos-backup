package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// This file owns the issued-sheet LIFECYCLE (issue / amend / lock) and the SERVE path that reads the
// frozen rows. The generation math is reused verbatim from service.go's generate() -- these
// operations freeze and serve its output, they never recompute it differently.

// IssueRequest addresses one (park, workflow) on the feed day derived from AsOf.
//
// Feed for day D is produced on D-1, so every operation computes feed_day = businessDate(AsOf)+1: on
// business day X the issue, correction and lock all act on feed_day X+1.
type IssueRequest struct {
	TenantID string
	ParkID   string
	Workflow string
	// AsOf is the business instant the operation runs at. Zero means now. The lifecycle *_at
	// timestamps are stamped from it, so a pinned-clock test is deterministic.
	AsOf time.Time
}

// LifecycleReport is the outcome of an issue/amend/lock operation.
type LifecycleReport struct {
	Header          domain.IssueHeader
	FeedDay         string
	Workflow        string
	Outcome         string
	AffectedShedIDs []string
}

// IssueDirection ISSUES (or exactly-replays, or re-issues in place) one workflow's sheet for the
// feed day, freezing the generated rows. See ports.IssueStore.PersistIssue for the idempotency and
// re-issue semantics.
func (s *Service) IssueDirection(ctx context.Context, req IssueRequest) (LifecycleReport, error) {
	prep, err := s.prepareLifecycle(ctx, req, true)
	if err != nil {
		return LifecycleReport{}, err
	}
	result, err := s.issues.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID:       req.TenantID,
		ParkID:         req.ParkID,
		FeedDay:        prep.feedDay,
		Workflow:       prep.workflow,
		IssuedAt:       prep.asOf,
		Fingerprint:    prep.fingerprint,
		IdempotencyKey: issueIdempotencyKey(req.TenantID, req.ParkID, prep.feedDay, prep.workflow),
		GeneratedBy:    s.generatedBy,
		Cells:          prep.cells,
	})
	if err != nil {
		return LifecycleReport{}, err
	}
	return LifecycleReport{Header: result.Header, FeedDay: prep.feedDay, Workflow: prep.workflow, Outcome: result.Outcome}, nil
}

// AmendDirection recomputes the feed day's sheet, diffs it against the stored one, and persists an
// amendment for the AFFECTED SHEDS ONLY. A no-op amend still records that the correction ran. It is
// refused once the sheet is locked.
func (s *Service) AmendDirection(ctx context.Context, req IssueRequest) (LifecycleReport, error) {
	prep, err := s.prepareLifecycle(ctx, req, true)
	if err != nil {
		return LifecycleReport{}, err
	}
	result, err := s.issues.AmendIssue(ctx, ports.AmendIssueCommand{
		TenantID:    req.TenantID,
		ParkID:      req.ParkID,
		FeedDay:     prep.feedDay,
		Workflow:    prep.workflow,
		AmendedAt:   prep.asOf,
		Fingerprint: prep.fingerprint,
		Cells:       prep.cells,
	})
	if err != nil {
		return LifecycleReport{}, err
	}
	return LifecycleReport{
		Header: result.Header, FeedDay: prep.feedDay, Workflow: prep.workflow,
		Outcome: result.Outcome, AffectedShedIDs: result.AffectedShedIDs,
	}, nil
}

// LockDirection LOCKS the feed day's sheet: no further change, later changes roll to the next feed
// day. Idempotent.
func (s *Service) LockDirection(ctx context.Context, req IssueRequest) (LifecycleReport, error) {
	prep, err := s.prepareLifecycle(ctx, req, false)
	if err != nil {
		return LifecycleReport{}, err
	}
	result, err := s.issues.LockIssue(ctx, ports.LockIssueCommand{
		TenantID: req.TenantID,
		ParkID:   req.ParkID,
		FeedDay:  prep.feedDay,
		Workflow: prep.workflow,
		LockedAt: prep.asOf,
	})
	if err != nil {
		return LifecycleReport{}, err
	}
	return LifecycleReport{Header: result.Header, FeedDay: prep.feedDay, Workflow: prep.workflow, Outcome: result.Outcome}, nil
}

// lifecyclePrep holds the resolved, validated inputs shared by the three operations.
type lifecyclePrep struct {
	asOf        time.Time
	feedDay     string
	feedDayTime time.Time
	workflow    string
	cells       []domain.StoredCell
	fingerprint string
}

// prepareLifecycle validates the request, resolves the feed day and dispatch clock, and (when
// generate is true) generates + freezes this workflow's rows. Lock passes generate=false because it
// changes no rows.
func (s *Service) prepareLifecycle(ctx context.Context, req IssueRequest, generate bool) (lifecyclePrep, error) {
	if s.issues == nil || s.schedule == nil {
		return lifecyclePrep{}, fmt.Errorf("feeddirection: issue store and schedule reader are required for the lifecycle")
	}
	tenantID := strings.TrimSpace(req.TenantID)
	parkID := strings.TrimSpace(req.ParkID)
	if tenantID == "" || parkID == "" {
		return lifecyclePrep{}, ports.ErrParkRequired
	}
	workflow := strings.TrimSpace(req.Workflow)
	if workflow != domain.WorkflowNormal && workflow != domain.WorkflowExperiment {
		return lifecyclePrep{}, ports.ErrInvalidWorkflow
	}

	asOf := req.AsOf
	if asOf.IsZero() {
		asOf = s.now()
	}
	asOf = asOf.In(biztime.DefaultLocation())
	// Feed for day D is produced on D-1: feed_day = the business day AFTER the operation runs.
	feedDayTime := biztime.BusinessDayStart(asOf).AddDate(0, 0, 1)
	feedDay := biztime.BusinessDate(feedDayTime)

	// The (park, workflow) must have an authored dispatch clock, or there is no time to issue
	// against. This is also the read that migration 000004's clock finally gets wired into.
	clocks, err := s.schedule.ListScheduleClocks(ctx, tenantID, parkID, asOf)
	if err != nil {
		return lifecyclePrep{}, err
	}
	if !hasWorkflow(clocks, workflow) {
		return lifecyclePrep{}, ports.ErrWorkflowNotConfigured
	}

	prep := lifecyclePrep{asOf: asOf, feedDay: feedDay, feedDayTime: feedDayTime, workflow: workflow}
	if !generate {
		return prep, nil
	}

	// Generate the WHOLE park scope for the feed day, then keep only this workflow's rows. The math
	// is the same verified pipeline the live preview uses; we freeze its output, we do not recompute
	// it differently.
	result, err := s.generate(ctx, generateRequest{
		tenantID:   tenantID,
		parkID:     parkID,
		targetDate: feedDayTime,
		limit:      MaxShedPageLimit,
	})
	if err != nil {
		return lifecyclePrep{}, err
	}
	workflowRows := rowsForWorkflow(result.scopeRows, workflow)
	prep.cells = domain.FlattenRows(workflowRows)
	prep.fingerprint = domain.FingerprintRows(workflowRows)
	return prep, nil
}

// ---------------------------------------------------------------------------
// Serve path
// ---------------------------------------------------------------------------

// servePreview reads the FROZEN sheet for a feed day and returns one page of its stored rows, or the
// honest pending/never-issued state when nothing was issued.
func (s *Service) servePreview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error) {
	if s.issues == nil {
		return domain.PreviewPage{}, fmt.Errorf("feeddirection: issue store is required to serve issued sheets")
	}
	feedDay := biztime.BusinessDate(q.TargetDate)
	scopeRows, lifecycle, served, err := s.loadServedRows(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	if !served {
		return domain.PreviewPage{
			Items:      []domain.DirectionRow{},
			Summary:    domain.SummarizeScope(nil, nil),
			Lifecycle:  lifecycle,
			TargetDate: feedDay,
			Limit:      q.Limit,
			Offset:     q.Offset,
		}, nil
	}

	// Apply the shed and session narrowing to the stored rows, exactly as the live path filtered its
	// generated rows.
	scopeRows = filterPreviewRows(scopeRows, q.ShedID, q.SessionNo)
	shedOrder := shedOrderOf(scopeRows)
	pageSheds, hasMore := sliceStringPage(shedOrder, q.Limit, q.Offset)
	pageRows := rowsForShedIDs(scopeRows, pageSheds)
	items := domain.DistinctFeedItems(scopeRows)

	return domain.PreviewPage{
		Items:      pageRows,
		Summary:    domain.SummarizeScope(scopeRows, items),
		Lifecycle:  lifecycle,
		TargetDate: feedDay,
		Limit:      q.Limit,
		Offset:     q.Offset,
		HasMore:    hasMore,
	}, nil
}

// servePacking is the packing-worklist twin of servePreview.
func (s *Service) servePacking(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error) {
	if s.issues == nil {
		return domain.PackingPage{}, fmt.Errorf("feeddirection: issue store is required to serve issued sheets")
	}
	feedDay := biztime.BusinessDate(q.TargetDate)
	scopeRows, lifecycle, served, err := s.loadServedRows(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
	if err != nil {
		return domain.PackingPage{}, err
	}
	if !served {
		return domain.PackingPage{
			Items:      []domain.PackingRow{},
			Summary:    domain.SummarizePacking(nil, nil),
			Lifecycle:  lifecycle,
			TargetDate: feedDay,
			Limit:      q.Limit,
			Offset:     q.Offset,
		}, nil
	}

	shedOrder := shedOrderOf(scopeRows)
	pageSheds, hasMore := sliceStringPage(shedOrder, q.Limit, q.Offset)
	pageRows := rowsForShedIDs(scopeRows, pageSheds)
	items := domain.DistinctFeedItems(scopeRows)

	return domain.PackingPage{
		Items:      domain.BuildPackingRows(pageRows, items),
		Summary:    domain.SummarizePacking(domain.BuildPackingRows(scopeRows, items), items),
		Lifecycle:  lifecycle,
		TargetDate: feedDay,
		Limit:      q.Limit,
		Offset:     q.Offset,
		HasMore:    hasMore,
	}, nil
}

// loadServedRows loads the frozen rows for a park-day and builds the lifecycle metadata. served is
// false when nothing was issued -- lifecycle then carries the pending or never-issued state.
func (s *Service) loadServedRows(ctx context.Context, tenantID, parkID, feedDay, workflow string) ([]domain.DirectionRow, domain.Lifecycle, bool, error) {
	headers, err := s.issues.LoadIssueHeaders(ctx, tenantID, parkID, feedDay, workflow)
	if err != nil {
		return nil, domain.Lifecycle{}, false, err
	}
	if len(headers) == 0 {
		lifecycle, err := s.pendingLifecycle(ctx, tenantID, parkID, feedDay, workflow)
		return nil, lifecycle, false, err
	}

	issueIDs := make([]string, 0, len(headers))
	for _, h := range headers {
		issueIDs = append(issueIDs, h.IssueID)
	}
	rowsByIssue, err := s.issues.LoadIssueRows(ctx, tenantID, issueIDs)
	if err != nil {
		return nil, domain.Lifecycle{}, false, err
	}

	scopeRows := make([]domain.DirectionRow, 0)
	for _, h := range headers {
		scopeRows = append(scopeRows, domain.ReconstructRows(rowsByIssue[h.IssueID])...)
	}
	return scopeRows, aggregateLifecycle(headers), true, nil
}

// pendingLifecycle builds the not-yet-issued / never-issued state, naming when each expected
// workflow is (or was) due to be issued. It is the state the maintainer wants for a future date
// instead of a speculative number.
func (s *Service) pendingLifecycle(ctx context.Context, tenantID, parkID, feedDay, workflow string) (domain.Lifecycle, error) {
	if s.schedule == nil {
		return domain.Lifecycle{State: domain.LifecycleStateNotIssued, Workflows: []domain.WorkflowLifecycle{}}, nil
	}
	clocks, err := s.schedule.ListScheduleClocks(ctx, tenantID, parkID, s.now())
	if err != nil {
		return domain.Lifecycle{}, err
	}
	now := s.now().In(biztime.DefaultLocation())

	wfs := make([]domain.WorkflowLifecycle, 0, len(clocks))
	allPending := true
	anyExpected := false
	for _, clock := range clocks {
		if workflow != "" && clock.Workflow != workflow {
			continue
		}
		anyExpected = true
		issueAt, err := clock.ExpectedIssueInstant(feedDay)
		if err != nil {
			return domain.Lifecycle{}, err
		}
		expected := domain.FormatBusinessInstant(issueAt)
		state := domain.LifecycleStateNotIssued
		if now.Before(issueAt) {
			state = domain.LifecycleStatePending
		} else {
			allPending = false
		}
		wfs = append(wfs, domain.WorkflowLifecycle{
			Workflow:        clock.Workflow,
			State:           state,
			ExpectedIssueAt: &expected,
		})
	}

	lifecycle := domain.Lifecycle{Workflows: wfs}
	switch {
	case !anyExpected:
		// No dispatch clock for this park/workflow: nothing was scheduled to issue, so nothing was
		// ever issued. Honest and explicit, not a live compute.
		lifecycle.State = domain.LifecycleStateNotIssued
		lifecycle.Message = fmt.Sprintf("no feed sheet was issued for %s and no dispatch clock is configured", feedDay)
	case allPending:
		lifecycle.State = domain.LifecycleStatePending
		lifecycle.Message = fmt.Sprintf("feed sheet for %s has not been issued yet; %s", feedDay, describeExpected(wfs))
	default:
		lifecycle.State = domain.LifecycleStateNotIssued
		lifecycle.Message = fmt.Sprintf("no feed sheet was issued for %s", feedDay)
	}
	return lifecycle, nil
}

// aggregateLifecycle rolls the (at most two) issue headers into one lifecycle. The aggregate state
// is the LEAST-ADVANCED state among them, so a park-day is never reported "locked" while one of its
// workflows is still merely issued.
func aggregateLifecycle(headers []domain.IssueHeader) domain.Lifecycle {
	rank := map[string]int{domain.IssueStateIssued: 0, domain.IssueStateAmended: 1, domain.IssueStateLocked: 2}
	byRank := []string{domain.IssueStateIssued, domain.IssueStateAmended, domain.IssueStateLocked}

	lifecycle := domain.Lifecycle{Workflows: make([]domain.WorkflowLifecycle, 0, len(headers))}
	minRank := 2
	var earliestIssued *time.Time
	var latestAmended *time.Time
	var latestLocked *time.Time
	var totalAmendments int32

	for _, h := range headers {
		if r, ok := rank[h.State]; ok && r < minRank {
			minRank = r
		}
		issued := h.IssuedAt
		if earliestIssued == nil || issued.Before(*earliestIssued) {
			cp := issued
			earliestIssued = &cp
		}
		if h.AmendedAt != nil && (latestAmended == nil || h.AmendedAt.After(*latestAmended)) {
			cp := *h.AmendedAt
			latestAmended = &cp
		}
		if h.LockedAt != nil && (latestLocked == nil || h.LockedAt.After(*latestLocked)) {
			cp := *h.LockedAt
			latestLocked = &cp
		}
		totalAmendments += h.AmendmentCount
		lifecycle.Workflows = append(lifecycle.Workflows, domain.WorkflowLifecycle{
			Workflow:       h.Workflow,
			State:          h.State,
			IssuedAt:       instantPtr(&h.IssuedAt),
			AmendedAt:      instantPtr(h.AmendedAt),
			LockedAt:       instantPtr(h.LockedAt),
			AmendmentCount: h.AmendmentCount,
		})
	}

	lifecycle.State = byRank[minRank]
	lifecycle.IssuedAt = instantPtr(earliestIssued)
	lifecycle.AmendedAt = instantPtr(latestAmended)
	lifecycle.LockedAt = instantPtr(latestLocked)
	lifecycle.AmendmentCount = totalAmendments
	return lifecycle
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func issueIdempotencyKey(tenantID, parkID, feedDay, workflow string) string {
	return fmt.Sprintf("issue:%s:%s:%s:%s", tenantID, parkID, feedDay, workflow)
}

func hasWorkflow(clocks []domain.WorkflowClock, workflow string) bool {
	for _, c := range clocks {
		if c.Workflow == workflow {
			return true
		}
	}
	return false
}

// rowsForWorkflow keeps only the rows a planner stamped with the given workflow. Sheds are
// partitioned by workflow (a shed is experiment iff it has hand-authored rows), so this cleanly
// splits the whole-park generation into the per-workflow issue it belongs to.
func rowsForWorkflow(rows []domain.DirectionRow, workflow string) []domain.DirectionRow {
	out := make([]domain.DirectionRow, 0, len(rows))
	for _, r := range rows {
		if r.Workflow == workflow {
			out = append(out, r)
		}
	}
	return out
}

func filterPreviewRows(rows []domain.DirectionRow, shedID string, sessionNo int32) []domain.DirectionRow {
	if shedID == "" && sessionNo == 0 {
		return rows
	}
	out := make([]domain.DirectionRow, 0, len(rows))
	for _, r := range rows {
		if shedID != "" && r.ShedID != shedID {
			continue
		}
		if sessionNo != 0 && r.SessionNo != sessionNo {
			continue
		}
		out = append(out, r)
	}
	return out
}

// shedOrderOf returns the distinct shed ids in first-appearance order -- the order the frozen rows
// were stored in, which is generation (shed-catalog) order. Paging by shed uses this, so a shed's
// grains never straddle a page boundary.
func shedOrderOf(rows []domain.DirectionRow) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, r := range rows {
		if _, ok := seen[r.ShedID]; ok {
			continue
		}
		seen[r.ShedID] = struct{}{}
		out = append(out, r.ShedID)
	}
	return out
}

// sliceStringPage takes the requested page out of an ordered shed-id list. Same contract as
// sliceShedPage: an offset past the end is an empty page, not an error.
func sliceStringPage(sheds []string, limit, offset int32) ([]string, bool) {
	start := int(offset)
	if start > len(sheds) {
		start = len(sheds)
	}
	end := start + int(limit)
	if end > len(sheds) {
		end = len(sheds)
	}
	return sheds[start:end], end < len(sheds)
}

// rowsForShedIDs narrows rows to the sheds on the page, preserving order.
func rowsForShedIDs(rows []domain.DirectionRow, sheds []string) []domain.DirectionRow {
	if len(sheds) == 0 {
		return []domain.DirectionRow{}
	}
	wanted := make(map[string]struct{}, len(sheds))
	for _, s := range sheds {
		wanted[s] = struct{}{}
	}
	out := make([]domain.DirectionRow, 0, len(rows))
	for _, r := range rows {
		if _, ok := wanted[r.ShedID]; ok {
			out = append(out, r)
		}
	}
	return out
}

func describeExpected(wfs []domain.WorkflowLifecycle) string {
	parts := make([]string, 0, len(wfs))
	for _, wf := range wfs {
		at := ""
		if wf.ExpectedIssueAt != nil {
			at = *wf.ExpectedIssueAt
		}
		parts = append(parts, fmt.Sprintf("%s at %s", wf.Workflow, at))
	}
	return "expected: " + strings.Join(parts, "; ")
}

func instantPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := domain.FormatBusinessInstant(*t)
	return &v
}
