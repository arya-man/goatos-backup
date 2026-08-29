package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// ErrDuplicateEmail signals that an active workforce member already carries the
// email being created. Surfaced as a 409 so the admin sees "already exists"
// rather than a second person row for the same login.
var ErrDuplicateEmail = errors.New("a person with this email already exists")

type ListPeopleParams struct {
	TenantID     string
	ParkID       string
	DepartmentID string
	Status       string
	Search       string
	Limit        int
	// Cursor is the opaque keyset cursor from a previous page's NextCursor.
	Cursor string
}

// CreatePersonCommand is the fully-resolved write: the app service has already
// validated the request, ensured the Firebase account, and derived UserID. The
// repository's job is ONE transaction — idempotency reservation, member insert,
// scope grant, allowlist admission, audit — all-or-nothing.
type CreatePersonCommand struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string

	UserID      string
	FirstName   string
	LastName    string
	DisplayName string
	Email       string
	// NormalizedEmail is authallow.NormalizeEmail(Email); stored on the
	// allowlist row and used for the duplicate probe.
	NormalizedEmail string

	Role      string
	ScopeType string // "park" or "tenant"
	ScopeID   string

	RoleHint         string
	DesignationGrade string // "" = ungraded
	ParkID           string // "" = none; primary_location_id
	DepartmentID     string // "" = none
}

type PreflightCreatePersonCommand struct {
	TenantID         string
	IdempotencyKey   string
	NormalizedEmail  string
	FirstName        string
	LastName         string
	Role             string
	ScopeType        string
	ScopeID          string
	DepartmentID     string
	DesignationGrade string
}

type PreflightCreatePersonResult struct {
	Replay *domain.PersonSummary
}

// PeopleRepository is the People/HRMS directory port, implemented by the same
// postgres Repository as the operator port.
type PeopleRepository interface {
	ListPeople(ctx context.Context, params ListPeopleParams) ([]domain.PersonSummary, string, error)
	PeopleCatalog(ctx context.Context, tenantID string) (domain.PeopleCatalog, error)
	PreflightCreatePerson(ctx context.Context, cmd PreflightCreatePersonCommand) (PreflightCreatePersonResult, error)
	CreatePerson(ctx context.Context, cmd CreatePersonCommand) (domain.PersonSummary, error)
}
