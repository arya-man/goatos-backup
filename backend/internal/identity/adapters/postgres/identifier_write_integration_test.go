package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	addIdentifierCommandName    = "addGoatIdentifier"
	retireIdentifierCommandName = "retireGoatIdentifier"
)

func TestIdentifierWritePathWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	t.Run("add success writes identifier decision event audit outbox and schema-valid payloads", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-write-0001", goatID, "rfid", " rfid-synthetic-0001 ", "global:rfid", false, goatRowVersion(t, pool, goatID))
		result, err := repo.AddGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("AddGoatIdentifier: %v", err)
		}
		if result.Replayed || result.Goat.GoatID != goatID || result.Decision.DecisionType != "attach_identifier" {
			t.Fatalf("unexpected add result: %#v", result)
		}
		if len(result.Identifiers) != 1 || result.Identifiers[0].IdentifierType != "rfid" || result.Identifiers[0].Status != "active" {
			t.Fatalf("unexpected identifiers: %#v", result.Identifiers)
		}
		identifierID := result.Identifiers[0].IdentifierID
		if got := goatRowVersion(t, pool, goatID); got != 2 {
			t.Fatalf("goat row_version = %d, want 2", got)
		}
		var normalized, status string
		if err := pool.QueryRow(ctx, `SELECT normalized_value, status FROM goat_identifiers WHERE identifier_id = $1`, identifierID).Scan(&normalized, &status); err != nil {
			t.Fatal(err)
		}
		if normalized != "RFID-SYNTHETIC-0001" || status != "active" {
			t.Fatalf("identifier normalized/status = %s/%s", normalized, status)
		}
		assertIdentifierMutationRows(t, pool, cmd.StoredIdempotencyKey, goatID, identifierID, result.Decision.DecisionID, "attach", "goat.identifier.added")
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, identifierID)
	})

	t.Run("add exact replay rebuilds response and changed body conflicts", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-write-0002", goatID, "rfid", "rfid-synthetic-0002", "global:rfid", false, 1)
		first, err := repo.AddGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("first add: %v", err)
		}
		replay, err := repo.AddGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("replay add: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != first.Identifiers[0].IdentifierID {
			t.Fatalf("unexpected replay metadata: %#v", replay)
		}
		if replay.Decision.DecisionID != first.Decision.DecisionID || replay.Events[0].EventID != first.Events[0].EventID {
			t.Fatalf("replay did not refetch original decision/event: first=%#v replay=%#v", first, replay)
		}
		changed := addIdentifierCommand(t, meshaTenant, "idem-add-write-0002", goatID, "rfid", "rfid-synthetic-0002-changed", "global:rfid", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("duplicate active rfid rolls back goat guard and idempotency", func(t *testing.T) {
		goatA := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		goatB := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-rfid-0001", goatA, "rfid", "rfid-synthetic-dupe", "global:rfid", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first rfid add: %v", err)
		}
		dupe := addIdentifierCommand(t, meshaTenant, "idem-add-rfid-0002", goatB, "rfid", " RFID-SYNTHETIC-DUPE ", "global:rfid", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, dupe); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate RFID write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatB); got != 1 {
			t.Fatalf("duplicate RFID guard was not rolled back, row_version=%d", got)
		}
		assertNoRows(t, pool, "idempotency after duplicate RFID", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", dupe.StoredIdempotencyKey)
		assertNoRows(t, pool, "identifier after duplicate RFID", "SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = $2", goatB, "RFID-SYNTHETIC-DUPE")
	})

	t.Run("duplicate old tag same scope fails while different scope succeeds", func(t *testing.T) {
		goatA := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		goatB := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-oldtag-0001", goatA, "old_tag", "synthetic-oldtag-dupe", "park:CBE", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first old_tag add: %v", err)
		}
		sameScope := addIdentifierCommand(t, meshaTenant, "idem-add-oldtag-0002", goatB, "old_tag", "synthetic-oldtag-dupe", "park:CBE", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, sameScope); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate old_tag write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatB); got != 1 {
			t.Fatalf("same-scope duplicate was not rolled back, row_version=%d", got)
		}
		differentScope := addIdentifierCommand(t, meshaTenant, "idem-add-oldtag-0003", goatB, "old_tag", "synthetic-oldtag-dupe", "park:CPT", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, differentScope); err != nil {
			t.Fatalf("different-scope old_tag should succeed: %v", err)
		}
	})

	t.Run("primary identifier conflict rolls back row version", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-primary-0001", goatID, "old_tag", "synthetic-primary-1", "park:CBE", true, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first primary add: %v", err)
		}
		rowVersion := goatRowVersion(t, pool, goatID)
		conflict := addIdentifierCommand(t, meshaTenant, "idem-add-primary-0002", goatID, "old_tag", "synthetic-primary-2", "park:CPT", true, rowVersion)
		if _, err := repo.AddGoatIdentifier(ctx, conflict); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected primary write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatID); got != rowVersion {
			t.Fatalf("primary conflict did not roll back goat row_version: got %d want %d", got, rowVersion)
		}
	})

	t.Run("add rejects merged stale and wrong tenant goats", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		mergedID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		if _, err := pool.Exec(ctx, `UPDATE goats SET identity_state = 'merged', merged_into_goat_id = $1 WHERE goat_id = $2`, survivorID, mergedID); err != nil {
			t.Fatal(err)
		}
		merged := addIdentifierCommand(t, meshaTenant, "idem-add-merged-0001", mergedID, "rfid", "rfid-synthetic-merged", "global:rfid", false, goatRowVersion(t, pool, mergedID))
		if _, err := repo.AddGoatIdentifier(ctx, merged); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected merged goat write conflict, got %v", err)
		}

		staleGoat := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		stale := addIdentifierCommand(t, meshaTenant, "idem-add-stale-0001", staleGoat, "rfid", "rfid-synthetic-stale", "global:rfid", false, 2)
		if _, err := repo.AddGoatIdentifier(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale row_version conflict, got %v", err)
		}

		wrongTenant := addIdentifierCommand(t, secondTenant, "idem-add-wrongtenant-0001", staleGoat, "rfid", "rfid-synthetic-wrongtenant", "global:rfid", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, wrongTenant); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}
	})

	t.Run("add forced failure rolls back identifier decision event audit outbox and idempotency", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-rollback-0001", goatID, "rfid", "rfid-synthetic-rollback", "global:rfid", false, 1)
		cmd.TraceID = "trace-add-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced add rollback") }
		_, err := repo.AddGoatIdentifier(ctx, cmd)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced add rollback error")
		}
		if got := goatRowVersion(t, pool, goatID); got != 1 {
			t.Fatalf("goat row_version after add rollback = %d", got)
		}
		assertNoRows(t, pool, "identifier after add rollback", "SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = $2", goatID, "RFID-SYNTHETIC-ROLLBACK")
		assertNoRows(t, pool, "idempotency after add rollback", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "decision after add rollback", "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", cmd.TraceID)
		assertNoRows(t, pool, "event after add rollback", "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after add rollback", "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID)
		assertNoRows(t, pool, "outbox after add rollback", "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID)
	})

	t.Run("retire success replay and changed body conflict", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0001", goatID, "old_tag", "synthetic-retire-1", "park:CBE", false, 1))
		if err != nil {
			t.Fatalf("add for retire: %v", err)
		}
		identifierID := added.Identifiers[0].IdentifierID
		cmd := retireIdentifierCommand(t, meshaTenant, "idem-retire-write-0001", goatID, identifierID, goatRowVersion(t, pool, goatID), "synthetic retire after source review")
		result, err := repo.RetireGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("RetireGoatIdentifier: %v", err)
		}
		if result.Replayed || result.Decision.DecisionType != "retire_identifier" || result.Identifiers[0].Status != "retired" || result.Identifiers[0].ValidTo == nil {
			t.Fatalf("unexpected retire result: %#v", result)
		}
		if got := goatRowVersion(t, pool, goatID); got != 3 {
			t.Fatalf("goat row_version = %d, want 3", got)
		}
		assertIdentifierMutationRows(t, pool, cmd.StoredIdempotencyKey, goatID, identifierID, result.Decision.DecisionID, "retire", "goat.identifier.retired")
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, identifierID)

		replay, err := repo.RetireGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("retire replay: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != identifierID || replay.Decision.DecisionID != result.Decision.DecisionID {
			t.Fatalf("unexpected retire replay: %#v", replay)
		}
		changed := retireIdentifierCommand(t, meshaTenant, "idem-retire-write-0001", goatID, identifierID, 2, "synthetic changed retire reason")
		if _, err := repo.RetireGoatIdentifier(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected retire idempotency conflict, got %v", err)
		}
	})

	t.Run("retire rejects already retired stale wrong goat and wrong tenant", func(t *testing.T) {
		goatA := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0002", goatA, "old_tag", "synthetic-retire-2", "park:CBE", false, 1))
		if err != nil {
			t.Fatalf("add for retire variants: %v", err)
		}
		identifierID := added.Identifiers[0].IdentifierID
		firstRetire := retireIdentifierCommand(t, meshaTenant, "idem-retire-variant-0001", goatA, identifierID, goatRowVersion(t, pool, goatA), "synthetic first retire")
		if _, err := repo.RetireGoatIdentifier(ctx, firstRetire); err != nil {
			t.Fatalf("first retire: %v", err)
		}
		already := retireIdentifierCommand(t, meshaTenant, "idem-retire-variant-0002", goatA, identifierID, goatRowVersion(t, pool, goatA), "synthetic already retired")
		if _, err := repo.RetireGoatIdentifier(ctx, already); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected already-retired write conflict, got %v", err)
		}

		activeGoat := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		active, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0003", activeGoat, "old_tag", "synthetic-retire-3", "park:CBE", false, 1))
		if err != nil {
			t.Fatalf("add active for stale/wrong goat: %v", err)
		}
		stale := retireIdentifierCommand(t, meshaTenant, "idem-retire-variant-0003", activeGoat, active.Identifiers[0].IdentifierID, 1, "synthetic stale retire")
		if _, err := repo.RetireGoatIdentifier(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale retire conflict, got %v", err)
		}
		wrongGoat := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		wrongGoatCmd := retireIdentifierCommand(t, meshaTenant, "idem-retire-variant-0004", wrongGoat, active.Identifiers[0].IdentifierID, 1, "synthetic wrong goat retire")
		if _, err := repo.RetireGoatIdentifier(ctx, wrongGoatCmd); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong goat not found, got %v", err)
		}
		if got := goatRowVersion(t, pool, wrongGoat); got != 1 {
			t.Fatalf("wrong goat guard did not roll back, row_version=%d", got)
		}
		wrongTenant := retireIdentifierCommand(t, secondTenant, "idem-retire-variant-0005", activeGoat, active.Identifiers[0].IdentifierID, goatRowVersion(t, pool, activeGoat), "synthetic wrong tenant retire")
		if _, err := repo.RetireGoatIdentifier(ctx, wrongTenant); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}
	})

	t.Run("retire forced failure rolls back identifier goat event audit outbox and idempotency", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-rollback-0001", goatID, "old_tag", "synthetic-retire-rollback", "park:CBE", false, 1))
		if err != nil {
			t.Fatalf("add for retire rollback: %v", err)
		}
		rowVersion := goatRowVersion(t, pool, goatID)
		identifierID := added.Identifiers[0].IdentifierID
		cmd := retireIdentifierCommand(t, meshaTenant, "idem-retire-rollback-0001", goatID, identifierID, rowVersion, "synthetic retire rollback")
		cmd.TraceID = "trace-retire-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced retire rollback") }
		_, err = repo.RetireGoatIdentifier(ctx, cmd)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced retire rollback error")
		}
		if got := goatRowVersion(t, pool, goatID); got != rowVersion {
			t.Fatalf("goat row_version after retire rollback = %d want %d", got, rowVersion)
		}
		var status string
		var validToSet bool
		if err := pool.QueryRow(ctx, `SELECT status, valid_to IS NOT NULL FROM goat_identifiers WHERE identifier_id = $1`, identifierID).Scan(&status, &validToSet); err != nil {
			t.Fatal(err)
		}
		if status != "active" || validToSet {
			t.Fatalf("identifier mutated despite rollback: status=%s valid_to_set=%v", status, validToSet)
		}
		assertNoRows(t, pool, "idempotency after retire rollback", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "decision after retire rollback", "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", cmd.TraceID)
		assertNoRows(t, pool, "event after retire rollback", "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after retire rollback", "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID)
		assertNoRows(t, pool, "outbox after retire rollback", "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID)
	})

	t.Run("goat outbox trigger rejects missing goat identity event", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		_, err := pool.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id,
  event_id,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id,
  topic,
  payload,
  headers,
  idempotency_key,
  trace_id,
  status
) VALUES (
  $1,
  gen_random_uuid(),
  'goat.identifier.added',
  'v1',
  'goat',
  $2,
  'identity.events',
  '{}'::jsonb,
  '{}'::jsonb,
  'synthetic-malformed-goat-outbox',
  'trace-malformed-goat-outbox',
  'pending'
)`, meshaTenant, goatID)
		if err == nil {
			t.Fatal("expected goat outbox trigger to reject missing goat_identity_events row")
		}
	})
}

func insertSyntheticGoat(t *testing.T, pool *pgxpool.Pool, tenantID, parkID string) string {
	t.Helper()
	var goatID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO goats (
  tenant_id,
  lifecycle_status,
  identity_state,
  custodian_party_id,
  current_location_id,
  park_id,
  breed,
  sex
) VALUES (
  $1,
  'alive',
  'clean',
  $2,
  $3,
  $3,
  'Synthetic Boer',
  'female'
)
RETURNING goat_id::text`, tenantID, meshaParty, parkID).Scan(&goatID); err != nil {
		t.Fatal(err)
	}
	return goatID
}

