package e2e

// Run the whole kernel-story suite with:
//
//	go test ./backend/tests/e2e/... -run TestKernelStor -v
//
// or via the helper script `backend/tests/e2e/run.sh` (same command, plus it prints the HTML
// report path on success). Each story boots its own throwaway Postgres container via
// backend/internal/platform/pgtest (Docker required; tests call pgtest.SkipIfNoDocker and skip
// cleanly when Docker is unavailable) and renders its outcome into
// backend/tests/e2e/report/index.html via TestMain in main_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	domainconsumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	domainconsumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	domainconsumerwiring "github.com/vgoats/goatos/backend/internal/domainconsumer/wiring"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	outboxports "github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	pipg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	proofpg "github.com/vgoats/goatos/backend/internal/proof/adapters/postgres"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	verifpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/verification/adapters/proofmedia"
	verifports "github.com/vgoats/goatos/backend/internal/verification/ports"
)

// Baseline fixture ids: every migrated database already has these rows (see migration
// 000001_phase_1_identity_foundation.sql), the same way the rest of the backend's integration
// tests reuse them (e.g. internal/obligation/adapters/postgres/cancel_integration_test.go's
// tenantID/meshaParty/cbePark constants). Reusing them means the harness never has to create a
// tenant/party/park from scratch.
const (
	fxTenant = "00000000-0000-4000-8000-000000000001" // baseline tenant
	fxParty  = "00000000-0000-4000-8000-000000001001" // baseline custodian party (Mesha)
	fxPark   = "00000000-0000-4000-8000-000000003001" // baseline park location (CBE, Coimbatore)
)

// Fixture bundles an ephemeral pool with the same repository/service constructors the rest of the
// backend's integration tests use, plus small seed helpers shared by all three kernel stories.
type Fixture struct {
	T    *testing.T
	Ctx  context.Context
	Pool *pgxpool.Pool

	Proto    *protopg.Repository
	Identity *identityapp.Service
	Obl      *oblpg.Repository
	Outbox   *outboxpg.Repository
	Vacc     *vaccpg.Repository
	VaccExec *vaccexecpg.Repository
	Proof    *proofpg.Repository
	PI       *pipg.Repository
	Inv      *invapp.Service
	Calendar *calendarapp.Service
	// CalendarRepo is the concrete calendar repository used for direct tests that need to verify
	// low-level storage behavior.
	CalendarRepo *calendarpg.Repository
	Bus          eventbus.Bus
	Consumer     *domainconsumerapp.Service
	Relay        *outboxapp.Service
	VerifRepo    *verifpg.Repository
	VerifMedia   verifports.MediaResolver
}

// e2eConsumerPublisher is the in-memory transport seam between the production outbox relay and
// production domain consumer. It preserves the real envelope, attributes, processed-event store,
// retry result, and published-state transition while avoiding a network dependency on Pub/Sub.
type e2eConsumerPublisher struct{ consumer *domainconsumerapp.Service }

func (p e2eConsumerPublisher) Publish(ctx context.Context, message outboxports.PublishMessage) error {
	return p.consumer.HandleMessage(ctx, domainconsumerapp.Message{
		ID:   message.OutboxID,
		Data: message.Payload,
		Attributes: map[string]string{
			"outbox_id": message.OutboxID, "event_id": message.EventID,
			"event_type": message.EventType, "tenant_id": message.TenantID,
		},
		DeliveryAttempt: 1,
	})
}

// e2eProofDownloader is a stub proof downloader for verification tests that don't need actual media.
type e2eProofDownloader struct{}

func (d e2eProofDownloader) DownloadURL(ctx context.Context, tenantID, proofID string) (string, error) {
	// Stub: return a placeholder URL. Real tests don't exercise media download.
	return "http://stub-proof-url/" + proofID, nil
}

// EnsureObjectAvailable satisfies the verdict-time evidence gate (verification/ports.
// EvidenceAvailabilityChecker via proofmedia.ObjectAvailabilityChecker). This stub stands for
// "the object is present", which is the state every e2e verification flow assumes.
func (d e2eProofDownloader) EnsureObjectAvailable(ctx context.Context, tenantID, proofID string) error {
	return nil
}

