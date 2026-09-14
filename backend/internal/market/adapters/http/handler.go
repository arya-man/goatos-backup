// Package http serves the market survey's routes: config and analytics for admin-web under
// Sales, and the day's cards plus the entry write for the phone.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/market/app"
	"github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Handler serves the market routes.
type Handler struct {
	service *app.Service
	log     *slog.Logger
}

// NewHandler constructs the transport.
func NewHandler(service *app.Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register mounts the routes. Patterns must stay byte-identical to permissions/routes.go.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /market/config", h.GetConfig)
	mux.HandleFunc("POST /market/cities", h.CreateCity)
	mux.HandleFunc("PUT /market/cities/{city_id}", h.UpdateCity)
	mux.HandleFunc("POST /market/questions", h.CreateQuestion)
	mux.HandleFunc("PUT /market/questions/{question_id}", h.UpdateQuestion)
	mux.HandleFunc("PUT /market/config/call-time", h.SetCallTime)
	mux.HandleFunc("GET /market/analytics", h.GetAnalytics)
	mux.HandleFunc("GET /app/market/survey", h.GetSurveyDay)
	mux.HandleFunc("POST /app/market/survey/{city_id}", h.RecordSurveyCity)
}

const maxRequestBytes = 64 * 1024

// --- wire shapes -------------------------------------------------------------------------------

type cityPayload struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

type questionPayload struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	UnitLabel string `json:"unit_label"`
	SortOrder int    `json:"sort_order"`
	Status    string `json:"status"`
}

type configPayload struct {
	Cities    []cityPayload     `json:"cities"`
	Questions []questionPayload `json:"questions"`
	// CallTime is the "HH:MM" IST time the day's calls open (the default when none is stored).
	CallTime string `json:"call_time"`
}

type callTimeWritePayload struct {
	CallTime string `json:"call_time"`
}

type callTimePayload struct {
	CallTime string `json:"call_time"`
}

type cityWritePayload struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type questionWritePayload struct {
	Label     string `json:"label"`
	UnitLabel string `json:"unit_label"`
	Status    string `json:"status,omitempty"`
}

type answerPayload struct {
	QuestionID string  `json:"question_id"`
	Price      float64 `json:"price"`
}

type dayEntryPayload struct {
	BusinessDate string          `json:"business_date,omitempty"`
	Answers      []answerPayload `json:"answers"`
}

type cardQuestionPayload struct {
	QuestionID string   `json:"question_id"`
	Label      string   `json:"label"`
	UnitLabel  string   `json:"unit_label"`
	Price      *float64 `json:"price"`
}

type dayCardPayload struct {
	CityID    string                `json:"city_id"`
	CityName  string                `json:"city_name"`
	Status    string                `json:"status"`
	Answered  int                   `json:"answered"`
	Total     int                   `json:"total"`
	Questions []cardQuestionPayload `json:"questions"`
}

type dayViewPayload struct {
	BusinessDate string           `json:"business_date"`
	Cards        []dayCardPayload `json:"cards"`
	Pending      int              `json:"pending"`
	Done         int              `json:"done"`
	// CanRecord is whether THIS caller may save answers (sales.market.entry), derived from the
	// same grants the write route authorizes against, so the phone never offers a form the
	// server would refuse.
	CanRecord bool `json:"can_record"`
	// Open is whether the day's calls have opened; OpensAt is today's configured call time
	// ("HH:MM" IST). Before it, Cards is empty and the phone says when they open.
	Open    bool   `json:"open"`
	OpensAt string `json:"opens_at"`
}

type seriesPointPayload struct {
	BusinessDate string  `json:"business_date"`
	Price        float64 `json:"price"`
}

type seriesPayload struct {
	CityID        string               `json:"city_id"`
	CityName      string               `json:"city_name"`
	QuestionID    string               `json:"question_id"`
	QuestionLabel string               `json:"question_label"`
	UnitLabel     string               `json:"unit_label"`
	Points        []seriesPointPayload `json:"points"`
}

