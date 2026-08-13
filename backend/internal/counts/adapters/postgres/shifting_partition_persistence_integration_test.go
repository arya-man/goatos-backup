package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// A FIXED business-day anchor, not time.Now(): a Goat OS business day is an India business day, and
// a movement raised at 02:00 IST must not read as the previous date. Fixing the instant also makes
// these tests deterministic rather than dependent on the hour they run -- the exact defect class that
// once made 15 calendar tests pass or fail by time of day.
func fixedBusinessInstant() time.Time {
	return time.Date(2026, 8, 6, 9, 0, 0, 0, biztime.DefaultLocation())
}

// Regression proof for a defect that survived a full code review and was only found by running the
// real path end to end (2026-08-06).
//
// The bug: `INSERT INTO shifting_events` never NAMED source_partition_label /
// destination_partition_label. Every other layer was correct -- the columns had existed since
// migration 000113, the OpenAPI contract declared destination_partition_label, the Go handler parsed
// AND validated it against the shed's real pens, and the Android DTO declared both fields. Only the
// INSERT's column list omitted them, so Postgres defaulted both to NULL. Omitting a nullable column
// from an INSERT is legal SQL: nothing errored, nothing warned, and every shifting movement ever
// raised recorded its pen as NULL. A Castro 1 -> Castro 2 move was stored, and then displayed, as
// Castro -> Castro.
//
// Why this test is an INTEGRATION test and not a service/handler test: a fake repository returns
// whatever the test stocked it with, so it would have "proved" the pen round-tripped while
// production silently dropped it. The only thing that can catch a missing column is the real INSERT
// against the real schema, followed by reading the row back.
//
// Why it asserts the READ-BACK and not just a nil error: the buggy INSERT returned no error at all.
// Any assertion weaker than "select the column and compare it" passes against the bug.
func TestRaisedShiftingEventPersistsItsPartitionLabels(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)

	source := "Part 2"
	destination := "Part 7"
	event := domain.ShiftingEvent{
		TenantID:                  countsTenant,
		LogicalShiftingEventKey:   "partition-persistence-both-ends",
		Priority:                  "low",
		Category:                  "growth",
		SourceParkID:              strPtrCounts(destSecondPark),
		SourceShedID:              strPtrCounts(destCastroCPT),
		SourcePartitionLabel:      &source,
		DestinationParkID:         destSecondPark,
		DestinationShedID:         destCastroCBE,
		DestinationPartitionLabel: &destination,
		RaisedAt:                  fixedBusinessInstant(),
		EffectiveAt:               fixedBusinessInstant(),
		AuthorizationState:        "pending",
		VerificationState:         "unverified",
		EventStatus:               "pending",
		SourceSystem:              "goatos_canonical",
		SourceRef:                 "partition-persistence",
		PayloadHash:               "hash-partition-persistence",
		IdempotencyKey:            "idem-partition-persistence",
		RequestFingerprint:        "fingerprint-partition-persistence",
	}

	id := insertShiftingEventForTest(t, ctx, pool, event)

	var gotSource, gotDestination *string
	if err := pool.QueryRow(ctx, `
SELECT source_partition_label, destination_partition_label
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, id).Scan(&gotSource, &gotDestination); err != nil {
		t.Fatalf("read back the raised movement: %v", err)
	}

	if gotDestination == nil {
		t.Fatal("destination_partition_label is NULL: the INSERT dropped the pen the operator chose, so a Castro 1 -> Castro 2 move is stored as Castro -> Castro")
	}
	if *gotDestination != destination {
		t.Fatalf("destination_partition_label = %q, want %q", *gotDestination, destination)
	}
	if gotSource == nil {
		t.Fatal("source_partition_label is NULL: the movement no longer records which pen the animals left")
	}
	if *gotSource != source {
		t.Fatalf("source_partition_label = %q, want %q", *gotSource, source)
	}
}

// A movement between whole sheds must keep storing NULL rather than a sentinel: "no pen" is a real
// and common answer, and writing "whole" (or "") into the column would leak the matching sentinel
// into a value every partition-aware read then has to special-case.
func TestRaisedShiftingEventWithoutPartitionsStoresNull(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	seedDestinationTopology(t, ctx, pool)

	event := domain.ShiftingEvent{
		TenantID:                countsTenant,
		LogicalShiftingEventKey: "partition-persistence-neither-end",
		Priority:                "low",
		Category:                "growth",
		DestinationParkID:       destSecondPark,
		DestinationShedID:       destCastroCBE,
		RaisedAt:                fixedBusinessInstant(),
		EffectiveAt:             fixedBusinessInstant(),
		AuthorizationState:      "pending",
		VerificationState:       "unverified",
		EventStatus:             "pending",
		SourceSystem:            "goatos_canonical",
		SourceRef:               "partition-persistence-null",
		PayloadHash:             "hash-partition-persistence-null",
		IdempotencyKey:          "idem-partition-persistence-null",
		RequestFingerprint:      "fingerprint-partition-persistence-null",
	}

	id := insertShiftingEventForTest(t, ctx, pool, event)

	var gotSource, gotDestination *string
	if err := pool.QueryRow(ctx, `
SELECT source_partition_label, destination_partition_label
FROM shifting_events
WHERE tenant_id = $1::uuid AND shifting_event_id = $2::uuid`,
		countsTenant, id).Scan(&gotSource, &gotDestination); err != nil {
		t.Fatalf("read back the raised movement: %v", err)
	}
	if gotDestination != nil {
		t.Fatalf("destination_partition_label = %q, want NULL for a whole-shed movement", *gotDestination)
	}
	if gotSource != nil {
		t.Fatalf("source_partition_label = %q, want NULL for a whole-shed movement", *gotSource)
	}
}

// insertShiftingEventForTest drives the PRODUCTION insert (insertShiftingEvent), inside its own
// transaction, so the statement under test is byte-for-byte the one a real raise executes.
func insertShiftingEventForTest(t *testing.T, ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, event domain.ShiftingEvent) string {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, replayed, err := insertShiftingEvent(ctx, tx, event)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insertShiftingEvent: %v", err)
	}
	if replayed {
		_ = tx.Rollback(ctx)
		t.Fatal("unexpected idempotent replay for a fresh logical key")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id
}

func strPtrCounts(v string) *string { return &v }