// NewFixture boots a fresh throwaway Postgres container (all committed migrations applied) and
// wires the repositories/services each story needs. The container is removed via t.Cleanup.
func NewFixture(t *testing.T) *Fixture {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	const timeout = 5 * time.Second
	proto := protopg.NewRepository(pool, timeout)
	identity := identityapp.NewService(identitypg.NewRepository(pool, timeout))
	obl := oblpg.NewRepository(pool, timeout)
	outboxRepo := outboxpg.NewRepository(pool, timeout)
	vacc := vaccpg.NewRepository(pool, timeout)
	calendarRepo := calendarpg.NewRepository(pool, timeout)
	bus := domainconsumerwiring.BuildDomainBus(pool, timeout, nil)
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("load production domain-event envelope schema: %v", err)
	}
	consumer := domainconsumerapp.NewService(bus, validator).
		WithProcessedEventStore(domainconsumerpg.NewProcessedEventStore(pool, timeout))
	relay := outboxapp.NewService(outboxRepo, e2eConsumerPublisher{consumer: consumer}, validator, outboxapp.Config{
		Limit: 100, MaxAttempts: 5, BackoffBase: time.Millisecond, BackoffMax: time.Millisecond,
	})
	verifRepo := verifpg.NewRepository(pool, timeout)
	proofRepo := proofpg.NewRepository(pool, timeout)
	verifMedia := proofmedia.NewResolver(e2eProofDownloader{})
	return &Fixture{
		T:    t,
		Ctx:  ctx,
		Pool: pool,

		Proto:        proto,
		Identity:     identity,
		Obl:          obl,
		Outbox:       outboxRepo,
		Vacc:         vacc,
		VaccExec:     vaccexecpg.NewRepository(pool, timeout),
		Proof:        proofRepo,
		PI:           pipg.NewRepository(pool, timeout),
		Inv:          invapp.NewService(invpg.NewRepository(pool, timeout)),
		Calendar:     calendarapp.NewService(calendarRepo),
		CalendarRepo: calendarRepo,
		Bus:          bus,
		Consumer:     consumer,
		Relay:        relay,
		VerifRepo:    verifRepo,
		VerifMedia:   verifMedia,
	}
}

// RelayOutboxEvents relays pending outbox messages through the domain consumer (and thus
// all registered business handlers on the domain bus). This is used in E2E tests to drive
// production event-based state transitions after API calls that publish outbox events.
//
// It drains until empty (RunUntilDrained), because relaying an event can transitively
// enqueue a NEW outbox row that a single pass would miss. Concretely the production chain is
// multi-hop: verification.item.closed -> VerificationHandler -> CompletionService.AcceptExisting
// -> obligation.MarkCompleted writes a fresh vaccination.completed row to the OUTBOX in the same
// tx -> that row must then be drained to reach VaccinationCompletedHandler, which schedules the
// successor (booster) obligation. A single RunOnce would relay the close event but leave the
// just-written vaccination.completed row pending, so the successor obligation would never be
// created. Draining to empty faithfully mirrors the always-running production outbox relay.
func (f *Fixture) RelayOutboxEvents() {
	f.T.Helper()
	if _, err := f.Relay.RunUntilDrained(f.Ctx); err != nil {
		f.T.Logf("warning: relay pending outbox events: %v", err)
	}
}

// PublishGoatEvent injects an allowed initial-input envelope through the production domain
// consumer. Passing a deterministic occurredAt fast-forwards business time without wall-clock
// sleeps; all derived obligations still come from the registered production handler.
func (f *Fixture) PublishGoatEvent(eventType, goatID string, occurredAt time.Time) {
	f.T.Helper()
	eventID := uuid.NewString()
	envelope, err := json.Marshal(map[string]any{
		"event_id":        eventID,
		"event_type":      eventType,
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json",
		"aggregate_type":  "goat",
		"aggregate_id":    goatID,
		"occurred_at":     occurredAt.UTC().Format(time.RFC3339Nano),
		"recorded_at":     occurredAt.UTC().Format(time.RFC3339Nano),
		"producer":        map[string]any{"service": "goatos-e2e", "module": "fixture-input"},
		"idempotency_key": "e2e-input:" + eventType + ":" + goatID + ":" + occurredAt.UTC().Format(time.RFC3339Nano),
		"actor":           map[string]any{"actor_type": "system_rule", "actor_ref": "e2e-initial-input"},
		"subject_type":    "goat",
		"subject_id":      goatID,
		"visibility_scope": map[string]any{
			"tenant_id": fxTenant,
		},
		"evidence_refs": []map[string]string{},
		"payload":       map[string]any{"goat_id": goatID},
		"trace_id":      "trace-e2e-input-" + eventID,
	})
	if err != nil {
		f.T.Fatalf("encode %s fixture envelope for %s: %v", eventType, goatID, err)
	}
	if err := f.Consumer.HandleMessage(f.Ctx, domainconsumerapp.Message{
		ID:   "fixture-" + eventID,
		Data: envelope,
		Attributes: map[string]string{
			"event_id":   eventID,
			"event_type": eventType,
			"tenant_id":  fxTenant,
		},
		DeliveryAttempt: 1,
	}); err != nil {
		f.T.Fatalf("consume %s fixture envelope for %s: %v", eventType, goatID, err)
	}
}

