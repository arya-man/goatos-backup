package postgres

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	ltTenant    = "00000000-0000-4000-8000-00000000a001"
	ltDirector  = "00000000-0000-4000-8000-00000000d001"
	ltDirector2 = "00000000-0000-4000-8000-00000000d002"
	ltCXO       = "00000000-0000-4000-8000-00000000c001"
	ltCXO2      = "00000000-0000-4000-8000-00000000c002"
	ltProof1    = "00000000-0000-4000-8000-00000000f001"
	ltProof2    = "00000000-0000-4000-8000-00000000f002"
)

// seedLeadershipFixture inserts only EXTERNAL facts: the tenant, four roster rows with
// their user ids, the assignable people's grants/ticks, and two completed attachment proofs. Every
// task row, number, seen stamp, audit row and outbox message below is produced by the
// repository under test.
func seedLeadershipFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Leadership Tasks Test', 'active') ON CONFLICT DO NOTHING`, ltTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	people := []struct{ userID, code, name string }{
		{ltDirector, "HEM", "Hemant"},
		{ltDirector2, "DIN", "Dinakar"},
		{ltCXO, "RAV", "Ravi"},
		{ltCXO2, "MAN", "Manohar"},
	}
	for _, p := range people {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active')`, ltTenant, p.userID, p.code, p.name); err != nil {
			t.Fatalf("seed member %s: %v", p.name, err)
		}
	}
	// Assignability is the person's own mobile tick at Oversee PLUS an active leadership grant
	// (CEO/CXO, park head, director). Dinakar below is ticked with no grant: never assignable.
	for _, cxo := range []string{ltCXO, ltCXO2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, cxo); err != nil {
			t.Fatalf("seed grant: %v", err)
		}
	}
	for _, ticked := range []string{ltCXO, ltCXO2, ltDirector2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT $1::uuid, workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[]
FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ticked); err != nil {
			t.Fatalf("seed tick: %v", err)
		}
	}
	// A leadership-shaped grant with NO tick: must never be assignable.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, ltDirector); err != nil {
		t.Fatalf("seed unticked grant: %v", err)
	}
	for _, proof := range []string{ltProof1, ltProof2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, size_bytes, upload_state, scope_type, scope_id, subject_type, proof_type, uploaded_by)
VALUES ($1::uuid, $2::uuid, 'local', 'attachments/' || $1::text, 'audio/mp4', 4096, 'completed', 'tenant', $2::uuid, 'other', 'attachment', $3::uuid)`, proof, ltTenant, ltDirector); err != nil {
			t.Fatalf("seed proof: %v", err)
		}
	}
}

func raiseParams(assignee, title, key string, proofs ...string) ports.RaiseParams {
	atts := make([]domain.Attachment, 0, len(proofs))
	for i, p := range proofs {
		atts = append(atts, domain.Attachment{ProofID: p, Kind: domain.AttachmentAudio, MimeType: "audio/mp4", FileName: "note.m4a", SizeBytes: 4096, Position: i})
	}
	return ports.RaiseParams{
		TenantID: ltTenant, ActorID: ltDirector, AssigneeUserID: assignee,
		Title: title, Body: "Please look at this.", Attachments: atts, IdempotencyKey: key,
	}
}

// TestLeadershipTaskLifecyclePostgresPaths walks the whole machine against a real Postgres:
// numbering, the idempotent raise replay, name resolution, the seen stamp and badge count,
// the status ladder under the row-version fence, the raiser's edit and cancel, and the
// outbox rows every write owes.
func TestLeadershipTaskLifecyclePostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	director := domain.Actor{UserID: ltDirector, CanRaise: true}
	cxo := domain.Actor{UserID: ltCXO, CanAct: true}

	// 1. Raise: number #1, names resolved, attachments stored from the resolved facts.
	task, err := repo.Raise(ctx, raiseParams(ltCXO, "Approve the vendor contract", "raise-1", ltProof1, ltProof2))
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if task.TaskNo != 1 || task.Status != domain.StatusOpen || task.RaisedByName != "Hemant" || task.AssigneeName != "Ravi" {
		t.Fatalf("raised task = %+v", task)
	}
	if len(task.Attachments) != 2 || task.Attachments[0].ProofID != ltProof1 || task.Attachments[0].MimeType != "audio/mp4" {
		t.Fatalf("attachments = %+v", task.Attachments)
	}
	// 2. An exact replay returns the SAME task and mints no second number.
	replay, err := repo.Raise(ctx, raiseParams(ltCXO, "Approve the vendor contract", "raise-1", ltProof1, ltProof2))
	if err != nil || replay.TaskID != task.TaskID || replay.TaskNo != 1 {
		t.Fatalf("replay = %+v err %v", replay, err)
	}
	// ...and a same-key different-payload replay is refused.
	if _, err := repo.Raise(ctx, raiseParams(ltCXO, "A different ask", "raise-1")); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay: %v", err)
	}
	// 3. An UNTICKED person is refused inside the write, whatever the picker said -- even one
	// holding a leadership role.
	if _, err := repo.Raise(ctx, raiseParams(ltDirector, "Not ticked", "raise-bad")); !errors.Is(err, domain.ErrAssigneeNotAssignable) {
		t.Fatalf("unticked assignee: %v", err)
	}

	// 4. The badge: one unseen task for Ravi, none for Manohar, none for the raiser.
	if n, _ := repo.UnseenCount(ctx, ltTenant, ltCXO); n != 1 {
		t.Fatalf("unseen for assignee = %d, want 1", n)
	}
	if n, _ := repo.UnseenCount(ctx, ltTenant, ltCXO2); n != 0 {
		t.Fatalf("unseen for another CXO = %d, want 0", n)
	}
	if n, _ := repo.UnseenCount(ctx, ltTenant, ltDirector); n != 0 {
		t.Fatalf("unseen for raiser = %d, want 0", n)
	}
	// Seen is stamped once; a replay changes nothing; the badge drops to zero.
	seen, err := repo.MarkSeen(ctx, ltTenant, task.TaskID, ltCXO)
	if err != nil || seen.SeenAt == nil {
		t.Fatalf("mark seen: %+v err %v", seen, err)
	}
	again, _ := repo.MarkSeen(ctx, ltTenant, task.TaskID, ltCXO)
	if again.SeenAt == nil || !again.SeenAt.Equal(*seen.SeenAt) {
		t.Fatalf("seen must be stamped once: %v vs %v", again.SeenAt, seen.SeenAt)
	}
	if n, _ := repo.UnseenCount(ctx, ltTenant, ltCXO); n != 0 {
		t.Fatalf("unseen after open = %d, want 0", n)
	}

	// 5. Status ladder under the version fence: a stale row_version is refused, then Start.
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: 99, IdempotencyKey: "st-stale"}); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale version: %v", err)
	}
	started, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: seen.RowVersion, IdempotencyKey: "st-1"})
	if err != nil || started.Status != domain.StatusInProgress {
		t.Fatalf("start: %+v err %v", started, err)
	}
	// The raiser cannot walk the ladder; the assignee cannot cancel.
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: director, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: started.RowVersion, IdempotencyKey: "st-2"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("raiser completing: %v", err)
	}
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: started.RowVersion, IdempotencyKey: "st-3"}); !errors.Is(err, domain.ErrNotRaiser) {
		t.Fatalf("assignee cancelling: %v", err)
	}

	// 6. Two-way notes append chronologically: the assignee and raiser both write to the
	// activity stream, while the compatibility comment field still carries the assignee note.
	commented, err := repo.SetComment(ctx, ports.CommentParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Comment: "Checking with park head.", IdempotencyKey: "comment-1"})
	if err != nil || commented.AssigneeComment != "Checking with park head." {
		t.Fatalf("assignee note: %+v err %v", commented, err)
	}
	replied, err := repo.SetComment(ctx, ports.CommentParams{TenantID: ltTenant, Actor: director, TaskID: task.TaskID, Comment: "Add a voice note when done.", IdempotencyKey: "comment-2"})
	if err != nil {
		t.Fatalf("raiser note: %+v err %v", replied, err)
	}
	if replied.AssigneeComment != "Checking with park head." {
		t.Fatalf("raiser note overwrote compatibility comment: %q", replied.AssigneeComment)
	}
	if len(replied.Notes) != 2 || replied.Notes[0].AuthorID != ltCXO || replied.Notes[1].AuthorID != ltDirector || replied.Notes[1].Body != "Add a voice note when done." {
		t.Fatalf("notes = %+v", replied.Notes)
	}

	// 7. Edit by the raiser: brief replaced, attachment list diffed (proof 2 dropped).
	edited, err := repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "Approve the vendor contract (revised)", Body: "New brief.",
		Attachments: []domain.Attachment{{ProofID: ltProof1, Kind: domain.AttachmentAudio, MimeType: "audio/mp4", FileName: "note.m4a", SizeBytes: 4096}},
		RowVersion:  replied.RowVersion, IdempotencyKey: "edit-1",
	})
	if err != nil || edited.Title != "Approve the vendor contract (revised)" || len(edited.Attachments) != 1 || edited.Attachments[0].ProofID != ltProof1 {
		t.Fatalf("edit: %+v err %v", edited, err)
	}
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector2, TaskID: task.TaskID, Title: "x", RowVersion: edited.RowVersion, IdempotencyKey: "edit-2"}); !errors.Is(err, domain.ErrNotRaiser) {
		t.Fatalf("another director editing: %v", err)
	}
	// 7b. A leadership monitor who is neither party edits and notes the task (CEO decision
	// 2026-09-18) through the SAME domain predicates the payload flags answer with.
	leader := domain.Actor{UserID: ltDirector2, CanRaise: true, CanAct: true, CanMonitor: true}
	edited, err = repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: leader.UserID, Actor: leader, TaskID: task.TaskID, Title: "Approve the vendor contract (revised)", Body: "New brief, leadership edit.",
		Attachments: []domain.Attachment{{ProofID: ltProof1, Kind: domain.AttachmentAudio, MimeType: "audio/mp4", FileName: "note.m4a", SizeBytes: 4096}},
		RowVersion:  edited.RowVersion, IdempotencyKey: "edit-2b",
	})
	if err != nil || edited.Body != "New brief, leadership edit." {
		t.Fatalf("monitor edit: %+v err %v", edited, err)
	}
	edited, err = repo.SetComment(ctx, ports.CommentParams{TenantID: ltTenant, Actor: leader, TaskID: task.TaskID, Comment: "Leadership note.", IdempotencyKey: "cm-2b"})
	if err != nil || len(edited.Notes) != 3 || edited.Notes[2].AuthorID != leader.UserID {
		t.Fatalf("monitor note: %+v err %v", edited.Notes, err)
	}

	// 8. Done, then the raiser can no longer edit; the assignee may reopen.
	done, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: edited.RowVersion, IdempotencyKey: "st-4"})
	if err != nil || done.Status != domain.StatusDone || done.DoneAt == nil {
		t.Fatalf("done: %+v err %v", done, err)
	}
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "late", RowVersion: done.RowVersion, IdempotencyKey: "edit-3"}); !errors.Is(err, domain.ErrTaskClosed) {
		t.Fatalf("edit after done: %v", err)
	}
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: leader.UserID, Actor: leader, TaskID: task.TaskID, Title: "late", RowVersion: done.RowVersion, IdempotencyKey: "edit-3b"}); !errors.Is(err, domain.ErrTaskClosed) {
		t.Fatalf("monitor edit after done: %v", err)
	}
	// A monitor walks the ladder too: reopen, then hand back to the assignee's reopen path.
	reopened, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: leader, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: done.RowVersion, IdempotencyKey: "st-5b"})
	if err != nil || reopened.Status != domain.StatusInProgress || reopened.DoneAt != nil {
		t.Fatalf("monitor reopen: %+v err %v", reopened, err)
	}
	done, err = repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: leader, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: reopened.RowVersion, IdempotencyKey: "st-5c"})
	if err != nil || done.Status != domain.StatusDone {
		t.Fatalf("monitor done again: %+v err %v", done, err)
	}
	reopened, err = repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: done.RowVersion, IdempotencyKey: "st-5"})
	if err != nil || reopened.Status != domain.StatusInProgress || reopened.DoneAt != nil {
		t.Fatalf("reopen: %+v err %v", reopened, err)
	}
	// 9. Cancel by the raiser is terminal.
	cancelled, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: director, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: reopened.RowVersion, IdempotencyKey: "st-6"})
	if err != nil || cancelled.Status != domain.StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel: %+v err %v", cancelled, err)
	}
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusOpen, RowVersion: cancelled.RowVersion, IdempotencyKey: "st-7"}); !errors.Is(err, domain.ErrTaskClosed) {
		t.Fatalf("reopening a cancelled task: %v", err)
	}

	// 10. Every write announced itself: one raised event, five status events (start, done,
	// reopen, cancel) -- the refused transitions emitted nothing.
	var raised, statusChanged int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE event_type = 'leadership_task.raised'), count(*) FILTER (WHERE event_type = 'leadership_task.status_changed') FROM outbox_messages WHERE tenant_id = $1 AND aggregate_id = $2`, ltTenant, task.TaskID).Scan(&raised, &statusChanged); err != nil {
		t.Fatalf("outbox count: %v", err)
	}
	if raised != 1 || statusChanged != 4 {
		t.Fatalf("outbox rows: raised=%d status_changed=%d, want 1/4", raised, statusChanged)
	}
	var audited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND resource_type = 'leadership_task' AND resource_id = $2`, ltTenant, task.TaskID).Scan(&audited); err != nil {
		t.Fatalf("audit count: %v", err)
	}
	if audited < 7 {
		t.Fatalf("audit rows = %d, want raise + seen + 4 status + edit", audited)
	}
	_ = fmt.Sprintf
}

// TestLeadershipTaskNumberingUnderContention proves two directors raising at once cannot
// share a number: the advisory lock serializes the mint, and the unique constraint would
// fail the loser if it did not.
func TestLeadershipTaskNumberingUnderContention(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := raiseParams(ltCXO, fmt.Sprintf("Concurrent ask %d", i), fmt.Sprintf("conc-%d", i))
			if i%2 == 1 {
				p.ActorID = ltDirector2
			}
			_, errs[i] = repo.Raise(ctx, p)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("raise %d: %v", i, err)
		}
	}
	var distinct, total int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT task_no), count(*) FROM leadership_tasks WHERE tenant_id = $1`, ltTenant).Scan(&distinct, &total); err != nil {
		t.Fatal(err)
	}
	if total != n || distinct != n {
		t.Fatalf("numbers: %d distinct of %d rows", distinct, total)
	}
	var maxNo int64
	_ = pool.QueryRow(ctx, `SELECT max(task_no) FROM leadership_tasks WHERE tenant_id = $1`, ltTenant).Scan(&maxNo)
	if maxNo != n {
		t.Fatalf("max task_no = %d, want %d (dense, no gaps)", maxNo, n)
	}
}

