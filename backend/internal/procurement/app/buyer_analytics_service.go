package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// BuyerAnalyticsService serves the Sales > Buyer analytics page: every buyer the farm has sold
// to, with the register's contact facts and their purchase history.
//
// Thin like LoadwiseService: the repository resolves who bought, the domain groups and pages,
// and the service only validates the filter and pins the clock.
type BuyerAnalyticsService struct {
	repo ports.BuyerAnalyticsRepository
	now  func() time.Time
}

func NewBuyerAnalyticsService(repo ports.BuyerAnalyticsRepository) *BuyerAnalyticsService {
	return &BuyerAnalyticsService{repo: repo, now: time.Now}
}

// WithClock pins the clock "days since last purchase" is measured from. Tests only.
func (s *BuyerAnalyticsService) WithClock(now func() time.Time) *BuyerAnalyticsService {
	s.now = now
	return s
}

// ErrBuyerFarmInvalid is the rejected farm filter: a present-but-unknown farm is refused rather
// than silently served company-wide, the rule every sales page keeps.
var ErrBuyerFarmInvalid = errors.New("procurement: buyer analytics farm filter is not one of the parks or all")

// BuyerAnalytics returns the whole page for the filter: one page of buyers plus whole-filter
// totals. limit/offset page the rows only.
func (s *BuyerAnalyticsService) BuyerAnalytics(ctx context.Context, tenantID, farmRaw string, limit, offset int) (domain.BuyerAnalytics, error) {
	return s.BuyerAnalyticsSorted(ctx, tenantID, farmRaw, "", "", limit, offset)
}

// BuyerAnalyticsSorted is BuyerAnalytics ordered by one buyer column over EVERY buyer in the
// filter before the page is sliced ("sort all rows", 2026-09-25); a blank column is newest last
// sale first, and an unknown column or direction is refused.
func (s *BuyerAnalyticsService) BuyerAnalyticsSorted(ctx context.Context, tenantID, farmRaw, sortKey, dir string, limit, offset int) (domain.BuyerAnalytics, error) {
	farms, err := s.repo.ListParkCodes(ctx, tenantID)
	if err != nil {
		return domain.BuyerAnalytics{}, err
	}
	farm, ok := domain.NormalizeBuyerFarmFilter(farmRaw, farms)
	if !ok {
		return domain.BuyerAnalytics{}, ErrBuyerFarmInvalid
	}
	if offset < 0 || offset > domain.MaxBuyerOffset {
		return domain.BuyerAnalytics{}, ErrBuyerOffsetInvalid
	}
	order, err := domain.ParseTableSort(sortKey, dir, domain.BuyerOrderable, domain.BuyerDefaultSort)
	if err != nil {
		return domain.BuyerAnalytics{}, err
	}
	facts, err := s.repo.ClosedBuyerDeals(ctx, tenantID, farm)
	if err != nil {
		return domain.BuyerAnalytics{}, err
	}
	// The clock is the IST business day: "days since last purchase" is a difference of business
	// dates, never of instants.
	asOf := biztime.BusinessDayStart(s.now())
	return domain.BuildBuyerAnalyticsSorted(facts, asOf, order, limit, offset), nil
}

// ErrBuyerOffsetInvalid rejects a negative or absurdly deep page rather than clamping it, so a page
// number never shows the wrong rows.
var ErrBuyerOffsetInvalid = errors.New("procurement: buyer analytics page offset is out of range")

// BuyerAnalyticsHTTPError maps a buyer analytics error onto the transport error shape.
func BuyerAnalyticsHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrBuyerFarmInvalid):
		return &Error{Code: "invalid_farm", Message: "Pick one of your parks, or all farms.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrBuyerOffsetInvalid):
		return &Error{Code: "invalid_offset", Message: "That page is out of range.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, domain.ErrTableSortInvalid):
		return &Error{Code: "invalid_sort", Message: "That column cannot be sorted.", HTTPStatus: http.StatusBadRequest}
	default:
		return Internal("The buyer figures could not be loaded. Try again.")
	}
}
