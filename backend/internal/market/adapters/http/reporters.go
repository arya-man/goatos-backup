package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/market/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// MARKET SURVEY REPORTERS ON THE SALES SOP PAGE (maintainer instruction 2026-09-19: "what
// questions, whom they are going to -- everything on that page should be configurable"). WHO
// makes the morning calls is the `market_survey` phone module held per person; it used to be
// reachable only through /people. These two routes list the holders and toggle one person, and
// the toggle goes through the SAME person-access service /people uses (validation, audit, fence),
// never a second write path onto person_module_access.

// ReporterSource lists and toggles the people holding the market survey phone module.
type ReporterSource interface {
	ListMarketReporters(ctx context.Context, tenantID string) ([]MarketReporter, error)
	SetMarketReporter(ctx context.Context, tenantID, actorID, personID string, enabled bool) error
}

// MarketReporter is one person as the reporters list shows them.
type MarketReporter struct {
	PersonID    string `json:"person_id"`
	DisplayName string `json:"display_name"`
	Title       string `json:"title"`
	ParkLabel   string `json:"park_label"`
	// Reporter is whether the person holds the market survey phone module today.
	Reporter bool `json:"reporter"`
}

type reportersPayload struct {
	People []MarketReporter `json:"people"`
}

type reporterTogglePayload struct {
	Enabled bool `json:"enabled"`
}

// WithReporterSource wires the person-access seam.
func (h *Handler) WithReporterSource(src ReporterSource) *Handler {
	h.reporters = src
	return h
}

// ListReporters serves GET /market/reporters: every active person with a login, flagged by
// whether they hold the market survey module, so the page shows who reports AND who could.
func (h *Handler) ListReporters(w http.ResponseWriter, r *http.Request) {
	if h.reporters == nil {
		h.writeErr(w, r, &app.Error{Code: "reporters_unavailable", Message: "Reporters are not available.", HTTPStatus: http.StatusInternalServerError})
		return
	}
	people, err := h.reporters.ListMarketReporters(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	if people == nil {
		people = []MarketReporter{}
	}
	httpresponse.WriteJSON(w, http.StatusOK, reportersPayload{People: people})
}

// SetReporter serves PUT /market/reporters/{person_id}: gives or takes the market survey phone
// module through the person-access service.
func (h *Handler) SetReporter(w http.ResponseWriter, r *http.Request) {
	if h.reporters == nil {
		h.writeErr(w, r, &app.Error{Code: "reporters_unavailable", Message: "Reporters are not available.", HTTPStatus: http.StatusInternalServerError})
		return
	}
	personID := strings.TrimSpace(r.PathValue("person_id"))
	if personID == "" {
		h.writeErr(w, r, app.BadRequest("invalid_person", "Pick a person."))
		return
	}
	var body reporterTogglePayload
	if !h.decode(w, r, &body) {
		return
	}
	if err := h.reporters.SetMarketReporter(r.Context(), tenantID(r), actorID(r), personID, body.Enabled); err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	people, err := h.reporters.ListMarketReporters(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, toAppError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, reportersPayload{People: people})
}
