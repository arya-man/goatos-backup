package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/app"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// Feed Analytics read: the windowed DIRECTED rollup. Every figure is what the
// sheet directed, never measured consumption — the DTO field names and the page
// contract copy both carry that word so no client can honestly relabel it.

type analyticsDayDTO struct {
	FeedDay      string `json:"feed_day"`
	DirectedKg   string `json:"directed_kg"`
	HeadDays     int64  `json:"head_days"`
	PerHeadGrams string `json:"per_head_grams"`
}

type analyticsItemDTO struct {
	FeedDay       string `json:"feed_day"`
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	DirectedKg    string `json:"directed_kg"`
	HeadDays      int64  `json:"head_days"`
	PerHeadGrams  string `json:"per_head_grams"`
}

type directedAnalyticsDTO struct {
	DateFrom string             `json:"date_from"`
	DateTo   string             `json:"date_to"`
	Days     []analyticsDayDTO  `json:"days"`
	Items    []analyticsItemDTO `json:"items"`
}

// GetDirectedAnalytics serves GET /feed-analytics/directed.
//
// date_from/date_to are optional business dates (inclusive). The default window
// is the 30 days ending YESTERDAY: today's sheet is still being executed, so
// every chart on the page excludes it, and the backend owns that rule rather
// than trusting each client to subtract a day.
func (h *Handler) GetDirectedAnalytics(w http.ResponseWriter, r *http.Request) {
	in, ok := h.analyticsInput(w, r)
	if !ok {
		return
	}
	result, err := h.service.DirectedAnalytics(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, r, "feed analytics directed", err)
		return
	}
	from, to := domain.ClampAnalyticsWindow(in.DateFrom, in.DateTo)
	dto := directedAnalyticsDTO{
		DateFrom: from.Format("2006-01-02"),
		DateTo:   to.Format("2006-01-02"),
		Days:     make([]analyticsDayDTO, 0, len(result.Days)),
		Items:    make([]analyticsItemDTO, 0, len(result.Items)),
	}
	for _, d := range result.Days {
		dto.Days = append(dto.Days, analyticsDayDTO(d))
	}
	for _, it := range result.Items {
		dto.Items = append(dto.Items, analyticsItemDTO(it))
	}
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}

// optionalBusinessDate parses an optional YYYY-MM-DD query param, applying the
// caller's default when absent. A PRESENT-but-malformed value is rejected, never
// silently replaced by the default the caller did not ask for.
func optionalBusinessDate(query map[string][]string, name string, fallback time.Time) (time.Time, error) {
	values := query[name]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return fallback, nil
	}
	return businessDateFromString(strings.TrimSpace(values[0]))
}

type executionDayDTO struct {
	Date                       string `json:"date"`
	PackingVerified            int64  `json:"packing_verified"`
	PackingAwaiting            int64  `json:"packing_awaiting"`
	PackingRework              int64  `json:"packing_rework"`
	DistributionVerified       int64  `json:"distribution_verified"`
	DistributionAwaiting       int64  `json:"distribution_awaiting"`
	DistributionRework         int64  `json:"distribution_rework"`
	TransportCompleted         int64  `json:"transport_completed"`
	TransportOpen              int64  `json:"transport_open"`
	TransportAwaitingVerdict   int64  `json:"transport_awaiting_verdict"`
	TransportRework            int64  `json:"transport_rework"`
	MedianVerifyLatencyMinutes *int64 `json:"median_verify_latency_minutes"`
}

