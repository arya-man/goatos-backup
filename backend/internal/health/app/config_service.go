package app

import (
	"context"
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
}

func NewConfigService(repo ports.ProtocolAuthoring) *ConfigService {
	return &ConfigService{repo: repo}
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
	return s.repo.CreateDisease(ctx, cmd)
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
	cmd.Protocol.Steps = domain.SortAuthoredSteps(cmd.Protocol.Steps)
	return s.repo.SaveDraft(ctx, cmd)
}

// PublishDraft applies the strict rulebook. The adapter re-validates what is actually STORED
// inside the publish transaction, which is the check that counts: this one exists so an invalid
// publish is rejected before a transaction is opened, and so the caller gets the same field errors
// either way.
func (s *ConfigService) PublishDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	return s.repo.PublishDraft(ctx, cmd)
}

func (s *ConfigService) DiscardDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error) {
	return s.repo.DiscardDraft(ctx, cmd)
}
