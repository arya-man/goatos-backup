package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type recordingAnalyticsReader struct {
	seen domain.HealthAnalyticsQuery
	out  domain.HealthAnalytics
	err  error
}

func (r *recordingAnalyticsReader) GetHealthAnalytics(_ context.Context, req domain.HealthAnalyticsQuery) (domain.HealthAnalytics, error) {
	r.seen = req
	return r.out, r.err
}

func fixedClock(t *testing.T, value string) func() time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", value, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse clock %q: %v", value, err)
	}
	return func() time.Time { return parsed }
}

const analyticsTenant = "11111111-1111-4111-8111-111111111111"

// The service resolves the window through the SAME domain function the HTTP
// layer validated it with, so an absent window reaches the repository already
// resolved rather than as two empty strings the SQL would have to guess at.
func TestAnalyticsServiceResolvesTheDefaultWindowBeforeReading(t *testing.T) {
	reader := &recordingAnalyticsReader{}
	svc := NewAnalyticsService(reader).WithClock(fixedClock(t, "2027-03-17"))

	if _, err := svc.GetHealthAnalytics(context.Background(), domain.HealthAnalyticsQuery{TenantID: analyticsTenant}); err != nil {
		t.Fatalf("read: %v", err)
	}
	wantFrom, wantTo := domain.HealthAnalyticsDefaultWindow(fixedClock(t, "2027-03-17")())
	if reader.seen.FromDate != wantFrom || reader.seen.ToDate != wantTo {
		t.Fatalf("window = %q..%q, want the resolved default %q..%q",
			reader.seen.FromDate, reader.seen.ToDate, wantFrom, wantTo)
	}
}

// A blank-but-present park is treated as ABSENT, never as a park whose id is the
// empty string. The latter matches no row, so the page would report a farm with
// no health work at all rather than every park.
func TestAnalyticsServiceTreatsABlankParkAsEveryPark(t *testing.T) {
	blank := "   "
	reader := &recordingAnalyticsReader{}
	svc := NewAnalyticsService(reader).WithClock(fixedClock(t, "2026-09-05"))

	if _, err := svc.GetHealthAnalytics(context.Background(), domain.HealthAnalyticsQuery{
		TenantID: analyticsTenant,
		ParkID:   &blank,
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if reader.seen.ParkID != nil {
		t.Fatalf("park = %q, want nil so every park is read", *reader.seen.ParkID)
	}
}

func TestAnalyticsServiceCarriesARealParkThrough(t *testing.T) {
	park := "  22222222-2222-4222-8222-222222222222 "
	reader := &recordingAnalyticsReader{}
	svc := NewAnalyticsService(reader).WithClock(fixedClock(t, "2026-09-05"))

	if _, err := svc.GetHealthAnalytics(context.Background(), domain.HealthAnalyticsQuery{
		TenantID: analyticsTenant,
		ParkID:   &park,
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if reader.seen.ParkID == nil || *reader.seen.ParkID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("park = %v, want the trimmed id", reader.seen.ParkID)
	}
}

// A malformed window never reaches the repository. Without this the SQL would
// fall back to its own default and serve a window nobody asked for under the
// label the caller chose.
func TestAnalyticsServiceRefusesAMalformedWindow(t *testing.T) {
	reader := &recordingAnalyticsReader{}
	svc := NewAnalyticsService(reader).WithClock(fixedClock(t, "2026-09-05"))

	_, err := svc.GetHealthAnalytics(context.Background(), domain.HealthAnalyticsQuery{
		TenantID: analyticsTenant,
		FromDate: "01-08-2026",
		ToDate:   "2026-08-31",
	})
	if !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("err = %v, want ErrInvalidDate", err)
	}
	if reader.seen.TenantID != "" {
		t.Fatal("the repository was read with a window the service should have refused")
	}
}

func TestAnalyticsServiceRefusesAMissingTenant(t *testing.T) {
	reader := &recordingAnalyticsReader{}
	svc := NewAnalyticsService(reader)

	if _, err := svc.GetHealthAnalytics(context.Background(), domain.HealthAnalyticsQuery{TenantID: "not-a-uuid"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if reader.seen.TenantID != "" {
		t.Fatal("the repository was read without a tenant")
	}
}