// packingVarianceRowDTO is one intended-vs-entered packing mismatch. LEADERSHIP-ONLY payload: the
// verifier enters her readings blind and the page serving this is leadership-gated -- never render
// this comparison on a verifier surface.
type packingVarianceRowDTO struct {
	FeedDay                    string `json:"feed_day"`
	ParkLabel                  string `json:"park_label"`
	ShedID                     string `json:"shed_id"`
	ShedLabel                  string `json:"shed_label"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	SessionNo                  int32  `json:"session_no"`
	SessionLabel               string `json:"session_label,omitempty"`
	Workflow                   string `json:"workflow"`
	FeedItemKey                string `json:"feed_item_key"`
	FeedItemLabel              string `json:"feed_item_label"`
	// PlannedKg is "" when the frozen sheet carried no resolved quantity -- blank and zero are
	// never conflated.
	PlannedKg  string `json:"planned_kg"`
	VerifiedKg string `json:"verified_kg"`
	VarianceKg string `json:"variance_kg"`
}

type feedConsumptionRowDTO struct {
	FeedDay                    string `json:"feed_day"`
	ParkLabel                  string `json:"park_label"`
	ShedID                     string `json:"shed_id"`
	ShedLabel                  string `json:"shed_label"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	BreedLabel                 string `json:"breed_label"`
	AgeGroup                   string `json:"age_group"`
	FeedItemKey                string `json:"feed_item_key"`
	FeedItemLabel              string `json:"feed_item_label"`
	TargetKg                   string `json:"target_kg"`
	ActualKg                   string `json:"actual_kg"`
	VarianceKg                 string `json:"variance_kg"`
	HasVariance                bool   `json:"has_variance"`
}

type feedConsumptionTrendDayDTO struct {
	FeedDay      string `json:"feed_day"`
	TargetKg     string `json:"target_kg"`
	ActualKg     string `json:"actual_kg"`
	VarianceRows int64  `json:"variance_rows"`
	ComparedRows int64  `json:"compared_rows"`
}

type executionAnalyticsDTO struct {
	DateFrom         string                       `json:"date_from"`
	DateTo           string                       `json:"date_to"`
	Days             []executionDayDTO            `json:"days"`
	ConsumptionRows  []feedConsumptionRowDTO      `json:"consumption_rows"`
	ConsumptionTrend []feedConsumptionTrendDayDTO `json:"consumption_trend"`
	// PackingVariance is always present (possibly empty) so the renderer needs no null branch.
	PackingVariance []packingVarianceRowDTO `json:"packing_variance"`
}

