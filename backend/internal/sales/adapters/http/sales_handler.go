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
	GetValuationAssumptions(ctx context.Context, tenantID string) (domain.ValuationAssumptions, error)
	ListStageRegister(ctx context.Context, tenantID string) ([]domain.StageRegisterEntry, error)
	PutValuationAssumptions(ctx context.Context, tenantID string, write domain.ValuationAssumptions, actorID string) (domain.ValuationAssumptions, error)
	ListDeals(ctx context.Context, tenantID string, q app.DealListQuery) (ports.DealPage, error)
	CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error)
	SellableProducts(ctx context.Context, tenantID string) ([]domain.Product, map[string][]string, error)
	ListSellableProducts(ctx context.Context, tenantID string) ([]domain.ProductRow, error)
	FeedItems(ctx context.Context, tenantID string) ([]string, error)
	SellableSpecies(ctx context.Context, tenantID string) ([]string, error)
	SaveSellableProduct(ctx context.Context, tenantID string, write domain.ProductWrite, actorID string) (domain.Product, error)
	DeleteSellableProduct(ctx context.Context, tenantID, code, actorID string) error
	RecordDealPayment(ctx context.Context, tenantID, dealID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)
	UpdateDealPayment(ctx context.Context, tenantID, dealID, paymentID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error)
	DeleteDealPayment(ctx context.Context, tenantID, dealID, paymentID string, actorID, idempotencyKey string) (domain.Deal, error)
	SetDealStatus(ctx context.Context, tenantID, dealID, status string, stockShortfallAcknowledged bool, actorID string) (domain.Deal, error)
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
	// The farm's own list of what it sells, authored on Sales Config.
	mux.HandleFunc("GET /sales/products", h.ListSellableProducts)
	mux.HandleFunc("POST /sales/products", h.SaveSellableProduct)
	mux.HandleFunc("DELETE /sales/products/{product_code}", h.DeleteSellableProduct)
	mux.HandleFunc("GET /sales/valuation-assumptions", h.GetValuationAssumptions)
	mux.HandleFunc("PUT /sales/valuation-assumptions", h.PutValuationAssumptions)
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

// GetOptions serves GET /sales/options: the vocabularies a record-sale form renders.
//
// It is a TENANT READ now (migration 000422). What the farm sells is its own registry, and each
// product's variants are the farm's own live breeds and feed catalogue, so this can no longer be
// composed from constants at build time -- which was the whole point of retiring them.
func (h *SalesHandler) GetOptions(w http.ResponseWriter, r *http.Request) {
	products, variants, err := h.service.SellableProducts(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, buildSalesOptionsPayload(products, variants))
}

func buildSalesOptionsPayload(products []domain.Product, variants map[string][]string) salesOptionsPayload {
	statuses := make([]salesStatusOptionPayload, 0, len(domain.Statuses))
	for _, s := range domain.Statuses {
		statuses = append(statuses, salesStatusOptionPayload{Key: s, Label: s, Tone: domain.StatusTone(s)})
	}
	names := make([]string, 0, len(products))
	options := make([]salesProductOptionPayload, 0, len(products))
	breeds := make(map[string][]string, len(products))
	for _, p := range products {
		names = append(names, p.Name)
		options = append(options, salesProductOptionPayload{
			Name: p.Name, Code: p.Code, Kind: p.Kind, Unit: p.Unit,
			PricedPerUnit: p.PricedPerUnit(),
		})
		// The field keeps the name `breeds` so a client written before this still finds its list;
		// what it holds widened from an animal's breeds to any product's variants, and a feed
		// product's are the farm's feed items.
		list := variants[p.Name]
		if list == nil {
			list = []string{}
		}
		breeds[p.Name] = list
	}
	return salesOptionsPayload{
		Farms:                append([]string(nil), domain.Farms...),
		ProductTypes:         names,
		Products:             options,
		Breeds:               breeds,
		Statuses:             statuses,
		DefaultStatus:        domain.StatusDealClosed,
		MaxSaleDateDaysAhead: domain.MaxSaleDateDaysAhead,
	}
}

// ListSellableProducts serves GET /sales/products: the farm's registry as its editor sees it,
// archived rows included so a switched-off item can be switched back on.
func (h *SalesHandler) ListSellableProducts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.ListSellableProducts(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	out := make([]sellableProductPayload, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSellableProductPayload(row))
	}
	// The farm's configured feeds ride with the list, so the Feed row can SAY which feeds it
	// covers -- the reader sees the dropdown is theirs and invents nothing.
	feeds, err := h.service.FeedItems(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	species, err := h.service.SellableSpecies(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, sellableProductPagePayload{
		Products:  out,
		Kinds:     sellableProductKindPayloads(),
		Units:     sellableProductUnitPayloads(),
		FeedItems: feeds,
		Species:   species,
	})
}

// SaveSellableProduct serves POST /sales/products: add an item, or edit one.
func (h *SalesHandler) SaveSellableProduct(w http.ResponseWriter, r *http.Request) {
	var body sellableProductWritePayload
	if !decodeBody(h, w, r, &body, "That item could not be read. Check the fields and try again.") {
		return
	}
	saved, err := h.service.SaveSellableProduct(r.Context(), tenantID(r), body.toDomain(),
		httpmiddleware.ActorIDFromContext(r.Context()))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, salesProductOptionPayload{
		Name: saved.Name, Code: saved.Code, Kind: saved.Kind, Unit: saved.Unit,
		PricedPerUnit: saved.PricedPerUnit(),
	})
}

// DeleteSellableProduct serves DELETE /sales/products/{product_code}.
func (h *SalesHandler) DeleteSellableProduct(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSellableProduct(r.Context(), tenantID(r), r.PathValue("product_code"),
		httpmiddleware.ActorIDFromContext(r.Context())); err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
		body.Status, body.StockShortfallAcknowledged, httpmiddleware.ActorIDFromContext(r.Context()))
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
	return decodeBody(h, w, r, dst, "That sale form could not be read. Check the fields and try again.")
}

// decodeBody reads one bounded JSON body, refusing a field the payload does not declare -- so a
// client sending something this build does not understand is TOLD, rather than having it dropped
// and the write silently doing something else.
func decodeBody[T any](h *SalesHandler, w http.ResponseWriter, r *http.Request, dst *T, message string) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxSalesRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", message))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", message))
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
