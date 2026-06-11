package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/vgoats/goatos/backend/internal/legacy_import"
	importdb "github.com/vgoats/goatos/backend/internal/legacy_import/adapters/postgres/sqlc"
)

const (
	meshaPartyIDForApply = "00000000-0000-4000-8000-000000001001"
	applyActorID         = "90000000-0000-4000-8000-000000000901"
)

func TestRFIDApplyWithDockerPostgres(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	pool := startLegacyImportDB(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	applier := legacy_import.NewApplier(repo)

	t.Run("approved RFID breed aliases resolve through apply normalization", func(t *testing.T) {
		q := importdb.New(pool)
		parentBreeds := map[string]bool{
			"Malai":  true,
			"Beetal": true,
			"Sojat":  true,
			"Boer":   true,
		}
		for parent := range parentBreeds {
			if got := countRows(t, pool, `SELECT count(*) FROM breeds WHERE species = 'goat' AND canonical_name = $1 AND status = 'active'`, parent); got != 1 {
				t.Fatalf("active parent breed %s rows=%d, want 1", parent, got)
			}
		}

		expected := map[string]string{
			"Sirohi":         "Sirohi",
			"Beetal x Malai": "Beetal x Malai",
			"Malai x Beetal": "Beetal x Malai",
			"Beetal x Sojat": "Beetal x Sojat",
			"Boer x Beetal":  "Boer x Beetal",
			"Boer x Malai":   "Boer x Malai",
			"Boer x Sirohi":  "Boer x Sirohi",
			"Boer x Sojat":   "Boer x Sojat",
			"Malai x Sojat":  "Malai x Sojat",
			"Sojat x Malai":  "Malai x Sojat",
		}
		resolvedIDs := map[string]string{}
		for rawLabel, wantCanonical := range expected {
			normalized := normalizedBreedAlias(rawLabel)
			row, err := q.ResolveBreedAliasForApply(ctx, importdb.ResolveBreedAliasForApplyParams{
				NormalizedAlias: normalized,
				SourceSystem:    textParam("legacy_rfid_db"),
			})
			if err != nil {
				t.Fatalf("ResolveBreedAliasForApply(%q -> %q): %v", rawLabel, normalized, err)
			}
			if row.CanonicalName != wantCanonical {
				t.Fatalf("ResolveBreedAliasForApply(%q) canonical=%q, want %q", rawLabel, row.CanonicalName, wantCanonical)
			}
			if strings.Contains(rawLabel, " x ") && parentBreeds[row.CanonicalName] {
				t.Fatalf("crossbreed %q resolved to parent breed %q", rawLabel, row.CanonicalName)
			}
			resolvedIDs[rawLabel] = row.BreedID
		}
		if resolvedIDs["Beetal x Malai"] != resolvedIDs["Malai x Beetal"] {
			t.Fatalf("reverse aliases Beetal x Malai/Malai x Beetal resolved to different breed_id")
		}
		if resolvedIDs["Malai x Sojat"] != resolvedIDs["Sojat x Malai"] {
			t.Fatalf("reverse aliases Malai x Sojat/Sojat x Malai resolved to different breed_id")
		}

		if _, err := q.ResolveBreedAliasForApply(ctx, importdb.ResolveBreedAliasForApplyParams{
			NormalizedAlias: normalizedBreedAlias("Anantapur Sheep"),
			SourceSystem:    textParam("legacy_rfid_db"),
		}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("Anantapur Sheep resolve err=%v, want pgx.ErrNoRows", err)
		}

		migrationPath := filepath.Join(repoRoot(t), "backend", "migrations", "postgres", "000011_phase_1_rfid_breed_cross_mappings.sql")
		sqlBytes, err := os.ReadFile(migrationPath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, extractGooseUp(string(sqlBytes))); err != nil {
			t.Fatalf("reapply 000011 up migration: %v", err)
		}
		if got := countRows(t, pool, `
SELECT count(*)
FROM breed_aliases
WHERE source_system = 'legacy_rfid_db'
  AND normalized_alias IN (
    'sirohi',
    'beetal_x_malai',
    'malai_x_beetal',
    'beetal_x_sojat',
    'boer_x_beetal',
    'boer_x_malai',
    'boer_x_sirohi',
    'boer_x_sojat',
    'malai_x_sojat',
    'sojat_x_malai'
  )`); got != 10 {
			t.Fatalf("approved RFID breed alias rows=%d, want 10 after idempotent reapply", got)
		}
	})

	t.Run("clean pending row creates canonical goat identity ledger event outbox and audit", func(t *testing.T) {
		runID, rowID := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{{
			RowNumber: 2,
			Raw:       rawRFIDRow("APPLY100", "CBE", "9900000000000000000000009100001", "Female", "Boer", "Fattening"),
		}})

		result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{
			TenantID:    meshaTenant,
			ImportRunID: runID,
			BatchSize:   1,
			ActorID:     strPtr(applyActorID),
		})
		if err != nil {
			t.Fatalf("ApplyRFIDRows: %v", err)
		}
		if result.AppliedCount != 1 || result.ReviewCount != 0 || result.ReplayCount != 0 || len(result.CreatedGoatIDs) != 1 {
			t.Fatalf("unexpected apply result: %#v", result)
		}
		goatID := result.CreatedGoatIDs[0]

		var displayID, sex, lifecycle, growth, identityState, custodian, breed string
		var breedID string
		if err := pool.QueryRow(ctx, `
SELECT display_id, sex, lifecycle_status, COALESCE(growth_cohort_tag, ''), identity_state, custodian_party_id::text, breed, breed_id::text
FROM goats
WHERE goat_id = $1`, goatID).Scan(&displayID, &sex, &lifecycle, &growth, &identityState, &custodian, &breed, &breedID); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(displayID, "G-") || sex != "female" || lifecycle != "alive" || growth != "F2" || identityState != "clean" || custodian != meshaPartyIDForApply || breed != "Boer" || breedID == "" {
			t.Fatalf("goat mapping mismatch display=%s sex=%s lifecycle=%s growth=%s state=%s custodian=%s breed=%s breedID=%s", displayID, sex, lifecycle, growth, identityState, custodian, breed, breedID)
		}
		if custodian == meshaTenant {
			t.Fatal("custodian incorrectly used tenant_id instead of Mesha party_id")
		}
		assertRowState(t, pool, runID, 2, legacy_import.StateCreatedGoat, "")
		if got := countRows(t, pool, `SELECT count(*) FROM legacy_import_rows WHERE legacy_row_id = $1 AND matched_goat_id = $2`, rowID, goatID); got != 1 {
			t.Fatalf("legacy row matched goat rows=%d", got)
		}
		if got := countRows(t, pool, `SELECT created_goat_count FROM legacy_import_runs WHERE import_run_id = $1`, runID); got != 1 {
			t.Fatalf("created_goat_count=%d, want 1", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_location_history WHERE goat_id = $1`, goatID); got != 0 {
			t.Fatalf("goat_location_history rows=%d", got)
		}

		var rfidID, oldTagID string
		if err := pool.QueryRow(ctx, `
SELECT identifier_id::text
FROM goat_identifiers
WHERE goat_id = $1 AND identifier_type = 'rfid' AND normalized_value = '9900000000000000000000009100001' AND scope_key = 'global' AND status = 'active' AND is_primary_for_goat`, goatID).Scan(&rfidID); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `
SELECT identifier_id::text
FROM goat_identifiers
WHERE goat_id = $1 AND identifier_type = 'old_tag' AND normalized_value = 'APPLY100' AND scope_key = 'park:CBE' AND status = 'active' AND is_primary_for_goat`, goatID).Scan(&oldTagID); err != nil {
			t.Fatal(err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_ownership WHERE goat_id = $1 AND owner_party_id = $2 AND share_bps = 10000 AND status = 'active' AND valid_to IS NULL`, goatID, meshaPartyIDForApply); got != 1 {
			t.Fatalf("ownership rows=%d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goat_custody_history WHERE goat_id = $1 AND custodian_party_id = $2 AND reason = 'first_rfid_import' AND valid_to IS NULL`, goatID, meshaPartyIDForApply); got != 1 {
			t.Fatalf("custody history rows=%d", got)
		}

		var decisionID, decisionByType, decisionResult string
		if err := pool.QueryRow(ctx, `
SELECT d.decision_id::text, d.decided_by_type, d.decision_result
FROM identity_decisions d
JOIN identity_decision_goats dg ON dg.tenant_id = d.tenant_id AND dg.decision_id = d.decision_id
WHERE dg.goat_id = $1 AND d.decision_type = 'create_goat'`, goatID).Scan(&decisionID, &decisionByType, &decisionResult); err != nil {
			t.Fatal(err)
		}
		if decisionByType != "import_policy" || decisionResult != "imported_from_rfid_source" {
			t.Fatalf("decision authority/result=%s/%s", decisionByType, decisionResult)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_identifiers WHERE decision_id = $1 AND action = 'attach'`, decisionID); got != 2 {
			t.Fatalf("decision identifier rows=%d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_identifiers WHERE decision_id = $1 AND identifier_id IN ($2, $3)`, decisionID, rfidID, oldTagID); got != 2 {
			t.Fatalf("decision identifier linkage rows=%d", got)
		}
		decisionPayload := queryBytes(t, pool, `SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1`, decisionID)
		validateDecisionRecord(t, decisionPayload)
		var decisionRecord map[string]any
		if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
			t.Fatal(err)
		}
		affectedGoats := decisionRecord["affected_goats"].([]any)
		if len(affectedGoats) != 1 || affectedGoats[0].(map[string]any)["goat_id"] != goatID {
			t.Fatalf("decision affected_goats=%#v, want created goat %s", affectedGoats, goatID)
		}
		identifierActionRows := decisionRecord["identifier_actions"].([]any)
		if len(identifierActionRows) != 2 {
			t.Fatalf("decision identifier_actions=%#v, want 2 attach actions", identifierActionRows)
		}

		var eventID string
		var eventRecordedAt time.Time
		if err := pool.QueryRow(ctx, `
SELECT gie.identity_event_id::text, gie.recorded_at
FROM goat_identity_events gie
WHERE gie.goat_id = $1 AND gie.event_type = 'goat.created'`, goatID).Scan(&eventID, &eventRecordedAt); err != nil {
			t.Fatal(err)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM identity_decision_events WHERE decision_id = $1 AND event_id = $2 AND event_recorded_at = $3`, decisionID, eventID, eventRecordedAt); got != 1 {
			t.Fatalf("decision event exact timestamp rows=%d", got)
		}
		outboxPayload := queryBytes(t, pool, `SELECT payload FROM outbox_messages WHERE event_id = $1`, eventID)
		validateDomainEventEnvelope(t, outboxPayload)
		if got := countRows(t, pool, `SELECT count(*) FROM audit_log WHERE resource_type = 'goat' AND resource_id = $1 AND decision_id = $2`, goatID, decisionID); got != 1 {
			t.Fatalf("audit rows=%d", got)
		}
		var idempotencyKey string
		if err := pool.QueryRow(ctx, `SELECT idempotency_key FROM goat_identity_events WHERE identity_event_id = $1`, eventID).Scan(&idempotencyKey); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(idempotencyKey, runID) {
			t.Fatalf("business idempotency key included import_run_id: %s", idempotencyKey)
		}

		if _, err := pool.Exec(ctx, `
UPDATE legacy_import_rows
SET processing_state = 'pending', matched_goat_id = NULL
WHERE legacy_row_id = $1`, rowID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `
UPDATE legacy_import_runs
SET created_goat_count = 0
WHERE import_run_id = $1`, runID); err != nil {
			t.Fatal(err)
		}
		replay, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{TenantID: meshaTenant, ImportRunID: runID, BatchSize: 1, ActorID: strPtr(applyActorID)})
		if err != nil {
			t.Fatalf("replay apply: %v", err)
		}
		if replay.ReplayCount != 1 || replay.AppliedCount != 0 {
			t.Fatalf("unexpected replay result: %#v", replay)
		}
		if got := countRows(t, pool, `SELECT created_goat_count FROM legacy_import_runs WHERE import_run_id = $1`, runID); got != 1 {
			t.Fatalf("created_goat_count after replay=%d, want 1", got)
		}
		for label, query := range map[string]string{
			"goats":              `SELECT count(*) FROM goats WHERE goat_id = $1`,
			"identifiers":        `SELECT count(*) FROM goat_identifiers WHERE goat_id = $1`,
			"ownership":          `SELECT count(*) FROM goat_ownership WHERE goat_id = $1`,
			"custody":            `SELECT count(*) FROM goat_custody_history WHERE goat_id = $1`,
			"events":             `SELECT count(*) FROM goat_identity_events WHERE goat_id = $1`,
			"outbox":             `SELECT count(*) FROM outbox_messages WHERE aggregate_id = $1`,
			"decisions_for_goat": `SELECT count(*) FROM identity_decision_goats WHERE goat_id = $1`,
		} {
			want := 1
			if label == "identifiers" {
				want = 2
			}
			if got := countRows(t, pool, query, goatID); got != want {
				t.Fatalf("%s after replay=%d, want %d", label, got, want)
			}
		}
	})

	t.Run("deterministic row failure marks sanitized error and continues applying later rows", func(t *testing.T) {
		const failingRFID = "9900000000000000000000009150001"
		runID, rowID := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{{
			RowNumber: 2,
			Raw:       rawRFIDRow("ROLLBACKAPPLY", "CBE", failingRFID, "Female", "Boer", ""),
		}, {
			RowNumber: 3,
			Raw:       rawRFIDRow("AFTERERROR", "CBE", "9900000000000000000000009150002", "Female", "Boer", ""),
		}})
		var sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash string
		if err := pool.QueryRow(ctx, `
	SELECT source_system, source_dataset, source_row_key, source_row_version_hash
FROM legacy_import_rows
WHERE legacy_row_id = $1`, rowID).Scan(&sourceSystem, &sourceDataset, &sourceRowKey, &sourceRowVersionHash); err != nil {
			t.Fatal(err)
		}
		idempotencyKey := stableApplyIdempotencyKey(meshaTenant, sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash)
		beforeGoats := countRows(t, pool, `SELECT count(*) FROM goats`)

		hookCalls := 0
		repo.afterAuditHook = func(context.Context) error {
			hookCalls++
			if hookCalls == 1 {
				return &pgconn.PgError{
					Code:           "23514",
					Message:        "synthetic check failed",
					Detail:         "synthetic RFID " + failingRFID + " violates apply check",
					ConstraintName: "synthetic_apply_check",
					TableName:      "legacy_import_rows",
				}
			}
			return nil
		}
		defer func() { repo.afterAuditHook = nil }()
		result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{
			TenantID:    meshaTenant,
			ImportRunID: runID,
			BatchSize:   1,
			ActorID:     strPtr(applyActorID),
		})
		if err != nil {
			t.Fatalf("apply with isolated row error: %v", err)
		}
		if result.ErrorCount != 1 || result.AppliedCount != 1 || result.PendingScanned != 2 {
			t.Fatalf("unexpected isolated-error result: %#v", result)
		}
		assertRowState(t, pool, runID, 2, legacy_import.StateError, "sqlstate=23514")
		var errorReason string
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(error_reason, '')
FROM legacy_import_rows
WHERE import_run_id = $1 AND row_number = 2`, runID).Scan(&errorReason); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{failingRFID, "synthetic RFID", "violates apply check"} {
			if strings.Contains(errorReason, forbidden) {
				t.Fatalf("error_reason leaks raw detail %q in %q", forbidden, errorReason)
			}
		}
		if !strings.Contains(errorReason, "constraint=synthetic_apply_check") {
			t.Fatalf("error_reason=%q, want sanitized constraint metadata", errorReason)
		}
		assertRowState(t, pool, runID, 3, legacy_import.StateCreatedGoat, "")
		if got := countRows(t, pool, `SELECT created_goat_count FROM legacy_import_runs WHERE import_run_id = $1`, runID); got != 1 {
			t.Fatalf("created_goat_count after isolated error=%d, want 1", got)
		}
		if got := countRows(t, pool, `SELECT error_count FROM legacy_import_runs WHERE import_run_id = $1`, runID); got != 1 {
			t.Fatalf("error_count after isolated error=%d, want 1", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats`); got != beforeGoats+1 {
			t.Fatalf("goat rows after isolated error=%d, want %d", got, beforeGoats+1)
		}
		assertZeroRows(t, pool, "idempotency after apply rollback", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, idempotencyKey)
		assertZeroRows(t, pool, "rfid identifier after apply rollback", `SELECT count(*) FROM goat_identifiers WHERE normalized_value = '9900000000000000000000009150001'`)
		assertZeroRows(t, pool, "old tag identifier after apply rollback", `SELECT count(*) FROM goat_identifiers WHERE normalized_value = 'ROLLBACKAPPLY'`)
		assertZeroRows(t, pool, "decision after apply rollback", `SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1`, "rfid-apply:"+rowID)
		assertZeroRows(t, pool, "event after apply rollback", `SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1`, idempotencyKey)
		assertZeroRows(t, pool, "outbox after apply rollback", `SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1`, idempotencyKey)
		assertZeroRows(t, pool, "audit after apply rollback", `SELECT count(*) FROM audit_log WHERE trace_id = $1`, "rfid-apply:"+rowID)
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE normalized_value = 'AFTERERROR' AND status = 'active'`); got != 1 {
			t.Fatalf("following clean row old_tag identifiers=%d, want 1", got)
		}
	})

	t.Run("transient SQL row failure aborts without marking row error", func(t *testing.T) {
		const transientRFID = "9900000000000000000000009160001"
		runID, rowID := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{{
			RowNumber: 2,
			Raw:       rawRFIDRow("TRANSIENTAPPLY", "CBE", transientRFID, "Female", "Boer", ""),
		}, {
			RowNumber: 3,
			Raw:       rawRFIDRow("AFTERTRANSIENT", "CBE", "9900000000000000000000009160002", "Female", "Boer", ""),
		}})
		var sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash string
		if err := pool.QueryRow(ctx, `
	SELECT source_system, source_dataset, source_row_key, source_row_version_hash
FROM legacy_import_rows
WHERE legacy_row_id = $1`, rowID).Scan(&sourceSystem, &sourceDataset, &sourceRowKey, &sourceRowVersionHash); err != nil {
			t.Fatal(err)
		}
		idempotencyKey := stableApplyIdempotencyKey(meshaTenant, sourceSystem, sourceDataset, sourceRowKey, sourceRowVersionHash)
		beforeGoats := countRows(t, pool, `SELECT count(*) FROM goats`)

		repo.afterAuditHook = func(context.Context) error {
			return &pgconn.PgError{
				Code:    "40001",
				Message: "synthetic serialization failure",
				Detail:  "synthetic transient failure should not quarantine RFID " + transientRFID,
			}
		}
		defer func() { repo.afterAuditHook = nil }()
		_, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{
			TenantID:    meshaTenant,
			ImportRunID: runID,
			BatchSize:   1,
			ActorID:     strPtr(applyActorID),
		})
		if err == nil || !strings.Contains(err.Error(), "40001") {
			t.Fatalf("apply err=%v, want transient failure", err)
		}
		for _, forbidden := range []string{transientRFID, "synthetic serialization failure", "synthetic transient failure"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("apply err leaks raw detail %q in %q", forbidden, err.Error())
			}
		}
		assertRowState(t, pool, runID, 2, legacy_import.StatePending, "")
		assertRowState(t, pool, runID, 3, legacy_import.StatePending, "")
		if got := countRows(t, pool, `SELECT error_count FROM legacy_import_runs WHERE import_run_id = $1`, runID); got != 0 {
			t.Fatalf("error_count after transient failure=%d, want 0", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats`); got != beforeGoats {
			t.Fatalf("goat rows after transient failure=%d, want %d", got, beforeGoats)
		}
		assertZeroRows(t, pool, "idempotency after transient rollback", `SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1`, idempotencyKey)
		assertZeroRows(t, pool, "transient failed rfid", `SELECT count(*) FROM goat_identifiers WHERE normalized_value = '9900000000000000000000009160001'`)
		assertZeroRows(t, pool, "transient following rfid not reached", `SELECT count(*) FROM goat_identifiers WHERE normalized_value = '9900000000000000000000009160002'`)
	})

	t.Run("dry-run previews applyable and review rows without mutation", func(t *testing.T) {
		runID, _ := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{
			{RowNumber: 2, Raw: rawRFIDRow("APPLYDRY1", "CBE", "9900000000000000000000009200001", "Male", "Boer", "")},
			{RowNumber: 3, Raw: rawRFIDRow("APPLYDRY2", "CBE", "9900000000000000000000009200002", "Female", "Anantapur Sheep", "")},
		})
		beforeGoats := countRows(t, pool, `SELECT count(*) FROM goats`)
		result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{TenantID: meshaTenant, ImportRunID: runID, DryRun: true, BatchSize: 1})
		if err != nil {
			t.Fatalf("dry apply: %v", err)
		}
		if result.AppliedCount != 1 || result.ReviewCount != 1 || result.ReviewReasons["species_or_breed_requires_review"] != 1 {
			t.Fatalf("unexpected dry-run result: %#v", result)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats`); got != beforeGoats {
			t.Fatalf("dry-run mutated goats: before=%d after=%d", beforeGoats, got)
		}
		assertRowState(t, pool, runID, 2, legacy_import.StatePending, "")
		assertRowState(t, pool, runID, 3, legacy_import.StatePending, "")
	})

	t.Run("unsafe rows route to review and old tag different scope still applies", func(t *testing.T) {
		existingGoatID := insertApplyBaselineGoat(t, pool, "APPLYCONFLICT", "park:CBE", "9900000000000000000000009300000")
		_ = existingGoatID
		runID, _ := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{
			{RowNumber: 2, Raw: rawRFIDRow("REVIEWSTATUS", "CBE", "9900000000000000000000009300001", "Female", "Boer", "F2-Male")},
			{RowNumber: 3, Raw: rawRFIDRow("REVIEWSHEEP", "CBE", "9900000000000000000000009300002", "Female", "Anantapur Sheep", "")},
			{RowNumber: 4, Raw: rawRFIDRow("REVIEWUNKNOWN", "CBE", "9900000000000000000000009300003", "Female", "Boer", "Mystery")},
			{RowNumber: 5, Raw: rawRFIDRow("REVIEWRFID", "CBE", "9900000000000000000000009300000", "Female", "Boer", "")},
			{RowNumber: 6, Raw: rawRFIDRow("APPLYCONFLICT", "CBE", "9900000000000000000000009300004", "Female", "Boer", "")},
			{RowNumber: 7, Raw: rawRFIDRow("APPLYCONFLICT", "CPT", "9900000000000000000000009300005", "Female", "Boer", "")},
			{RowNumber: 8, Raw: rawRFIDRow("SKIPREVIEW", "CBE", "9900000000000000000000009300006", "Female", "Boer", ""), State: legacy_import.StateNeedsReview},
			{RowNumber: 9, Raw: rawRFIDRow("SKIPERROR", "CBE", "9900000000000000000000009300007", "Female", "Boer", ""), State: legacy_import.StateError, ErrorReason: "synthetic_error"},
		})
		result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{TenantID: meshaTenant, ImportRunID: runID, BatchSize: 2})
		if err != nil {
			t.Fatalf("review apply: %v", err)
		}
		if result.AppliedCount != 1 || result.ReviewCount != 5 || result.PendingScanned != 6 {
			t.Fatalf("unexpected review result: %#v", result)
		}
		assertRowState(t, pool, runID, 2, legacy_import.StateNeedsReview, "status_mapping_requires_review")
		assertRowState(t, pool, runID, 3, legacy_import.StateNeedsReview, "species_or_breed_requires_review")
		assertRowState(t, pool, runID, 4, legacy_import.StateNeedsReview, "unknown_status_mapping")
		assertRowState(t, pool, runID, 5, legacy_import.StateNeedsReview, "rfid_already_linked")
		assertRowState(t, pool, runID, 6, legacy_import.StateNeedsReview, "old_tag_same_scope_conflict")
		assertRowState(t, pool, runID, 7, legacy_import.StateCreatedGoat, "")
		assertRowState(t, pool, runID, 8, legacy_import.StateNeedsReview, "")
		assertRowState(t, pool, runID, 9, legacy_import.StateError, "synthetic_error")
		if got := countRows(t, pool, `SELECT count(*) FROM goat_identifiers WHERE identifier_type = 'old_tag' AND normalized_value = 'APPLYCONFLICT' AND scope_key = 'park:CPT' AND status = 'active'`); got != 1 {
			t.Fatalf("different-scope old tag rows=%d", got)
		}
		if got := countRows(t, pool, `SELECT count(*) FROM goats g JOIN legacy_import_rows lir ON lir.matched_goat_id = g.goat_id WHERE lir.import_run_id = $1`, runID); got != 1 {
			t.Fatalf("goats created for review run=%d", got)
		}
	})

	t.Run("same source key with different hash is review-driven", func(t *testing.T) {
		firstRunID, _ := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{
			{RowNumber: 2, Raw: rawRFIDRow("APPLYCHANGED", "CBE", "9900000000000000000000009400001", "Female", "Boer", "")},
		})
		runID, _ := stageSyntheticRun(t, ctx, repo, []stagedFixtureRow{
			{RowNumber: 2, Raw: rawRFIDRow("APPLYCHANGED", "CBE", "9900000000000000000000009400001", "Female", "Malai", "")},
		})
		result, err := applier.ApplyRFIDRows(ctx, legacy_import.ApplyCommand{TenantID: meshaTenant, ImportRunID: runID, BatchSize: 2})
		if err != nil {
			t.Fatalf("changed source apply: %v", err)
		}
		if result.AppliedCount != 0 || result.ReviewCount != 1 || result.ReviewReasons["source_row_changed"] != 1 {
			t.Fatalf("unexpected changed source result: %#v", result)
		}
		assertRowState(t, pool, firstRunID, 2, legacy_import.StatePending, "")
		assertRowState(t, pool, runID, 2, legacy_import.StateNeedsReview, "source_row_changed")
	})
}

