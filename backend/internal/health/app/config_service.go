package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// ConfigService is the Health Config application service: it normalizes and VALIDATES authored
// input before any of it reaches the database, and leaves persistence to the adapter.
//
// Validation lives here rather than in the handler because it is a business rule, not a transport
// concern, and it must apply identically to any future caller (a seed command, a bulk import, a
// second client). The handler's job is to decode and map errors to status codes.
type ConfigService struct {
	repo ports.ProtocolAuthoring
	// catalog is the item registry a medication step must name. Optional so a caller that
	// only reads protocols (a fixture, a contract test) needs no registry.
	catalog ports.MedicineCatalog
	// rulebookChanged is told the tenant whose ACTIVE disease set may have changed, so the
	// cause-of-death list (DeathCauseCatalogService.Invalidate) shows it at once. Optional.
	rulebookChanged func(tenantID string)
}

// WithRulebookChanged registers a callback run after a write that can change which diseases
// are active or what they are called.
func (s *ConfigService) WithRulebookChanged(fn func(tenantID string)) *ConfigService {
	s.rulebookChanged = fn
	return s
}

func (s *ConfigService) notifyRulebookChanged(tenantID string) {
	if s.rulebookChanged != nil {
		s.rulebookChanged(tenantID)
	}
}

func NewConfigService(repo ports.ProtocolAuthoring) *ConfigService {
	return &ConfigService{repo: repo}
}

// WithMedicineCatalog turns on the rule that a medication step names a medicine FROM THE
// CATALOG and never free text.
func (s *ConfigService) WithMedicineCatalog(catalog ports.MedicineCatalog) *ConfigService {
	s.catalog = catalog
	return s
}

// ListMedicines is the authoring picker's read.
func (s *ConfigService) ListMedicines(ctx context.Context, tenantID string) ([]domain.CatalogItem, error) {
	if s.catalog == nil {
		return []domain.CatalogItem{}, nil
	}
	return s.catalog.ListMedicines(ctx, tenantID)
}

/*
checkMedicinesAreInTheCatalog refuses a step naming a medicine the farm does not stock.

THE POINT IS THE STORE, NOT THE SPELLING. A course that names a medicine nobody has is a
course an operator cannot carry out, and one authored by typing produced two spellings of
the same medicine and a dosage attached to something the store has never heard of. The
medicine list is maintained on /configuration/items; this is what makes it the only source.

It matches on NAME, case-insensitively, rather than on the id the editor sends, because the
importer and older clients write a name only. A step whose name matches nothing ACTIVE is a
field error naming that step, so the editor can mark the row -- the author's next move is to
pick another medicine or add the one they meant to the registry.
*/
func (s *ConfigService) checkMedicinesAreInTheCatalog(ctx context.Context, tenantID string, steps []domain.AuthoredStep) error {
	if s.catalog == nil {
		return nil
	}
	wanted := map[string][]int{}
	for i, step := range steps {
		if step.RecordType != domain.RecordTypeMedication {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(step.MedicineName))
		if name == "" {
			continue // the "Enter the medicine." rule already covers a blank.
		}
		wanted[name] = append(wanted[name], i)
	}
	if len(wanted) == 0 {
		return nil
	}

	items, err := s.catalog.ListMedicines(ctx, tenantID)
	if err != nil {
		return err
	}
	stocked := make(map[string]bool, len(items))
	for _, item := range items {
		stocked[strings.ToLower(strings.TrimSpace(item.Name))] = true
	}

	var errs []domain.FieldError
	for name, indexes := range wanted {
		if stocked[name] {
			continue
		}
		for _, i := range indexes {
			errs = append(errs, domain.FieldError{
				Field: fmt.Sprintf("steps[%d].medicine_name", i),
				Message: "Choose a medicine from the list. Add it under Configuration " +
					"\u203a Items first if it is missing.",
			})
		}
	}
	if len(errs) > 0 {
		sort.Slice(errs, func(a, b int) bool { return errs[a].Field < errs[b].Field })
		return &domain.ValidationError{Errors: errs}
	}
	return nil
}

func (s *ConfigService) ListProtocolCatalog(ctx context.Context, q domain.ProtocolCatalogQuery) (domain.ProtocolCatalogPage, error) {
	q.AgeBand = strings.ToLower(strings.TrimSpace(q.AgeBand))
	if q.AgeBand != "" && q.AgeBand != domain.AgeBandAdult && q.AgeBand != domain.AgeBandKid {
		return domain.ProtocolCatalogPage{}, &domain.ValidationError{Errors: []domain.FieldError{
			{Field: "age_band", Message: "Choose adult or kid."},
		}}
	}
	return s.repo.ListProtocolCatalog(ctx, q)
}