// TestLeadershipTaskListOneToManyPaginationPageBoundaryAndEveryStatusBuckets pins the list's grain
// and its keyset: rows are the caller's PARTY rows only, attachments never fan a task into
// several rows (OneToMany), the chip counts range over the whole party list across every
// status bucket (StatusMatrix) and never over the page, and a page boundary hands the exact
// next row through the cursor with no duplicate and no gap.
//
// It also pins the two shapes the ONE-ROUND-TRIP page read introduced, because both would be
// invisible to a test that only counted rows. The three chip aggregates now ride ONE statement
// (sqlListAggregatesTemplate) as three UNION ALL arms, so the arms must not be able to pick up
// each other's rows: the same request asked at two page sizes, and at page 1 versus page 2,
// must return the IDENTICAL chips, because every arm is whole-list and none of them knows the
// page. And the attachments, notes and note mentions now ride ONE pgx batch, so a task holding
// all three at once must still be ONE row with each fact hung in the right place -- in
// particular the mention must land on its NOTE, which only holds if the batch's note result is
// drained before the mention result is scanned.
func TestLeadershipTaskListOneToManyPaginationPageBoundaryAndEveryStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	cxo := domain.Actor{UserID: ltCXO, CanAct: true}

	// 5 tasks for Ravi (one with two attachments), 2 for Manohar. Statuses spread across the
	// matrix: open x2, in_progress x1, done x1, cancelled x1 for Ravi.
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		p := raiseParams(ltCXO, fmt.Sprintf("Ravi ask %d", i), fmt.Sprintf("ravi-%d", i))
		if i == 0 {
			p = raiseParams(ltCXO, "Ravi ask 0", "ravi-0", ltProof1, ltProof2)
		}
		task, err := repo.Raise(ctx, p)
		if err != nil {
			t.Fatalf("raise %d: %v", i, err)
		}
		ids = append(ids, task.TaskID)
		time.Sleep(2 * time.Millisecond) // distinct raised_at for a deterministic keyset
	}
	for i := 0; i < 2; i++ {
		if _, err := repo.Raise(ctx, raiseParams(ltCXO2, fmt.Sprintf("Manohar ask %d", i), fmt.Sprintf("man-%d", i))); err != nil {
			t.Fatalf("raise other: %v", err)
		}
	}
	move := func(id, status, key string, actor domain.Actor) {
		t.Helper()
		cur, err := repo.GetTask(ctx, ltTenant, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: actor, TaskID: id, Status: status, RowVersion: cur.RowVersion, IdempotencyKey: key}); err != nil {
			t.Fatalf("move %s -> %s: %v", id, status, err)
		}
	}
	move(ids[1], domain.StatusInProgress, "m1", cxo)
	move(ids[2], domain.StatusInProgress, "m2a", cxo)
	move(ids[2], domain.StatusDone, "m2b", cxo)
	move(ids[3], domain.StatusCancelled, "m3", domain.Actor{UserID: ltDirector, CanRaise: true})

	// Ravi, All (hides cancelled): 4 rows over two pages of 3, counts whole-list.
	page1, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1.Rows) != 3 || page1.NextCursor == "" {
		t.Fatalf("page 1 rows=%d cursor=%q", len(page1.Rows), page1.NextCursor)
	}
	want := map[string]int{domain.StatusOpen: 2, domain.StatusInProgress: 1, domain.StatusDone: 1, domain.StatusCancelled: 1}
	for status, n := range want {
		if page1.StatusCounts[status] != n {
			t.Fatalf("status counts = %v, want %v (whole party list, every bucket)", page1.StatusCounts, want)
		}
	}
	if page1.UnseenCount != 4 {
		t.Fatalf("unseen = %d, want 4 (cancelled excluded)", page1.UnseenCount)
	}
	// Seven raised, one cancelled: Team progress hides cancelled, so its chip and its list
	// both say 6 -- a tab badge must never advertise a row the tab does not show.
	if page1.ScopeCounts[domain.ScopeAssignedToMe] != 4 || page1.ScopeCounts[domain.ScopeAssignedByMe] != 0 || page1.ScopeCounts[domain.ScopeTeamProgress] != 6 {
		t.Fatalf("scope counts = %v, want assigned_to_me=4 assigned_by_me=0 team_progress=6", page1.ScopeCounts)
	}
	// With no request filter the tab SIZES equal the tab counts, arm for arm.
	if !reflect.DeepEqual(page1.ScopeTotals, page1.ScopeCounts) {
		t.Fatalf("scope totals = %v, want the same as the unfiltered scope counts %v", page1.ScopeTotals, page1.ScopeCounts)
	}
	page2, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2.Rows) != 1 || page2.NextCursor != "" {
		t.Fatalf("page 2 rows=%d cursor=%q", len(page2.Rows), page2.NextCursor)
	}
	seenIDs := map[string]bool{}
	for _, r := range append(page1.Rows, page2.Rows...) {
		if seenIDs[r.TaskID] {
			t.Fatalf("task %s served twice across the page boundary", r.TaskID)
		}
		seenIDs[r.TaskID] = true
		if r.AssigneeUserID != ltCXO {
			t.Fatalf("another person's task leaked into Ravi's list: %+v", r)
		}
		if r.Status == domain.StatusCancelled {
			t.Fatalf("All must hide cancelled: %+v", r)
		}
	}
	if len(seenIDs) != 4 {
		t.Fatalf("served %d distinct tasks, want 4", len(seenIDs))
	}
	// The two-attachment task is ONE row with two attachments, never two rows.
	var withAtt *domain.Task
	for _, r := range append(page1.Rows, page2.Rows...) {
		if r.TaskID == ids[0] {
			row := r
			withAtt = &row
		}
	}
	if withAtt == nil || len(withAtt.Attachments) != 2 || withAtt.AttachmentCount != 2 {
		t.Fatalf("attachment fan-out: %+v", withAtt)
	}
	// The Done chip lists exactly the done one; the director's list is HIS raised set.
	donePage, _ := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: domain.StatusesForFilter(domain.FilterDone), Limit: 20})
	if len(donePage.Rows) != 1 || donePage.Rows[0].TaskID != ids[2] {
		t.Fatalf("done page = %+v", donePage.Rows)
	}
	directorPage, _ := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltDirector, Scope: domain.ScopeAssignedByMe, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20})
	if len(directorPage.Rows) != 6 || directorPage.UnseenCount != 0 {
		t.Fatalf("director sees %d rows (want 6 uncancelled raised) unseen=%d", len(directorPage.Rows), directorPage.UnseenCount)
	}
	teamPage, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltDirector, Scope: domain.ScopeTeamProgress, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20})
	if err != nil || len(teamPage.Rows) != 6 {
		t.Fatalf("team progress sees %d rows (err %v), want 6 uncancelled tenant tasks", len(teamPage.Rows), err)
	}
	// THE CHIPS ARE WHOLE-LIST, NOT PAGE-LOCAL. Three UNION ALL arms in one statement is where
	// an arm could silently inherit the row query's LIMIT or another arm's predicate, so the
	// proof is that the page size and the page position change NOTHING about the chips.
	wide, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20})
	if err != nil {
		t.Fatalf("wide page: %v", err)
	}
	for _, other := range []struct {
		name string
		page ports.Page
	}{{"page 2 of 3", page2}, {"one page of 20", wide}} {
		if !reflect.DeepEqual(other.page.StatusCounts, page1.StatusCounts) {
			t.Fatalf("%s status counts = %v, want %v (aggregate is whole-list, never page-local)", other.name, other.page.StatusCounts, page1.StatusCounts)
		}
		if !reflect.DeepEqual(other.page.ScopeCounts, page1.ScopeCounts) {
			t.Fatalf("%s scope counts = %v, want %v (aggregate is whole-list, never page-local)", other.name, other.page.ScopeCounts, page1.ScopeCounts)
		}
		if other.page.UnseenCount != page1.UnseenCount {
			t.Fatalf("%s unseen = %d, want %d (aggregate is whole-list, never page-local)", other.name, other.page.UnseenCount, page1.UnseenCount)
		}
	}

	// ATTACHMENTS + NOTES + A MENTION ON ONE TASK, all three fetched in one batch. ids[0]
	// already carries two attachments; give it a note that mentions the other CXO.
	noted, err := repo.GetTask(ctx, ltTenant, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: cxo, TaskID: noted.TaskID,
		Comment: "Sending this to @Manohar for the vendor call.", MentionUserIDs: []string{ltCXO2},
		IdempotencyKey: "batch-note-1",
	}); err != nil {
		t.Fatalf("note with mention: %v", err)
	}
	batched, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20})
	if err != nil {
		t.Fatalf("batched page: %v", err)
	}
	hits := 0
	for _, r := range batched.Rows {
		if r.TaskID != ids[0] {
			continue
		}
		hits++
		if len(r.Attachments) != 2 || r.AttachmentCount != 2 {
			t.Fatalf("batched attachments = %d (count %d), want 2 -- the batch must not drop or fan the attachment arm", len(r.Attachments), r.AttachmentCount)
		}
		if len(r.Notes) != 1 {
			t.Fatalf("batched notes = %d, want 1", len(r.Notes))
		}
		// The mention rides the NOTE, not the task: this is the assertion that fails if the
		// batch's mention result is scanned before its note result has landed on the row.
		if len(r.Notes[0].Mentions) != 1 || r.Notes[0].Mentions[0].UserID != ltCXO2 {
			t.Fatalf("mention did not land on its note: %+v", r.Notes[0])
		}
		if r.Notes[0].Mentions[0].NoteID != r.Notes[0].NoteID {
			t.Fatalf("mention carries note %q but hangs on note %q", r.Notes[0].Mentions[0].NoteID, r.Notes[0].NoteID)
		}
	}
	if hits != 1 {
		t.Fatalf("task carrying two attachments, a note and a mention appeared %d times, want exactly 1 row", hits)
	}

	// The picker lists TITLES (maintainer request 2026-09-11): Ravi's HRMS title when one is
	// set, Manohar's designation label when none is. Ordered by title, so the CTO sorts after
	// the catalog label. Both title joins are 1:1 on the person's PK: two assignees stay two rows.
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_titles (tenant_id, workforce_member_id, title)
SELECT tenant_id, workforce_member_id, 'CTO' FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ltCXO); err != nil {
		t.Fatalf("seed title: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO person_access (tenant_id, workforce_member_id, designation_code)
SELECT tenant_id, workforce_member_id, 'ceo_internal' FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid
ON CONFLICT DO NOTHING`, ltTenant, ltCXO2); err != nil {
		t.Fatalf("seed designation: %v", err)
	}
	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil || len(assignees) != 2 || assignees[0].Name != "Manohar" || assignees[1].Name != "Ravi" {
		t.Fatalf("assignees = %+v err %v (Dinakar is ticked but holds no leadership grant)", assignees, err)
	}
	if assignees[0].Title != "CEO / CXO" || assignees[1].Title != "CTO" {
		t.Fatalf("assignee titles = %q, %q; want designation label for Manohar and the HRMS title for Ravi", assignees[0].Title, assignees[1].Title)
	}
	// Aggregate projection proof names for this changed predicate: OneToMany grants do not
	// duplicate assignees, Pagination remains covered by the >100 assignee page below, and
	// EveryStatusBuckets stays covered by the task status matrix above.
	// A tick with no leadership grant is refused at raise time too, by the same predicate.
	if _, err := repo.Raise(ctx, raiseParams(ltDirector2, "Ticked, no grant", "raise-no-grant")); !errors.Is(err, domain.ErrAssigneeNotAssignable) {
		t.Fatalf("raise for ticked-but-ungranted assignee: err = %v, want ErrAssigneeNotAssignable", err)
	}
	// Granting Dinakar a park-head role puts him on the list at once.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, ltDirector2); err != nil {
		t.Fatalf("seed park head grant: %v", err)
	}
	assignees, err = repo.ListAssignees(ctx, ltTenant)
	if err != nil || len(assignees) != 3 || assignees[0].Name != "Dinakar" {
		t.Fatalf("assignees after park head grant = %+v err %v", assignees, err)
	}

	// THE WORKLIST FILTERS ON TOP OF THE SAME PAGE. The party scope binds $2, the status filter
	// binds $3, and every request filter must number itself AFTER those -- this is the shape
	// that broke before (repository.go's per-scope bind rule). The chip counts are SEPARATE
	// queries, so they are asserted here too: under a filter they must count the FILTERED
	// whole list, never the unfiltered one and never the page.
	filtered, err := repo.ListTasks(ctx, ports.ListParams{
		TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe,
		Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3,
		Query: "Ravi ask", RaisedBy: ltDirector, Sort: ports.SortRaisedAtAsc,
	})
	if err != nil {
		t.Fatalf("filtered page: %v", err)
	}
	if len(filtered.Rows) != 3 || filtered.NextCursor == "" {
		t.Fatalf("filtered page rows=%d cursor=%q, want 3 of the 4 uncancelled Ravi asks", len(filtered.Rows), filtered.NextCursor)
	}
	// Four uncancelled Ravi asks match, plus the cancelled one the status filter hides from the
	// rows but not from the chips: the chips report each bucket of the FILTERED list.
	wantFiltered := map[string]int{domain.StatusOpen: 2, domain.StatusInProgress: 1, domain.StatusDone: 1, domain.StatusCancelled: 1}
	for status, n := range wantFiltered {
		if filtered.StatusCounts[status] != n {
			t.Fatalf("filtered status counts = %v, want %v", filtered.StatusCounts, wantFiltered)
		}
	}
	// Manohar's two asks are outside the text filter, so the tenant-wide tab drops from 6 to 4.
	if filtered.ScopeCounts[domain.ScopeTeamProgress] != 4 || filtered.ScopeCounts[domain.ScopeAssignedToMe] != 4 {
		t.Fatalf("filtered scope counts = %v, want team_progress=4 assigned_to_me=4 (the filters narrow the tabs too)", filtered.ScopeCounts)
	}
	// ...while the tab SIZES ignore the text filter: they are what the tab label shows and what
	// "Clear the filters to see all N" names (Gate-1 #2), so they must not move with the search.
	if filtered.ScopeTotals[domain.ScopeTeamProgress] != 6 || filtered.ScopeTotals[domain.ScopeAssignedToMe] != 4 {
		t.Fatalf("filtered scope totals = %v, want team_progress=6 assigned_to_me=4 (the filters never narrow the tab sizes)", filtered.ScopeTotals)
	}
	// One more page under the SAME filters and sort: the filter set has to survive the cursor.
	filtered2, err := repo.ListTasks(ctx, ports.ListParams{
		TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe,
		Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3,
		Query: "Ravi ask", RaisedBy: ltDirector, Sort: ports.SortRaisedAtAsc, Cursor: filtered.NextCursor,
	})
	if err != nil || len(filtered2.Rows) != 1 || filtered2.NextCursor != "" {
		t.Fatalf("filtered page 2 rows=%d cursor=%q err=%v", len(filtered2.Rows), filtered2.NextCursor, err)
	}
	if filtered2.Rows[0].TaskID == filtered.Rows[2].TaskID {
		t.Fatalf("filtered paging repeated a row across the boundary")
	}
	// Manohar's own list under the same text filter is empty, and its chips say so.
	other, err := repo.ListTasks(ctx, ports.ListParams{
		TenantID: ltTenant, UserID: ltCXO2, Scope: domain.ScopeAssignedToMe,
		Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20, Query: "Ravi ask",
	})
	if err != nil || len(other.Rows) != 0 || other.StatusCounts[domain.StatusOpen] != 0 {
		t.Fatalf("another person's filtered list = %d rows, counts %v, err %v", len(other.Rows), other.StatusCounts, err)
	}
	_ = cxo
}

func TestListAssigneesDoesNotDropEmployeesPastHundred(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	for i := 0; i < 101; i++ {
		userID := fmt.Sprintf("00000000-0000-4000-8000-00000001%04d", i)
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active')`, ltTenant, userID, fmt.Sprintf("EMP%03d", i), fmt.Sprintf("Worker %03d", i)); err != nil {
			t.Fatalf("seed employee %03d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT $1::uuid, workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[]
FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, userID); err != nil {
			t.Fatalf("seed employee tick %03d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, userID); err != nil {
			t.Fatalf("seed employee grant %03d: %v", i, err)
		}
	}

	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil {
		t.Fatalf("list assignees: %v", err)
	}
	// 101 ticked park heads + Ravi + Manohar; Dinakar is ticked with no leadership grant.
	if len(assignees) != 103 {
		t.Fatalf("assignees len = %d, want 103", len(assignees))
	}
	if got := assignees[len(assignees)-1].Name; got != "Worker 100" {
		t.Fatalf("last assignee = %q, want Worker 100", got)
	}
}

// A LOCAL DEVELOPMENT login (a workforce row seed-dev-grant stamped `dev_account: true`) is not a
// person anyone assigns work to or @mentions: it is left out of the assignee list, refused by the
// raise-time tick check, and left out of the mention candidates -- even when it is ticked and
// granted exactly like a real leader, and even when it is a party to the task. A real leader
// with the same tick, grant and an EMPTY metadata stays listed: the marker is the seeder's own,
// never a display-name pattern.
func TestDevAccountsAreHiddenFromPeoplePickers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	seed := func(userID, name, metadata string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, metadata)
VALUES ($1::uuid, $2::uuid, 'auth:' || $2, $3, 'active', $4::jsonb)`, ltTenant, userID, name, metadata); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT $1::uuid, workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[]
FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, userID); err != nil {
			t.Fatalf("tick %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, userID); err != nil {
			t.Fatalf("grant %s: %v", name, err)
		}
	}
	const devUser = "90000000-0000-4000-8000-000000000101"
	const realUser = "90000000-0000-4000-8000-000000000102"
	seed(devUser, "dev-ceo_internal", `{"source":"seed-dev-grant","dev_account":true}`)
	seed(realUser, "Devika", `{}`)

	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil {
		t.Fatalf("list assignees: %v", err)
	}
	var sawDev, sawReal bool
	for _, a := range assignees {
		sawDev = sawDev || a.UserID == devUser
		sawReal = sawReal || a.UserID == realUser
	}
	if sawDev || !sawReal {
		t.Fatalf("assignees: dev listed=%v real listed=%v, want false/true (%+v)", sawDev, sawReal, assignees)
	}
	// The raise-time check agrees with the list: a stale client naming the dev login is refused.
	if _, err := repo.Raise(ctx, raiseParams(devUser, "For the dev login", "dev-raise-1")); !errors.Is(err, domain.ErrAssigneeNotAssignable) {
		t.Fatalf("raise for the dev login: err = %v, want ErrAssigneeNotAssignable", err)
	}
	if _, err := repo.Raise(ctx, raiseParams(realUser, "For Devika", "dev-raise-2")); err != nil {
		t.Fatalf("raise for a real leader: %v", err)
	}
	// Even as the RAISER of a task, the dev login is not a mention candidate on it.
	task, err := repo.Raise(ctx, ports.RaiseParams{TenantID: ltTenant, ActorID: devUser, AssigneeUserID: ltCXO, Title: "Raised by the dev login", IdempotencyKey: "dev-raise-3"})
	if err != nil {
		t.Fatalf("raise by the dev login: %v", err)
	}
	users, err := repo.ListMentionableUsers(ctx, ltTenant, task.TaskID)
	if err != nil {
		t.Fatalf("mentionable: %v", err)
	}
	sawDev, sawReal = false, false
	for _, u := range users {
		sawDev = sawDev || u.UserID == devUser
		sawReal = sawReal || u.UserID == realUser
	}
	if sawDev || !sawReal {
		t.Fatalf("mentionable: dev listed=%v real listed=%v, want false/true (%+v)", sawDev, sawReal, users)
	}
}

// The UNFILTERED team read is the one the web desk opens with (CXO, scope_mode=company): no
// status filter, no cursor. It used to bind the user id to a predicate that never read it and
// fail with "expected 1 arguments, got 2" -- the whole /tasks page fell over. The filtered
// variant hid it, because a later $3 made pgx's placeholder count line up by accident.
func TestTeamProgressUnfilteredReadBindsOnlyWhatItReads(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	if _, err := repo.Raise(ctx, raiseParams(ltCXO, "Unfiltered team read", "team-unfiltered-1")); err != nil {
		t.Fatalf("raise: %v", err)
	}
	for _, scope := range []string{domain.ScopeTeamProgress, domain.ScopeAssignedByMe, domain.ScopeAssignedToMe} {
		page, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltDirector, Scope: scope, Limit: 20})
		if err != nil {
			t.Fatalf("unfiltered %s read: %v", scope, err)
		}
		if scope != domain.ScopeAssignedToMe && len(page.Rows) != 1 {
			t.Fatalf("unfiltered %s sees %d rows, want 1", scope, len(page.Rows))
		}
	}
}

// The title joins added for the picker (workforce_member_titles, person_access,
// designation_catalog) must not change the picker's grain: one row per ticked leader.
//   - OneToMany: a person holding SEVERAL active grants (director + park head + toxin tester)
//     and a title and a designation is still ONE row, with ONE title.
//   - Pagination: the list is bounded by the tick, never paged; 100+ titled leaders all return.
//   - StatusBuckets: a ticked leader whose roster row is inactive is not listed, whatever title
//     they carry -- the member status filter still governs.
func TestListAssigneesTitleJoinsKeepOneToManyGrantsPaginationAndStatusBucketsHonest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// Ravi: three grants, a title AND a designation. One row, the HRMS title wins.
	for _, role := range []string{"park_head", "toxin_tester"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, $3, 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, ltCXO, role); err != nil {
			t.Fatalf("seed extra grant: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_titles (tenant_id, workforce_member_id, title)
SELECT tenant_id, workforce_member_id, 'CTO' FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ltCXO); err != nil {
		t.Fatalf("seed title: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO person_access (tenant_id, workforce_member_id, designation_code)
SELECT tenant_id, workforce_member_id, 'ceo_internal' FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid
ON CONFLICT DO NOTHING`, ltTenant, ltCXO); err != nil {
		t.Fatalf("seed designation: %v", err)
	}
	// Manohar: ticked, granted, titled -- but INACTIVE on the roster. Never listed.
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_titles (tenant_id, workforce_member_id, title)
SELECT tenant_id, workforce_member_id, 'COO' FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ltCXO2); err != nil {
		t.Fatalf("seed title 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE workforce_members SET status = 'inactive' WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ltCXO2); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	// 101 titled, granted, ticked directors: all of them come back, each once, each with a title.
	for i := 0; i < 101; i++ {
		userID := fmt.Sprintf("00000000-0000-4000-8000-00000002%04d", i)
		var memberID string
		if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active') RETURNING workforce_member_id::text`, ltTenant, userID, fmt.Sprintf("DIR%03d", i), fmt.Sprintf("Director %03d", i)).Scan(&memberID); err != nil {
			t.Fatalf("seed director %03d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'feed_director', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, userID); err != nil {
			t.Fatalf("seed director grant %03d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
VALUES ($1::uuid, $2::uuid, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[])`, ltTenant, memberID); err != nil {
			t.Fatalf("seed director tick %03d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_titles (tenant_id, workforce_member_id, title) VALUES ($1::uuid, $2::uuid, $3)`, ltTenant, memberID, fmt.Sprintf("Feed Director %03d", i)); err != nil {
			t.Fatalf("seed director title %03d: %v", i, err)
		}
	}

	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil {
		t.Fatalf("list assignees: %v", err)
	}
	if len(assignees) != 102 {
		t.Fatalf("assignees len = %d, want 102 (Ravi once despite three grants, 101 directors, Manohar excluded as inactive)", len(assignees))
	}
	seen := map[string]int{}
	for _, a := range assignees {
		seen[a.UserID]++
		if a.Title == "" {
			t.Fatalf("assignee %s (%s) has no title", a.Name, a.UserID)
		}
		if a.Name == "Manohar" {
			t.Fatalf("inactive Manohar must not be listed: %+v", a)
		}
	}
	if seen[ltCXO] != 1 {
		t.Fatalf("Ravi listed %d times, want 1", seen[ltCXO])
	}
	for _, a := range assignees {
		if a.UserID == ltCXO && a.Title != "CTO" {
			t.Fatalf("Ravi title = %q, want the HRMS title over the designation label", a.Title)
		}
	}
}

// The deadline round-trips through Postgres exactly: stored on raise, read back as the same
// instant, kept by an edit that does not name one (an older phone editing the brief), replaced
// by one that does, and refused -- under the row lock, against the STORED raise instant -- when
// the new one is not after the raise. A raise with no deadline (the Work Board flag) stores NULL.
func TestLeadershipTaskDeadlineRoundTripsAndEditKeepsItWhenNotSent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	ist := time.FixedZone("IST", 5*3600+1800)
	deadline := time.Date(2026, 9, 20, 17, 0, 0, 0, ist)
	params := raiseParams(ltCXO, "Approve the vendor contract", "raise-dl-1", ltProof1)
	params.DeadlineAt = &deadline
	task, err := repo.Raise(ctx, params)
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if task.DeadlineAt == nil || !task.DeadlineAt.Equal(deadline) {
		t.Fatalf("deadline after raise = %v, want %v", task.DeadlineAt, deadline)
	}
	read, err := repo.GetTask(ctx, ltTenant, task.TaskID)
	if err != nil || read.DeadlineAt == nil || !read.DeadlineAt.Equal(deadline) {
		t.Fatalf("deadline read back = %v err %v", read.DeadlineAt, err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT to_char(deadline_at AT TIME ZONE 'Asia/Kolkata', 'DD/MM/YYYY HH24:MI') FROM leadership_tasks WHERE task_id = $1`, task.TaskID).Scan(&stored); err != nil {
		t.Fatalf("read stored deadline: %v", err)
	}
	if stored != "20/09/2026 17:00" {
		t.Fatalf("stored deadline reads %q in IST, want 20/09/2026 17:00", stored)
	}

	// An edit that names no deadline keeps the stored one.
	edited, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "Approve the vendor contract (v2)", Body: "Updated.", RowVersion: task.RowVersion, IdempotencyKey: "edit-dl-1"})
	if err != nil {
		t.Fatalf("edit without deadline: %v", err)
	}
	if edited.DeadlineAt == nil || !edited.DeadlineAt.Equal(deadline) {
		t.Fatalf("edit without deadline must keep it: %v", edited.DeadlineAt)
	}
	// An edit that names one replaces it.
	moved := deadline.Add(48 * time.Hour)
	edited2, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: edited.Title, Body: edited.Body, RowVersion: edited.RowVersion, IdempotencyKey: "edit-dl-2", DeadlineAt: &moved})
	if err != nil {
		t.Fatalf("edit with deadline: %v", err)
	}
	if edited2.DeadlineAt == nil || !edited2.DeadlineAt.Equal(moved) {
		t.Fatalf("edit must replace the deadline: %v", edited2.DeadlineAt)
	}
	// A deadline not after the stored raise is refused and writes nothing.
	behind := edited2.RaisedAt.Add(-time.Minute)
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: edited.Title, Body: edited.Body, RowVersion: edited2.RowVersion, IdempotencyKey: "edit-dl-3", DeadlineAt: &behind}); !errors.Is(err, domain.ErrDeadlineNotAfterRaise) {
		t.Fatalf("deadline behind the raise: %v", err)
	}
	after, _ := repo.GetTask(ctx, ltTenant, task.TaskID)
	if after.RowVersion != edited2.RowVersion || !after.DeadlineAt.Equal(moved) {
		t.Fatalf("refused edit must write nothing: %+v", after)
	}

	// A raise with no deadline stores NULL: no counter until the raiser sets one.
	flag, err := repo.Raise(ctx, raiseParams(ltCXO2, "Check · Weigh Godel 1 - Part 3", "raise-dl-flag"))
	if err != nil || flag.DeadlineAt != nil {
		t.Fatalf("flag raise = %v err %v", flag.DeadlineAt, err)
	}
}

// raiseWithDeadline raises a task the deadline sorts can order. The stored CHECK demands a
// deadline later than the raise instant, so every offset here is in the future.
func raiseWithDeadline(assignee, title, key string, deadline *time.Time) ports.RaiseParams {
	p := raiseParams(assignee, title, key)
	p.DeadlineAt = deadline
	return p
}

// TestLeadershipTaskListFiltersSortsAndCursorStayHonest pins the worklist contract added for the
// Jira-like list:
//   - the four sorts order the page, and a task with NO deadline sorts LAST in BOTH deadline
//     directions (it is neither the most nor the least urgent);
//   - the keyset stays TOTAL across the null boundary: paging a sort two rows at a time serves
//     exactly the same sequence as one big page, with no duplicate and no gap;
//   - a cursor minted under one sort is REFUSED under another instead of silently serving a
//     wrong page, and the legacy plaintext cursor still works under the sort it was minted for;
//   - the free text, person and date filters narrow the rows AND the chip counts together, so a
//     chip never advertises a row the list hides.
func TestLeadershipTaskListFiltersSortsAndCursorStayHonest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	base := time.Now().UTC()
	at := func(d time.Duration) *time.Time { v := base.Add(d); return &v }
	// Raised oldest-first, so raise order is alpha and the deadline order is deliberately not.
	seeds := []struct {
		title    string
		deadline *time.Time
	}{
		{"Alpha vendor contract", at(4 * time.Hour)},
		{"Bravo vendor contract", at(1 * time.Hour)},
		{"Charlie audit", nil},
		{"Delta audit", at(2 * time.Hour)},
		{"Echo audit", nil},
	}
	byTitle := map[string]string{}
	for i, s := range seeds {
		task, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, s.title, fmt.Sprintf("sortseed-%d", i), s.deadline))
		if err != nil {
			t.Fatalf("raise %s: %v", s.title, err)
		}
		byTitle[s.title] = task.TaskID
		time.Sleep(2 * time.Millisecond) // distinct raised_at for a deterministic keyset
	}
	titles := func(rows []domain.Task) []string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Title)
		}
		return out
	}
	list := func(t *testing.T, p ports.ListParams) ports.Page {
		t.Helper()
		p.TenantID, p.UserID = ltTenant, ltCXO
		if p.Scope == "" {
			p.Scope = domain.ScopeAssignedToMe
		}
		if p.Statuses == nil {
			p.Statuses = domain.StatusesForFilter(domain.FilterAll)
		}
		if p.Limit == 0 {
			p.Limit = 20
		}
		page, err := repo.ListTasks(ctx, p)
		if err != nil {
			t.Fatalf("list %+v: %v", p, err)
		}
		return page
	}
	sameOrder := func(got, want []string) bool {
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

	// 1. The four orders. Both deadline directions park the two NULL-deadline tasks at the END,
	// ordered among themselves by task_id so the sequence is total.
	wantByDeadlineAsc := []string{"Bravo vendor contract", "Delta audit", "Alpha vendor contract"}
	ascPage := list(t, ports.ListParams{Sort: ports.SortDeadlineAsc})
	got := titles(ascPage.Rows)
	if len(got) != 5 || !sameOrder(got[:3], wantByDeadlineAsc) {
		t.Fatalf("deadline_asc = %v, want the three dated tasks %v first", got, wantByDeadlineAsc)
	}
	for _, r := range ascPage.Rows[3:] {
		if r.DeadlineAt != nil {
			t.Fatalf("deadline_asc put a dated task after an undated one: %v", titles(ascPage.Rows))
		}
	}
	descPage := list(t, ports.ListParams{Sort: ports.SortDeadlineDesc})
	got = titles(descPage.Rows)
	wantByDeadlineDesc := []string{"Alpha vendor contract", "Delta audit", "Bravo vendor contract"}
	if len(got) != 5 || !sameOrder(got[:3], wantByDeadlineDesc) {
		t.Fatalf("deadline_desc = %v, want %v first", got, wantByDeadlineDesc)
	}
	for _, r := range descPage.Rows[3:] {
		if r.DeadlineAt != nil {
			t.Fatalf("deadline_desc must ALSO park undated tasks last: %v", titles(descPage.Rows))
		}
	}
	raisedDesc := titles(list(t, ports.ListParams{Sort: ports.SortRaisedAtDesc}).Rows)
	raisedAsc := titles(list(t, ports.ListParams{Sort: ports.SortRaisedAtAsc}).Rows)
	if len(raisedAsc) != 5 || raisedAsc[0] != "Alpha vendor contract" || raisedDesc[0] != "Echo audit" {
		t.Fatalf("raise orders = asc %v / desc %v", raisedAsc, raisedDesc)
	}

	// 2. The keyset is total in every sort: two rows at a time must reproduce the one-page order
	// exactly, INCLUDING across the dated/undated boundary the deadline sorts introduce.
	for _, sortKey := range ports.SortKeys {
		whole := titles(list(t, ports.ListParams{Sort: sortKey}).Rows)
		paged := make([]string, 0, 5)
		seen := map[string]bool{}
		cursor := ""
		for page := 0; page < 5; page++ {
			p := list(t, ports.ListParams{Sort: sortKey, Limit: 2, Cursor: cursor})
			for _, r := range p.Rows {
				if seen[r.TaskID] {
					t.Fatalf("%s served %s twice across a page boundary", sortKey, r.Title)
				}
				seen[r.TaskID] = true
				paged = append(paged, r.Title)
			}
			cursor = p.NextCursor
			if cursor == "" {
				break
			}
		}
		if !sameOrder(paged, whole) {
			t.Fatalf("%s paged 2-at-a-time = %v, want the whole-page order %v (no gap, no duplicate)", sortKey, paged, whole)
		}
	}

	// 3. A cursor minted under one sort is REFUSED under another: the same instant means a
	// different position in a different order, so serving it would hand back a wrong page.
	crossPage := list(t, ports.ListParams{Sort: ports.SortDeadlineAsc, Limit: 2})
	if crossPage.NextCursor == "" {
		t.Fatal("expected a next cursor to carry across sorts")
	}
	if _, err := repo.ListTasks(ctx, ports.ListParams{
		TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe,
		Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 2,
		Sort: ports.SortRaisedAtDesc, Cursor: crossPage.NextCursor,
	}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("cursor reused under a changed sort: err = %v, want ErrInvalidArgument", err)
	}
	// An unknown sort is refused outright rather than silently falling back to the default.
	if _, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Sort: "title_asc"}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("unknown sort: err = %v, want ErrInvalidArgument", err)
	}
	// The legacy plaintext cursor an in-flight Android build already holds still pages, but only
	// under the default sort it was minted for.
	firstDesc := list(t, ports.ListParams{Sort: ports.SortRaisedAtDesc, Limit: 2})
	legacy := firstDesc.Rows[1].RaisedAt.UTC().Format(time.RFC3339Nano) + "|" + firstDesc.Rows[1].TaskID
	legacyPage := list(t, ports.ListParams{Limit: 2, Cursor: legacy})
	if len(legacyPage.Rows) != 2 || legacyPage.Rows[0].Title != raisedDesc[2] {
		t.Fatalf("legacy cursor page = %v, want it to resume at %q", titles(legacyPage.Rows), raisedDesc[2])
	}
	if _, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Sort: ports.SortDeadlineAsc, Cursor: legacy}); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("legacy cursor under a deadline sort: err = %v, want ErrInvalidArgument", err)
	}

	// 4. Free text narrows the rows AND the chips together. "audit" is three of the five.
	textPage := list(t, ports.ListParams{Query: "AUDIT"})
	if len(textPage.Rows) != 3 {
		t.Fatalf("q=AUDIT rows = %v, want the three audits (case-insensitive substring)", titles(textPage.Rows))
	}
	if textPage.StatusCounts[domain.StatusOpen] != 3 {
		t.Fatalf("q=AUDIT status counts = %v, want open=3: a chip must never advertise a row the list hides", textPage.StatusCounts)
	}
	if textPage.ScopeCounts[domain.ScopeAssignedToMe] != 3 || textPage.ScopeCounts[domain.ScopeTeamProgress] != 3 {
		t.Fatalf("q=AUDIT scope counts = %v, want 3 on the caller's tab and 3 tenant-wide", textPage.ScopeCounts)
	}
	// The brief is searched too; every seeded task shares the same body.
	if bodyPage := list(t, ports.ListParams{Query: "look at this"}); len(bodyPage.Rows) != 5 {
		t.Fatalf("q over the brief matched %d rows, want 5", len(bodyPage.Rows))
	}
	// A bare integer ALSO matches the task's own number exactly: "3" finds #3 as well as any
	// title carrying a 3.
	third, err := repo.GetTask(ctx, ltTenant, byTitle["Charlie audit"])
	if err != nil {
		t.Fatal(err)
	}
	no := third.TaskNo
	numberPage := list(t, ports.ListParams{Query: fmt.Sprintf("%d", no), QueryTaskNo: &no})
	foundByNumber := false
	for _, r := range numberPage.Rows {
		if r.TaskID == third.TaskID {
			foundByNumber = true
		}
	}
	if !foundByNumber {
		t.Fatalf("q=%d did not find task #%d by its number: %v", no, no, titles(numberPage.Rows))
	}
	if miss := list(t, ports.ListParams{Query: "no such words here"}); len(miss.Rows) != 0 || miss.StatusCounts[domain.StatusOpen] != 0 || miss.ScopeCounts[domain.ScopeTeamProgress] != 0 || miss.ScopeTotals[domain.ScopeTeamProgress] == 0 {
		t.Fatalf("a text miss must empty the rows AND the chips, and leave the tab sizes alone: rows=%d status=%v scope=%v totals=%v", len(miss.Rows), miss.StatusCounts, miss.ScopeCounts, miss.ScopeTotals)
	}

	// 5. The person filters. On the caller's own tab the assignee is already pinned, so an
	// assignee filter is dropped upstream; the raiser filter still narrows, and on the
	// tenant-wide tab both do.
	if byRaiser := list(t, ports.ListParams{RaisedBy: ltDirector}); len(byRaiser.Rows) != 5 {
		t.Fatalf("raised_by the seeding director = %d rows, want 5", len(byRaiser.Rows))
	}
	if byOther := list(t, ports.ListParams{RaisedBy: ltDirector2}); len(byOther.Rows) != 0 || byOther.StatusCounts[domain.StatusOpen] != 0 {
		t.Fatalf("raised_by someone who raised nothing: rows=%d counts=%v", len(byOther.Rows), byOther.StatusCounts)
	}
	teamByAssignee := list(t, ports.ListParams{Scope: domain.ScopeTeamProgress, AssigneeUserID: ltCXO2})
	if len(teamByAssignee.Rows) != 0 || teamByAssignee.ScopeCounts[domain.ScopeTeamProgress] != 0 {
		t.Fatalf("team progress filtered to an assignee with no tasks: rows=%d scope=%v", len(teamByAssignee.Rows), teamByAssignee.ScopeCounts)
	}
	// The tenant-wide tab still binds only what it reads: the team_progress branch binds ONE
	// arg for the scope, and the filters must number themselves after it (the "expected 1
	// arguments, got 2" trap at the top of ListTasks).
	teamFiltered := list(t, ports.ListParams{Scope: domain.ScopeTeamProgress, Query: "audit", AssigneeUserID: ltCXO, RaisedBy: ltDirector, Sort: ports.SortDeadlineDesc})
	if len(teamFiltered.Rows) != 3 {
		t.Fatalf("team progress with every filter at once = %v, want the three audits", titles(teamFiltered.Rows))
	}

	// 6. The date ranges are INCLUSIVE on both ends, and a deadline range excludes the tasks
	// that have no deadline at all.
	from, to := base.Add(30*time.Minute), base.Add(2*time.Hour)
	dl := list(t, ports.ListParams{DeadlineFrom: &from, DeadlineTo: &to})
	if got := titles(dl.Rows); len(got) != 2 {
		t.Fatalf("deadline range = %v, want Bravo (+1h) and Delta (+2h, the inclusive upper end)", got)
	}
	if dl.StatusCounts[domain.StatusOpen] != 2 {
		t.Fatalf("deadline range chips = %v, want open=2", dl.StatusCounts)
	}
	oldest, err := repo.GetTask(ctx, ltTenant, byTitle["Alpha vendor contract"])
	if err != nil {
		t.Fatal(err)
	}
	rFrom, rTo := oldest.RaisedAt, oldest.RaisedAt
	raised := list(t, ports.ListParams{RaisedFrom: &rFrom, RaisedTo: &rTo})
	if len(raised.Rows) != 1 || raised.Rows[0].TaskID != oldest.TaskID {
		t.Fatalf("a raise range whose ends are the SAME instant must include that row: %v", titles(raised.Rows))
	}

	// 7. THE TYPED TEXT IS LITERAL. `%` and `_` are ILIKE wildcards, so a leader searching for
	// "50%" or "shed_4" must match those characters themselves -- an unescaped `_` would match
	// any single character and quietly return rows whose text the reader never typed.
	if _, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Cut wastage by 50% this month", "literal-pct", at(6*time.Hour))); err != nil {
		t.Fatalf("raise literal percent: %v", err)
	}
	if _, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Repair shed_4 water line", "literal-underscore", at(7*time.Hour))); err != nil {
		t.Fatalf("raise literal underscore: %v", err)
	}
	pct := list(t, ports.ListParams{Query: "50%"})
	if len(pct.Rows) != 1 || pct.Rows[0].Title != "Cut wastage by 50% this month" {
		t.Fatalf(`q="50%%" = %v, want only the task carrying a literal per-cent sign`, titles(pct.Rows))
	}
	// Bare "50" still matches it as ordinary text, so the escape narrowed nothing it should not.
	if plain := list(t, ports.ListParams{Query: "50"}); len(plain.Rows) != 1 {
		t.Fatalf(`q="50" = %v, want the same row by plain substring`, titles(plain.Rows))
	}
	under := list(t, ports.ListParams{Query: "shed_4"})
	if len(under.Rows) != 1 || under.Rows[0].Title != "Repair shed_4 water line" {
		t.Fatalf(`q="shed_4" = %v, want only the task carrying a literal underscore`, titles(under.Rows))
	}
	// The wildcard reading of the same text would have matched "shed 4"; the literal one must not.
	if _, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Repair shed 4 gate", "literal-space", at(8*time.Hour))); err != nil {
		t.Fatalf("raise wildcard decoy: %v", err)
	}
	under = list(t, ports.ListParams{Query: "shed_4"})
	if len(under.Rows) != 1 || under.Rows[0].Title != "Repair shed_4 water line" {
		t.Fatalf(`q="shed_4" matched a wildcard decoy: %v`, titles(under.Rows))
	}
	if space := list(t, ports.ListParams{Query: "shed 4"}); len(space.Rows) != 1 || space.Rows[0].Title != "Repair shed 4 gate" {
		t.Fatalf(`q="shed 4" = %v, want only the space-separated task`, titles(space.Rows))
	}
	// A bare escape character is escaped too, and finds nothing rather than erroring.
	if esc := list(t, ports.ListParams{Query: `\`}); len(esc.Rows) != 0 {
		t.Fatalf(`q="\\" = %v, want no rows and no error`, titles(esc.Rows))
	}

	// 6. The OVERDUE lens: only still-working tasks whose deadline is strictly before the clock,
	// and the chip's count is that same set, whole-list, under the same filters -- never a sum
	// of status buckets. A done task past its deadline is finished, not late; a task due
	// exactly at the clock is not late yet.
	lateOpen, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Late open audit", "overdue-open", at(-2*time.Hour)))
	if err != nil {
		t.Fatalf("raise late open: %v", err)
	}
	lateDone, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Late but done audit", "overdue-done", at(-3*time.Hour)))
	if err != nil {
		t.Fatalf("raise late done: %v", err)
	}
	cxoActor := domain.Actor{UserID: ltCXO, CanAct: true}
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxoActor, TaskID: lateDone.TaskID, Status: domain.StatusDone, RowVersion: lateDone.RowVersion, IdempotencyKey: "overdue-done-st"}); err != nil {
		t.Fatalf("finish late task: %v", err)
	}
	if _, err := repo.Raise(ctx, raiseWithDeadline(ltCXO, "Due exactly now audit", "overdue-edge", &base)); err != nil {
		t.Fatalf("raise edge: %v", err)
	}
	lens := list(t, ports.ListParams{Statuses: domain.StatusesForFilter(domain.FilterOverdue), OverdueBefore: &base, OverdueAt: base})
	if len(lens.Rows) != 1 || lens.Rows[0].TaskID != lateOpen.TaskID {
		t.Fatalf("overdue lens = %v, want only the late open task", titles(lens.Rows))
	}
	if lens.OverdueCount != 1 {
		t.Fatalf("overdue count under the lens = %d, want 1", lens.OverdueCount)
	}
	// The count is advertised on EVERY read against the same clock, lens or not, and it follows
	// the request filters like the status counts do (q narrows it to zero here).
	if all := list(t, ports.ListParams{OverdueAt: base}); all.OverdueCount != 1 {
		t.Fatalf("overdue count on the All chip = %d, want 1", all.OverdueCount)
	}
	if none := list(t, ports.ListParams{OverdueAt: base, Query: "vendor"}); none.OverdueCount != 0 {
		t.Fatalf("overdue count under q=vendor = %d, want 0", none.OverdueCount)
	}
	// Later clock: the edge task is now late too, and the lens agrees with the count.
	later := base.Add(time.Minute)
	lens = list(t, ports.ListParams{Statuses: domain.StatusesForFilter(domain.FilterOverdue), OverdueBefore: &later, OverdueAt: later})
	if len(lens.Rows) != 2 || lens.OverdueCount != 2 {
		t.Fatalf("overdue lens a minute later = %v (count %d), want the two late working tasks", titles(lens.Rows), lens.OverdueCount)
	}
}

// raiseAs raises one task for an arbitrary raiser, assignee and deadline. raiseParams pins the
// seeding director as the raiser, which is right for most of this file but not for the scope
// tabs: assigned_by_me only means something when the ACTOR has raised tasks of their own.
func raiseAs(raiser, assignee, title, key string, deadline *time.Time) ports.RaiseParams {
	p := raiseParams(assignee, title, key)
	p.ActorID = raiser
	p.DeadlineAt = deadline
	return p
}

// sumCounts totals a chip map. Every assertion below that compares a chip total against the
// rows the list served depends on the buckets being DISJOINT, which is the property
// TestLeadershipTaskAggregateStatusMatrixOneToManyFanOutAndPageBoundaryStayHonest proves.
func sumCounts(counts map[string]int) int {
	total := 0
	for _, n := range counts {
		total += n
	}
	return total
}

// TestLeadershipTaskAggregateDateShiftDeadlineRangeCountsWhatItLists is the DATE proof for the
// unioned list aggregate (repository.go sqlListAggregates).
//
// The two date columns of a task mean different things and fall on different DAYS: raised_at is
// when the leader asked, deadline_at is when they want it back. Every dated task below is
// raised today and due two, five, nine or twelve days later, so a count arm that silently read
// raised_at where the rows read deadline_at -- or that dropped the date predicate altogether,
// which is the cheap way a chip starts advertising rows the list hides -- cannot pass by
// coincidence the way it could on a single-date fixture.
//
// What is proved, in order: the fixture really is date-shifted; a deadline range narrows the
// rows AND every chip arm by the same predicate; a task with NO deadline leaves the rows and
// the chips together; a raise range and a deadline range are independent predicates on every
// arm; and none of it moves when the page size does.
func TestLeadershipTaskAggregateDateShiftDeadlineRangeCountsWhatItLists(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	base := time.Now().UTC()
	day := func(n int) *time.Time { v := base.Add(time.Duration(n) * 24 * time.Hour); return &v }
	seeds := []struct {
		title    string
		deadline *time.Time
		status   string
	}{
		{"DateShift alpha", day(2), domain.StatusOpen},
		{"DateShift bravo", day(5), domain.StatusDone},
		{"DateShift charlie", day(9), domain.StatusInProgress},
		{"DateShift delta", nil, domain.StatusOpen},
		{"DateShift echo", day(12), domain.StatusCancelled},
	}
	byTitle := map[string]domain.Task{}
	for i, s := range seeds {
		task, err := repo.Raise(ctx, raiseAs(ltDirector, ltCXO, s.title, fmt.Sprintf("dateshift-%d", i), s.deadline))
		if err != nil {
			t.Fatalf("raise %s: %v", s.title, err)
		}
		if s.status != domain.StatusOpen {
			actor := domain.Actor{UserID: ltCXO, CanAct: true}
			if s.status == domain.StatusCancelled {
				actor = domain.Actor{UserID: ltDirector, CanRaise: true}
			}
			if s.status == domain.StatusDone {
				// The ladder goes through in_progress; done is not reachable in one step from open
				// for the assignee, so walk it.
				if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: actor, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("dateshift-mid-%d", i)}); err != nil {
					t.Fatalf("%s -> in_progress: %v", s.title, err)
				}
				cur, err := repo.GetTask(ctx, ltTenant, task.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				task = cur
			}
			moved, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: actor, TaskID: task.TaskID, Status: s.status, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("dateshift-move-%d", i)})
			if err != nil {
				t.Fatalf("%s -> %s: %v", s.title, s.status, err)
			}
			task = moved
		}
		byTitle[s.title] = task
		time.Sleep(2 * time.Millisecond) // distinct raised_at for a deterministic keyset
	}

	// 0. THE FIXTURE IS ACTUALLY DATE-SHIFTED. Asserted, not assumed: the reference calls a
	// single-date fixture a false green, and every proof below is worthless if raised_at and
	// deadline_at happen to land on the same farm day.
	ist := time.FixedZone("IST", 5*3600+1800)
	for _, title := range []string{"DateShift alpha", "DateShift bravo", "DateShift charlie", "DateShift echo"} {
		task := byTitle[title]
		if task.DeadlineAt == nil {
			t.Fatalf("%s lost its deadline", title)
		}
		raisedDay := task.RaisedAt.In(ist).Format("2006-01-02")
		dueDay := task.DeadlineAt.In(ist).Format("2006-01-02")
		if raisedDay == dueDay {
			t.Fatalf("%s was raised and is due on the SAME farm day (%s); the date proof needs a shifted fixture", title, raisedDay)
		}
	}

	list := func(t *testing.T, p ports.ListParams) ports.Page {
		t.Helper()
		p.TenantID, p.UserID = ltTenant, ltCXO
		if p.Scope == "" {
			p.Scope = domain.ScopeAssignedToMe
		}
		if p.Statuses == nil {
			p.Statuses = domain.StatusesForFilter(domain.FilterAll)
		}
		if p.Limit == 0 {
			p.Limit = 20
		}
		page, err := repo.ListTasks(ctx, p)
		if err != nil {
			t.Fatalf("list %+v: %v", p, err)
		}
		return page
	}

	// 1. NO DATE FILTER. Five tasks, one of them cancelled. The rows hide the cancelled one; the
	// status chips deliberately do not (repository.go statusCountsWhere documents that), so the
	// chip total is 5 against 4 rows and the scope tabs -- which DO carry status <> 'cancelled'
	// on every arm -- say 4.
	wide := list(t, ports.ListParams{})
	if len(wide.Rows) != 4 {
		t.Fatalf("unfiltered rows = %d, want the 4 uncancelled tasks", len(wide.Rows))
	}
	if got := sumCounts(wide.StatusCounts); got != 5 {
		t.Fatalf("unfiltered status chips total %d over %v, want 5 (the chips drop the status predicate, so cancelled is in)", got, wide.StatusCounts)
	}
	if wide.ScopeCounts[domain.ScopeAssignedToMe] != 4 || wide.ScopeCounts[domain.ScopeTeamProgress] != 4 || wide.ScopeCounts[domain.ScopeAssignedByMe] != 0 {
		t.Fatalf("unfiltered scope chips = %v, want assigned_to_me=4 team_progress=4 assigned_by_me=0", wide.ScopeCounts)
	}

	// 2. A DEADLINE RANGE NARROWS THE ROWS AND EVERY CHIP ARM BY THE SAME PREDICATE. Days 1..6
	// catch alpha (day 2, open) and bravo (day 5, done) and nothing else.
	from, to := base.Add(24*time.Hour), base.Add(6*24*time.Hour)
	ranged := list(t, ports.ListParams{DeadlineFrom: &from, DeadlineTo: &to})
	if len(ranged.Rows) != 2 {
		t.Fatalf("deadline range rows = %d, want alpha and bravo", len(ranged.Rows))
	}
	wantRanged := map[string]int{domain.StatusOpen: 1, domain.StatusDone: 1}
	if !reflect.DeepEqual(ranged.StatusCounts, wantRanged) {
		t.Fatalf("deadline range status chips = %v, want exactly %v -- a count arm that dropped the deadline predicate would report the whole list here", ranged.StatusCounts, wantRanged)
	}
	if sumCounts(ranged.StatusCounts) != len(ranged.Rows) {
		t.Fatalf("deadline range chips total %d but the list served %d rows; the chips must count over the SAME date predicate the rows do", sumCounts(ranged.StatusCounts), len(ranged.Rows))
	}
	if ranged.ScopeCounts[domain.ScopeAssignedToMe] != 2 || ranged.ScopeCounts[domain.ScopeTeamProgress] != 2 || ranged.ScopeCounts[domain.ScopeAssignedByMe] != 0 {
		t.Fatalf("deadline range scope chips = %v, want assigned_to_me=2 team_progress=2 assigned_by_me=0", ranged.ScopeCounts)
	}

	// 3. A TASK WITH NO DEADLINE LEAVES THE ROWS AND THE CHIPS TOGETHER. Days 0..30 catch every
	// DATED task -- including the cancelled one, which the chips count and the rows do not -- and
	// must drop delta, which has no deadline at all, from both sides at once.
	allFrom, allTo := base, base.Add(30*24*time.Hour)
	dated := list(t, ports.ListParams{DeadlineFrom: &allFrom, DeadlineTo: &allTo})
	if len(dated.Rows) != 3 {
		t.Fatalf("wide deadline range rows = %d, want 3 (alpha, bravo, charlie; delta has no deadline and echo is cancelled)", len(dated.Rows))
	}
	if got := sumCounts(dated.StatusCounts); got != 4 {
		t.Fatalf("wide deadline range status chips total %d over %v, want 4 dated tasks; the undated task must drop out of the chips exactly as it drops out of the rows", got, dated.StatusCounts)
	}
	if dated.ScopeCounts[domain.ScopeAssignedToMe] != 3 || dated.ScopeCounts[domain.ScopeTeamProgress] != 3 {
		t.Fatalf("wide deadline range scope chips = %v, want 3 on both arms", dated.ScopeCounts)
	}
	for _, r := range dated.Rows {
		if r.Title == "DateShift delta" {
			t.Fatal("a deadline range served the task that has no deadline")
		}
	}

	// 4. A RANGE THAT MATCHES NOTHING EMPTIES THE ROWS AND EVERY ARM. This is the assertion that
	// fails loudest if any one arm of the union forgets the request's date predicate: the rows go
	// to zero while that arm keeps reporting the whole list.
	farFrom, farTo := base.Add(30*24*time.Hour), base.Add(40*24*time.Hour)
	empty := list(t, ports.ListParams{DeadlineFrom: &farFrom, DeadlineTo: &farTo})
	if len(empty.Rows) != 0 || sumCounts(empty.StatusCounts) != 0 {
		t.Fatalf("a deadline range past every deadline served %d rows and chips %v, want none of either", len(empty.Rows), empty.StatusCounts)
	}
	for _, scope := range domain.ScopeKeys {
		if empty.ScopeCounts[scope] != 0 {
			t.Fatalf("scope chip %s = %d under a deadline range that matches nothing, want 0 (arm %v)", scope, empty.ScopeCounts[scope], empty.ScopeCounts)
		}
	}

	// 5. THE TWO DATE COLUMNS ARE INDEPENDENT PREDICATES ON EVERY ARM. Everything here was raised
	// TODAY, so a raise range over yesterday must empty the list and all three arms even though
	// the deadlines it is combined with are squarely inside their own range. An arm that read
	// raised_at where the rows read deadline_at (or the reverse) cannot satisfy both this and 2.
	pastFrom, pastTo := base.Add(-48*time.Hour), base.Add(-24*time.Hour)
	stale := list(t, ports.ListParams{RaisedFrom: &pastFrom, RaisedTo: &pastTo, DeadlineFrom: &from, DeadlineTo: &to})
	if len(stale.Rows) != 0 || sumCounts(stale.StatusCounts) != 0 || sumCounts(stale.ScopeCounts) != 0 {
		t.Fatalf("a raise range before every raise served %d rows, status %v, scope %v; want empty everywhere", len(stale.Rows), stale.StatusCounts, stale.ScopeCounts)
	}
	// And a raise range that DOES cover today changes nothing about the deadline-ranged answer.
	todayFrom, todayTo := byTitle["DateShift alpha"].RaisedAt.Add(-time.Minute), base.Add(time.Hour)
	both := list(t, ports.ListParams{RaisedFrom: &todayFrom, RaisedTo: &todayTo, DeadlineFrom: &from, DeadlineTo: &to})
	if !reflect.DeepEqual(both.StatusCounts, ranged.StatusCounts) || !reflect.DeepEqual(both.ScopeCounts, ranged.ScopeCounts) || len(both.Rows) != len(ranged.Rows) {
		t.Fatalf("a raise range covering every raise changed the deadline-ranged answer: rows %d/%d status %v/%v scope %v/%v",
			len(both.Rows), len(ranged.Rows), both.StatusCounts, ranged.StatusCounts, both.ScopeCounts, ranged.ScopeCounts)
	}

	// 6. THE DATE-FILTERED CHIPS ARE WHOLE-LIST, NOT PAGE-LOCAL. One row at a time across the
	// whole ranged list must report the same chips as one page of twenty.
	cursor := ""
	pages := 0
	served := map[string]bool{}
	for {
		p := list(t, ports.ListParams{DeadlineFrom: &allFrom, DeadlineTo: &allTo, Limit: 1, Cursor: cursor})
		pages++
		for _, r := range p.Rows {
			if served[r.TaskID] {
				t.Fatalf("task %s served twice while paging the date-filtered list", r.TaskID)
			}
			served[r.TaskID] = true
		}
		if !reflect.DeepEqual(p.StatusCounts, dated.StatusCounts) || !reflect.DeepEqual(p.ScopeCounts, dated.ScopeCounts) {
			t.Fatalf("page %d of 1 under the date filter reports status %v scope %v, want the whole-list %v / %v", pages, p.StatusCounts, p.ScopeCounts, dated.StatusCounts, dated.ScopeCounts)
		}
		cursor = p.NextCursor
		if cursor == "" || pages > 10 {
			break
		}
	}
	if len(served) != 3 {
		t.Fatalf("paging the date-filtered list one row at a time served %d distinct tasks over %d pages, want 3", len(served), pages)
	}
}

// TestLeadershipTaskAggregateScopeHierarchyCountsWhatEachTabLists is the SCOPE proof for the
// unioned list aggregate.
//
// The three tabs are three different populations, not three views of one: assigned_to_me pins
// the actor as ASSIGNEE, assigned_by_me pins them as RAISER, team_progress pins neither and is
// tenant-wide. Each tab's badge is its own arm of the union, and each arm DROPS the person
// filter its own tab already pins -- otherwise the badge would count rows the tab then refuses
// to show. That per-tab drop is the thing most likely to be got wrong (the two arms differ only
// in which of two booleans they pass), so the proof here is not "the numbers look plausible"
// but "each arm's number equals the rows that tab actually serves, walked page by page".
//
// The fixture makes the three populations three genuinely DIFFERENT sizes (3 / 4 / 9 unfiltered
// and 3 / 3 / 2 filtered, from three distinct filter sets), so an arm that dropped the wrong
// person filter or read the wrong party column cannot land on the right number by luck. It also pins the disjointness the union
// relies on: the leadership_tasks_not_self CHECK forbids raising a task to yourself, so no task
// can sit in both party arms and the two inboxes never double-count one fact.
func TestLeadershipTaskAggregateScopeHierarchyCountsWhatEachTabLists(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	// A THIRD assignable person. The seed fixture ticks Dinakar without a leadership grant, so
	// he is not assignable by default; granting him one gives the outbox two distinct assignees,
	// which is what lets the assignee filter actually narrow assigned_by_me below. Without a
	// second assignee on that arm, an arm that dropped the assignee filter instead of the raiser
	// filter would still land on the right number and the honesty assertion would prove nothing.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, ltDirector2); err != nil {
		t.Fatalf("seed park head grant: %v", err)
	}

	seeds := []struct {
		raiser, assignee, title string
		cancel                  bool
	}{
		{ltDirector, ltCXO, "ScopeHierarchy to me 1", false},
		{ltDirector, ltCXO, "ScopeHierarchy to me 2", false},
		{ltDirector, ltCXO, "ScopeHierarchy to me 3", false},
		{ltDirector, ltCXO, "ScopeHierarchy to me 4 cancelled", true},
		{ltCXO, ltDirector2, "ScopeHierarchy by me 4", false},
		{ltCXO, ltCXO2, "ScopeHierarchy by me 1", false},
		{ltCXO, ltCXO2, "ScopeHierarchy by me 2", false},
		{ltCXO, ltCXO2, "ScopeHierarchy by me 3", false},
		{ltDirector, ltCXO2, "ScopeHierarchy team only 1", false},
		{ltDirector, ltCXO2, "ScopeHierarchy team only 2", false},
	}
	for i, s := range seeds {
		task, err := repo.Raise(ctx, raiseAs(s.raiser, s.assignee, s.title, fmt.Sprintf("scopeh-%d", i), nil))
		if err != nil {
			t.Fatalf("raise %s: %v", s.title, err)
		}
		if s.cancel {
			if _, err := repo.ChangeStatus(ctx, ports.StatusParams{
				TenantID: ltTenant, Actor: domain.Actor{UserID: s.raiser, CanRaise: true},
				TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: task.RowVersion,
				IdempotencyKey: fmt.Sprintf("scopeh-cancel-%d", i),
			}); err != nil {
				t.Fatalf("cancel %s: %v", s.title, err)
			}
		}
		time.Sleep(2 * time.Millisecond) // distinct raised_at for a deterministic keyset
	}

	// walk serves ONE tab completely, two rows at a time, and returns the titles it listed. The
	// small page size is deliberate: the arm-versus-rows comparison below is only worth
	// something if the rows were genuinely paged across a boundary.
	walk := func(t *testing.T, p ports.ListParams) ([]string, ports.Page) {
		t.Helper()
		p.TenantID, p.UserID = ltTenant, ltCXO
		p.Statuses = domain.StatusesForFilter(domain.FilterAll)
		p.Limit = 2
		titles := []string{}
		seen := map[string]bool{}
		var first ports.Page
		cursor := ""
		for page := 0; page < 20; page++ {
			p.Cursor = cursor
			got, err := repo.ListTasks(ctx, p)
			if err != nil {
				t.Fatalf("walk %s: %v", p.Scope, err)
			}
			if page == 0 {
				first = got
			} else if !reflect.DeepEqual(got.ScopeCounts, first.ScopeCounts) {
				t.Fatalf("%s page %d reports scope chips %v, want the whole-list %v (the arms must not be page-local)", p.Scope, page, got.ScopeCounts, first.ScopeCounts)
			}
			for _, r := range got.Rows {
				if seen[r.TaskID] {
					t.Fatalf("%s served %q twice across a page boundary", p.Scope, r.Title)
				}
				seen[r.TaskID] = true
				titles = append(titles, r.Title)
			}
			cursor = got.NextCursor
			if cursor == "" {
				break
			}
		}
		return titles, first
	}

	// PART A: NO PERSON FILTERS. assigned_to_me is the actor's live inbox (3 -- the fourth was
	// cancelled), assigned_by_me is their outbox (4), and team_progress is every uncancelled task
	// in the tenant (9). Three different numbers from three different party predicates.
	toMeTitles, toMePage := walk(t, ports.ListParams{Scope: domain.ScopeAssignedToMe})
	byMeTitles, byMePage := walk(t, ports.ListParams{Scope: domain.ScopeAssignedByMe})
	teamTitles, teamPage := walk(t, ports.ListParams{Scope: domain.ScopeTeamProgress})
	wantArms := map[string]int{
		domain.ScopeAssignedToMe: len(toMeTitles),
		domain.ScopeAssignedByMe: len(byMeTitles),
		domain.ScopeTeamProgress: len(teamTitles),
	}
	if len(toMeTitles) != 3 || len(byMeTitles) != 4 || len(teamTitles) != 9 {
		t.Fatalf("tab row counts = to_me %d %v / by_me %d %v / team %d, want 3 / 4 / 9", len(toMeTitles), toMeTitles, len(byMeTitles), byMeTitles, len(teamTitles))
	}
	for _, from := range []struct {
		name string
		page ports.Page
	}{{"assigned_to_me", toMePage}, {"assigned_by_me", byMePage}, {"team_progress", teamPage}} {
		for scope, want := range wantArms {
			if from.page.ScopeCounts[scope] != want {
				t.Fatalf("read from the %s tab, the %s badge says %d but that tab LISTS %d rows (all badges %v)", from.name, scope, from.page.ScopeCounts[scope], want, from.page.ScopeCounts)
			}
		}
	}
	// THE TWO PARTY ARMS ARE DISJOINT, and that is an EXTERNAL fact rather than an assumption:
	// leadership_tasks_not_self forbids raising a task to yourself, so the inbox and the outbox
	// cannot share a task and the tenant-wide arm never double-counts one. Asserted both ways --
	// no title on both party tabs, and the two inboxes fitting inside the tenant-wide arm.
	onToMe := map[string]bool{}
	for _, title := range toMeTitles {
		onToMe[title] = true
	}
	for _, title := range byMeTitles {
		if onToMe[title] {
			t.Fatalf("%q is on BOTH party tabs; leadership_tasks_not_self should make that impossible", title)
		}
	}
	if _, err := repo.Raise(ctx, raiseAs(ltCXO, ltCXO, "ScopeHierarchy self", "scopeh-self", nil)); err == nil {
		t.Fatal("raising a task to yourself was accepted; the party arms are only disjoint because the database refuses it")
	}
	if len(toMeTitles)+len(byMeTitles) > len(teamTitles) {
		t.Fatalf("the two party arms (%d + %d) do not fit inside the tenant-wide arm (%d)", len(toMeTitles), len(byMeTitles), len(teamTitles))
	}
	// The cancelled task is on no tab and in no arm; every arm carries status <> 'cancelled'.
	for _, list := range [][]string{toMeTitles, byMeTitles, teamTitles} {
		for _, title := range list {
			if title == "ScopeHierarchy to me 4 cancelled" {
				t.Fatalf("a cancelled task was listed on a tab: %v", list)
			}
		}
	}

	// PART B: THE PER-TAB HONESTY RULE. The request carries BOTH person filters at once. Each
	// tab drops the one it pins -- assigned_to_me keeps only the raiser filter, assigned_by_me
	// keeps only the assignee filter, team_progress keeps both -- and each arm must drop exactly
	// what its tab drops. The three populations come out 3 / 4 / 2, all different, so an arm
	// that dropped the WRONG filter reports 4 where the tab lists 3.
	toMeFiltered, toMeFilteredPage := walk(t, ports.ListParams{Scope: domain.ScopeAssignedToMe, Query: "ScopeHierarchy", RaisedBy: ltDirector})
	byMeFiltered, byMeFilteredPage := walk(t, ports.ListParams{Scope: domain.ScopeAssignedByMe, Query: "ScopeHierarchy", AssigneeUserID: ltCXO2})
	teamFiltered, teamFilteredPage := walk(t, ports.ListParams{Scope: domain.ScopeTeamProgress, Query: "ScopeHierarchy", AssigneeUserID: ltCXO2, RaisedBy: ltDirector})
	if len(toMeFiltered) != 3 || len(byMeFiltered) != 3 || len(teamFiltered) != 2 {
		t.Fatalf("filtered tab row counts = to_me %d %v / by_me %d %v / team %d %v, want 3 / 3 / 2", len(toMeFiltered), toMeFiltered, len(byMeFiltered), byMeFiltered, len(teamFiltered), teamFiltered)
	}
	// EACH TAB'S OWN BADGE EQUALS THE ROWS THAT TAB SERVED. That is the honesty rule, and it is
	// the per-tab one: the badge is computed under the filter set the REQUEST carries, and the
	// request's filter set is itself per-tab because the app drops the person filter the open tab
	// pins. So the claim proved here is the one a leader can act on -- the number on the tab I am
	// standing on is the number of rows this tab just gave me -- asserted for all three tabs.
	for _, own := range []struct {
		name string
		page ports.Page
		rows int
	}{
		{domain.ScopeAssignedToMe, toMeFilteredPage, len(toMeFiltered)},
		{domain.ScopeAssignedByMe, byMeFilteredPage, len(byMeFiltered)},
		{domain.ScopeTeamProgress, teamFilteredPage, len(teamFiltered)},
	} {
		if own.page.ScopeCounts[own.name] != own.rows {
			t.Fatalf("on the filtered %s tab the badge says %d but the tab LISTED %d rows (badges %v)", own.name, own.page.ScopeCounts[own.name], own.rows, own.page.ScopeCounts)
		}
	}
	// AND THE OTHER TABS' BADGES ARE THE SAME ARITHMETIC UNDER THIS REQUEST'S FILTERS, which is a
	// different question and a different number. Standing on assigned_to_me, the request carries
	// only the raiser filter (the tab pins the assignee), so the tenant-wide badge counts the
	// raiser's uncancelled tenant tasks -- 5 -- while OPENING that tab re-applies the assignee
	// filter too and lists 2. Pinned exactly, so that the design cannot drift unnoticed: these
	// are the numbers each arm owes for the filters it was actually given.
	wantPerRequest := []struct {
		name string
		page ports.Page
		arms map[string]int
	}{
		{domain.ScopeAssignedToMe, toMeFilteredPage, map[string]int{domain.ScopeAssignedToMe: 3, domain.ScopeAssignedByMe: 4, domain.ScopeTeamProgress: 5}},
		{domain.ScopeAssignedByMe, byMeFilteredPage, map[string]int{domain.ScopeAssignedToMe: 3, domain.ScopeAssignedByMe: 3, domain.ScopeTeamProgress: 5}},
		{domain.ScopeTeamProgress, teamFilteredPage, map[string]int{domain.ScopeAssignedToMe: 3, domain.ScopeAssignedByMe: 3, domain.ScopeTeamProgress: 2}},
	}
	for _, from := range wantPerRequest {
		if !reflect.DeepEqual(from.page.ScopeCounts, from.arms) {
			t.Fatalf("read from the filtered %s tab, the badges say %v, want %v", from.name, from.page.ScopeCounts, from.arms)
		}
	}
	// One filter that matches nothing empties the rows and every arm together.
	missTitles, missPage := walk(t, ports.ListParams{Scope: domain.ScopeTeamProgress, Query: "no such words here"})
	if len(missTitles) != 0 {
		t.Fatalf("a text miss listed %v", missTitles)
	}
	for _, scope := range domain.ScopeKeys {
		if missPage.ScopeCounts[scope] != 0 {
			t.Fatalf("a text miss left the %s badge at %d (all %v)", scope, missPage.ScopeCounts[scope], missPage.ScopeCounts)
		}
	}
}

// liveTaskStatuses reads the status vocabulary from the DATABASE's own CHECK constraint rather
// than from a remembered list, which the aggregate reference requires: a migration that adds a
// fifth status must break the status-matrix test rather than leave it silently covering four.
func liveTaskStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	var def string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
WHERE c.conrelid = 'public.leadership_tasks'::regclass
  AND c.contype = 'c'
  AND pg_get_constraintdef(c.oid) ILIKE '%status%'
ORDER BY c.conname
LIMIT 1`).Scan(&def); err != nil {
		t.Fatalf("read the live status CHECK constraint: %v", err)
	}
	parts := strings.Split(def, "'")
	out := make([]string, 0, 4)
	for i := 1; i < len(parts); i += 2 {
		out = append(out, parts[i])
	}
	if len(out) == 0 {
		t.Fatalf("no status literals in the CHECK constraint %q", def)
	}
	sort.Strings(out)
	return out
}

// TestLeadershipTaskAggregateStatusMatrixOneToManyFanOutAndPageBoundaryStayHonest is the
// STATUS, CARDINALITY and PAGE-BOUNDARY proof for the unioned list aggregate.
//
// Three things, and they have to be proved together because each one can hide the others:
//
//   - STATUS MATRIX. Every status the live DB CHECK constraint allows is occupied, the
//     vocabulary is read FROM that constraint rather than from a remembered list, and the
//     buckets are proved disjoint by summing to the population exactly once.
//   - ONE-TO-MANY. One task carries two attachments and two notes, one of them with a mention.
//     Four (attachment, note) combinations exist for it, so a count that reached those tables
//     would report that task two or four times; the bucket totals must not move at all, and the
//     row must come back once with all of its children on it.
//   - PAGE BOUNDARY. The whole list is walked ONE row at a time and the buckets must be
//     identical on every page and identical to one page of fifty. A bucket that had inherited
//     the row query's LIMIT would drift page by page.
func TestLeadershipTaskAggregateStatusMatrixOneToManyFanOutAndPageBoundaryStayHonest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	assignee := domain.Actor{UserID: ltCXO, CanAct: true}
	raiser := domain.Actor{UserID: ltDirector, CanRaise: true}

	// The vocabulary comes from the database, so this test cannot quietly cover a stale set.
	live := liveTaskStatuses(t, ctx, pool)
	known := []string{domain.StatusCancelled, domain.StatusDone, domain.StatusInProgress, domain.StatusOpen}
	if !reflect.DeepEqual(live, known) {
		t.Fatalf("the live status CHECK allows %v but this module knows %v; the matrix below covers the module's set, so reconcile them before trusting any chip", live, known)
	}

	// One task per live status, plus a second open and a second done so no bucket is a
	// degenerate 1 and the fan-out task's bucket has a neighbour to be confused with.
	want := map[string]int{domain.StatusOpen: 2, domain.StatusInProgress: 1, domain.StatusDone: 2, domain.StatusCancelled: 1}
	plan := []string{domain.StatusOpen, domain.StatusOpen, domain.StatusInProgress, domain.StatusDone, domain.StatusDone, domain.StatusCancelled}
	fanOutID := ""
	for i, status := range plan {
		p := raiseAs(ltDirector, ltCXO, fmt.Sprintf("StatusMatrix ask %d", i), fmt.Sprintf("statusmatrix-%d", i), nil)
		// The LAST done task is the one-to-many subject: two attachment proofs on the row.
		if i == 4 {
			p = raiseAs(ltDirector, ltCXO, "StatusMatrix ask 4 with children", "statusmatrix-4", nil)
			p.Attachments = raiseParams(ltCXO, "x", "x", ltProof1, ltProof2).Attachments
		}
		task, err := repo.Raise(ctx, p)
		if err != nil {
			t.Fatalf("raise %d: %v", i, err)
		}
		if i == 4 {
			fanOutID = task.TaskID
		}
		switch status {
		case domain.StatusInProgress, domain.StatusDone:
			moved, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: assignee, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("sm-mid-%d", i)})
			if err != nil {
				t.Fatalf("%d -> in_progress: %v", i, err)
			}
			task = moved
			if status == domain.StatusDone {
				if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: assignee, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("sm-done-%d", i)}); err != nil {
					t.Fatalf("%d -> done: %v", i, err)
				}
			}
		case domain.StatusCancelled:
			if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: raiser, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("sm-cancel-%d", i)}); err != nil {
				t.Fatalf("%d -> cancelled: %v", i, err)
			}
		}
		time.Sleep(2 * time.Millisecond) // distinct raised_at for a deterministic keyset
	}

	// TWO notes on the fan-out task, one of them naming another leader. With its two
	// attachments that is four (attachment, note) pairs for a single task.
	for n, note := range []struct{ body, key string }{
		{"Sending this to @Manohar for the vendor call.", "sm-note-1"},
		{"Second note, same task.", "sm-note-2"},
	} {
		cur, err := repo.GetTask(ctx, ltTenant, fanOutID)
		if err != nil {
			t.Fatal(err)
		}
		mentions := []string{ltCXO2}
		if n == 1 {
			mentions = nil
		}
		if _, err := repo.SetComment(ctx, ports.CommentParams{
			TenantID: ltTenant, Actor: assignee, TaskID: cur.TaskID,
			Comment: note.body, MentionUserIDs: mentions, IdempotencyKey: note.key,
		}); err != nil {
			t.Fatalf("note %d: %v", n, err)
		}
	}

	list := func(t *testing.T, limit int, cursor string) ports.Page {
		t.Helper()
		page, err := repo.ListTasks(ctx, ports.ListParams{
			TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe,
			Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: limit, Cursor: cursor,
		})
		if err != nil {
			t.Fatalf("list limit=%d cursor=%q: %v", limit, cursor, err)
		}
		return page
	}

	// 1. EVERY LIVE BUCKET, AND THE BUCKETS ARE DISJOINT. The chips drop the status predicate, so
	// their population is all six party tasks including the cancelled one; total = sum of the
	// buckets exactly once, with no bucket outside the live vocabulary.
	whole := list(t, 50, "")
	if !reflect.DeepEqual(whole.StatusCounts, want) {
		t.Fatalf("status buckets = %v, want %v (one per live status, none fanned out by the children)", whole.StatusCounts, want)
	}
	if got := sumCounts(whole.StatusCounts); got != len(plan) {
		t.Fatalf("status buckets total %d over %v, want the %d tasks raised; a fan-out would show up here as a total larger than the population", got, whole.StatusCounts, len(plan))
	}
	for bucket := range whole.StatusCounts {
		if !domain.IsKnownStatus(bucket) {
			t.Fatalf("status buckets carry %q, which is not in the live vocabulary %v", bucket, live)
		}
	}
	// The rows hide cancelled, so the five uncancelled ones are the list; the unseen badge is
	// the same five, and neither number is page-local.
	if len(whole.Rows) != 5 || whole.UnseenCount != 5 {
		t.Fatalf("whole page: %d rows, unseen %d, want 5 and 5", len(whole.Rows), whole.UnseenCount)
	}

	// 2. THE FAN-OUT TASK IS ONE ROW WITH ALL ITS CHILDREN, AND ITS BUCKET DID NOT GROW. Its two
	// attachments and two notes are four combinations; done must still read 2, not 3, 4 or 8.
	hits := 0
	for _, r := range whole.Rows {
		if r.TaskID != fanOutID {
			continue
		}
		hits++
		if len(r.Attachments) != 2 || r.AttachmentCount != 2 {
			t.Fatalf("fan-out row attachments = %d (count %d), want 2", len(r.Attachments), r.AttachmentCount)
		}
		if len(r.Notes) != 2 {
			t.Fatalf("fan-out row notes = %d, want 2", len(r.Notes))
		}
		mentioned := 0
		for _, n := range r.Notes {
			mentioned += len(n.Mentions)
		}
		if mentioned != 1 {
			t.Fatalf("fan-out row mentions = %d across its notes, want 1", mentioned)
		}
	}
	if hits != 1 {
		t.Fatalf("the task carrying two attachments and two notes came back %d times, want exactly 1 row", hits)
	}

	// 3. PAGE BOUNDARY. One row at a time over the whole list: same buckets, same unseen badge,
	// every row exactly once.
	cursor := ""
	served := map[string]bool{}
	for page := 0; page < 10; page++ {
		p := list(t, 1, cursor)
		if !reflect.DeepEqual(p.StatusCounts, want) {
			t.Fatalf("page %d of 1 reports buckets %v, want the whole-list %v (a bucket that inherited the row LIMIT would drift here)", page, p.StatusCounts, want)
		}
		if p.UnseenCount != whole.UnseenCount {
			t.Fatalf("page %d of 1 reports unseen %d, want the whole-list %d", page, p.UnseenCount, whole.UnseenCount)
		}
		for _, r := range p.Rows {
			if served[r.TaskID] {
				t.Fatalf("task %s served twice while paging one row at a time", r.TaskID)
			}
			served[r.TaskID] = true
		}
		cursor = p.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(served) != 5 {
		t.Fatalf("paging one row at a time served %d distinct tasks, want 5", len(served))
	}
	// 4. AND THE SAME ANSWER FROM A SINGLE FILTERED CHIP. Asking for only the done ones narrows
	// the ROWS to two while the buckets stay the whole matrix: the chips describe the list the
	// filter is being applied to, which is what makes them usable as a filter control.
	doneOnly, err := repo.ListTasks(ctx, ports.ListParams{
		TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe,
		Statuses: domain.StatusesForFilter(domain.FilterDone), Limit: 50,
	})
	if err != nil {
		t.Fatalf("done-only page: %v", err)
	}
	if len(doneOnly.Rows) != 2 {
		t.Fatalf("done-only rows = %d, want 2", len(doneOnly.Rows))
	}
	if !reflect.DeepEqual(doneOnly.StatusCounts, want) {
		t.Fatalf("done-only buckets = %v, want the whole matrix %v", doneOnly.StatusCounts, want)
	}
}

