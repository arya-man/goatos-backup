package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestBirthColostrumRepairUsesBirthTimeAndAddsNextDay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	const tenantID = "c0000000-0000-4000-8000-000000000001"
	const workflowID = "c0000000-0000-4000-8000-000000000002"
	birthAt := time.Date(2026, 7, 28, 12, 19, 0, 0, time.FixedZone("IST", 5*60*60+30*60))
	if _, err := pool.Exec(ctx, `
INSERT INTO workflow_instances (
  workflow_id, tenant_id, template_key, module, subject_goat_id, event_at, event_date,
  actions_total, actions_done
) VALUES ($1::uuid, $2::uuid, 'birth_kid', 'birth', gen_random_uuid(), $3, DATE '2026-07-28', 8, 0)`,
		workflowID, tenantID, birthAt); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workflow_actions (
  tenant_id, workflow_id, action_key, seq, section, action_type, title, detail,
  requires_video, options, due_at
) VALUES
  ($1::uuid,$2::uuid,'take_weight',6,'main','question_select','Take Weight of Kid','old bands',true,'["Below 2.0 kg"]', $3),
  ($1::uuid,$2::uuid,'tag_the_kid',8,'main','action','Tag the kid','tag',true,NULL,$3 + interval '2 days'),
  ($1::uuid,$2::uuid,'colostrum_session_1',9,'colostrum_session','action','Colostrum session · 07:00','old',true,NULL, timestamptz '2026-07-28 07:00 Asia/Kolkata'),
  ($1::uuid,$2::uuid,'colostrum_session_2',10,'colostrum_session','action','Colostrum session · 11:00','old',true,NULL, timestamptz '2026-07-28 11:00 Asia/Kolkata'),
  ($1::uuid,$2::uuid,'colostrum_session_3',11,'colostrum_session','action','Colostrum session · 15:00','old',true,NULL, timestamptz '2026-07-28 15:00 Asia/Kolkata'),
  ($1::uuid,$2::uuid,'colostrum_session_4',12,'colostrum_session','action','Colostrum session · 18:30','old',true,NULL, timestamptz '2026-07-28 18:30 Asia/Kolkata'),
  ($1::uuid,$2::uuid,'colostrum_session_5',13,'colostrum_session','action','Colostrum session · 22:00','old',true,NULL, timestamptz '2026-07-28 22:00 Asia/Kolkata')`,
		tenantID, workflowID, birthAt); err != nil {
		t.Fatalf("seed legacy actions: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE workflow_actions
SET status='completed', answer_value='2.0 – 2.5 kg', proof_ref='legacy-weight-proof',
    completed_at=$2, idempotency_key='legacy-weight-key', request_fingerprint='legacy-weight-fp'
WHERE workflow_id=$1::uuid AND action_key='take_weight'`, workflowID, birthAt); err != nil {
		t.Fatalf("complete legacy band weight: %v", err)
	}

	migration, err := os.ReadFile("000046_birth_weight_and_colostrum_repair.sql")
	if err != nil {
		t.Fatalf("read repair migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(migration))); err != nil {
		t.Fatalf("apply repair migration: %v", err)
	}

	rows, err := pool.Query(ctx, `
SELECT due_at AT TIME ZONE 'Asia/Kolkata'
FROM workflow_actions
WHERE workflow_id=$1::uuid AND section='colostrum_session'
ORDER BY due_at`, workflowID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []time.Time
	for rows.Next() {
		var due time.Time
		if err := rows.Scan(&due); err != nil {
			t.Fatal(err)
		}
		got = append(got, due)
	}
	want := []time.Time{
		time.Date(2026, 7, 28, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 28, 18, 30, 0, 0, time.UTC),
		time.Date(2026, 7, 28, 22, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 29, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 29, 11, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 29, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 29, 18, 30, 0, 0, time.UTC),
		time.Date(2026, 7, 29, 22, 0, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("repaired sessions=%v, want=%v", got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("session %d=%v, want=%v", i, got[i], want[i])
		}
	}

	var actionType string
	var optionsNil bool
	var status string
	var answerNil, proofNil bool
	if err := pool.QueryRow(ctx, `
SELECT action_type, options IS NULL, status, answer_value IS NULL, proof_ref IS NULL
FROM workflow_actions WHERE workflow_id=$1::uuid AND action_key='take_weight'`, workflowID).
		Scan(&actionType, &optionsNil, &status, &answerNil, &proofNil); err != nil {
		t.Fatal(err)
	}
	if actionType != "question" || !optionsNil || status != "pending" || !answerNil || !proofNil {
		t.Fatalf("weight action type=%q options_nil=%v status=%q answer_nil=%v proof_nil=%v",
			actionType, optionsNil, status, answerNil, proofNil)
	}
}

func migrationUp(sqlText string) string {
	up := strings.SplitN(sqlText, "-- +goose Down", 2)[0]
	return strings.TrimSpace(strings.TrimPrefix(up, "-- +goose Up"))
}
