package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The four per-issue sheet collapses of migration 000433 must equal a from-scratch collapse of
// feed_direction_issue_rows after EVERY production write: first issue, re-issue in place (delete +
// re-insert), amend (upsert changed cells + delete dropped cells) and a second sheet of the same
// day that carries the same pen (normal + experiment). Each check is a two-way EXCEPT ALL, so a
// missing, extra or stale summary row fails.
var feedIssueSummaryChecks = []struct{ name, want, got string }{
	{
		"pens",
		`SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key,
		        MIN(shed_tag), SUM(quantity_kg), MAX(head_count)
		 FROM feed_direction_issue_rows WHERE quantity_kg IS NOT NULL GROUP BY 1, 2, 3, 4, 5, 6`,
		`SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, shed_tag_key, breed_key,
		        shed_tag, quantity_kg, head_count FROM feed_direction_issue_pens`,
	},
	{
		"items",
		`SELECT tenant_id, feed_direction_issue_id, feed_item_key, MIN(l), SUM(kg), SUM(h)
		 FROM (SELECT tenant_id, feed_direction_issue_id, feed_item_key, MIN(feed_item_label) l,
		              SUM(quantity_kg) kg, MAX(head_count) h
		       FROM feed_direction_issue_rows WHERE quantity_kg IS NOT NULL
		       GROUP BY 1, 2, 3, shed_id, partition_key, shed_tag_key, breed_key) x
		 GROUP BY 1, 2, 3`,
		`SELECT tenant_id, feed_direction_issue_id, feed_item_key, feed_item_label, quantity_kg, head_count
		 FROM feed_direction_issue_items`,
	},
	{
		"tag_items",
		`SELECT tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, SUM(quantity_kg)
		 FROM feed_direction_issue_rows WHERE quantity_kg IS NOT NULL GROUP BY 1, 2, 3, 4`,
		`SELECT tenant_id, feed_direction_issue_id, shed_tag_key, feed_item_key, quantity_kg
		 FROM feed_direction_issue_tag_items`,
	},
	{
		"session_items",
		`SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
		        SUM(quantity_kg), MAX(session_label),
		        MIN(COALESCE(NULLIF(breed, ''), 'Unspecified')), MAX(COALESCE(NULLIF(breed, ''), 'Unspecified'))
		 FROM feed_direction_issue_rows WHERE quantity_kg IS NOT NULL GROUP BY 1, 2, 3, 4, 5, 6, 7`,
		`SELECT tenant_id, feed_direction_issue_id, shed_id, partition_key, session_no, workflow, feed_item_key,
		        quantity_kg, session_label, breed_min, breed_max FROM feed_direction_issue_session_items`,
	},
}

func assertFeedIssueSummariesMatchRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, step string) {
	t.Helper()
	for _, c := range feedIssueSummaryChecks {
		var missing, extra, rows int
		if err := pool.QueryRow(ctx, `SELECT
		    (SELECT count(*) FROM (`+c.want+` EXCEPT ALL `+c.got+`) a),
		    (SELECT count(*) FROM (`+c.got+` EXCEPT ALL `+c.want+`) b),
		    (SELECT count(*) FROM (`+c.got+`) g)`).Scan(&missing, &extra, &rows); err != nil {
			t.Fatalf("%s: %s check: %v", step, c.name, err)
		}
		if missing != 0 || extra != 0 {
			t.Fatalf("%s: %s summary drifted from the sheet rows: %d missing, %d extra (%d stored)", step, c.name, missing, extra, rows)
		}
		if rows == 0 {
			t.Fatalf("%s: %s summary is empty", step, c.name)
		}
	}
}

func TestFeedIssueSummariesOneToManyStayEqualToRowsThroughEveryWrite(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	persist := func(workflow, fingerprint string, cells []domain.StoredCell) {
		t.Helper()
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: workflow,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":2026-07-30:" + workflow,
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s/%s: %v", workflow, fingerprint, err)
		}
	}

	persist(domain.WorkflowNormal, "fp-1", analyticsCells())
	assertFeedIssueSummariesMatchRows(t, ctx, pool, "first issue")

	// Re-issue in place: every row is deleted and re-inserted with more heads in shed A part 1.
	reissued := analyticsCells()
	for i := range reissued {
		if reissued[i].ShedID == fdiShedA && reissued[i].PartitionLabel == "1" {
			reissued[i].HeadCount = 11
		}
	}
	persist(domain.WorkflowNormal, "fp-2", reissued)
	assertFeedIssueSummariesMatchRows(t, ctx, pool, "re-issue")

	// Amend: change one quantity, drop the Milk cell, add a second-session hay cell.
	amended := append([]domain.StoredCell(nil), reissued[:4]...)
	q := "1.250"
	amended[0].QuantityKg = &q
	hay := amended[0]
	hay.SessionNo, hay.FeedItemLabel, hay.FeedItemKey, hay.RowSeq, hay.ItemSeq = 2, "Hay", "hay", 1, 1
	hayKg := "0.750"
	hay.QuantityKg = &hayKg
	amended = append(amended, hay)
	if _, err := repo.AmendIssue(ctx, ports.AmendIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		AmendedAt: issuedAt.Add(time.Hour), Fingerprint: "fp-3", Cells: amended,
	}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	assertFeedIssueSummariesMatchRows(t, ctx, pool, "amend")

	// A second sheet of the SAME day carrying the SAME pen-item (shed A part 1, Beetal,
	// Non-Pregnant, Concentrate) with a different head count: the directed items must count that
	// pen's heads ONCE (the MAX, 12) and add the kg.
	exp := analyticsCells()[0]
	exp.Workflow, exp.HeadCount = domain.WorkflowExperiment, 12
	expKg := "1.000"
	exp.QuantityKg = &expKg
	persist(domain.WorkflowExperiment, "fp-exp", []domain.StoredCell{exp})
	assertFeedIssueSummariesMatchRows(t, ctx, pool, "second sheet")

	got, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil {
		t.Fatalf("DirectedAnalytics: %v", err)
	}
	var conc *domain.DirectedDayItem
	for i := range got.Items {
		if got.Items[i].FeedItemKey == "concentrate" {
			conc = &got.Items[i]
		}
	}
	if conc == nil {
		t.Fatalf("no concentrate item: %+v", got.Items)
	}
	// Normal sheet: A/1 11 heads (1.250 + 1.000 kg), A/2 5 heads (0.500 kg); experiment: A/1 12
	// heads (1.000 kg). Heads = max(11, 12) + 5 = 17, never 11 + 12 + 5.
	if conc.HeadDays != 17 || conc.DirectedKg != "3.750" {
		t.Fatalf("shared pen-item: want 17 head-days / 3.750 kg, got %d / %s", conc.HeadDays, conc.DirectedKg)
	}
	if len(got.Days) != 1 || got.Days[0].HeadDays != 17 {
		t.Fatalf("day heads must count the shared pen once (17): %+v", got.Days)
	}
}
