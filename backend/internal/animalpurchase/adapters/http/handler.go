// Package http serves Animal purchases: the phone's loads / animals reads and writes under
// /app/procurement/animal-purchases, and the CEO/CXO review list + decision on admin-web.
package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/app"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Handler serves the routes.
type Handler struct {
	service *app.Service
	log     *slog.Logger
}

func NewHandler(service *app.Service, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{service: service, log: log}
}

// Register mounts the routes. Patterns must stay byte-identical to permissions/routes.go.
func Register(mux *http.ServeMux, h *Handler) {
	mux.HandleFunc("GET /app/procurement/animal-purchases/options", h.Options)
	mux.HandleFunc("GET /app/procurement/animal-purchases/loads", h.ListLoads)
	mux.HandleFunc("POST /app/procurement/animal-purchases/loads", h.CreateLoad)
	mux.HandleFunc("GET /app/procurement/animal-purchases/loads/{load_id}", h.GetLoad)
	mux.HandleFunc("GET /app/procurement/animal-purchases/loads/{load_id}/animals", h.ListAnimals)
	mux.HandleFunc("POST /app/procurement/animal-purchases/loads/{load_id}/animals", h.AddAnimal)
	mux.HandleFunc("GET /procurement/animal-purchases/review", h.ListReview)
	mux.HandleFunc("POST /procurement/animal-purchases/animals/{candidate_id}/decision", h.Decide)
}

const maxRequestBytes = 64 * 1024

func (h *Handler) Options(w http.ResponseWriter, r *http.Request) {
	opts, err := h.service.Options(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, optionsPayload{
		Species: opts.Species, Sexes: opts.Sexes, Conditions: opts.Conditions, Farms: opts.Farms,
		BreedSuggestions: opts.BreedSuggestions, Copy: formCopy(),
	})
}

func (h *Handler) ListLoads(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListLoads(r.Context(), tenantID(r), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	out := loadPagePayload{Loads: make([]loadPayload, 0, len(page.Loads)), NextCursor: page.NextCursor, CanRecord: callerCanRecord(r)}
	for _, l := range page.Loads {
		out.Loads = append(out.Loads, toLoadPayload(l))
	}
	httpresponse.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateLoad(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var body createLoadBody
	if !h.decode(w, r, &body) {
		return
	}
	load, err := h.service.CreateLoad(r.Context(), ports.CreateLoadParams{
		TenantID: tenantID(r),
		Write: domain.LoadWrite{LoadRef: body.LoadRef, VendorID: body.VendorID, FarmLabel: body.Farm,
			ExpectedCount: body.ExpectedCount, Notes: body.Notes},
		ActorID: httpmiddleware.ActorIDFromContext(r.Context()), IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toLoadPayload(load))
}

func (h *Handler) GetLoad(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenant := tenantID(r)
	load, err := h.service.GetLoad(ctx, tenant, r.PathValue("load_id"))
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	page, err := h.service.ListCandidates(ctx, tenant, load.LoadID, "", domain.DefaultPageSize)
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, loadDetailPayload{
		Load: toLoadPayload(load), Animals: h.candidates(r, page.Candidates), NextCursor: page.NextCursor, CanRecord: callerCanRecord(r),
	})
}

func (h *Handler) ListAnimals(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListCandidates(r.Context(), tenantID(r), r.PathValue("load_id"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, candidatePagePayload{
		Animals: h.candidates(r, page.Candidates), NextCursor: page.NextCursor, Counts: toCounts(page.Counts),
	})
}

func (h *Handler) AddAnimal(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var body addAnimalBody
	if !h.decode(w, r, &body) {
		return
	}
	c, err := h.service.AddCandidate(r.Context(), ports.AddCandidateParams{
		TenantID: tenantID(r), LoadID: r.PathValue("load_id"),
		Write: domain.CandidateWrite{Species: body.Species, Sex: body.Sex, Breed: body.Breed, AgeMonths: body.AgeMonths,
			WeightKg: body.WeightKg, Condition: body.Condition, TempTag: body.TempTag, Notes: body.Notes, VideoProofRef: body.VideoProofRef},
		ActorID: httpmiddleware.ActorIDFromContext(r.Context()), IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, h.candidates(r, []domain.Candidate{c})[0])
}

// ListReview serves the CEO/CXO queue. `decision` picks a chip (pending by default); `load_id`
// narrows to one load.
func (h *Handler) ListReview(w http.ResponseWriter, r *http.Request) {
	limit, ok := h.limit(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	decision := strings.TrimSpace(q.Get("decision"))
	if decision == "" {
		decision = domain.DecisionPending
	}
	page, err := h.service.ListReview(r.Context(), tenantID(r), q.Get("load_id"), decision, q.Get("cursor"), limit)
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	// The chips count the SAME filter set (the load, or everything) across all decisions, so the
	// numbers on the chips do not move when a chip is picked.
	all, err := h.service.ListReview(r.Context(), tenantID(r), q.Get("load_id"), "all", "", 1)
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, reviewPagePayload{
		Animals: h.candidates(r, page.Candidates), NextCursor: page.NextCursor, Counts: toCounts(all.Counts),
		Filters: []filterPayload{
			{Key: domain.DecisionPending, Label: "Awaiting decision", Count: all.Counts.Pending, Selected: decision == domain.DecisionPending},
			{Key: domain.DecisionAccepted, Label: "Accepted", Count: all.Counts.Accepted, Selected: decision == domain.DecisionAccepted},
			{Key: domain.DecisionRejected, Label: "Rejected", Count: all.Counts.Rejected, Selected: decision == domain.DecisionRejected},
			{Key: "all", Label: "All", Count: all.Counts.Total, Selected: decision == "all"},
		},
	})
}

func (h *Handler) Decide(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var body decisionBody
	if !h.decode(w, r, &body) {
		return
	}
	c, err := h.service.Decide(r.Context(), ports.DecideParams{
		TenantID: tenantID(r), CandidateID: r.PathValue("candidate_id"),
		Write:   domain.DecisionWrite{Decision: body.Decision, Note: body.Note, RowVersion: body.RowVersion},
		ActorID: httpmiddleware.ActorIDFromContext(r.Context()), IdempotencyKey: key,
	})
	if err != nil {
		h.writeErr(w, r, app.HTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, h.candidates(r, []domain.Candidate{c})[0])
}

// candidates composes the payloads, signing every video for playback in one read.
func (h *Handler) candidates(r *http.Request, rows []domain.Candidate) []candidatePayload {
	out := make([]candidatePayload, 0, len(rows))
	media := h.service.Media(r.Context(), tenantID(r), rows)
	for _, c := range rows {
		m := media[c.VideoProofRef]
		out = append(out, toCandidatePayload(c, m.URL, m.MimeType))
	}
	return out
}

func (h *Handler) limit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
		return 0, false
	}
	return n, true
}

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

func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	envelope := map[string]any{"error": appErr.Code, "message": appErr.Message}
	if appErr.Field != "" {
		envelope["field"] = appErr.Field
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, envelope, errors.New(appErr.Code))
}

// callerCanRecord reports whether the principal holds the write permission -- derived from the
// SAME grants the route table authorizes against, never from a role string the client sends.
func callerCanRecord(r *http.Request) bool {
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.AnimalPurchaseWrite) {
			return true
		}
	}
	return false
}

