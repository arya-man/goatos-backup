package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// ErrAccessVersionConflict signals that the person's access changed between the
// editor loading it and the save arriving. Surfaced as a 409 so the admin
// reloads and re-decides rather than silently clobbering another admin's edit --
// a lost update here is invisible until someone cannot do their job.
var ErrAccessVersionConflict = errors.New("this person's access was changed by someone else")

// ErrPersonNotFound signals an unknown or inactive workforce member.
var ErrPersonNotFound = errors.New("person not found")

// ErrUnknownPark signals a park id that is not an active park in this tenant.
// Rejected rather than dropped: silently discarding a park the admin selected
// would narrow someone's scope without telling anyone.
var ErrUnknownPark = errors.New("unknown park")

// PersonAccessRecord is the stored access for one person, as read back.
type PersonAccessRecord struct {
	PersonID        string
	DisplayName     string
	Email           string
	DesignationCode string
	ScopeMode       string
	ParkIDs         []string
	// Assignments carry the module/surface/capability rows in the permissions
	// package's own vocabulary, so the resolver and the editor read the same
	// shape and there is no second translation between them.
	Assignments []permissions.ModuleAssignment
	RowVersion  int
}

// SavePersonAccessCommand is a fully-validated replacement of one person's access.
type SavePersonAccessCommand struct {
	TenantID string
	ActorID  string
	PersonID string

	DesignationCode string
	ScopeMode       string
	ParkIDs         []string
	Assignments     []permissions.ModuleAssignment
	// ExpectedRowVersion fences the write. A mismatch is ErrAccessVersionConflict.
	ExpectedRowVersion int
}

// AccessCatalogOption is a tenant-owned selectable value (a park, a designation).
type AccessCatalogOption struct {
	Code  string
	Label string
	Grade string
}

// PersonAccessRepository owns the person_access / person_module_access /
// person_park_scope tables and the designation catalog.
type PersonAccessRepository interface {
	// LoadPersonAccess reads one person's stored access. A person with no rows
	// yet (never migrated, or newly created) returns a record with an empty
	// Assignments slice and RowVersion 0, NOT ErrPersonNotFound -- the editor
	// must be able to open on someone who has never been given anything.
	LoadPersonAccess(ctx context.Context, tenantID, personID string) (PersonAccessRecord, error)

	// SavePersonAccess replaces the person's access in ONE transaction: header,
	// module rows, park rows, and the audit entry. A partial write would leave
	// someone holding half of a decision.
	SavePersonAccess(ctx context.Context, cmd SavePersonAccessCommand) (PersonAccessRecord, error)

	// ListParks returns the tenant's active parks for the scope picker.
	ListParks(ctx context.Context, tenantID string) ([]AccessCatalogOption, error)

	// ListDesignations returns the active job titles, in catalog order.
	ListDesignations(ctx context.Context) ([]AccessCatalogOption, error)

	// DesignationDefaults returns what picking a designation pre-fills.
	DesignationDefaults(ctx context.Context, code string) ([]permissions.ModuleAssignment, error)

	// ResolvePermissions reads the flat permission set for a principal, for the
	// request path. Kept on this interface rather than in a second repository so
	// the write and the read that enforces it cannot drift apart.
	// provisioned=false means the access tables do not exist on this deployment yet --
	// the window between the code rolling out and its migration running. The caller then
	// takes the role path. It is a separate return rather than an error because an ERROR
	// fails closed: falling back to the role path on a read failure would hand back
	// exactly the authority a person's ticks were used to remove.
	ResolvePermissions(ctx context.Context, tenantID, userID string) (perms []string, provisioned bool, err error)
	ResolveParkScope(ctx context.Context, tenantID, userID string) (scopeMode string, parkIDs []string, provisioned bool, err error)

	// ResolvePageAccess reads which admin-web pages a principal keeps, for the
	// bootstrap's sidebar composition. Reports false when the person has no stored
	// rows, and the caller then serves the unnarrowed contract -- see
	// adminui/app/person_page_lens.go.
	ResolvePageAccess(ctx context.Context, tenantID, userID string) (permissions.PageAccess, bool, error)
}
