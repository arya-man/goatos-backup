package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

func TestProcessIntegrityCountCacheKeyIgnoresPageShape(t *testing.T) {
	asOf := time.Date(2026, 8, 24, 10, 4, 30, 0, time.UTC)
	dueBefore := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	category := domain.CategoryVaccination
	rowID := "row-a"
	cursor := &domain.Cursor{SortPriority: 3, DueAt: asOf, RowID: "row-cursor"}
	q := domain.Query{
		TenantID:                "00000000-0000-4000-8000-000000000001",
		Category:                &category,
		AsOf:                    asOf,
		DueBefore:               dueBefore,
		Limit:                   500,
		RowID:                   &rowID,
		Cursor:                  cursor,
		IncludeAdherenceSummary: true,
		IncludeCompleted:        true,
		OnlyBrokenOrAtRisk:      true,
		ScopeLatestDrive:        true,
	}

	sameCounts := q
	sameCounts.Limit = 1
	sameCounts.RowID = nil
	sameCounts.Cursor = nil
	sameCounts.IncludeAdherenceSummary = false

	if got, want := processIntegrityCountCacheKey(q), processIntegrityCountCacheKey(sameCounts); got != want {
		t.Fatalf("count cache key depends on page-only inputs:\n got %s\nwant %s", got, want)
	}
}