func tenantID(r *http.Request) string {
	return strings.TrimSpace(httpmiddleware.TenantIDFromContext(r.Context()))
}

// loadSummary is the backend-owned line under a load's title.
func loadSummary(l domain.Load) string {
	c := l.Counts
	if c.Total == 0 {
		if l.ExpectedCount > 0 {
			return fmt.Sprintf("No animals recorded yet · about %d expected", l.ExpectedCount)
		}
		return "No animals recorded yet"
	}
	noun := "animals"
	if c.Total == 1 {
		noun = "animal"
	}
	parts := []string{fmt.Sprintf("%d %s", c.Total, noun)}
	if c.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d awaiting decision", c.Pending))
	}
	if c.Accepted > 0 {
		parts = append(parts, fmt.Sprintf("%d accepted", c.Accepted))
	}
	if c.Rejected > 0 {
		parts = append(parts, fmt.Sprintf("%d rejected", c.Rejected))
	}
	return strings.Join(parts, " · ")
}

// formCopy is the phone form's copy, rendered verbatim (backend-owns-labels rule).
func formCopy() map[string]string {
	return map[string]string{
		"loads.title":             "Animal purchases",
		"loads.empty":             "No purchase loads yet. Add a load to start recording animals.",
		"loads.add":               "Add load",
		"load.form.title":         "Add a purchase load",
		"load.field.load_ref":     "Load number",
		"load.field.vendor":       "Vendor",
		"load.field.farm":         "For which farm",
		"load.field.expected":     "About how many animals",
		"load.field.notes":        "Note",
		"load.save":               "Save load",
		"load.animals.title":      "Animals",
		"load.animals.empty":      "No animals recorded in this load yet.",
		"load.animals.add":        "Add animal",
		"animal.form.title":       "Add an animal",
		"animal.field.species":    "Goat or sheep",
		"animal.field.sex":        "Male or female",
		"animal.field.breed":      "Breed",
		"animal.field.age":        "Age (months, roughly)",
		"animal.field.weight":     "Weight (kg, roughly)",
		"animal.field.condition":  "How does it look",
		"animal.field.temp_tag":   "Temporary tag (optional)",
		"animal.field.notes":      "Note",
		"animal.field.video":      "Video of the animal",
		"animal.video.record":     "Record video",
		"animal.video.retake":     "Record again",
		"animal.video.hint":       "Walk around the animal so the whole body is seen.",
		"animal.save":             "Save animal",
		"animal.saving":           "Saving...",
		"animal.decision.pending": "Awaiting decision",
		"animal.decided_by":       "Decided by",
		"required.hint":           "Fields marked * are required.",
	}
}
