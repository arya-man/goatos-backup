package http

// Handlers for the pipeline and evidence writes. Same rules as the deal handlers: mandatory
// Idempotency-Key on every write, DisallowUnknownFields decode, and every error mapped through
// app.SalesHTTPError.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/sales/app"
)

// leadQuery reads the shared limit/offset pair off a pipeline list request.
func (h *SalesHandler) leadQuery(w http.ResponseWriter, r *http.Request) (app.LeadListQuery, bool) {
	q := r.URL.Query()
	out := app.LeadListQuery{}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return out, false
		}
		out.Limit = parsed
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_offset", "That page is not valid."))
			return out, false
		}
		out.Offset = parsed
	}
	return out, true
}

// requireIdemKey reads the mandatory Idempotency-Key off a pipeline write.
func (h *SalesHandler) requireIdemKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This record could not be saved safely. Try again."))
		return "", false
	}
	return key, true
}

// decodeInto reads and validates a JSON write body into dst (any payload type). Same fail-loud
// contract as the deal decode.
func (h *SalesHandler) decodeInto(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
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

// ListBuyerLeads serves GET /sales/buyer-leads.
func (h *SalesHandler) ListBuyerLeads(w http.ResponseWriter, r *http.Request) {
	q, ok := h.leadQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListBuyerLeads(r.Context(), tenantID(r), q)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	items := make([]buyerLeadPayload, 0, len(page.Leads))
	for _, l := range page.Leads {
		items = append(items, toBuyerLeadPayload(l))
	}
	httpresponse.WriteJSON(w, http.StatusOK, buyerLeadPagePayload{
		Leads: items, Total: page.Total, StatusOptions: page.StatusOptions,
	})
}

// CreateBuyerLead serves POST /sales/buyer-leads.
func (h *SalesHandler) CreateBuyerLead(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body buyerLeadWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	created, err := h.service.CreateBuyerLead(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toBuyerLeadPayload(created))
}

// SetBuyerLeadStatus serves POST /sales/buyer-leads/{lead_id}/status.
func (h *SalesHandler) SetBuyerLeadStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body leadStatusWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	updated, err := h.service.SetBuyerLeadStatus(r.Context(), tenantID(r), r.PathValue("lead_id"),
		leadStatusFromPayload(body), httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toBuyerLeadPayload(updated))
}

// ListFPOLeads serves GET /sales/fpo-leads.
func (h *SalesHandler) ListFPOLeads(w http.ResponseWriter, r *http.Request) {
	q, ok := h.leadQuery(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListFPOLeads(r.Context(), tenantID(r), q)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	items := make([]fpoLeadPayload, 0, len(page.Leads))
	for _, l := range page.Leads {
		items = append(items, toFPOLeadPayload(l))
	}
	httpresponse.WriteJSON(w, http.StatusOK, fpoLeadPagePayload{
		Leads: items, Total: page.Total, StatusOptions: page.StatusOptions,
	})
}

// CreateFPOLead serves POST /sales/fpo-leads.
func (h *SalesHandler) CreateFPOLead(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body fpoLeadWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	created, err := h.service.CreateFPOLead(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toFPOLeadPayload(created))
}

// SetFPOLeadStatus serves POST /sales/fpo-leads/{lead_id}/status.
func (h *SalesHandler) SetFPOLeadStatus(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body leadStatusWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	updated, err := h.service.SetFPOLeadStatus(r.Context(), tenantID(r), r.PathValue("lead_id"),
		leadStatusFromPayload(body), httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toFPOLeadPayload(updated))
}

// CreateBenchmark serves POST /sales/market-benchmarks.
func (h *SalesHandler) CreateBenchmark(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body benchmarkWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	if err := h.service.CreateBenchmark(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key); err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, recordedPayload{Recorded: true})
}

// CreateSoldTags serves POST /sales/sold-tags.
func (h *SalesHandler) CreateSoldTags(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body soldTagsWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	recorded, err := h.service.CreateSoldTags(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, soldTagsResultPayload{Recorded: recorded})
}

// CreateWeightCheck serves POST /sales/weight-checks.
func (h *SalesHandler) CreateWeightCheck(w http.ResponseWriter, r *http.Request) {
	key, ok := h.requireIdemKey(w, r)
	if !ok {
		return
	}
	var body weightCheckWritePayload
	if !h.decodeInto(w, r, &body) {
		return
	}
	if err := h.service.CreateWeightCheck(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key); err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, recordedPayload{Recorded: true})
}
