package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	addIdentifierCommandName    = "addGoatIdentifier"
	retireIdentifierCommandName = "retireGoatIdentifier"
	createAdminGoatCommandName  = "createAdminGoat"
	adminCreateFarmLocation     = "00000000-0000-4000-8000-000000003901"
	adminCreateShedLocation     = "00000000-0000-4000-8000-000000003902"
	adminCreateOrphanShed       = "00000000-0000-4000-8000-000000003903"
	adminCreateWrongParkShed    = "00000000-0000-4000-8000-000000003904"
	adminMoveTargetShed         = "00000000-0000-4000-8000-000000003905"
)

func TestIdentifierWritePathWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	seedAdminCreateLocations(t, pool)

	t.Run("admin goat create writes goat identifiers decision event audit outbox and schema-valid payloads", func(t *testing.T) {
		cmd := adminGoatCreateCommand(t, "idem-create-goat-0001", "aid1-admin-create-0001", "admin-create-aid2-0001")
		result, err := repo.CreateAdminGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("CreateAdminGoat: %v", err)
		}
		if result.Replayed || result.Goat.GoatID == "" || result.Decision.DecisionType != "create_goat" || result.GenerationStatus != "queued" {
			t.Fatalf("unexpected create result: %#v", result)
		}
		if result.Goat.LocationPath.ParkID == nil || *result.Goat.LocationPath.ParkID != cbeLocation {
			t.Fatalf("unexpected goat park scope: %#v", result.Goat.LocationPath)
		}
		if len(result.Identifiers) != 2 {
			t.Fatalf("expected Animal ID 1 and Animal ID 2 identifiers, got %#v", result.Identifiers)
		}
		assertAdminGoatCreateRows(t, pool, cmd, result)
	})

	t.Run("admin goat create validation rejects orphan and wrong-parent sheds", func(t *testing.T) {
		cases := []struct {
			name   string
			shedID string
		}{
			{name: "orphan shed", shedID: adminCreateOrphanShed},
			{name: "wrong park shed", shedID: adminCreateWrongParkShed},
		}
		for i, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := adminGoatCreateCommand(t, "idem-create-goat-parent-"+string(rune('a'+i)), "aid1-admin-create-parent-"+string(rune('a'+i)), "admin-create-aid2-parent-"+string(rune('a'+i)))
				validation, err := repo.ValidateAdminGoatCreate(ctx, ports.ValidateAdminGoatCreateCommand{
					TenantID:             cmd.TenantID,
					StoredIdempotencyKey: cmd.StoredIdempotencyKey,
					RequestHash:          cmd.RequestHash,
					Identifiers:          cmd.Identifiers,
					FarmID:               cmd.FarmID,
					ParkID:               &cmd.ParkID,
					ShedID:               &tc.shedID,
				})
				if err != nil {
					t.Fatalf("ValidateAdminGoatCreate: %v", err)
				}
				if !hasFieldError(validation.Conflicts, "shed_id", "wrong_parent") {
					t.Fatalf("expected shed_id wrong_parent conflict, got %#v", validation.Conflicts)
				}
			})
		}
	})

	t.Run("admin goat create validation rejects unsupported management stage", func(t *testing.T) {
		cmd := adminGoatCreateCommand(t, "idem-create-goat-stage-invalid", "aid1-admin-create-stage-invalid", "admin-create-aid2-stage-invalid")
		invalidStage := "unsupported_stage"
		validation, err := repo.ValidateAdminGoatCreate(ctx, ports.ValidateAdminGoatCreateCommand{
			TenantID:             cmd.TenantID,
			StoredIdempotencyKey: cmd.StoredIdempotencyKey,
			RequestHash:          cmd.RequestHash,
			Identifiers:          cmd.Identifiers,
			FarmID:               cmd.FarmID,
			ParkID:               &cmd.ParkID,
			ShedID:               &cmd.ShedID,
			ManagementStage:      &invalidStage,
		})
		if err != nil {
			t.Fatalf("ValidateAdminGoatCreate: %v", err)
		}
		if !hasFieldError(validation.Conflicts, "management_stage", "not_found") {
			t.Fatalf("expected management_stage not_found conflict, got %#v", validation.Conflicts)
		}
	})

	t.Run("admin goat create exact replay rebuilds response and changed body conflicts", func(t *testing.T) {
		cmd := adminGoatCreateCommand(t, "idem-create-goat-0002", "aid1-admin-create-0002", "admin-create-aid2-0002")
		first, err := repo.CreateAdminGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("first create: %v", err)
		}
		replay, err := repo.CreateAdminGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("replay create: %v", err)
		}
		if !replay.Replayed || replay.FirstResultID == nil || *replay.FirstResultID != first.Goat.GoatID {
			t.Fatalf("unexpected replay metadata: %#v", replay)
		}
		if replay.Decision.DecisionID != first.Decision.DecisionID || replay.Events[0].EventID != first.Events[0].EventID {
			t.Fatalf("replay did not refetch original decision/event: first=%#v replay=%#v", first, replay)
		}
		changed := adminGoatCreateCommand(t, "idem-create-goat-0002", "aid1-admin-create-0002", "admin-create-aid2-0002")
		changed.RequestHash = "changed-admin-goat-create-request-hash"
		if _, err := repo.CreateAdminGoat(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("admin goat move writes goat.location.changed outbox for obligation re-scope", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-move-0001", "aid1-admin-move-0001", "admin-move-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for move: %v", err)
		}
		cmd := moveGoatCommand(t, "idem-move-goat-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		moved, err := repo.MoveGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("MoveGoat: %v", err)
		}
		if moved.Decision.DecisionType != "move_goat" || len(moved.Events) != 1 || moved.Events[0].EventType != "goat.location.changed" {
			t.Fatalf("unexpected move result: %#v", moved)
		}
		assertGoatLifecycleOutbox(t, pool, cmd.StoredIdempotencyKey, moved.Events[0].EventID, created.Goat.GoatID, "goat.location.changed", adminMoveTargetShed)
	})

	t.Run("admin goat exit writes goat.exited outbox for obligation cancellation", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-exit-0001", "aid1-admin-exit-0001", "admin-exit-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for exit: %v", err)
		}
		cmd := exitGoatCommand(t, "idem-exit-goat-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		exited, err := repo.ExitGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("ExitGoat: %v", err)
		}
		if exited.Goat.LifecycleStatus != "sold" || exited.Events[0].EventType != "goat.exited" {
			t.Fatalf("unexpected exit result: %#v", exited)
		}
		assertGoatLifecycleOutbox(t, pool, cmd.StoredIdempotencyKey, exited.Events[0].EventID, created.Goat.GoatID, "goat.exited", created.Goat.GoatID)
	})

	t.Run("admin goat exit blocks death guardrail transitions", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-death-0001", "aid1-admin-death-0001", "admin-death-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for death guardrail: %v", err)
		}
		cmd := exitGoatCommand(t, "idem-exit-death-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		cmd.LifecycleStatus = "dead"
		cmd.ExitReason = "died"
		cmd.Reason = "Synthetic death exit requiring guardrail review."
		body := map[string]any{
			"lifecycle_status": cmd.LifecycleStatus,
			"exit_reason":      cmd.ExitReason,
			"reason":           cmd.Reason,
			"evidence_refs":    cmd.EvidenceRefs,
			"row_version":      cmd.RowVersion,
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		cmd.RequestHash, err = app.CanonicalRequestHashWithSubject(meshaTenant, "exitGoat", "/admin/goats/{goat_id}/exit", created.Goat.GoatID, raw)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := repo.ExitGoat(ctx, cmd); !errors.Is(err, ports.ErrCriticalDeathGuardrailRequired) {
			t.Fatalf("ExitGoat death transition error = %v, want ErrCriticalDeathGuardrailRequired", err)
		}
		var lifecycleStatus string
		if err := pool.QueryRow(ctx, "SELECT lifecycle_status FROM goats WHERE tenant_id = $1 AND goat_id = $2", meshaTenant, created.Goat.GoatID).Scan(&lifecycleStatus); err != nil {
			t.Fatalf("lifecycle after blocked death transition: %v", err)
		}
		if lifecycleStatus != "alive" {
			t.Fatalf("lifecycle_status=%q, want alive", lifecycleStatus)
		}
		assertNoRows(t, pool, "idempotency after blocked death exit", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "outbox after blocked death exit", "SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)

		approved := cmd
		approved.ClientIdempotencyKey = "idem-exit-death-approved-0001"
		approved.StoredIdempotencyKey = meshaTenant + ":criticalDeathGoat:" + created.Goat.GoatID + ":" + approved.ClientIdempotencyKey
		approved.IdempotencyScope = "criticalDeathGoat"
		approved.GuardrailApproved = true
		approved.RequestHash, err = app.CanonicalRequestHashWithSubject(meshaTenant, "criticalDeathGoat", "/admin/goats/{goat_id}/critical-death-exit", created.Goat.GoatID, raw)
		if err != nil {
			t.Fatal(err)
		}
		exited, err := repo.ExitGoat(ctx, approved)
		if err != nil {
			t.Fatalf("approved critical death exit: %v", err)
		}
		if exited.Goat.LifecycleStatus != "dead" || exited.Events[0].EventType != "goat.exited" {
			t.Fatalf("approved critical death result=%#v, want dead goat.exited", exited)
		}
		assertGoatLifecycleOutbox(t, pool, approved.StoredIdempotencyKey, exited.Events[0].EventID, created.Goat.GoatID, "goat.exited", created.Goat.GoatID)
	})

	t.Run("admin goat stage writes goat.stage_changed outbox for rule recheck", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-stage-0001", "aid1-admin-stage-0001", "admin-stage-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for stage: %v", err)
		}
		cmd := stageGoatCommand(t, "idem-stage-goat-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		staged, err := repo.StageGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("StageGoat: %v", err)
		}
		if staged.Goat.ManagementStage == nil || *staged.Goat.ManagementStage != "weaner" || staged.Events[0].EventType != "goat.stage_changed" {
			t.Fatalf("unexpected stage result: %#v", staged)
		}
		assertGoatLifecycleOutbox(t, pool, cmd.StoredIdempotencyKey, staged.Events[0].EventID, created.Goat.GoatID, "goat.stage_changed", created.Goat.GoatID)
	})

	t.Run("admin goat health writes goat.health.changed outbox for noncritical recovery recheck", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-health-0001", "aid1-admin-health-0001", "admin-health-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for health: %v", err)
		}
		if _, err := pool.Exec(ctx, `
	UPDATE goats
	SET health_status = 'sick',
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, meshaTenant, created.Goat.GoatID); err != nil {
			t.Fatalf("seed sick health: %v", err)
		}
		cmd := healthGoatCommand(t, "idem-health-goat-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		healed, err := repo.HealthGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("HealthGoat: %v", err)
		}
		if healed.Goat.HealthStatus == nil || *healed.Goat.HealthStatus != "healthy" || healed.Events[0].EventType != "goat.health.changed" {
			t.Fatalf("unexpected health result: %#v", healed)
		}
		assertGoatLifecycleOutbox(t, pool, cmd.StoredIdempotencyKey, healed.Events[0].EventID, created.Goat.GoatID, "goat.health.changed", created.Goat.GoatID)
	})

	t.Run("admin goat health blocks critical guardrail transitions", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-critical-health-0001", "aid1-admin-critical-health-0001", "admin-critical-health-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for critical health: %v", err)
		}
		cmd := healthGoatCommand(t, "idem-health-critical-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		cmd.HealthStatus = "icu"
		if _, err := repo.HealthGoat(ctx, cmd); !errors.Is(err, ports.ErrGuardrailRequired) {
			t.Fatalf("HealthGoat critical transition error = %v, want ErrGuardrailRequired", err)
		}
		var got string
		if err := pool.QueryRow(ctx, "SELECT health_status FROM goats WHERE tenant_id = $1 AND goat_id = $2", meshaTenant, created.Goat.GoatID).Scan(&got); err != nil {
			t.Fatalf("health after blocked critical transition: %v", err)
		}
		if got != "healthy" {
			t.Fatalf("health_status=%q, want healthy", got)
		}
		assertNoRows(t, pool, "idempotency after blocked critical health", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "outbox after blocked critical health", "SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
	})

	t.Run("admin goat health blocks critical guardrail exits", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-critical-exit-0001", "aid1-admin-critical-exit-0001", "admin-critical-exit-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for critical exit: %v", err)
		}
		if _, err := pool.Exec(ctx, `
	UPDATE goats
	SET health_status = 'quarantine',
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, meshaTenant, created.Goat.GoatID); err != nil {
			t.Fatalf("seed quarantine health: %v", err)
		}
		cmd := healthGoatCommand(t, "idem-health-critical-exit-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		if _, err := repo.HealthGoat(ctx, cmd); !errors.Is(err, ports.ErrGuardrailRequired) {
			t.Fatalf("HealthGoat critical exit error = %v, want ErrGuardrailRequired", err)
		}
		var got string
		if err := pool.QueryRow(ctx, "SELECT health_status FROM goats WHERE tenant_id = $1 AND goat_id = $2", meshaTenant, created.Goat.GoatID).Scan(&got); err != nil {
			t.Fatalf("health after blocked critical exit: %v", err)
		}
		if got != "quarantine" {
			t.Fatalf("health_status=%q, want quarantine", got)
		}
		assertNoRows(t, pool, "idempotency after blocked critical exit", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "outbox after blocked critical exit", "SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
	})

	t.Run("admin goat reproductive writes goat.reproductive.changed outbox and pregnancy timing", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-repro-0001", "aid1-admin-repro-0001", "admin-repro-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for reproductive: %v", err)
		}
		if _, err := pool.Exec(ctx, `
	UPDATE goats
	SET reproductive_status = 'non_pregnant',
	    row_version = row_version + 1
	WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, meshaTenant, created.Goat.GoatID); err != nil {
			t.Fatalf("seed non_pregnant reproductive: %v", err)
		}
		cmd := reproductiveGoatCommand(t, "idem-repro-goat-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		bred, err := repo.ReproductiveGoat(ctx, cmd)
		if err != nil {
			t.Fatalf("ReproductiveGoat: %v", err)
		}
		if bred.Events[0].EventType != "goat.reproductive.changed" {
			t.Fatalf("unexpected reproductive result: %#v", bred)
		}
		var (
			gotStatus   string
			gotBreeding *time.Time
		)
		if err := pool.QueryRow(ctx, "SELECT reproductive_status, breeding_date FROM goats WHERE tenant_id = $1 AND goat_id = $2", meshaTenant, created.Goat.GoatID).Scan(&gotStatus, &gotBreeding); err != nil {
			t.Fatalf("reproductive state after transition: %v", err)
		}
		if gotStatus != "pregnant" {
			t.Fatalf("reproductive_status=%q, want pregnant", gotStatus)
		}
		if gotBreeding == nil || gotBreeding.Format("2006-01-02") != "2026-06-01" {
			t.Fatalf("breeding_date=%v, want 2026-06-01", gotBreeding)
		}
		assertGoatLifecycleOutbox(t, pool, cmd.StoredIdempotencyKey, bred.Events[0].EventID, created.Goat.GoatID, "goat.reproductive.changed", created.Goat.GoatID)
	})

	t.Run("admin goat reproductive rejects unknown vocabulary before any write", func(t *testing.T) {
		create := adminGoatCreateCommand(t, "idem-create-goat-repro-invalid-0001", "aid1-admin-repro-invalid-0001", "admin-repro-invalid-aid2-0001")
		created, err := repo.CreateAdminGoat(ctx, create)
		if err != nil {
			t.Fatalf("CreateAdminGoat for invalid reproductive: %v", err)
		}
		cmd := reproductiveGoatCommand(t, "idem-repro-invalid-0001", created.Goat.GoatID, rowVersionForGoat(t, pool, created.Goat.GoatID))
		cmd.ReproductiveStatus = "not_a_real_status"
		if _, err := repo.ReproductiveGoat(ctx, cmd); !errors.Is(err, ports.ErrInvalidReference) {
			t.Fatalf("ReproductiveGoat invalid vocabulary error = %v, want ErrInvalidReference", err)
		}
		assertNoRows(t, pool, "idempotency after invalid reproductive", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "outbox after invalid reproductive", "SELECT count(*) FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
	})

	t.Run("admin goat create duplicate Animal ID 1 rolls back goat idempotency audit and outbox", func(t *testing.T) {
		first := adminGoatCreateCommand(t, "idem-create-goat-dupe-0001", "aid1-admin-create-dupe", "admin-create-aid2-dupe-0001")
		if _, err := repo.CreateAdminGoat(ctx, first); err != nil {
			t.Fatalf("first create: %v", err)
		}
		dupe := adminGoatCreateCommand(t, "idem-create-goat-dupe-0002", " AID1-ADMIN-CREATE-DUPE ", "admin-create-aid2-dupe-0002")
		if _, err := repo.CreateAdminGoat(ctx, dupe); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate Animal ID 1 write conflict, got %v", err)
		}
		assertNoRows(t, pool, "idempotency after duplicate create Animal ID 1", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", dupe.StoredIdempotencyKey)
		assertNoRows(t, pool, "Animal ID 2 identifier after duplicate create Animal ID 1", "SELECT count(*) FROM goat_identifiers WHERE tenant_id = $1 AND normalized_value = $2 AND scope_key = $3", meshaTenant, "ADMIN-CREATE-AID2-DUPE-0002", "global")
		assertNoRows(t, pool, "location history after duplicate create Animal ID 1", "SELECT count(*) FROM goat_location_history WHERE tenant_id = $1 AND source_record_id = $2", meshaTenant, *dupe.SourceRecordID)
		assertNoRows(t, pool, "decision after duplicate create Animal ID 1", "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", dupe.TraceID)
		assertNoRows(t, pool, "event after duplicate create Animal ID 1", "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", dupe.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after duplicate create Animal ID 1", "SELECT count(*) FROM audit_log WHERE trace_id = $1", dupe.TraceID)
		assertNoRows(t, pool, "outbox after duplicate create Animal ID 1", "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", dupe.TraceID)
	})

	t.Run("admin goat create forced failure rolls back goat identifiers decision event audit outbox and idempotency", func(t *testing.T) {
		cmd := adminGoatCreateCommand(t, "idem-create-goat-rollback-0001", "aid1-admin-create-rollback", "admin-create-aid2-rollback")
		cmd.TraceID = "trace-admin-create-forced-rollback"
		repo.afterAuditHook = func(context.Context) error { return errors.New("forced admin create rollback") }
		_, err := repo.CreateAdminGoat(ctx, cmd)
		repo.afterAuditHook = nil
		if err == nil {
			t.Fatal("expected forced admin create rollback error")
		}
		assertNoRows(t, pool, "idempotency after admin create rollback", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "Animal ID 1 identifier after admin create rollback", "SELECT count(*) FROM goat_identifiers WHERE tenant_id = $1 AND normalized_value = $2", meshaTenant, "AID1-ADMIN-CREATE-ROLLBACK")
		assertNoRows(t, pool, "Animal ID 2 identifier after admin create rollback", "SELECT count(*) FROM goat_identifiers WHERE tenant_id = $1 AND normalized_value = $2 AND scope_key = $3", meshaTenant, "ADMIN-CREATE-AID2-ROLLBACK", "global")
		assertNoRows(t, pool, "location history after admin create rollback", "SELECT count(*) FROM goat_location_history WHERE tenant_id = $1 AND source_record_id = $2", meshaTenant, *cmd.SourceRecordID)
		assertNoRows(t, pool, "decision after admin create rollback", "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", cmd.TraceID)
		assertNoRows(t, pool, "event after admin create rollback", "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after admin create rollback", "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID)
		assertNoRows(t, pool, "outbox after admin create rollback", "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID)
	})

	t.Run("add success writes identifier decision event audit outbox and schema-valid payloads", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-write-0001", goatID, "animal_identifier_1", " aid1-synthetic-0001 ", "global", false, goatRowVersion(t, pool, goatID))
		result, err := repo.AddGoatIdentifier(ctx, cmd)
		if err != nil {
			t.Fatalf("AddGoatIdentifier: %v", err)
		}
		if result.Replayed || result.Goat.GoatID != goatID || result.Decision.DecisionType != "attach_identifier" {
			t.Fatalf("unexpected add result: %#v", result)
		}
		if len(result.Identifiers) != 1 || result.Identifiers[0].IdentifierType != "animal_identifier_1" || result.Identifiers[0].Status != "active" {
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
		if normalized != "AID1-SYNTHETIC-0001" || status != "active" {
			t.Fatalf("identifier normalized/status = %s/%s", normalized, status)
		}
		assertIdentifierMutationRows(t, pool, cmd.StoredIdempotencyKey, goatID, identifierID, result.Decision.DecisionID, "attach", "goat.identifier.added")
		assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, identifierID)
	})

	t.Run("add exact replay rebuilds response and changed body conflicts", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-write-0002", goatID, "animal_identifier_1", "aid1-synthetic-0002", "global", false, 1)
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
		changed := addIdentifierCommand(t, meshaTenant, "idem-add-write-0002", goatID, "animal_identifier_1", "aid1-synthetic-0002-changed", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, changed); !errors.Is(err, ports.ErrIdempotencyConflict) {
			t.Fatalf("expected idempotency conflict, got %v", err)
		}
	})

	t.Run("duplicate active Animal ID 1 rolls back goat guard and idempotency", func(t *testing.T) {
		goatA := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		goatB := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-aid1-0001", goatA, "animal_identifier_1", "aid1-synthetic-dupe", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first Animal ID 1 add: %v", err)
		}
		dupe := addIdentifierCommand(t, meshaTenant, "idem-add-aid1-0002", goatB, "animal_identifier_1", " AID1-SYNTHETIC-DUPE ", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, dupe); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate Animal ID 1 write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatB); got != 1 {
			t.Fatalf("duplicate Animal ID 1 guard was not rolled back, row_version=%d", got)
		}
		assertNoRows(t, pool, "idempotency after duplicate Animal ID 1", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", dupe.StoredIdempotencyKey)
		assertNoRows(t, pool, "identifier after duplicate Animal ID 1", "SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = $2", goatB, "AID1-SYNTHETIC-DUPE")
	})

	t.Run("duplicate Animal ID 2 is rejected for lifetime even with another scope", func(t *testing.T) {
		goatA := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		goatB := insertSyntheticGoat(t, pool, meshaTenant, cptLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-aid2-0001", goatA, "animal_identifier_2", "synthetic-aid2-dupe", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first animal_identifier_2 add: %v", err)
		}
		sameScope := addIdentifierCommand(t, meshaTenant, "idem-add-aid2-0002", goatB, "animal_identifier_2", "synthetic-aid2-dupe", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, sameScope); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate animal_identifier_2 write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatB); got != 1 {
			t.Fatalf("same-scope duplicate was not rolled back, row_version=%d", got)
		}
		differentScope := addIdentifierCommand(t, meshaTenant, "idem-add-aid2-0003", goatB, "animal_identifier_2", "synthetic-aid2-dupe", "another-scope", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, differentScope); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected duplicate Animal ID 2 lifetime conflict in another scope, got %v", err)
		}
	})

	t.Run("primary identifier conflict rolls back row version", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		first := addIdentifierCommand(t, meshaTenant, "idem-add-primary-0001", goatID, "animal_identifier_1", "synthetic-primary-1", "global", true, 1)
		if _, err := repo.AddGoatIdentifier(ctx, first); err != nil {
			t.Fatalf("first primary add: %v", err)
		}
		rowVersion := goatRowVersion(t, pool, goatID)
		conflict := addIdentifierCommand(t, meshaTenant, "idem-add-primary-0002", goatID, "animal_identifier_1", "synthetic-primary-2", "global", true, rowVersion)
		if _, err := repo.AddGoatIdentifier(ctx, conflict); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected primary write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatID); got != rowVersion {
			t.Fatalf("primary conflict did not roll back goat row_version: got %d want %d", got, rowVersion)
		}
	})

	t.Run("primary disallowed by identifier policy rolls back row version", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-policy-primary-0001", goatID, "animal_identifier_2", "synthetic-aid2-primary", "global", true, 1)
		if _, err := repo.AddGoatIdentifier(ctx, cmd); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected primary_allowed policy write conflict, got %v", err)
		}
		if got := goatRowVersion(t, pool, goatID); got != 1 {
			t.Fatalf("policy conflict did not roll back goat row_version: got %d", got)
		}
		assertNoRows(t, pool, "identifier after primary policy conflict", "SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = $2", goatID, "synthetic-aid2-primary")
		assertNoRows(t, pool, "idempotency after primary policy conflict", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
	})

	t.Run("add rejects merged stale and wrong tenant goats", func(t *testing.T) {
		survivorID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		mergedID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		if _, err := pool.Exec(ctx, `UPDATE goats SET merged_into_goat_id = $1 WHERE goat_id = $2`, survivorID, mergedID); err != nil {
			t.Fatal(err)
		}
		merged := addIdentifierCommand(t, meshaTenant, "idem-add-merged-0001", mergedID, "animal_identifier_1", "aid1-synthetic-merged", "global", false, goatRowVersion(t, pool, mergedID))
		if _, err := repo.AddGoatIdentifier(ctx, merged); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected merged goat write conflict, got %v", err)
		}

		staleGoat := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		stale := addIdentifierCommand(t, meshaTenant, "idem-add-stale-0001", staleGoat, "animal_identifier_1", "aid1-synthetic-stale", "global", false, 2)
		if _, err := repo.AddGoatIdentifier(ctx, stale); !errors.Is(err, ports.ErrWriteConflict) {
			t.Fatalf("expected stale row_version conflict, got %v", err)
		}

		wrongTenant := addIdentifierCommand(t, secondTenant, "idem-add-wrongtenant-0001", staleGoat, "animal_identifier_1", "aid1-synthetic-wrongtenant", "global", false, 1)
		if _, err := repo.AddGoatIdentifier(ctx, wrongTenant); !errors.Is(err, ports.ErrNotFound) {
			t.Fatalf("expected wrong tenant not found, got %v", err)
		}
	})

	t.Run("add forced failure rolls back identifier decision event audit outbox and idempotency", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		cmd := addIdentifierCommand(t, meshaTenant, "idem-add-rollback-0001", goatID, "animal_identifier_1", "aid1-synthetic-rollback", "global", false, 1)
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
		assertNoRows(t, pool, "identifier after add rollback", "SELECT count(*) FROM goat_identifiers WHERE goat_id = $1 AND normalized_value = $2", goatID, "AID1-SYNTHETIC-ROLLBACK")
		assertNoRows(t, pool, "idempotency after add rollback", "SELECT count(*) FROM idempotency_keys WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "decision after add rollback", "SELECT count(*) FROM identity_decisions WHERE evidence->'decision_record'->>'trace_id' = $1", cmd.TraceID)
		assertNoRows(t, pool, "event after add rollback", "SELECT count(*) FROM goat_identity_events WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
		assertNoRows(t, pool, "audit after add rollback", "SELECT count(*) FROM audit_log WHERE trace_id = $1", cmd.TraceID)
		assertNoRows(t, pool, "outbox after add rollback", "SELECT count(*) FROM outbox_messages WHERE trace_id = $1", cmd.TraceID)
	})

	t.Run("retire success replay and changed body conflict", func(t *testing.T) {
		goatID := insertSyntheticGoat(t, pool, meshaTenant, cbeLocation)
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0001", goatID, "animal_identifier_2", "synthetic-retire-1", "global", false, 1))
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
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0002", goatA, "animal_identifier_2", "synthetic-retire-2", "global", false, 1))
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
		active, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-0003", activeGoat, "animal_identifier_2", "synthetic-retire-3", "global", false, 1))
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
		added, err := repo.AddGoatIdentifier(ctx, addIdentifierCommand(t, meshaTenant, "idem-retire-add-rollback-0001", goatID, "animal_identifier_2", "synthetic-retire-rollback", "global", false, 1))
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

// TestGoatDisplayIDNoTruncatePastMillion proves next_goat_display_id() no longer
// truncates once the sequence passes 999,999. Old lpad(nextval::text,6) turned
// 1000000 into '100000' (collision with G-100000); to_char(nextval,'FM000000')
// yields G-1000000 with no truncation, unique and format-valid.
func TestGoatDisplayIDNoTruncatePastMillion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, _ := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	// Straddle the old 6-digit boundary: next three values are 999999,1000000,1000001.
	if _, err := pool.Exec(ctx, `SELECT setval('goat_display_id_seq', 999998, true)`); err != nil {
		t.Fatalf("setval: %v", err)
	}
	want := []string{"G-999999", "G-1000000", "G-1000001"}
	seen := map[string]bool{}
	for i := range want {
		var id string
		if err := pool.QueryRow(ctx, `SELECT next_goat_display_id()`).Scan(&id); err != nil {
			t.Fatalf("next_goat_display_id(): %v", err)
		}
		if id != want[i] {
			t.Fatalf("display id %d = %q, want %q (lpad-truncation regression at the 1M boundary)", i, id, want[i])
		}
		if seen[id] {
			t.Fatalf("duplicate display id %q past 1M", id)
		}
		seen[id] = true
	}
}

func insertSyntheticGoat(t *testing.T, pool *pgxpool.Pool, tenantID, parkID string) string {
	t.Helper()
	var goatID string
	if err := pool.QueryRow(context.Background(), `
INSERT INTO goats (
  tenant_id,
  lifecycle_status,
  species,
  custodian_party_id,
  current_location_id,
  park_id,
  breed,
  sex
) VALUES (
  $1,
  'alive',
  'goat',
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

func seedAdminCreateLocations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name,
  parent_location_id, country, timezone, status
	) VALUES
	($1::uuid, $2::uuid, 'farm', 'ADMIN_CREATE_FARM', 'Synthetic admin create farm',
	 NULL, 'IN', 'Asia/Kolkata', 'active'),
	($3::uuid, $2::uuid, 'shed', 'ADMIN_CREATE_SHED', 'Synthetic admin create shed',
	 $4::uuid, 'IN', 'Asia/Kolkata', 'active'),
	($5::uuid, $2::uuid, 'shed', 'ADMIN_CREATE_ORPHAN_SHED', 'Synthetic admin create orphan shed',
	 NULL, 'IN', 'Asia/Kolkata', 'active'),
	($6::uuid, $2::uuid, 'shed', 'ADMIN_CREATE_WRONG_PARK_SHED', 'Synthetic admin create wrong-park shed',
	 $7::uuid, 'IN', 'Asia/Kolkata', 'active'),
	($8::uuid, $2::uuid, 'shed', 'ADMIN_MOVE_TARGET_SHED', 'Synthetic admin move target shed',
	 $4::uuid, 'IN', 'Asia/Kolkata', 'active')
ON CONFLICT (tenant_id, location_code) DO NOTHING`,
		adminCreateFarmLocation, meshaTenant, adminCreateShedLocation, cbeLocation, adminCreateOrphanShed, adminCreateWrongParkShed, cptLocation, adminMoveTargetShed); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
VALUES
  ($1::uuid, 'adult', 'Adult', 10, 'active'),
  ($1::uuid, 'weaner', 'Weaner', 20, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE
SET name = EXCLUDED.name,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    updated_at = now()`, meshaTenant); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM locations
WHERE tenant_id = $1
  AND location_id IN ($2, $3, $4, $5, $6)
  AND status = 'active'`, meshaTenant, adminCreateFarmLocation, adminCreateShedLocation, adminCreateOrphanShed, adminCreateWrongParkShed, adminMoveTargetShed); got != 5 {
		t.Fatalf("admin create fixture locations = %d, want 5", got)
	}
}

func adminGoatCreateCommand(t *testing.T, key, animalID1, animalID2 string) ports.CreateAdminGoatCommand {
	t.Helper()
	entryDate := time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC)
	dob := time.Date(2025, time.December, 15, 0, 0, 0, 0, time.UTC)
	breed := "Synthetic Boer"
	managementStage := "adult"
	healthStatus := "healthy"
	sourceRecordID := "synthetic-admin-goat-create-" + key
	description := "Synthetic source row for admin goat create."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   sourceRecordID,
		SourceSystem: &sourceSystem,
		Description:  &description,
	}}
	farmID := adminCreateFarmLocation
	body := map[string]any{
		"animal_identifier_1": strings.TrimSpace(animalID1),
		"animal_identifier_2": strings.TrimSpace(animalID2),
		"species":             "goat",
		"farm_id":             farmID,
		"park_id":             cbeLocation,
		"shed_id":             adminCreateShedLocation,
		"breed":               breed,
		"sex":                 "female",
		"dob":                 "2025-12-15",
		"dob_estimated":       true,
		"origin_type":         "procured",
		"entry_date":          "2026-06-25",
		"management_stage":    managementStage,
		"health_status":       healthStatus,
		"source_record_id":    sourceRecordID,
		"evidence_refs":       evidenceRefs,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, createAdminGoatCommandName, "/admin/goats", "", raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.CreateAdminGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":" + createAdminGoatCommandName + ":" + key,
		IdempotencyScope:     createAdminGoatCommandName,
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		Identifiers: []ports.AdminGoatCreateIdentifier{
			{
				IdentifierType:  "animal_identifier_1",
				IdentifierValue: strings.TrimSpace(animalID1),
				NormalizedValue: strings.ToUpper(strings.TrimSpace(animalID1)),
				ScopeKey:        "global",
				IsPrimary:       true,
			},
			{
				IdentifierType:  "animal_identifier_2",
				IdentifierValue: strings.TrimSpace(animalID2),
				NormalizedValue: strings.ToUpper(strings.TrimSpace(animalID2)),
				ScopeKey:        "global",
				IsPrimary:       false,
			},
		},
		CustodianPartyID: meshaParty,
		Species:          "goat",
		FarmID:           &farmID,
		ParkID:           cbeLocation,
		ShedID:           adminCreateShedLocation,
		Breed:            &breed,
		Sex:              "female",
		DOB:              &dob,
		DOBEstimated:     true,
		OriginType:       "procured",
		EntryDate:        entryDate,
		ManagementStage:  &managementStage,
		HealthStatus:     &healthStatus,
		SourceRecordID:   &sourceRecordID,
		EvidenceRefs:     evidenceRefs,
	}
}

func moveGoatCommand(t *testing.T, key, goatID string, rowVersion int) ports.MoveGoatCommand {
	t.Helper()
	reason := "Synthetic shed move for vaccination re-scope."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-move-" + key,
		SourceSystem: &sourceSystem,
	}}
	body := map[string]any{
		"park_id":       cbeLocation,
		"shed_id":       adminMoveTargetShed,
		"reason":        reason,
		"evidence_refs": evidenceRefs,
		"row_version":   rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "moveGoat", "/admin/goats/{goat_id}/move", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.MoveGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":moveGoat:" + goatID + ":" + key,
		IdempotencyScope:     "moveGoat",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		ToParkID:             cbeLocation,
		ToShedID:             adminMoveTargetShed,
		Reason:               reason,
		OccurredAt:           time.Date(2026, time.June, 26, 9, 0, 0, 0, time.UTC),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func exitGoatCommand(t *testing.T, key, goatID string, rowVersion int) ports.ExitGoatCommand {
	t.Helper()
	reason := "Synthetic sale exit for vaccination cancellation."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-exit-" + key,
		SourceSystem: &sourceSystem,
	}}
	body := map[string]any{
		"lifecycle_status": "sold",
		"exit_reason":      "sold",
		"reason":           reason,
		"evidence_refs":    evidenceRefs,
		"row_version":      rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "exitGoat", "/admin/goats/{goat_id}/exit", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.ExitGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":exitGoat:" + goatID + ":" + key,
		IdempotencyScope:     "exitGoat",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		LifecycleStatus:      "sold",
		ExitReason:           "sold",
		Reason:               reason,
		OccurredAt:           time.Date(2026, time.June, 26, 10, 0, 0, 0, time.UTC),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func stageGoatCommand(t *testing.T, key, goatID string, rowVersion int) ports.StageGoatCommand {
	t.Helper()
	reason := "Synthetic stage change for vaccination rule recheck."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-stage-" + key,
		SourceSystem: &sourceSystem,
	}}
	body := map[string]any{
		"management_stage": "weaner",
		"reason":           reason,
		"evidence_refs":    evidenceRefs,
		"row_version":      rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "stageGoat", "/admin/goats/{goat_id}/stage", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.StageGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":stageGoat:" + goatID + ":" + key,
		IdempotencyScope:     "stageGoat",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		ManagementStage:      "weaner",
		Reason:               reason,
		OccurredAt:           time.Date(2026, time.June, 26, 11, 0, 0, 0, time.UTC),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func healthGoatCommand(t *testing.T, key, goatID string, rowVersion int) ports.HealthGoatCommand {
	t.Helper()
	reason := "Synthetic health recovery for vaccination rule recheck."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-health-" + key,
		SourceSystem: &sourceSystem,
	}}
	body := map[string]any{
		"health_status": "healthy",
		"reason":        reason,
		"evidence_refs": evidenceRefs,
		"row_version":   rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "healthGoat", "/admin/goats/{goat_id}/health", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.HealthGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":healthGoat:" + goatID + ":" + key,
		IdempotencyScope:     "healthGoat",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		HealthStatus:         "healthy",
		Reason:               reason,
		OccurredAt:           time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func reproductiveGoatCommand(t *testing.T, key, goatID string, rowVersion int) ports.ReproductiveGoatCommand {
	t.Helper()
	reason := "Synthetic reproductive transition for vaccination pregnancy timing."
	sourceSystem := "synthetic_admin_register"
	evidenceRefs := []domain.EvidenceRef{{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-reproductive-" + key,
		SourceSystem: &sourceSystem,
	}}
	breedingDate := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	body := map[string]any{
		"reproductive_status": "pregnant",
		"breeding_date":       breedingDate.Format("2006-01-02"),
		"reason":              reason,
		"evidence_refs":       evidenceRefs,
		"row_version":         rowVersion,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "reproductiveGoat", "/admin/goats/{goat_id}/reproductive", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.ReproductiveGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":reproductiveGoat:" + goatID + ":" + key,
		IdempotencyScope:     "reproductiveGoat",
		RequestHash:          hash,
		TraceID:              "trace-" + key,
		GoatID:               goatID,
		ReproductiveStatus:   "pregnant",
		BreedingDate:         &breedingDate,
		Reason:               reason,
		OccurredAt:           time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC),
		EvidenceRefs:         evidenceRefs,
		RowVersion:           rowVersion,
	}
}

func identifierEvidenceRef() domain.EvidenceRef {
	description := "Synthetic source row for identifier mutation."
	sourceSystem := "synthetic_goatos_fixture"
	return domain.EvidenceRef{
		EvidenceType: "source_record",
		EvidenceID:   "synthetic-identifier-row-1",
		SourceSystem: &sourceSystem,
		Description:  &description,
	}
}

func normalizeIdentifierForTest(identifierType, value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func assertAdminGoatCreateRows(t *testing.T, pool *pgxpool.Pool, cmd ports.CreateAdminGoatCommand, result *ports.AdminGoatMutationResult) {
	t.Helper()
	ctx := context.Background()
	assertIdempotencyCompleted(t, pool, cmd.StoredIdempotencyKey, result.Goat.GoatID)
	if got := countRows(t, pool, `
