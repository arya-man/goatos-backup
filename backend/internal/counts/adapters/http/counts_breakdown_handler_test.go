package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type countsBreakdownHandlerService struct {
	milkPreparationHandlerService
	seen domain.CountsBreakdownQuery
}

func (f *countsBreakdownHandlerService) GetBreakdown(_ context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error) {
	f.seen = req
	return domain.CountsBreakdown{}, nil
}

// Every filter dimension is repeatable and each occurrence must reach the domain query — a
// second occurrence silently dropped here would render a page that CLAIMS two stages are
// selected while counting only one.
func TestGetBreakdownParsesRepeatedFilterParams(t *testing.T) {
	service := &countsBreakdownHandlerService{}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet,
		"/counts/breakdown?park_id=20000000-0000-4000-8000-000000000001&park_id=20000000-0000-4000-8000-000000000002"+
			"&management_stage=K1&management_stage=Buck&breed=Osmanabadi&sex=male&sex=female"+
			"&pen=30000000-0000-4000-8000-000000000001&pen=30000000-0000-4000-8000-000000000002%23Part%203", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetBreakdown(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got, want := service.seen.ParkIDs, []string{"20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("park_ids=%v want %v", got, want)
	}
	if got, want := service.seen.ManagementStages, []string{"K1", "Buck"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stages=%v want %v", got, want)
	}
	if got, want := service.seen.Breeds, []string{"Osmanabadi"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("breeds=%v want %v", got, want)
	}
	if got, want := service.seen.Sexes, []string{"male", "female"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sexes=%v want %v", got, want)
	}
	wantPens := []domain.CountsBreakdownPen{
		{ShedID: "30000000-0000-4000-8000-000000000001"},
		{ShedID: "30000000-0000-4000-8000-000000000002", PartitionLabel: "Part 3"},
	}
	if !reflect.DeepEqual(service.seen.Pens, wantPens) {
		t.Fatalf("pens=%v want %v", service.seen.Pens, wantPens)
	}
}

// The single-valued form an installed mobile client sends must keep working unchanged: one
// occurrence of each param, plus the legacy shed_id + partition_label pair mapping to one pen.
func TestGetBreakdownKeepsLegacySingleValuedParams(t *testing.T) {
	service := &countsBreakdownHandlerService{}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet,
		"/counts/breakdown?park_id=20000000-0000-4000-8000-000000000001"+
			"&shed_id=30000000-0000-4000-8000-000000000001&partition_label=Part%202"+
			"&management_stage=K1&breed=Beetal&sex=female", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetBreakdown(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if got, want := service.seen.ParkIDs, []string{"20000000-0000-4000-8000-000000000001"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("park_ids=%v want %v", got, want)
	}
	wantPens := []domain.CountsBreakdownPen{{ShedID: "30000000-0000-4000-8000-000000000001", PartitionLabel: "Part 2"}}
	if !reflect.DeepEqual(service.seen.Pens, wantPens) {
		t.Fatalf("pens=%v want %v", service.seen.Pens, wantPens)
	}
	if got, want := service.seen.ManagementStages, []string{"K1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stages=%v want %v", got, want)
	}
	if got, want := service.seen.Breeds, []string{"Beetal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("breeds=%v want %v", got, want)
	}
	if got, want := service.seen.Sexes, []string{"female"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sexes=%v want %v", got, want)
	}
}

// A pen value with no shed half ("#Part 2") is rejected 400, never silently rewritten into a
// filter that matches nothing.
func TestGetBreakdownRejectsMalformedPen(t *testing.T) {
	service := &countsBreakdownHandlerService{}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/breakdown?pen=%23Part%202", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetBreakdown(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
}
