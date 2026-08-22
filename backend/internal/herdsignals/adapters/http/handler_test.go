package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeService struct {
	ingestResp domain.IngestResponse
	ingestErr  error
	ingestGot  domain.IngestRequest

	liveResp domain.LiveResponse
	liveErr  error

	timelineResp domain.TimelineResponse
	timelineErr  error
	timelineGot  struct {
		tagID, from, to string
		bucketSeconds   int
	}

	gatewaysResp domain.GatewaysResponse
	gatewaysErr  error

	insightsResp domain.InsightsResponse
	insightsErr  error
}

func (f *fakeService) IngestPackets(_ context.Context, _ domain.Actor, req domain.IngestRequest) (domain.IngestResponse, error) {
	f.ingestGot = req
	return f.ingestResp, f.ingestErr
}

func (f *fakeService) ListLive(_ context.Context, _ domain.Actor, _, _, _ *string, _ *bool, _ string, _ int) (domain.LiveResponse, error) {
	return f.liveResp, f.liveErr
}

func (f *fakeService) GetTimeline(_ context.Context, _ domain.Actor, tagID, from, to string, bucketSeconds int) (domain.TimelineResponse, error) {
	f.timelineGot.tagID, f.timelineGot.from, f.timelineGot.to, f.timelineGot.bucketSeconds = tagID, from, to, bucketSeconds
	return f.timelineResp, f.timelineErr
}

func (f *fakeService) ListGateways(_ context.Context, _ domain.Actor) (domain.GatewaysResponse, error) {
	return f.gatewaysResp, f.gatewaysErr
}

func (f *fakeService) GetInsights(_ context.Context, _ domain.Actor) (domain.InsightsResponse, error) {
	return f.insightsResp, f.insightsErr
}

func authedRequest(method, target string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx := httpmiddleware.WithTenantID(req.Context(), "00000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithActorID(ctx, "10000000-0000-4000-8000-000000000001")
	return req.WithContext(ctx)
}

func TestIngestPacketsRejectsUnauthenticated(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	req := httptest.NewRequest("POST", "/herd-signals/packets", bytes.NewReader([]byte(`{"gateway_id":"gw1","gateway_seen_at":"2026-01-01T00:00:00Z","packets":[]}`)))
	w := httptest.NewRecorder()

	h.IngestPackets(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestIngestPacketsRejectsUnknownFields(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	req := authedRequest("POST", "/herd-signals/packets", []byte(`{
		"gateway_id":"gw1",
		"gateway_seen_at":"2026-01-01T00:00:00Z",
		"packets":[],
		"unexpected_field": true
	}`))
	w := httptest.NewRecorder()

	h.IngestPackets(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (strict decode must reject unknown fields)", w.Code, http.StatusBadRequest)
	}
}

func TestIngestPacketsRejectsMalformedJSON(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	req := authedRequest("POST", "/herd-signals/packets", []byte(`{not valid json`))
	w := httptest.NewRecorder()

	h.IngestPackets(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestIngestPacketsAcceptsValidRequestAndForwardsToService(t *testing.T) {
	svc := &fakeService{ingestResp: domain.IngestResponse{Accepted: 1, Stored: 1, LatestUpdated: 1}}
	h := NewHandler(svc)
	req := authedRequest("POST", "/herd-signals/packets", []byte(`{
		"gateway_id":"gw1",
		"gateway_seen_at":"2026-01-01T00:00:00Z",
		"packets":[{"tag_id":"tag-1","tag_mac":"aa:bb:cc:dd:ee:ff","seen_at":"2026-01-01T00:00:00Z"}]
	}`))
	w := httptest.NewRecorder()

	h.IngestPackets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if svc.ingestGot.GatewayID != "gw1" {
		t.Errorf("gateway_id forwarded = %q, want gw1", svc.ingestGot.GatewayID)
	}
	if len(svc.ingestGot.Packets) != 1 || svc.ingestGot.Packets[0].TagID != "tag-1" {
		t.Errorf("packets forwarded = %+v, want one packet with tag_id tag-1", svc.ingestGot.Packets)
	}
}

func TestListLiveRejectsUnauthenticated(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	req := httptest.NewRequest("GET", "/herd-signals/live", nil)
	w := httptest.NewRecorder()

	h.ListLive(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestGetTimelineRequiresFromAndTo(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	mux := http.NewServeMux()
	Register(mux, h)

	req := authedRequest("GET", "/herd-signals/tags/tag-1/timeline", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestGetTimelineForwardsBucketSecondsZeroByDefault(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc)
	mux := http.NewServeMux()
	Register(mux, h)

	req := authedRequest("GET", "/herd-signals/tags/tag-1/timeline?from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	// bucket_seconds not supplied: handler must forward 0 so the service selects the tier
	// (defect fix -- it used to hardcode 60 regardless of range).
	if svc.timelineGot.bucketSeconds != 0 {
		t.Errorf("bucketSeconds forwarded = %d, want 0 (let service select tier)", svc.timelineGot.bucketSeconds)
	}
}
