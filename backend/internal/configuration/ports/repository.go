// Package ports is the seam between the configuration service and its stores.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
)

var (
	// ErrNotFound is a row the register does not carry.
	ErrNotFound = errors.New("configuration: row not found")
	// ErrVersionConflict is a write fenced on a row_version the row has moved past.
	ErrVersionConflict = errors.New("configuration: row changed since it was read")
	// ErrDuplicate is a name or code the register already carries.
	ErrDuplicate = errors.New("configuration: duplicate")
	// ErrInUse is an archive or delete refused because other rows still name this one.
	ErrInUse = errors.New("configuration: row is in use")
	// ErrUnknownRef is a ref field naming a row that does not exist or is archived.
	ErrUnknownRef = errors.New("configuration: unknown reference")
	// ErrIdempotencyConflict is a replay under the same key with a different payload.
	ErrIdempotencyConflict = errors.New("configuration: idempotency key reused with a different request")
)

// RefError names the field whose reference failed, so the drawer can mark it.
type RefError struct {
	Field string
	Label string
}

func (e *RefError) Error() string {
	return "configuration: " + e.Field + " names an unknown " + e.Label
}
func (e *RefError) Unwrap() error { return ErrUnknownRef }

// DuplicateError names the field the duplicate was found on.
type DuplicateError struct {
	Field   string
	Message string
}

func (e *DuplicateError) Error() string { return "configuration: duplicate " + e.Field }
func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// InUseError carries the usage that blocked an archive or delete.
type InUseError struct {
	Usage domain.Usage
}

func (e *InUseError) Error() string { return "configuration: in use: " + e.Usage.Sentence() }
func (e *InUseError) Unwrap() error { return ErrInUse }

// WriteParams is who is writing, under which key.
type WriteParams struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
}

// ListParams scopes a register read. Filters are column key -> value (ref ids or enum values).
type ListParams struct {
	Status  string // active (default) | archived | all
	Query   string
	Filters map[string]string
	// FilterJSON is a store-composed containment filter merged with Filters (an items
	// category filter becomes {"category_ids": [id]} so the whole subtree matches).
	FilterJSON map[string]any
	Cursor     string
	Limit      int
}

// Page is one keyset page of rows.
type Page struct {
	Rows       []domain.Row
	NextCursor string
	// Total is the whole-filter count, never the page's.
	Total int
}

// RefOption is one choice a ref column offers: the target row and, where the target itself
// belongs to a parent (a pen to a park), that parent's id so the drawer can narrow.
type RefOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	ParentID string `json:"parent_id,omitempty"`
	// Kind carries the item kind for a category option, so the item drawer shows the right fields.
	Kind string `json:"kind,omitempty"`
}

// Repository is what the service needs from a store.
type Repository interface {
	Counts(ctx context.Context, tenantID string) (map[string]int, error)
	List(ctx context.Context, tenantID, register string, p ListParams) (Page, error)
	Get(ctx context.Context, tenantID, register, id string) (domain.Row, error)
	Options(ctx context.Context, tenantID, register string) ([]RefOption, error)
	Usage(ctx context.Context, tenantID, register, id string) (domain.Usage, error)
	Create(ctx context.Context, w WriteParams, register string, fields map[string]any) (domain.Row, error)
	Update(ctx context.Context, w WriteParams, register, id string, fields map[string]any, rowVersion int) (domain.Row, error)
	SetStatus(ctx context.Context, w WriteParams, register, id, status string, rowVersion int) (domain.Row, error)
	Delete(ctx context.Context, w WriteParams, register, id string, rowVersion int) error
}
