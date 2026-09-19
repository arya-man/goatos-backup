package http

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/sales/app"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// FARM VALUATION ASSUMPTIONS (maintainer instruction 2026-09-19): GET on sales.read, PUT on
// sales.valuation.write (permissions/routes.go). The wire shape is the row as authored.

type valuationBucketPayload struct {
	Bucket        string   `json:"bucket"`
	Label         string   `json:"label"`
	FixedWeightKg *float64 `json:"fixed_weight_kg"`
	PricePerKg    float64  `json:"price_per_kg"`
	DisplayOrder  int      `json:"display_order"`
}

type valuationPayload struct {
	Buckets                []valuationBucketPayload `json:"buckets"`
	UnsoldStockPriceRupees *float64                 `json:"unsold_stock_price_rupees"`
	RowVersion             int                      `json:"row_version"`
	UpdatedAt              string                   `json:"updated_at,omitempty"`
	UpdatedByName          string                   `json:"updated_by_name,omitempty"`
	// The bands the figures are refused outside of, so the form can say so before the round trip.
	Limits valuationLimitsPayload `json:"limits"`
}

type valuationLimitsPayload struct {
	PriceMin       float64 `json:"price_per_kg_min"`
	PriceMax       float64 `json:"price_per_kg_max"`
	WeightMin      float64 `json:"fixed_weight_kg_min"`
	WeightMax      float64 `json:"fixed_weight_kg_max"`
	UnsoldPriceMin float64 `json:"unsold_stock_price_min"`
	UnsoldPriceMax float64 `json:"unsold_stock_price_max"`
}

func toValuationPayload(v domain.ValuationAssumptions) valuationPayload {
	out := valuationPayload{
		UnsoldStockPriceRupees: v.UnsoldStockPriceRupees, RowVersion: v.RowVersion,
		UpdatedAt: v.UpdatedAt, UpdatedByName: v.UpdatedByName,
		Limits: valuationLimitsPayload{
			PriceMin: domain.ValuationPriceMinINR, PriceMax: domain.ValuationPriceMaxINR,
			WeightMin: domain.ValuationWeightMinKg, WeightMax: domain.ValuationWeightMaxKg,
			UnsoldPriceMin: domain.ValuationUnsoldMinINR, UnsoldPriceMax: domain.ValuationUnsoldMaxINR,
		},
		Buckets: make([]valuationBucketPayload, 0, len(v.Buckets)),
	}
	for _, b := range v.Buckets {
		out.Buckets = append(out.Buckets, valuationBucketPayload{Bucket: b.Bucket, Label: b.Label, FixedWeightKg: b.FixedWeightKg, PricePerKg: b.PricePerKg, DisplayOrder: b.DisplayOrder})
	}
	return out
}

// GetValuationAssumptions serves GET /sales/valuation-assumptions.
func (h *SalesHandler) GetValuationAssumptions(w http.ResponseWriter, r *http.Request) {
	v, err := h.service.GetValuationAssumptions(r.Context(), tenantID(r))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toValuationPayload(v))
}

// PutValuationAssumptions serves PUT /sales/valuation-assumptions.
func (h *SalesHandler) PutValuationAssumptions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxSalesRequestBytes))
	if err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_body", "The request could not be read."))
		return
	}
	var in valuationPayload
	if err := json.Unmarshal(body, &in); err != nil {
		h.writeErr(w, r, app.BadRequest("invalid_json", "The request body is not valid JSON."))
		return
	}
	write := domain.ValuationAssumptions{UnsoldStockPriceRupees: in.UnsoldStockPriceRupees, RowVersion: in.RowVersion}
	for _, b := range in.Buckets {
		write.Buckets = append(write.Buckets, domain.ValuationBucketRate{Bucket: b.Bucket, Label: b.Label, FixedWeightKg: b.FixedWeightKg, PricePerKg: b.PricePerKg, DisplayOrder: b.DisplayOrder})
	}
	v, err := h.service.PutValuationAssumptions(r.Context(), tenantID(r), write, httpmiddleware.ActorIDFromContext(r.Context()))
	if err != nil {
		h.writeErr(w, r, app.SalesHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, toValuationPayload(v))
}
