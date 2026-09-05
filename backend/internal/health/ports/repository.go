package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
)

var (
	ErrNotFound              = errors.New("health: not found")
	ErrConflict              = errors.New("health: idempotency conflict")
	ErrGoatNotAlive          = errors.New("health: goat is not alive")
	ErrProtocolNotPublished  = errors.New("health: disease protocol is not published")
	ErrAgeBandMismatch       = errors.New("health: requested age band does not match goat")
	ErrCriticalActionGuarded = errors.New("health: critical action requires policy-pack handoff")
	// ErrCaseNotOpen refuses a clinical closure (or a completion on a closure-canceled session)
	// when the case is already closed or is held by the death-review workflow, which owns it.
	ErrCaseNotOpen = errors.New("health: case is not open")
)

type Repository interface {
	OpenCase(context.Context, domain.OpenCaseInput) (domain.OpenCaseResult, error)
	ListWorkItems(context.Context, domain.ListFilter) (domain.WorkItemPage, error)
	GetWorkItem(context.Context, string, string) (domain.WorkItemDetail, error)
	CompleteWorkItem(context.Context, domain.CompleteInput) (domain.CompleteResult, error)
	CloseCase(context.Context, domain.CloseCaseInput) (domain.CloseCaseResult, error)
	HoldForDeathReview(context.Context, string, string) error
	ResumeAfterDeathRejected(context.Context, string, string) error
	CloseForApprovedDeath(context.Context, string, string, domain.DeathCause) error
}
type ProtocolImporter interface {
	ReplacePublishedProtocols(context.Context, string, string, string, string, []domain.SourceProtocol) error
}

var (
	// ErrDiagnosisNotProposed guards the confirmation gate: a diagnosis the
	// engine did not propose cannot be confirmed here.
	ErrDiagnosisNotProposed = errors.New("health: diagnosis was not proposed by this run")
	// ErrDiagnosisAlreadyDecided is a confirmation arriving for a run that has
	// already been confirmed or superseded.
	ErrDiagnosisAlreadyDecided = errors.New("health: diagnosis run already decided")
	// ErrDiagnosisNotConfirmable is a rejected form or an out-of-scope animal.
	// Neither produces anything to confirm.
	ErrDiagnosisNotConfirmable = errors.New("health: run produced no confirmable diagnosis")
	// ErrSOPNotAuthored is the fail-closed path. A confirmed diagnosis whose
	// treatment card has never been published cannot open a course: an empty
	// course is worse than a refusal, because it reads to the operator as
	// "nothing to do" rather than "nobody has written this down yet".
	ErrSOPNotAuthored = errors.New("health: treatment card is not authored for this diagnosis")
)

// EvaluateFunc is the pure diagnosis step, supplied by the service and invoked
// by the repository once it has resolved the animal inside its transaction. It
// exists so the engine is called in exactly one place: an adapter that could
// diagnose on its own could diagnose differently.
type EvaluateFunc func(diagnosis.Animal, diagnosis.Findings, diagnosis.Context) (diagnosis.Proposal, []domain.ConfirmableProblem)

// DiagnosisRepository persists observation runs and opens the courses a
// confirmation authorises.
type DiagnosisRepository interface {
	SubmitObservation(context.Context, domain.SubmitObservationInput, EvaluateFunc) (domain.SubmitObservationResult, error)
	ConfirmDiagnosis(context.Context, domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error)
	GetDiagnosisRun(context.Context, string, string) (domain.DiagnosisRun, error)
	ListDiagnosisRuns(context.Context, domain.DiagnosisQueueFilter) (domain.DiagnosisQueuePage, error)
}

// AnalyticsReader is the Health Analytics leadership read.
//
// It is a NARROW port of its own rather than three more methods on Repository:
// the analytics read is read-only, opens no case, completes no session and
// writes nothing, so a write-path fake has no business having to satisfy it.
type AnalyticsReader interface {
	GetHealthAnalytics(context.Context, domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error)
}
