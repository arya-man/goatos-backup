package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type staleCalendarService struct{}

func (staleCalendarService) ListEvents(context.Context, domain.Query) (domain.CalendarEventListResponse, error) {
	return domain.CalendarEventListResponse{}, app.Unavailable("projection_stale", "calendar projection is stale; retry after refresh")
}
func (staleCalendarService) GetEventDetail(context.Context, domain.EventQuery) (domain.CalendarEventDetail, error) {
	return domain.CalendarEventDetail{}, nil
}
func (staleCalendarService) ListDriveTargets(context.Context, domain.DriveTargetQuery) (domain.CalendarDriveTargetListResponse, error) {
	return domain.CalendarDriveTargetListResponse{}, nil
}
func (staleCalendarService) History(context.Context, domain.HistoryQuery) (domain.CalendarHistoryResponse, error) {
	return domain.CalendarHistoryResponse{}, nil
}
func (staleCalendarService) SendNudge(context.Context, ports.SendNudge) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}
func (staleCalendarService) Snooze(context.Context, ports.Snooze) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}
func (staleCalendarService) AcknowledgeEscalation(context.Context, ports.AcknowledgeEscalation) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}
func (staleCalendarService) ResolveEscalation(context.Context, ports.ResolveEscalation) (domain.CalendarActionResponse, error) {
	return domain.CalendarActionResponse{}, nil
}

func TestCalendarProjectionStaleReturnsTyped503(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(staleCalendarService{}))
	req := httptest.NewRequest(http.MethodGet, "/calendar/vaccination/events", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "projection_stale" {
		t.Fatalf("code=%q, want projection_stale", body.Code)
	}
}
