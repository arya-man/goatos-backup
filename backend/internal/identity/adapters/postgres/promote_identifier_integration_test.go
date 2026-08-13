package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const promoteCommandName = "promoteTemporaryIdentifier"

// TestPromoteTemporaryIdentifierWithDockerPostgres proves the temp->permanent conversion end to end
// against real Postgres: the retire of the temporary_tag and the attach of the permanent
// animal_identifier_1 happen atomically, both identity events are emitted, a replay is a no-op that
// returns the permanent identifier, and promoting a goat with no temp tag is rejected.
func TestPromoteTemporaryIdentifierWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	seedAdminCreateLocations(t, pool)

	// A newborn created with ONLY a temporary tag: no active animal_identifier_1, so it reads as an
	// untagged kid until promoted.
	create := adminGoatCreateCommand(t, "idem-create-temp-goat-0001", "unused-1", "unused-2")
	create.Identifiers = []ports.AdminGoatCreateIdentifier{{
		IdentifierType:  "temporary_tag",
		IdentifierValue: "TEMP-PROMOTE-1",
		NormalizedValue: "TEMP-PROMOTE-1",
		ScopeKey:        "global",
		IsPrimary:       true,
	}}
	created, err := repo.CreateAdminGoat(ctx, create)
	if err != nil {
		t.Fatalf("CreateAdminGoat with temporary tag: %v", err)
	}
	goatID := created.Goat.GoatID
	assertActiveIdentifier(t, pool, goatID, "temporary_tag", true)
	assertNoActiveIdentifier(t, pool, goatID, "animal_identifier_1")

	// Promote: assign the permanent RFID.
	rv := rowVersionForGoat(t, pool, goatID)
	res, err := repo.PromoteTemporaryIdentifier(ctx, promoteCommand(t, "idem-promote-0001", goatID, "RFID-PERMANENT-1", rv))
	if err != nil {
		t.Fatalf("PromoteTemporaryIdentifier: %v", err)
	}
	if res.Replayed {
		t.Fatalf("first promotion reported replayed=true")
	}

	// THE ATOMIC OUTCOME: the temp is retired and the permanent animal_identifier_1 is active+primary.
	assertNoActiveIdentifier(t, pool, goatID, "temporary_tag")
	assertActiveIdentifier(t, pool, goatID, "animal_identifier_1", true)
	if got := identifierStatus(t, pool, goatID, "temporary_tag"); got != "retired" {
		t.Fatalf("temporary_tag status=%q after promote, want retired", got)
	}

	// BOTH identity events reached the outbox.
	if !outboxHasEvent(t, pool, goatID, "goat.identifier.added") {
		t.Fatalf("promotion did not emit goat.identifier.added to the outbox")
	}
	if !outboxHasEvent(t, pool, goatID, "goat.identifier.retired") {
		t.Fatalf("promotion did not emit goat.identifier.retired to the outbox")
	}

	// Exact replay: returns the permanent identifier, moves nothing.
	replay, err := repo.PromoteTemporaryIdentifier(ctx, promoteCommand(t, "idem-promote-0001", goatID, "RFID-PERMANENT-1", rv))
	if err != nil {
		t.Fatalf("promote replay: %v", err)
	}
	if !replay.Replayed {
		t.Fatalf("replay reported replayed=false, want the original result")
	}

	// Promoting again (the goat no longer has a temp tag) is rejected, not a silent no-op.
	rv2 := rowVersionForGoat(t, pool, goatID)
	if _, err := repo.PromoteTemporaryIdentifier(ctx, promoteCommand(t, "idem-promote-0002", goatID, "RFID-PERMANENT-2", rv2)); !errors.Is(err, ports.ErrNoTemporaryIdentifier) {
		t.Fatalf("promote with no temp tag err=%v, want ports.ErrNoTemporaryIdentifier", err)
	}
}

func promoteCommand(t *testing.T, key, goatID, permanent string, rowVersion int) ports.PromoteTemporaryIdentifierCommand {
	t.Helper()
	route := "/app/counts/goats/" + goatID + "/promote-identifier"
	body := map[string]any{"permanent_identifier": permanent, "row_version": rowVersion}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, promoteCommandName, route, goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.PromoteTemporaryIdentifierCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":" + promoteCommandName + ":" + goatID + ":" + key,
		IdempotencyScope:     promoteCommandName,
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		PermanentValue:       permanent,
		NormalizedValue:      permanent,
		EvidenceRefs:         []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: key}},
		RowVersion:           rowVersion,
		Reason:               "test promote",
	}
}

func assertActiveIdentifier(t *testing.T, pool *pgxpool.Pool, goatID, identifierType string, primary bool) {
	t.Helper()
	var isPrimary bool
	err := pool.QueryRow(context.Background(), `
SELECT is_primary_for_goat FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND identifier_type = $3 AND status = 'active'`,
		meshaTenant, goatID, identifierType).Scan(&isPrimary)
	if err != nil {
		t.Fatalf("expected an active %s for goat %s: %v", identifierType, goatID, err)
	}
	if isPrimary != primary {
		t.Fatalf("active %s is_primary=%v, want %v", identifierType, isPrimary, primary)
	}
}

func assertNoActiveIdentifier(t *testing.T, pool *pgxpool.Pool, goatID, identifierType string) {
	t.Helper()
	if got := countRows(t, pool, `
SELECT count(*) FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND identifier_type = $3 AND status = 'active'`,
		meshaTenant, goatID, identifierType); got != 0 {
		t.Fatalf("goat %s has %d active %s, want 0", goatID, got, identifierType)
	}
}

func identifierStatus(t *testing.T, pool *pgxpool.Pool, goatID, identifierType string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `
SELECT status FROM goat_identifiers
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND identifier_type = $3
ORDER BY valid_from DESC LIMIT 1`,
		meshaTenant, goatID, identifierType).Scan(&status); err != nil {
		t.Fatalf("read %s status: %v", identifierType, err)
	}
	return status
}

func outboxHasEvent(t *testing.T, pool *pgxpool.Pool, goatID, eventType string) bool {
	t.Helper()
	return countRows(t, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = $3`,
		meshaTenant, goatID, eventType) > 0
}
