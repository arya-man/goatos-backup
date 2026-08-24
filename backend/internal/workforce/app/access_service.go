package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// AccessService serves the People/HRMS access editor: what a person may do, on
// which surface, at what level.
//
// It composes EVERY visible word -- module labels, capability labels, blurbs,
// park names, warnings -- from the permissions catalog and tenant data. The
// client renders them verbatim and owns nothing but layout.
type AccessService struct {
	repo ports.PersonAccessRepository
}

func NewAccessService(repo ports.PersonAccessRepository) *AccessService {
	return &AccessService{repo: repo}
}

// ErrInvalidAccessRequest signals a malformed save. Wrapped with a farm-readable
// reason, because the admin needs to know WHICH tick was rejected and why.
var ErrInvalidAccessRequest = errors.New("invalid access request")

// GetPersonAccess builds the whole editor payload in one read.
func (s *AccessService) GetPersonAccess(ctx context.Context, tenantID, personID string) (domain.PersonAccessResponse, error) {
	record, err := s.repo.LoadPersonAccess(ctx, tenantID, personID)
	if err != nil {
		return domain.PersonAccessResponse{}, err
	}
	parks, err := s.repo.ListParks(ctx, tenantID)
	if err != nil {
		return domain.PersonAccessResponse{}, fmt.Errorf("parks: %w", err)
	}
	designations, err := s.repo.ListDesignations(ctx)
	if err != nil {
		return domain.PersonAccessResponse{}, fmt.Errorf("designations: %w", err)
	}

	resp := domain.PersonAccessResponse{
		PersonID:        record.PersonID,
		DisplayName:     record.DisplayName,
		Email:           record.Email,
		DesignationCode: record.DesignationCode,
		ScopeMode:       record.ScopeMode,
		ParkIDs:         record.ParkIDs,
		Modules:         moduleRows(record.Assignments),
		Capabilities:    capabilityOptions(),
		Parks:           parkOptions(parks),
		Designations:    designationOptions(designations),
		Warnings:        warnings(record.Assignments),
		RowVersion:      record.RowVersion,
	}
	if resp.ScopeMode == "" {
		resp.ScopeMode = "parks"
	}
	if resp.ParkIDs == nil {
		resp.ParkIDs = []string{}
	}
	return resp, nil
}

// SavePersonAccess validates and replaces a person's access.
func (s *AccessService) SavePersonAccess(ctx context.Context, tenantID, actorID, personID string, req domain.SavePersonAccessRequest) (domain.PersonAccessResponse, error) {
	assignments, err := validatedAssignments(req.Modules)
	if err != nil {
		return domain.PersonAccessResponse{}, err
	}
	scopeMode := strings.TrimSpace(req.ScopeMode)
	switch scopeMode {
	case "tenant", "parks":
	case "":
		scopeMode = "parks"
	default:
		return domain.PersonAccessResponse{}, fmt.Errorf("%w: %q is not a scope this screen offers", ErrInvalidAccessRequest, req.ScopeMode)
	}
	parkIDs := req.ParkIDs
	if scopeMode == "tenant" {
		// A tenant-wide person needs no park rows, and keeping a stale list would go
		// out of date the moment a park is added -- quietly narrowing someone who is
		// meant to see everything.
		parkIDs = nil
	} else if len(parkIDs) == 0 {
		return domain.PersonAccessResponse{}, fmt.Errorf("%w: choose at least one park, or set this person to cover every park", ErrInvalidAccessRequest)
	}

	if _, err := s.repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID:           tenantID,
		ActorID:            actorID,
		PersonID:           personID,
		DesignationCode:    strings.TrimSpace(req.DesignationCode),
		ScopeMode:          scopeMode,
		ParkIDs:            parkIDs,
		Assignments:        assignments,
		ExpectedRowVersion: req.RowVersion,
	}); err != nil {
		return domain.PersonAccessResponse{}, err
	}
	// Read back rather than echoing the request: the response carries the new
	// row version and the recomputed warnings, and the editor must show what was
	// actually stored, not what was sent.
	return s.GetPersonAccess(ctx, tenantID, personID)
}

