package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// THE 2026-08-08 STG OUTAGE, reproduced against a real Postgres.
//
// Freezing a sheet for a PARTITIONED shed used to fail with
//
//	duplicate key value violates unique constraint "feed_direction_issue_rows_natural_key_uidx"
//	(SQLSTATE 23505)
//
// because Castro 1 / Castro 2 / Castro 3 share shed_id, session_no, shed_tag_key, breed_key and
// feed_item_key and differ ONLY by pen, which the natural key did not include. Every
// /feed-direction/preview returned 500 -- 7 requests, 7 failures, zero successes -- until
// migration 000135 added partition_label + partition_key and put the pen in the key.
//
// This test MUST run against the real schema. The app-layer fake issue store is a Go map keyed by
// (park, feedDay, workflow) with no unique index, so it accepted the colliding cells silently and
// every app-layer test stayed green while production was down. A defect that only a constraint can
// catch needs a database to catch it -- which also means it only runs under
// GOATOS_RUN_POSTGRES_TESTS, so a default `make ci-local` still cannot see it.
func TestPersistIssueFreezesEachPartitionOfOneShedAsItsOwnCell(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	// ONE shed, three pens, identical in every other key column -- the exact shape that collided.
	pens := []string{"1", "2", "3"}
	cells := make([]domain.StoredCell, 0, len(pens))
	for i, pen := range pens {
		cells = append(cells, domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE",
			ShedID: fdiShedA, ShedLabel: "Castro", PartitionLabel: pen,
			ShedTag: "Non-Pregnant", Breed: "Beetal", RationGroup: "Beetal/Sirohi",
			SessionNo: 1, SessionLabel: "Morning", HeadCount: int64(10 + i),
			Workflow: domain.WorkflowNormal, FeedItemLabel: "Concentrate", FeedItemKey: "concentrate",
			QuantityKg: kg("1.000"), GramsPerHead: kg("100.000"), ShedFactor: kg("1.0000"),
			SessionTotalKg: "1.000", RowSeq: int32(i), ItemSeq: 0,
		})
	}

	if _, err := repo.PersistIssue(ctx, issueCmd(cells, "fp-partitions", time.Now().UTC())); err != nil {
		t.Fatalf("freezing a partitioned shed must not violate the natural key: %v", err)
	}

	// All three pens survive as DISTINCT stored rows. Before 000135 this INSERT rolled back whole,
	// so the assertion to make is the count, not merely "no error".
	var stored int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_direction_issue_rows
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND feed_item_key = 'concentrate'`,
		fdiTenant, fdiShedA).Scan(&stored); err != nil {
		t.Fatalf("count stored cells: %v", err)
	}
	if stored != len(pens) {
		t.Fatalf("stored %d concentrate cells, want %d (one per pen)", stored, len(pens))
	}

	// The pen must round-trip: a frozen sheet that forgets which pen a bag belongs to is unusable
	// on the packing floor even when the row count is right.
	var issueID string
	if err := pool.QueryRow(ctx, `
SELECT feed_direction_issue_id::text FROM feed_direction_issues
WHERE tenant_id = $1::uuid AND feed_day = '2026-07-30' AND workflow = 'normal'`,
		fdiTenant).Scan(&issueID); err != nil {
		t.Fatalf("resolve issue id: %v", err)
	}
	byIssue, err := repo.LoadIssueRows(ctx, fdiTenant, []string{issueID})
	if err != nil {
		t.Fatalf("LoadIssueRows: %v", err)
	}
	if len(byIssue[issueID]) == 0 {
		t.Fatal("LoadIssueRows returned nothing for the frozen sheet")
	}
	seen := map[string]int64{}
	for _, loaded := range byIssue {
		for _, c := range loaded {
			if c.FeedItemKey == "concentrate" {
				seen[c.PartitionLabel] = c.HeadCount
			}
		}
	}
	for i, pen := range pens {
		want := int64(10 + i)
		got, ok := seen[pen]
		if !ok {
			t.Fatalf("pen %q did not round-trip; got pens %v", pen, seen)
		}
		if got != want {
			t.Fatalf("pen %q head count = %d, want %d: the pens were mixed up", pen, got, want)
		}
	}
}

// An UNDIVIDED shed still has exactly one identity. partition_key normalizes NULL/” to 'whole' for
// precisely this reason: Postgres treats NULLs as distinct in a unique index, so keying on the raw
// nullable label would let the same cell be stored twice for every unpartitioned shed -- silently
// double-feeding it. This asserts the constraint still BITES when there is no pen.
func TestPersistIssueStillRejectsADuplicateCellForAnUndividedShed(t *testing.T) {
	ctx := context.Background()
	_, pool := setupIssueDB(t, ctx)

	issueID := "fd100000-0000-4000-8000-00000000a001"
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow,
  state, issued_at, generation_input_fingerprint, request_fingerprint, idempotency_key, generated_by,
  source_contract, source_contract_version)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-31', 'normal', 'issued', now(), 'fp', 'rfp', 'idem-undivided', 'test',
  'feed.direction.sheet', 1)`,
		issueID, fdiTenant, fdiPark); err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	insert := func() error {
		_, err := pool.Exec(ctx, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label,
  shed_id, shed_label, shed_tag, breed, ration_group, session_no, session_label, head_count,
  head_count_informational, workflow, feed_item_label, quantity_kg, session_total_kg,
  overdue_pending, row_seq, item_seq, amended)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, 'Yashoda', 'Non-Pregnant', 'Beetal',
  'Beetal/Sirohi', 1, 'Morning', 10, false, 'normal', 'Concentrate', 1.0, 1.0, false, 0, 0, false)`,
			fdiTenant, issueID, fdiPark, fdiShedB)
		return err
	}
	if err := insert(); err != nil {
		t.Fatalf("first insert for an undivided shed must succeed: %v", err)
	}
	if err := insert(); err == nil {
		t.Fatal("a duplicate cell for an UNDIVIDED shed must still violate the natural key; " +
			"a NULL-keyed index would have allowed it and double-fed the shed")
	}
}