// TestListAssigneesReadsTheMobileTickForEveryCallingSurface pins the fact a 2026-09-18
// investigation got wrong. Three staging directors (Chandrakant, Dinakar, Hemant) carry the
// Tasks module at oversee on their MOBILE row and at view+configure only on their WEB row,
// and that was reported as a data gap shrinking the web assignee/mention picker.
//
// It is not a gap. The picker's population has ONE authored surface -- the person's mobile
// tick (migration 000292: "every person whose own mobile row on /people carries the Tasks
// module at OVERSEE") -- and ListAssignees, sqlAssigneeIsTicked and sqlListMentionableUsers
// all pass permissions.SurfaceMobile regardless of which client called. admin-web and Android
// share the one endpoint, so a mobile-only oversee tick is ALREADY in the web picker and
// backfilling web rows would widen nobody's authority while inventing a second source of
// truth for a population that has one.
//
// So this asserts both halves: a mobile-only tick IS assignable, and a web-only tick is NOT.
// The second half is what makes the intended migration visibly wrong -- writing web oversee
// rows cannot add anyone to this list.
func TestListAssigneesReadsTheMobileTickForEveryCallingSurface(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// ltDirector already holds an active park_head grant and NO tick. Give him the shape the
	// three staging directors have on WEB -- view+configure, no oversee -- plus a web oversee
	// row, which is the exact row the proposed migration would have written.
	if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT $1::uuid, workforce_member_id, 'web', 'leadership_tasks', ARRAY['view','oversee','configure']::text[]
FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ltDirector); err != nil {
		t.Fatalf("seed web-only tick: %v", err)
	}

	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil {
		t.Fatalf("list assignees: %v", err)
	}
	got := make(map[string]bool, len(assignees))
	for _, a := range assignees {
		got[a.UserID] = true
	}

	// ltCXO is ticked at oversee on MOBILE ONLY and holds ceo_internal: the staging directors'
	// shape. He must be in the picker every client sees.
	if !got[ltCXO] {
		t.Errorf("mobile-only oversee tick is missing from the picker: a web caller would not see a person whose authored tick is on mobile, which is the whole population")
	}
	// A web oversee tick with no mobile oversee tick admits nobody.
	if got[ltDirector] {
		t.Errorf("a WEB oversee tick made someone assignable: the picker must read the mobile tick alone, else per-person access has two sources of truth for this population")
	}

	// And the write path agrees with the list, so the picker cannot offer someone the raise refuses.
	var ticked bool
	if err := pool.QueryRow(ctx, sqlAssigneeIsTicked,
		ltTenant, ltDirector, "mobile", leadershipTasksModuleKey, "oversee", assignableLeadershipRoles).Scan(&ticked); err != nil {
		t.Fatalf("assignee is ticked: %v", err)
	}
	if ticked {
		t.Errorf("raise-time check accepted a web-only oversee tick; it must read the same mobile tick the picker does")
	}
}