type stagedFixtureRow struct {
	RowNumber   int
	Raw         map[string]string
	State       string
	ErrorReason string
}

func stageSyntheticRun(t *testing.T, ctx context.Context, repo *Repository, fixtures []stagedFixtureRow) (string, string) {
	t.Helper()
	policy, err := repo.LoadApprovedPolicy(ctx, legacy_import.DefaultPolicyVersion)
	if err != nil {
		t.Fatal(err)
	}
	workbookRows := make([]legacy_import.WorkbookRow, 0, len(fixtures))
	for _, fixture := range fixtures {
		workbookRows = append(workbookRows, legacy_import.WorkbookRow{RowNumber: fixture.RowNumber, Raw: fixture.Raw})
	}
	staged, err := legacy_import.NormalizeWorkbookRows(policy, meshaTenant, workbookRows)
	if err != nil {
		t.Fatal(err)
	}
	byRow := map[int]stagedFixtureRow{}
	for _, fixture := range fixtures {
		byRow[fixture.RowNumber] = fixture
	}
	for i := range staged {
		if fixture := byRow[staged[i].RowNumber]; fixture.State != "" {
			staged[i].ProcessingState = fixture.State
			if fixture.ErrorReason != "" {
				staged[i].ErrorReason = &fixture.ErrorReason
			}
		}
	}
	runID, err := repo.CreateImportRun(ctx, legacy_import.CreateRunParams{
		TenantID:       meshaTenant,
		SourceName:     "Synthetic RFID apply run",
		SourceSystem:   policy.SourceSystem,
		SourceDataset:  policy.SourceDataset,
		SourceFileRef:  nil,
		SourceFileHash: "sha256:synthetic-apply",
		PolicyVersion:  policy.PolicyVersion,
		DryRun:         false,
		Status:         "completed",
		RowCount:       len(staged),
		ErrorCount:     legacy_import.ErrorCount(staged),
		StartedBy:      nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range staged {
		staged[i].ImportRunID = runID
	}
	if _, err := repo.InsertRows(ctx, staged, 3); err != nil {
		t.Fatal(err)
	}
	var firstRowID string
	if err := repo.pool.QueryRow(ctx, `SELECT legacy_row_id::text FROM legacy_import_rows WHERE import_run_id = $1 ORDER BY row_number, legacy_row_id LIMIT 1`, runID).Scan(&firstRowID); err != nil {
		t.Fatal(err)
	}
	return runID, firstRowID
}

func rawRFIDRow(oldTag, suffix, rfid, gender, breed, tag string) map[string]string {
	return map[string]string{
		"Farm":          suffix,
		"Old ID":        oldTag,
		"Old ID Suffix": suffix,
		"RFID":          rfid,
		"Age":           "Adult",
		"Gender":        gender,
		"Breed":         breed,
		"Tag":           tag,
		"Shed":          "Synthetic Shed",
		"Partition":     "Synthetic Partition",
	}
}

func insertApplyBaselineGoat(t *testing.T, pool *pgxpool.Pool, oldTag, oldTagScope, rfid string) string {
	t.Helper()
	var goatID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO goats (tenant_id, breed, breed_id, sex, lifecycle_status, identity_state, custodian_party_id)
VALUES ($1, 'Boer', '00000000-0000-4000-8000-000000002005', 'female', 'alive', 'clean', $2)
RETURNING goat_id::text`, meshaTenant, meshaPartyIDForApply).Scan(&goatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version)
VALUES
  ($1, $2, 'rfid', $3, $3, 'global', true, 'active', now(), 'synthetic_apply', 'baseline-rfid', 'identifier_normalizer_v1'),
  ($1, $2, 'old_tag', $4, $4, $5, true, 'active', now(), 'synthetic_apply', 'baseline-old-tag', 'identifier_normalizer_v1')`,
		meshaTenant, goatID, rfid, oldTag, oldTagScope); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO goat_ownership (tenant_id, goat_id, owner_party_id, share_bps, valid_from, status)
VALUES ($1, $2, $3, 10000, now(), 'active')`, meshaTenant, goatID, meshaPartyIDForApply); err != nil {
		t.Fatal(err)
	}
	return goatID
}

func queryBytes(t *testing.T, pool *pgxpool.Pool, query string, args ...any) []byte {
	t.Helper()
	var payload []byte
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func validateDomainEventEnvelope(t *testing.T, payload []byte) {
	t.Helper()
	validateJSONSchema(t, "domain-event-envelope.schema.json", payload)
}

func validateDecisionRecord(t *testing.T, payload []byte) {
	t.Helper()
	validateJSONSchema(t, "decision-record.schema.json", payload)
}

func assertZeroRows(t *testing.T, pool *pgxpool.Pool, label, query string, args ...any) {
	t.Helper()
	if got := countRows(t, pool, query, args...); got != 0 {
		t.Fatalf("%s rows=%d, want 0", label, got)
	}
}

func validateJSONSchema(t *testing.T, schemaName string, payload []byte) {
	t.Helper()
	root := repoRoot(t)
	schemaPath := filepath.Join(root, "contracts", "jsonschema", schemaName)
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		t.Fatal(err)
	}
	payloadDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, string(payload))
	}
	if err := schema.Validate(payloadDoc); err != nil {
		t.Fatalf("%s validation failed: %v\n%s", schemaName, err, string(payload))
	}
}

func strPtr(value string) *string {
	return &value
}
