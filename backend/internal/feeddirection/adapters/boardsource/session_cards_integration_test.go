package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// The per-session direction cards (maintainer instruction 2026-09-25), adversarially: one pen with
// MANY bags in one session is one pen of that session's card; paging walks every session card
// once; a park never sees another park's sessions; and every state a card can hold filters to
// exactly that card.

func startSeededFeed(t *testing.T) (context.Context, *Source) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)
	seed(t, ctx, pool)
	return ctx, New(pool, 5*time.Second)
}

func TestFeedDirectionSessionOneToManyBagsStayOnePen(t *testing.T) {
	ctx, src := startSeededFeed(t)
	// A second feed item on Godel 1's MORNING row and a second evening item on Castro: more
	// sheet rows, never more pens.
	exec(t, ctx, src.pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, partition_label, shed_tag, breed, ration_group, session_no, session_label,
  head_count, head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Pen', NULL, 'Dry', 'Beetal', 'Beetal', 1, 'Morning',
        10, false, 'normal', 'Hay', 1.0, 2.0, false, 0, 1, false),
       ($1::uuid, $2::uuid, $3::uuid, 'CBE', $5::uuid, 'Pen', NULL, 'Dry', 'Beetal', 'Beetal', 2, 'Evening',
        10, false, 'normal', 'Hay', 1.0, 2.0, false, 3, 1, false)
ON CONFLICT DO NOTHING`, bsTenant, "00000000-0000-4000-8000-0000000081a2", bsPark, bsShedA, bsShedB)
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	got := byID(rows)
	for _, id := range []string{directionID("1"), directionID("2")} {
		if pens := got[id].Counts.Done + got[id].Counts.Pending; pens != 2 {
			t.Errorf("%s: %d pens, want 2 (a second feed item is a second bag line, not a second pen)", id, pens)
		}
	}
}

func TestFeedCardsPaginationWalksEverySessionCardOnce(t *testing.T) {
	ctx, src := startSeededFeed(t)
	seen := map[string]bool{}
	after := ""
	for i := 0; i < 10; i++ {
		q := query("")
		q.Limit = 1
		q.AfterSourceID = after
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if seen[page[0].SourceID] {
			t.Fatalf("card %s served twice", page[0].SourceID)
		}
		seen[page[0].SourceID] = true
		after = page[0].SourceID
	}
	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	if len(seen) != 5 || total != 5 || !seen[directionID("1")] || !seen[directionID("2")] {
		t.Fatalf("one-card pages saw %v (count %d), want the five cards with both direction sessions", seen, total)
	}
}

func TestFeedDirectionSessionCardsParkScope(t *testing.T) {
	ctx, src := startSeededFeed(t)
	other, err := src.ListRows(ctx, ports.SourceQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range other {
		if r.ParkID != bsOtherPk {
			t.Fatalf("other park served a card of park %s", r.ParkID)
		}
		if _, ok := parseSourceID(r.SourceID); !ok || r.Title == "Feed direction · Morning" || r.Title == "Feed direction · Evening" {
			t.Fatalf("other park served %q (%s): its sheet issued no direction", r.Title, r.SourceID)
		}
	}
	page, err := src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, SourceID: directionID("1"), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("another park's morning card drilled into %d pens", page.Total)
	}
}

func TestFeedCardsStatusMatrix(t *testing.T) {
	ctx, src := startSeededFeed(t)
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	want := map[domain.WorkState]map[string]bool{}
	for _, r := range rows {
		if want[r.WorkState] == nil {
			want[r.WorkState] = map[string]bool{}
		}
		want[r.WorkState][r.SourceID] = true
	}
	for _, state := range domain.WorkStates() {
		got, err := src.ListRows(ctx, query("", state))
		if err != nil {
			t.Fatal(err)
		}
		counts, err := src.CountByState(ctx, query("", state))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want[state]) || counts[state] != len(want[state]) {
			t.Fatalf("state %s: %d rows / count %d, want %d", state, len(got), counts[state], len(want[state]))
		}
		for _, r := range got {
			if !want[state][r.SourceID] || r.WorkState != state {
				t.Fatalf("state %s served %s in state %s", state, r.SourceID, r.WorkState)
			}
		}
	}
}
