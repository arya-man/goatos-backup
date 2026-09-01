package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

var (
	// ErrNotProposable is returned when a confirmation names a diagnosis the
	// engine did not propose. Diagnosing off the register is the Director's
	// power, but it is not this endpoint -- allowing it here would open a course
	// for a disease no evidence supports.
	ErrNotProposable = errors.New("health: diagnosis was not proposed by this run")
	// ErrAlreadyDecided is returned when a run has already been confirmed or
	// superseded and a different decision arrives.
	ErrAlreadyDecided = errors.New("health: diagnosis run has already been decided")
	// ErrNotDiagnosable is returned when the form was rejected or the animal is
	// out of the register's scope. Neither can be confirmed.
	ErrNotDiagnosable = errors.New("health: this observation produced no confirmable diagnosis")
)

// DiagnosisService runs the observation form through the engine and mediates the
// Director's confirmation.
//
// It holds the registers rather than loading them per request: the rule tables are
// immutable for the life of the process, and re-parsing one on every observation
// would make an already-hot path do avoidable work.
//
// ONE REGISTER PER CLASS, chosen from the animal. This used to be a single register,
// which meant a kid resolved to `kid_milk` was still diagnosed off the ADULT table --
// the proposal even said `scope: kid_milk, register_version: adult-1`, so the wrong
// answer was labelled with the right cohort. That is the exact failure the class split
// exists to prevent, and it is invisible unless you read the version.
type DiagnosisService struct {
	repo      ports.DiagnosisRepository
	registers map[string]*diagnosis.Register
}

// NewDiagnosisService wires the service to every class register. It fails closed: a
// missing register would silently diagnose nothing, which reads to an operator as a
// healthy animal.
//
// All four are resolved up front rather than on first use, so a malformed rule table is
// a startup failure the deploy surfaces, never a 500 the first manager to check a
// weaning kid discovers in a shed.
func NewDiagnosisService(repo ports.DiagnosisRepository, _ *diagnosis.Register) (*DiagnosisService, error) {
	if repo == nil {
		return nil, errors.New("health: diagnosis repository is required")
	}
	registers := make(map[string]*diagnosis.Register, len(diagnosis.Classes))
	for _, class := range diagnosis.Classes {
		reg, err := diagnosis.RegisterFor(class)
		if err != nil {
			return nil, fmt.Errorf("health: diagnosis register for %s: %w", class, err)
		}
		registers[class] = reg
	}
	return &DiagnosisService{repo: repo, registers: registers}, nil
}

// SubmitObservation evaluates one form and stores the proposal.
//
// The animal's own facts (species, sex, status, age band, lifecycle) are read
// from GoatOS inside the repository transaction, not taken from the caller. The
// engine's answer depends on them, so a client able to assert them could steer
// the diagnosis.
func (s *DiagnosisService) SubmitObservation(ctx context.Context, in domain.SubmitObservationInput) (domain.SubmitObservationResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.GoatID = strings.TrimSpace(in.GoatID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)

	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.GoatID) || in.IdempotencyKey == "" {
		return domain.SubmitObservationResult{}, ErrInvalidInput
	}
	if in.BusinessDate.IsZero() {
		// The business day is India's, never UTC's, and the grain is the DAY --
		// BusinessDayStart anchors at 00:00 IST rather than at the instant the
		// form arrived. A form filled at 01:00 IST belongs to that IST day.
		in.BusinessDate = biztime.BusinessDayStart(time.Now())
	}

	// The engine is pure, but it needs the animal. The repository resolves the
	// animal, runs the closure below inside its transaction, and persists the
	// result atomically with the audit and outbox rows.
	return s.repo.SubmitObservation(ctx, in, s.evaluate)
}

// evaluate is the pure step the repository calls once it has resolved the
// animal. Keeping it here rather than in the adapter means the engine is invoked
// in exactly one place, and the adapter cannot quietly diagnose differently.
// The register is chosen from the animal's own class, never fixed. ResolveAnimal has
// already refused anything it could not class, so an unknown class here means the two
// have drifted apart -- and the safe answer is to diagnose NOTHING rather than to fall
// back to adult, which would hand a kid the adult table under a kid label.
func (s *DiagnosisService) evaluate(animal diagnosis.Animal, f diagnosis.Findings, dctx diagnosis.Context) (diagnosis.Proposal, []domain.ConfirmableProblem) {
	reg, ok := s.registers[animal.Class]
	if !ok {
		return diagnosis.Proposal{
			Valid:        false,
			RejectReason: fmt.Sprintf("no rule table for animal class %q", animal.Class),
			Scope:        diagnosis.ScopeOutOfScope,
		}, nil
	}
	proposal := reg.Evaluate(animal, f, dctx)
	return proposal, s.confirmableFrom(proposal)
}

