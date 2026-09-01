package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// LoadwiseService is the behaviour this transport depends on.
type LoadwiseService interface {
	LoadwiseSales(ctx context.Context, tenantID string) (domain.LoadwiseSales, error)
	SetLoadCost(ctx context.Context, tenantID, loadID string, edit domain.LoadCostEdit, actorID string) error
}

// LoadwiseHandler serves the Sales page's load-wise reconciliation and the load-cost entry.
type LoadwiseHandler struct {
	service LoadwiseService
	log     *slog.Logger
}

func NewLoadwiseHandler(service LoadwiseService, log ...*slog.Logger) *LoadwiseHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &LoadwiseHandler{service: service, log: l}
}

// RegisterLoadwise mounts the load-wise read and the load-cost write.
//
// Patterns here must stay byte-identical to the entries in permissions/routes.go -- the permission
// table is matched by method + pattern, and a mismatch serves the route ungated.
func RegisterLoadwise(mux *http.ServeMux, h *LoadwiseHandler) {
	mux.HandleFunc("GET /procurement/loadwise-sales", h.LoadwiseSales)
	mux.HandleFunc("PUT /procurement/loads/{load_id}/cost", h.SetLoadCost)
}

// maxLoadCostRequestBytes caps the cost body: three optional numbers.
const maxLoadCostRequestBytes = 8 * 1024

type loadwiseLoadPayload struct {
	LoadID       string `json:"load_id"`
	LoadRef      string `json:"load_ref,omitempty"`
	VendorName   string `json:"vendor_name"`
	PurchaseDate string `json:"purchase_date,omitempty"`
	Status       string `json:"status"`
	Farm         string `json:"farm,omitempty"`

	DeclaredCount int `json:"declared_count"`
	Purchased     int `json:"purchased"`
	Sold          int `json:"sold"`
	Mortality     int `json:"mortality"`
	OtherExits    int `json:"other_exits"`
	Remaining     int `json:"remaining"`
	Unaccounted   int `json:"unaccounted"`

	AnimalCost    *float64 `json:"animal_cost,omitempty"`
	TransportCost *float64 `json:"transport_cost,omitempty"`
	OtherCost     *float64 `json:"other_cost,omitempty"`
	PurchaseValue *float64 `json:"purchase_value,omitempty"`

	SoldValue      float64  `json:"sold_value"`
	SoldPriced     int      `json:"sold_priced"`
	AvgSoldPrice   *float64 `json:"avg_sold_price,omitempty"`
	PriceBasis     string   `json:"price_basis"`
	RemainingValue *float64 `json:"remaining_value,omitempty"`
	ProfitLoss     *float64 `json:"profit_loss,omitempty"`

	// The pre-GoatOS history already folded into the counts above, exposed so the screen can say
	// "already sold / already died before tracking started" with the dates it spans.
	PriorSold *loadwisePriorPayload `json:"prior_sold,omitempty"`
	PriorDead *loadwisePriorPayload `json:"prior_dead,omitempty"`

	RowVersion int `json:"row_version"`
}

type loadwisePriorPayload struct {
	Count   int      `json:"count"`
	Value   *float64 `json:"value,omitempty"`
	FirstOn string   `json:"first_on,omitempty"`
	LastOn  string   `json:"last_on,omitempty"`
}

func toPriorPayload(p domain.LoadwisePriorOutcome) *loadwisePriorPayload {
	if p.Count == 0 {
		return nil
	}
	return &loadwisePriorPayload{Count: p.Count, Value: p.Value, FirstOn: p.FirstOn, LastOn: p.LastOn}
}

type loadwiseSummaryPayload struct {
	Purchased   int `json:"purchased"`
	Sold        int `json:"sold"`
	Mortality   int `json:"mortality"`
	OtherExits  int `json:"other_exits"`
	Remaining   int `json:"remaining"`
	Unaccounted int `json:"unaccounted"`

	PurchaseValue  float64 `json:"purchase_value"`
	CostedLoads    int     `json:"costed_loads"`
	SoldValue      float64 `json:"sold_value"`
	RemainingValue float64 `json:"remaining_value"`
	ProfitLoss     float64 `json:"profit_loss"`
}