SELECT count(*)
FROM goat_location_history
WHERE tenant_id = $1
  AND goat_id = $2
  AND to_location_id = $3
  AND reason = 'admin_goat_create'
  AND source_record_id = $4`, cmd.TenantID, result.Goat.GoatID, cmd.ShedID, stringValue(cmd.SourceRecordID)); got != 1 {
		t.Fatalf("goat location history rows = %d", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM identity_decision_goats
WHERE decision_id = $1
  AND goat_id = $2
  AND role = 'created'`, result.Decision.DecisionID, result.Goat.GoatID); got != 1 {
		t.Fatalf("decision goat rows = %d", got)
	}

	var eventID string
	var recordedAt string
	var linkedDecisionID string
	if err := pool.QueryRow(ctx, `
SELECT identity_event_id::text, recorded_at::text, decision_id::text
FROM goat_identity_events
WHERE idempotency_key = $1`, cmd.StoredIdempotencyKey).Scan(&eventID, &recordedAt, &linkedDecisionID); err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].EventID != eventID || result.Events[0].EventType != "goat.created" {
		t.Fatalf("unexpected create events: %#v", result.Events)
	}
	if linkedDecisionID != result.Decision.DecisionID {
		t.Fatalf("event decision_id = %s, want %s", linkedDecisionID, result.Decision.DecisionID)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM identity_decision_events
WHERE decision_id = $1
  AND event_id = $2
  AND event_recorded_at::text = $3`, result.Decision.DecisionID, eventID, recordedAt); got != 1 {
		t.Fatalf("decision-event linkage rows = %d", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM outbox_messages
WHERE idempotency_key = $1
  AND event_id = $2
  AND event_type = 'goat.created'
  AND aggregate_type = 'goat'
  AND aggregate_id = $3
  AND topic = 'identity.events'`, cmd.StoredIdempotencyKey, eventID, result.Goat.GoatID); got != 1 {
		t.Fatalf("outbox rows = %d", got)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM audit_log
WHERE trace_id = $1
  AND resource_type = 'goat'
  AND resource_id = $2
  AND action = 'goat.created'`, cmd.TraceID, result.Goat.GoatID); got != 1 {
		t.Fatalf("audit rows = %d", got)
	}

	decisionPayload := queryBytes(t, pool, "SELECT evidence->'decision_record' FROM identity_decisions WHERE decision_id = $1", result.Decision.DecisionID)
	validateDecisionRecord(t, decisionPayload)
	var decisionRecord map[string]any
	if err := json.Unmarshal(decisionPayload, &decisionRecord); err != nil {
		t.Fatal(err)
	}
	if decisionRecord["policy_version"] != adminGoatPolicyVersion {
		t.Fatalf("unexpected decision record policy: %#v", decisionRecord)
	}
	affectedGoats := decisionRecord["affected_goats"].([]any)
	affectedGoat := affectedGoats[0].(map[string]any)
	if affectedGoat["goat_id"] != result.Goat.GoatID || affectedGoat["role"] != "affected" {
		t.Fatalf("unexpected affected goat: %#v", affectedGoat)
	}

	payload := queryBytes(t, pool, "SELECT payload FROM outbox_messages WHERE idempotency_key = $1", cmd.StoredIdempotencyKey)
	validateDomainEventEnvelope(t, payload)
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["event_id"] != eventID || envelope["event_type"] != "goat.created" || envelope["aggregate_id"] != result.Goat.GoatID || envelope["subject_id"] != result.Goat.GoatID {
		t.Fatalf("unexpected outbox envelope: %#v", envelope)
	}
	visibility := envelope["visibility_scope"].(map[string]any)
	if visibility["tenant_id"] != meshaTenant || visibility["park_id"] != cbeLocation || visibility["shed_id"] != adminCreateShedLocation {
		t.Fatalf("visibility scope missing location: %#v", visibility)
	}
	eventPayload := envelope["payload"].(map[string]any)
	if eventPayload["generation_status"] != "queued" {
		t.Fatalf("unexpected generation status in payload: %#v", eventPayload)
	}
}

func rowVersionForGoat(t *testing.T, pool *pgxpool.Pool, goatID string) int {
	t.Helper()
	var rowVersion int
	if err := pool.QueryRow(context.Background(), `
SELECT row_version
FROM goats
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, meshaTenant, goatID).Scan(&rowVersion); err != nil {
		t.Fatal(err)
	}
	return rowVersion
}

