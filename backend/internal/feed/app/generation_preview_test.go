package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/feed/domain"
)

type fakeCountsProjectionProvider struct {
	got countsdomain.CountProjectionRequest
	out countsdomain.CountProjection
	err error
}

func (f *fakeCountsProjectionProvider) ProjectedCountFor(_ context.Context, in countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error) {
	f.got = in
	return f.out, f.err
}

func TestGenerationPreviewConsumesProjectedCountsAndBlocksPregnantDestinationRisk(t *testing.T) {
	parkID := "10000000-0000-4000-8000-000000000001"
	shedID := "20000000-0000-4000-8000-000000000001"
	next := "next-page"
	rowBlocker := "pregnant destination shed ration context unresolved; shortage can cause abortion risk"
	provider := &fakeCountsProjectionProvider{out: countsdomain.CountProjection{
		TenantID:              "tenant-1",
		ParkID:                parkID,
		TargetDate:            time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		SnapshotID:            "33000000-0000-4000-8000-000000000001",
		ProjectionStatus:      "ready",
		SourceContractVersion: countsdomain.SourceContractVersionV1,
		SourceHash:            "source-hash",
		BaseAnchorIDsHash:     "base-hash",
		ShiftingEventIDsHash:  "shift-hash",
		ExceptionCount:        1,
		TotalRowCount:         1,
		Rows: []countsdomain.ProjectionRow{{
			ProjectionRowID:              "44000000-0000-4000-8000-000000000001",
			ParkID:                       parkID,
			ShedID:                       shedID,
			TargetDate:                   time.Date(2026, 7, 1, 8, 30, 0, 0, time.UTC),
			GrainKey:                     shedID + ":f1:pregnant",
			BreedKey:                     "f1",
			BreedLabel:                   "F1",
			StageTag:                     strPtr("pregnant"),
			AgeClass:                     strPtr("adult"),
			Sex:                          strPtr("female"),
			HeadCount:                    18,
			PregnantCount:                12,
			WarmupCount:                  2,
			RationContextResolutionState: "blocked",
			BlockerReason:                &rowBlocker,
			SourceRowHash:                "row-hash",
		}},
		ShedBreedTotals: []countsdomain.ProjectionShedBreedTotal{{
			ParkID: parkID, ShedID: shedID, BreedKey: "f1", BreedLabel: "F1",
			HeadCount: 18, PregnantCount: 12, WarmupCount: 2,
			RationContextResolutionState: "blocked",
		}},
		Blockers: []countsdomain.ProjectionBlocker{{
			ExceptionType: "destination_shortage",
			SourceKey:     "shift:priority-pregnant-1",
			GrainKey:      shedID + ":f1:pregnant",
			Severity:      "critical",
			BlockerReason: "destination shed does not have enough reviewed feed for pregnant animals",
		}},
		NextCursor: &next,
	}}

	shedFilter := " " + shedID + " "
	breedFilter := " F1 "
	out, err := NewService(nil).
		WithCountsProjectionProvider(provider).
		GenerationPreview(context.Background(), domain.GenerationPreviewQuery{
			TenantID:   " tenant-1 ",
			ParkID:     parkID,
			TargetDate: time.Date(2026, 7, 1, 18, 45, 0, 0, time.FixedZone("IST", 5*60*60+30*60)),
			ShedID:     &shedFilter,
			BreedKey:   &breedFilter,
			Limit:      25,
		})
	if err != nil {
		t.Fatalf("GenerationPreview err=%v", err)
	}
	if provider.got.TenantID != "tenant-1" || provider.got.ParkID != parkID ||
		provider.got.ShedID == nil || *provider.got.ShedID != shedID ||
		provider.got.BreedKey == nil || *provider.got.BreedKey != "f1" ||
		provider.got.Limit != 25 ||
		!provider.got.TargetDate.Equal(dateOnly(time.Date(2026, 7, 1, 18, 45, 0, 0, time.FixedZone("IST", 5*60*60+30*60)))) {
		t.Fatalf("projection request=%+v", provider.got)
	}
	if out.Status != domain.ReadinessBlocked || out.GenerationAllowed {
		t.Fatalf("preview status=%q allowed=%t, want blocked/false", out.Status, out.GenerationAllowed)
	}
	if out.NextCursor == nil || *out.NextCursor != next {
		t.Fatalf("next cursor=%v, want %q", out.NextCursor, next)
	}
	if len(out.Rows) != 1 || out.Rows[0].PregnantCount != 12 || out.Rows[0].WarmupCount != 2 ||
		out.Rows[0].StageTag == nil || *out.Rows[0].StageTag != "pregnant" {
		t.Fatalf("rows=%+v", out.Rows)
	}
	if len(out.ShedBreedTotals) != 1 || out.ShedBreedTotals[0].PregnantCount != 12 {
		t.Fatalf("totals=%+v", out.ShedBreedTotals)
	}
	if !hasGenerationBlocker(out.Blockers, "destination_shortage") ||
		!hasGenerationBlocker(out.Blockers, "ration_context_unresolved") ||
		!strings.Contains(out.BlockerReason, "pregnant") {
		t.Fatalf("blockers=%+v reason=%q", out.Blockers, out.BlockerReason)
	}
}

