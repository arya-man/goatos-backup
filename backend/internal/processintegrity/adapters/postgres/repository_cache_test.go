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

func TestProcessIntegrityReadCacheReturnsDefensiveCopy(t *testing.T) {
	r := NewRepository(nil, 0)
	key := "list|tenant|bucket"
	result := domain.ListResult{
		Rows: []domain.Row{{
			RowID: "row-1",
			Evidence: domain.Evidence{
				ProofIDs: []string{"proof-1"},
			},
		}},
		CountsByWorkState: []domain.CountByWorkState{{WorkState: domain.WorkStateDue, Count: 1}},
	}

	r.setReadCache(key, result)
	result.Rows[0].Evidence.ProofIDs[0] = "mutated-before-read"
	result.CountsByWorkState[0].Count = 99

	first, ok := r.getReadCache(key)
	if !ok {
		t.Fatal("expected read cache hit")
	}
	first.Rows[0].Evidence.ProofIDs[0] = "mutated-after-read"
	first.Rows[0].Evidence.Media = []domain.MediaItem{{ProofID: "proof-1", DownloadURL: "stale-url"}}
	first.CountsByWorkState[0].Count = 42

	second, ok := r.getReadCache(key)
	if !ok {
		t.Fatal("expected second read cache hit")
	}
	if got := second.Rows[0].Evidence.ProofIDs[0]; got != "proof-1" {
		t.Fatalf("cached proof IDs were mutated through caller-owned slice: got %q", got)
	}
	if len(second.Rows[0].Evidence.Media) != 0 {
		t.Fatalf("cached media was mutated through caller-owned rows: got %+v", second.Rows[0].Evidence.Media)
	}
	if got := second.CountsByWorkState[0].Count; got != 1 {
		t.Fatalf("cached counts were mutated through caller-owned slice: got %d", got)
	}
}
