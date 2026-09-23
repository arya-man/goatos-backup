package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

var (
	// ErrRegisterDraftExists is returned when a second author opens a draft on a
	// register that already has one. It is not a race to be retried: the first
	// author's unpublished edits are sitting in that draft.
	ErrRegisterDraftExists = errors.New("health: a draft is already open for this register")

	// ErrRegisterNotFound is an unknown version id or animal class.
	ErrRegisterNotFound = errors.New("health: diagnosis register not found")

	// ErrRegisterNotADraft is returned when publish or discard addresses a published
	// or retired version. A published register is immutable and a retired one is
	// history; editing either would rewrite what animals were actually diagnosed
	// against.
	ErrRegisterNotADraft = errors.New("health: register version is not a draft")

	// ErrRegisterTreatsUnknown is returned when a rule's treats names a disease that
	// does not exist in this tenant's catalog at all.
	//
	// It is DISTINCT from a disease whose treatment card nobody has authored yet, and
	// the distinction is the whole value of the check: a key that matches no disease
	// is a TYPO, and publishing it would leave a diagnosis that fires and then cannot
	// open a course. A disease that exists but has no published protocol is WORK NOT
	// YET DONE -- nine of the register's own diagnoses are in that state today -- and
	// refusing it would make the farm's own rulebook unpublishable.
	ErrRegisterTreatsUnknown = errors.New("health: a rule treats a disease that does not exist")
)

// RegisterAuthoring is the diagnosis-register half of Health Config.
type RegisterAuthoring interface {
	// ListRegisters returns the live and draft register for every animal class.
	ListRegisters(ctx context.Context, tenantID string) ([]domain.RegisterSummary, error)

	// GetRegister returns one version with its document and its current verdict.
	GetRegister(ctx context.Context, tenantID, registerVersionID string) (domain.RegisterDetail, error)

	// GetDraftForEdit returns the open draft for a class, CREATING it as a copy of
	// the published version when none is open. Opening the editor is what creates a
	// draft, so a vet never has to decide to "start" one -- and the copy is why an
	// edit begins from what is live rather than from a blank form.
	GetRegisterDraftForEdit(ctx context.Context, cmd domain.RegisterVersionCommand, animalClass string) (domain.RegisterDetail, error)

	// SaveDraft replaces a draft's whole document.
	SaveRegisterDraft(ctx context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error)

	// PublishDraft promotes a draft and retires the version it replaces, in one
	// transaction, after re-validating what is actually STORED -- including every
	// rule's treats against the published protocol catalog, which is the check that
	// can only be made here because it reads another table.
	PublishRegisterDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error)

	// DiscardDraft deletes a draft. Published versions are never deletable.
	DiscardRegisterDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error)

	// PublishedRegister is the SERVING read: the live register for one animal class.
	// A class with no published version returns ErrRegisterNotFound, and the caller
	// falls back to the committed seed rather than diagnosing against nothing.
	PublishedRegister(ctx context.Context, tenantID, animalClass string) (domain.RegisterDetail, error)
}

// DiagnosisTypeAuthoring is the ROUTING half of Health Config: which types exist, and which
// animals reach each one (migration 000395).
//
// It is a separate interface from RegisterAuthoring because the two answer different questions
// and a caller usually wants one of them: RegisterAuthoring edits a type's RULES, this decides
// who those rules are applied to.
type DiagnosisTypeAuthoring interface {
	// DiagnosisRouting is the whole Types screen in one read: the types, the routes with their
	// labels and live animal counts, and the stages that hold animals and reach no type.
	DiagnosisRouting(ctx context.Context, tenantID string) (domain.DiagnosisRoutingView, error)

	// SaveDiagnosisType creates a type or relabels/retires an existing one. The KEY is set on
	// creation and never rewritten: a stored run names the type it was judged under.
	SaveDiagnosisType(ctx context.Context, cmd domain.SaveDiagnosisTypeCommand) (domain.DiagnosisType, error)

	// SaveStageRoute points one stage, or a whole age band, at a type.
	SaveStageRoute(ctx context.Context, cmd domain.SaveStageRouteCommand) (domain.StageRouteRow, error)

	// DeleteStageRoute removes one route. Removing a band wildcard makes that whole band
	// fail-closed, which is a real choice a farm may make and not an error.
	DeleteStageRoute(ctx context.Context, cmd domain.DeleteStageRouteCommand) error
}
