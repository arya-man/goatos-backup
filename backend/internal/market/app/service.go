// Package app is the market survey's use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/market/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Window bounds for the analytics read. Ninety days is the default the page opens on; a year is
// the most one request may ask for, so the read stays bounded by config caps x days.
const (
	DefaultAnalyticsDays = 90
	MaxAnalyticsDays     = 400
	// MaxBackfillDays is how far back a day entry may be dated. A call the reporter forgot to
	// type in yesterday is still real; a price typed for last month is not a survey.
	MaxBackfillDays = 7
)

// Service is the use-case layer.
type Service struct {
	repo ports.Repository
	now  func() time.Time
}

// NewService wires the service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock pins the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Now is the service clock.
func (s *Service) Now() time.Time { return s.now() }

// GetConfig reads the authored survey.
func (s *Service) GetConfig(ctx context.Context, tenantID string) (domain.Config, error) {
	return s.repo.GetConfig(ctx, tenantID)
}

// CreateCity adds a market to phone.
func (s *Service) CreateCity(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.CityWrite) (domain.City, error) {
	write = write.Normalize()
	if err := write.Validate(); err != nil {
		return domain.City{}, err
	}
	return s.repo.CreateCity(ctx, tenantID, actorID, idempotencyKey, write)
}

// UpdateCity renames, retires or reactivates a market.
func (s *Service) UpdateCity(ctx context.Context, tenantID, actorID, cityID string, write domain.CityWrite) (domain.City, error) {
	write = write.Normalize()
	if err := write.Validate(); err != nil {
		return domain.City{}, err
	}
	return s.repo.UpdateCity(ctx, tenantID, actorID, strings.TrimSpace(cityID), write)
}

// CreateQuestion adds something to ask in every city.
func (s *Service) CreateQuestion(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.QuestionWrite) (domain.Question, error) {
	write = write.Normalize()
	if err := write.Validate(); err != nil {
		return domain.Question{}, err
	}
	return s.repo.CreateQuestion(ctx, tenantID, actorID, idempotencyKey, write)
}

// UpdateQuestion relabels, re-units, retires or reactivates a question.
func (s *Service) UpdateQuestion(ctx context.Context, tenantID, actorID, questionID string, write domain.QuestionWrite) (domain.Question, error) {
	write = write.Normalize()
	if err := write.Validate(); err != nil {
		return domain.Question{}, err
	}
	return s.repo.UpdateQuestion(ctx, tenantID, actorID, strings.TrimSpace(questionID), write)
}

// DayView is the phone's screen for one business day.
type DayView struct {
	BusinessDate string
	Cards        []domain.DayCard
	// Pending and Done are whole-day counts, never page-local: the screen has one page.
	Pending int
	Done    int
}

// GetDay composes the cards for a business day: today when blank. A future date is refused --
// a price nobody has asked for yet cannot be recorded.
func (s *Service) GetDay(ctx context.Context, tenantID, businessDate string) (DayView, error) {
	date, err := s.resolveDate(businessDate)
	if err != nil {
		return DayView{}, err
	}
	cfg, err := s.repo.GetConfig(ctx, tenantID)
	if err != nil {
		return DayView{}, err
	}
	entries, err := s.repo.ListDayEntries(ctx, tenantID, date)
	if err != nil {
		return DayView{}, err
	}
	view := DayView{BusinessDate: date, Cards: domain.BuildDayCards(cfg, entries)}
	for _, c := range view.Cards {
		if c.Status == domain.CardDone {
			view.Done++
		} else {
			view.Pending++
		}
	}
	return view, nil
}

// RecordDay writes a city's answers for a day and returns that city's card as it now stands.
func (s *Service) RecordDay(ctx context.Context, tenantID, actorID, idempotencyKey string, write domain.DayEntryWrite) (domain.DayCard, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return domain.DayCard{}, ports.ErrIdempotencyKeyRequired
	}
	date, err := s.resolveDate(write.BusinessDate)
	if err != nil {
		return domain.DayCard{}, err
	}
	write.BusinessDate = date
	write.CityID = strings.TrimSpace(write.CityID)
	for i := range write.Answers {
		write.Answers[i].QuestionID = strings.TrimSpace(write.Answers[i].QuestionID)
	}
	if err := write.Validate(); err != nil {
		return domain.DayCard{}, err
	}
	if _, err := s.repo.RecordDayEntry(ctx, ports.RecordDayEntryParams{
		TenantID: tenantID, ActorID: actorID, IdempotencyKey: idempotencyKey, Write: write,
	}); err != nil {
		return domain.DayCard{}, err
	}
	// The card is re-read through the same composition the day view uses, so the phone renders
	// after a save exactly what it will render on its next refresh.
	view, err := s.GetDay(ctx, tenantID, date)
	if err != nil {
		return domain.DayCard{}, err
	}
	for _, c := range view.Cards {
		if c.City.ID == write.CityID {
			return c, nil
		}
	}
	return domain.DayCard{}, ports.ErrNotFound
}

