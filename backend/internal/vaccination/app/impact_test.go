package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestImpactPreviewExpiryWarningUsesNormalizedFilterAsOf(t *testing.T) {
	repo := newCompletionRepoFake()
	expiry := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	repo.stockAvailable = "100"
	repo.stockExpiry = &expiry
	itemID := "item-1"
	svc := NewService(repo)

	out, err := svc.ImpactPreview(context.Background(), domain.ImpactRequest{
		Filter:        domain.ImpactFilter{TenantID: "tenant-1", AsOf: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)},
		VaccineItemID: &itemID,
		DosesPerGoat:  1,
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
