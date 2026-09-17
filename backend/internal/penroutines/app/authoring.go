package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// AuthoringService is the CEO's side: write the rule, list what it raised.
type AuthoringService struct {
	repo ports.Repository
	now  func() time.Time
}

// NewAuthoringService wires the authoring service.
func NewAuthoringService(repo ports.Repository) *AuthoringService {
	return &AuthoringService{repo: repo, now: time.Now}
}

// WithClock pins the clock, for tests.
func (s *AuthoringService) WithClock(now func() time.Time) *AuthoringService {
	s.now = now
	return s
}

// Today is the IST business date.
func (s *AuthoringService) Today() string { return biztime.BusinessDate(s.now()) }

// Catalog is what the create / edit drawer needs: the park's pens and, for every assignable
// role, who currently holds it for that park.
type Catalog struct {
	Pens  []ports.CatalogPen
	Roles []ports.RoleHolders
}

// List lists the routines of one park (or all) with their counts, and the park options.
func (s *AuthoringService) List(ctx context.Context, tenantID, parkID string) ([]ports.RoutineListRow, []ports.Park, error) {
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return nil, nil, ports.ErrInvalidArgument
	}
	rows, err := s.repo.ListRoutines(ctx, ports.RoutineListParams{TenantID: tenantID, ParkID: parkID, Today: s.Today()})
	if err != nil {
		return nil, nil, err
	}
	parks, err := s.repo.ListParks(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	return rows, parks, nil
}

// Get reads one routine.
func (s *AuthoringService) Get(ctx context.Context, tenantID, routineID string) (domain.Definition, error) {
	if !uuidutil.IsUUIDString(routineID) {
		return domain.Definition{}, ports.ErrRoutineNotFound
	}
	return s.repo.GetRoutine(ctx, tenantID, routineID)
}

// Catalog reads the drawer's pens and role holders for a park.
func (s *AuthoringService) Catalog(ctx context.Context, tenantID, parkID string) (Catalog, error) {
	if !uuidutil.IsUUIDString(parkID) {
		return Catalog{}, ports.ErrInvalidArgument
	}
	pens, err := s.repo.CatalogPens(ctx, tenantID, parkID)
	if err != nil {
		return Catalog{}, err
	}
	roles, err := s.repo.RoleHoldersForPark(ctx, tenantID, parkID)
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Pens: pens, Roles: roles}, nil
}

// Create validates and writes a new routine.
func (s *AuthoringService) Create(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Definition{}, ErrIdempotencyKeyRequired
	}
	d, err := s.prepare(ctx, w.TenantID, d)
	if err != nil {
		return domain.Definition{}, err
	}
	return s.repo.CreateRoutine(ctx, w, d)
}

// Update validates and writes a new version of an existing routine.
func (s *AuthoringService) Update(ctx context.Context, w ports.WriteParams, d domain.Definition) (domain.Definition, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Definition{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(d.RoutineID) {
		return domain.Definition{}, ports.ErrRoutineNotFound
	}
	// An edit that names no start date keeps the stored one: re-anchoring an every-N-days
	// routine on today would silently move every future occurrence.
	if strings.TrimSpace(d.StartDate) == "" {
		before, err := s.repo.GetRoutine(ctx, w.TenantID, d.RoutineID)
		if err != nil {
			return domain.Definition{}, err
		}
		d.StartDate = before.StartDate
	}
	d, err := s.prepare(ctx, w.TenantID, d)
	if err != nil {
		return domain.Definition{}, err
	}
	return s.repo.UpdateRoutine(ctx, w, d)
}

// SetStatus pauses, resumes or retires a routine.
func (s *AuthoringService) SetStatus(ctx context.Context, w ports.WriteParams, routineID, status string, rowVersion int) (domain.Definition, error) {
	if strings.TrimSpace(w.IdempotencyKey) == "" {
		return domain.Definition{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(routineID) {
		return domain.Definition{}, ports.ErrRoutineNotFound
	}
	status = strings.TrimSpace(status)
	if status != domain.StatusActive && status != domain.StatusPaused && status != domain.StatusRetired {
		return domain.Definition{}, fmt.Errorf("%w: unknown status %q", domain.ErrInvalidRoutine, status)
	}
	return s.repo.SetRoutineStatus(ctx, w, routineID, status, rowVersion)
}

// ListTasks is the web Today table for one park and day.
func (s *AuthoringService) ListTasks(ctx context.Context, p ports.ParkListParams) (ports.ParkPage, error) {
	if !uuidutil.IsUUIDString(p.ParkID) {
		return ports.ParkPage{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(p.BusinessDate) == "" {
		p.BusinessDate = s.Today()
	}
	if _, err := time.Parse("2006-01-02", p.BusinessDate); err != nil {
		return ports.ParkPage{}, ports.ErrInvalidArgument
	}
	if p.RoutineID != "" && !uuidutil.IsUUIDString(p.RoutineID) {
		return ports.ParkPage{}, ports.ErrInvalidArgument
	}
	return s.repo.ListForPark(ctx, p)
}

// prepare normalizes and validates an authored definition. WHO it is for is a set of roles,
// validated against the closed vocabulary; nobody is checked by name, because the holders of a
// role change without the routine changing.
func (s *AuthoringService) prepare(_ context.Context, _ string, d domain.Definition) (domain.Definition, error) {
	d.Name = strings.TrimSpace(d.Name)
	d.Instruction = strings.TrimSpace(d.Instruction)
	d.ParkID = strings.TrimSpace(d.ParkID)
	d.NotifyTime = strings.TrimSpace(d.NotifyTime)
	if d.NotifyTime == "" {
		d.NotifyTime = "07:00"
	}
	d.StartDate = strings.TrimSpace(d.StartDate)
	if d.StartDate == "" {
		d.StartDate = s.Today()
	}
	d.Evidence = domain.NormalizeEvidence(d.Evidence)
	d.AfterWorkKinds = domain.SortWorkKinds(d.AfterWorkKinds)
	if d.CadenceKind != domain.CadenceEveryNDays {
		d.IntervalDays = 0
	}
	if !uuidutil.IsUUIDString(d.ParkID) {
		return domain.Definition{}, fmt.Errorf("%w: a park is required", domain.ErrInvalidRoutine)
	}
	pens := make([]domain.PenRef, 0, len(d.Pens))
	seenPens := map[string]bool{}
	for _, p := range d.Pens {
		p.ShedID = strings.TrimSpace(p.ShedID)
		p.Partition = strings.TrimSpace(p.Partition)
		if !uuidutil.IsUUIDString(p.ShedID) {
			return domain.Definition{}, fmt.Errorf("%w: a pen is not valid", domain.ErrInvalidRoutine)
		}
		key := domain.PenKey(p.ShedID, p.Partition)
		if seenPens[key] {
			continue
		}
		seenPens[key] = true
		pens = append(pens, p)
	}
	d.Pens = pens
	// Every pen of the park needs no list. A whole-park routine KEEPS what was sent so the
	// validation refuses pens on it rather than silently dropping them.
	if d.ScopeKind == domain.ScopeAllPens {
		d.Pens = nil
	}
	roles := make([]string, 0, len(d.AssigneeRoles))
	for _, r := range d.AssigneeRoles {
		roles = append(roles, strings.TrimSpace(r))
	}
	d.AssigneeRoles = roles
	if err := domain.ValidateDefinition(d); err != nil {
		return domain.Definition{}, err
	}
	d.AssigneeRoles = domain.SortRoles(d.AssigneeRoles)
	return d, nil
}
