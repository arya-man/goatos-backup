// Package app is the Animal purchases application service: the phone's load and animal writes,
// the CEO/CXO decision, and the reads both surfaces render.
package app

import (
	"context"
	"errors"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
)

// Error is the transport-facing error shape.
type Error struct {
	// Cause is the underlying error for logs; never serialised.
	Cause      error `json:"-"`
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
	// catalogs serves the inspection SOP by version; nil = the seeded catalog only (tests).
	catalogs ports.CatalogSource
}

// NewService wires the service. media may be nil (no playback URLs are composed then).
func NewService(repo ports.Repository, proofs ports.ProofValidator, media ports.MediaResolver) *Service {
	return &Service{repo: repo, proofs: proofs, media: media, now: time.Now}
}

// WithCatalogSource makes the service read the inspection from the published SOP version.
func (s *Service) WithCatalogSource(src ports.CatalogSource) *Service {
	s.catalogs = src
	return s
}

// Catalog resolves one SOP version (0 = published). Without a source the seeded catalog is the
// only version there is.
func (s *Service) Catalog(ctx context.Context, tenantID string, version int) (domain.Catalog, error) {
	if s.catalogs == nil {
		seeded := domain.SeededCatalog()
		if version != 0 && version != seeded.Version {
			return domain.Catalog{}, ErrCatalogVersionUnknown
		}
		return seeded, nil
	}
	if version == 0 {
		return s.catalogs.PublishedCatalog(ctx, tenantID)
	}
	cat, err := s.catalogs.CatalogVersion(ctx, tenantID, version)
	if errors.Is(err, ports.ErrCatalogVersionUnknown) {
		return domain.Catalog{}, ErrCatalogVersionUnknown
	}
	return cat, err
}

// CatalogFor returns the catalog a recorded candidate was answered on, memoised per version
// for a page of rows (one read per distinct version, never one per row).
func (s *Service) CatalogFor(ctx context.Context, tenantID string, rows []domain.Candidate) map[int]domain.Catalog {
	out := map[int]domain.Catalog{}
	for _, c := range rows {
		v := c.QuestionnaireVersion
		if v == 0 {
			continue
		}
		if _, done := out[v]; done {
			continue
		}
		cat, err := s.Catalog(ctx, tenantID, v)
		if err != nil {
			// exception:exempt a version that cannot be read renders the row without its question labels; the answers themselves are still on the row.
			continue
		}
		out[v] = cat
	}
	return out
}

// ErrCatalogVersionUnknown: the phone answered on a version this tenant never published.
var ErrCatalogVersionUnknown = &Error{Code: "questionnaire_unknown", Message: "That inspection form is not one this farm published. Refresh and record the animal again.", HTTPStatus: http.StatusConflict}

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
	// LoadForm is the load's own questions of the same version (PROCUREMENT SOP).
	LoadForm []domain.Question
}

func (s *Service) Options(ctx context.Context, tenantID string) (Options, error) {
	breeds, err := s.repo.BreedSuggestions(ctx, tenantID)
	if err != nil {
		return Options{}, err
	}
	cat, err := s.Catalog(ctx, tenantID, 0)
	if err != nil {
		return Options{}, err
	}
	farms, err := s.repo.ListParkCodes(ctx, tenantID)
	if err != nil {
		return Options{}, err
	}
	cat = cat.WithFarms(farms)
	return Options{
		Species:              domain.Species(),
		Sexes:                domain.Sexes(),
		Conditions:           domain.Conditions(),
		Farms:                domain.Farms(farms),
		BreedSuggestions:     breeds,
		Questionnaire:        cat.Questions,
		QuestionnaireVersion: cat.Version,
		LoadForm:             cat.LoadQuestions,
	}, nil
}

