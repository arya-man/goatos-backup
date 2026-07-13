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

// historyFlaggedCalendarService returns a completed-history list whose serving history projection is
// stale AND partially covers the requested window, so the response must carry those flags on the wire.
type historyFlaggedCalendarService struct{ staleCalendarService }

func (historyFlaggedCalendarService) ListEvents(context.Context, domain.Query) (domain.CalendarEventListResponse, error) {
	return domain.CalendarEventListResponse{
		Source: domain.SourceAPI,
		Items:  []domain.CalendarEvent{},
		HistoryProjection: &domain.ProjectionMetadata{
			ProjectionVersion: 42,
			FreshnessStatus:   "yellow",
			ServingState:      "stale",
			Stale:             true,
			PartialCoverage:   true,
		},
	}, nil
}

// C5-002: the stale/partial-coverage history safety signal must be in the API CONTRACT, not just the
// Go domain type. Consumers act on `history_projection.stale` / `.partial_coverage` in the JSON body;
// prove they are actually serialized on the wire (they were absent from OpenAPI + never asserted).
func TestCalendarHistoryProjectionMetadataOnWire(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(historyFlaggedCalendarService{}))
	req := httptest.NewRequest(http.MethodGet, "/calendar/vaccination/events?status=completed", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		HistoryProjection *struct {
			Stale           bool   `json:"stale"`
			PartialCoverage bool   `json:"partial_coverage"`
			ServingState    string `json:"serving_state"`
		} `json:"history_projection"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.HistoryProjection == nil {
		t.Fatalf("history_projection missing from response body: %s", rec.Body.String())
	}
	if !body.HistoryProjection.Stale || !body.HistoryProjection.PartialCoverage || body.HistoryProjection.ServingState != "stale" {
		t.Fatalf("history_projection=%+v, want stale=true partial_coverage=true serving_state=stale", *body.HistoryProjection)
	}
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
