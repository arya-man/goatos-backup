package identityhttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// Sale-allocation HTTP surface: pick, review, confirm.
//
// The handler is thin on purpose -- it decodes, calls, and responds. Every rule about
// which animals may be sold lives in identity/domain.ResolveSaleBlocker, reached through
// the service, so there is nothing here for a future edit to accidentally soften.

// SaleAllocationHandler serves the three sale-allocation routes.
type SaleAllocationHandler struct {
	service *app.SaleAllocationService
	log     *slog.Logger
}

func NewSaleAllocationHandler(service *app.SaleAllocationService, log ...*slog.Logger) *SaleAllocationHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &SaleAllocationHandler{service: service, log: l}
}

// respondSaleError maps an app error onto the shared error envelope. It reuses the
// module's existing writer so these routes cannot drift into their own error shape.
func (h *SaleAllocationHandler) respondSaleError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	envelope := domain.ErrorEnvelope{
		Code: "internal_error", Message: "internal server error",
		FieldErrors: []domain.FieldError{}, TraceID: traceID(r), Retryable: true,
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		status = appErr.HTTPStatus
		envelope.Code = appErr.Code
		envelope.Message = appErr.Message
		envelope.Retryable = appErr.Retryable
	}
	writeHandlerError(w, r, h.log, status, envelope, err)
}

// RegisterSaleAllocation wires the routes.
//
// They live under /admin/goats/... rather than under /sales/... because they READ AND
// WRITE HERD IDENTITY. Putting a route that exits animals under the sales prefix would
// invite a future reader to implement it inside the sales module, which is exactly the
// isolation lock migration 000177 exists to keep.
func RegisterSaleAllocation(mux *http.ServeMux, h *SaleAllocationHandler) {
	mux.HandleFunc("GET /admin/goats/sale-locations", h.ListSaleLocations)
	mux.HandleFunc("GET /admin/goats/sale-candidates", h.ListSaleCandidates)
	mux.HandleFunc("POST /admin/goats/sale-allocations/preview", h.PreviewSaleAllocation)
	mux.HandleFunc("POST /admin/goats/sale-allocations/confirm", h.ConfirmSaleAllocation)
	mux.HandleFunc("GET /admin/goats/sale-allocations/{sales_deal_id}", h.GetSaleAllocation)
	mux.HandleFunc("GET /admin/goats/sale-tagging", h.ListSaleTaggingQueue)
}

// allowedParkIDs resolves the caller's park scope for every allocation route (maintainer
// decision 2026-09-11: a park head tags animals at THEIR park and nowhere else).
//
// nil means tenant-wide -- the sales desk and the CXO see every park, exactly as before. A
// park-scoped caller gets the parks their sale-allocation grant carries, and a caller whose
// grants resolve to no park at all is refused here rather than served the whole farm. Resolved
// ONCE at the boundary and handed to the service as a plain list, so the service stays free of
// request context and testable with a slice.
func (h *SaleAllocationHandler) allowedParkIDs(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	decision := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(r.Context(), tenantID(r), "", permissions.SalesAllocateAnimals)
	if !decision.Allowed {
		h.respondSaleError(w, r, app.Forbidden(decision.Code, decision.Message))
		return nil, false
	}
	if len(decision.ParkIDs) == 0 {
		return nil, true
	}
	return append([]string(nil), decision.ParkIDs...), true
}

