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
	return ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: sourceID, Limit: 10}
}

func states(steps []domain.Step) string {
	out := ""
	for _, s := range steps {
		out += string(s.State) + " "
	}
	return out
}

// TestFeedTransportSubtasksAreAttemptsWorstFirstOnADatabaseRoundTrip asserts the OUTPUT of the
// per-attempt drill: the trip as a to-do before any attempt, one subtask per filmed attempt with
// the operator who filmed it, the rejected attempt first, and the whole count.
func TestFeedTransportSubtasksAreAttemptsWorstFirstOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// Nothing filmed yet: the trip itself, owned by nobody.
	page, err := src.ListSubtasks(ctx, subtaskQuery(taskDue))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("an owed trip never drills into nothing: %+v", page)
	}
	trip := page.Subtasks[0]
	if trip.Name != "Trip" || trip.Subtitle != "Not filmed yet" || trip.WorkState != domain.WorkStateDue || trip.Lane != domain.LaneToDo || trip.Owner.Name != "" {
		t.Fatalf("owed trip %+v", trip)
	}
	if states(trip.Steps) != "todo locked " || trip.Steps[0].Name != "Film the trip" || trip.Steps[1].Name != "Verify" {
		t.Fatalf("owed chain %+v", trip.Steps)
	}

	// Filmed once: the attempt, awaiting the verdict, owned by the operator who filmed it.
	page, err = src.ListSubtasks(ctx, subtaskQuery(taskFilmed))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("filmed %+v", page)
	}
	att := page.Subtasks[0]
	if att.Name != "Trip" || att.Subtitle != "Filmed 15:30" || att.WorkState != domain.WorkStateVerificationPending || att.Lane != domain.LaneInReview {
		t.Fatalf("filmed attempt %+v", att)
	}
	if states(att.Steps) != "done in_review " || att.Owner.Name != "Dinakar" || att.Owner.UserID != bsOperator || att.Owner.WorkforceMemberID != bsMember {
		t.Fatalf("filmed chain %+v owner %+v", att.Steps, att.Owner)
	}

	// Sent back and re-shot: the rejected attempt sorts first with its reason, the re-shoot is
	// named as such, and the count is both.
	exec(t, ctx, pool, `
INSERT INTO feed_transport_attempts (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, rejection_reason, idempotency_key, submitted_at)
VALUES ('00000000-0000-4000-8000-000000009902', $1::uuid, $2::uuid, 1, 'proof:1', $3::uuid, 'rejected', 'Truck not in frame', 'board-att-1', '2026-09-10 14:00:00+05:30'),
       ('00000000-0000-4000-8000-000000009903', $1::uuid, $2::uuid, 2, 'proof:2', $3::uuid, 'verification_due', NULL, 'board-att-2', '2026-09-10 14:40:00+05:30')`,
		bsTenant, taskRework, bsOperator)
	page, err = src.ListSubtasks(ctx, subtaskQuery(taskRework))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Subtasks) != 2 || page.NextCursor != "" {
		t.Fatalf("rework %+v", page)
	}
	rejected, reshoot := page.Subtasks[0], page.Subtasks[1]
	if rejected.Name != "Trip" || !rejected.NeedsAttention || rejected.WorkState != domain.WorkStateRejected || rejected.Steps[1].State != domain.StepRework || rejected.Steps[1].Detail != "Truck not in frame" {
		t.Fatalf("rejected attempt %+v", rejected)
	}
	if reshoot.Name != "Trip · attempt 2" || reshoot.Subtitle != "Filmed 14:40" || reshoot.WorkState != domain.WorkStateVerificationPending {
		t.Fatalf("re-shoot %+v", reshoot)
	}

	// Approved: done.
	exec(t, ctx, pool, `
INSERT INTO feed_transport_attempts (attempt_id, tenant_id, task_id, attempt_no, proof_ref, operator_id, status, idempotency_key, submitted_at, verified_at)
VALUES ('00000000-0000-4000-8000-000000009904', $1::uuid, $2::uuid, 1, 'proof:3', $3::uuid, 'approved', 'board-att-3', '2026-09-10 13:00:00+05:30', now())`,
		bsTenant, taskDone, bsOtherOp)
	page, err = src.ListSubtasks(ctx, subtaskQuery(taskDone))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || states(page.Subtasks[0].Steps) != "done done " {
		t.Fatalf("approved %+v", page)
	}

	// Retired, the other park and another day drill into nothing.
	for name, q := range map[string]ports.SubtaskQuery{
		"retired":    subtaskQuery(taskRetired),
		"other park": {TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, SourceID: taskDue, Limit: 10},
		"other day":  {TenantID: bsTenant, ParkID: bsPark, BusinessDate: "2026-09-11", SourceID: taskDue, Limit: 10},
	} {
		page, err := src.ListSubtasks(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, page)
		}
	}
}