func (f *Fixture) goatRowVersion(goatID string) int {
	f.T.Helper()
	var version int
	if err := f.Pool.QueryRow(f.Ctx,
		`SELECT row_version FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatID).Scan(&version); err != nil {
		f.T.Fatalf("read goat row version %s: %v", goatID, err)
	}
	return version
}

// ChangeGoatHealth uses the production identity command, then delivers its durable outbox event to
// the real vaccination recheck consumer.
func (f *Fixture) ChangeGoatHealth(goatID, status, key string, occurredAt time.Time) {
	f.T.Helper()
	body, _ := json.Marshal(map[string]any{
		"health_status": status,
		"reason":        "E2E health transition through identity service",
		"occurred_at":   occurredAt,
		"evidence_refs": []map[string]string{{"evidence_type": "source_record", "evidence_id": "e2e-health-" + key}},
		"row_version":   f.goatRowVersion(goatID),
	})
	if _, err := f.Identity.HealthGoat(f.Ctx, identityapp.HealthGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: key, TraceID: "trace-" + key,
		GoatID: goatID, RawBody: body,
	}); err != nil {
		f.T.Fatalf("change goat health %s to %s: %v", goatID, status, err)
	}
	f.dispatchIdentityOutbox(goatID, vaccapp.EventGoatHealthChanged)
}

// ChangeGoatReproductive uses the production identity reproductive command and delivers its
// durable event to the vaccination recheck consumer.
func (f *Fixture) ChangeGoatReproductive(goatID, status, key string, occurredAt time.Time, breedingDate *time.Time) {
	f.T.Helper()
	bodyMap := map[string]any{
		"reproductive_status": status,
		"reason":              "E2E reproductive transition through identity service",
		"occurred_at":         occurredAt,
		"evidence_refs":       []map[string]string{{"evidence_type": "source_record", "evidence_id": "e2e-reproductive-" + key}},
		"row_version":         f.goatRowVersion(goatID),
	}
	if breedingDate != nil {
		bodyMap["breeding_date"] = breedingDate.Format("2006-01-02")
	}
	body, _ := json.Marshal(bodyMap)
	if _, err := f.Identity.ReproductiveGoat(f.Ctx, identityapp.ReproductiveGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: key, TraceID: "trace-" + key,
		GoatID: goatID, RawBody: body,
	}); err != nil {
		f.T.Fatalf("change goat reproductive status %s to %s: %v", goatID, status, err)
	}
	f.dispatchIdentityOutbox(goatID, vaccapp.EventGoatReproductiveChanged)
}

// MoveGoat uses the production identity move command and delivers the emitted location event to
// both production consumers: obligation re-scope and vaccination recheck.
func (f *Fixture) MoveGoat(goatID, shedID, key string, occurredAt time.Time) {
	f.T.Helper()
	body, _ := json.Marshal(map[string]any{
		"park_id":       fxPark,
		"shed_id":       shedID,
		"reason":        "E2E shed move through identity service",
		"occurred_at":   occurredAt,
		"evidence_refs": []map[string]string{{"evidence_type": "source_record", "evidence_id": "e2e-move-" + key}},
		"row_version":   f.goatRowVersion(goatID),
	})
	if _, err := f.Identity.MoveGoat(f.Ctx, identityapp.MoveGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: key, TraceID: "trace-" + key,
		GoatID: goatID, RawBody: body,
	}); err != nil {
		f.T.Fatalf("move goat %s to %s: %v", goatID, shedID, err)
	}
	f.dispatchIdentityOutbox(goatID, vaccapp.EventGoatLocationChanged)
}

// ExitGoat uses the production identity exit command (including the critical-death guardrail path)
// and delivers the emitted goat.exited envelope to SM-3.
func (f *Fixture) ExitGoat(goatID, lifecycle, key string, occurredAt time.Time) {
	f.T.Helper()
	reasonByLifecycle := map[string]string{"dead": "died", "sold": "sold", "culled": "culled", "transferred": "transferred", "lost": "lost"}
	exitReason := reasonByLifecycle[lifecycle]
	body, _ := json.Marshal(map[string]any{
		"lifecycle_status": lifecycle,
		"exit_reason":      exitReason,
		"reason":           "E2E lifecycle exit through identity service",
		"occurred_at":      occurredAt,
		"evidence_refs":    []map[string]string{{"evidence_type": "source_record", "evidence_id": "e2e-exit-" + key}},
		"row_version":      f.goatRowVersion(goatID),
	})
	input := identityapp.ExitGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: key, TraceID: "trace-" + key,
		GoatID: goatID, RawBody: body,
	}
	var err error
	if lifecycle == "dead" {
		_, err = f.Identity.CriticalDeathExit(f.Ctx, input)
	} else {
		_, err = f.Identity.ExitGoat(f.Ctx, input)
	}
	if err != nil {
		f.T.Fatalf("exit goat %s as %s: %v", goatID, lifecycle, err)
	}
	f.dispatchIdentityOutbox(goatID, oblapp.EventGoatExited)
}

func (f *Fixture) dispatchIdentityOutbox(goatID, eventType string) {
	f.T.Helper()
	f.dispatchOutbox(goatID, eventType)
}

// dispatchOutbox feeds ONE real durable outbox envelope through the production domain-consumer
// service. This exercises schema validation, durable processed-event claim/dedupe/finalization,
// and the same registered business handlers used by the deployed subscriber, without draining
// unrelated pending outbox rows that belong to other story setup steps. The relay's own
// claim/publish state machine is covered at its adapter seam; here the E2E proof target is the
// durable envelope + consumer path.
func (f *Fixture) dispatchOutbox(aggregateID, eventType string) {
	f.T.Helper()
	var outboxID string
	var payload []byte
	var status string
	if err := f.Pool.QueryRow(f.Ctx, `
SELECT outbox_id::text, payload, status
FROM outbox_messages
WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type=$3
ORDER BY created_at DESC
LIMIT 1`, fxTenant, aggregateID, eventType).Scan(&outboxID, &payload, &status); err != nil {
		f.T.Fatalf("read %s outbox for %s: %v", eventType, aggregateID, err)
	}
	if status != "published" {
		now := time.Now().UTC()
		claimed, err := f.Outbox.ClaimMessage(f.Ctx, outboxID, now)
		if err != nil {
			f.T.Fatalf("claim %s outbox for %s: %v", eventType, aggregateID, err)
		}
		if err := f.Consumer.HandleMessage(f.Ctx, domainconsumerapp.Message{
			ID:   claimed.OutboxID,
			Data: claimed.Payload,
			Attributes: map[string]string{
				"outbox_id":  claimed.OutboxID,
				"event_id":   claimed.EventID,
				"event_type": claimed.EventType,
				"tenant_id":  claimed.TenantID,
			},
			DeliveryAttempt: 1,
		}); err != nil {
			f.T.Fatalf("consume %s outbox for %s: %v", eventType, aggregateID, err)
		}
		if err := f.Outbox.MarkPublished(f.Ctx, claimed.OutboxID, now); err != nil {
			f.T.Fatalf("mark %s outbox published for %s: %v", eventType, aggregateID, err)
		}
		outboxID = claimed.OutboxID
		payload = claimed.Payload
	}

	// A second call deliberately models Pub/Sub redelivery of the same envelope.
	// The production consumer must skip it through its durable dedupe row.
	if err := f.Consumer.HandleMessage(f.Ctx, domainconsumerapp.Message{
		ID:   outboxID,
		Data: payload,
		Attributes: map[string]string{
			"outbox_id":  outboxID,
			"event_type": eventType,
			"tenant_id":  fxTenant,
		},
		DeliveryAttempt: 2,
	}); err != nil {
		f.T.Fatalf("consume %s outbox for %s: %v", eventType, aggregateID, err)
	}
}

func (f *Fixture) exec(label, sql string, args ...any) {
	f.T.Helper()
	if _, err := f.Pool.Exec(f.Ctx, sql, args...); err != nil {
		f.T.Fatalf("seed %s: %v", label, err)
	}
}

func (f *Fixture) scanText(sql string, args ...any) string {
	f.T.Helper()
	var v string
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&v); err != nil {
		f.T.Fatalf("scan text (%s): %v", sql, err)
	}
	return v
}

func (f *Fixture) countRows(sql string, args ...any) int {
	f.T.Helper()
	var n int
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&n); err != nil {
		f.T.Fatalf("count rows (%s): %v", sql, err)
	}
	return n
}

func (f *Fixture) scanTime(sql string, args ...any) time.Time {
	f.T.Helper()
	var v time.Time
	if err := f.Pool.QueryRow(f.Ctx, sql, args...).Scan(&v); err != nil {
		f.T.Fatalf("scan time (%s): %v", sql, err)
	}
	return v
}

// SeedShed creates a shed location under the baseline CBE park plus the animal-stage lookup, shed
// profile, and operational attributes (usable for vaccination, not ICU/quarantine) that the
// vaccination-execution and process-integrity read models need to resolve a park/shed row --
// mirroring internal/processintegrity/adapters/postgres/repository_integration_test.go's
// seedProcessIntegrityProjection and internal/vaccinationexecution/adapters/postgres/
// repository_integration_test.go's seedVaccinationExecutionProjection.
func (f *Fixture) SeedShed(shedID, shedCode, stageID string) {
	f.T.Helper()
	f.exec("shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("animal stage lookup",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1', 'K1 kids', 'active')
		 ON CONFLICT (animal_stage_id) DO NOTHING`,
		stageID, fxTenant)
	f.exec("shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedID, fxTenant, stageID)
	f.exec("shed operational attributes",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedID)
}

// SeedWorkforce seeds one operator (at the shed), one park head, and one verifier (at the park) --
// the minimal workforce roster the process-integrity/execution read models join against for
// owner/verifier display and the "blocked" work-state gate.
func (f *Fixture) SeedWorkforce(operatorID, parkHeadID, verifierID, shedID string) {
	f.T.Helper()
	f.exec("operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-E2E', 'E2E Operator', 'active', 'operator', $3)`,
		operatorID, fxTenant, shedID)
	f.exec("park head",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'PH-E2E', 'E2E Park Head', 'active', 'park_head', $3)`,
		parkHeadID, fxTenant, fxPark)
	f.exec("verifier",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'VER-E2E', 'E2E Verifier', 'active', 'verifier', $3)`,
		verifierID, fxTenant, fxPark)
}