type latestCellPayload struct {
	CityID        string   `json:"city_id"`
	CityName      string   `json:"city_name"`
	QuestionID    string   `json:"question_id"`
	QuestionLabel string   `json:"question_label"`
	UnitLabel     string   `json:"unit_label"`
	BusinessDate  string   `json:"business_date"`
	Price         float64  `json:"price"`
	PreviousPrice *float64 `json:"previous_price"`
}

type analyticsPayload struct {
	From   string              `json:"from"`
	To     string              `json:"to"`
	Days   int                 `json:"days"`
	Latest []latestCellPayload `json:"latest"`
	Series []seriesPayload     `json:"series"`
}

func toCity(c domain.City) cityPayload {
	return cityPayload{ID: c.ID, Name: c.Name, SortOrder: c.SortOrder, Status: c.Status}
}

func toQuestion(q domain.Question) questionPayload {
	return questionPayload{ID: q.ID, Label: q.Label, UnitLabel: q.UnitLabel, SortOrder: q.SortOrder, Status: q.Status}
}

func toConfig(cfg domain.Config) configPayload {
	out := configPayload{Cities: make([]cityPayload, 0, len(cfg.Cities)), Questions: make([]questionPayload, 0, len(cfg.Questions)), CallTime: cfg.EffectiveCallTime()}
	for _, c := range cfg.Cities {
		out.Cities = append(out.Cities, toCity(c))
	}
	for _, q := range cfg.Questions {
		out.Questions = append(out.Questions, toQuestion(q))
	}
	return out
}

func toCard(c domain.DayCard) dayCardPayload {
	out := dayCardPayload{CityID: c.City.ID, CityName: c.City.Name, Status: c.Status, Answered: c.Answered, Total: c.Total,
		Questions: make([]cardQuestionPayload, 0, len(c.Questions))}
	for _, q := range c.Questions {
		out.Questions = append(out.Questions, cardQuestionPayload{QuestionID: q.Question.ID, Label: q.Question.Label, UnitLabel: q.Question.UnitLabel, Price: q.Price})
	}
	return out
}

func toAnalytics(a domain.Analytics) analyticsPayload {
	out := analyticsPayload{From: a.From, To: a.To, Days: a.Days,
		Latest: make([]latestCellPayload, 0, len(a.Latest)), Series: make([]seriesPayload, 0, len(a.Series))}
	for _, c := range a.Latest {
		out.Latest = append(out.Latest, latestCellPayload{CityID: c.CityID, CityName: c.CityName, QuestionID: c.QuestionID,
			QuestionLabel: c.QuestionLabel, UnitLabel: c.UnitLabel, BusinessDate: c.BusinessDate, Price: c.Price, PreviousPrice: c.PreviousPrice})
	}
	for _, s := range a.Series {
		sp := seriesPayload{CityID: s.CityID, CityName: s.CityName, QuestionID: s.QuestionID, QuestionLabel: s.QuestionLabel,
			UnitLabel: s.UnitLabel, Points: make([]seriesPointPayload, 0, len(s.Points))}
		for _, p := range s.Points {
			sp.Points = append(sp.Points, seriesPointPayload{BusinessDate: p.BusinessDate, Price: p.Price})
		}
		out.Series = append(out.Series, sp)
	}
	return out
}

// --- handlers ----------------------------------------------------------------------------------

// GetConfig serves GET /market/config.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.service.GetConfig(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toConfig(cfg))
}

// CreateCity serves POST /market/cities.
func (h *Handler) CreateCity(w http.ResponseWriter, r *http.Request) {
	var body cityWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	city, err := h.service.CreateCity(r.Context(), tenantID(r), actorID(r), idempotencyKey(r),
		domain.CityWrite{Name: body.Name, Status: body.Status})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toCity(city))
}

// UpdateCity serves PUT /market/cities/{city_id}.
func (h *Handler) UpdateCity(w http.ResponseWriter, r *http.Request) {
	var body cityWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	city, err := h.service.UpdateCity(r.Context(), tenantID(r), actorID(r), r.PathValue("city_id"),
		domain.CityWrite{Name: body.Name, Status: body.Status})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toCity(city))
}

