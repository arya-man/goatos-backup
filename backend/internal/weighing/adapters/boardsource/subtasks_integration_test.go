package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

func subtaskQuery(sourceID, after string, limit int) ports.SubtaskQuery {
	return ports.SubtaskQuery{TenantID: bsTenant, ParkID: bsPark, BusinessDate: bsDate, SourceID: sourceID, AfterKey: after, Limit: limit}
}

func stepStates(steps []domain.Step) []domain.StepState {
	out := make([]domain.StepState, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.State)
	}
	return out
}

func sameStates(got []domain.StepState, want ...domain.StepState) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// TestWeighingSubtasksListScannedAnimalsWorstFirstOnADatabaseRoundTrip asserts the OUTPUT of
// the per-animal drill: the tag string verbatim, the weight, the four-link chain in each
// verification status, the recorder's name, the worst-first order and the keyset with its
// whole count -- on a real database.
func TestWeighingSubtasksListScannedAnimalsWorstFirstOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)
	const bucket = "00000000-0000-4000-8000-000000009101"
	const proofID = "00000000-0000-4000-8000-000000009301"

	// Ten more scans on the individual bucket (12 with the seed's two), then one sent back and
	// one verified, so every branch of the chain and a second page are on the table.
	for _, tag := range []string{"tag-c", "tag-d", "tag-e", "tag-f", "tag-g", "tag-h", "tag-i", "tag-j", "tag-k", "tag-l"} {
		exec(t, ctx, pool, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 14.25, $5::uuid, $6::uuid, 'board-' || $4)`, bsTenant, "00000000-0000-4000-8000-000000009001", bucket, tag, proofID, bsOperator)
	}
	exec(t, ctx, pool, `UPDATE weighing_observations SET verification_status = 'rework', rework_reason = 'Scale not in frame', submitted_at = now() WHERE scanned_identifier = 'tag-a'`)
	exec(t, ctx, pool, `UPDATE weighing_observations SET verification_status = 'verified', verified_at = now(), submitted_at = now() WHERE scanned_identifier = 'tag-b'`)

	page, err := src.ListSubtasks(ctx, subtaskQuery(ids[bucket], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 12 || len(page.Subtasks) != 10 || page.NextCursor == "" {
		t.Fatalf("page 1: total %d, %d subtasks, next %q", page.Total, len(page.Subtasks), page.NextCursor)
	}
	first := page.Subtasks[0]
	if first.Name != "tag-a" || first.Subtitle != "12.5 kg" || !first.NeedsAttention || first.WorkState != domain.WorkStateRejected || first.Lane != domain.LaneInProgress {
		t.Fatalf("the sent-back animal comes first: %+v", first)
	}
	if !sameStates(stepStates(first.Steps), domain.StepDone, domain.StepDone, domain.StepRework, domain.StepLocked) || first.Steps[2].Detail != "Scale not in frame" || first.Steps[0].Detail != "12.5 kg" {
		t.Fatalf("rework chain %+v", first.Steps)
	}
	if first.Owner.Name != "Dinakar" || first.Owner.UserID != bsOperator || first.Owner.WorkforceMemberID != bsMember {
		t.Fatalf("owner %+v", first.Owner)
	}
	for _, st := range page.Subtasks[1:] {
		if st.WorkState != domain.WorkStateInProgress || st.NeedsAttention || st.Subtitle != "14.2 kg" && st.Subtitle != "14.3 kg" {
			t.Fatalf("a pending scan under a capturing bucket is in progress: %+v", st)
		}
		if !sameStates(stepStates(st.Steps), domain.StepDone, domain.StepTodo, domain.StepLocked, domain.StepLocked) {
			t.Fatalf("pending chain %+v", st.Steps)
		}
	}
	page2, err := src.ListSubtasks(ctx, subtaskQuery(ids[bucket], page.NextCursor, 10))
	if err != nil {
		t.Fatal(err)
	}
	if page2.Total != 12 || len(page2.Subtasks) != 2 || page2.NextCursor != "" {
		t.Fatalf("page 2: total %d, %d subtasks, next %q", page2.Total, len(page2.Subtasks), page2.NextCursor)
	}
	last := page2.Subtasks[1]
	if last.Name != "tag-b" || last.WorkState != domain.WorkStateCompleted || last.Lane != domain.LaneDone {
		t.Fatalf("the verified animal comes last: %+v", last)
	}
	if !sameStates(stepStates(last.Steps), domain.StepDone, domain.StepDone, domain.StepDone, domain.StepTodo) {
		t.Fatalf("verified chain (bucket not closed yet) %+v", last.Steps)
	}
	seen := map[string]bool{}
	for _, st := range append(page.Subtasks, page2.Subtasks...) {
		if seen[st.Key] {
			t.Fatalf("subtask %s served twice", st.Key)
		}
		seen[st.Key] = true
	}
}

// TestWeighingSubtasksWholePenAndScope: a lump-sum bucket drills into the pen itself -- as a
// to-do before it is weighed, as the one observation afterwards -- and a bucket outside the
// query's park/day, or canceled, drills into nothing.
func TestWeighingSubtasksWholePenAndScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seed(t, ctx, pool)
	src := New(pool, 5*time.Second)
	const proofID = "00000000-0000-4000-8000-000000009301"

	// 9102: whole pen, in progress, nothing weighed yet.
	page, err := src.ListSubtasks(ctx, subtaskQuery(ids["00000000-0000-4000-8000-000000009102"], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("a live whole-pen bucket never drills into nothing: %+v", page)
	}
	pen := page.Subtasks[0]
	if pen.Name != "Whole pen" || pen.Subtitle != "Not weighed yet" || pen.WorkState != domain.WorkStateDue || pen.Lane != domain.LaneToDo {
		t.Fatalf("unweighed pen %+v", pen)
	}
	if !sameStates(stepStates(pen.Steps), domain.StepTodo, domain.StepTodo, domain.StepLocked, domain.StepLocked) || pen.Steps[0].Name != "Weigh" {
		t.Fatalf("unweighed chain %+v", pen.Steps)
	}
	// 9103: whole pen submitted (bucket completed), one observation awaiting the verdict.
	exec(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 312, 12, 26, $4::uuid, $5::uuid, 'board-pen-9103')`,
		bsTenant, "00000000-0000-4000-8000-000000009003", "00000000-0000-4000-8000-000000009103", proofID, bsOperator)
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids["00000000-0000-4000-8000-000000009103"], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("submitted pen %+v", page)
	}
	pen = page.Subtasks[0]
	if pen.Name != "Whole pen" || pen.Subtitle != "312.0 kg · 26 animals" || pen.WorkState != domain.WorkStateVerificationPending || pen.Lane != domain.LaneInReview || pen.Owner.Name != "Dinakar" {
		t.Fatalf("submitted pen %+v", pen)
	}
	if !sameStates(stepStates(pen.Steps), domain.StepDone, domain.StepDone, domain.StepInReview, domain.StepLocked) {
		t.Fatalf("submitted chain %+v", pen.Steps)
	}
	// 9108: closed after submit, observation verified -> every link done.
	exec(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, verification_status, verified_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 100, 10, 10, $4::uuid, $5::uuid, 'board-pen-9108', 'verified', now())`,
		bsTenant, "00000000-0000-4000-8000-000000009008", "00000000-0000-4000-8000-000000009108", proofID, bsOtherOp)
	page, err = src.ListSubtasks(ctx, subtaskQuery(ids["00000000-0000-4000-8000-000000009108"], "", 10))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || !sameStates(stepStates(page.Subtasks[0].Steps), domain.StepDone, domain.StepDone, domain.StepDone, domain.StepDone) {
		t.Fatalf("closed pen %+v", page)
	}
	// Scope: the other park, another day, and a canceled bucket each drill into nothing.
	for name, q := range map[string]ports.SubtaskQuery{
		"other park": {TenantID: bsTenant, ParkID: bsOtherPk, BusinessDate: bsDate, SourceID: ids["00000000-0000-4000-8000-000000009102"], Limit: 10},
		"other day":  {TenantID: bsTenant, ParkID: bsPark, BusinessDate: "2026-09-11", SourceID: ids["00000000-0000-4000-8000-000000009102"], Limit: 10},
		"canceled":   subtaskQuery(ids["00000000-0000-4000-8000-000000009107"], "", 10),
	} {
		page, err := src.ListSubtasks(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, page)
		}
	}
	if _, err := src.ListSubtasks(ctx, subtaskQuery(ids["00000000-0000-4000-8000-000000009102"], "garbage", 10)); err == nil {
		t.Fatal("a malformed cursor must be refused")
	}
}