// GetAnalytics reads a window of entries as series and a latest table. Blank bounds mean the
// last DefaultAnalyticsDays days ending today.
func (s *Service) GetAnalytics(ctx context.Context, tenantID, from, to string) (domain.Analytics, error) {
	today := biztime.BusinessDate(s.now())
	if strings.TrimSpace(to) == "" {
		to = today
	}
	toDate, err := time.Parse("2006-01-02", to)
	if err != nil {
		return domain.Analytics{}, domain.ErrFieldValidation{Field: "to", Reason: "must be a date"}
	}
	if strings.TrimSpace(from) == "" {
		from = toDate.AddDate(0, 0, -(DefaultAnalyticsDays - 1)).Format("2006-01-02")
	}
	fromDate, err := time.Parse("2006-01-02", from)
	if err != nil {
		return domain.Analytics{}, domain.ErrFieldValidation{Field: "from", Reason: "must be a date"}
	}
	if fromDate.After(toDate) {
		return domain.Analytics{}, domain.ErrFieldValidation{Field: "from", Reason: "must not be after to"}
	}
	if toDate.Sub(fromDate) > time.Duration(MaxAnalyticsDays)*24*time.Hour {
		return domain.Analytics{}, domain.ErrFieldValidation{Field: "from", Reason: fmt.Sprintf("window is limited to %d days", MaxAnalyticsDays)}
	}
	entries, err := s.repo.ListEntriesBetween(ctx, tenantID, from, to)
	if err != nil {
		return domain.Analytics{}, err
	}
	return domain.BuildAnalytics(from, to, entries), nil
}

// resolveDate defaults a blank date to today's IST business day and refuses a future one or one
// older than MaxBackfillDays.
func (s *Service) resolveDate(raw string) (string, error) {
	today := biztime.BusinessDate(s.now())
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return today, nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return "", domain.ErrFieldValidation{Field: "business_date", Reason: "must be a date"}
	}
	t, _ := time.Parse("2006-01-02", today)
	if d.After(t) {
		return "", domain.ErrFieldValidation{Field: "business_date", Reason: "cannot be in the future"}
	}
	if t.Sub(d) > time.Duration(MaxBackfillDays)*24*time.Hour {
		return "", domain.ErrFieldValidation{Field: "business_date", Reason: fmt.Sprintf("cannot be more than %d days ago", MaxBackfillDays)}
	}
	return raw, nil
}

// Error is the transport-facing error shape: a stable code plus a farm-worded message.
type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string { return e.Code }

// BadRequest builds a 400 with a stable code.
func BadRequest(code, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: http.StatusBadRequest}
}

// HTTPError maps a market error onto the transport shape. Every branch carries a message the
// reporter or the sales desk can act on; unknown errors fall through to a generic 500.
func HTTPError(err error) *Error {
	var fieldErr domain.ErrFieldValidation
	switch {
	case err == nil:
		return nil
	case errors.As(err, &fieldErr):
		return &Error{Code: "field_validation", Message: fieldMessage(fieldErr), HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrNotFound):
		return &Error{Code: "not_found", Message: "That market entry was not found.", HTTPStatus: http.StatusNotFound}
	case errors.Is(err, ports.ErrDuplicateName):
		return &Error{Code: "duplicate_name", Message: "That name is already in the list.", HTTPStatus: http.StatusConflict}
	case errors.Is(err, ports.ErrCityRetired):
		return &Error{Code: "city_retired", Message: "This city is no longer on the morning call list.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrTooMany):
		return &Error{Code: "too_many", Message: "The list is full. Retire one before adding another.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, domain.ErrNothingToRecord):
		return &Error{Code: "nothing_to_record", Message: "Enter at least one price before saving.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, domain.ErrUnknownQuestion):
		return &Error{Code: "unknown_question", Message: "One of the questions has changed. Refresh and try again.", HTTPStatus: http.StatusUnprocessableEntity}
	case errors.Is(err, ports.ErrIdempotencyKeyRequired):
		return BadRequest("missing_idempotency_key", "This could not be saved safely. Try again.")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return &Error{Code: "idempotency_conflict", Message: "This was already saved with different values. Refresh to see what was recorded.", HTTPStatus: http.StatusConflict}
	default:
		return &Error{Code: "internal_error", Message: "Something went wrong. Try again.", HTTPStatus: http.StatusInternalServerError}
	}
}

func fieldMessage(e domain.ErrFieldValidation) string {
	label := map[string]string{
		"name": "City name", "label": "Question", "unit_label": "Unit", "status": "Status",
		"price": "Price", "business_date": "Date", "city_id": "City", "question_id": "Question",
		"from": "From date", "to": "To date",
	}[e.Field]
	if label == "" {
		label = e.Field
	}
	switch e.Reason {
	case "required":
		return label + " is required."
	case "too long":
		return label + " is too long."
	default:
		return label + " " + e.Reason + "."
	}
}
