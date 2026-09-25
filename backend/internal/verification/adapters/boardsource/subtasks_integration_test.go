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

// TestVerificationSubtasksAreTheItemsProofsOnADatabaseRoundTrip asserts the OUTPUT of the
// per-proof drill: one subtask per media reference named by what it is (video / photo), the
// review step in the item's status with the verdict reason, the operator whose work it is,
// and one proof for an item whose references cannot be resolved.
func TestVerificationSubtasksAreTheItemsProofsOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seed(t, ctx, pool)
	src := New(pool, 5*time.Second)

	// A pending item with one unresolvable reference: one proof, in review, operator named.
	page, err := src.ListSubtasks(ctx, subtaskQuery(itemPending))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("pending %+v", page)
	}
	proof := page.Subtasks[0]
	if proof.Name != "Proof" || proof.WorkState != domain.WorkStateVerificationPending || proof.Lane != domain.LaneInReview || proof.Owner.Name != "Dinakar" {
		t.Fatalf("pending proof %+v", proof)
	}
	if len(proof.Steps) != 1 || proof.Steps[0].Name != "Review" || proof.Steps[0].State != domain.StepInReview {
		t.Fatalf("pending chain %+v", proof.Steps)
	}

	// A rejected item: needs attention, the reason on the review step.
	page, err = src.ListSubtasks(ctx, subtaskQuery(itemRejected))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || page.Subtasks[0].Steps[0].State != domain.StepRework || page.Subtasks[0].Steps[0].Detail != "Wrong animal in frame" {
		t.Fatalf("rejected %+v", page)
	}

	// An approved item carrying two real artifacts: a video and a photo, in order, both done.
	const video, photo = "00000000-0000-4000-8000-00000000a101", "00000000-0000-4000-8000-00000000a102"
	for _, a := range []struct{ id, mime, kind string }{{video, "video/mp4", "video"}, {photo, "image/jpeg", "photo"}} {
		exec(t, ctx, pool, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'board-test/' || $1, $3, 'completed', 'shed', $4::uuid, 'shed', $4::uuid, $5, $6::uuid, now())`,
			a.id, bsTenant, a.mime, bsShedB, a.kind, bsOperator)
	}
	exec(t, ctx, pool, `UPDATE verification_items SET media_refs = $2::jsonb WHERE item_id = $1::uuid`, itemApproved, `["`+video+`","`+photo+`"]`)
	page, err = src.ListSubtasks(ctx, subtaskQuery(itemApproved))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Subtasks) != 2 || page.NextCursor != "" {
		t.Fatalf("approved %+v", page)
	}
	if page.Subtasks[0].Name != "Video 1" || page.Subtasks[1].Name != "Photo 2" || page.Subtasks[0].Subtitle != "Castro 2 · 41.5 kg" {
		t.Fatalf("proof names %q %q subtitle %q", page.Subtasks[0].Name, page.Subtasks[1].Name, page.Subtasks[0].Subtitle)
	}
	for _, st := range page.Subtasks {
		if st.WorkState != domain.WorkStateCompleted || st.Lane != domain.LaneDone || st.Steps[0].State != domain.StepDone {
			t.Fatalf("approved proof %+v", st)
		}
	}

	// Withdrawn, another day and the other park drill into nothing.
	for name, q := range map[string]ports.SubtaskQuery{
		"withdrawn":  subtaskQuery(itemWithdrawn),
		"next day":   subtaskQuery(itemNextDay),
		"other park": subtaskQuery(itemOtherPark),
		"feed video": subtaskQuery(itemFeed),
		"milk video": subtaskQuery(itemMilk),
		"weighing":   subtaskQuery(itemWeighing),
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
