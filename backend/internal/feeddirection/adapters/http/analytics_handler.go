package http

import (
	"net/http"
	"strconv"
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
	PackingDay                 string `json:"packing_day"`
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
	BreedLabel                 string `json:"breed_label"`
	// PlannedKg is "" when the frozen sheet carried no resolved quantity -- blank and zero are
	// never conflated.
	PlannedKg       string `json:"planned_kg"`
	VerifiedKg      string `json:"verified_kg"`
	VarianceKg      string `json:"variance_kg"`
}

type feedConsumptionTrendDayDTO struct {
	FeedDay      string `json:"feed_day"`
	PackingDay   string `json:"packing_day"`
	TargetKg     string `json:"target_kg"`
	ActualKg     string `json:"actual_kg"`
	VarianceRows int64  `json:"variance_rows"`
	ComparedRows int64  `json:"compared_rows"`
}

type executionAnalyticsDTO struct {
	DateFrom         string                       `json:"date_from"`
	DateTo           string                       `json:"date_to"`
	Days             []executionDayDTO            `json:"days"`
	ConsumptionTrend []feedConsumptionTrendDayDTO `json:"consumption_trend"`
	// PackingVariance is always present (possibly empty) so the renderer needs no null branch. It
	// is a PAGE; every other figure in this payload is a whole-window aggregate.
	PackingVariance        []packingVarianceRowDTO `json:"packing_variance"`
	PackingVarianceHasMore bool                    `json:"packing_variance_has_more"`
}

// GetExecutionAnalytics serves GET /feed-analytics/execution.
func (h *Handler) GetExecutionAnalytics(w http.ResponseWriter, r *http.Request) {
	in, ok := h.analyticsInput(w, r)
	if !ok {
		return
	}
	sections, ok := executionSections(w, r, h)
	if !ok {
		return
	}
	in.Sections = sections
	limit, offset, ok := executionVariancePage(w, r, h)
	if !ok {
		return
	}
	in.PackingVarianceLimit, in.PackingVarianceOffset = limit, offset
	in.PackingVarianceParkLabel = strings.TrimSpace(r.URL.Query().Get("variance_park_label"))
	in.PackingVarianceFeedItemKey = strings.TrimSpace(r.URL.Query().Get("variance_feed_item_key"))
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
	dto.ConsumptionTrend = make([]feedConsumptionTrendDayDTO, 0, len(result.ConsumptionTrend))
	for _, day := range result.ConsumptionTrend {
		dto.ConsumptionTrend = append(dto.ConsumptionTrend, feedConsumptionTrendDayDTO{
			FeedDay:      day.FeedDay,
			PackingDay:   day.PackingDay,
			TargetKg:     day.TargetKg,
			ActualKg:     day.ActualKg,
			VarianceRows: day.VarianceRows,
			ComparedRows: day.ComparedRows,
		})
	}
	dto.PackingVarianceHasMore = result.PackingVarianceHasMore
	dto.PackingVariance = make([]packingVarianceRowDTO, 0, len(result.PackingVariance))
	for _, v := range result.PackingVariance {
		dto.PackingVariance = append(dto.PackingVariance, packingVarianceRowDTO{
			FeedDay:                    v.FeedDay,
			PackingDay:                 v.PackingDay,
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
			BreedLabel:                 v.BreedLabel,
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

type stockForecastItemDTO struct {
	FarmLabel     string `json:"farm_label"`
	FeedItemLabel string `json:"feed_item_label"`
	FeedItemKey   string `json:"feed_item_key"`
	AvgDailyKg    string `json:"avg_daily_kg"`
	RequiredKg    string `json:"required_kg"`
	StockKg       string `json:"stock_kg"`
	ShortfallKg   string `json:"shortfall_kg"`
	PerKgCost     string `json:"per_kg_cost"`
	RequiredCost  string `json:"required_cost"`
}

type stockAnalyticsDTO struct {
	DateFrom  string             `json:"date_from"`
	DateTo    string             `json:"date_to"`
	Items     []stockItemDTO     `json:"items"`
	FarmItems []stockFarmItemDTO `json:"farm_items"`
	// Forecast is always present (possibly empty) so the renderer needs no null branch.
	Forecast    []stockForecastItemDTO `json:"forecast"`
	Expenditure []expenditureDayDTO    `json:"expenditure"`
	Spend       spendSummaryDTO        `json:"spend"`
}

// executionSections reads the optional `sections` narrowing. An unknown name is a 400 rather than
// a silent drop: serving a payload without the array the caller asked for would render as "the
// farm has no data" on a screen that simply asked wrong.
func executionSections(w http.ResponseWriter, r *http.Request, h *Handler) ([]domain.ExecutionSection, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("sections"))
	if raw == "" {
		return nil, true
	}
	var out []domain.ExecutionSection
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		section, ok := domain.ParseExecutionSection(name)
		if !ok {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "unknown sections value: "+name, nil)
			return nil, false
		}
		out = append(out, section)
	}
	return out, true
}

// executionVariancePage reads the mismatch list's page. A present but unparseable or out-of-range
// value is a 400: silently falling back to page one would answer a different question than the one
// the URL asks, under the heading of the page the reader thinks they are on.
func executionVariancePage(w http.ResponseWriter, r *http.Request, h *Handler) (int, int, bool) {
	query := r.URL.Query()
	read := func(name string) (int, bool) {
		raw := strings.TrimSpace(query.Get(name))
		if raw == "" {
			return 0, true
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, name+" must be a whole number", nil)
			return 0, false
		}
		return value, true
	}
	limit, ok := read("variance_limit")
	if !ok {
		return 0, 0, false
	}
	offset, ok := read("variance_offset")
	if !ok {
		return 0, 0, false
	}
	if _, _, err := domain.NormalisePackingVariancePage(limit, offset); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, err.Error(), nil)
		return 0, 0, false
	}
	return limit, offset, true
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
		Forecast:    make([]stockForecastItemDTO, 0, len(result.Forecast)),
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
	for _, f := range result.Forecast {
		dto.Forecast = append(dto.Forecast, stockForecastItemDTO(f))
	}
	dto.Spend = spendSummaryDTO(result.Spend)
	httpresponse.WriteJSON(w, http.StatusOK, dto)
}
