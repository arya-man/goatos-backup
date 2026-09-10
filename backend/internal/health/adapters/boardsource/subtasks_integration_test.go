package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

func subtaskQuery(sourceID string) ports.SubtaskQuery {
	return ports.SubtaskQuery{TenantID: hbTenant, ParkID: hbPark, BusinessDate: hbDate, SourceID: sourceID, Limit: 10}
}

func states(steps []domain.Step) string {
	out := ""
	for _, s := range steps {
		out += string(s.State) + " "
	}
	return out
}

// TestHealthSubtasksAreTreatmentStepsWorstFirstOnADatabaseRoundTrip asserts the OUTPUT of the
// per-step drill: one subtask per treatment step named by the medicine or the action, the
// pending step before the given ones, the session itself when it has no steps, the person who
// completed the session, and nothing for the other park.
func TestHealthSubtasksAreTreatmentStepsWorstFirstOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// The in-progress session: two medicines given, one still pending -- the pending one first.
	exec(t, ctx, pool, `UPDATE health_session_steps SET dosage_text = '2 ml', medicine_route = 'intramuscular' WHERE health_session_id = $1::uuid`, ids[4])
	page, err := src.ListSubtasks(ctx, subtaskQuery(ids[4]))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Subtasks) != 3 || page.NextCursor != "" {
		t.Fatalf("in-progress session %+v", page)
	}
	pending := page.Subtasks[0]
	if pending.Name != "Enrofloxacin" || pending.Subtitle != "2 ml · Intramuscular" || pending.WorkState != domain.WorkStateInProgress || pending.Lane != domain.LaneInProgress {
		t.Fatalf("pending step %+v", pending)
	}
	if states(pending.Steps) != "in_progress locked " || pending.Steps[0].Name != "Give" {
		t.Fatalf("pending chain %+v", pending.Steps)
	}
	for _, given := range page.Subtasks[1:] {
		if given.WorkState != domain.WorkStateCompleted || given.Lane != domain.LaneDone || states(given.Steps) != "done in_review " {
			t.Fatalf("given step %+v", given)
		}
	}

	// A due session with a plain action: named by its instruction, to do.
	exec(t, ctx, pool, `
INSERT INTO health_session_steps (tenant_id, health_session_id, seq, record_type, instruction, status)
VALUES ($1::uuid, $2::uuid, 1, 'action', 'Isolate the animal', 'pending')`, hbTenant, ids[3])
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[3]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Isolate the animal" || page.Subtasks[0].WorkState != domain.WorkStateDue || page.Subtasks[0].Steps[0].Name != "Do" || page.Subtasks[0].Steps[0].State != domain.StepTodo {
		t.Fatalf("action step %+v", page)
	}

	// No steps materialised: the session itself, once. Completed by Dinakar, awaiting review.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[5]))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("a live session never drills into nothing: %+v", page)
	}
	whole := page.Subtasks[0]
	if whole.Name != "Mastitis treatment" || whole.Subtitle != "No steps recorded" || whole.WorkState != domain.WorkStateCompleted || whole.Owner.Name != "Dinakar" || whole.Owner.UserID != hbOperator || states(whole.Steps) != "done in_review " {
		t.Fatalf("stepless completed session %+v", whole)
	}
	// Sent back: needs attention, the review step in rework.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[6]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || page.Subtasks[0].Steps[1].State != domain.StepRework {
		t.Fatalf("rework session %+v", page)
	}
	// Held while the death is reviewed: blocked.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[7]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateBlocked || page.Subtasks[0].Steps[0].State != domain.StepLocked {
		t.Fatalf("held session %+v", page)
	}
	// Canceled and the other park drill into nothing.
	var other string
	if err := pool.QueryRow(ctx, `SELECT health_session_id::text FROM health_treatment_sessions WHERE health_case_id = $1::uuid`, hbCaseOth).Scan(&other); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"canceled": ids[9], "other park": other} {
		page, err := src.ListSubtasks(ctx, subtaskQuery(id))
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, page)
		}
	}
}