// GetExecutionAnalytics serves GET /feed-analytics/execution.
func (h *Handler) GetExecutionAnalytics(w http.ResponseWriter, r *http.Request) {
	in, ok := h.analyticsInput(w, r)
	if !ok {
		return
	}
	result, err := h.service.ExecutionAnalytics(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, r, "feed analytics execution", err)
		return
	}
	from, to := domain.ClampAnalyticsWindow(in.DateFrom, in.DateTo)
	dto := executionAnalyticsDTO{
		DateFrom: from.Format("2006-01-02"),
		DateTo:   to.Format("2006-01-02"),
		Days:     make([]executionDayDTO, 0, len(result.Days)),
	}
	for _, d := range result.Days {
		dto.Days = append(dto.Days, executionDayDTO{
			Date:                       d.Date,
			PackingVerified:            d.PackingVerified,
			PackingAwaiting:            d.PackingAwaiting,
			PackingRework:              d.PackingRework,
			DistributionVerified:       d.DistributionVerified,
			DistributionAwaiting:       d.DistributionAwaiting,
			DistributionRework:         d.DistributionRework,
			TransportCompleted:         d.TransportCompleted,
			TransportOpen:              d.TransportOpen,
			TransportAwaitingVerdict:   d.TransportAwaitingVerdict,
			TransportRework:            d.TransportRework,
			MedianVerifyLatencyMinutes: d.MedianVerifyLatencyMinutes,
		})
	}
	dto.ConsumptionRows = make([]feedConsumptionRowDTO, 0, len(result.ConsumptionRows))
	for _, row := range result.ConsumptionRows {
		dto.ConsumptionRows = append(dto.ConsumptionRows, feedConsumptionRowDTO{
			FeedDay:                    row.FeedDay,
			ParkLabel:                  row.ParkLabel,
			ShedID:                     row.ShedID,
			ShedLabel:                  row.ShedLabel,
			PartitionLabel:             row.PartitionLabel,
			OperationalLocationDisplay: row.OperationalLocationDisplay,
			BreedLabel:                 row.BreedLabel,
			AgeGroup:                   row.AgeGroup,
			FeedItemKey:                row.FeedItemKey,
			FeedItemLabel:              row.FeedItemLabel,
			TargetKg:                   row.TargetKg,
			ActualKg:                   row.ActualKg,
			VarianceKg:                 row.VarianceKg,
			HasVariance:                row.HasVariance,
		})
	}
	dto.ConsumptionTrend = make([]feedConsumptionTrendDayDTO, 0, len(result.ConsumptionTrend))
	for _, day := range result.ConsumptionTrend {
		dto.ConsumptionTrend = append(dto.ConsumptionTrend, feedConsumptionTrendDayDTO{
			FeedDay:      day.FeedDay,
			TargetKg:     day.TargetKg,
			ActualKg:     day.ActualKg,
			VarianceRows: day.VarianceRows,
			ComparedRows: day.ComparedRows,
		})
	}
	dto.PackingVariance = make([]packingVarianceRowDTO, 0, len(result.PackingVariance))
	for _, v := range result.PackingVariance {
		dto.PackingVariance = append(dto.PackingVariance, packingVarianceRowDTO{
			FeedDay:                    v.FeedDay,
			ParkLabel:                  v.ParkLabel,
			ShedID:                     v.ShedID,
			ShedLabel:                  v.ShedLabel,
			PartitionLabel:             v.PartitionLabel,
			OperationalLocationDisplay: v.OperationalLocationDisplay,
			SessionNo:                  v.SessionNo,
			SessionLabel:               v.SessionLabel,
			Workflow:                   v.Workflow,
			FeedItemKey:                v.FeedItemKey,
			FeedItemLabel:              v.FeedItemLabel,
			PlannedKg:                  v.PlannedKg,
			VerifiedKg:                 v.VerifiedKg,
			VarianceKg:                 v.VarianceKg,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}

type experimentItemDTO struct {
	FeedDay       string `json:"feed_day"`
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	Kg            string `json:"kg"`
}

type experimentWastagePenDTO struct {
	ShedID                     string `json:"shed_id"`
	ParkLabel                  string `json:"park_label"`
	ShedLabel                  string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	LifecycleStatus            string `json:"lifecycle_status"`
	WastageKg                  string `json:"wastage_kg"`
}

type experimentAnalyticsDTO struct {
	DateFrom    string                    `json:"date_from"`
	DateTo      string                    `json:"date_to"`
	Items       []experimentItemDTO       `json:"items"`
	WastageDay  string                    `json:"wastage_day"`
	WastagePens []experimentWastagePenDTO `json:"wastage_pens"`
}

// GetExperimentAnalytics serves GET /feed-analytics/experiment.
func (h *Handler) GetExperimentAnalytics(w http.ResponseWriter, r *http.Request) {
	in, ok := h.analyticsInput(w, r)
	if !ok {
		return
	}
	// wastage_day picks the single business day the per-pen wastage table
	// describes; absent means TODAY (Asia/Kolkata) — wastage is collected live
	// during the feed day, unlike the kg window which ends yesterday.
	today := biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation()))
	wastageDay, err := optionalBusinessDate(r.URL.Query(), "wastage_day", today)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return
	}
	in.WastageDay = wastageDay
	result, err := h.service.ExperimentAnalytics(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, r, "feed analytics experiment", err)
		return
	}
	from, to := domain.ClampAnalyticsWindow(in.DateFrom, in.DateTo)
	dto := experimentAnalyticsDTO{
		DateFrom:    from.Format("2006-01-02"),
		DateTo:      to.Format("2006-01-02"),
		Items:       make([]experimentItemDTO, 0, len(result.Items)),
		WastageDay:  result.WastageDay,
		WastagePens: make([]experimentWastagePenDTO, 0, len(result.WastagePens)),
	}
	for _, it := range result.Items {
		dto.Items = append(dto.Items, experimentItemDTO(it))
	}
	for _, p := range result.WastagePens {
		dto.WastagePens = append(dto.WastagePens, experimentWastagePenDTO(p))
	}
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}