// CreateQuestion serves POST /market/questions.
func (h *Handler) CreateQuestion(w http.ResponseWriter, r *http.Request) {
	var body questionWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	q, err := h.service.CreateQuestion(r.Context(), tenantID(r), actorID(r), idempotencyKey(r),
		domain.QuestionWrite{Label: body.Label, UnitLabel: body.UnitLabel, Status: body.Status})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toQuestion(q))
}

// UpdateQuestion serves PUT /market/questions/{question_id}.
func (h *Handler) UpdateQuestion(w http.ResponseWriter, r *http.Request) {
	var body questionWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	q, err := h.service.UpdateQuestion(r.Context(), tenantID(r), actorID(r), r.PathValue("question_id"),
		domain.QuestionWrite{Label: body.Label, UnitLabel: body.UnitLabel, Status: body.Status})
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toQuestion(q))
}

// SetCallTime serves PUT /market/config/call-time.
func (h *Handler) SetCallTime(w http.ResponseWriter, r *http.Request) {
	var body callTimeWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	callTime, err := h.service.SetCallTime(r.Context(), tenantID(r), actorID(r), body.CallTime)
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, callTimePayload{CallTime: callTime})
}

// GetAnalytics serves GET /market/analytics?from=&to=.
func (h *Handler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a, err := h.service.GetAnalytics(r.Context(), tenantID(r), q.Get("from"), q.Get("to"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toAnalytics(a))
}

// GetSurveyDay serves GET /app/market/survey?date= -- the phone's cards for a business day.
func (h *Handler) GetSurveyDay(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.GetDay(r.Context(), tenantID(r), r.URL.Query().Get("date"))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toDayView(view, callerCanRecord(r)))
}

// RecordSurveyCity serves POST /app/market/survey/{city_id} -- one city's answers for a day.
func (h *Handler) RecordSurveyCity(w http.ResponseWriter, r *http.Request) {
	var body dayEntryPayload
	if !h.decode(w, r, &body) {
		return
	}
	write := domain.DayEntryWrite{CityID: r.PathValue("city_id"), BusinessDate: body.BusinessDate,
		Answers: make([]domain.PriceAnswer, 0, len(body.Answers))}
	for _, a := range body.Answers {
		write.Answers = append(write.Answers, domain.PriceAnswer{QuestionID: a.QuestionID, Price: a.Price})
	}
	card, err := h.service.RecordDay(r.Context(), tenantID(r), actorID(r), idempotencyKey(r), write)
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toCard(card))
}

func toDayView(v app.DayView, canRecord bool) dayViewPayload {
	out := dayViewPayload{BusinessDate: v.BusinessDate, Pending: v.Pending, Done: v.Done, CanRecord: canRecord, Open: v.Open, OpensAt: v.OpensAt,
		Cards: make([]dayCardPayload, 0, len(v.Cards))}
	for _, c := range v.Cards {
		out.Cards = append(out.Cards, toCard(c))
	}
	return out
}

// --- helpers -----------------------------------------------------------------------------------

func (h *Handler) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That form could not be read. Check the fields and try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That form could not be read. Check the fields and try again."))
		return false
	}
	return true
}

func toAppError(err error) *app.Error {
	var appErr *app.Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return app.HTTPError(err)
}

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	// Both keys: `error` is the shape the older module handlers emit and the phone reads;
	// `code` is what admin-web's envelope parser (and this package's own error logging) keys
	// on, so a 409 duplicate_name reaches the config screen as its own sentence.
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"code":    appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

func tenantID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
}

func actorID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.ActorIDFromContext(r.Context()))
}

func idempotencyKey(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
}

// callerCanRecord reports whether the principal holds sales.market.entry, from the SAME source
// the route table authorized against: the per-person permission set when THAT decided the
// request, else the grant roles -- never a role string the client sends.
func callerCanRecord(r *http.Request) bool {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(r.Context()); ok {
		for _, p := range perms {
			if p == permissions.MarketEntry {
				return true
			}
		}
		return false
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.MarketEntry) {
			return true
		}
	}
	return false
}
