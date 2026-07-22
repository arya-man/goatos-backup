package ceoai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/cubeclient"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// fixedNow is a Wednesday (2026-07-22) in IST used to derive deterministic
// business-calendar windows across the time-range parser tests.
var fixedNow = time.Date(2026, 7, 22, 15, 30, 0, 0, biztime.DefaultLocation())

func vaccBinding() metricBinding { return cubeMetricBindings["vaccination_due"] }

func TestTimeDimensionFor_ScopedWindows(t *testing.T) {
	b := vaccBinding()
	cases := []struct {
		name      string
		in        string
		wantFrom  string
		wantTo    string
		wantGrain string
	}{
		{"this month", "this month", "2026-07-01", "2026-07-31", ""},
		{"last month", "last month", "2026-06-01", "2026-06-30", ""},
		{"this week (Mon-start)", "this week", "2026-07-20", "2026-07-26", ""},
		{"last week", "last week", "2026-07-13", "2026-07-19", ""},
		{"today", "today", "2026-07-22", "2026-07-22", ""},
		{"yesterday", "yesterday", "2026-07-21", "2026-07-21", ""},
		{"last 7 days", "last 7 days", "2026-07-16", "2026-07-22", ""},
		{"last 3 months", "last 3 months", "2026-04-23", "2026-07-22", ""},
		{"fixed range dotdot", "2026-07-01..2026-07-10", "2026-07-01", "2026-07-10", ""},
		{"fixed range to", "2026-07-01 to 2026-07-10", "2026-07-01", "2026-07-10", ""},
		{"single iso day", "2026-07-05", "2026-07-05", "2026-07-05", ""},
		{"trend by month", "mortality trend by month", "2024-08-01", "2026-07-31", "month"},
		{"bare grain month", "month", "2024-08-01", "2026-07-31", "month"},
		{"bare grain week", "week", "", "", "week"}, // window checked below by grain only
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			td, ok := timeDimensionFor(b, tc.in, fixedNow)
			if !ok {
				t.Fatalf("expected a time dimension for %q", tc.in)
			}
			if td.Dimension != b.view+"."+b.timeDim {
				t.Fatalf("wrong member: %s", td.Dimension)
			}
			if len(td.DateRange) != 2 {
				t.Fatalf("expected [from,to], got %v", td.DateRange)
			}
			if td.Granularity != tc.wantGrain {
				t.Fatalf("granularity: got %q want %q", td.Granularity, tc.wantGrain)
			}
			if tc.wantFrom != "" && td.DateRange[0] != tc.wantFrom {
				t.Fatalf("from: got %s want %s", td.DateRange[0], tc.wantFrom)
			}
			if tc.wantTo != "" && td.DateRange[1] != tc.wantTo {
				t.Fatalf("to: got %s want %s", td.DateRange[1], tc.wantTo)
			}
		})
	}
}

func TestTimeDimensionFor_DropsUngroundable(t *testing.T) {
	b := vaccBinding()
	for _, in := range []string{"", "   ", "sometime soon", "when convenient", "q3ish"} {
		if _, ok := timeDimensionFor(b, in, fixedNow); ok {
			t.Fatalf("expected no time dimension for %q", in)
		}
	}
}

func TestTimeDimensionFor_NoTimeMemberBinding(t *testing.T) {
	// A binding without a business-day member can never be time-scoped.
	b := metricBinding{view: "kpi_x", measure: "m", timeDim: ""}
	if _, ok := timeDimensionFor(b, "this month", fixedNow); ok {
		t.Fatal("expected no time dimension when binding has no timeDim")
	}
}

// TestCubeQuery_EmitsTimeDimensionOverWire proves the real production path:
// cubeMetricService.Query must serialize a Cube timeDimension when the planner
// supplies a time_range, and must NOT when it is absent. Regression guard for
// the "TimeRange silently dropped -> all-time value labelled as scoped" bug.
func TestCubeQuery_EmitsTimeDimensionOverWire(t *testing.T) {
	var captured struct {
		Query cubeclient.Query `json:"query"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"kpi_vaccination.vaccination_due":"836"}]}`))
	}))
	defer srv.Close()

	client, err := cubeclient.New(cubeclient.Config{BaseURL: srv.URL, APISecret: "test-secret-32-bytes-minimum-xxxxxx"})
	if err != nil {
		t.Fatalf("cube client: %v", err)
	}
	svc := &cubeMetricService{client: client}
	actor := domain.Actor{TenantID: "11111111-1111-1111-1111-111111111111"}

	t.Run("time_range present -> timeDimension on wire", func(t *testing.T) {
		if _, err := svc.Query(context.Background(), actor, ports.MetricQuery{
			Metric: "vaccination_due", TimeRange: "this month",
		}); err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(captured.Query.TimeDimensions) != 1 {
			t.Fatalf("expected 1 timeDimension on the wire, got %+v", captured.Query.TimeDimensions)
		}
		td := captured.Query.TimeDimensions[0]
		if td.Dimension != "kpi_vaccination.due_business_day" {
			t.Fatalf("wrong time member: %s", td.Dimension)
		}
		if len(td.DateRange) != 2 || !strings.HasPrefix(td.DateRange[0], "20") {
			t.Fatalf("expected [from,to] date range, got %v", td.DateRange)
		}
	})

	t.Run("no time_range -> no timeDimension", func(t *testing.T) {
		captured.Query = cubeclient.Query{}
		if _, err := svc.Query(context.Background(), actor, ports.MetricQuery{
			Metric: "vaccination_due",
		}); err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(captured.Query.TimeDimensions) != 0 {
			t.Fatalf("expected no timeDimension without a time_range, got %+v", captured.Query.TimeDimensions)
		}
	})

	// Census counts are all-time: active_animal_count filters lifecycle_status
	// ='alive', whose exit_business_day is NULL, so windowing on exit_business_day
	// would zero out the living herd. Even WITH a time_range, census must NOT emit
	// a timeDimension. Regression guard for the "active animals last month -> ~0"
	// mislabelled-census bug.
	for _, metric := range []string{"active_animals", "total_animals"} {
		t.Run("census "+metric+" ignores time_range (no timeDimension)", func(t *testing.T) {
			captured.Query = cubeclient.Query{}
			if _, err := svc.Query(context.Background(), actor, ports.MetricQuery{
				Metric: metric, TimeRange: "last month",
			}); err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(captured.Query.TimeDimensions) != 0 {
				t.Fatalf("census metric %q must be all-time; got timeDimension %+v", metric, captured.Query.TimeDimensions)
			}
		})
	}
}

// TestCubeMetrics_CensusHasNoTimeGrains proves the planner catalog does not
// advertise TimeGrains for census metrics, so a planner cannot try to time-scope
// them onto the NULL-for-living exit_business_day member. Time-bound metrics keep
// their grains.
func TestCubeMetrics_CensusHasNoTimeGrains(t *testing.T) {
	svc := &cubeMetricService{}
	specs, err := svc.Metrics(context.Background())
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	byName := map[string][]string{}
	for _, s := range specs {
		byName[s.Name] = s.TimeGrains
	}
	for _, census := range []string{"active_animals", "total_animals"} {
		if g := byName[census]; len(g) != 0 {
			t.Fatalf("census metric %q must advertise no TimeGrains, got %v", census, g)
		}
	}
	// A time-bound metric still advertises grains.
	if g := byName["vaccination_due"]; len(g) == 0 {
		t.Fatal("vaccination_due should still advertise TimeGrains")
	}
}
