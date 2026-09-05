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

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// VendorService is the vendor-register behaviour this transport depends on.
type VendorService interface {
	ListVendors(ctx context.Context, tenantID string, q app.VendorListQuery) (ports.VendorPage, error)
	GetVendor(ctx context.Context, tenantID, vendorID string, includeFinance bool) (domain.Vendor, error)
	CreateVendor(ctx context.Context, tenantID string, write domain.VendorWrite, actorID string, includeFinance bool) (domain.Vendor, error)
	UpdateVendor(ctx context.Context, tenantID, vendorID string, write domain.VendorWrite, rowVersion int64, actorID string, includeFinance bool) (domain.Vendor, error)
	UpdateVendorStatus(ctx context.Context, tenantID, vendorID, status string, rowVersion int64, actorID string) (domain.Vendor, error)
	ListVendorCatalog(ctx context.Context, tenantID string, side string) ([]domain.VendorCatalogEntry, error)
	ListVendorOptions(ctx context.Context, tenantID string) (domain.VendorOptions, error)
}

// VendorHandler serves /procurement/vendors.
type VendorHandler struct {
	service VendorService
	log     *slog.Logger
}

func NewVendorHandler(service VendorService, log ...*slog.Logger) *VendorHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &VendorHandler{service: service, log: l}
}

// RegisterVendors mounts the vendor register.
//
// Kept separate from Register so the register can be wired with its own service, and so the route
// list for the contact book is readable on its own rather than buried in the source-entry mux.
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the permission
// table is matched by method + pattern, and a mismatch serves the route ungated.
func RegisterVendors(mux *http.ServeMux, h *VendorHandler) {
	mux.HandleFunc("GET /procurement/vendors", h.ListVendors)
	mux.HandleFunc("POST /procurement/vendors", h.CreateVendor)
	mux.HandleFunc("GET /procurement/vendors/{vendor_id}", h.GetVendor)
	mux.HandleFunc("PUT /procurement/vendors/{vendor_id}", h.UpdateVendor)
	mux.HandleFunc("POST /procurement/vendors/{vendor_id}/status", h.UpdateVendorStatus)
	mux.HandleFunc("GET /procurement/vendor-catalog", h.ListVendorCatalog)
	mux.HandleFunc("GET /procurement/vendor-options", h.ListVendorOptions)
}

// maxVendorRequestBytes caps a write body. The register's largest legitimate payload is a couple of
// kilobytes; the cap stops a malformed or hostile client streaming an unbounded body into memory.
const maxVendorRequestBytes = 64 * 1024

// callerMaySeeFinance resolves VendorFinanceRead from the REQUEST's active grants.
//
// This is read from the caller's grants and never from a query parameter or request body: a client
// must not be able to ask for payment instruments it does not hold the permission for. It is the
// only input that decides whether bank/account/IFSC/UPI/PAN reach the response.
func callerMaySeeFinance(r *http.Request) bool {
	for _, grant := range httpmiddleware.AuthGrantsFromContext(r.Context()) {
		if permissions.RoleHasPermission(grant.Role, permissions.VendorFinanceRead) {
			return true
		}
	}
	return false
}

// ListVendors serves GET /procurement/vendors.
func (h *VendorHandler) ListVendors(w http.ResponseWriter, r *http.Request) {
	labels := h.catalogLabels(r)
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

	page, err := h.service.ListVendors(r.Context(), tenantID(r), app.VendorListQuery{
		Filter: domain.VendorFilter{
			Search:     q.Get("search"),
			RecordType: q.Get("record_type"),
			Status:     q.Get("status"),
			State:      q.Get("state"),
			City:       q.Get("city"),
			Breed:      q.Get("breed"),
			// SIDE names which half of the register the caller is looking at: Procurement > Vendors
			// or Sales > Vendors. Absent means the whole register, which is what every reader meant
			// before the split. An unknown value is REFUSED by the service, never widened.
			Side: q.Get("side"),
		},
		Limit:          limit,
		Offset:         offset,
		IncludeFinance: callerMaySeeFinance(r),
	})
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}

	items := make([]vendorPayload, 0, len(page.Vendors))
	for _, v := range page.Vendors {
		items = append(items, toVendorPayload(v, labels))
	}
	httpresponse.WriteJSON(w, http.StatusOK, vendorListPayload{
		Vendors: items,
		// Total is the WHOLE-FILTER count, not len(Vendors). The screen renders it as "306 vendors"
		// and derives the page count from it; taking it from the page would report the page size.
		Total:  page.Total,
		Limit:  domain.ClampVendorPageSize(limit),
		Offset: offset,
	})
}

// GetVendor serves GET /procurement/vendors/{vendor_id}.
func (h *VendorHandler) GetVendor(w http.ResponseWriter, r *http.Request) {
	labels := h.catalogLabels(r)
	vendorID := r.PathValue("vendor_id")
	if strings.TrimSpace(vendorID) == "" {
		h.writeErr(w, r, app.BadRequest("invalid_vendor_id", "That vendor link is not valid."))
		return
	}
	vendor, err := h.service.GetVendor(r.Context(), tenantID(r), vendorID, callerMaySeeFinance(r))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toVendorPayload(vendor, labels))
}

// CreateVendor serves POST /procurement/vendors.
func (h *VendorHandler) CreateVendor(w http.ResponseWriter, r *http.Request) {
	labels := h.catalogLabels(r)
	var body vendorWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	created, err := h.service.CreateVendor(r.Context(), tenantID(r), body.toDomain(), httpmiddleware.ActorIDFromContext(r.Context()), callerMaySeeFinance(r))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusCreated, toVendorPayload(created, labels))
}