func (s *Service) CreateLoad(ctx context.Context, p ports.CreateLoadParams) (domain.Load, error) {
	if err := requireIdempotencyKey(p.IdempotencyKey); err != nil {
		return domain.Load{}, err
	}
	cat, err := s.Catalog(ctx, p.TenantID, p.QuestionnaireVersion)
	if err != nil {
		return domain.Load{}, err
	}
	farms, err := s.repo.ListParkCodes(ctx, p.TenantID)
	if err != nil {
		return domain.Load{}, err
	}
	cat = cat.WithFarms(farms)
	p.Write.Catalog = cat
	p.Write.Farms = farms
	p.QuestionnaireVersion = cat.Version
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
	// The version the phone rendered validates this write, even if a newer one was published
	// since the form was opened; a version that never existed is refused.
	cat, err := s.Catalog(ctx, p.TenantID, p.QuestionnaireVersion)
	if err != nil {
		return domain.Candidate{}, err
	}
	p.Write.Catalog = cat
	p.QuestionnaireVersion = cat.Version
	p.Write.Normalize()
	if err := p.Write.Validate(); err != nil {
		return domain.Candidate{}, err
	}
	if s.proofs != nil {
		// Each capture must be a finished in-app upload of a kind its SOP slot accepts.
		allowed := map[string][]string{}
		for _, q := range cat.MediaSlots() {
			for _, ref := range p.Write.Media[q.Slot] {
				allowed[ref] = q.Accepts
			}
		}
		if err := s.proofs.ValidateCandidateMediaKinds(ctx, p.TenantID, allowed); err != nil {
			if errors.Is(err, ports.ErrMediaKindNotAccepted) {
				return domain.Candidate{}, &Error{Code: "media_kind_not_accepted", Message: "A capture is not the kind this step asks for (photo or video). Record it again.", Field: "media", HTTPStatus: http.StatusUnprocessableEntity}
			}
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

// ReviewFilter is the web review's filter set: one load or every load, a decision chip, and a
// recorded-on window of IST business dates ("2026-09-14"), both ends optional and inclusive.
type ReviewFilter struct {
	LoadID       string
	Decision     string
	RecordedFrom string
	RecordedTo   string
}

func (s *Service) ListReview(ctx context.Context, tenantID string, f ReviewFilter, cursor string, limit int) (ports.CandidatePage, error) {
	loadID, decision := f.LoadID, f.Decision
	from, to, err := reviewWindow(f.RecordedFrom, f.RecordedTo)
	if err != nil {
		return ports.CandidatePage{}, err
	}
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
		LoadID: strings.TrimSpace(loadID), Decision: decision, From: from, To: to, Cursor: c, Limit: domain.ClampLimit(limit),
	})
}

// reviewWindow turns inclusive IST business dates into [start of from, start of the day after to).
// A window whose end is before its start is a form mistake, refused rather than silently empty.
func reviewWindow(fromDate, toDate string) (from, to time.Time, err error) {
	loc := biztime.DefaultLocation()
	parse := func(raw string) (time.Time, bool, error) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return time.Time{}, false, nil
		}
		t, err := time.ParseInLocation("2006-01-02", raw, loc)
		if err != nil {
			return time.Time{}, false, BadRequest("invalid_date_filter", "Enter the date as YYYY-MM-DD.")
		}
		return t, true, nil
	}
	f, hasFrom, err := parse(fromDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	t, hasTo, err := parse(toDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if hasTo {
		t = t.AddDate(0, 0, 1)
	}
	if hasFrom && hasTo && !f.Before(t) {
		return time.Time{}, time.Time{}, BadRequest("invalid_date_filter", "The end date is before the start date.")
	}
	return f, t, nil
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

// Media resolves proof metadata and stable backend download routes in ONE read. Best-effort: a
// lookup failure leaves that media unavailable, never an error that hides the rows.
func (s *Service) Media(ctx context.Context, tenantID string, rows []domain.Candidate) map[string]ports.Media {
	if s.media == nil || len(rows) == 0 {
		return nil
	}
	refs := make([]string, 0, len(rows)*2)
	for _, c := range rows {
		if ref := strings.TrimSpace(c.VideoProofRef); ref != "" {
			refs = append(refs, ref)
		}
		// Every capture the row holds, whatever slot its SOP version named.
		for _, slotRefs := range c.Media {
			refs = append(refs, slotRefs...)
		}
	}
	out, err := s.media.ResolveMedia(ctx, tenantID, refs)
	if err != nil {
		// exception:exempt media metadata is best-effort on read; callers render rows without playable media.
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
		// The cause travels with the envelope so the transport can log it (never shown to a screen).
		return &Error{Code: "internal_error", Message: "Could not complete that request.", HTTPStatus: http.StatusInternalServerError, Cause: err}
	}
}