// confirmableFrom turns the proposal's problems into the Director's decision
// list, resolving each to the treatment card it would open and reporting
// honestly when that card does not exist.
//
// It does NOT filter unavailable ones out. A Director deciding on a probable
// tetanus needs to see it even when the tetanus card is unauthored -- hiding it
// would make a missing card look like a missing diagnosis.
// Every in-scope class produces a decision list, not just adults. Gating this on
// ScopeAdult meant a kid could be diagnosed and then present the Director with an empty
// list -- a proposal nobody could act on, which reads as "nothing found".
func (s *DiagnosisService) confirmableFrom(p diagnosis.Proposal) []domain.ConfirmableProblem {
	if !p.Valid || p.Scope == diagnosis.ScopeOutOfScope {
		return nil
	}
	out := make([]domain.ConfirmableProblem, 0, len(p.Problems))
	for _, id := range p.Problems {
		sopRef := p.SOP[id]
		out = append(out, domain.ConfirmableProblem{
			ID:         id,
			Tier:       string(p.Tiers[id]),
			SOPRef:     sopRef,
			ExitType:   p.CourseType[id],
			DiseaseKey: domain.SOPRefToDiseaseKey(sopRef),
		})
	}
	return out
}

// ConfirmDiagnosis records the Director's decision and opens a course for each
// confirmed problem.
//
// Everything is one transaction: the run flips to confirmed, the cases open, the
// sessions are snapshotted, and the audit and outbox rows are written together.
// A partial confirmation -- run marked confirmed while a case failed to open --
// would leave an animal recorded as diagnosed with no treatment scheduled.
func (s *DiagnosisService) ConfirmDiagnosis(ctx context.Context, in domain.ConfirmDiagnosisInput) (domain.ConfirmDiagnosisResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.DiagnosisRunID = strings.TrimSpace(in.DiagnosisRunID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)

	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.DiagnosisRunID) || in.IdempotencyKey == "" {
		return domain.ConfirmDiagnosisResult{}, ErrInvalidInput
	}

	cleaned := make([]string, 0, len(in.ConfirmedProblems))
	seen := map[string]bool{}
	for _, id := range in.ConfirmedProblems {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	in.ConfirmedProblems = cleaned

	return s.repo.ConfirmDiagnosis(ctx, in)
}

// GetDiagnosisRun reads one run back, for the Director's queue and for the
// animal's diagnosis history.
func (s *DiagnosisService) GetDiagnosisRun(ctx context.Context, tenantID, runID string) (domain.DiagnosisRun, error) {
	tenantID, runID = strings.TrimSpace(tenantID), strings.TrimSpace(runID)
	if !validUUID(tenantID) || !validUUID(runID) {
		return domain.DiagnosisRun{}, ErrInvalidInput
	}
	return s.repo.GetDiagnosisRun(ctx, tenantID, runID)
}

// RegisterVersion reports the rule table for ONE animal class. There is no
// service-wide answer any more: each class pins its own version (adult-1,
// kid-milk-7, kid-weaning-1, kid-fattening-1), and every stored run carries the
// one that actually produced it, so an operator screen can show which version
// produced an old proposal. An unknown class returns "" rather than the adult
// version, because naming the wrong table is worse than naming none.
func (s *DiagnosisService) RegisterVersion(class string) string {
	if reg, ok := s.registers[class]; ok {
		return reg.Version
	}
	return ""
}

// Queue paging bounds. Twenty is one phone viewport with a little headroom; the
// hard ceiling stops a caller asking for a page that is no longer a page.
const (
	diagnosisQueueDefaultLimit = 20
	diagnosisQueueMaxLimit     = 50
)

// ListDiagnosisRuns serves the Director's queue.
//
// The default status is `proposed` -- work still awaiting a decision, which is
// what a queue is for. An explicitly requested status is honoured so the same
// endpoint can show what was already decided, and an explicit `all` clears the
// filter rather than being rejected as an unknown status.
func (s *DiagnosisService) ListDiagnosisRuns(
	ctx context.Context, f domain.DiagnosisQueueFilter,
) (domain.DiagnosisQueuePage, error) {
	f.TenantID = strings.TrimSpace(f.TenantID)
	if !validUUID(f.TenantID) {
		return domain.DiagnosisQueuePage{}, ErrInvalidInput
	}
	f.GoatID = strings.TrimSpace(f.GoatID)
	if f.GoatID != "" && !validUUID(f.GoatID) {
		return domain.DiagnosisQueuePage{}, ErrInvalidInput
	}

	switch status := strings.TrimSpace(strings.ToLower(f.Status)); status {
	case "":
		f.Status = domain.DiagnosisStatusProposed
	case "all":
		f.Status = ""
	case domain.DiagnosisStatusProposed, domain.DiagnosisStatusConfirmed, domain.DiagnosisStatusDeclined, domain.DiagnosisStatusSuperseded:
		f.Status = status
	default:
		// Rejected rather than silently ignored: a typo that quietly returns the
		// whole table would read as working while showing the wrong queue.
		return domain.DiagnosisQueuePage{}, ErrInvalidInput
	}

	if f.Limit <= 0 {
		f.Limit = diagnosisQueueDefaultLimit
	}
	if f.Limit > diagnosisQueueMaxLimit {
		f.Limit = diagnosisQueueMaxLimit
	}
	return s.repo.ListDiagnosisRuns(ctx, f)
}
