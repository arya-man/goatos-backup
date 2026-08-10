package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// This file adds the WRITE and lifecycle boundaries the issued-sheet feature needs. The generation
// ports above stay read-only; these own the frozen feed_direction_issues / feed_direction_issue_rows
// tables and the feed_schedule_config clock read that drives when a sheet is issued.
var (
	// ErrIssueNotFound is returned when an amend or lock addresses a day/workflow that was never
	// issued. You cannot correct or lock a sheet that does not exist.
	ErrIssueNotFound = errors.New("feeddirection: no issued sheet for that park/feed_day/workflow")
	// ErrReissueAfterAmendOrLock is returned when a plain re-Issue arrives with CHANGED inputs after
	// the sheet has already been amended or locked. A correction batch or the transport lock has
	// acted on the issued document; changing it now must go through Amend, not a silent re-Issue.
	ErrReissueAfterAmendOrLock = errors.New("feeddirection: sheet already amended or locked; re-issue with changed inputs is refused")
	// ErrAmendAfterLock is returned when an amend arrives after the transport lock. A later change
	// rolls to the next feed day.
	ErrAmendAfterLock = errors.New("feeddirection: sheet is locked; amend is refused")
	// ErrWorkflowNotConfigured is returned when an issue is requested for a (park, workflow) that has
	// no feed_schedule_config clock. There is no authored time to issue against.
	ErrWorkflowNotConfigured = errors.New("feeddirection: no feed_schedule_config for that park/workflow")
	// ErrInvalidWorkflow is returned for a workflow value that is neither normal nor experiment.
	ErrInvalidWorkflow = errors.New("feeddirection: workflow must be normal or experiment")
)

// Issue write outcomes, echoed so a worker log and a test can assert what actually happened.
const (
	IssueOutcomeInserted   = "inserted"  // first issue of the day/workflow.
	IssueOutcomeReplayed   = "replayed"  // exact re-issue (same fingerprint): no side effects.
	IssueOutcomeReissued   = "reissued"  // changed inputs while still 'issued': rows replaced.
	AmendOutcomeAmended    = "amended"   // a correction changed some sheds.
	AmendOutcomeUnchanged  = "unchanged" // amend ran but nothing moved (still recorded).
	LockOutcomeLocked      = "locked"    // transitioned to locked.
	LockOutcomeAlreadyDone = "already_locked"
)

// PersistIssueCommand carries a freshly generated, frozen sheet for one (park, feed_day, workflow),
// plus the idempotency envelope. The store applies it atomically.
type PersistIssueCommand struct {
	TenantID    string
	ParkID      string
	FeedDay     string // Asia/Kolkata business date, YYYY-MM-DD
	Workflow    string
	IssuedAt    time.Time
	Fingerprint string
	// IdempotencyKey is the stable operation identity; an exact re-issue replays against it.
	IdempotencyKey string
	GeneratedBy    string
	// Cells is the WHOLE generated scope for this workflow, in generation order.
	Cells []domain.StoredCell
}

// IssueResult is the outcome of a PersistIssue call.
type IssueResult struct {
	Header  domain.IssueHeader
	Outcome string
}

// AmendIssueCommand carries a fresh recompute to diff against the stored sheet.
type AmendIssueCommand struct {
	TenantID    string
	ParkID      string
	FeedDay     string
	Workflow    string
	AmendedAt   time.Time
	Fingerprint string
	Cells       []domain.StoredCell
}

// AmendResult is the outcome of an AmendIssue call.
type AmendResult struct {
	Header          domain.IssueHeader
	Outcome         string
	AffectedShedIDs []string
	// HeadCountChangedPens is the strictly narrower subset of operational locations whose ANIMAL
	// COUNT the correction moved. It drives the packing reopen and nothing else -- see
	// domain.CellDiff.HeadCountChangedPens for why a cosmetic change must not appear here.
	HeadCountChangedPens []domain.PenKey
}

// LockIssueCommand locks a day/workflow's sheet.
type LockIssueCommand struct {
	TenantID string
	ParkID   string
	FeedDay  string
	Workflow string
	LockedAt time.Time
}

// LockResult is the outcome of a LockIssue call.
type LockResult struct {
	Header  domain.IssueHeader
	Outcome string
}

// IssueStore owns the frozen issue tables. Every method is one transaction and idempotent.
type IssueStore interface {
	// PersistIssue issues (or exactly-replays, or re-issues in place) one workflow's sheet in one
	// transaction. See the outcome constants and ErrReissueAfterAmendOrLock.
	PersistIssue(ctx context.Context, cmd PersistIssueCommand) (IssueResult, error)
	// AmendIssue diffs a fresh recompute against the stored sheet and writes ONLY the changed cells,
	// flipping the issue to 'amended'. A no-op amend is still recorded. Refuses a locked sheet.
	AmendIssue(ctx context.Context, cmd AmendIssueCommand) (AmendResult, error)
	// LockIssue transitions a sheet to 'locked'. Idempotent.
	LockIssue(ctx context.Context, cmd LockIssueCommand) (LockResult, error)

	// LoadIssueHeaders returns the (at most two) live issue headers for a park + feed day, optionally
	// narrowed to one workflow. Used by the read path to decide issued-vs-pending-vs-never.
	LoadIssueHeaders(ctx context.Context, tenantID, parkID, feedDay, workflow string) ([]domain.IssueHeader, error)
	// LoadIssueRows returns the stored cells for a set of issues in ONE set-based read, keyed by
	// issue id. Bounded by the (<=2) issues of a park-day, never a per-shed fan-out.
	LoadIssueRows(ctx context.Context, tenantID string, issueIDs []string) (map[string][]domain.StoredCell, error)
}

// ScheduleReader reads the feed_schedule_config dispatch clock (migration 000004) that until now was
// read by nothing. It answers WHEN a sheet is issued and WHICH workflows a park runs.
type ScheduleReader interface {
	// ListScheduleClocks returns every configured (workflow, clock) for a park, in force on asOfDate.
	ListScheduleClocks(ctx context.Context, tenantID, parkID string, asOf time.Time) ([]domain.WorkflowClock, error)
	// ListScheduledParks returns the distinct parks that have any feed_schedule_config in force on
	// asOfDate -- the worker's park set when it is asked to issue for all parks.
	ListScheduledParks(ctx context.Context, tenantID string, asOf time.Time) ([]string, error)
}
