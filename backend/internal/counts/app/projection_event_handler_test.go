package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

func TestProjectionInputHandlerRecomputesBothHorizonsForBaseCount(t *testing.T) {
	recomputer := &fakeProjectionRecomputer{}
	handler := NewProjectionInputHandler(recomputer)
	payload := projectionInputPayload{
		InputKind:             "base_count_anchor",
		ParkID:                "00000000-0000-4000-8000-000000003001",
		CountedAt:             "2026-06-30T06:00:00Z",
		SourceContractVersion: domain.SourceContractVersionV1,
		RecomputeHorizons:     []string{"count_as_of", "feed_target_date"},
	}
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		ID:       "65000000-0000-4000-8000-000000000001",
		Type:     domain.EventBaseCountAnchorRecorded,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Key:      "base-anchor-1",
		Payload:  mustJSON(t, payload),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(recomputer.reqs) != 2 {
		t.Fatalf("recompute reqs=%+v, want 2", recomputer.reqs)
	}
	if recomputer.reqs[0].Horizon != "count_as_of" || recomputer.reqs[1].Horizon != "feed_target_date" {
		t.Fatalf("horizons=%q/%q", recomputer.reqs[0].Horizon, recomputer.reqs[1].Horizon)
	}
	for _, req := range recomputer.reqs {
		if req.ParkID != payload.ParkID || req.TenantID == "" {
			t.Fatalf("req scope=%+v", req)
		}
		if req.AsOf.Format(time.RFC3339) != "2026-06-30T11:30:00+05:30" {
			t.Fatalf("as_of=%s", req.AsOf)
		}
		if req.TargetDate.Format("2006-01-02") != "2026-06-30" {
			t.Fatalf("target_date=%s", req.TargetDate)
		}
		if req.GeneratedBy != defaultProjectionEventGeneratedBy || req.SourceContractVersion != domain.SourceContractVersionV1 {
			t.Fatalf("req provenance=%+v", req)
		}
		if req.TraceID == nil || *req.TraceID != "65000000-0000-4000-8000-000000000001" {
			t.Fatalf("trace_id=%v", req.TraceID)
		}
	}
}

func TestProjectionInputHandlerRecomputesSourceAndDestinationParksForShift(t *testing.T) {
	recomputer := &fakeProjectionRecomputer{}
	handler := NewProjectionInputHandler(recomputer)
	sourcePark := "00000000-0000-4000-8000-000000003001"
	destPark := "00000000-0000-4000-8000-000000003002"
	payload := projectionInputPayload{
		InputKind:         "shifting_event",
		SourceParkID:      sourcePark,
		DestinationParkID: destPark,
		EffectiveAt:       "2026-06-30T13:00:00Z",
		RecomputeHorizons: []string{"feed_target_date"},
	}
	if err := handler.HandleEvent(context.Background(), eventbus.Event{
		ID:       "65000000-0000-4000-8000-000000000002",
		Type:     domain.EventShiftingEventRecorded,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Payload:  mustJSON(t, payload),
	}); err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if len(recomputer.reqs) != 2 {
		t.Fatalf("recompute reqs=%+v, want source and destination park", recomputer.reqs)
	}
	if recomputer.reqs[0].ParkID != sourcePark || recomputer.reqs[1].ParkID != destPark {
		t.Fatalf("parks=%q/%q", recomputer.reqs[0].ParkID, recomputer.reqs[1].ParkID)
	}
	for _, req := range recomputer.reqs {
		if req.Horizon != "feed_target_date" || req.TargetDate.Format("2006-01-02") != "2026-06-30" {
			t.Fatalf("req=%+v", req)
		}
	}
}

func TestProjectionInputHandlerRejectsInvalidHorizon(t *testing.T) {
	err := NewProjectionInputHandler(&fakeProjectionRecomputer{}).HandleEvent(context.Background(), eventbus.Event{
		ID:       "65000000-0000-4000-8000-000000000003",
		Type:     domain.EventBaseCountAnchorRecorded,
		TenantID: "00000000-0000-4000-8000-000000000001",
		Payload:  mustJSON(t, projectionInputPayload{ParkID: "park-1", RecomputeHorizons: []string{"everything"}}),
	})
	if !errors.Is(err, ErrInvalidHorizon) {
		t.Fatalf("err=%v, want ErrInvalidHorizon", err)
	}
}

func TestProjectionInputHandlerRejectsMissingParkScope(t *testing.T) {
	err := NewProjectionInputHandler(&fakeProjectionRecomputer{}).HandleEvent(context.Background(), eventbus.Event{
		ID:         "65000000-0000-4000-8000-000000000004",
		Type:       domain.EventBaseCountAnchorRecorded,
		TenantID:   "00000000-0000-4000-8000-000000000001",
		OccurredAt: time.Date(2026, 6, 30, 6, 0, 0, 0, time.UTC),
		Payload:    mustJSON(t, projectionInputPayload{}),
	})
	if err == nil {
		t.Fatal("expected missing park scope error")
	}
}

type fakeProjectionRecomputer struct {
	reqs []domain.ProjectionRecomputeRequest
}

func (f *fakeProjectionRecomputer) RecomputeProjectionSnapshotWithResult(_ context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionRecomputeResult, error) {
	f.reqs = append(f.reqs, req)
	return domain.ProjectionRecomputeResult{SnapshotID: "snapshot-" + req.Horizon, Horizon: req.Horizon}, nil
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
