package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// seedRemoval gives a campaign its feed & water removal task and, when status is not empty, the
// pen's evidence row in that status.
func seedRemoval(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, campaign, bucket, status, reason string) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO weighing_fasting_tasks (fasting_task_id, tenant_id, campaign_id, park_id, operator_user_id, planned_weigh_date, weigh_business_date, idempotency_key, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::date, $6::date, 'board-removal-' || $1, $5::uuid)`,
		taskID, bsTenant, campaign, bsPark, bsOtherOp, bsDate)
	if status == "" {
		return
	}
	const proofID = "00000000-0000-4000-8000-000000009301"
	exec(t, ctx, pool, `
INSERT INTO weighing_fasting_shed_proofs (tenant_id, fasting_task_id, campaign_shed_id, shed_label, feed_proof_ref, water_proof_ref, status, rework_reason)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Godel 1', $4::uuid, $4::uuid, $5, NULLIF($6, ''))`,
		bsTenant, taskID, bucket, proofID, status, reason)
}

// TestFeedAndWaterRemovalIsPartOfTheWeighingCard: the removal the evening before the weigh
// belongs to the weighing task (maintainer, 2026-09-25) and its verification items are left to
// this card by the Verification lane, so it must show HERE: one "Feed & water removal" unit in
// the pen's list, and a removal sent back turns an open pen's card rejected. Once the pen is
// weighed and submitted the re-shoot never re-blocks the weigh, so the card stays in review
// while the unit still says sent back. A campaign with no removal has no such unit.
func TestFeedAndWaterRemovalIsPartOfTheWeighingCard(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	const (
		openPen     = "00000000-0000-4000-8000-000000009102" // whole pen, in progress, nothing weighed
		scanPen     = "00000000-0000-4000-8000-000000009101" // individual, capturing
		submitted   = "00000000-0000-4000-8000-000000009109" // whole pen, submitted
		noRemovalPn = "00000000-0000-4000-8000-000000009106" // no removal at all
	)
	seedRemoval(t, ctx, pool, "00000000-0000-4000-8000-000000009401", "00000000-0000-4000-8000-000000009002", openPen, "rework", "Water trough still full")
	seedRemoval(t, ctx, pool, "00000000-0000-4000-8000-000000009402", "00000000-0000-4000-8000-000000009001", scanPen, "", "")
	seedRemoval(t, ctx, pool, "00000000-0000-4000-8000-000000009403", "00000000-0000-4000-8000-000000009009", submitted, "rework", "Feed tray visible")

	// The open pen: its removal was sent back, so the card is rejected with attention 1.
	rows, err := src.ListRows(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	cards := byBucket(rows)
	if c := cards[ids[openPen]]; c.WorkState != domain.WorkStateRejected || c.Counts.NeedsAttention != 1 {
		t.Fatalf("a pen whose removal was sent back needs attention: %+v", c)
	}
	// The submitted pen stays in review: the re-shoot never re-blocks the weigh.
	if c := cards[ids[submitted]]; c.WorkState != domain.WorkStateVerificationPending || c.Counts.NeedsAttention != 0 {
		t.Fatalf("a submitted pen stays in review whatever its removal verdict: %+v", c)
	}
	// Rows and counts describe one set: the count query agrees with the rows, state by state.
	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	fromRows := map[domain.WorkState]int{}
	for _, r := range rows {
		fromRows[r.WorkState]++
	}
	for state, n := range fromRows {
		if counts[state] != n {
			t.Fatalf("state %s: %d rows but count %d (%v)", state, n, counts[state], counts)
		}
	}
	// And the rejected filter finds the open pen through the same arm.
	rejected, err := src.ListRows(ctx, query("", domain.WorkStateRejected))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byBucket(rejected)[ids[openPen]]; !ok {
		t.Fatalf("the rejected filter must find the pen whose removal was sent back: %+v", rejected)
	}

	// The open pen's list: the sent-back removal first, then the pen still to weigh.
	page, err := src.ListSubtasks(ctx, subtaskQuery(ids[openPen], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Subtasks) != 2 {
		t.Fatalf("removal + pen: %+v", page)
	}
	removal := page.Subtasks[0]
	if removal.Name != "Feed & water removal" || removal.WorkState != domain.WorkStateRejected || !removal.NeedsAttention || removal.Subtitle != "Sent back" {
		t.Fatalf("sent-back removal %+v", removal)
	}
	if !sameStates(stepStates(removal.Steps), domain.StepDone, domain.StepRework) || removal.Steps[1].Detail != "Water trough still full" {
		t.Fatalf("sent-back removal chain %+v", removal.Steps)
	}
	if removal.Owner.UserID != bsOtherOp {
		t.Fatalf("the removal belongs to the removal operator, not the weigher: %+v", removal.Owner)
	}
	if page.Subtasks[1].Name != "Whole pen" {
		t.Fatalf("the pen follows its removal: %+v", page.Subtasks[1])
	}

	// The capturing pen: removal owed, nothing recorded yet -> a to-do, beside the two scans.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[scanPen], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 {
		t.Fatalf("two scans + the removal: %+v", page)
	}
	var owed *domain.Subtask
	for i := range page.Subtasks {
		if page.Subtasks[i].Name == "Feed & water removal" {
			owed = &page.Subtasks[i]
		}
	}
	if owed == nil || owed.WorkState != domain.WorkStateDue || owed.Lane != domain.LaneToDo ||
		!sameStates(stepStates(owed.Steps), domain.StepTodo, domain.StepLocked) {
		t.Fatalf("unrecorded removal %+v", owed)
	}

	// The submitted pen: the removal unit still says sent back.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[submitted], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, st := range page.Subtasks {
		if st.Name == "Feed & water removal" {
			found = st.WorkState == domain.WorkStateRejected && st.Steps[1].Detail == "Feed tray visible"
		}
	}
	if !found {
		t.Fatalf("the submitted pen's removal still shows sent back: %+v", page.Subtasks)
	}

	// A campaign with no removal: no removal unit.
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids[noRemovalPn], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range page.Subtasks {
		if st.Name == "Feed & water removal" {
			t.Fatalf("a campaign without a removal must not list one: %+v", page.Subtasks)
		}
	}
}
