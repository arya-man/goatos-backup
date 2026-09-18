// Package app is the configuration service: the one place a register write is validated against
// its definition before the store sees it, and the one place a store error becomes a farm
// sentence.
package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
)

// Service validates and forwards register reads and writes.
type Service struct {
	repo ports.Repository
}

// NewService constructs the service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

// DefaultPageSize is the list page when the caller names none; MaxPageSize its ceiling.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Registers is the catalog the screen renders from: the static registers plus one per
// reference list the tenant keeps, in rail order.
func (s *Service) Registers(ctx context.Context, tenantID string) ([]domain.Register, error) {
	lists, err := s.repo.ReferenceLists(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Register, 0, len(domain.Registers)+len(lists))
	out = append(out, domain.Registers...)
	for _, list := range lists {
		out = append(out, domain.ReferenceRegister(list))
	}
	return out, nil
}

// Register resolves one register with its real label (a reference list's own name).
func (s *Service) Register(ctx context.Context, tenantID, key string) (domain.Register, error) {
	reg, ok := domain.RegisterByKey(key)
	if !ok {
		return domain.Register{}, domain.ErrUnknownRegister
	}
	if !domain.IsReferenceRegister(key) {
		return reg, nil
	}
	list, err := s.repo.Get(ctx, tenantID, domain.RegReferenceLists, domain.ReferenceListKey(key))
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return domain.Register{}, domain.ErrUnknownRegister
		}
		return domain.Register{}, err
	}
	return domain.ReferenceRegister(domain.ReferenceList{Key: list.ID, Name: domain.FieldString(list.Fields, "name"), Description: domain.FieldString(list.Fields, "description"), IsBuiltin: list.IsBuiltin}), nil
}

// Counts is the active-row count per register, for the rail.
func (s *Service) Counts(ctx context.Context, tenantID string) (map[string]int, error) {
	return s.repo.Counts(ctx, tenantID)
}

// List is one keyset page of a register.
func (s *Service) List(ctx context.Context, tenantID, register string, p ports.ListParams) (ports.Page, error) {
	reg, ok := domain.RegisterByKey(register)
	if !ok {
		return ports.Page{}, domain.ErrUnknownRegister
	}
	switch p.Status {
	case "", domain.StatusActive:
		p.Status = domain.StatusActive
	case domain.StatusArchived, "all":
	default:
		return ports.Page{}, BadRequest("invalid_status", "Status must be active, archived or all.")
	}
	if p.Limit <= 0 {
		p.Limit = DefaultPageSize
	}
	if p.Limit > MaxPageSize {
		p.Limit = MaxPageSize
	}
	for key := range p.Filters {
		if _, known := reg.Column(key); !known {
			return ports.Page{}, BadRequest("invalid_filter", "That filter is not part of "+strings.ToLower(reg.Label)+".")
		}
	}
	return s.repo.List(ctx, tenantID, register, p)
}

// Get is one row.
func (s *Service) Get(ctx context.Context, tenantID, register, id string) (domain.Row, error) {
	if _, ok := domain.RegisterByKey(register); !ok {
		return domain.Row{}, domain.ErrUnknownRegister
	}
	return s.repo.Get(ctx, tenantID, register, id)
}

// Options is every active row of a register as ref choices.
func (s *Service) Options(ctx context.Context, tenantID, register string) ([]ports.RefOption, error) {
	if _, ok := domain.RegisterByKey(register); !ok {
		return nil, domain.ErrUnknownRegister
	}
	return s.repo.Options(ctx, tenantID, register)
}

// Usage is what still names the row.
func (s *Service) Usage(ctx context.Context, tenantID, register, id string) (domain.Usage, error) {
	if _, ok := domain.RegisterByKey(register); !ok {
		return domain.Usage{}, domain.ErrUnknownRegister
	}
	return s.repo.Usage(ctx, tenantID, register, id)
}

func writable(register string) (domain.Register, error) {
	reg, ok := domain.RegisterByKey(register)
	if !ok {
		return domain.Register{}, domain.ErrUnknownRegister
	}
	if reg.ReadOnly {
		return domain.Register{}, domain.ErrReadOnlyRegister
	}
	return reg, nil
}

// Create validates the fields and inserts the row.
func (s *Service) Create(ctx context.Context, w ports.WriteParams, register string, raw map[string]any) (domain.Row, error) {
	reg, err := writable(register)
	if err != nil {
		return domain.Row{}, err
	}
	kind, err := s.kindFor(ctx, w.TenantID, reg, raw, nil)
	if err != nil {
		return domain.Row{}, err
	}
	fields, err := domain.ValidateWrite(reg, raw, nil, kind)
	if err != nil {
		return domain.Row{}, err
	}
	return s.repo.Create(ctx, w, register, fields)
}

// Update validates the changed fields against the stored row and writes them, fenced on the
// row_version the screen read.
func (s *Service) Update(ctx context.Context, w ports.WriteParams, register, id string, raw map[string]any, rowVersion int) (domain.Row, error) {
	reg, err := writable(register)
	if err != nil {
		return domain.Row{}, err
	}
	existing, err := s.repo.Get(ctx, w.TenantID, register, id)
	if err != nil {
		return domain.Row{}, err
	}
	kind, err := s.kindFor(ctx, w.TenantID, reg, raw, &existing)
	if err != nil {
		return domain.Row{}, err
	}
	fields, err := domain.ValidateWrite(reg, raw, &existing, kind)
	if err != nil {
		return domain.Row{}, err
	}
	if len(fields) == 0 {
		return existing, nil
	}
	return s.repo.Update(ctx, w, register, id, fields, rowVersion)
}