type loadwisePayload struct {
	Loads               []loadwiseLoadPayload  `json:"loads"`
	TotalLoads          int                    `json:"total_loads"`
	OverallAvgSoldPrice *float64               `json:"overall_avg_sold_price,omitempty"`
	Summary             loadwiseSummaryPayload `json:"summary"`
}

// LoadwiseSales serves GET /procurement/loadwise-sales.
func (h *LoadwiseHandler) LoadwiseSales(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.LoadwiseSales(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.LoadwiseHTTPError(err))
		return
	}
	loads := make([]loadwiseLoadPayload, 0, len(out.Loads))
	for _, l := range out.Loads {
		loads = append(loads, loadwiseLoadPayload{
			LoadID:       l.LoadID,
			LoadRef:      l.LoadRef,
			VendorName:   l.VendorName,
			PurchaseDate: l.PurchaseDate,
			Status:       l.Status,
			Farm:         l.Farm,

			DeclaredCount: l.DeclaredCount,
			Purchased:     l.Purchased,
			Sold:          l.Sold,
			Mortality:     l.Mortality,
			OtherExits:    l.OtherExits,
			Remaining:     l.Remaining,
			Unaccounted:   l.Unaccounted,

			AnimalCost:    l.AnimalCost,
			TransportCost: l.TransportCost,
			OtherCost:     l.OtherCost,
			PurchaseValue: l.PurchaseValue,

			SoldValue:      l.SoldValue,
			SoldPriced:     l.SoldPriced,
			AvgSoldPrice:   l.AvgSoldPrice,
			PriceBasis:     l.PriceBasis,
			RemainingValue: l.RemainingValue,
			ProfitLoss:     l.ProfitLoss,

			PriorSold: toPriorPayload(l.PriorSold),
			PriorDead: toPriorPayload(l.PriorDead),

			RowVersion: l.RowVersion,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, loadwisePayload{
		Loads: loads,
		// TotalLoads and the summary are whole-read aggregates over the served scope, per the
		// operational read-model contract; no client re-derives its own totals.
		TotalLoads:          out.TotalLoads,
		OverallAvgSoldPrice: out.OverallAvgSoldPrice,
		Summary: loadwiseSummaryPayload{
			Purchased:   out.Summary.Purchased,
			Sold:        out.Summary.Sold,
			Mortality:   out.Summary.Mortality,
			OtherExits:  out.Summary.OtherExits,
			Remaining:   out.Summary.Remaining,
			Unaccounted: out.Summary.Unaccounted,

			PurchaseValue:  out.Summary.PurchaseValue,
			CostedLoads:    out.Summary.CostedLoads,
			SoldValue:      out.Summary.SoldValue,
			RemainingValue: out.Summary.RemainingValue,
			ProfitLoss:     out.Summary.ProfitLoss,
		},
	})
}

type loadCostWritePayload struct {
	AnimalCost    *float64 `json:"animal_cost"`
	TransportCost *float64 `json:"transport_cost"`
	OtherCost     *float64 `json:"other_cost"`
}

// SetLoadCost serves PUT /procurement/loads/{load_id}/cost.
func (h *LoadwiseHandler) SetLoadCost(w http.ResponseWriter, r *http.Request) {
	var body loadCostWritePayload
	// DisallowUnknownFields, the same fail-loud rule every procurement write body keeps.
	dec := json.NewDecoder(io.LimitReader(r.Body, maxLoadCostRequestBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That cost form could not be read. Check the fields and try again."))
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeErr(w, r, app.BadRequest("invalid_body", "That cost form could not be read. Check the fields and try again."))
		return
	}
	err := h.service.SetLoadCost(r.Context(), tenantID(r), r.PathValue("load_id"), domain.LoadCostEdit{
		AnimalCost:    body.AnimalCost,
		TransportCost: body.TransportCost,
		OtherCost:     body.OtherCost,
	}, httpmiddleware.ActorIDFromContext(r.Context()))
	if err != nil {
		h.writeErr(w, r, app.LoadwiseHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (h *LoadwiseHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}
