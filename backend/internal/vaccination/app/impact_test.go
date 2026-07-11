package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestImpactPreviewReadsRollupNotGoats is the regression guard: the config impact preview must read the
// eligibility rollup read model and NEVER scan goats on the request path. The fake flags any live
// goat-count call; this test fails if a future edit reintroduces one.
func TestImpactPreviewReadsRollupNotGoats(t *testing.T) {
	repo := newCompletionRepoFake()
	repo.rollupAgg = domain.EligibilityRollupAggregate{EligibleAnimals: 250, AffectedSheds: 4, SourceRevision: 1700000000000}
	repo.dailyCap = 100
	svc := NewService(repo)

	out, err := svc.ImpactPreview(context.Background(), domain.ImpactRequest{
		Filter:   domain.ImpactFilter{TenantID: "tenant-1", Species: "goat"},
		DoseRows: 2,
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if repo.goatScanned {
		t.Fatalf("impact preview scanned goats; it must read the eligibility rollup read model only")
	}
	if out.EligibleAnimals != 250 {
		t.Fatalf("eligible_animals: want 250, got %d", out.EligibleAnimals)
	}
	if out.VaccinationCells != 500 { // 250 × 2 dose rows
		t.Fatalf("vaccination_cells: want 500, got %d", out.VaccinationCells)
	}
	if out.AffectedSheds != 4 {
		t.Fatalf("affected_sheds: want 4, got %d", out.AffectedSheds)
	}
	if out.DailyCap != 100 {
		t.Fatalf("daily_cap: want 100, got %d", out.DailyCap)
	}
	if out.EstimatedDays != 5 { // ceil(500 / 100)
		t.Fatalf("estimated_days: want 5, got %d", out.EstimatedDays)
	}
	if out.SourceRevision != 1700000000000 {
		t.Fatalf("source_revision: want 1700000000000, got %d", out.SourceRevision)
	}
}

// TestImpactPreviewEstimatedDaysCeilsAndDefaultsDoseRows proves estimated_days uses ceil integer math and
// dose_rows defaults to 1 when unset/zero.
func TestImpactPreviewEstimatedDaysCeilsAndDefaultsDoseRows(t *testing.T) {
	repo := newCompletionRepoFake()
	repo.rollupAgg = domain.EligibilityRollupAggregate{EligibleAnimals: 101, AffectedSheds: 2, SourceRevision: 1}
	repo.dailyCap = 100
	svc := NewService(repo)

	out, err := svc.ImpactPreview(context.Background(), domain.ImpactRequest{
		Filter: domain.ImpactFilter{TenantID: "tenant-1"},
		// DoseRows omitted -> defaults to 1.
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if out.VaccinationCells != 101 {
		t.Fatalf("vaccination_cells: want 101 (dose_rows defaults to 1), got %d", out.VaccinationCells)
	}
	if out.EstimatedDays != 2 { // ceil(101 / 100) = 2, not 1
		t.Fatalf("estimated_days: want 2 (ceil), got %d", out.EstimatedDays)
	}
}

// TestImpactPreviewEmptyRollupWarns proves an un-recomputed scope (source_revision 0) surfaces the
// "run recompute" warning instead of silently showing zeros as if real.
func TestImpactPreviewEmptyRollupWarns(t *testing.T) {
	repo := newCompletionRepoFake()
	repo.rollupAgg = domain.EligibilityRollupAggregate{} // source_revision 0
	svc := NewService(repo)

	out, err := svc.ImpactPreview(context.Background(), domain.ImpactRequest{
		Filter: domain.ImpactFilter{TenantID: "tenant-1"},
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "rollup has no data") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected empty-rollup warning, got %v", out.Warnings)
	}
}

func TestImpactPreviewExpiryWarningUsesNormalizedFilterAsOf(t *testing.T) {
	repo := newCompletionRepoFake()
	repo.rollupAgg = domain.EligibilityRollupAggregate{EligibleAnimals: 3, AffectedSheds: 1, SourceRevision: 1}
	expiry := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	repo.stockAvailable = "100"
	repo.stockExpiry = &expiry
	itemID := "item-1"
	svc := NewService(repo)

	out, err := svc.ImpactPreview(context.Background(), domain.ImpactRequest{
		Filter:        domain.ImpactFilter{TenantID: "tenant-1", AsOf: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		VaccineItemID: &itemID,
		DoseRows:      1,
		HorizonDays:   3,
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	for _, warning := range out.Warnings {
		if strings.Contains(warning, "stock expiry before horizon") {
			t.Fatalf("expiry warning used wall-clock instead of filter as_of: warnings=%v", out.Warnings)
		}
	}
}