func TestGenerationPreviewFailsClosedWithoutProjectionProvider(t *testing.T) {
	_, err := NewService(nil).GenerationPreview(context.Background(), domain.GenerationPreviewQuery{
		TenantID:   "tenant-1",
		ParkID:     "10000000-0000-4000-8000-000000000001",
		TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrCountsProjectionUnavailable) {
		t.Fatalf("err=%v, want ErrCountsProjectionUnavailable", err)
	}
}

func TestGenerationPreviewCleanProjectionStillDoesNotAllowGeneration(t *testing.T) {
	parkID := "10000000-0000-4000-8000-000000000001"
	shedID := "20000000-0000-4000-8000-000000000001"
	provider := &fakeCountsProjectionProvider{out: countsdomain.CountProjection{
		TenantID: "tenant-1", ParkID: parkID, TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		SnapshotID: "33000000-0000-4000-8000-000000000001", ProjectionStatus: "ready",
		Rows: []countsdomain.ProjectionRow{{
			ProjectionRowID: "44000000-0000-4000-8000-000000000001",
			ParkID:          parkID, ShedID: shedID, TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			GrainKey: shedID + ":beetal", BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: 20, RationContextResolutionState: "resolved",
		}},
		ShedBreedTotals: []countsdomain.ProjectionShedBreedTotal{{
			ParkID: parkID, ShedID: shedID, BreedKey: "beetal", BreedLabel: "Beetal",
			HeadCount: 20, RationContextResolutionState: "resolved",
		}},
	}}

	out, err := NewService(nil).
		WithCountsProjectionProvider(provider).
		GenerationPreview(context.Background(), domain.GenerationPreviewQuery{
			TenantID: "tenant-1", ParkID: parkID, TargetDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		})
	if err != nil {
		t.Fatalf("GenerationPreview err=%v", err)
	}
	if out.Status != domain.ReadinessPending || out.GenerationAllowed {
		t.Fatalf("preview status=%q allowed=%t, want pending/false", out.Status, out.GenerationAllowed)
	}
	if len(out.Blockers) != 0 {
		t.Fatalf("blockers=%+v, want none", out.Blockers)
	}
	if !strings.Contains(out.BlockerReason, "G3-G17") {
		t.Fatalf("blocker reason=%q", out.BlockerReason)
	}
}

func hasGenerationBlocker(blockers []domain.GenerationPreviewBlocker, blockerType string) bool {
	for _, blocker := range blockers {
		if blocker.Type == blockerType {
			return true
		}
	}
	return false
}
