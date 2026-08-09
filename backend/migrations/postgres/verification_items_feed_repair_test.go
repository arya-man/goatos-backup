package postgres

import (
	"strings"
	"testing"
)

// The two feed repair migrations (000139 partition backfill, 000140 subject-label repair) are
// one-time UPDATEs over rows already queued for a verifier. They take their values from the SOURCE
// completion, so what matters is that they read the right column off the right row and cannot
// smear one pen's or one session's value onto another item.
//
// These are structural assertions on the SQL itself rather than a DB round trip: both statements
// are plain correlated UPDATEs, and the property that makes them safe -- the join is on the
// completion's PRIMARY KEY, scoped by tenant -- is readable and pinnable directly. A drift here
// (dropping the tenant predicate, widening the join, back-filling a NULL over a real value) is
// exactly what silently corrupts a verifier's evidence trail.

func TestFeedPartitionBackfillJoinsSourceCompletionOnItsPrimaryKey(t *testing.T) {
	name, body := onlyMigrationWithSuffix(t, "verification_items_feed_partition_backfill")
	t.Logf("partition backfill migration: %s", name)

	up := upBlock(t, body)

	for _, table := range []string{"feed_packing_completions", "feed_distribution_completions"} {
		if !strings.Contains(up, table) {
			t.Errorf("%s: missing the %s source -- a feed item's pen lives on its completion", name, table)
		}
	}
	// Tenant scoping and the completion primary key together are what make the join 1:1.
	if !strings.Contains(up, "vi.tenant_id = c.tenant_id") {
		t.Errorf("%s: join is not tenant-scoped", name)
	}
	if !strings.Contains(up, "vi.source_ref_id = c.completion_id") {
		t.Errorf("%s: join must be on the completion's primary key, or an item can take another pen's label", name)
	}
	// Only fill a GAP, and only from a REAL value: 000137 deliberately left pre-existing
	// completions without a pen, and stamping one would fabricate proof provenance.
	if !strings.Contains(up, "vi.partition_label IS NULL") {
		t.Errorf("%s: must only fill items whose pen is absent, never overwrite one", name)
	}
	if !strings.Contains(up, "c.partition_label IS NOT NULL") || !strings.Contains(up, "btrim(c.partition_label) <> ''") {
		t.Errorf("%s: must not copy a NULL/blank pen onto an item", name)
	}
	// Transport has no pen grain (one task per physical shed per day), so it must not appear.
	if strings.Contains(up, "feed_transport") {
		t.Errorf("%s: transport has no pen to back-fill and must not be touched", name)
	}
}

func TestFeedSubjectLabelRepairTakesSessionFromItsOwnCompletion(t *testing.T) {
	name, body := onlyMigrationWithSuffix(t, "verification_items_feed_subject_label_repair")
	t.Logf("subject label repair migration: %s", name)

	up := upBlock(t, body)

	if !strings.Contains(up, "vi.source_ref_id = c.completion_id") {
		t.Errorf("%s: session must come from the item's OWN completion, not any row of the table", name)
	}
	if !strings.Contains(up, "vi.tenant_id = c.tenant_id") {
		t.Errorf("%s: repair is not tenant-scoped", name)
	}
	if !strings.Contains(up, "'Session ' || c.session_no::text") {
		t.Errorf("%s: distribution subject must be the session and nothing else -- it used to embed the raw shed UUID", name)
	}
	// The whole point of the repair: no shed id, no duplicated pen in user-facing copy.
	for _, banned := range []string{"shed_id::text", "'Pen '"} {
		if strings.Contains(up, banned) {
			t.Errorf("%s: repaired label still composes %s -- that is the defect being removed", name, banned)
		}
	}
	if !strings.Contains(up, "feed_transport_attempt") || !strings.Contains(up, "subject_label = NULL") {
		t.Errorf("%s: transport's subject must be cleared -- it printed the shed a second time", name)
	}
}

// upBlock returns just the executable Up half, so an assertion cannot be satisfied by text that
// only appears in a comment header or in the Down block.
func upBlock(t *testing.T, body string) string {
	t.Helper()
	down := strings.Index(body, "-- +goose Down")
	if down < 0 {
		t.Fatalf("migration has no Down marker")
	}
	up := body[:down]
	// Strip comment lines so a banned token mentioned in the rationale does not fail the test.
	var b strings.Builder
	for _, line := range strings.Split(up, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