func assertGoatLifecycleOutbox(t *testing.T, pool *pgxpool.Pool, idempotencyKey, eventID, goatID, eventType, scopeID string) {
	t.Helper()
	assertIdempotencyCompleted(t, pool, idempotencyKey, goatID)
	payload := queryBytes(t, pool, "SELECT payload FROM outbox_messages WHERE idempotency_key = $1", idempotencyKey)
	validateDomainEventEnvelope(t, payload)
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["event_id"] != eventID || envelope["event_type"] != eventType || envelope["aggregate_id"] != goatID || envelope["subject_id"] != goatID {
		t.Fatalf("unexpected lifecycle envelope: %#v", envelope)
	}
	eventPayload := envelope["payload"].(map[string]any)
	if eventPayload["scope_id"] != scopeID {
		t.Fatalf("payload scope_id = %#v want %s; payload=%#v", eventPayload["scope_id"], scopeID, eventPayload)
	}
	if got := countRows(t, pool, `
SELECT count(*)
FROM identity_decision_goats idg
JOIN goat_identity_events gie ON gie.tenant_id = idg.tenant_id AND gie.decision_id = idg.decision_id
WHERE gie.idempotency_key = $1
  AND idg.goat_id = $2::uuid
  AND idg.role = 'affected'`, idempotencyKey, goatID); got != 1 {
		t.Fatalf("decision goat linkage rows = %d", got)
	}
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

func hasFieldError(errors []domain.FieldError, field, code string) bool {
	for _, err := range errors {
		if err.Field == field && err.Code == code {
			return true
		}
	}
	return false
}

// identityGoatCommand builds a DOB/entry-date correction command. dob/entry are optional YYYY-MM-DD.
func identityGoatCommand(t *testing.T, key, goatID string, rowVersion int, dob, entry *string) ports.IdentityGoatCommand {
	t.Helper()
	sourceSystem := "synthetic_admin_register"
	body := map[string]any{
		"reason":        "Synthetic identity correction for vaccination anchors.",
		"evidence_refs": []domain.EvidenceRef{{EvidenceType: "source_record", EvidenceID: "synthetic-identity-" + key, SourceSystem: &sourceSystem}},
		"row_version":   rowVersion,
	}
	var dobT, entryT *time.Time
	if dob != nil {
		body["dob"] = *dob
		d, _ := time.Parse("2006-01-02", *dob)
		dobT = &d
	}
	if entry != nil {
		body["entry_date"] = *entry
		e, _ := time.Parse("2006-01-02", *entry)
		entryT = &e
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := app.CanonicalRequestHashWithSubject(meshaTenant, "identityGoat", "/admin/goats/{goat_id}/identity", goatID, raw)
	if err != nil {
		t.Fatal(err)
	}
	return ports.IdentityGoatCommand{
		TenantID:             meshaTenant,
		ActorID:              correctionActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: meshaTenant + ":identityGoat:" + goatID + ":" + key,
		IdempotencyScope:     "identityGoat",
		RequestHash:          hash,
		GoatID:               goatID,
		DOB:                  dobT,
		EntryDate:            entryT,
		Reason:               "Synthetic identity correction for vaccination anchors.",
		// india-date-guard:ignore: owner=ravi issue=CI-postgres-opt-in scope=test-event-absolute-instant expiry=2026-12-31
		OccurredAt: time.Now().UTC(),
		RowVersion: rowVersion,
	}
}

// TestIdentityGoatEnforcesChronology is the VACC-REV-08 guard: a correction must never move DOB after
// entry_date (or entry before DOB) on the EFFECTIVE pair, and a rejection makes no mutation, decision,
// or outbox event. The created goat has dob=2025-12-15, entry_date=2026-06-25.
func TestIdentityGoatEnforcesChronology(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	seedAdminCreateLocations(t, pool)

	create := adminGoatCreateCommand(t, "idem-create-identity-chrono", "aid1-identity-chrono", "aid2-identity-chrono")
	created, err := repo.CreateAdminGoat(ctx, create)
	if err != nil {
		t.Fatalf("CreateAdminGoat: %v", err)
	}
	goatID := created.Goat.GoatID

	assertNoMutation := func(t *testing.T, name string) {
		t.Helper()
		var dob, entry string
		if err := pool.QueryRow(ctx, `SELECT to_char(dob,'YYYY-MM-DD'), to_char(entry_date,'YYYY-MM-DD') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, meshaTenant, goatID).Scan(&dob, &entry); err != nil {
			t.Fatalf("%s: read goat: %v", name, err)
		}
		if dob != "2025-12-15" || entry != "2026-06-25" {
			t.Fatalf("%s: goat mutated on a rejected correction: dob=%s entry=%s", name, dob, entry)
		}
		n := 0
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.identity.changed'`, meshaTenant, goatID).Scan(&n); err != nil {
			t.Fatalf("%s: count events: %v", name, err)
		}
		if n != 0 {
			t.Fatalf("%s: a rejected correction emitted %d identity events", name, n)
		}
	}

	// DOB-only correction moving DOB after the stored arrival date.
	badDOB := "2026-07-01"
	if _, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-bad-dob", goatID, rowVersionForGoat(t, pool, goatID), &badDOB, nil)); !errors.Is(err, ports.ErrInvalidChronology) {
		t.Fatalf("DOB-after-entry error = %v, want ErrInvalidChronology", err)
	}
	assertNoMutation(t, "dob-only")

	// Entry-only correction moving arrival before the stored DOB.
	badEntry := "2025-11-01"
	if _, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-bad-entry", goatID, rowVersionForGoat(t, pool, goatID), nil, &badEntry)); !errors.Is(err, ports.ErrInvalidChronology) {
		t.Fatalf("entry-before-dob error = %v, want ErrInvalidChronology", err)
	}
	assertNoMutation(t, "entry-only")

	// Two-field correction with an inverted pair.
	twoDOB, twoEntry := "2026-03-01", "2026-01-01"
	if _, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-bad-both", goatID, rowVersionForGoat(t, pool, goatID), &twoDOB, &twoEntry)); !errors.Is(err, ports.ErrInvalidChronology) {
		t.Fatalf("inverted two-field error = %v, want ErrInvalidChronology", err)
	}
	assertNoMutation(t, "two-field")

	// VACC-REV-12: a SUBMITTED DOB or entry_date in the future is rejected at the repo (defense-in-depth
	// for any non-HTTP caller); a rejection makes no mutation or event. 2999 is safely past cmd.OccurredAt.
	futureDOB := "2999-01-01"
	if _, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-future-dob", goatID, rowVersionForGoat(t, pool, goatID), &futureDOB, nil)); !errors.Is(err, ports.ErrFutureAnchor) {
		t.Fatalf("future dob error = %v, want ErrFutureAnchor", err)
	}
	assertNoMutation(t, "future-dob")

	futureEntry := "2999-01-01"
	if _, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-future-entry", goatID, rowVersionForGoat(t, pool, goatID), nil, &futureEntry)); !errors.Is(err, ports.ErrFutureAnchor) {
		t.Fatalf("future entry_date error = %v, want ErrFutureAnchor", err)
	}
	assertNoMutation(t, "future-entry")

	// A valid DOB-only correction (still on/before entry) succeeds and emits the event.
	goodDOB := "2025-11-01"
	res, err := repo.IdentityGoat(ctx, identityGoatCommand(t, "idem-identity-good", goatID, rowVersionForGoat(t, pool, goatID), &goodDOB, nil))
	if err != nil {
		t.Fatalf("valid correction rejected: %v", err)
	}
	if len(res.Events) == 0 || res.Events[0].EventType != "goat.identity.changed" {
		t.Fatalf("valid correction did not emit goat.identity.changed: %#v", res)
	}
}
