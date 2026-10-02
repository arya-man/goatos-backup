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

// SalesExecutiveAnalyticsService serves the Sales > Sales executive analytics page: who on the
// sales desk added or changed vendors, recorded calls, sales and payments, per person and per day.
//
// Thin like BuyerAnalyticsService: the repository resolves the activities, the domain groups them,
// and the service only validates the period and pins the clock.
type SalesExecutiveAnalyticsService struct {
	repo ports.SalesExecutiveAnalyticsRepository
	now  func() time.Time
}

func NewSalesExecutiveAnalyticsService(repo ports.SalesExecutiveAnalyticsRepository) *SalesExecutiveAnalyticsService {
	return &SalesExecutiveAnalyticsService{repo: repo, now: time.Now}
}

// WithClock pins "today". Tests only.
func (s *SalesExecutiveAnalyticsService) WithClock(now func() time.Time) *SalesExecutiveAnalyticsService {
	s.now = now
	return s
}

// ErrSalesExecutiveDaysInvalid is a period the page does not offer. Refused rather than clamped,
// so a typed URL never shows a window nobody chose.
var ErrSalesExecutiveDaysInvalid = errors.New("procurement: sales executive period is not one of the offered periods")

// ErrSalesExecutiveOffsetInvalid is a negative or absurdly deep page of either list.
var ErrSalesExecutiveOffsetInvalid = errors.New("procurement: sales executive page offset is out of range")

// SalesExecutiveAnalytics returns the page for a period of days business days ending today
// (0 = the default period).
func (s *SalesExecutiveAnalyticsService) SalesExecutiveAnalytics(ctx context.Context, tenantID string, days int, page domain.SalesExecutivePage) (domain.SalesExecutiveAnalytics, error) {
	if days == 0 {
		days = domain.SalesExecutiveDefaultDays
	}
	if !domain.ValidSalesExecutiveDays(days) {
		return domain.SalesExecutiveAnalytics{}, ErrSalesExecutiveDaysInvalid
	}
	if !page.Valid() {
		return domain.SalesExecutiveAnalytics{}, ErrSalesExecutiveOffsetInvalid
	}
	// The period is IST business days, never instants: today's business-day start, and the
	// period before is read too so the page can say whether the flow rose or fell.
	today := biztime.BusinessDayStart(s.now())
	_, prevFrom, _ := domain.SalesExecutivePeriod(today, days)

	facts, err := s.repo.SalesActivities(ctx, tenantID, prevFrom)
	if err != nil {
		return domain.SalesExecutiveAnalytics{}, err
	}
	latest, err := s.repo.LatestVendors(ctx, tenantID, domain.SalesExecutivePageSize, page.VendorOffset)
	if err != nil {
		return domain.SalesExecutiveAnalytics{}, err
	}
	total, imported, err := s.repo.VendorRegisterTotals(ctx, tenantID)
	if err != nil {
		return domain.SalesExecutiveAnalytics{}, err
	}
	return domain.BuildSalesExecutiveAnalytics(facts, latest, total, imported, days, today, page), nil
}

// SalesExecutiveAnalyticsHTTPError maps a sales executive analytics error onto the transport shape.
func SalesExecutiveAnalyticsHTTPError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrSalesExecutiveDaysInvalid):
		return &Error{Code: "invalid_days", Message: "Pick one of the offered periods.", HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrSalesExecutiveOffsetInvalid):
		return &Error{Code: "invalid_offset", Message: "That page is out of range.", HTTPStatus: http.StatusBadRequest}
	default:
		return Internal("The sales desk figures could not be loaded. Try again.")
	}
}
