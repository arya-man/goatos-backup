package postgres

import (
	"context"
	"errors"
	"fmt"
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
// their user ids, the CXOs' ceo_internal grants, and two completed attachment proofs. Every
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
	// Assignability is the person's own mobile tick at Oversee, never the role: both CXOs
	// hold ceo_internal, only these two are ticked, and Dinakar (a director) is ticked too
	// to prove the tick is what the picker reads.
	for _, cxo := range []string{ltCXO, ltCXO2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, cxo); err != nil {
			t.Fatalf("seed grant: %v", err)
		}
	}
	for _, ticked := range []string{ltCXO, ltCXO2} {
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT $1::uuid, workforce_member_id, 'mobile', 'leadership_tasks', ARRAY['view','oversee']::text[]
FROM workforce_members WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, ltTenant, ticked); err != nil {
			t.Fatalf("seed tick: %v", err)
		}
	}
	// A CXO-shaped grant with NO tick: must never be assignable.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, ltTenant, ltDirector2); err != nil {
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
	// holding the CXO role.
	if _, err := repo.Raise(ctx, raiseParams(ltDirector2, "Not ticked", "raise-bad")); !errors.Is(err, domain.ErrAssigneeNotCXO) {
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

	// 6. Edit by the raiser: brief replaced, attachment list diffed (proof 2 dropped).
	edited, err := repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "Approve the vendor contract (revised)", Body: "New brief.",
		Attachments: []domain.Attachment{{ProofID: ltProof1, Kind: domain.AttachmentAudio, MimeType: "audio/mp4", FileName: "note.m4a", SizeBytes: 4096}},
		RowVersion:  started.RowVersion, IdempotencyKey: "edit-1",
	})
	if err != nil || edited.Title != "Approve the vendor contract (revised)" || len(edited.Attachments) != 1 || edited.Attachments[0].ProofID != ltProof1 {
		t.Fatalf("edit: %+v err %v", edited, err)
	}
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector2, TaskID: task.TaskID, Title: "x", RowVersion: edited.RowVersion, IdempotencyKey: "edit-2"}); !errors.Is(err, domain.ErrNotRaiser) {
		t.Fatalf("another director editing: %v", err)
	}

	// 7. Done, then the raiser can no longer edit; the assignee may reopen.
	done, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: edited.RowVersion, IdempotencyKey: "st-4"})
	if err != nil || done.Status != domain.StatusDone || done.DoneAt == nil {
		t.Fatalf("done: %+v err %v", done, err)
	}
	if _, err := repo.Edit(ctx, ports.EditParams{TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "late", RowVersion: done.RowVersion, IdempotencyKey: "edit-3"}); !errors.Is(err, domain.ErrTaskClosed) {
		t.Fatalf("edit after done: %v", err)
	}
	reopened, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: done.RowVersion, IdempotencyKey: "st-5"})
	if err != nil || reopened.Status != domain.StatusInProgress || reopened.DoneAt != nil {
		t.Fatalf("reopen: %+v err %v", reopened, err)
	}
	// 8. Cancel by the raiser is terminal.
	cancelled, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: director, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: reopened.RowVersion, IdempotencyKey: "st-6"})
	if err != nil || cancelled.Status != domain.StatusCancelled || cancelled.CancelledAt == nil {
		t.Fatalf("cancel: %+v err %v", cancelled, err)
	}
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusOpen, RowVersion: cancelled.RowVersion, IdempotencyKey: "st-7"}); !errors.Is(err, domain.ErrTaskClosed) {
		t.Fatalf("reopening a cancelled task: %v", err)
	}

	// 9. Every write announced itself: one raised event, five status events (start, done,
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

// TestLeadershipTaskListPaginationPageBoundaryAndEveryStatusBuckets pins the list's grain
// and its keyset: rows are the caller's PARTY rows only, attachments never fan a task into
// several rows (OneToMany), the chip counts range over the whole party list across every
// status bucket (StatusMatrix) and never over the page, and a page boundary hands the exact
// next row through the cursor with no duplicate and no gap.
func TestLeadershipTaskListPaginationPageBoundaryAndEveryStatusBuckets(t *testing.T) {
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
	page1, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3})
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
	page2, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 3, Cursor: page1.NextCursor})
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
	donePage, _ := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Statuses: domain.StatusesForFilter(domain.FilterDone), Limit: 20})
	if len(donePage.Rows) != 1 || donePage.Rows[0].TaskID != ids[2] {
		t.Fatalf("done page = %+v", donePage.Rows)
	}
	directorPage, _ := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltDirector, Statuses: domain.StatusesForFilter(domain.FilterAll), Limit: 20})
	if len(directorPage.Rows) != 6 || directorPage.UnseenCount != 0 {
		t.Fatalf("director sees %d rows (want 6 uncancelled raised) unseen=%d", len(directorPage.Rows), directorPage.UnseenCount)
	}
	assignees, err := repo.ListAssignees(ctx, ltTenant)
	if err != nil || len(assignees) != 2 || assignees[0].Name != "Manohar" || assignees[1].Name != "Ravi" {
		t.Fatalf("assignees = %+v err %v", assignees, err)
	}
	_ = cxo
}
