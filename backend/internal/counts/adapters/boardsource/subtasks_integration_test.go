package boardsource

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

func states(steps []domain.Step) string {
	out := ""
	for _, s := range steps {
		out += string(s.State) + " "
	}
	return out
}

// TestApprovalSubtasksAreTheRequestOnADatabaseRoundTrip asserts the OUTPUT of the approval
// drill: the request as one subtask named by its farm word, the kids or animals it names,
// the raise -> approve -> apply chain in each status with the raiser and the reason, and
// nothing for a request outside the park or day.
func TestApprovalSubtasksAreTheRequestOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seedApprovals(t, ctx, pool)
	src := NewApprovals(pool, 5*time.Second)
	q := func(id string) ports.SubtaskQuery {
		return ports.SubtaskQuery{TenantID: apTenant, ParkID: apPark, BusinessDate: apDate, SourceID: id, Limit: 10}
	}

	rows, err := src.ListRows(ctx, approvalQuery(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Href != "/approvals?ap_row="+r.SourceID {
			t.Fatalf("%s href %q", r.SourceID, r.Href)
		}
	}
	page, err := src.ListSubtasks(ctx, q(ids["birth-pending"]))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("pending birth %+v", page)
	}
	birth := page.Subtasks[0]
	if birth.Name != "Birth" || birth.Subtitle != "1 kid" || birth.WorkState != domain.WorkStateDue || birth.Lane != domain.LaneToDo || birth.Owner.Name != "" {
		t.Fatalf("pending birth %+v", birth)
	}
	if states(birth.Steps) != "done todo locked " || birth.Steps[0].Detail != "Raised 00:10 by Dinakar" || birth.Steps[1].Name != "Approve" || birth.Steps[2].Name != "Apply" {
		t.Fatalf("pending chain %+v", birth.Steps)
	}

	page, err = src.ListSubtasks(ctx, q(ids["shift-pending"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Pen move" || page.Subtasks[0].Subtitle != "3 animals" {
		t.Fatalf("pen move %+v", page)
	}
	// An approved pen move's Apply step follows the shifting event, never the decision.
	for key, want := range map[string]struct {
		steps string
		state domain.WorkState
	}{
		"shift-authorized": {"done done todo ", domain.WorkStateInProgress},
		"shift-filmed":     {"done done in_review ", domain.WorkStateVerificationPending},
		"shift-applied":    {"done done done ", domain.WorkStateCompleted},
	} {
		page, err = src.ListSubtasks(ctx, q(ids[key]))
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Subtasks) != 1 || states(page.Subtasks[0].Steps) != want.steps || page.Subtasks[0].WorkState != want.state {
			t.Fatalf("%s: %+v", key, page)
		}
	}
	page, err = src.ListSubtasks(ctx, q(ids["death-pending"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Death" || page.Subtasks[0].Subtitle != "1 animal" {
		t.Fatalf("death %+v", page)
	}

	page, err = src.ListSubtasks(ctx, q(ids["birth-approved"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || states(page.Subtasks[0].Steps) != "done done done " || page.Subtasks[0].Steps[1].Detail != "Approved 11:00" {
		t.Fatalf("approved %+v", page)
	}
	page, err = src.ListSubtasks(ctx, q(ids["birth-rejected"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || states(page.Subtasks[0].Steps) != "done rework locked " || page.Subtasks[0].Steps[1].Detail != "not this pen" {
		t.Fatalf("rejected %+v", page)
	}
	for name, id := range map[string]string{"yesterday": ids["birth-yesterday"], "other park": ids["shift-other-park"]} {
		page, err := src.ListSubtasks(ctx, q(id))
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 0 || len(page.Subtasks) != 0 {
			t.Fatalf("%s must drill into nothing: %+v", name, page)
		}
	}
}

// TestMilkFeedingSubtasksAreTheSessionOnADatabaseRoundTrip asserts the OUTPUT of the milk
// drill: the session as one subtask with its clock and head count, the prepare -> feed ->
// submit -> verify chain in each status, the operator once one has submitted, and nothing for
// a retired row or another park.
func TestMilkFeedingSubtasksAreTheSessionOnADatabaseRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	ids := seedMilk(t, ctx, pool)
	src := NewMilkFeeding(pool, 5*time.Second)
	q := func(id string) ports.SubtaskQuery {
		return ports.SubtaskQuery{TenantID: mkTenant, ParkID: mkPark, BusinessDate: mkDate, SourceID: id, Limit: 10}
	}

	rows, err := src.ListRows(ctx, ports.SourceQuery{TenantID: mkTenant, ParkID: mkPark, BusinessDate: mkDate, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Href != "/counts/milk-preparation?mp_park="+mkPark {
			t.Fatalf("%s href %q", r.SourceID, r.Href)
		}
	}
	page, err := src.ListSubtasks(ctx, q(ids[1]))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Subtasks) != 1 {
		t.Fatalf("owed session %+v", page)
	}
	s1 := page.Subtasks[0]
	if s1.Name != "Session 1" || s1.Subtitle != "08:00 · 12 kids" || s1.WorkState != domain.WorkStateDue || s1.Owner.Name != "" {
		t.Fatalf("owed session %+v", s1)
	}
	if states(s1.Steps) != "todo todo locked " || s1.Steps[0].Name != "Feed" || s1.Steps[1].Name != "Submit" || s1.Steps[2].Name != "Verify" {
		t.Fatalf("owed chain %+v", s1.Steps)
	}
	page, err = src.ListSubtasks(ctx, q(ids[2]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].Name != "Session 2" || page.Subtasks[0].WorkState != domain.WorkStateVerificationPending || states(page.Subtasks[0].Steps) != "done done in_review " || page.Subtasks[0].Owner.Name != "Dinakar" {
		t.Fatalf("submitted session %+v", page)
	}
	page, err = src.ListSubtasks(ctx, q(ids[3]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || page.Subtasks[0].WorkState != domain.WorkStateCompleted || states(page.Subtasks[0].Steps) != "done done done " {
		t.Fatalf("completed session %+v", page)
	}
	page, err = src.ListSubtasks(ctx, q(ids[4]))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subtasks) != 1 || !page.Subtasks[0].NeedsAttention || page.Subtasks[0].WorkState != domain.WorkStateRejected || states(page.Subtasks[0].Steps) != "done rework rework " {
		t.Fatalf("rework session %+v", page)
	}
	// The retired legacy row and the other park's session drill into nothing.
	var retired string
	if err := pool.QueryRow(ctx, `SELECT task_id::text FROM milk_feeding_tasks WHERE tenant_id = $1::uuid AND status = 'retired' LIMIT 1`, mkTenant).Scan(&retired); err != nil {
		t.Fatal(err)
	}
	page, err = src.ListSubtasks(ctx, q(retired))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Subtasks) != 0 {
		t.Fatalf("retired must drill into nothing: %+v", page)
	}
	page, err = src.ListSubtasks(ctx, ports.SubtaskQuery{TenantID: mkTenant, ParkID: mkOtherPk, BusinessDate: mkDate, SourceID: ids[1], Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("other park must drill into nothing: %+v", page)
	}
}
