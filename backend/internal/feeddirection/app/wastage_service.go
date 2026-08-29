package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Feed WASTAGE — the app half (maintainer decision, 2026-08-18). A daily task on EXPERIMENT pens
// only: the worklist is DERIVED from the day's frozen EXPERIMENT sheet (the same rows packing
// reads), one row per pen per feed day. The operator submits ONE mandatory wastage video, which
// writes a 'pending_verification' feed_wastage_completions row and enqueues ONE verification item;
// the pen-day is 'completed' only when a verifier approves. The verifier's measured leftover weight
// rides her own route (RecordWastageMeasurement), never the verdict payload.

// ErrWastageEnqueuerNotWired is returned when a wastage completion cannot enqueue its verification
// item because the enqueue seam was never wired — a composition bug, surfaced loudly rather than
// silently stranding a pending_verification row.
var ErrWastageEnqueuerNotWired = errors.New("feeddirection: wastage verification enqueuer is not wired")

// FeedWastageVerificationEnqueuer enqueues the mandatory-proof verification item for a submitted
// feed wastage completion (mirrors FeedPackingVerificationEnqueuer). The composition layer adapts
// the verification module's CreateItem to this narrow port so feeddirection never touches
// verification's tables directly.
type FeedWastageVerificationEnqueuer interface {
	EnqueueFeedWastageVerification(ctx context.Context, in FeedWastageVerificationEnqueueRequest) error
}

// FeedWastageVerificationEnqueueRequest is one wastage completion (its video) handed to the
// verifier queue.
type FeedWastageVerificationEnqueueRequest struct {
	TenantID        string
	CompletionID    string
	ParkID          string
	ShedID          string
	ShedName        string
	PartitionLabel  string
	TargetDate      time.Time
	WastageProofRef string
	OperatorID      string
	CapturedAt      time.Time
	IdempotencyKey  string
	// ExperimentArm is the pen's authored trial group, carried onto the verifier's item so she
	// knows which trial the leftover she is measuring belongs to. Blank when the sheet could not
	// name one — a completion must never fail because its decoration could not be composed.
	ExperimentArm string
	// HeadCountSummary is the pen's projected head count, context for judging the leftover. Blank
	// when unknown.
	HeadCountSummary string
}

// CompleteWastageInput is the app-level wastage completion request the HTTP handler builds from the
// body plus the authenticated actor context. There is no SessionNo (the grain is the pen-day) and
// no Workflow (wastage is experiment-only by definition).
type CompleteWastageInput struct {
	TenantID        string
	ParkID          string
	ShedID          string
	PartitionLabel  string
	TargetDate      time.Time
	WastageProofRef string
	CompletedBy     string
	IdempotencyKey  string
	ActorID         string
	ActorType       string
	TraceID         string
}

