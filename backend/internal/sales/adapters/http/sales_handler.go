// Package http serves the sales module's routes.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/sales/app"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// SalesService is the behaviour this transport depends on.
type SalesService interface {
	GetOverview(ctx context.Context, tenantID, farm string) (domain.Overview, error)
	ListDeals(ctx context.Context, tenantID string, q app.DealListQuery) (ports.DealPage, error)
	CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error)
	RecordDealPayment(ctx context.Context, tenantID, dealID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)
	UpdateDealPayment(ctx context.Context, tenantID, dealID, paymentID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)
	DeleteDealPayment(ctx context.Context, tenantID, dealID, paymentID string, actorID, idempotencyKey string) (domain.Deal, error)
	SetDealStatus(ctx context.Context, tenantID, dealID, status, actorID string) (domain.Deal, error)
	ListBuyerLeads(ctx context.Context, tenantID string, q app.LeadListQuery) (ports.BuyerLeadPage, error)
	CreateBuyerLead(ctx context.Context, tenantID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)
	SetBuyerLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)
	UpdateBuyerLead(ctx context.Context, tenantID, leadID string, write domain.BuyerLeadWrite, actorID, idempotencyKey string) (domain.BuyerLead, error)
	ListFPOLeads(ctx context.Context, tenantID string, q app.LeadListQuery) (ports.FPOLeadPage, error)
	CreateFPOLead(ctx context.Context, tenantID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error)
	SetFPOLeadStatus(ctx context.Context, tenantID, leadID string, write domain.LeadStatusWrite, actorID, idempotencyKey string) (domain.FPOLead, error)
	UpdateFPOLead(ctx context.Context, tenantID, leadID string, write domain.FPOLeadWrite, actorID, idempotencyKey string) (domain.FPOLead, error)
	CreateBenchmark(ctx context.Context, tenantID string, write domain.BenchmarkWrite, actorID, idempotencyKey string) error
	CreateSoldTags(ctx context.Context, tenantID string, write domain.SoldTagsWrite, actorID, idempotencyKey string) (int, error)
	CreateWeightCheck(ctx context.Context, tenantID string, write domain.WeightCheckWrite, actorID, idempotencyKey string) error
}

// SalesHandler serves /sales.
type SalesHandler struct {
	service SalesService
	log     *slog.Logger
}

func NewSalesHandler(service SalesService, log ...*slog.Logger) *SalesHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &SalesHandler{service: service, log: l}
}

// Register mounts the sales module.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the permission
// table is matched by method + pattern, and a mismatch serves the route ungated.
func Register(mux *http.ServeMux, h *SalesHandler) {
	mux.HandleFunc("GET /sales/overview", h.GetOverview)
	mux.HandleFunc("GET /sales/options", h.GetOptions)
	mux.HandleFunc("GET /sales/deals", h.ListDeals)
	mux.HandleFunc("POST /sales/deals", h.CreateDeal)
	mux.HandleFunc("POST /sales/deals/{deal_id}/payments", h.RecordDealPayment)
	mux.HandleFunc("PUT /sales/deals/{deal_id}/payments/{payment_id}", h.UpdateDealPayment)
	mux.HandleFunc("DELETE /sales/deals/{deal_id}/payments/{payment_id}", h.DeleteDealPayment)
	mux.HandleFunc("POST /sales/deals/{deal_id}/status", h.SetDealStatus)
	mux.HandleFunc("GET /sales/buyer-leads", h.ListBuyerLeads)
	mux.HandleFunc("POST /sales/buyer-leads", h.CreateBuyerLead)
	mux.HandleFunc("POST /sales/buyer-leads/{lead_id}", h.UpdateBuyerLead)
	mux.HandleFunc("POST /sales/buyer-leads/{lead_id}/status", h.SetBuyerLeadStatus)
	mux.HandleFunc("GET /sales/fpo-leads", h.ListFPOLeads)
	mux.HandleFunc("POST /sales/fpo-leads", h.CreateFPOLead)
	mux.HandleFunc("POST /sales/fpo-leads/{lead_id}", h.UpdateFPOLead)
	mux.HandleFunc("POST /sales/fpo-leads/{lead_id}/status", h.SetFPOLeadStatus)
	mux.HandleFunc("POST /sales/market-benchmarks", h.CreateBenchmark)
	mux.HandleFunc("POST /sales/sold-tags", h.CreateSoldTags)
	mux.HandleFunc("POST /sales/weight-checks", h.CreateWeightCheck)
}

// maxSalesRequestBytes caps a write body. The largest legitimate record-sale payload is well under
// a kilobyte; the cap stops a hostile client streaming an unbounded body into memory.
const maxSalesRequestBytes = 64 * 1024

// GetOverview serves GET /sales/overview.
func (h *SalesHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := h.service.GetOverview(r.Context(), tenantID(r), r.URL.Query().Get("farm"))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toOverviewPayload(overview))
}

