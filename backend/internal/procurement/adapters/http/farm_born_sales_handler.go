package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/procurement/app"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// FarmBornSalesService is the behaviour this transport depends on.
type FarmBornSalesService interface {
	FarmBornSales(ctx context.Context, tenantID string, req app.FarmBornRequest) (domain.FarmBornSales, error)
}

// FarmBornSalesHandler serves the Sales > Farm born read.
type FarmBornSalesHandler struct {
	service FarmBornSalesService
	log     *slog.Logger
}

func NewFarmBornSalesHandler(service FarmBornSalesService, log ...*slog.Logger) *FarmBornSalesHandler {
	l := slog.Default()
	if len(log) > 0 && log[0] != nil {
		l = log[0]
	}
	return &FarmBornSalesHandler{service: service, log: l}
}

// RegisterFarmBornSales mounts the read. The pattern must stay byte-identical to its entry in
// permissions/routes.go -- the permission table is matched by method + pattern, and a mismatch
// serves the route ungated.
func RegisterFarmBornSales(mux *http.ServeMux, h *FarmBornSalesHandler) {
	mux.HandleFunc("GET /procurement/farm-born-sales", h.FarmBornSales)
}

type farmBornBucketPayload struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	Detail     string  `json:"detail,omitempty"`
	ParkID     string  `json:"park_id,omitempty"`
	OnFarm     int     `json:"on_farm"`
	Sold       int     `json:"sold"`
	SoldPriced int     `json:"sold_priced"`
	Revenue    float64 `json:"revenue"`
}

type farmBornSummaryPayload struct {
	OnFarm     int     `json:"on_farm"`
	Sold       int     `json:"sold"`
	SoldPriced int     `json:"sold_priced"`
	Revenue    float64 `json:"revenue"`
	AvgPrice   float64 `json:"avg_price"`
	From       string  `json:"from"`
	To         string  `json:"to"`
	Origin     string  `json:"origin"`
}

type farmBornSoldRowPayload struct {
	GoatID    string   `json:"goat_id"`
	DisplayID string   `json:"display_id"`
	Tag       string   `json:"tag"`
	Species   string   `json:"species"`
	Breed     string   `json:"breed"`
	Sex       string   `json:"sex"`
	Stage     string   `json:"stage"`
	ParkName  string   `json:"park_name"`
	Pen       string   `json:"pen"`
	SaleDate  string   `json:"sale_date"`
	SaleValue *float64 `json:"sale_value,omitempty"`
	BuyerName string   `json:"buyer_name"`
	DealID    string   `json:"deal_id,omitempty"`
}

type farmBornOptionPayload struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	ParkID string `json:"park_id,omitempty"`
}

type farmBornOptionsPayload struct {
	Parks   []farmBornOptionPayload `json:"parks"`
	Pens    []farmBornOptionPayload `json:"pens"`
	Species []farmBornOptionPayload `json:"species"`
	Breeds  []farmBornOptionPayload `json:"breeds"`
	Sexes   []farmBornOptionPayload `json:"sexes"`
	Stages  []farmBornOptionPayload `json:"stages"`
}

type farmBornSalesPayload struct {
	Summary   farmBornSummaryPayload   `json:"summary"`
	ByBreed   []farmBornBucketPayload  `json:"by_breed"`
	BySex     []farmBornBucketPayload  `json:"by_sex"`
	ByStage   []farmBornBucketPayload  `json:"by_stage"`
	ByPen     []farmBornBucketPayload  `json:"by_pen"`
	Sold      []farmBornSoldRowPayload `json:"sold"`
	TotalSold int                      `json:"total_sold"`
	Limit     int                      `json:"limit"`
	Offset    int                      `json:"offset"`
	Options   farmBornOptionsPayload   `json:"options"`
}

// FarmBornSales serves GET /procurement/farm-born-sales.
func (h *FarmBornSalesHandler) FarmBornSales(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, offset := 0, 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_limit", "That page size is not valid."))
			return
		}
		limit = parsed
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			h.writeErr(w, r, app.BadRequest("invalid_offset", "That page is not valid."))
			return
		}
		offset = parsed
	}
	out, err := h.service.FarmBornSales(r.Context(), tenantID(r), app.FarmBornRequest{
		From:    q.Get("from"),
		To:      q.Get("to"),
		Origin:  q.Get("origin"),
		ParkID:  q.Get("park_id"),
		Pen:     q.Get("pen"),
		Species: q.Get("species"),
		Breed:   q.Get("breed"),
		Sex:     q.Get("sex"),
		Stage:   q.Get("stage"),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		h.writeErr(w, r, app.FarmBornHTTPError(err))
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, farmBornPayload(out))
}

func farmBornPayload(out domain.FarmBornSales) farmBornSalesPayload {
	s := out.Summary
	sold := make([]farmBornSoldRowPayload, 0, len(out.Sold))
	for _, row := range out.Sold {
		sold = append(sold, farmBornSoldRowPayload{
			GoatID:    row.GoatID,
			DisplayID: row.DisplayID,
			Tag:       row.Tag,
			Species:   row.Species,
			Breed:     row.Breed,
			Sex:       row.Sex,
			Stage:     row.Stage,
			ParkName:  row.ParkName,
			Pen:       row.PenDisplay,
			SaleDate:  row.SaleDate,
			SaleValue: row.SaleValue,
			BuyerName: row.BuyerName,
			DealID:    row.DealID,
		})
	}
	return farmBornSalesPayload{
		Summary: farmBornSummaryPayload{
			OnFarm: s.OnFarm, Sold: s.Sold, SoldPriced: s.SoldPriced, Revenue: s.Revenue,
			AvgPrice: s.AvgPrice, From: s.From, To: s.To, Origin: s.Origin,
		},
		ByBreed:   bucketsPayload(out.ByBreed),
		BySex:     bucketsPayload(out.BySex),
		ByStage:   bucketsPayload(out.ByStage),
		ByPen:     bucketsPayload(out.ByPen),
		Sold:      sold,
		TotalSold: out.TotalSold,
		Limit:     out.Limit,
		Offset:    out.Offset,
		Options: farmBornOptionsPayload{
			Parks:   optionsPayload(out.Options.Parks),
			Pens:    optionsPayload(out.Options.Pens),
			Species: optionsPayload(out.Options.Species),
			Breeds:  optionsPayload(out.Options.Breeds),
			Sexes:   optionsPayload(out.Options.Sexes),
			Stages:  optionsPayload(out.Options.Stages),
		},
	}
}

func bucketsPayload(in []domain.FarmBornBucket) []farmBornBucketPayload {
	out := make([]farmBornBucketPayload, 0, len(in))
	for _, b := range in {
		out = append(out, farmBornBucketPayload{
			Key: b.Key, Label: b.Label, Detail: b.Detail, ParkID: b.ParkID,
			OnFarm: b.OnFarm, Sold: b.Sold, SoldPriced: b.SoldPriced, Revenue: b.Revenue,
		})
	}
	return out
}

func optionsPayload(in []domain.FarmBornOption) []farmBornOptionPayload {
	out := make([]farmBornOptionPayload, 0, len(in))
	for _, o := range in {
		out = append(out, farmBornOptionPayload{Key: o.Key, Label: o.Label, ParkID: o.ParkID})
	}
	return out
}

func (h *FarmBornSalesHandler) writeErr(w http.ResponseWriter, r *http.Request, appErr *app.Error) {
	if appErr == nil {
		return
	}
	httpresponse.WriteError(w, r, h.log, appErr.HTTPStatus, map[string]any{
		"error":   appErr.Code,
		"message": appErr.Message,
	}, errors.New(appErr.Code))
}