// DesignationDefaults reports what picking a job title pre-fills.
func (s *AccessService) DesignationDefaults(ctx context.Context, code string) (domain.DesignationDefaultsResponse, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return domain.DesignationDefaultsResponse{}, fmt.Errorf("%w: no designation named", ErrInvalidAccessRequest)
	}
	assignments, err := s.repo.DesignationDefaults(ctx, code)
	if err != nil {
		return domain.DesignationDefaultsResponse{}, err
	}
	designations, err := s.repo.ListDesignations(ctx)
	if err != nil {
		return domain.DesignationDefaultsResponse{}, err
	}
	label := code
	for _, d := range designations {
		if d.Code == code {
			label = d.Label
			break
		}
	}

	byModule := map[string]*domain.AccessModuleWrite{}
	order := make([]string, 0, len(assignments))
	for _, a := range assignments {
		row, seen := byModule[a.Module]
		if !seen {
			row = &domain.AccessModuleWrite{ModuleKey: a.Module, Web: []string{}, Mobile: []string{}}
			byModule[a.Module] = row
			order = append(order, a.Module)
		}
		if a.Surface == permissions.SurfaceWeb {
			row.Web = append(row.Web, a.Capabilities...)
		} else {
			row.Mobile = append(row.Mobile, a.Capabilities...)
		}
	}
	sort.Strings(order)
	out := make([]domain.AccessModuleWrite, 0, len(order))
	for _, key := range order {
		out = append(out, *byModule[key])
	}
	return domain.DesignationDefaultsResponse{Code: code, Label: label, Modules: out}, nil
}

// validatedAssignments turns the request rows into catalog-checked assignments.
//
// It REJECTS an unknown module, an unknown capability, and a capability the
// module does not offer on that surface, rather than dropping them. Dropping
// would let the editor show a tick that grants nothing -- the exact failure the
// resolver's fail-closed behaviour protects against at read time, surfaced here
// as an error the admin can see instead of an access gap they discover later.
func validatedAssignments(rows []domain.AccessModuleWrite) ([]permissions.ModuleAssignment, error) {
	out := make([]permissions.ModuleAssignment, 0, len(rows)*2)
	seen := map[string]struct{}{}
	for _, row := range rows {
		module := strings.TrimSpace(row.ModuleKey)
		def, ok := permissions.LookupModuleCapability(module)
		if !ok {
			return nil, fmt.Errorf("%w: %q is not a module on this screen", ErrInvalidAccessRequest, row.ModuleKey)
		}
		if _, dup := seen[module]; dup {
			return nil, fmt.Errorf("%w: %q appears twice", ErrInvalidAccessRequest, def.Label)
		}
		seen[module] = struct{}{}

		for _, pair := range []struct {
			surface string
			levels  []string
		}{
			{permissions.SurfaceWeb, row.Web},
			{permissions.SurfaceMobile, row.Mobile},
		} {
			levels, err := validatedLevels(def, pair.surface, pair.levels)
			if err != nil {
				return nil, err
			}
			if len(levels) == 0 {
				// An empty set is a real answer -- "no access to this module on this
				// surface" -- and it is stored by NOT writing a row, so the resolver
				// grants nothing. No row is needed here.
				continue
			}
			out = append(out, permissions.ModuleAssignment{
				Module:       module,
				Surface:      pair.surface,
				Capabilities: levels,
			})
		}
	}
	return out, nil
}