// analyticsInput centralises the shared window + park-scope parsing of the
// three /feed-analytics/* reads. Returns ok=false after writing the error.
func (h *Handler) analyticsInput(w http.ResponseWriter, r *http.Request) (app.DirectedAnalyticsInput, bool) {
	tenantID := httpmiddleware.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized, "missing tenant context", nil)
		return app.DirectedAnalyticsInput{}, false
	}
	query := r.URL.Query()
	yesterday := biztime.BusinessDayStart(time.Now().In(biztime.DefaultLocation())).AddDate(0, 0, -1)
	dateTo, err := optionalBusinessDate(query, "date_to", yesterday)
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return app.DirectedAnalyticsInput{}, false
	}
	dateFrom, err := optionalBusinessDate(query, "date_from", dateTo.AddDate(0, 0, -29))
	if err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return app.DirectedAnalyticsInput{}, false
	}
	parkScope := httpmiddleware.ResolveAuthorizedParkScopeForCapabilities(
		r.Context(), tenantID, strings.TrimSpace(query.Get("park_id")), permissions.FeedDirectionRead,
	)
	if !parkScope.Allowed {
		httpresponse.WriteError(w, r, h.log, parkScope.Status, parkScope.Message, nil)
		return app.DirectedAnalyticsInput{}, false
	}
	return app.DirectedAnalyticsInput{
		TenantID:          tenantID,
		ParkID:            parkScope.ParkID,
		AuthorizedParkIDs: parkScope.ParkIDs,
		DateFrom:          dateFrom,
		DateTo:            dateTo,
	}, true
}

type stockItemDTO struct {
	FarmLabel     string `json:"farm_label"`
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	BalanceKg     string `json:"balance_kg"`
	AvgDailyKg    string `json:"avg_daily_kg"`
	DaysLeft      *int64 `json:"days_left"`
	LatestBatchNo int64  `json:"latest_batch_no"`
	LowStock      bool   `json:"low_stock"`
}

type expenditureDayDTO struct {
	FeedDay string `json:"feed_day"`
	Rupees  string `json:"rupees"`
}

type spendSummaryDTO struct {
	ThisWeek    string `json:"this_week"`
	ThisMonth   string `json:"this_month"`
	ThreeMonths string `json:"three_months"`
	ThisYear    string `json:"this_year"`
}

type stockFarmItemDTO struct {
	FarmLabel          string `json:"farm_label"`
	FeedItemLabel      string `json:"feed_item_label"`
	FeedItemKey        string `json:"feed_item_key"`
	FirstPurchaseDate  string `json:"first_purchase_date"`
	FirstDirectedDay   string `json:"first_directed_day"`
	AvgDailyKg         string `json:"avg_daily_kg"`
	WeeklyRequiredKg   string `json:"weekly_required_kg"`
	LastLoadBatchNo    int64  `json:"last_load_batch_no"`
	LastLoadDate       string `json:"last_load_date"`
	LastLoadQuantityKg string `json:"last_load_quantity_kg"`
	LastLoadVendor     string `json:"last_load_vendor"`
	LastLoadTotalCost  string `json:"last_load_total_cost"`
	LastLoadPerKgCost  string `json:"last_load_per_kg_cost"`
	LedgerStockKg      string `json:"ledger_stock_kg"`
}

type stockAnalyticsDTO struct {
	DateFrom    string              `json:"date_from"`
	DateTo      string              `json:"date_to"`
	Items       []stockItemDTO      `json:"items"`
	FarmItems   []stockFarmItemDTO  `json:"farm_items"`
	Expenditure []expenditureDayDTO `json:"expenditure"`
	Spend       spendSummaryDTO     `json:"spend"`
}

// GetStockAnalytics serves GET /feed-analytics/stock.
func (h *Handler) GetStockAnalytics(w http.ResponseWriter, r *http.Request) {
	in, ok := h.analyticsInput(w, r)
	if !ok {
		return
	}
	result, err := h.service.StockAnalytics(r.Context(), in)
	if err != nil {
		h.writeServiceError(w, r, "feed analytics stock", err)
		return
	}
	from, to := domain.ClampAnalyticsWindow(in.DateFrom, in.DateTo)
	dto := stockAnalyticsDTO{
		DateFrom:    from.Format("2006-01-02"),
		DateTo:      to.Format("2006-01-02"),
		Items:       make([]stockItemDTO, 0, len(result.Items)),
		FarmItems:   make([]stockFarmItemDTO, 0, len(result.FarmItems)),
		Expenditure: make([]expenditureDayDTO, 0, len(result.Expenditure)),
	}
	for _, it := range result.Items {
		dto.Items = append(dto.Items, stockItemDTO(it))
	}
	for _, fi := range result.FarmItems {
		dto.FarmItems = append(dto.FarmItems, stockFarmItemDTO(fi))
	}
	for _, d := range result.Expenditure {
		dto.Expenditure = append(dto.Expenditure, expenditureDayDTO(d))
	}
	dto.Spend = spendSummaryDTO(result.Spend)
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}