// UpdateVendor serves PUT /procurement/vendors/{vendor_id}.
func (h *VendorHandler) UpdateVendor(w http.ResponseWriter, r *http.Request) {
	labels := h.catalogLabels(r)
	vendorID := r.PathValue("vendor_id")
	if strings.TrimSpace(vendorID) == "" {
		h.writeErr(w, r, app.BadRequest("invalid_vendor_id", "That vendor link is not valid."))
		return
	}
	var body vendorWritePayload
	if !h.decode(w, r, &body) {
		return
	}
	updated, err := h.service.UpdateVendor(r.Context(), tenantID(r), vendorID, body.toDomain(), body.RowVersion, httpmiddleware.ActorIDFromContext(r.Context()), callerMaySeeFinance(r))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toVendorPayload(updated, labels))
}

// UpdateVendorStatus serves POST /procurement/vendors/{vendor_id}/status.
func (h *VendorHandler) UpdateVendorStatus(w http.ResponseWriter, r *http.Request) {
	labels := h.catalogLabels(r)
	vendorID := r.PathValue("vendor_id")
	if strings.TrimSpace(vendorID) == "" {
		h.writeErr(w, r, app.BadRequest("invalid_vendor_id", "That vendor link is not valid."))
		return
	}
	var body vendorStatusPayload
	dec := json.NewDecoder(io.LimitReader(r.Body, maxVendorRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That status change could not be read."))
		return
	}
	updated, err := h.service.UpdateVendorStatus(r.Context(), tenantID(r), vendorID, body.Status, body.RowVersion,
		httpmiddleware.ActorIDFromContext(r.Context()))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toVendorPayload(updated, labels))
}

// ListVendorCatalog serves GET /procurement/vendor-catalog.
func (h *VendorHandler) ListVendorCatalog(w http.ResponseWriter, r *http.Request) {
	entries, err := h.service.ListVendorCatalog(r.Context(), tenantID(r), r.URL.Query().Get("side"))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	// Grouped by kind so the client can bind a dropdown directly without re-bucketing the list.
	grouped := map[string][]vendorCatalogEntryPayload{}
	for _, kind := range domain.VendorCatalogKinds {
		grouped[kind] = []vendorCatalogEntryPayload{}
	}
	for _, e := range entries {
		grouped[e.Kind] = append(grouped[e.Kind], vendorCatalogEntryPayload{
			Value: e.Value, Label: e.Label, IsActive: e.IsActive,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, vendorCatalogPayload{
		RecordTypes:       grouped[domain.CatalogKindRecordType],
		Breeds:            grouped[domain.CatalogKindBreed],
		States:            grouped[domain.CatalogKindState],
		Cities:            grouped[domain.CatalogKindCity],
		CapacityUnits:     grouped[domain.CatalogKindCapacityUnit],
		SupplyFrequencies: grouped[domain.CatalogKindSupplyFrequency],
		Statuses:          grouped[domain.CatalogKindStatus],
		Feeds:             grouped[domain.CatalogKindFeed],
	})
}

// ListVendorOptions serves GET /procurement/vendor-options.
//
// The ACTIVE register as a bounded picklist, read by any screen that must name a counterparty --
// today the Sales record-sale drawer, which maps every deal to a vendor (maintainer decision
// 2026-08-27).
//
// Separate from ListVendors rather than a mode of it, for three reasons: it is active-only (a
// banned or inactive vendor must not be offerable as the buyer of a NEW deal), it is not paged (a
// dropdown that stops at page one silently hides buyers), and it carries five columns instead of
// the full row, so it stays outside the VendorFinanceRead surface entirely.
func (h *VendorHandler) ListVendorOptions(w http.ResponseWriter, r *http.Request) {
	options, err := h.service.ListVendorOptions(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.VendorHTTPError(err))
		return
	}
	items := make([]vendorOptionPayload, 0, len(options.Vendors))
	for _, v := range options.Vendors {
		items = append(items, vendorOptionPayload{
			VendorID: v.VendorID, BusinessName: v.BusinessName,
			RecordType: v.RecordType, City: v.City, State: v.State,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, vendorOptionsPayload{Vendors: items, Truncated: options.Truncated})
}

// decode reads and validates a JSON write body.
//
// DisallowUnknownFields is deliberate: a client sending "buisness_name" must be told, not silently
// ignored into an empty required field. It is the same fail-loud rule the fixture loaders use.
func (h *VendorHandler) decode(w http.ResponseWriter, r *http.Request, dst *vendorWritePayload) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxVendorRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That vendor form could not be read. Check the fields and try again."))
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That vendor form could not be read. Check the fields and try again."))
		return false
	}
	return true
}

func (h *VendorHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}

// catalogLabels resolves the tenant's catalog once per request so a vendor's capacity line can be
// composed with the LABELS the farm chose ("Every 2 weeks") rather than the stored values. A
// catalog read failure degrades to the raw values rather than failing the vendor read: the
// register must stay readable when its vocabulary table is momentarily unreachable.
func (h *VendorHandler) catalogLabels(r *http.Request) catalogLabels {
	// The WHOLE vocabulary, deliberately unnarrowed by side: this resolves the labels ON a vendor
	// row, and a row must render its own capacity unit correctly whichever register it is listed in.
	entries, err := h.service.ListVendorCatalog(r.Context(), tenantID(r), "")
	if err != nil {
		h.log.Warn("vendor catalog labels unavailable", "err", err)
		return nil
	}
	out := catalogLabels{}
	for _, e := range entries {
		if out[e.Kind] == nil {
			out[e.Kind] = map[string]string{}
		}
		out[e.Kind][e.Value] = e.Label
	}
	return out
}
