package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// PARK ORDER IS THE PARK CODE, CBE THEN CPT (maintainer decision 2026-09-16). The All-parks
// reads used to ORDER BY park_label, which is the park's full NAME -- and "Channapatna" (CPT)
// sorts ahead of "Coimbatore" (CBE), so every two-park table opened on CPT. The fixture seeds
// the parks with code-as-name; this test gives them their real names so a name-keyed order
// would put CPT first, then asserts CBE leads on every read that lists both parks.
func TestAllParksReadsListCBEBeforeCPTByCodeNotName(t *testing.T) {
	ctx := context.Background()
	repo, target := completionFixture(t, ctx)
	// Both the live register name and the sheet's own snapshot label, because the completion
	// table prefers the live name while the feed-by-pen read renders the snapshot.
	for _, row := range [][2]string{{fdiPark, "Coimbatore"}, {fdcParkB, "Channapatna"}} {
		if _, err := repo.pool.Exec(ctx, `UPDATE locations SET name = $3 WHERE tenant_id = $1::uuid AND location_id = $2::uuid`,
			fdiTenant, row[0], row[1]); err != nil {
			t.Fatalf("rename park: %v", err)
		}
		if _, err := repo.pool.Exec(ctx, `UPDATE feed_direction_issue_rows r SET park_label = $3
FROM feed_direction_issues i
WHERE i.tenant_id = $1::uuid AND i.park_id = $2::uuid AND r.tenant_id = i.tenant_id AND r.feed_direction_issue_id = i.feed_direction_issue_id`,
			fdiTenant, row[0], row[1]); err != nil {
			t.Fatalf("relabel sheet rows: %v", err)
		}
	}

	// The completion table labels a farm by its CODE (maintainer decision 2026-09-24, "use cbe/cpt"),
	// so it reads CBE even though the park's name is now Coimbatore -- and still opens on it.
	got := completionRead(t, ctx, repo, target, nil)
	if n := len(got.CompletionFilterOptions); n < 2 || got.CompletionFilterOptions[0].ParkLabel != "CBE" {
		t.Fatalf("completion filter options must open on CBE: got %+v", got.CompletionFilterOptions)
	}
	if n := len(got.DistributionCompletions); n == 0 || got.DistributionCompletions[0].ParkLabel != "CBE" {
		t.Fatalf("completion rows must open on CBE: got first row %+v", firstOrNil(got.DistributionCompletions))
	}
	assertClustered(t, "completion rows", parkLabels(got.DistributionCompletions, func(r domain.DistributionCompletionRow) string { return r.ParkLabel }), "CBE", "CPT")
	t.Run("OneToMany", func(t *testing.T) {
		// Six feed cells include two items for one pen-session. The new park
		// join/grouping must still yield five sessions and two shed options.
		if len(got.DistributionCompletions) != 5 || len(completionKeys(got.DistributionCompletions)) != 5 || len(got.CompletionFilterOptions) != 2 {
			t.Fatalf("park ordering changed cardinality: rows=%d options=%d", len(got.DistributionCompletions), len(got.CompletionFilterOptions))
		}
	})
	t.Run("PageBoundary", func(t *testing.T) {
		var combined []domain.DistributionCompletionRow
		for _, offset := range []int{0, 2, 4} {
			page := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
				q.CompletionLimit, q.CompletionOffset = 2, offset
			})
			if page.CompletionTotals != got.CompletionTotals || page.DistributionCompletionsHasMore != (offset < 4) {
				t.Fatalf("page %d changed totals or continuation: %+v", offset, page)
			}
			combined = append(combined, page.DistributionCompletions...)
		}
		if len(combined) != 5 || len(completionKeys(combined)) != 5 {
			t.Fatalf("pages lost or repeated pen sessions: %+v", combined)
		}
		for i, row := range combined {
			want := got.DistributionCompletions[i]
			if row.ParkID != want.ParkID || row.ShedID != want.ShedID || row.PartitionLabel != want.PartitionLabel || row.SessionNo != want.SessionNo {
				t.Fatalf("page order differs at row %d: got %+v want %+v", i, row, want)
			}
		}
	})
	t.Run("ParkScope", func(t *testing.T) {
		page := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
			q.ParkIDs = []uuid.UUID{uuid.MustParse(fdcParkB)}
		})
		if len(page.DistributionCompletions) != 2 || len(page.CompletionFilterOptions) != 1 {
			t.Fatalf("CPT scope cardinality changed: %+v", page)
		}
		for _, row := range page.DistributionCompletions {
			if row.ParkID != fdcParkB {
				t.Fatalf("CBE-first ordering leaked another park: %+v", row)
			}
		}
		if page.CompletionFilterOptions[0].ParkID != fdcParkB || page.CompletionTotals != (domain.CompletionStatusTotals{NotStarted: 1, Completed: 1}) {
			t.Fatalf("park scope leaked options or totals: %+v", page)
		}
	})
	t.Run("StatusMatrix", func(t *testing.T) {
		for status, count := range map[string]int{
			domain.DistributionCompletionNotStarted:           2,
			domain.DistributionCompletionAwaitingVerification: 1,
			domain.DistributionCompletionRework:               1,
			domain.DistributionCompletionCompleted:            1,
		} {
			page := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) { q.CompletionStatus = status })
			if len(page.DistributionCompletions) != count || page.CompletionTotals != got.CompletionTotals || len(page.CompletionFilterOptions) != 2 {
				t.Fatalf("%s changed rows, totals, or options: %+v", status, page)
			}
			for _, row := range page.DistributionCompletions {
				if row.Status != status {
					t.Fatalf("%s filter returned %+v", status, row)
				}
			}
		}
	})

	shedFeed, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil {
		t.Fatalf("ShedFeedAnalytics: %v", err)
	}
	if len(shedFeed.Rows) == 0 || shedFeed.Rows[0].ParkLabel != "Coimbatore" {
		t.Fatalf("feed-by-pen rows must open on CBE (Coimbatore): got first row %+v", firstOrNil(shedFeed.Rows))
	}
	assertClustered(t, "feed-by-pen rows", parkLabels(shedFeed.Rows, func(r domain.ShedFeedPenRow) string { return r.ParkLabel }), "Coimbatore", "Channapatna")
}

func parkLabels[T any](rows []T, label func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, label(row))
	}
	return out
}

func firstOrNil[T any](rows []T) any {
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

// assertClustered fails when a park reappears after another park has started: two clusters,
// never interleaved.
func assertClustered(t *testing.T, what string, labels []string, first, second string) {
	t.Helper()
	seen := map[string]bool{}
	last := ""
	for _, label := range labels {
		if label != last && seen[label] {
			t.Fatalf("%s interleave parks: %v", what, labels)
		}
		seen[label] = true
		last = label
	}
	if !seen[first] || !seen[second] {
		t.Fatalf("%s must list both parks: %v", what, labels)
	}
}