type saleCandidateListResponse struct {
	Candidates []saleCandidatePayload `json:"candidates"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

type saleCandidatePayload struct {
	GoatID                     string `json:"goat_id"`
	DisplayID                  string `json:"display_id,omitempty"`
	TagNumber                  string `json:"tag_number,omitempty"`
	SecondaryTagNumber         string `json:"secondary_tag_number,omitempty"`
	ParkID                     string `json:"park_id,omitempty"`
	ParkName                   string `json:"park_name,omitempty"`
	ShedID                     string `json:"shed_id,omitempty"`
	ShedName                   string `json:"shed_name,omitempty"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Breed                      string `json:"breed,omitempty"`
	Sex                        string `json:"sex,omitempty"`
	RowVersion                 int    `json:"row_version"`
	Sellable                   bool   `json:"sellable"`
	Blocker                    string `json:"blocker,omitempty"`
	// BlockedReason is farm copy composed by the backend. Clients render it verbatim and
	// must not compose their own sentence from `blocker` -- the code says WHICH rule
	// fired, the reason says what the person should do about it.
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// ListSaleCandidates is the picker read.
func (h *SaleAllocationHandler) ListSaleCandidates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	allowed, ok := h.allowedParkIDs(w, r)
	if !ok {
		return
	}
	candidates, cursor, err := h.service.ListSaleCandidates(r.Context(), app.ListSaleCandidatesInput{
		TenantID:       tenantID(r),
		AllowedParkIDs: allowed,
		ParkID:         strings.TrimSpace(q.Get("park_id")),
		ShedID:         strings.TrimSpace(q.Get("shed_id")),
		// Repeated ?partition_label= params, so several pens of one shed come back in one
		// page. A person picking for one sale routinely takes animals from more than one.
		PartitionLabels: q["partition_label"],
		Query:           strings.TrimSpace(q.Get("q")),
		Limit:           limit,
		Cursor:          strings.TrimSpace(q.Get("cursor")),
	})
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	out := saleCandidateListResponse{Candidates: make([]saleCandidatePayload, 0, len(candidates)), NextCursor: cursor}
	for _, c := range candidates {
		out.Candidates = append(out.Candidates, saleCandidatePayload{
			GoatID: c.GoatID, DisplayID: c.DisplayID, TagNumber: c.TagNumber,
			SecondaryTagNumber: c.SecondaryTagNumber,
			ParkID:             c.ParkID, ParkName: c.ParkName, ShedID: c.ShedID, ShedName: c.ShedName,
			PartitionLabel: c.PartitionLabel, OperationalLocationDisplay: c.OperationalLocationDisplay,
			Breed: c.Breed, Sex: c.Sex, RowVersion: c.RowVersion,
			Sellable: c.Sellable, Blocker: c.Blocker, BlockedReason: c.BlockedReason,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type saleAllocationRequest struct {
	SalesDealID string   `json:"sales_deal_id"`
	GoatIDs     []string `json:"goat_ids"`
	// animal_weights_kg: live weight per picked goat id, as decimal strings (maintainer
	// decision 2026-09-08). Ignored by preview; required for every animal on confirm.
	AnimalWeightsKg map[string]string `json:"animal_weights_kg,omitempty"`
	// animal_rates_rupees: the price agreed per picked goat id, as decimal strings (maintainer
	// decision 2026-09-11). OPTIONAL; the phone's tag-only flow sends one for every animal.
	AnimalRatesRupees map[string]string `json:"animal_rates_rupees,omitempty"`
	Reason            string            `json:"reason,omitempty"`
}

type saleShedGroupPayload struct {
	ParkName                   string   `json:"park_name,omitempty"`
	ShedID                     string   `json:"shed_id,omitempty"`
	ShedName                   string   `json:"shed_name,omitempty"`
	PartitionLabel             string   `json:"partition_label,omitempty"`
	OperationalLocationDisplay string   `json:"operational_location_display"`
	Animals                    int      `json:"animals"`
	TagNumbers                 []string `json:"tag_numbers"`
}

type salePreviewResponse struct {
	SalesDealID         string                 `json:"sales_deal_id"`
	DeclaredAnimalCount int                    `json:"declared_animal_count"`
	AlreadyTagged       int                    `json:"already_tagged"`
	Complete            bool                   `json:"complete"`
	Sellable            int                    `json:"sellable"`
	Blocked             int                    `json:"blocked"`
	ShedGroups          []saleShedGroupPayload `json:"shed_groups"`
	BlockedAnimals      []saleCandidatePayload `json:"blocked_animals"`
}

type saleConfirmResponse struct {
	SalesDealID string                 `json:"sales_deal_id"`
	Allocated   int                    `json:"allocated"`
	ShedGroups  []saleShedGroupPayload `json:"shed_groups"`
	// Animals is the one-per-animal read-back (GET only): tag, pen, weight and rate as
	// recorded at tagging. Absent on a confirm response.
	Animals []saleAllocationAnimalPayload `json:"animals,omitempty"`
}

type saleAllocationAnimalPayload struct {
	GoatID                     string `json:"goat_id"`
	TagNumber                  string `json:"tag_number,omitempty"`
	ShedID                     string `json:"shed_id,omitempty"`
	ShedName                   string `json:"shed_name,omitempty"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	WeightKg                   string `json:"weight_kg,omitempty"`
	RateRupees                 string `json:"rate_rupees,omitempty"`
}

// saleTaggingQueueResponse is the park head's tag-only queue. NO buyer, NO money.
type saleTaggingQueueResponse struct {
	Deals      []saleTaggingDealPayload `json:"deals"`
	NextCursor *string                  `json:"next_cursor,omitempty"`
}

type saleTaggingDealPayload struct {
	SalesDealID         string `json:"sales_deal_id"`
	SaleDate            string `json:"sale_date"`
	Farm                string `json:"farm"`
	ProductType         string `json:"product_type"`
	Breed               string `json:"breed,omitempty"`
	DeclaredAnimalCount int    `json:"declared_animal_count"`
	AlreadyTagged       int    `json:"already_tagged"`
	Remaining           int    `json:"remaining"`
}

// ListSaleTaggingQueue serves the park head's queue: the sales at their park still owed
// animals (maintainer decision 2026-09-11).
func (h *SaleAllocationHandler) ListSaleTaggingQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	allowed, ok := h.allowedParkIDs(w, r)
	if !ok {
		return
	}
	deals, cursor, err := h.service.ListSaleTaggingQueue(r.Context(), app.ListSaleTaggingQueueInput{
		TenantID:       tenantID(r),
		AllowedParkIDs: allowed,
		Limit:          limit,
		Cursor:         strings.TrimSpace(q.Get("cursor")),
	})
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	out := saleTaggingQueueResponse{Deals: make([]saleTaggingDealPayload, 0, len(deals)), NextCursor: cursor}
	for _, d := range deals {
		out.Deals = append(out.Deals, saleTaggingDealPayload{
			SalesDealID: d.SalesDealID, SaleDate: d.SaleDate, Farm: d.Farm,
			ProductType: d.ProductType, Breed: d.Breed,
			DeclaredAnimalCount: d.DeclaredAnimalCount, AlreadyTagged: d.AlreadyTagged, Remaining: d.Remaining(),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// PreviewSaleAllocation is the review step. It mutates nothing.
func (h *SaleAllocationHandler) PreviewSaleAllocation(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req saleAllocationRequest
	if err := decodeSaleBody(body, &req); err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	allowed, ok := h.allowedParkIDs(w, r)
	if !ok {
		return
	}
	preview, err := h.service.PreviewSaleAllocation(r.Context(), app.PreviewSaleAllocationInput{
		TenantID: tenantID(r), SalesDealID: req.SalesDealID, GoatIDs: req.GoatIDs, AllowedParkIDs: allowed,
	})
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	out := salePreviewResponse{
		SalesDealID:         preview.SalesDealID,
		DeclaredAnimalCount: preview.DeclaredAnimalCount,
		AlreadyTagged:       preview.AlreadyTagged,
		Complete:            preview.Complete,
		Sellable:            preview.Sellable,
		Blocked:             preview.Blocked,
		ShedGroups:          saleShedGroups(preview.ShedGroups),
	}
	out.BlockedAnimals = make([]saleCandidatePayload, 0, len(preview.BlockedAnimals))
	for _, c := range preview.BlockedAnimals {
		out.BlockedAnimals = append(out.BlockedAnimals, saleCandidatePayload{
			GoatID: c.GoatID, DisplayID: c.DisplayID, TagNumber: c.TagNumber,
			SecondaryTagNumber:         c.SecondaryTagNumber,
			OperationalLocationDisplay: c.OperationalLocationDisplay,
			RowVersion:                 c.RowVersion,
			Sellable:                   false, Blocker: c.Blocker, BlockedReason: c.BlockedReason,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ConfirmSaleAllocation records the allocation and exits every named animal as sold.
func (h *SaleAllocationHandler) ConfirmSaleAllocation(w http.ResponseWriter, r *http.Request) {
	body, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	var req saleAllocationRequest
	if err := decodeSaleBody(body, &req); err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	allowed, ok := h.allowedParkIDs(w, r)
	if !ok {
		return
	}
	result, err := h.service.ConfirmSaleAllocation(r.Context(), app.ConfirmSaleAllocationInput{
		TenantID:          tenantID(r),
		ActorID:           actorID(r),
		IdempotencyKey:    r.Header.Get("Idempotency-Key"),
		TraceID:           traceID(r),
		SalesDealID:       req.SalesDealID,
		GoatIDs:           req.GoatIDs,
		AnimalWeightsKg:   req.AnimalWeightsKg,
		AnimalRatesRupees: req.AnimalRatesRupees,
		AllowedParkIDs:    allowed,
		Reason:            req.Reason,
	})
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, saleConfirmResponse{
		SalesDealID: result.SalesDealID,
		Allocated:   result.Allocated,
		ShedGroups:  saleShedGroups(result.ShedGroups),
	})
}

// GetSaleAllocation reads back the animals one sale is made of, shed-wise.
func (h *SaleAllocationHandler) GetSaleAllocation(w http.ResponseWriter, r *http.Request) {
	groups, err := h.service.GetSaleAllocation(r.Context(), tenantID(r), r.PathValue("sales_deal_id"))
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	animals, err := h.service.GetSaleAllocationAnimals(r.Context(), tenantID(r), r.PathValue("sales_deal_id"))
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	total := 0
	for _, g := range groups {
		total += g.Animals
	}
	out := saleConfirmResponse{
		SalesDealID: r.PathValue("sales_deal_id"),
		Allocated:   total,
		ShedGroups:  saleShedGroups(groups),
		Animals:     make([]saleAllocationAnimalPayload, 0, len(animals)),
	}
	for _, a := range animals {
		out.Animals = append(out.Animals, saleAllocationAnimalPayload{
			GoatID: a.GoatID, TagNumber: a.TagNumber, ShedID: a.ShedID, ShedName: a.ShedName,
			PartitionLabel: a.PartitionLabel, OperationalLocationDisplay: a.OperationalLocationDisplay,
			WeightKg: a.WeightKg, RateRupees: a.RateRupees,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func saleShedGroups(in []ports.SaleAllocationShedGroup) []saleShedGroupPayload {
	out := make([]saleShedGroupPayload, 0, len(in))
	for _, g := range in {
		tags := g.TagNumbers
		if tags == nil {
			tags = []string{}
		}
		out = append(out, saleShedGroupPayload{
			ParkName: g.ParkName, ShedID: g.ShedID, ShedName: g.ShedName,
			PartitionLabel: g.PartitionLabel, OperationalLocationDisplay: g.OperationalLocationDisplay,
			Animals: g.Animals, TagNumbers: tags,
		})
	}
	return out
}

// decodeSaleBody rejects unknown fields, so a client that sends a field this route does
// not honour is told rather than silently ignored. That matters most for a field like an
// override flag: accepting and discarding it would read to the caller as honoured.
func decodeSaleBody(body []byte, dest any) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return app.BadRequest("invalid_body", "request body could not be read: "+err.Error())
	}
	return nil
}

// ListSaleLocations serves the picker's park/shed/pen vocabulary.
func (h *SaleAllocationHandler) ListSaleLocations(w http.ResponseWriter, r *http.Request) {
	catalog, err := h.service.GetSaleLocations(r.Context(), tenantID(r))
	if err != nil {
		h.respondSaleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}
