package app

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// THE DISPATCH GATE (maintainer decision 2026-08-08).
//
// SUPERSEDES the 2026-07-20 serve-path rule "whenever I ask for feed data by date, generate the feed
// for that date", which returned a live `preview` for any not-yet-issued day. That preview recomputed
// on EVERY read, so a shed's kilograms moved with every shifting approved after the crew had already
// packed the bags -- the mismatch the maintainer named. Draft (the explicit config-authoring what-if)
// is NOT affected: it branches before the serve path and still live-computes.
//
// A feed day's sheet now has exactly two visible states, decided by its workflow's dispatch clock
// (feed_schedule_config, per park x workflow; normal fires at direction_time 07:00, experiment at
// 14:00, both on D-1 because feed for day D is produced the day before):
//
//	before the instant  -> NO ROWS. The lifecycle carries `pending` and the expected issue time, so
//	                       the screen says when the sheet arrives instead of showing a number that is
//	                       not yet real.
//	at/after            -> the FIRST read generates the sheet and PERSISTS it, then serves the frozen
//	                       rows. Every later read serves those same frozen rows.
//
// Packing needs no gate of its own: servePacking reads the SAME frozen rows through loadServedRows
// and derives its bags from them, so freezing the direction sheet freezes the packing worklist with
// it. That is why "normal packing freezes at 07:00, experiment at 14:00" is one mechanism, not two.
//
// Two properties worth keeping if this is ever refactored:
//
//   - The freeze is stamped at the SCHEDULED instant, not at first-read time. The sheet reads
//     "issued 07:00" whether the first person opened it at 07:01 or 09:30, so the audit trail does
//     not record who happened to look first.
//   - Concurrent first-reads are safe without a lock: PersistIssue is idempotent on
//     (tenant, park, feed_day, workflow), so a second racing freeze is an exact replay, not a
//     duplicate sheet.
//
// The horizon guard still runs FIRST and still wins. A past day whose instant has long passed must
// never be freeze-on-read: generating it would stamp today's herd onto a day whose real counts are
// gone. See withinFeedHorizon.

// dispatchGate is the per-workflow verdict for a feed day that has no frozen sheet yet.
type dispatchGate struct {
	// due lists the workflows whose issue instant has passed: freeze them, then serve frozen rows.
	due []dueWorkflow
	// pending lists the workflows still ahead of their clock, carrying the expected issue instant so
	// the caller can tell the operator when the sheet arrives. These contribute NO rows.
	pending []domain.WorkflowLifecycle
}

// dueWorkflow is one workflow that is past its dispatch instant and must be frozen.
type dueWorkflow struct {
	workflow string
	// issueAt is the SCHEDULED instant (direction_time on D-1), used as the freeze's AsOf so the
	// stored issued_at is the clock time rather than whenever the first read happened to land.
	issueAt time.Time
}

// dispatchGateFor reads the dispatch clocks and splits the matching workflows into due (instant
// passed) and pending (instant ahead). workflow == "" means every configured workflow, which is how
// a mixed morning resolves correctly: at 08:00 normal is due and experiment is still pending, so the
// screen shows the frozen normal sheet and names 14:00 for experiment.
func (s *Service) dispatchGateFor(ctx context.Context, tenantID, parkID, feedDay, workflow string) (dispatchGate, error) {
	if s.schedule == nil {
		return dispatchGate{}, nil
	}
	clocks, err := s.schedule.ListScheduleClocks(ctx, tenantID, parkID, s.now())
	if err != nil {
		return dispatchGate{}, err
	}
	now := s.now().In(biztime.DefaultLocation())

	gate := dispatchGate{
		due:     make([]dueWorkflow, 0, len(clocks)),
		pending: make([]domain.WorkflowLifecycle, 0, len(clocks)),
	}
	for _, clock := range clocks {
		if workflow != "" && clock.Workflow != workflow {
			continue
		}
		issueAt, err := clock.ExpectedIssueInstant(feedDay)
		if err != nil {
			return dispatchGate{}, err
		}
		if now.Before(issueAt) {
			expected := domain.FormatBusinessInstant(issueAt)
			gate.pending = append(gate.pending, domain.WorkflowLifecycle{
				Workflow:        clock.Workflow,
				State:           domain.LifecycleStatePending,
				ExpectedIssueAt: &expected,
			})
			continue
		}
		gate.due = append(gate.due, dueWorkflow{workflow: clock.Workflow, issueAt: issueAt})
	}
	return gate, nil
}

