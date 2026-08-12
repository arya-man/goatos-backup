package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// THE 2026-08-08 SHARED-PROOF DEFECT, against the real schema.
//
// Reported on STG: submitting the Castro - 1 morning packing video flipped Castro - 2 morning to
// "in review" as well, and the same for evening and for distribution. Both completion tables keyed
// on (tenant, park, shed_id, session_no, target_date, workflow) with no pen, so every partition of
// a shed resolved to ONE completion row -- and that row carries the proof reference, so one clip
// became the evidence for pens nobody filmed. Castro has 3 pens and Godel 1 has 7.
//
// Migration 000137 puts the pen in the natural key. These tests assert the OUTCOME on a real DB:
// two pens of ONE shed, same session, same day, same workflow are TWO independent completions.
//
// A Go-level fake store cannot catch this -- it is a map with no unique index, and it accepted the
// colliding rows while STG was broken. This is why the regression lives here and runs only under
// GOATOS_RUN_POSTGRES_TESTS.
func TestPackingCompletionIsPerPenNotPerShed(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "1")
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "2")

	pen1 := packingParams()
	pen1.PartitionLabel = "1"
	pen1.PackingProofRef = "proof-packing-pen-1"
	pen1.IdempotencyKey = "feed-packing-pen-1"

	pen2 := packingParams()
	pen2.PartitionLabel = "2"
	pen2.PackingProofRef = "proof-packing-pen-2"
	pen2.IdempotencyKey = "feed-packing-pen-2"

	first, err := repo.CompletePacking(ctx, pen1)
	if err != nil {
		t.Fatalf("complete pen 1: %v", err)
	}
	second, err := repo.CompletePacking(ctx, pen2)
	if err != nil {
		t.Fatalf("complete pen 2 must not collide with pen 1: %v", err)
	}
	if first.CompletionID == second.CompletionID {
		t.Fatal("both pens returned the SAME completion: pen 2 was absorbed into pen 1's row")
	}
	if !second.NewlyPending {
		t.Fatal("pen 2 must be a FRESH pending transition; treating it as a replay means no verifier " +
			"item is enqueued and pen 2's video is never reviewed")
	}

	// Two rows, and each holds its OWN video. If the pen were missing from the key there would be
	// one row and one proof standing for both.
	rows, err := pool.Query(ctx, `
	SELECT sp.partition_label, c.packing_proof_ref
	FROM feed_packing_completions c
	JOIN shed_partitions sp
	  ON sp.tenant_id = c.tenant_id
	 AND sp.operational_location_id = c.shed_id
	WHERE c.tenant_id = $1::uuid AND sp.shed_id = $2::uuid
	ORDER BY sp.partition_label`, pen1.TenantID, pen1.ShedID)
	if err != nil {
		t.Fatalf("read completions: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var pen, proof string
		if err := rows.Scan(&pen, &proof); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[pen] = proof
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("stored %d completions for one shed, want 2 (one per pen): %v", len(got), got)
	}
	if got["1"] != "proof-packing-pen-1" || got["2"] != "proof-packing-pen-2" {
		t.Fatalf("each pen must keep its OWN video, got %v", got)
	}
}

// The same shed, same pen, submitted twice is still ONE completion. Widening the key must not turn
// a genuine duplicate into a second row -- that would let the same pen be packed twice and enqueue
// two verifier items for one piece of work.
func TestPackingCompletionStillDeduplicatesTheSamePen(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "Part 3")

	p := packingParams()
	p.PartitionLabel = "Part 3"

	first, err := repo.CompletePacking(ctx, p)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	again, err := repo.CompletePacking(ctx, p)
	if err != nil {
		t.Fatalf("exact replay must be a no-op, not an error: %v", err)
	}
	if first.CompletionID != again.CompletionID {
		t.Fatalf("replay created a SECOND completion (%s vs %s) for the same pen",
			first.CompletionID, again.CompletionID)
	}
	if again.NewlyPending {
		t.Fatal("an exact replay must not re-enqueue a verifier item")
	}
}

func TestPackingCompletionRequiresCatalogPartitionForPartitionedShed(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupFeedDirectionDB(t, ctx)
	seedFeedDirectionPartition(t, ctx, pool, fdShedA, "1")

	blank := packingParams()
	blank.IdempotencyKey = "feed-packing-blank-partitioned-shed"
	if _, err := repo.CompletePacking(ctx, blank); !errors.Is(err, ports.ErrInvalidPartition) {
		t.Fatalf("blank partitioned shed err = %v, want ErrInvalidPartition", err)
	}

	fabricated := packingParams()
	fabricated.PartitionLabel = "999"
	fabricated.IdempotencyKey = "feed-packing-fabricated-partition"
	if _, err := repo.CompletePacking(ctx, fabricated); !errors.Is(err, ports.ErrInvalidPartition) {
		t.Fatalf("fabricated partition err = %v, want ErrInvalidPartition", err)
	}
}

// An UNDIVIDED shed has exactly one identity. partition_key normalizes NULL/” to 'whole' because
// Postgres treats NULLs as DISTINCT in a unique index -- keying on the raw nullable label would let
// the same undivided shed be completed twice, which is the opposite of the constraint's purpose.
func TestPackingCompletionKeepsOneIdentityForAnUndividedShed(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupFeedDirectionDB(t, ctx)

	blank := packingParams() // PartitionLabel left empty: an undivided shed
	first, err := repo.CompletePacking(ctx, blank)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	again, err := repo.CompletePacking(ctx, blank)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first.CompletionID != again.CompletionID {
		t.Fatal("an undivided shed produced TWO completions: a NULL-keyed index would double-feed it")
	}
}

var _ = domain.WorkflowNormal
var _ = ports.CompletePackingParams{}
