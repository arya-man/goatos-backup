package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/sop/ports"
)

// Android captures wall-clock time as epoch milliseconds. Values in current years are far above
// PostgreSQL int4, so the SQL parameter must be typed as bigint before NULLIF infers its type.
func TestScanWritesAcceptAndroidEpochMilliseconds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenantID   = "00000000-0000-4000-8000-000000000001"
		actorID    = "76000000-0000-4000-8000-000000000099"
		sopID      = "76000000-0000-4000-8000-000000000001"
		sopVersion = "76000000-0000-4000-8000-000000000002"
		taskID     = "76000000-0000-4000-8000-000000000003"
		capturedAt = int64(1_784_498_195_350)
	)

	if _, err := pool.Exec(ctx, `
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.scan_epoch_regression', 'Scan epoch regression', 'active')`,
		sopID, tenantID); err != nil {
		t.Fatalf("seed SOP definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_versions (
  sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published',
  '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
  '{"required":false}'::jsonb,
  '{"valid":true,"errors":[],"warnings":[]}'::jsonb
)`, sopVersion, tenantID, sopID); err != nil {
		t.Fatalf("seed SOP version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_tasks (
  task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination', 'Scan epoch regression',
  'queued', 'tenant', $2::uuid
)`, taskID, tenantID, sopID, sopVersion); err != nil {
		t.Fatalf("seed scan task: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	capture, err := repo.RecordScanCapture(ctx, ports.RecordScanCaptureCommand{
		TenantID:       tenantID,
		ActorID:        actorID,
		TaskID:         taskID,
		IdempotencyKey: "scan-epoch-capture",
		Body: domain.ScanCaptureRequest{
			FieldKey:     "vaccinated_goats",
			Tag:          "VAX-EPOCH-1",
			CapturedAtMs: int64Ptr(capturedAt),
		},
	})
	if err != nil {
		t.Fatalf("RecordScanCapture(epoch milliseconds): %v", err)
	}
	attempt, err := repo.RecordScanAttempt(ctx, ports.RecordScanAttemptCommand{
		TenantID:       tenantID,
		ActorID:        actorID,
		TaskID:         taskID,
		IdempotencyKey: "scan-epoch-attempt",
		Body: domain.ScanAttemptRequest{
			FieldKey:     "vaccinated_goats",
			Tag:          "VAX-EPOCH-1",
			Outcome:      "accepted",
			TagRole:      "primary",
			CapturedAtMs: int64Ptr(capturedAt),
		},
	})
	if err != nil {
		t.Fatalf("RecordScanAttempt(epoch milliseconds): %v", err)
	}

	want := time.UnixMilli(capturedAt).UTC()
	for name, raw := range map[string]string{"capture": capture.CapturedAt, "attempt": attempt.CapturedAt} {
		got, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatalf("parse %s captured_at %q: %v", name, raw, err)
		}
		if !got.Equal(want) {
			t.Fatalf("%s captured_at=%s want %s", name, got, want)
		}
	}
}

func int64Ptr(value int64) *int64 { return &value }