func validatedLevels(def permissions.ModuleCapability, surface string, levels []string) ([]string, error) {
	if len(levels) == 0 {
		return nil, nil
	}
	if !permissions.ModuleSupportsSurface(def.Key, surface) {
		return nil, fmt.Errorf("%w: %s is not available on %s", ErrInvalidAccessRequest, def.Label, surfaceLabel(surface))
	}
	chosen := map[string]struct{}{}
	for _, level := range levels {
		level = strings.TrimSpace(level)
		if level == "" || level == permissions.LevelNone {
			continue
		}
		if !permissions.LevelOffered(def.Key, level) {
			return nil, fmt.Errorf("%w: %s does not offer %q", ErrInvalidAccessRequest, def.Label, level)
		}
		chosen[level] = struct{}{}
	}
	// Catalog order, not request order: the stored row is compared, diffed and
	// rendered, and a set that reorders between saves reads as a change nobody made.
	out := make([]string, 0, len(chosen))
	for _, level := range permissions.LevelOrder {
		if _, ok := chosen[level]; ok {
			out = append(out, level)
		}
	}
	return out, nil
}

func surfaceLabel(surface string) string {
	if surface == permissions.SurfaceMobile {
		return "the phone"
	}
	return "the web console"
}

// moduleRows renders every catalog module, whether or not the person holds it --
// the editor is a complete picture of what could be granted, not a list of what
// already is.
func moduleRows(assignments []permissions.ModuleAssignment) []domain.AccessModuleRow {
	granted := map[string][]string{}
	for _, a := range assignments {
		granted[a.Module+"|"+a.Surface] = a.Capabilities
	}
	catalog := permissions.ModuleCapabilities()
	out := make([]domain.AccessModuleRow, 0, len(catalog))
	for _, def := range catalog {
		out = append(out, domain.AccessModuleRow{
			ModuleKey:     def.Key,
			Label:         def.Label,
			Blurb:         def.Blurb,
			OfferedWeb:    offeredLevels(def, permissions.SurfaceWeb),
			OfferedMobile: offeredLevels(def, permissions.SurfaceMobile),
			GrantedWeb:    orEmpty(granted[def.Key+"|"+permissions.SurfaceWeb]),
			GrantedMobile: orEmpty(granted[def.Key+"|"+permissions.SurfaceMobile]),
		})
	}
	return out
}

func offeredLevels(def permissions.ModuleCapability, surface string) []string {
	if !permissions.ModuleSupportsSurface(def.Key, surface) {
		return []string{}
	}
	out := make([]string, 0, len(permissions.LevelOrder))
	for _, level := range permissions.LevelOrder {
		if level == permissions.LevelNone {
			continue
		}
		if _, offered := def.Levels[level]; offered {
			out = append(out, level)
		}
	}
	return out
}

func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func capabilityOptions() []domain.AccessCapabilityOption {
	out := make([]domain.AccessCapabilityOption, 0, len(permissions.CapabilityVocabulary))
	for _, c := range permissions.CapabilityVocabulary {
		out = append(out, domain.AccessCapabilityOption{Level: c.Level, Label: c.Label, Blurb: c.Blurb})
	}
	return out
}

func parkOptions(in []ports.AccessCatalogOption) []domain.AccessParkOption {
	out := make([]domain.AccessParkOption, 0, len(in))
	for _, o := range in {
		out = append(out, domain.AccessParkOption{ParkID: o.Code, Label: o.Label})
	}
	return out
}

func designationOptions(in []ports.AccessCatalogOption) []domain.AccessDesignationOption {
	out := make([]domain.AccessDesignationOption, 0, len(in))
	for _, o := range in {
		out = append(out, domain.AccessDesignationOption{Code: o.Code, Label: o.Label, Grade: o.Grade})
	}
	return out
}

// warnings composes the separation-of-duty cautions, with the module's FARM name
// rather than its key.
func warnings(assignments []permissions.ModuleAssignment) []domain.AccessWarning {
	risks := permissions.SeparationRisks(assignments)
	out := make([]domain.AccessWarning, 0, len(risks))
	for _, r := range risks {
		message := r.Reason
		if def, ok := permissions.LookupModuleCapability(r.ConflictsWithModule); ok && def.Label != "" {
			message = "This person carries out " + def.Label + " work and would also approve the proof for it. " +
				"Video verification is meant to be a second pair of eyes."
		}
		out = append(out, domain.AccessWarning{ModuleKey: r.ConflictsWithModule, Message: message})
	}
	return out
}
