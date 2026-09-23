package app

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// RegisterConfigService is the diagnosis-register half of Health Config.
//
// Validation lives here rather than in the handler for the same reason it does for the
// treatment protocols: it is a business rule, not a transport concern, and it must
// apply identically to a seed command, a bulk import or a second client. The handler
// decodes and maps errors to status codes.
//
// The adapter re-validates what is actually STORED inside the publish transaction.
// That is the check that counts; this one exists so an author is told what is wrong
// before a transaction is opened, and so both paths return the same field errors.
type RegisterConfigService struct {
	repo  ports.RegisterAuthoring
	types ports.DiagnosisTypeAuthoring
}

// NewRegisterConfigService wires the register editor.
//
// `types` may be nil, and a nil one falls back to the four SHIPPED classes. That is not a
// convenience: several callers construct this service for the register half alone, and making
// the type source mandatory would force them to carry a routing repository they never use. What
// it must never become is a fallback on the SERVING path -- see validClass.
func NewRegisterConfigService(repo ports.RegisterAuthoring) *RegisterConfigService {
	return &RegisterConfigService{repo: repo}
}

// WithTypes lets the register editor author a register for any ACTIVE authored type, not just
// the four the engine shipped with (migration 000395). Without it, creating a type on the
// routing screen would produce one nobody could ever write rules for.
func (s *RegisterConfigService) WithTypes(types ports.DiagnosisTypeAuthoring) *RegisterConfigService {
	s.types = types
	return s
}

func (s *RegisterConfigService) ListRegisters(ctx context.Context, tenantID string) ([]domain.RegisterSummary, error) {
	return s.repo.ListRegisters(ctx, tenantID)
}

func (s *RegisterConfigService) GetRegister(ctx context.Context, tenantID, versionID string) (domain.RegisterDetail, error) {
	return s.repo.GetRegister(ctx, tenantID, versionID)
}

func (s *RegisterConfigService) GetDraftForEdit(ctx context.Context, cmd domain.RegisterVersionCommand, animalClass string) (domain.RegisterDetail, error) {
	animalClass = normalizeClass(animalClass)
	if err := s.validClass(ctx, cmd.TenantID, animalClass); err != nil {
		return domain.RegisterDetail{}, err
	}
	return s.repo.GetRegisterDraftForEdit(ctx, cmd, animalClass)
}

// SaveDraft validates the whole document as a DRAFT.
//
// A draft is held to the same standard as a publish, deliberately. It is tempting to
// let a half-finished register save freely and only check it on publish, but the two
// direction checks -- a rule naming a token no answer emits, an answer reaching no
// rule -- are exactly what an author needs while they are still editing. Told at save
// time, a missing emit is one line to fix; told at publish time it is a hunt through a
// document that has moved on.
func (s *RegisterConfigService) SaveDraft(ctx context.Context, cmd domain.SaveRegisterDraftCommand) (domain.RegisterAuthoringResult, error) {
	cmd.AnimalClass = normalizeClass(cmd.AnimalClass)
	if err := s.validClass(ctx, cmd.TenantID, cmd.AnimalClass); err != nil {
		return domain.RegisterAuthoringResult{}, err
	}
	cmd.Document.RegisterVersion = strings.TrimSpace(cmd.Document.RegisterVersion)

	if problems := cmd.Document.Validate(); problems.Fatal() {
		return domain.RegisterAuthoringResult{}, &domain.ValidationError{
			Errors: fieldErrorsFrom(problems),
		}
	}
	return s.repo.SaveRegisterDraft(ctx, cmd)
}

func (s *RegisterConfigService) PublishDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return s.repo.PublishRegisterDraft(ctx, cmd)
}

func (s *RegisterConfigService) DiscardDraft(ctx context.Context, cmd domain.RegisterVersionCommand) (domain.RegisterAuthoringResult, error) {
	return s.repo.DiscardRegisterDraft(ctx, cmd)
}

// RegisterFor resolves the register one animal is diagnosed against.
//
// THE FALLBACK IS THE COMMITTED SEED, and it is a fallback rather than a default: a
// tenant that has never published a register is a tenant whose seed has not run, and
// diagnosing against nothing -- or against a blank table -- would be worse than
// diagnosing against the rulebook this repo ships. A published register always wins.
//
// It is also why the seed command exists as a command: the fallback keeps a farm
// working, it does not make the farm's register editable. Only a published one is.
func (s *RegisterConfigService) RegisterFor(ctx context.Context, tenantID, animalClass string) (diagnosis.AuthoredRegister, error) {
	animalClass = normalizeClass(animalClass)
	detail, err := s.repo.PublishedRegister(ctx, tenantID, animalClass)
	if err == nil {
		return detail.Document, nil
	}
	if !errors.Is(err, ports.ErrRegisterNotFound) {
		return diagnosis.AuthoredRegister{}, err
	}
	seeded, seedErr := diagnosis.SeedAuthored(animalClass, domain.SOPRefToDiseaseKey)
	if seedErr != nil {
		return diagnosis.AuthoredRegister{}, seedErr
	}
	return *seeded, nil
}

// PublishedRegisterDocument is the live document for one type, for the sheet export.
//
// It returns the PUBLISHED version rather than an open draft: a download is a starting point for
// an edit, and starting from somebody else's half-finished draft would hand one author another's
// unreviewed work without saying so.
func (s *RegisterConfigService) PublishedRegisterDocument(ctx context.Context, tenantID, animalClass string) (diagnosis.AuthoredRegister, error) {
	animalClass = normalizeClass(animalClass)
	if err := s.validClass(ctx, tenantID, animalClass); err != nil {
		return diagnosis.AuthoredRegister{}, err
	}
	detail, err := s.repo.PublishedRegister(ctx, tenantID, animalClass)
	if err != nil {
		return diagnosis.AuthoredRegister{}, err
	}
	return detail.Document, nil
}

func normalizeClass(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if c == "" {
		return diagnosis.ClassAdult
	}
	return c
}

// validClass accepts any type this farm actually diagnoses against.
//
// It used to compare against diagnosis.Classes -- the four compiled into the binary -- which
// would make a type created on the routing screen un-authorable: the farm could point a stage at
// it and then never write its rules. The authored list is read per tenant; the shipped four
// remain the answer when no type source is wired, so a caller that only edits registers behaves
// exactly as before.
//
// A RETIRED type is refused here even though routing already refuses it, because the two say
// different things: routing declines to send animals to it, while this declines to let an author
// spend an afternoon writing rules for a type the farm has put away.
func (s *RegisterConfigService) validClass(ctx context.Context, tenantID, c string) error {
	known := diagnosis.Classes
	if s.types != nil {
		view, err := s.types.DiagnosisRouting(ctx, tenantID)
		if err != nil {
			return err
		}
		known = known[:0:0]
		for _, t := range view.Types {
			if t.Status == "active" {
				known = append(known, t.TypeKey)
			}
		}
	}
	for _, k := range known {
		if k == c {
			return nil
		}
	}
	return &domain.ValidationError{Errors: []domain.FieldError{{
		Field:   "animal_class",
		Message: "Choose one of the animal types this farm diagnoses.",
	}}}
}

func fieldErrorsFrom(ps diagnosis.Problems) []domain.FieldError {
	out := make([]domain.FieldError, 0, len(ps))
	for _, p := range ps {
		if !p.Fatal {
			continue
		}
		out = append(out, domain.FieldError{Field: p.Path, Message: p.Message})
	}
	return out
}
