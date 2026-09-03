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
	candidates, cursor, err := h.service.ListSaleCandidates(r.Context(), app.ListSaleCandidatesInput{
		TenantID: tenantID(r),
		ParkID:   strings.TrimSpace(q.Get("park_id")),
		ShedID:   strings.TrimSpace(q.Get("shed_id")),
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
	Reason      string   `json:"reason,omitempty"`
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
	preview, err := h.service.PreviewSaleAllocation(r.Context(), app.PreviewSaleAllocationInput{
		TenantID: tenantID(r), SalesDealID: req.SalesDealID, GoatIDs: req.GoatIDs,
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
	result, err := h.service.ConfirmSaleAllocation(r.Context(), app.ConfirmSaleAllocationInput{
		TenantID:       tenantID(r),
		ActorID:        actorID(r),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		TraceID:        traceID(r),
		SalesDealID:    req.SalesDealID,
		GoatIDs:        req.GoatIDs,
		Reason:         req.Reason,
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
	total := 0
	for _, g := range groups {
		total += g.Animals
	}
	writeJSON(w, http.StatusOK, saleConfirmResponse{
		SalesDealID: r.PathValue("sales_deal_id"),
		Allocated:   total,
		ShedGroups:  saleShedGroups(groups),
	})
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
