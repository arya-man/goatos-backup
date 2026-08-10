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
	// ReopenedPackingCompletionIDs names the packing pen-days the correction sent back to the
	// operator because their animal count moved. Empty on issue/lock and on the ordinary correction
	// that lands before anyone has packed.
	ReopenedPackingCompletionIDs []string
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
//
// It then REOPENS any already-submitted packing for the pens whose animal count moved -- see
// reopenPackingForCorrection.
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
	reopened, err := s.reopenPackingForCorrection(ctx, req.TenantID, req.ParkID, prep, result.HeadCountChangedPens)
	if err != nil {
		return LifecycleReport{}, err
	}
	return LifecycleReport{
		Header: result.Header, FeedDay: prep.feedDay, Workflow: prep.workflow,
		Outcome: result.Outcome, AffectedShedIDs: result.AffectedShedIDs,
		ReopenedPackingCompletionIDs: reopened,
	}, nil
}

// packingReopenedReason is the operator-facing sentence stored on a pen reopened by the afternoon
// correction. Backend owns the copy (the golden frontend rule), and it says the farm thing: animals
// moved, the quantities changed, pack again and film it again. It names no table, job or window.
const packingReopenedReason = "Animals moved in or out of this pen, so the feed quantities changed. Pack the new amounts and record a new video."

