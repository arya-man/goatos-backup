package postgres

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// BUG-005 proof. A terminal procurement decision (died / sold / lost) already flipped the animal's
// procurement row and the canonical goats row to an exited lifecycle, but nothing told the rest of
// the operating system. The animal's open vaccination obligations stayed 'scheduled' forever, so a
// dead goat kept generating work, kept occupying operator capacity, and kept showing as overdue.
// The fix emits goat.exited into goat_identity_events AND outbox_messages inside the decision
// transaction, so the registered obligation SM-3 consumer cancels the open work.
//
// This test refuses to accept "the INSERTs are present" as proof. It drives the whole spine:
// RecordDecision -> outbox row -> the REAL envelope validator the outbox relay runs -> the REAL
// eventbus.EventFromEnvelope mapping the domain-event consumer runs -> the REAL registered
// GoatExitedHandler -> obligation rows actually canceled.

// domainEventEnvelopeSchemaPath resolves the same contract file cmd/domain-event-consumer and
// cmd/outbox-relay compile at boot. Tests run from the package dir, so walk up to the repo root.
func domainEventEnvelopeSchemaPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, "contracts", "jsonschema", "domain-event-envelope.schema.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("domain-event-envelope.schema.json not found above %s", dir)
	return ""
}

func readGoatExitedOutboxPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) []byte {
	t.Helper()
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT payload FROM outbox_messages
WHERE tenant_id=$1 AND event_type='goat.exited' AND aggregate_id=$2`, testTenant, goatID).Scan(&payload); err != nil {
		t.Fatalf("read goat.exited outbox payload: %v", err)
	}
	return payload
}

// TestTerminalProcurementDecisionCancelsOpenObligationsEndToEnd is the BUG-005 RED/GREEN anchor.
func TestTerminalProcurementDecisionCancelsOpenObligationsEndToEnd(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcurementCommon(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	load := createProcurementLoad(t, ctx, repo, "terminal-exit-load", 1)
	goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
		TenantID: testTenant, LoadID: load.LoadID, AnimalIdentifier1: strPtr("TERMINAL-EXIT"),
		SourceEntryState: "accepted", OwnershipState: "mesha_owned", IdempotencyKey: "terminal-exit-goat",
	})

	// Run the animal all the way through real intake so it is a LIVE herd animal with a shed, not a
	// candidate row: only an accepted_herd_intake animal is allowed to carry vaccination obligations
	// (vw_procurement_vaccination_excluded_goats guards that), which is exactly the state in which
	// losing the animal has consequences worth cancelling.
	if _, err := repo.RecordSourceHealth(ctx, ports.SourceHealth{
		TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, HealthState: domain.HealthPassed,
		CheckedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC), IdempotencyKey: "terminal-exit-health",
	}); err != nil {
		t.Fatalf("health pass: %v", err)
	}
	if _, err := repo.RecordDecision(ctx, ports.Decision{
		TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID, DecisionStage: "pre_dispatch",
		DecisionType: domain.DecisionAccepted, DecidedAt: time.Date(2026, 6, 1, 9, 30, 0, 0, time.UTC),
		IdempotencyKey: "terminal-exit-accept",
	}); err != nil {
		t.Fatalf("pre-dispatch accept: %v", err)
	}
	proofID := insertProof(t, ctx, pool, "71000000-0000-4000-8000-000000000901", "terminal-exit-dispatch-proof")
	if _, err := repo.DispatchLoad(ctx, ports.DispatchLoad{
		TenantID: testTenant, LoadID: load.LoadID, ToLocationID: testPark, ProofRefID: &proofID,
		DispatchedAt: time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC), IdempotencyKey: "terminal-exit-dispatch",
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if _, err := repo.RecordArrivalReview(ctx, ports.ArrivalReview{
		TenantID: testTenant, LoadID: load.LoadID, ParkLocationID: testPark,
		ExpectedCount: 1, LoadedCount: 1, ArrivedCount: 1, MatchedCount: 1,
		Status: domain.DecisionAccepted, ReviewedAt: time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC),
		IdempotencyKey: "terminal-exit-arrival",
		Goats:          []ports.ArrivalGoat{{GoatID: &goat.GoatID, ArrivalState: "accepted"}},
	}); err != nil {
		t.Fatalf("arrival review: %v", err)
	}
	if _, err := repo.AcceptIntake(ctx, ports.AcceptIntake{
		TenantID: testTenant, LoadID: load.LoadID, GoatIDs: []string{goat.GoatID},
		ParkLocationID: testPark, ShedLocationID: testShed,
		AcceptedAt:     time.Date(2026, 6, 1, 17, 0, 0, 0, time.UTC),
		EntryDate:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		IdempotencyKey: "terminal-exit-intake",
	}); err != nil {
		t.Fatalf("accept intake: %v", err)
	}

	// The animal has REAL open vaccination work at the moment it dies.
	versionID, ruleID := seedVaccinationProtocol(t, ctx, pool, "terminal-exit")
	if err := insertActiveVaccinationObligation(ctx, pool, versionID, ruleID, goat.GoatID, "terminal-exit-obligation"); err != nil {
		t.Fatalf("seed open vaccination obligation: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("fixture invalid: open obligations = %d, want 1", got)
	}

	decision := ports.Decision{
		TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID,
		DecisionStage: "exit", DecisionType: domain.DecisionRejected,
		Reason:         "goat died in shed after intake",
		DecidedAt:      time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC),
		IdempotencyKey: "terminal-exit-decision",
	}
	if _, err := repo.RecordDecision(ctx, decision); err != nil {
		t.Fatalf("RecordDecision(terminal): %v", err)
	}

	// (a) Canonical exit landed.
	if got := scanProcurementText(t, ctx, pool,
		`SELECT lifecycle_status FROM goats WHERE tenant_id=$1 AND goat_id=$2`, testTenant, goat.GoatID); got != "dead" {
		t.Fatalf("goat lifecycle_status = %q, want dead", got)
	}

	// (b) BOTH event rows exist, exactly once.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.exited'`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("goat.exited identity events = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.exited' AND aggregate_id=$2`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("goat.exited outbox messages = %d, want 1", got)
	}

	// (c) The outbox payload must survive the relay. cmd/outbox-relay validates every payload
	// against contracts/jsonschema/domain-event-envelope.schema.json BEFORE publishing, and marks a
	// non-conforming message FAILED ('invalid_event_envelope') instead of publishing it. A payload
	// that is not a full envelope is a message that is written, never delivered, and never retried:
	// the spine looks wired in the code and is silently dead in production.
	payload := readGoatExitedOutboxPayload(t, ctx, pool, goat.GoatID)
	validator, err := outboxapp.NewEnvelopeValidator(domainEventEnvelopeSchemaPath(t))
	if err != nil {
		t.Fatalf("compile envelope schema: %v", err)
	}
	if err := validator.Validate(payload); err != nil {
		t.Fatalf("goat.exited outbox payload is not a valid domain event envelope, the relay would drop it as invalid_event_envelope: %v\npayload: %s", err, payload)
	}

	// (d) The consumer's real envelope->Event mapping must recover routing identity from the payload
	// ALONE (no Pub/Sub attribute fallback), because that is what carries tenant + goat id to the
	// handler. Wrong/missing aggregate_id means the handler cancels nothing.
	event, err := eventbus.EventFromEnvelope(payload, eventbus.Event{})
	if err != nil {
		t.Fatalf("EventFromEnvelope: %v", err)
	}
	if event.Type != oblapp.EventGoatExited {
		t.Fatalf("event type = %q, want %q", event.Type, oblapp.EventGoatExited)
	}
	if event.TenantID != testTenant {
		t.Fatalf("event tenant = %q, want %q", event.TenantID, testTenant)
	}
	if event.Key != goat.GoatID {
		t.Fatalf("event key = %q, want goat id %q", event.Key, goat.GoatID)
	}

	// (e) The real registered consumer cancels the animal's open work.
	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(oblpg.NewRepository(pool, 5*time.Second)).Register(bus)
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("canceled obligations = %d, want 1 (a dead animal must not keep open vaccination work)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','due','in_progress')`,
		testTenant, goat.GoatID); got != 0 {
		t.Fatalf("still-open obligations for exited goat = %d, want 0", got)
	}

	// (f) Idempotency (AGENTS.md write-path contract): an exact replay of the decision must not
	// double-emit, and a re-delivery of the event must not double-cancel.
	if _, err := repo.RecordDecision(ctx, decision); err != nil {
		t.Fatalf("replay RecordDecision: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.exited'`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("after replay: goat.exited identity events = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.exited' AND aggregate_id=$2`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("after replay: goat.exited outbox messages = %d, want 1", got)
	}
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("re-publish goat.exited: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`,
		testTenant, goat.GoatID); got != 1 {
		t.Fatalf("after re-delivery: canceled obligations = %d, want 1", got)
	}
}

// TestNonTerminalProcurementDecisionEmitsNoGoatExited is the negative control: an ordinary
// non-terminal rejection must not exit the animal or cancel its vaccination work.
func TestNonTerminalProcurementDecisionEmitsNoGoatExited(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedProcurementCommon(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	load := createProcurementLoad(t, ctx, repo, "nonterminal-load", 1)
	goat := addProcurementGoat(t, ctx, repo, load.LoadID, ports.AddGoatToLoad{
		TenantID: testTenant, LoadID: load.LoadID, AnimalIdentifier1: strPtr("NONTERMINAL"),
		SourceEntryState: "accepted", OwnershipState: "mesha_owned", IdempotencyKey: "nonterminal-goat",
	})
	if _, err := repo.RecordDecision(ctx, ports.Decision{
		TenantID: testTenant, LoadID: load.LoadID, GoatID: goat.GoatID,
		DecisionStage: "pre_dispatch", DecisionType: domain.DecisionRejected,
		Reason:         "underweight for this load",
		DecidedAt:      time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		IdempotencyKey: "nonterminal-decision",
	}); err != nil {
		t.Fatalf("RecordDecision(non-terminal): %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.exited' AND aggregate_id=$2`,
		testTenant, goat.GoatID); got != 0 {
		t.Fatalf("goat.exited outbox messages for non-terminal decision = %d, want 0", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.exited'`,
		testTenant, goat.GoatID); got != 0 {
		t.Fatalf("goat.exited identity events for non-terminal decision = %d, want 0", got)
	}
}

func scanProcurementText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("scan text: %v", err)
	}
	return out
}
