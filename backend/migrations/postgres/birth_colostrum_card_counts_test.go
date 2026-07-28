package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestBirthColostrumCardCountsIncludeEveryVisibleOperatorAction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	const tenantID = "d0000000-0000-4000-8000-000000000001"
	const workflowID = "d0000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx, `
INSERT INTO workflow_instances (
  workflow_id, tenant_id, template_key, module, subject_goat_id, event_at, event_date,
  actions_total, actions_done, next_action_key, next_action_title
) VALUES (
  $1::uuid, $2::uuid, 'birth_kid', 'birth', gen_random_uuid(),
  timestamptz '2026-07-28 12:19 Asia/Kolkata', DATE '2026-07-28',
  8, 7, 'tag_the_kid', 'Tag the kid'
)`, workflowID, tenantID); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, requires_video, status
)
SELECT
  $1::uuid,
  $2::uuid,
  CASE WHEN seq = 8 THEN 'tag_the_kid' ELSE 'main_' || seq::text END,
  CASE WHEN seq = 8 THEN 16 ELSE seq END,
  'main', 'action',
  CASE WHEN seq = 8 THEN 'Tag the kid' ELSE 'Main ' || seq::text END,
  true,
  CASE WHEN seq <= 7 THEN 'completed' ELSE 'pending' END
FROM generate_series(1, 8) AS seq
UNION ALL
SELECT
  $1::uuid, $2::uuid, 'colostrum_' || seq::text, seq + 7,
  'colostrum_session', 'action', 'Colostrum ' || seq::text, true,
  CASE WHEN seq <= 2 THEN 'completed' ELSE 'pending' END
FROM generate_series(1, 8) AS seq`, tenantID, workflowID); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	migration, err := os.ReadFile("000047_birth_colostrum_card_counts.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up := migrationUp(string(migration))
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	var total, done, rowVersion int
	var nextKey string
	if err := pool.QueryRow(ctx, `
SELECT actions_total, actions_done, next_action_key, row_version
FROM workflow_instances
WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid`, tenantID, workflowID).
		Scan(&total, &done, &nextKey, &rowVersion); err != nil {
		t.Fatal(err)
	}
	if total != 16 || done != 9 || nextKey != "colostrum_3" {
		t.Fatalf("card=%d/%d next=%q, want 9/16 next colostrum_3", done, total, nextKey)
	}

	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("reapply migration: %v", err)
	}
	var replayVersion int
	if err := pool.QueryRow(ctx, `
SELECT row_version FROM workflow_instances
WHERE tenant_id=$1::uuid AND workflow_id=$2::uuid`, tenantID, workflowID).Scan(&replayVersion); err != nil {
		t.Fatal(err)
	}
	if replayVersion != rowVersion {
		t.Fatalf("idempotent replay row_version=%d, want unchanged %d", replayVersion, rowVersion)
	}
}
