package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	// ErrFastingWindowClosed refuses a create/edit whose weigh date can no
	// longer be fasted for: at or after 20:00 IST the removal window for
	// tomorrow's weighing is already open, so tomorrow is not plannable.
	ErrFastingWindowClosed = errors.New("weighing: fasting window closed for that date")

	// ErrFastingOperatorRequired refuses a create that names no feed & water
	// removal operator. The fasting task is not optional for weighing.
	ErrFastingOperatorRequired = errors.New("weighing: feed & water removal operator required")

	// ErrFastingNotAssigned refuses a fasting submit from anyone but the
	// assigned removal operator. Holding weighing.execute alone never
	// authorizes another person's card.
	ErrFastingNotAssigned = errors.New("weighing: fasting task not assigned to caller")

	// ErrFastingProofRequired refuses a submit missing either video of any
	// shed. Every shed of the round owes its own feed and water videos.
	ErrFastingProofRequired = errors.New("weighing: feed and water videos are both required")


	// ErrFastingProofInvalid refuses a submit whose proof refs do not resolve
	// to completed in-app-camera VIDEO artifacts owned by this tenant.
	ErrFastingProofInvalid = errors.New("weighing: fasting proof is not a completed live-camera video")

	// ErrFastingAlreadySubmitted refuses a second submit of a task that is not
	// in an open/rework state (an exact idempotent replay returns the original
	// result instead of this error).
	ErrFastingAlreadySubmitted = errors.New("weighing: fasting task already submitted")

	// ErrFastingSubmittedDateLocked refuses moving a campaign's weigh date
	// after its fasting task was submitted: the fast was performed for the
	// planned night and cannot be transplanted onto another date.
	ErrFastingSubmittedDateLocked = errors.New("weighing: weigh date locked, feed & water removal already submitted")
)

// FastingStore is the fasting task read/write side. It is deliberately NOT part
// of Repository so the existing planner/execution fakes keep compiling
// unchanged; the postgres Repository implements both and is wired twice.
type FastingStore interface {

	// ListFastingShedCardsForOperator serves ONE CARD PER SHED (maintainer
	// correction #2, 2026-09-03) under the same 20:00 IST visibility window and
	// terminal-campaign withholding as the round list.
	ListFastingShedCardsForOperator(ctx context.Context, tenantID, operatorUserID string, now time.Time, cursor string, limit int) (domain.FastingShedCardPage, error)

	// SubmitFastingShed records ONE shed's pair in one transaction: pair
	// validated (distinct, completed, live-camera, this tenant, not the shed's
	// rejected clips), the shed row upserted to pending_verification, and —
	// when this was the round's LAST unsubmitted shed — the parent's
	// submitted_at stamped, which is the fact the midnight gate reads. The
	// result carries the shed's evidence row for the per-shed verification
	// enqueue and the parent round as it now stands.
	SubmitFastingShed(ctx context.Context, cmd domain.SubmitFastingShed) (domain.FastingShedSubmitResult, error)

	// FastingTaskByID reads one task. operatorUserID, when non-empty, is an
	// authorization predicate inside the query (the caller must be the
	// assigned operator); empty means an internal/oversight read.
	FastingTaskByID(ctx context.Context, tenantID, fastingTaskID, operatorUserID string) (domain.FastingTask, error)


	// ApplyFastingVerdict applies a verifier approve/rework to the fasting row
	// (status + verified_by/at or rework_reason). Keyed on the verdict event id
	// for replay safety. It never touches submitted_at: the midnight gate reads
	// submission, and a rework must not un-run a weighing that already
	// happened.
	ApplyFastingVerdict(ctx context.Context, verdict domain.FastingVerdict) error

	// CampaignStartDate reads the campaign's current weigh date plus whether
	// its fasting task (if any) is already submitted — the two facts the edit
	// path's cutoff/lock checks need.
	CampaignStartDate(ctx context.Context, tenantID, campaignID string) (startDate string, fastingSubmitted bool, hasFasting bool, err error)
}
