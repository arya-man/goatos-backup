package postgres

import (
	"context"
	"testing"
	"time"

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

	got := completionRead(t, ctx, repo, target, nil)
	if n := len(got.CompletionFilterOptions); n < 2 || got.CompletionFilterOptions[0].ParkLabel != "Coimbatore" {
		t.Fatalf("completion filter options must open on CBE (Coimbatore): got %+v", got.CompletionFilterOptions)
	}
	if n := len(got.DistributionCompletions); n == 0 || got.DistributionCompletions[0].ParkLabel != "Coimbatore" {
		t.Fatalf("completion rows must open on CBE (Coimbatore): got first row %+v", firstOrNil(got.DistributionCompletions))
	}
	assertClustered(t, "completion rows", parkLabels(got.DistributionCompletions, func(r domain.DistributionCompletionRow) string { return r.ParkLabel }))

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
	assertClustered(t, "feed-by-pen rows", parkLabels(shedFeed.Rows, func(r domain.ShedFeedPenRow) string { return r.ParkLabel }))
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
func assertClustered(t *testing.T, what string, labels []string) {
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
	if !seen["Coimbatore"] || !seen["Channapatna"] {
		t.Fatalf("%s must list both parks: %v", what, labels)
	}
}