// reopenPackingForCorrection throws away the packing videos of pens the correction re-counted.
//
// MAINTAINER DECISION 2026-08-10. A low-priority movement raised in the morning is due tomorrow, and
// tomorrow's normal sheet was issued at 07:00 and is already being packed. The afternoon correction
// now recomputes it including movements nobody has approved yet -- but a pen whose bag was packed
// and filmed at 09:00 was packed for the old head count, and nothing was telling the packer. The
// video is reverted and the card comes back with the new numbers.
//
// TWO NARROWINGS, both load-bearing:
//
//   - EXPERIMENT IS EXEMPT. Experiment rations are authored as absolute kg per pen, so a head-count
//     change moves no quantity there; reopening one would discard a perfectly good video for a sheet
//     that did not change.
//   - HEAD COUNT ONLY, per pen. AffectedShedIDs would also fire for a relabelled ration group or a
//     re-authored gram rate, and it is shed-wide -- reopening Castro - 1 and Castro - 3 because
//     Castro - 2 gained animals. Making an operator refilm is expensive, so it is spent only where
//     the number of mouths actually moved.
//
// A pen nobody has packed yet reopens nothing: the store finds no submitted row and the operator
// simply sees the corrected numbers on a card that was still pending.
func (s *Service) reopenPackingForCorrection(
	ctx context.Context,
	tenantID, parkID string,
	prep lifecyclePrep,
	pens []domain.PenKey,
) ([]string, error) {
	if s.packing == nil || len(pens) == 0 || prep.workflow != domain.WorkflowNormal {
		return nil, nil
	}
	result, err := s.packing.ReopenPackingForFeedChange(ctx, ports.ReopenPackingParams{
		TenantID:   tenantID,
		ParkID:     parkID,
		TargetDate: prep.feedDayTime,
		Workflow:   prep.workflow,
		Pens:       pens,
		Reason:     packingReopenedReason,
		ActorID:    s.generatedBy,
	})
	if err != nil {
		return nil, fmt.Errorf("feeddirection: reopen packing after correction: %w", err)
	}
	return result.ReopenedCompletionIDs, nil
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

// servePreview serves one page of feed direction rows for a feed day. When an issued/amended/locked
// sheet exists it returns the FROZEN stored rows (issued always wins). When nothing is issued the
// DISPATCH GATE decides: before the workflow's clock the day has no rows at all, and at/after it the
// first read freezes the sheet and serves it frozen. See lifecycle_gate.go (maintainer decision
// 2026-08-08, superseding the 2026-07-20 always-generate preview).
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
		gate, err := s.gateOrFreeze(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
		if err != nil {
			return domain.PreviewPage{}, err
		}
		if !gate.frozeAny {
			// Nothing is due yet (or the day is outside the generation horizon): no rows, and a
			// lifecycle that says when the sheet arrives.
			return domain.PreviewPage{
				Items:      []domain.DirectionRow{},
				Summary:    emptyPreviewSummary(),
				Lifecycle:  gate.emptyLifecycle,
				TargetDate: feedDay,
				Limit:      q.Limit,
				Offset:     q.Offset,
			}, nil
		}
		scopeRows, lifecycle, served, err = s.loadServedRows(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
		if err != nil {
			return domain.PreviewPage{}, err
		}
		if !served {
			return domain.PreviewPage{}, fmt.Errorf("feeddirection: froze the %s sheet for %s but no stored rows came back", q.Workflow, feedDay)
		}
		lifecycle = withPendingWorkflows(lifecycle, gate.pending)
	}

	// Apply the shed and session narrowing to the stored rows, exactly as the live path filtered its
	// generated rows.
	scopeRows = filterPreviewRows(scopeRows, q.ShedID, q.SessionNo)
	// Stamp LifecycleStatus/Completed and apply the status filter over the WHOLE scope BEFORE paging, so
	// the page and the summary describe the same status set and pagination stays correct.
	statusMap, err := s.directionStatusMap(ctx, q.TenantID, q.ParkID, q.TargetDate)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	scopeRows = stampAndFilterDirectionRows(scopeRows, statusMap, q.Status)
	// ONE ROW PER OPERATIONAL LOCATION. The stored rows stay at the ration grain -- servePacking reads
	// the same frozen rows and must keep packing exactly the bag it packs today -- so the fold happens
	// here, on the direction read, after stamping and before paging. Summarizing the collapsed rows
	// keeps the summary describing precisely what the sheet shows.
	scopeRows = domain.CollapseDirectionRowsByLocation(scopeRows)
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
		// Packing has no gate of its own: it freezes with the direction sheet it reads. Normal
		// packing therefore freezes at 07:00 and experiment at 14:00, from the same clock.
		gate, err := s.gateOrFreeze(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
		if err != nil {
			return domain.PackingPage{}, err
		}
		if !gate.frozeAny {
			return domain.PackingPage{
				Items:      []domain.PackingRow{},
				Summary:    emptyPackingSummary(),
				Lifecycle:  gate.emptyLifecycle,
				TargetDate: feedDay,
				Limit:      q.Limit,
				Offset:     q.Offset,
			}, nil
		}
		scopeRows, lifecycle, served, err = s.loadServedRows(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
		if err != nil {
			return domain.PackingPage{}, err
		}
		if !served {
			return domain.PackingPage{}, fmt.Errorf("feeddirection: froze the %s sheet for %s but no stored rows came back", q.Workflow, feedDay)
		}
		lifecycle = withPendingWorkflows(lifecycle, gate.pending)
	}

	// No session filter: a packing line is a whole pen-day and every session belongs to it. The
	// worklist has no shed filter either, so the frozen scope is served as loaded.

	// Filter the underlying DirectionRows by PACKING status BEFORE the shed paging, so the page and
	// its summary describe the same status set and pagination stays correct.
	//
	// Keyed with packingCompletedKey, NOT the direction stamper: the packing status map is keyed at
	// the pen-DAY grain, so stamping direction rows with the session-bearing key would miss on every
	// single row. Every row would read `pending`, a `completed` filter would return an empty
	// worklist, and a submitted pen would offer itself for filming again.
	statusMap, err := s.packingStatusMap(ctx, q.TenantID, q.ParkID, q.TargetDate)
	if err != nil {
		return domain.PackingPage{}, err
	}
	scopeRows = stampAndFilterDirectionRowsForPacking(scopeRows, statusMap, q.Status)

	shedOrder := shedOrderOf(scopeRows)
	pageSheds, hasMore := sliceStringPage(shedOrder, q.Limit, q.Offset)
	pageRows := rowsForShedIDs(scopeRows, pageSheds)
	items := domain.DistinctFeedItems(scopeRows)

	// scopeRows are already status-filtered; stamp the built packing rows for display (no further
	// filter — statusFilter "").
	scopePacking := stampAndFilterPackingRows(domain.BuildPackingRows(scopeRows, items), statusMap, "")
	pagePacking := stampAndFilterPackingRows(domain.BuildPackingRows(pageRows, items), statusMap, "")

	return domain.PackingPage{
		Items:      pagePacking,
		Summary:    domain.SummarizePacking(scopePacking, items),
		Lifecycle:  lifecycle,
		TargetDate: feedDay,
		Limit:      q.Limit,
		Offset:     q.Offset,
		HasMore:    hasMore,
	}, nil
}

// loadServedRows loads the FROZEN rows for a park-day. served is false when nothing was issued; the
// caller then GENERATES a preview instead (see servePreviewGenerated / servePackingGenerated), so no
// lifecycle is built here for the unserved case.
func (s *Service) loadServedRows(ctx context.Context, tenantID, parkID, feedDay, workflow string) ([]domain.DirectionRow, domain.Lifecycle, bool, error) {
	headers, err := s.issues.LoadIssueHeaders(ctx, tenantID, parkID, feedDay, workflow)
	if err != nil {
		return nil, domain.Lifecycle{}, false, err
	}
	if len(headers) == 0 {
		return nil, domain.Lifecycle{}, false, nil
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

// servePreviewGenerated LIVE-GENERATES the full scope for a feed day that has NO issued sheet and
// returns it labelled as a `preview` lifecycle. It reuses the SAME generation path the draft/issue
// paths use (s.generate) -- the kilogram-exact math is not forked -- and pages it by shed exactly as
// the frozen serve path pages stored rows, so a preview and an issued sheet have an identical shape
// (paged page + whole-scope, page-size-invariant summary; blocked-vs-zero preserved).
func (s *Service) servePreviewGenerated(ctx context.Context, q domain.PreviewQuery, feedDay string) (domain.PreviewPage, error) {
	// HORIZON GUARD (maintainer decision 2026-07-20). A feed day with no issued sheet is generated on
	// demand ONLY when it is within [today, tomorrow]; outside that window generating would fabricate a
	// sheet by silently freezing today's herd onto a day whose real counts are unknown. Return an
	// honest empty beyond_horizon state instead of a made-up sheet, and DO NOT read config/counts.
	if !s.withinFeedHorizon(feedDay) {
		return domain.PreviewPage{
			Items:      []domain.DirectionRow{},
			Summary:    emptyPreviewSummary(),
			Lifecycle:  s.beyondHorizonLifecycle(feedDay),
			TargetDate: feedDay,
			Limit:      q.Limit,
			Offset:     q.Offset,
		}, nil
	}
	// Generate the WHOLE scope (shed + session filters are honoured inside generate). MaxShedPageLimit
	// only bounds generate's own page; scopeRows is always the full scope, and we re-page it below
	// AFTER the workflow filter so a shed's grains never straddle a page boundary.
	result, err := s.generate(ctx, generateRequest{
		tenantID:   q.TenantID,
		parkID:     q.ParkID,
		targetDate: q.TargetDate,
		shedID:     q.ShedID,
		sessionNo:  q.SessionNo,
		limit:      MaxShedPageLimit,
	})
	if err != nil {
		return domain.PreviewPage{}, err
	}
	scopeRows := result.scopeRows
	if q.Workflow != "" {
		scopeRows = rowsForWorkflow(scopeRows, q.Workflow)
	}
	lifecycle, err := s.previewLifecycle(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
	if err != nil {
		return domain.PreviewPage{}, err
	}

	// Stamp + status-filter over the whole scope before paging (same contract as the served path).
	statusMap, err := s.directionStatusMap(ctx, q.TenantID, q.ParkID, q.TargetDate)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	scopeRows = stampAndFilterDirectionRows(scopeRows, statusMap, q.Status)
	// One row per operational location -- see the note on the served-issue path above. This is the
	// only other direction read, and packing is deliberately not folded.
	scopeRows = domain.CollapseDirectionRowsByLocation(scopeRows)

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

// servePackingGenerated is the packing-worklist twin of servePreviewGenerated.
func (s *Service) servePackingGenerated(ctx context.Context, q domain.PackingQuery, feedDay string) (domain.PackingPage, error) {
	// Same horizon guard as servePreviewGenerated: refuse to generate a worklist for a day outside
	// [today, tomorrow] rather than fabricating one from today's herd.
	if !s.withinFeedHorizon(feedDay) {
		return domain.PackingPage{
			Items:      []domain.PackingRow{},
			Summary:    emptyPackingSummary(),
			Lifecycle:  s.beyondHorizonLifecycle(feedDay),
			TargetDate: feedDay,
			Limit:      q.Limit,
			Offset:     q.Offset,
		}, nil
	}
	result, err := s.generate(ctx, generateRequest{
		tenantID:   q.TenantID,
		parkID:     q.ParkID,
		targetDate: q.TargetDate,
		// sessionNo 0 = generate EVERY session. A packing line carries the whole pen-day, so
		// generating one session would build a card missing half its bags.
		limit: MaxShedPageLimit,
	})
	if err != nil {
		return domain.PackingPage{}, err
	}
	scopeRows := result.scopeRows
	if q.Workflow != "" {
		scopeRows = rowsForWorkflow(scopeRows, q.Workflow)
	}
	lifecycle, err := s.previewLifecycle(ctx, q.TenantID, q.ParkID, feedDay, q.Workflow)
	if err != nil {
		return domain.PackingPage{}, err
	}

	// Filter DirectionRows by packing status before the shed paging, then stamp the built rows. The
	// pen-day-keyed stamper, for the reason given in servePacking.
	statusMap, err := s.packingStatusMap(ctx, q.TenantID, q.ParkID, q.TargetDate)
	if err != nil {
		return domain.PackingPage{}, err
	}
	scopeRows = stampAndFilterDirectionRowsForPacking(scopeRows, statusMap, q.Status)

	shedOrder := shedOrderOf(scopeRows)
	pageSheds, hasMore := sliceStringPage(shedOrder, q.Limit, q.Offset)
	pageRows := rowsForShedIDs(scopeRows, pageSheds)
	items := domain.DistinctFeedItems(scopeRows)

	return domain.PackingPage{
		Items:      stampAndFilterPackingRows(domain.BuildPackingRows(pageRows, items), statusMap, ""),
		Summary:    domain.SummarizePacking(stampAndFilterPackingRows(domain.BuildPackingRows(scopeRows, items), statusMap, ""), items),
		Lifecycle:  lifecycle,
		TargetDate: feedDay,
		Limit:      q.Limit,
		Offset:     q.Offset,
		HasMore:    hasMore,
	}, nil
}

// previewLifecycle builds the `preview` lifecycle for a generated (not-yet-issued) feed day. The
// aggregate state is preview -- the rows ARE returned -- while each workflow still carries its
// pending (issue instant ahead) or not_issued (issue instant passed) detail plus the expected issue
// instant, so the operator sees when the sheet WILL be formally frozen.
func (s *Service) previewLifecycle(ctx context.Context, tenantID, parkID, feedDay, workflow string) (domain.Lifecycle, error) {
	wfs, err := s.pendingWorkflows(ctx, tenantID, parkID, feedDay, workflow)
	if err != nil {
		return domain.Lifecycle{}, err
	}
	return domain.Lifecycle{
		State:     domain.LifecycleStatePreview,
		Message:   fmt.Sprintf("generated preview — the sheet for %s has not been issued yet", feedDay),
		Workflows: wfs,
	}, nil
}

// ---------------------------------------------------------------------------
// Generation horizon [today, tomorrow]
// ---------------------------------------------------------------------------

// withinFeedHorizon reports whether feedDay (an Asia/Kolkata business date, YYYY-MM-DD) falls inside
// the on-demand generation window [today, tomorrow].
//
// The window is exactly two business days: "today" is being fed (packed yesterday) and "tomorrow" is
// being packed now. The projected shed count that drives a sheet -- live herd + approved-but-
// unexecuted shiftings (counted from the day each is authorized; maintainer decision 2026-07-27) --
// is only meaningful across those two days. Beyond tomorrow the counts depend on shiftings not yet
// approved; before today the herd is no longer what it was. Generating outside the window would
// silently freeze today's herd onto the wrong day, which is fabrication.
//
// "today"/"tomorrow" come from the injected clock (s.now(), in Asia/Kolkata), NOT SQL now(), so a
// pinned-clock test is deterministic. Business-date strings compare correctly with ==.
func (s *Service) withinFeedHorizon(feedDay string) bool {
	today, tomorrow := s.feedHorizon()
	return feedDay == today || feedDay == tomorrow
}

// feedHorizon returns the inclusive [today, tomorrow] window as Asia/Kolkata business dates.
func (s *Service) feedHorizon() (today, tomorrow string) {
	todayStart := biztime.BusinessDayStart(s.now())
	return biztime.BusinessDate(todayStart), biztime.BusinessDate(todayStart.AddDate(0, 0, 1))
}

// beyondHorizonLifecycle builds the honest no-data lifecycle for a feed day outside [today, tomorrow]
// that has no issued sheet. The message names the tomorrow limit (or, for a past day, why the past is
// not regenerated) so the operator understands this is the honest replacement for a fabricated sheet,
// not an error.
func (s *Service) beyondHorizonLifecycle(feedDay string) domain.Lifecycle {
	today, tomorrow := s.feedHorizon()
	var msg string
	if feedDay < today {
		// A deliberately-browsed past day: no sheet was ever issued for it, and a past day cannot be
		// regenerated from current counts (the herd is no longer what it was). This is an honest
		// "nothing to show", not an error.
		msg = fmt.Sprintf("no feed sheet was issued for %s; a past day cannot be regenerated from current counts", feedDay)
	} else {
		msg = fmt.Sprintf("counts are only projected through %s; a feed sheet for %s cannot be produced yet", tomorrow, feedDay)
	}
	return domain.Lifecycle{
		State:     domain.LifecycleStateBeyondHorizon,
		Message:   msg,
		Workflows: []domain.WorkflowLifecycle{},
	}
}

// emptyPreviewSummary is the whole-scope summary of a beyond-horizon preview: no rows, no totals. It
// still stamps SummaryScopeFiltered so a client's coverage assertion holds.
func emptyPreviewSummary() domain.PreviewSummary {
	return domain.PreviewSummary{Scope: domain.SummaryScopeFiltered, TotalKgByFeedItem: []domain.FeedItemTotal{}}
}

// emptyPackingSummary is the packing twin of emptyPreviewSummary.
func emptyPackingSummary() domain.PackingSummary {
	return domain.PackingSummary{Scope: domain.SummaryScopeFiltered, TotalKgByFeedItem: []domain.FeedItemTotal{}}
}

// pendingWorkflows reads the dispatch clocks and returns, per matching workflow, its pending
// (issue instant still ahead) or not_issued (issue instant passed) state plus the expected issue
// instant. It never live-computes feed quantities; it only reads the schedule clock.
func (s *Service) pendingWorkflows(ctx context.Context, tenantID, parkID, feedDay, workflow string) ([]domain.WorkflowLifecycle, error) {
	if s.schedule == nil {
		return []domain.WorkflowLifecycle{}, nil
	}
	clocks, err := s.schedule.ListScheduleClocks(ctx, tenantID, parkID, s.now())
	if err != nil {
		return nil, err
	}
	now := s.now().In(biztime.DefaultLocation())

	wfs := make([]domain.WorkflowLifecycle, 0, len(clocks))
	for _, clock := range clocks {
		if workflow != "" && clock.Workflow != workflow {
			continue
		}
		issueAt, err := clock.ExpectedIssueInstant(feedDay)
		if err != nil {
			return nil, err
		}
		expected := domain.FormatBusinessInstant(issueAt)
		state := domain.LifecycleStateNotIssued
		if now.Before(issueAt) {
			state = domain.LifecycleStatePending
		}
		wfs = append(wfs, domain.WorkflowLifecycle{
			Workflow:        clock.Workflow,
			State:           state,
			ExpectedIssueAt: &expected,
		})
	}
	return wfs, nil
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

func instantPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	v := domain.FormatBusinessInstant(*t)
	return &v
}