// GoatSpec is the minimal, realistic goat fixture every story needs. Zero-value fields fall back
// to sensible defaults (alive/healthy/K1/goat).
type GoatSpec struct {
	GoatID             string
	ShedID             string // optional; when set, current_location_id/shed_id both point here
	Lifecycle          string // default "alive"
	Health             string // default "healthy"
	Stage              string // default "K1"
	Species            string // default "goat"
	ReproductiveStatus string
	BreedingDate       *time.Time
	OriginType         string
	EntryDate          *time.Time
	DOB                *time.Time
	// AgeBand is the animal's kid/adult classification at INTAKE ('kid'/'adult'; empty stores NULL).
	// It belongs on the initial insert, not a later UPDATE: a story that changes a goat's band must
	// do it through the production identity path, and e2e-kernel-integrity enforces that.
	AgeBand     string
	NoDOB       bool // when true, dob is stored NULL (missing-DOB defer stories)
	NoEntryDate bool // when true, entry_date is NULL (missing-entry-date defer stories; use procured origin)
}

// SeedGoat inserts one goat row directly (goats are owned by the identity module, exactly like the
// rest of the backend's integration tests seed them -- see e.g. generation_integration_test.go's
// seedGenGoat). current_location_id follows the shed when present, else the park.
func (f *Fixture) SeedGoat(spec GoatSpec) {
	f.T.Helper()
	lifecycle := spec.Lifecycle
	if lifecycle == "" {
		lifecycle = "alive"
	}
	health := spec.Health
	if health == "" {
		health = "healthy"
	}
	stage := spec.Stage
	if stage == "" {
		stage = "K1"
	}
	species := spec.Species
	if species == "" {
		species = "goat"
	}
	var shedID *string
	if spec.ShedID != "" {
		shedID = &spec.ShedID
	}
	var repro *string
	if spec.ReproductiveStatus != "" {
		repro = &spec.ReproductiveStatus
	}
	ageBand := nullIfEmpty(spec.AgeBand)
	if spec.NoDOB {
		f.exec("goat "+spec.GoatID,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
			    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date, age_band)
			 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, NULL, $10, $11::date, $12, $13::date, $14)`,
			spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage,
			repro, spec.BreedingDate, nullIfEmpty(spec.OriginType), spec.EntryDate, ageBand)
		return
	}
	if spec.NoEntryDate {
		origin := spec.OriginType
		if origin == "" {
			origin = "procured"
		}
		f.exec("goat "+spec.GoatID,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
			    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date, age_band)
			 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, $10::date, $11, $12::date, $13, NULL, $14)`,
			spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage, spec.DOB,
			repro, spec.BreedingDate, origin, ageBand)
		return
	}
	f.exec("goat "+spec.GoatID,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, reproductive_status, breeding_date, origin_type, entry_date, age_band)
		 VALUES ($1, $2, $3, $4, $5, $6, 'female', COALESCE($7::uuid, $8::uuid), $8, $7, $9, $10::date, $11, $12::date, $13, $14::date, $15)`,
		spec.GoatID, fxTenant, lifecycle, health, species, fxParty, shedID, fxPark, stage, spec.DOB,
		repro, spec.BreedingDate, nullIfEmpty(spec.OriginType), spec.EntryDate, ageBand)
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// SeedICUShed creates a shed marked ICU (location unusable for vaccination until the goat leaves).
func (f *Fixture) SeedICUShed(shedID, shedCode, stageID string) {
	f.T.Helper()
	f.SeedShed(shedID, shedCode, stageID)
	f.exec("icu shed attributes",
		`UPDATE location_operational_attributes
		    SET is_icu = true, usable_for_vaccination = false
		  WHERE tenant_id = $1 AND location_id = $2`,
		fxTenant, shedID)
}

// PublishSimpleProtocol creates and publishes a one-rule vaccination protocol version (a single
// birth-age primary dose), mirroring the rule shape used throughout
// internal/vaccination/adapters/postgres/generation_integration_test.go. When deferStates is
// non-empty it is threaded into the rule_dsl's top-level eligibility.defer_states, exactly like
// TestGoatRecheckDefersExistingScheduledObligation, so a goat whose health_status/lifecycle_status
// matches one of those states gets its obligation deferred (and reopened on recovery) by the real
// generation service.
func (f *Fixture) PublishSimpleProtocol(code string, offsetDays, dueWindowDays int32, deferStates []string) (versionID, ruleID string) {
	f.T.Helper()
	protoID, err := f.Proto.CreateDefinition(f.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		f.T.Fatalf("create protocol definition %s: %v", code, err)
	}

	ruleDSL := []byte(`{}`)
	if len(deferStates) > 0 {
		states, mErr := json.Marshal(deferStates)
		if mErr != nil {
			f.T.Fatalf("marshal defer states: %v", mErr)
		}
		ruleDSL = []byte(fmt.Sprintf(`{"eligibility":{"defer_states":%s}}`, states))
	}
	versionID, err = f.Proto.CreateVersion(f.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol version %s: %v", code, err)
	}
	ruleID, err = f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
		TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: offsetDays, DueWindowDays: dueWindowDays,
		Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol rule %s: %v", code, err)
	}
	if err := f.Proto.PublishVersion(f.Ctx, fxTenant, versionID, nil); err != nil {
		f.T.Fatalf("publish protocol version %s: %v", code, err)
	}
	return versionID, ruleID
}

// PublishBoosterProtocol creates and publishes a two-dose vaccination protocol version: a
// birth-age primary dose (sequence 1) plus a booster dose (sequence 2) triggered
// after_previous_completion. This is the multi-dose shape that lets SM-7 schedule a successor
// obligation once the primary dose is completed and accepted — the single-rule PublishSimpleProtocol
// has Repeat="none" and no higher sequence, so it can never produce a successor. Returns the version
// id plus the primary and booster rule ids so callers can assert on each separately.
func (f *Fixture) PublishBoosterProtocol(code string, primaryOffsetDays, dueWindowDays, boosterGapDays int32) (versionID, primaryRuleID, boosterRuleID string) {
	f.T.Helper()
	protoID, err := f.Proto.CreateDefinition(f.Ctx, protodomain.NewDefinition{
		TenantID: fxTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		f.T.Fatalf("create protocol definition %s: %v", code, err)
	}
	versionID, err = f.Proto.CreateVersion(f.Ctx, protodomain.NewVersion{
		TenantID: fxTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create protocol version %s: %v", code, err)
	}
	primaryRuleID, err = f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
		TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: primaryOffsetDays, DueWindowDays: dueWindowDays,
		Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create primary protocol rule %s: %v", code, err)
	}
	boosterRuleID, err = f.Proto.CreateRule(f.Ctx, protodomain.NewRule{
		TenantID: fxTenant, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "after_previous_completion", OffsetDays: boosterGapDays, DueWindowDays: dueWindowDays,
		MinGapDays: boosterGapDays, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		f.T.Fatalf("create booster protocol rule %s: %v", code, err)
	}
	if err := f.Proto.PublishVersion(f.Ctx, fxTenant, versionID, nil); err != nil {
		f.T.Fatalf("publish protocol version %s: %v", code, err)
	}
	return versionID, primaryRuleID, boosterRuleID
}

// DispatchVaccinationCompleted consumes the durable envelope through the production consumer and
// vaccination.completed handler. The next obligation is created by SM-7, never by test SQL.
func (f *Fixture) DispatchVaccinationCompleted(obligationID string) {
	f.T.Helper()
	f.dispatchOutbox(obligationID, "vaccination.completed")
}

// ---- Story narration / assertion recorder ----

// Story records one kernel story's narrative steps and assertions as a test runs, both driving
// real Go test pass/fail (via t.Errorf on a failed assertion) and building the StoryResult that
// gets rendered into the HTML report on Finish.
type Story struct {
	t      *testing.T
	result StoryResult
}

// NewStory starts recording a new story. Call Finish (typically via defer) to file it into the
// shared report.
func NewStory(t *testing.T, id, title, narrative string) *Story {
	t.Helper()
	return &Story{t: t, result: StoryResult{ID: id, Title: title, Narrative: narrative, Pass: true}}
}

// Certify declares the highest surface this story actually enters through. It is rendered in the
// Pages report so a kernel story can never be mistaken for HTTP, browser, or Android proof.
func (s *Story) Certify(surface string) {
	s.t.Helper()
	if surface != "" {
		s.result.Certification = surface
	}
}

// Step opens a new narrated step. Subsequent Assert calls attach to this step until the next Step.
func (s *Story) Step(name, narrative string) {
	s.t.Helper()
	s.result.Steps = append(s.result.Steps, StepResult{Name: name, Narrative: narrative})
}

// Assert records one pass/fail check against the current step (creating a default step if Step
// was never called) and fails the Go test (non-fatally, via t.Errorf) when cond is false so the
// suite still runs to completion and the report shows every check, not just the first failure.
func (s *Story) Assert(description string, cond bool, detailFormat string, args ...any) bool {
	s.t.Helper()
	if len(s.result.Steps) == 0 {
		s.result.Steps = append(s.result.Steps, StepResult{Name: "Result"})
	}
	detail := fmt.Sprintf(detailFormat, args...)
	idx := len(s.result.Steps) - 1
	s.result.Steps[idx].Assertions = append(s.result.Steps[idx].Assertions, AssertionResult{
		Description: description, Pass: cond, Detail: detail,
	})
	if !cond {
		s.result.Pass = false
		s.t.Errorf("%s / %s: %s (%s)", s.result.Steps[idx].Name, description, "FAILED", detail)
	}
	return cond
}

// Finish files the story into the shared report. Safe to call even after t.Fatalf elsewhere in the
// same goroutine (it still runs as a deferred call), so a setup failure still yields a partial,
// honest report instead of a silently missing story.
func (s *Story) Finish() {
	s.t.Helper()
	if s.t.Failed() {
		s.result.Pass = false
	}
	globalReport.Add(s.result)
}