// GetOptions serves GET /sales/options: the vocabularies a record-sale form renders. Static per
// build (they mirror the sales_deals CHECK constraints), so no tenant read is needed.
func (h *SalesHandler) GetOptions(w http.ResponseWriter, r *http.Request) {
	httpresponse.WriteJSON(w, http.StatusOK, buildSalesOptionsPayload())
}

func buildSalesOptionsPayload() salesOptionsPayload {
	statuses := make([]salesStatusOptionPayload, 0, len(domain.Statuses))
	for _, s := range domain.Statuses {
		statuses = append(statuses, salesStatusOptionPayload{Key: s, Label: s, Tone: domain.StatusTone(s)})
	}
	breeds := make(map[string][]string, len(domain.ProductTypes))
	for _, p := range domain.ProductTypes {
		breeds[p] = append([]string(nil), domain.BreedsByProduct[p]...)
	}
	return salesOptionsPayload{
		Farms:                append([]string(nil), domain.Farms...),
		ProductTypes:         append([]string(nil), domain.ProductTypes...),
		Breeds:               breeds,
		Statuses:             statuses,
		DefaultStatus:        domain.StatusDealClosed,
		MaxSaleDateDaysAhead: domain.MaxSaleDateDaysAhead,
	}
}

// ListDeals serves GET /sales/deals.
func (h *SalesHandler) ListDeals(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_offset", "That page is not valid."))
			return
		}
		offset = parsed
	}

	page, err := h.service.ListDeals(r.Context(), tenantID(r), app.DealListQuery{
		Farm:   q.Get("farm"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}

	items := make([]dealPayload, 0, len(page.Deals))
	for _, d := range page.Deals {
		items = append(items, toDealPayload(d))
	}
	httpresponse.WriteJSON(w, http.StatusOK, dealPagePayload{
		Deals: items,
		// Total is the WHOLE-FILTER count, not len(Deals); the screen derives the page count
		// from it.
		Total:  page.Total,
		Limit:  domain.ClampDealPageSize(limit),
		Offset: offset,
	})
}

// CreateDeal serves POST /sales/deals.
func (h *SalesHandler) CreateDeal(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This sale could not be recorded safely. Try again."))
		return
	}
	var body dealWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	created, err := h.service.CreateDeal(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toDealPayload(created))
}

// SetDealStatus serves POST /sales/deals/{deal_id}/status.
func (h *SalesHandler) SetDealStatus(w http.ResponseWriter, r *http.Request) {
	var body dealStatusWritePayload
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That status could not be read. Try again."))
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That status could not be read. Try again."))
		return
	}
	updated, err := h.service.SetDealStatus(r.Context(), tenantID(r), r.PathValue("deal_id"),
		body.Status, httpmiddleware.ActorIDFromContext(r.Context()))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toDealPayload(updated))
}

// RecordDealPayment serves POST /sales/deals/{deal_id}/payments.
func (h *SalesHandler) RecordDealPayment(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This payment could not be recorded safely. Try again."))
		return
	}
	var body dealPaymentWritePayload
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That payment form could not be read. Check the fields and try again."))
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That payment form could not be read. Check the fields and try again."))
		return
	}
	updated, err := h.service.RecordDealPayment(r.Context(), tenantID(r), r.PathValue("deal_id"),
		body.toDomain(), httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toDealPayload(updated))
}

// UpdateDealPayment serves PUT /sales/deals/{deal_id}/payments/{payment_id}.
func (h *SalesHandler) UpdateDealPayment(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This payment could not be edited safely. Try again."))
		return
	}
	var body dealPaymentWritePayload
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That payment form could not be read. Check the fields and try again."))
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That payment form could not be read. Check the fields and try again."))
		return
	}
	updated, err := h.service.UpdateDealPayment(r.Context(), tenantID(r), r.PathValue("deal_id"), r.PathValue("payment_id"),
		body.toDomain(), httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toDealPayload(updated))
}

// DeleteDealPayment serves DELETE /sales/deals/{deal_id}/payments/{payment_id}.
func (h *SalesHandler) DeleteDealPayment(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		h.writeErr(w, r, app.BadRequest("missing_idempotency_key", "This payment could not be removed safely. Try again."))
		return
	}
	updated, err := h.service.DeleteDealPayment(r.Context(), tenantID(r), r.PathValue("deal_id"), r.PathValue("payment_id"),
		httpmiddleware.ActorIDFromContext(r.Context()), key)
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toDealPayload(updated))
}

// decode reads and validates a JSON write body.
//
// DisallowUnknownFields is deliberate: a client sending "sales_valu" must be told, not silently
// ignored into a zero required field. Same fail-loud rule the fixture loaders use.
func (h *SalesHandler) decode(w http.ResponseWriter, r *http.Request, dst *dealWritePayload) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That sale form could not be read. Check the fields and try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That sale form could not be read. Check the fields and try again."))
		return false
	}
	return true
}

func (h *SalesHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

func tenantID(r *http.Request) string {
	return httpmiddleware.TenantIDFromContext(r.Context())
}