// CompleteWastage records a pen-day's ONE mandatory wastage video at 'pending_verification' and
// enqueues one verifier-queue item. It refuses a pen the day's experiment sheet does not cover
// (ports.ErrWastageNotExperimentPen): wastage is experiment-pen work only, and a row no worklist
// line matches would be stranded. It completes NOTHING — the pen-day is completed only when a
// verifier approves (the consumer's ApplyVerifiedWastage).
func (s *Service) CompleteWastage(ctx context.Context, in CompleteWastageInput) (ports.CompleteWastageResult, error) {
	if s.wastage == nil {
		return ports.CompleteWastageResult{}, ports.ErrWastageStoreUnavailable
	}
	if s.wastageEnqueuer == nil {
		// Fail closed: without the verifier-queue seam a completion would flip a pen-day to
		// pending_verification with nothing for a verifier to act on.
		return ports.CompleteWastageResult{}, ErrWastageEnqueuerNotWired
	}

	// Write path: the route already clamped the park to the caller's grant. See CompleteDistribution.
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return ports.CompleteWastageResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.PartitionLabel = strings.TrimSpace(in.PartitionLabel)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.WastageProofRef = strings.TrimSpace(in.WastageProofRef)

	if in.TenantID == "" || in.ParkID == "" {
		return ports.CompleteWastageResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return ports.CompleteWastageResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return ports.CompleteWastageResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.IdempotencyKey == "" {
		return ports.CompleteWastageResult{}, ports.ErrIdempotencyRequired
	}
	// The wastage VIDEO is mandatory — reject before any state changes (there is nothing for a
	// verifier to measure without it).
	if in.WastageProofRef == "" {
		return ports.CompleteWastageResult{}, ports.ErrWastageProofRequired
	}

	// THE PEN MUST BE ON THE DAY'S EXPERIMENT SHEET. This is the "rows trigger for experiment pens
	// only" rule enforced on the WRITE as well as the read: a completion for a normal pen — or for a
	// day whose experiment sheet was never issued — is refused rather than stored as work no
	// worklist line ever matches. The same read also hands the enqueue its arm/head-count context,
	// so this costs one bounded read, once, on a submit.
	pen, err := s.wastagePen(ctx, in.TenantID, in.ParkID, in.ShedID, in.PartitionLabel, in.TargetDate)
	if err != nil {
		return ports.CompleteWastageResult{}, err
	}
	if pen == nil {
		return ports.CompleteWastageResult{}, ports.ErrWastageNotExperimentPen
	}

	// When a validator is wired, the proof id must resolve to a real, completed, tenant-owned
	// upload before the completion is written.
	if s.proofs != nil {
		if err := s.proofs.ValidateFeedProofs(ctx, in.TenantID, []string{in.WastageProofRef}); err != nil {
			return ports.CompleteWastageResult{}, err
		}
	}

	result, err := s.wastage.CompleteWastage(ctx, ports.CompleteWastageParams{
		TenantID:        in.TenantID,
		ParkID:          in.ParkID,
		ShedID:          in.ShedID,
		PartitionLabel:  in.PartitionLabel,
		TargetDate:      in.TargetDate,
		WastageProofRef: in.WastageProofRef,
		CompletedBy:     strings.TrimSpace(in.CompletedBy),
		IdempotencyKey:  in.IdempotencyKey,
		ActorID:         in.ActorID,
		ActorType:       in.ActorType,
		TraceID:         in.TraceID,
	})
	if err != nil {
		return ports.CompleteWastageResult{}, err
	}

	// Enqueue the verifier item ONLY on a fresh pending transition (a new submit or a rework
	// re-submit). Idempotent on (completion_id + row_version), so a retry after a prior enqueue
	// failure heals rather than duplicates.
	if result.NewlyPending {
		heads := ""
		if pen.HeadCount > 0 {
			heads = fmt.Sprintf("%d", pen.HeadCount)
		}
		if enqErr := s.wastageEnqueuer.EnqueueFeedWastageVerification(ctx, FeedWastageVerificationEnqueueRequest{
			TenantID:         in.TenantID,
			CompletionID:     result.CompletionID,
			ParkID:           in.ParkID,
			ShedID:           in.ShedID,
			ShedName:         result.ShedName,
			PartitionLabel:   result.PartitionLabel,
			TargetDate:       in.TargetDate,
			WastageProofRef:  in.WastageProofRef,
			OperatorID:       strings.TrimSpace(in.CompletedBy),
			ExperimentArm:    pen.ExperimentArm,
			HeadCountSummary: heads,
			CapturedAt:       s.now().UTC(),
			// Keyed to the completion + its row_version so a rework re-submit (row_version bumped)
			// enqueues a fresh item while a retry of the same submit collapses onto one queue item.
			IdempotencyKey: fmt.Sprintf("feed-wastage-verification:%s:%d", result.CompletionID, result.RowVersion),
		}); enqErr != nil {
			return ports.CompleteWastageResult{}, enqErr
		}
	}
	return result, nil
}

// wastagePen resolves ONE pen's wastage line from the day's frozen experiment sheet, or nil when
// the sheet does not cover it. One bounded read of the same frozen rows the worklist serves, so the
// membership rule and the worklist cannot disagree.
func (s *Service) wastagePen(ctx context.Context, tenantID, parkID, shedID, partitionLabel string, targetDate time.Time) (*domain.WastageRow, error) {
	if s.issues == nil {
		return nil, fmt.Errorf("feeddirection: issue store is required to serve issued sheets")
	}
	feedDay := biztime.BusinessDate(targetDate)
	scopeRows, _, served, err := s.loadServedRows(ctx, tenantID, parkID, feedDay, domain.WorkflowExperiment)
	if err != nil {
		return nil, err
	}
	if !served {
		// Same freeze-on-first-touch contract as the worklist read: a submit that arrives before
		// anyone opened the day's list (its due clock already passed) freezes the sheet rather
		// than refusing work the pen genuinely owes. Before the clock, nothing freezes and the pen
		// resolves to nil — no wastage task exists yet.
		gate, err := s.gateOrFreeze(ctx, tenantID, parkID, feedDay, domain.WorkflowExperiment)
		if err != nil {
			return nil, err
		}
		if !gate.frozeAny {
			return nil, nil
		}
		scopeRows, _, served, err = s.loadServedRows(ctx, tenantID, parkID, feedDay, domain.WorkflowExperiment)
		if err != nil {
			return nil, err
		}
		if !served {
			return nil, nil
		}
	}
	wantPartition := domain.PartitionMatchKey(partitionLabel)
	for _, row := range domain.BuildWastageRows(scopeRows) {
		if row.ShedID == shedID && domain.PartitionMatchKey(row.PartitionLabel) == wantPartition {
			pen := row
			return &pen, nil
		}
	}
	return nil, nil
}

// WastageWorklist serves one page of the per-pen wastage view for one park and one feed day.
//
// It is DERIVED from the same frozen experiment sheet the packing worklist reads — not a second
// planner — so "which pens are on the experiment today" has exactly one source of truth. Completion
// rows only OVERLAY status onto the derived rows. Read-only.
func (s *Service) WastageWorklist(ctx context.Context, q domain.WastageQuery) (domain.WastagePage, error) {
	if s.issues == nil {
		return domain.WastagePage{}, fmt.Errorf("feeddirection: issue store is required to serve issued sheets")
	}
	resolvedPark, err := s.resolveParkID(ctx, q.TenantID, q.ParkID, q.AuthorizedParkIDs)
	if err != nil {
		return domain.WastagePage{}, err
	}
	q.ParkID = resolvedPark
	normalized, err := s.normalizeWastageQuery(q)
	if err != nil {
		return domain.WastagePage{}, err
	}

	feedDay := biztime.BusinessDate(normalized.TargetDate)
	scopeRows, lifecycle, served, err := s.loadServedRows(ctx, normalized.TenantID, normalized.ParkID, feedDay, domain.WorkflowExperiment)
	if err != nil {
		return domain.WastagePage{}, err
	}
	if !served {
		// Wastage has no gate of its own: it exists once the EXPERIMENT sheet for the day is
		// frozen, from the same clock. Same freeze-on-first-read contract as servePacking.
		gate, err := s.gateOrFreeze(ctx, normalized.TenantID, normalized.ParkID, feedDay, domain.WorkflowExperiment)
		if err != nil {
			return domain.WastagePage{}, err
		}
		if !gate.frozeAny {
			page := domain.WastagePage{
				Items:      []domain.WastageRow{},
				Summary:    domain.WastageSummary{},
				Lifecycle:  gate.emptyLifecycle,
				TargetDate: feedDay,
				Limit:      normalized.Limit,
				Offset:     normalized.Offset,
			}
			page.Filters, err = s.buildFilters(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate, normalized.AuthorizedParkIDs)
			if err != nil {
				return domain.WastagePage{}, err
			}
			return page, nil
		}
		scopeRows, lifecycle, served, err = s.loadServedRows(ctx, normalized.TenantID, normalized.ParkID, feedDay, domain.WorkflowExperiment)
		if err != nil {
			return domain.WastagePage{}, err
		}
		if !served {
			return domain.WastagePage{}, fmt.Errorf("feeddirection: froze the experiment sheet for %s but no stored rows came back", feedDay)
		}
		lifecycle = withPendingWorkflows(lifecycle, gate.pending)
	}

	// Narrow to the requested shed/partition BEFORE paging and summarizing, mirroring servePacking.
	scopeRows = filterPreviewRows(scopeRows, normalized.ShedID, normalized.PartitionLabel, 0)

	// Build the pen rows, overlay statuses, and status-filter over the WHOLE scope before paging so
	// the page and the summary describe the same status set.
	statusMap, err := s.wastageStatusMap(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate)
	if err != nil {
		return domain.WastagePage{}, err
	}
	scopePens := stampAndFilterWastageRows(domain.BuildWastageRows(scopeRows), statusMap, normalized.Status)

	shedOrder := wastageShedOrderOf(scopePens)
	pageSheds, hasMore := sliceStringPage(shedOrder, normalized.Limit, normalized.Offset)
	pagePens := wastageRowsForShedIDs(scopePens, pageSheds)

	page := domain.WastagePage{
		Items:      pagePens,
		Summary:    domain.SummarizeWastage(scopePens),
		Lifecycle:  lifecycle,
		TargetDate: feedDay,
		Limit:      normalized.Limit,
		Offset:     normalized.Offset,
		HasMore:    hasMore,
	}
	page.Filters, err = s.buildFilters(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate, normalized.AuthorizedParkIDs)
	if err != nil {
		return domain.WastagePage{}, err
	}
	return page, nil
}

func (s *Service) normalizeWastageQuery(q domain.WastageQuery) (domain.WastageQuery, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	q.ShedID = strings.TrimSpace(q.ShedID)
	q.PartitionLabel = strings.TrimSpace(q.PartitionLabel)
	if q.TenantID == "" || q.ParkID == "" {
		return domain.WastageQuery{}, ports.ErrParkRequired
	}
	if q.TargetDate.IsZero() {
		return domain.WastageQuery{}, ports.ErrInvalidTargetDate
	}
	q.TargetDate = biztime.BusinessDayStart(q.TargetDate)
	limit, offset, err := normalizePaging(q.Limit, q.Offset)
	if err != nil {
		return domain.WastageQuery{}, err
	}
	q.Limit, q.Offset = limit, offset
	return q, nil
}

// wastageCompletionState mirrors packingCompletionState at the pen-day grain, plus the recorded
// measurement so a completed card can show the value.
type wastageCompletionState struct {
	status       string
	rawStatus    string
	reworkReason string
	wastageKg    string
}

// wastageStatusMap reads ONE park-day's feed_wastage_completions in a single bounded indexed read,
// keyed by pen (shed + normalized partition).
func (s *Service) wastageStatusMap(ctx context.Context, tenantID, parkID string, asOf time.Time) (map[string]wastageCompletionState, error) {
	if s.wastage == nil {
		return nil, nil
	}
	list, err := s.wastage.ListWastageCompletionStatuses(ctx, tenantID, parkID, asOf)
	if err != nil {
		return nil, err
	}
	out := make(map[string]wastageCompletionState, len(list))
	for _, d := range list {
		out[wastageKey(d.ShedID, d.PartitionLabel)] = wastageCompletionState{
			status:       domain.NormalizeSessionStatus(d.Status),
			rawStatus:    strings.TrimSpace(d.Status),
			reworkReason: d.ReworkReason,
			wastageKg:    d.WastageKg,
		}
	}
	return out, nil
}

// wastageKey is the identity of ONE wastage completion: a shed's PEN on the feed day being served.
// No session and no workflow — the grain is the pen-day and the workflow is constant.
func wastageKey(shedID, partitionLabel string) string {
	return shedID + "|" + domain.PartitionMatchKey(partitionLabel)
}

// stampAndFilterWastageRows sets each pen row's LifecycleStatus (default SessionStatusPending),
// Completed, ReworkReason and recorded value from statusMap, then narrows to statusFilter. MUST run
// over the WHOLE scope BEFORE paging, same contract as the packing stamper.
func stampAndFilterWastageRows(rows []domain.WastageRow, statusMap map[string]wastageCompletionState, statusFilter string) []domain.WastageRow {
	out := rows
	if statusFilter != "" {
		out = make([]domain.WastageRow, 0, len(rows))
	}
	for i := range rows {
		state := statusMap[wastageKey(rows[i].ShedID, rows[i].PartitionLabel)]
		bucket := state.status
		if bucket == "" {
			bucket = domain.SessionStatusPending
		}
		rows[i].LifecycleStatus = bucket
		rows[i].Completed = bucket == domain.SessionStatusCompleted
		// Carried ONLY while the stored row is actually 'rework', so a stale sentence cannot tell an
		// operator to redo work they already redid.
		if state.rawStatus == domain.WastageStatusRework {
			rows[i].ReworkReason = state.reworkReason
		} else {
			rows[i].ReworkReason = ""
		}
		rows[i].WastageKg = state.wastageKg
		if statusFilter != "" {
			if bucket == statusFilter {
				out = append(out, rows[i])
			}
		}
	}
	return out
}

// wastageShedOrderOf lists the DISTINCT shed ids in first-appearance order, for shed-unit paging —
// one shed's pens never straddle a page boundary.
func wastageShedOrderOf(rows []domain.WastageRow) []string {
	seen := make(map[string]struct{}, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ShedID]; ok {
			continue
		}
		seen[row.ShedID] = struct{}{}
		order = append(order, row.ShedID)
	}
	return order
}

// wastageRowsForShedIDs narrows the pen rows to the sheds on the page, preserving order.
func wastageRowsForShedIDs(rows []domain.WastageRow, shedIDs []string) []domain.WastageRow {
	if len(shedIDs) == 0 {
		return []domain.WastageRow{}
	}
	wanted := make(map[string]struct{}, len(shedIDs))
	for _, id := range shedIDs {
		wanted[id] = struct{}{}
	}
	out := make([]domain.WastageRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := wanted[row.ShedID]; ok {
			out = append(out, row)
		}
	}
	return out
}
