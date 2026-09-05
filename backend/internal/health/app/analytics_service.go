package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
)

// AnalyticsService serves the Health Analytics leadership read.
//
// It is deliberately thin: the WINDOW rules are pure calendar logic in
// health/domain, and the aggregation is SQL in the repository. What lives here
// is the boundary work neither of those should do — trimming, the park scope,
// and resolving the window with the SAME functions the HTTP layer validated it
// with so the two cannot drift on what counts as a day.
type AnalyticsService struct {
	reader ports.AnalyticsReader
	now    func() time.Time
}

func NewAnalyticsService(reader ports.AnalyticsReader) *AnalyticsService {
	return &AnalyticsService{reader: reader, now: time.Now}
}

// WithClock pins the clock, so a test can assert a window without racing the
// wall clock across midnight IST.
func (s *AnalyticsService) WithClock(now func() time.Time) *AnalyticsService {
	if now != nil {
		s.now = now
	}
	return s
}

// GetHealthAnalytics resolves the window and scope, then reads.
//
// An EMPTY park means every park, which is what the top bar sends when the
// reader is scoped to the whole tenant. A blank-but-present park id is treated
// as absent rather than as a park whose id is the empty string — the latter
// would silently return nothing and read as a farm with no health work.
func (s *AnalyticsService) GetHealthAnalytics(ctx context.Context, req domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error) {
	req.TenantID = strings.TrimSpace(req.TenantID)
	if !validUUID(req.TenantID) {
		return domain.HealthAnalytics{}, ErrInvalidInput
	}
	if req.ParkID != nil {
		park := strings.TrimSpace(*req.ParkID)
		if park == "" {
			req.ParkID = nil
		} else {
			req.ParkID = &park
		}
	}

	from, to, err := domain.ResolveHealthAnalyticsWindow(req.FromDate, req.ToDate, s.now())
	if err != nil {
		return domain.HealthAnalytics{}, ErrInvalidDate
	}
	req.FromDate = from
	req.ToDate = to

	return s.reader.GetHealthAnalytics(ctx, req)
}