// SetStatus archives or restores a row. A built-in row is never archived; an archive of a row
// still named by others is refused with the usage, so the person can move those first.
func (s *Service) SetStatus(ctx context.Context, w ports.WriteParams, register, id, status string, rowVersion int) (domain.Row, error) {
	if _, err := writable(register); err != nil {
		return domain.Row{}, err
	}
	if status != domain.StatusActive && status != domain.StatusArchived {
		return domain.Row{}, BadRequest("invalid_status", "Status must be active or archived.")
	}
	existing, err := s.repo.Get(ctx, w.TenantID, register, id)
	if err != nil {
		return domain.Row{}, err
	}
	if status == domain.StatusArchived && existing.IsBuiltin {
		return domain.Row{}, domain.ErrBuiltin
	}
	if existing.Status == status {
		return existing, nil
	}
	return s.repo.SetStatus(ctx, w, register, id, status, rowVersion)
}

// Delete removes a row nothing names. The store re-checks usage inside the transaction.
func (s *Service) Delete(ctx context.Context, w ports.WriteParams, register, id string, rowVersion int) error {
	if _, err := writable(register); err != nil {
		return err
	}
	existing, err := s.repo.Get(ctx, w.TenantID, register, id)
	if err != nil {
		return err
	}
	if existing.IsBuiltin {
		return domain.ErrBuiltin
	}
	return s.repo.Delete(ctx, w, register, id, rowVersion)
}

// kindFor resolves the item kind an items write is validated under: the kind of the category the
// write names (or the stored row's category when the write leaves it alone). Only the items
// register has kind-scoped columns; every other register resolves to "".
func (s *Service) kindFor(ctx context.Context, tenantID string, reg domain.Register, raw map[string]any, existing *domain.Row) (string, error) {
	if reg.Key != domain.RegItems {
		return "", nil
	}
	categoryID := domain.FieldString(raw, "category_id")
	if categoryID == "" && existing != nil {
		categoryID = domain.FieldString(existing.Fields, "category_id")
	}
	if categoryID == "" {
		return "", nil
	}
	cat, err := s.repo.Get(ctx, tenantID, domain.RegCategories, categoryID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return "", &domain.ValidationError{Fields: []domain.FieldError{{Field: "category_id", Code: "unknown", Message: "Choose a category from the list."}}}
		}
		return "", err
	}
	return domain.FieldString(cat.Fields, "kind"), nil
}

// Error is the transport-facing error shape: a stable code plus a farm-worded message.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Fields     []domain.FieldError
}

func (e *Error) Error() string { return e.Code }

// BadRequest builds a 400 with a stable code.
func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

// HTTPError maps a module error onto the transport shape.
func HTTPError(err error) *Error {
	var appErr *Error
	var vErr *domain.ValidationError
	var refErr *ports.RefError
	var dupErr *ports.DuplicateError
	var inUse *ports.InUseError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &appErr):
		return appErr
	case errors.As(err, &vErr):
		return &Error{Code: "invalid_fields", Message: "Some fields need attention.", HTTPStatus: http.StatusUnprocessableEntity, Fields: vErr.Fields}
	case errors.As(err, &refErr):
		return &Error{Code: "unknown_reference", Message: "Choose a " + strings.ToLower(refErr.Label) + " from the list.", HTTPStatus: http.StatusUnprocessableEntity,
			Fields: []domain.FieldError{{Field: refErr.Field, Code: "unknown", Message: "Choose a " + strings.ToLower(refErr.Label) + " from the list."}}}
	case errors.As(err, &dupErr):
		return &Error{Code: "duplicate", Message: dupErr.Message, HTTPStatus: http.StatusConflict,
			Fields: []domain.FieldError{{Field: dupErr.Field, Code: "duplicate", Message: dupErr.Message}}}
	case errors.As(err, &inUse):
		return &Error{Code: "in_use", Message: inUse.Usage.Sentence() + ". Move or change those first.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, domain.ErrUnknownRegister):
		return &Error{Code: "unknown_register", Message: "That list does not exist.", HTTPStatus: http.StatusNotFound}
	case errors.Is(err, domain.ErrReadOnlyRegister):
		return &Error{Code: "read_only_register", Message: "This list is edited elsewhere.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, domain.ErrBuiltin):
		return &Error{Code: "builtin_row", Message: "This is built into the product and cannot be removed. You can rename it.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrNotFound):
		return &Error{Code: "not_found", Message: "That record no longer exists.", HTTPStatus: http.StatusNotFound}
	case errors.Is(err, ports.ErrVersionConflict):
		return &Error{Code: "write_conflict", Message: "Someone else changed this record. Reload and try again.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return &Error{Code: "idempotency_conflict", Message: "That save was already sent with different values. Reload and try again.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrInUse):
		return &Error{Code: "in_use", Message: "Other records still use this one. Move or change those first.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrDuplicate):
		return &Error{Code: "duplicate", Message: "A record with that name already exists.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, context.DeadlineExceeded):
		return &Error{Code: "timeout", Message: "That took too long. Try again.", HTTPStatus: http.StatusGatewayTimeout}
	}
	return &Error{Code: "internal_error", Message: "Something went wrong. Try again.", HTTPStatus: http.StatusInternalServerError}
}