// freezeDueWorkflows issues (persists) every due workflow's sheet, so the caller can re-read it as
// frozen rows. AsOf is the scheduled instant: IssueDirection derives feed_day = businessDate(AsOf)+1
// and the instant sits on feedDay-1, so this freezes exactly the requested day.
//
// A workflow that cannot be frozen is reported rather than swallowed -- serving a live preview after
// a failed freeze would silently reinstate the drifting numbers this gate exists to stop.
func (s *Service) freezeDueWorkflows(ctx context.Context, tenantID, parkID string, gate dispatchGate) error {
	for _, due := range gate.due {
		if _, err := s.IssueDirection(ctx, IssueRequest{
			TenantID: tenantID,
			ParkID:   parkID,
			Workflow: due.workflow,
			AsOf:     due.issueAt,
		}); err != nil {
			return fmt.Errorf("feeddirection: freeze %s at dispatch: %w", due.workflow, err)
		}
	}
	return nil
}

// pendingLifecycle is the no-rows lifecycle for a feed day whose every matching workflow is still
// ahead of its dispatch clock. The message names the earliest arrival time so the screen can say
// when to come back rather than showing an unexplained empty list.
func pendingLifecycle(feedDay string, pending []domain.WorkflowLifecycle) domain.Lifecycle {
	if len(pending) == 0 {
		pending = []domain.WorkflowLifecycle{}
	}
	return domain.Lifecycle{
		State:     domain.LifecycleStatePending,
		Message:   pendingMessage(feedDay, pending),
		Workflows: pending,
	}
}

// pendingMessage builds the operator sentence for a gated day. It names the EARLIEST expected issue
// instant among the pending workflows, because that is when something first appears on the screen.
func pendingMessage(feedDay string, pending []domain.WorkflowLifecycle) string {
	earliest := ""
	for _, wf := range pending {
		if wf.ExpectedIssueAt == nil {
			continue
		}
		if earliest == "" || *wf.ExpectedIssueAt < earliest {
			earliest = *wf.ExpectedIssueAt
		}
	}
	if earliest == "" {
		return fmt.Sprintf("the feed sheet for %s has not been issued yet", feedDay)
	}
	return fmt.Sprintf("the feed sheet for %s is issued at %s", feedDay, earliest)
}

// gateOutcome is what the serve path needs back from the gate: whether anything was frozen (and so
// can now be re-read as stored rows), the workflows still held back, and -- when nothing was frozen
// -- the lifecycle to return with the empty page.
type gateOutcome struct {
	frozeAny bool
	// pending survives a successful freeze so a mixed day (normal frozen, experiment not yet due)
	// still tells the operator when the second sheet arrives.
	pending        []domain.WorkflowLifecycle
	emptyLifecycle domain.Lifecycle
}

// gateOrFreeze applies the dispatch gate to a feed day that has no stored sheet.
//
// Order matters: the HORIZON check runs first and wins. A day outside [today, tomorrow] is never
// freeze-on-read, because freezing a past day would stamp today's herd onto a day whose real counts
// are gone -- fabrication that would then be permanent, which is strictly worse than the drifting
// preview this gate replaces.
func (s *Service) gateOrFreeze(ctx context.Context, tenantID, parkID, feedDay, workflow string) (gateOutcome, error) {
	if !s.withinFeedHorizon(feedDay) {
		return gateOutcome{emptyLifecycle: s.beyondHorizonLifecycle(feedDay)}, nil
	}
	gate, err := s.dispatchGateFor(ctx, tenantID, parkID, feedDay, workflow)
	if err != nil {
		return gateOutcome{}, err
	}
	if len(gate.due) == 0 {
		return gateOutcome{
			pending:        gate.pending,
			emptyLifecycle: pendingLifecycle(feedDay, gate.pending),
		}, nil
	}
	if err := s.freezeDueWorkflows(ctx, tenantID, parkID, gate); err != nil {
		return gateOutcome{}, err
	}
	return gateOutcome{frozeAny: true, pending: gate.pending}, nil
}

// withPendingWorkflows appends the still-gated workflows to a served lifecycle, so a mixed day
// reports both halves honestly: the frozen sheet it is serving, and the one that has not arrived.
// A workflow already described by the served headers is never duplicated.
func withPendingWorkflows(lifecycle domain.Lifecycle, pending []domain.WorkflowLifecycle) domain.Lifecycle {
	if len(pending) == 0 {
		return lifecycle
	}
	seen := make(map[string]struct{}, len(lifecycle.Workflows))
	for _, wf := range lifecycle.Workflows {
		seen[wf.Workflow] = struct{}{}
	}
	for _, wf := range pending {
		if _, dup := seen[wf.Workflow]; dup {
			continue
		}
		lifecycle.Workflows = append(lifecycle.Workflows, wf)
	}
	return lifecycle
}
