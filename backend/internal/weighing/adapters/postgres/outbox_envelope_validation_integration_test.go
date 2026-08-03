package postgres

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// assertEveryOutboxEnvelopeValidates is the gate that was missing.
//
// Every existing weighing test asserts that an outbox ROW EXISTS. None of them
// asserted the row is DELIVERABLE. The outbox relay validates each envelope
// against contracts/jsonschema/domain-event-envelope.schema.json and, on failure,
// marks the message 'failed' on attempt 1 -- permanently undeliverable, never
// retried, and not dead-lettered. Twelve such events sat unnoticed in the first
// real device run because "a row was written" was the only thing under test.
//
// This helper validates EVERY event the tenant produced, using the same validator
// the relay uses (outboxapp.EnvelopeValidator over the real schema file), so a new
// producer cannot quietly ship an undeliverable envelope.
func assertEveryOutboxEnvelopeValidates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	validator, err := outboxapp.NewEnvelopeValidator(weighingContractsSchemaPath(t))
	if err != nil {
		t.Fatalf("compile domain event envelope schema: %v", err)
	}
	rows, err := pool.Query(ctx, `
SELECT outbox_id::text, event_type, payload::text
FROM outbox_messages
WHERE tenant_id = $1::uuid
ORDER BY created_at, outbox_id`, tenantID)
	if err != nil {
		t.Fatalf("read outbox messages: %v", err)
	}
	defer rows.Close()
	checked := 0
	for rows.Next() {
		var outboxID, eventType, payload string
		if err := rows.Scan(&outboxID, &eventType, &payload); err != nil {
			t.Fatalf("scan outbox message: %v", err)
		}
		checked++
		if err := validator.Validate([]byte(payload)); err != nil {
			t.Errorf("event %s (outbox_id=%s) is UNDELIVERABLE -- the relay marks this invalid_event_envelope and never retries it:\n  %v\n  payload: %s",
				eventType, outboxID, err, payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox messages: %v", err)
	}
	if checked == 0 {
		t.Fatal("no outbox messages were checked; the test proved nothing")
	}
	t.Logf("validated %d outbox envelopes", checked)
}

func weighingContractsSchemaPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// .../backend/internal/weighing/adapters/postgres/<file> -> repo root
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..")
	return filepath.Join(root, "contracts", "jsonschema", "domain-event-envelope.schema.json")
}

// TestWeighingObservationEventsAreDeliverable covers BOTH observation write paths.
//
// The edit path is the one that matters here: it is the path a genuine weight
// correction and a verifier-rework re-capture take, and it is the path the
// two-phase capture defect was flooding. Fixing the fan-out removes the flood but
// NOT the defect on this path -- so this test drives an actual edit and asserts the
// resulting event validates, rather than merely existing.
func TestWeighingObservationEventsAreDeliverable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	const scannedTag = "901007000504407"

	base := domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          12,
		ProofArtifactID:   repoExpectedShedProof,
		ActualLocationID:  repoActualShed,
		RecordedBy:        repoOperator,
	}

	newCapture := base
	newCapture.IdempotencyKey = "deliverable:new"
	first, err := repo.RecordAnimalObservation(ctx, newCapture)
	if err != nil {
		t.Fatalf("new capture: %v", err)
	}
	if first.ActualLocationID != repoActualShed {
		t.Fatalf("new capture actual location=%q, want %q", first.ActualLocationID, repoActualShed)
	}

	// A GENUINE edit: different weight, so the updated CTE opens a new round.
	edit := base
	edit.WeightKg = 13.5
	edit.IdempotencyKey = "deliverable:edit"
	edited, err := repo.RecordAnimalObservation(ctx, edit)
	if err != nil {
		t.Fatalf("edit capture: %v", err)
	}
	if !edited.Superseded {
		t.Fatalf("edit Superseded=false, want true")
	}
	// THE DEFECT: the edit CTE hardcoded '' for actual_location_id/label, so an edit
	// silently dropped the operator-supplied location that the new-capture path
	// carries. The row still holds the real value -- only the RETURNING was blank.
	if edited.ActualLocationID != repoActualShed {
		t.Fatalf("edit actual location=%q, want %q: the edit path must return the row's real actual_location_id, not an empty string",
			edited.ActualLocationID, repoActualShed)
	}

	// A capture whose idempotency key is the length the real device actually sent. The client's
	// two-phase `:proof:`-suffixed key was 226 characters; trace_id's contract limit is 200, and
	// this producer used the key verbatim as the trace id, so all ten of these died in the relay
	// as invalid_event_envelope on attempt 1 and were never retried.
	longKey := domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "901007000504408",
		WeightKg:          14.25,
		ProofArtifactID:   repoExpectedShedProof,
		ActualLocationID:  repoActualShed,
		RecordedBy:        repoOperator,
		IdempotencyKey: "weighing:individual:" + strings.Repeat("0", 150) +
			":901007000504408:d7bc19f0-0000-4000-8000-000000000001:proof:018073aa",
	}
	if len(longKey.IdempotencyKey) <= maxEnvelopeTraceIDLen {
		t.Fatalf("fixture key is only %d chars; it must exceed the %d-char trace_id limit to reproduce the defect",
			len(longKey.IdempotencyKey), maxEnvelopeTraceIDLen)
	}
	if _, err := repo.RecordAnimalObservation(ctx, longKey); err != nil {
		t.Fatalf("long-key capture: %v", err)
	}

	assertEveryOutboxEnvelopeValidates(t, ctx, pool, repoTenant)
}

// TestBoundedTraceIDKeepsEnvelopeContract is the pure-unit half of the 12-undeliverable-events
// defect: it needs no Postgres and no Docker, so it runs everywhere.
func TestBoundedTraceIDKeepsEnvelopeContract(t *testing.T) {
	// Verbatim from the first real device run (read-only from the phone-QA DB): the keys that
	// published were 183 chars, the ten that died were 226, and the two campaign_created events
	// were 249 and 431.
	cases := []struct {
		name string
		idem string
	}{
		{"published length", strings.Repeat("a", 183)},
		{"boundary", strings.Repeat("a", maxEnvelopeTraceIDLen)},
		{"observation proof-suffixed", strings.Repeat("a", 226)},
		{"campaign created", strings.Repeat("a", 249)},
		{"campaign created long", strings.Repeat("a", 431)},
	}
	for _, tc := range cases {
		got := boundedTraceID(tc.idem)
		if len(got) > maxEnvelopeTraceIDLen {
			t.Errorf("%s: trace_id len=%d, want <= %d -- the relay marks this invalid_event_envelope and never retries", tc.name, len(got), maxEnvelopeTraceIDLen)
		}
		if len(tc.idem) <= maxEnvelopeTraceIDLen && got != tc.idem {
			t.Errorf("%s: a key already inside the limit must pass through unchanged, got %q", tc.name, got)
		}
	}
	// Two DISTINCT long keys sharing a prefix must not collide: a plain truncation would map
	// them to the same trace id.
	a := boundedTraceID(strings.Repeat("a", 300) + "-one")
	b := boundedTraceID(strings.Repeat("a", 300) + "-two")
	if a == b {
		t.Fatalf("distinct long keys collided on trace_id: %q", a)
	}
}