func goatRowVersion(t *testing.T, pool *pgxpool.Pool, goatID string) int {
	t.Helper()
	var rowVersion int
	if err := pool.QueryRow(context.Background(), `SELECT row_version FROM goats WHERE goat_id = $1`, goatID).Scan(&rowVersion); err != nil {
		t.Fatal(err)
	}
	return rowVersion
}

func addIdentifierCommand(t *testing.T, tenantID, key, goatID, identifierType, value, scopeKey string, primary bool, rowVersion int) ports.AddGoatIdentifierCommand {
	t.Helper()
	identifierValue := strings.TrimSpace(value)
	evidenceRefs := []domain.EvidenceRef{identifierEvidenceRef()}
	body := map[string]any{
		"identifier_type":     identifierType,
		"identifier_value":    value,
		"scope_key":           scopeKey,
		"is_primary_for_goat": primary,
		"evidence_refs":       evidenceRefs,
		"row_version":         rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	route := "/admin/goats/" + goatID + "/identifiers"
	hash, err := app.CanonicalRequestHashWithSubject(tenantID, addIdentifierCommandName, route, goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.AddGoatIdentifierCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":" + addIdentifierCommandName + ":" + goatID + ":" + key,
		IdempotencyScope:     addIdentifierCommandName,
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		IdentifierType:       identifierType,
		IdentifierValue:      identifierValue,
		NormalizedValue:      normalizeIdentifierForTest(identifierType, identifierValue),
		ScopeKey:             strings.TrimSpace(scopeKey),
		IsPrimaryForGoat:     primary,
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
		Reason:               "Admin attached identifier with evidence.",
	}
}

func retireIdentifierCommand(t *testing.T, tenantID, key, goatID, identifierID string, rowVersion int, reason string) ports.RetireGoatIdentifierCommand {
	t.Helper()
	evidenceRefs := []domain.EvidenceRef{identifierEvidenceRef()}
	body := map[string]any{
		"reason":        reason,
		"evidence_refs": evidenceRefs,
		"row_version":   rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	route := "/admin/goats/" + goatID + "/identifiers/" + identifierID + "/retire"
	subjectID := goatID + ":" + identifierID
	hash, err := app.CanonicalRequestHashWithSubject(tenantID, retireIdentifierCommandName, route, subjectID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.RetireGoatIdentifierCommand{
		TenantID:             tenantID,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: tenantID + ":" + retireIdentifierCommandName + ":" + goatID + ":" + identifierID + ":" + key,
		IdempotencyScope:     retireIdentifierCommandName,
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		IdentifierID:         identifierID,
		Reason:               strings.TrimSpace(reason),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func identifierEvidenceRef() domain.EvidenceRef {
	description := "Synthetic source row for identifier mutation."
	sourceSystem := "synthetic_import"
	return domain.EvidenceRef{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-identifier-row-1",
		SourceSystem: &sourceSystem,
		Description:  &description,
	}
}

func normalizeIdentifierForTest(identifierType, value string) string {
	value = strings.TrimSpace(value)
	if identifierType == "rfid" {
		return strings.ToUpper(value)
	}
	return value
}

func assertIdentifierMutationRows(t *testing.T, pool *pgxpool.Pool, idempotencyKey, goatID, identifierID, decisionID, action, eventType string) {
	t.Helper()
	ctx := context.Background()
	if got := countRows(t, pool, "SELECT count(*) FROM identity_decision_identifiers WHERE decision_id = $1 AND identifier_id = $2 AND action = $3", decisionID, identifierID, action); got != 1 {
		t.Fatalf("decision identifier rows = %d", got)
	}

	var eventID string
	var recordedAt string
	var linkedDecisionID string
	if err := pool.QueryRow(ctx, `
SELECT identity_event_id::text, recorded_at::text, decision_id::text
FROM goat_identity_events
WHERE idempotency_key = $1`, idempotencyKey).Scan(&eventID, &recordedAt, &linkedDecisionID); err != nil {
		t.Fatal(err)
	}
	if linkedDecisionID != decisionID {
		t.Fatalf("event decision_id = %s, want %s", linkedDecisionID, decisionID)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM identity_decision_events
WHERE decision_id = $1
  AND event_id = $2
  AND event_recorded_at::text = $3`, decisionID, eventID, recordedAt); got != 1 {
		t.Fatalf("decision-event linkage rows = %d", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM outbox_messages
WHERE idempotency_key = $1
  AND event_id = $2
  AND event_type = $3
  AND aggregate_type = 'goat'
  AND aggregate_id = $4
  AND topic = 'identity.events'`, idempotencyKey, eventID, eventType, goatID); got != 1 {
		t.Fatalf("outbox rows = %d", got)
	}
	if got := countRows(t, pool, "SELECT count(*) FROM audit_log WHERE resource_type = 'identifier' AND resource_id = $1 AND action = $2", identifierID, eventType); got != 1 {
		t.Fatalf("audit rows = %d", got)
	}

	decisionPayload := queryBytes(t, pool, "SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1", decisionID)
	validateDecisionRecord(t, decisionPayload)
	var decisionRecord map[string]any
	if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
		t.Fatal(err)
	}
	if decisionRecord["policy_version"] != identifierPolicyVersion {
		t.Fatalf("unexpected decision record policy: %#v", decisionRecord)
	}
	identifierActions := decisionRecord["identifier_actions"].([]any)
	identifierAction := identifierActions[0].(map[string]any)
	if identifierAction["identifier_id"] != identifierID || identifierAction["action"] != action {
		t.Fatalf("unexpected identifier action: %#v", identifierAction)
	}

	payload := queryBytes(t, pool, "SELECT payload FROM outbox_messages WHERE idempotency_key = $1", idempotencyKey)
	validateDomainEventEnvelope(t, payload)
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["event_id"] != eventID || envelope["event_type"] != eventType || envelope["aggregate_id"] != goatID || envelope["subject_id"] != identifierID {
		t.Fatalf("unexpected outbox envelope: %#v", envelope)
	}
	visibility := envelope["visibility_scope"].(map[string]any)
	if visibility["tenant_id"] != meshaTenant {
		t.Fatalf("visibility scope missing tenant: %#v", visibility)
	}
}

func assertNoRows(t *testing.T, pool *pgxpool.Pool, label, query string, args ...any) {
	t.Helper()
	if got := countRows(t, pool, query, args...); got != 0 {
		t.Fatalf("%s rows = %d", label, got)
	}
}
