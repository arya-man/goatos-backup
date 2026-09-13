// Package app is the Animal purchases application service: the phone's load and animal writes,
// the CEO/CXO decision, and the reads both surfaces render.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
)

// Error is the transport-facing error shape.
type Error struct {
	Code       string
	Message    string
	Field      string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

var (
	ErrIdempotencyKeyRequired = errors.New("animal purchase: idempotency key required")
)

// minIdempotencyKeyLength mirrors the shared domain-event envelope schema (idempotency_key
// minLength 8): the decision's outbox event carries the request's key verbatim, and the relay
// validates the envelope AFTER the decision has committed. A shorter key therefore lands the
// decision and silently drops its push -- found on the edge-case pass with a two-character key.
// Refusing it here keeps the write and its event on the same footing.
const minIdempotencyKeyLength = 8

func requireIdempotencyKey(key string) error {
	if len(strings.TrimSpace(key)) < minIdempotencyKeyLength {
		return ErrIdempotencyKeyRequired
	}
	return nil
}

// Service orchestrates the writes and reads.
type Service struct {
	repo   ports.Repository
	proofs ports.ProofValidator
	media  ports.MediaResolver
	now    func() time.Time
}

// NewService wires the service. media may be nil (no playback URLs are composed then).
func NewService(repo ports.Repository, proofs ports.ProofValidator, media ports.MediaResolver) *Service {
	return &Service{repo: repo, proofs: proofs, media: media, now: time.Now}
}

// Options is the phone form's vocabulary, backend-owned: the SOP questionnaire itself plus the
// load form's vocabularies.
type Options struct {
	Species              []domain.Option
	Sexes                []domain.Option
	Conditions           []domain.Option
	Farms                []domain.Option
	BreedSuggestions     []string
	Questionnaire        []domain.Question
	QuestionnaireVersion int
}

func (s *Service) Options(ctx context.Context, tenantID string) (Options, error) {
	breeds, err := s.repo.BreedSuggestions(ctx, tenantID)
	if err != nil {
		return Options{}, err
	}
	return Options{
		Species:              domain.Species(),
		Sexes:                domain.Sexes(),
		Conditions:           domain.Conditions(),
		Farms:                domain.Farms(),
		BreedSuggestions:     breeds,
		Questionnaire:        domain.Questionnaire(),
		QuestionnaireVersion: domain.QuestionnaireVersion,
	}, nil
}

func (s *Service) CreateLoad(ctx context.Context, p ports.CreateLoadParams) (domain.Load, error) {
	if err := requireIdempotencyKey(p.IdempotencyKey); err != nil {
		return domain.Load{}, err
	}
	p.Write.Normalize()
	if err := p.Write.Validate(); err != nil {
		return domain.Load{}, err
	}
	return s.repo.CreateLoad(ctx, p)
}

func (s *Service) ListLoads(ctx context.Context, tenantID, cursor string, limit int) (ports.LoadPage, error) {
	c, err := domain.DecodeCursor(cursor, domain.CursorKindLoad)
	if err != nil {
		return ports.LoadPage{}, BadRequest("invalid_cursor", "That page could not be read. Reload the list.")
	}
	return s.repo.ListLoads(ctx, tenantID, c, domain.ClampLimit(limit))
}

func (s *Service) GetLoad(ctx context.Context, tenantID, loadID string) (domain.Load, error) {
	return s.repo.GetLoad(ctx, tenantID, loadID)
}

// AddCandidate records one animal. The video is validated BEFORE the row is written, so a
// candidate row can never exist without a finished in-app-camera video behind it.
func (s *Service) AddCandidate(ctx context.Context, p ports.AddCandidateParams) (domain.Candidate, error) {
	if err := requireIdempotencyKey(p.IdempotencyKey); err != nil {
		return domain.Candidate{}, err
	}
	p.Write.Normalize()
	if err := p.Write.Validate(); err != nil {
		return domain.Candidate{}, err
	}
	if s.proofs != nil {
		if err := s.proofs.ValidateCandidateMedia(ctx, p.TenantID, p.Write.AllMediaRefs()); err != nil {
			return domain.Candidate{}, err
		}
	}
	return s.repo.AddCandidate(ctx, p)
}

func (s *Service) ListCandidates(ctx context.Context, tenantID, loadID, cursor string, limit int) (ports.CandidatePage, error) {
	c, err := domain.DecodeCursor(cursor, domain.CursorKindCandidate)
	if err != nil {
		return ports.CandidatePage{}, BadRequest("invalid_cursor", "That page could not be read. Reload the list.")
	}
	return s.repo.ListCandidates(ctx, tenantID, loadID, c, domain.ClampLimit(limit))
}

func (s *Service) ListReview(ctx context.Context, tenantID, loadID, decision, cursor string, limit int) (ports.CandidatePage, error) {
	c, err := domain.DecodeCursor(cursor, domain.CursorKindReview)
	if err != nil {
		return ports.CandidatePage{}, BadRequest("invalid_cursor", "That page could not be read. Reload the list.")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	switch decision {
	case "", "all":
		decision = ""
	case domain.DecisionPending, domain.DecisionAccepted, domain.DecisionRejected:
	default:
		return ports.CandidatePage{}, BadRequest("invalid_decision_filter", "That filter is not one of the decision states.")
	}
	return s.repo.ListReview(ctx, tenantID, ports.ReviewQuery{
		LoadID: strings.TrimSpace(loadID), Decision: decision, Cursor: c, Limit: domain.ClampLimit(limit),
	})
}

func (s *Service) GetCandidate(ctx context.Context, tenantID, candidateID string) (domain.Candidate, error) {
	return s.repo.GetCandidate(ctx, tenantID, candidateID)
}

// Decide records the CEO's accept / reject. Version-fenced on the row the screen read.
func (s *Service) Decide(ctx context.Context, p ports.DecideParams) (domain.Candidate, error) {
	if err := requireIdempotencyKey(p.IdempotencyKey); err != nil {
		return domain.Candidate{}, err
	}
	p.Write.Normalize()
	if err := p.Write.Validate(); err != nil {
		return domain.Candidate{}, err
	}
	return s.repo.Decide(ctx, p)
}

// Media signs the candidates' videos for playback in ONE read. Best-effort: a signing failure
// leaves that video unavailable, never an error that hides the rows.
func (s *Service) Media(ctx context.Context, tenantID string, rows []domain.Candidate) map[string]ports.Media {
	if s.media == nil || len(rows) == 0 {
		return nil
	}
	refs := make([]string, 0, len(rows)*2)
	for _, c := range rows {
		if ref := strings.TrimSpace(c.VideoProofRef); ref != "" {
			refs = append(refs, ref)
		}
		for _, q := range domain.MediaSlots() {
			refs = append(refs, c.Media[q.Slot]...)
		}
	}
	out, err := s.media.ResolveMedia(ctx, tenantID, refs)
	if err != nil {
		return nil
	}
	return out
}

// Now is the service clock.
func (s *Service) Now() time.Time { return s.now() }

// HTTPError maps every error this package can return onto the transport shape. Unrecognised
// errors become a generic 500 rather than echoing err.Error() (table and constraint names do not
// belong on a screen).
func HTTPError(err error) *Error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	var v *domain.ValidationError
	if errors.As(err, &v) {
		return &Error{Code: "validation_failed", Message: v.Message, Field: v.Field, HTTPStatus: http.StatusUnprocessableEntity}
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	case errors.Is(err, ports.ErrLoadNotFound):
		return &Error{Code: "not_found_or_not_allowed", Message: "That load was not found.", HTTPStatus: http.StatusNotFound}
	case errors.Is(err, ports.ErrCandidateNotFound):
		return &Error{Code: "not_found_or_not_allowed", Message: "That animal was not found.", HTTPStatus: http.StatusNotFound}
	case errors.Is(err, ports.ErrVendorNotFound):
		return &Error{Code: "validation_failed", Message: "Pick a vendor from the register.", Field: "vendor_id", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrLoadRefTaken):
		return &Error{Code: "load_ref_taken", Message: "A load with this number already exists. Use a different number.", Field: "load_ref", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrLoadClosed):
		return &Error{Code: "load_closed", Message: "This load is closed and takes no more animals.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrAlreadyDecided):
		return &Error{Code: "already_decided", Message: "This animal already has a decision.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrRowVersionMismatch):
		return &Error{Code: "row_version_conflict", Message: "This animal changed since you opened it. Reload and decide again.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return &Error{Code: "idempotency_conflict", Message: "This request was already made with different details.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrMediaAlreadyUsed):
		return &Error{Code: "validation_failed", Message: "One of these photos or videos already belongs to another animal. Capture this animal again.", Field: "media", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrInvalidVideo):
		return &Error{Code: "validation_failed", Message: "A photo or video did not finish uploading. Record it again.", Field: "media", HTTPStatus: http.StatusUnprocessableEntity}
	default:
		return &Error{Code: "internal_error", Message: fmt.Sprintf("Could not complete that request."), HTTPStatus: http.StatusInternalServerError}
	}
}
