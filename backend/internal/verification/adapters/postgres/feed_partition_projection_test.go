package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// A feed item's PEN must survive the round trip, because the verifier's card is composed from it.
//
// Reported on STG 2026-08-09: a feed packing card read "Mandela 1" for a proof filmed in
// "Mandela 1 Part 2", so a verifier could not tell which of ten pens a clip came from. The
// producers left verification_items.partition_label NULL (folding the pen into the display label
// instead), and migration 000139 back-fills the queued rows from their source completion.
//
// This asserts the READ, on both paths, because that is what the card renders: the wire boundary
// composes shed + pen via oploc.Display(), so a pen that does not come back out of the repository
// degrades silently to the bare shed name rather than failing.
func TestFeedItemPartitionRoundTripsThroughBothReadPaths_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	parkID := newPark(t, ctx, pool, tenantID)

	pen := "Part 2"
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID,
		Vertical: "feed",
		Module:   "feed",
		Category: "feed_packing",
		Source: domain.SourceRef{
			Module:  "feed",
			RefType: "feed_packing_completion",
			RefID:   tenantID,
		},
		SubjectLabel:   strPtr("Session 1"),
		PartitionLabel: &pen,
		ParkID:         &parkID,
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "feed-packing-verification:pen-1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	got, err := repo.GetItem(ctx, tenantID, created.Item.ItemID)
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if got.PartitionLabel == nil || *got.PartitionLabel != pen {
		t.Errorf("GetItem PartitionLabel = %v, want %q", got.PartitionLabel, pen)
	}

	// The queue read uses a DIFFERENT column list and scan function; the two drifting apart is the
	// defect this guards, and it fails at RUNTIME (field/destination count) rather than at compile.
	rows, err := repo.ListQueue(ctx, ports.ListQueueParams{TenantID: tenantID, Status: domain.StatusPending, Limit: 10})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("ListQueue returned no rows for the item just created")
	}
	if rows[0].PartitionLabel == nil || *rows[0].PartitionLabel != pen {
		t.Errorf("ListQueue PartitionLabel = %v, want %q -- the card would render the bare shed", rows[0].PartitionLabel, pen)
	}

	// The subject stays the SESSION alone: it used to embed the raw shed UUID and a duplicate pen,
	// which is what migration 000140 repairs on already-queued rows.
	if rows[0].SubjectLabel == nil || *rows[0].SubjectLabel != "Session 1" {
		t.Errorf("SubjectLabel = %v, want just the session", rows[0].SubjectLabel)
	}
}

func strPtr(s string) *string { return &s }