func (s *ConfigService) GetProtocolDetail(ctx context.Context, tenantID, protocolVersionID string) (domain.ProtocolDetail, error) {
	return s.repo.GetProtocolDetail(ctx, tenantID, protocolVersionID)
}

func (s *ConfigService) GetDraftForEdit(ctx context.Context, cmd domain.ProtocolVersionCommand, diseaseKey, ageBand string) (domain.ProtocolDetail, error) {
	diseaseKey = strings.TrimSpace(diseaseKey)
	ageBand = strings.ToLower(strings.TrimSpace(ageBand))
	if !domain.ValidDiseaseKey(diseaseKey) {
		return domain.ProtocolDetail{}, &domain.ValidationError{Errors: []domain.FieldError{
			{Field: "disease_key", Message: "Unknown disease."},
		}}
	}
	if ageBand != domain.AgeBandAdult && ageBand != domain.AgeBandKid {
		return domain.ProtocolDetail{}, &domain.ValidationError{Errors: []domain.FieldError{
			{Field: "age_band", Message: "Choose adult or kid."},
		}}
	}
	return s.repo.GetDraftForEdit(ctx, cmd, diseaseKey, ageBand)
}

// CreateDisease derives the machine key from the display name when the caller did not supply one.
//
// Deriving rather than asking is deliberate: the key is an internal join identity that an author
// has no reason to think about, and a hand-typed key that drifts from the name ("foot_rot" named
// "Footrot") makes every later log line harder to read. A caller MAY still supply one -- a seed
// command reproducing the imported keys needs to.
func (s *ConfigService) CreateDisease(ctx context.Context, cmd domain.CreateDiseaseCommand) (domain.AuthoringResult, error) {
	cmd.DisplayName = strings.Join(strings.Fields(cmd.DisplayName), " ")
	cmd.DiseaseKey = strings.ToLower(strings.TrimSpace(cmd.DiseaseKey))
	if cmd.DiseaseKey == "" {
		cmd.DiseaseKey = domain.NormalizeDiseaseKey(cmd.DisplayName)
	}
	if cmd.DurationDays == 0 {
		cmd.DurationDays = domain.DefaultDurationDays
	}

	// A new disease is validated as a DRAFT (forPublish=false): it has no steps yet, and that is
	// the whole point -- the author creates it and then authors the course.
	if err := domain.ValidateAuthoredProtocol(domain.AuthoredProtocol{
		DisplayName:  cmd.DisplayName,
		DurationDays: cmd.DurationDays,
	}, false); err != nil {
		return domain.AuthoringResult{}, err
	}
	if !domain.ValidDiseaseKey(cmd.DiseaseKey) {
		return domain.AuthoringResult{}, &domain.ValidationError{Errors: []domain.FieldError{
			{Field: "display_name", Message: "Use a name with at least one letter."},
		}}
	}
	res, err := s.repo.CreateDisease(ctx, cmd)
	if err == nil {
		s.notifyRulebookChanged(cmd.TenantID)
	}
	return res, err
}

// SaveDraft normalizes, validates as a draft, and orders the steps before persisting.
//
// Ordering at save time (rather than at read time) means the stored `seq` already reads in the
// order an operator works through the day, so every later reader -- the phone, the case's own step
// copy, an export -- gets the right order without re-deriving it.
func (s *ConfigService) SaveDraft(ctx context.Context, cmd domain.SaveDraftCommand) (domain.AuthoringResult, error) {
	cmd.DiseaseKey = strings.TrimSpace(cmd.DiseaseKey)
	cmd.AgeBand = strings.ToLower(strings.TrimSpace(cmd.AgeBand))
	cmd.Protocol = domain.NormalizeAuthoredProtocol(cmd.Protocol)

	if err := domain.ValidateAuthoredProtocol(cmd.Protocol, false); err != nil {
		return domain.AuthoringResult{}, err
	}
	if err := s.checkMedicinesAreInTheCatalog(ctx, cmd.TenantID, cmd.Protocol.Steps); err != nil {
		return domain.AuthoringResult{}, err
	}
	cmd.Protocol.Steps = domain.SortAuthoredSteps(cmd.Protocol.Steps)
	return s.repo.SaveDraft(ctx, cmd)
}

// PublishDraft applies the strict rulebook. The adapter re-validates what is actually STORED
// inside the publish transaction, which is the check that counts: this one exists so an invalid
// publish is rejected before a transaction is opened, and so the caller gets the same field errors
// either way.
func (s *ConfigService) PublishDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	res, err := s.repo.PublishDraft(ctx, cmd)
	if err == nil {
		s.notifyRulebookChanged(cmd.TenantID)
	}
	return res, err
}

func (s *ConfigService) DiscardDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	return s.repo.DiscardDraft(ctx, cmd)
}
