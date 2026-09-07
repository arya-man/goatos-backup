package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// assertEnvelopesValid validates every pen_visit.* outbox envelope against the SAME schema the
// relay validates against before publish. A rejected envelope is a SILENT drop (status failed,
// no consumer ever sees it) -- exactly what shipped on the first live run, where actor_type
// "system" and two top-level park/shed keys were refused -- so the shape is pinned here.
func assertEnvelopesValid(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	schema := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json")
	validator, err := outboxapp.NewEnvelopeValidator(schema)
	if err != nil {
		t.Fatalf("envelope validator: %v", err)
	}
	rows, err := pool.Query(ctx, `SELECT event_type, payload FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type LIKE 'pen_visit.%'`, tenantID)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var eventType string
		var payload []byte
		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatal(err)
		}
		if err := validator.Validate(payload); err != nil {
			t.Fatalf("%s envelope rejected by the relay schema: %v\n%s", eventType, err, payload)
		}
		n++
	}
	return n
}

const (
	pvTenant     = "00000000-0000-4000-8000-0000000e0001"
	pvParkCBE    = "00000000-0000-4000-8000-0000000e0101"
	pvParkCPT    = "00000000-0000-4000-8000-0000000e0102"
	pvShedCastro = "00000000-0000-4000-8000-0000000e0201"
	pvShedGodel  = "00000000-0000-4000-8000-0000000e0202"
	pvShedCPT    = "00000000-0000-4000-8000-0000000e0203"
	pvDinakar    = "00000000-0000-4000-8000-0000000e0301"
	pvOther      = "00000000-0000-4000-8000-0000000e0302"
	pvProof      = "00000000-0000-4000-8000-0000000e0401"
	pvProof2     = "00000000-0000-4000-8000-0000000e0402"
)

// seedPenVisitFixture: one tenant, two parks (CBE with a configured head, CPT with NONE), three
// sheds, the park head, and two finished proofs.
func seedPenVisitFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Pen Visit Test', 'active') ON CONFLICT DO NOTHING`, pvTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'Coimbatore', 'active'), ($3::uuid, $1::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, pvTenant, pvParkCBE, pvParkCPT)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-CASTRO', 'Castro', 'active', $4::uuid, 1),
       ($3::uuid, $1::uuid, 'shed', 'S-GODEL', 'Godel 1', 'active', $4::uuid, 2),
       ($5::uuid, $1::uuid, 'shed', 'S-YASH', 'Yashoda', 'active', $6::uuid, 1)
ON CONFLICT (location_id) DO NOTHING`, pvTenant, pvShedCastro, pvShedGodel, pvParkCBE, pvShedCPT, pvParkCPT)
	exec(`INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, 'DIN', 'Dinakar', 'active'), ($1::uuid, $3::uuid, 'OTH', 'Someone Else', 'active')`, pvTenant, pvDinakar, pvOther)
	exec(`INSERT INTO pen_visit_park_assignees (tenant_id, park_id, user_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, pvTenant, pvParkCBE, pvDinakar)
	for _, proof := range []string{pvProof, pvProof2} {
		exec(`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, size_bytes, upload_state, scope_type, scope_id, subject_type, proof_type, uploaded_by, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'visits/' || $1::text, 'video/mp4', 4096, 'completed', 'task', $1::uuid, 'other', 'video', $3::uuid, '{"capture_source":"in_app_camera"}'::jsonb)`, proof, pvTenant, pvDinakar)
	}
}

// seedWork raises one verification item -- what a vaccination shed submit or a PC Care submit
// leaves behind -- for a pen, at an instant.
func seedWork(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key, parkID, shedID, partition, module, category string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, partition_label, captured_at, created_at, idempotency_key)
VALUES (gen_random_uuid(), $1::uuid, 'preventive_care', $5, $6, $5, gen_random_uuid(), gen_random_uuid(), 'sop_submission', gen_random_uuid(), '["proof"]'::jsonb, 'pending', $2::uuid, $3::uuid, NULLIF($4, ''), $7::timestamptz, $7::timestamptz, $8)`,
		pvTenant, parkID, shedID, partition, module, category, at, "pen-visit-test:"+key); err != nil {
		t.Fatalf("seed work %s: %v", key, err)
	}
}

func istInstant(date string, hour int) time.Time {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	d, _ := time.ParseInLocation("2006-01-02", date, loc)
	return d.Add(time.Duration(hour) * time.Hour)
}

// TestPenVisitLifecycleOneToManyParkScopePaginationPostgresPaths drives the production path end to end on a real
// database: the materializer folds one day's proof items into one visit per pen (two items in
// one pen -> one task carrying both reasons; a park with no configured head gets nothing and is
// named), a replay creates nothing, the assignee's list and counts read it back, submit
// completes it under the version fence with audit + outbox, an exact replay returns the same
// row, and the roll-forward carries an unvisited pen to today as delayed. The name carries the
// three adversarial shapes it pins: OneToMany (several proof items -> one pen -> one task),
// ParkScope (a park with no configured head gets nothing), Pagination (keyset page boundary).
func TestPenVisitLifecycleOneToManyParkScopePaginationPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedPenVisitFixture(t, ctx, pool)

	const source = "2026-09-06"
	const today = "2026-09-07"
	now := istInstant(today, 0).Add(5 * time.Minute)
	repo := NewRepository(pool, 10*time.Second).WithClock(func() time.Time { return now })

	// Yesterday's work: Castro 2 vaccinated AND dewormed (two items, one pen), Godel 1 - Part 3
	// hoof-trimmed, a CPT pen vaccinated (no head configured), an inventory-vaccine item in
	// Castro 2 that is not pen work, and a Castro 2 item from TODAY that belongs to tomorrow's
	// visit.
	seedWork(t, ctx, pool, "castro-vacc", pvParkCBE, pvShedCastro, "2", "vaccination", "vaccination_proof", istInstant(source, 10))
	seedWork(t, ctx, pool, "castro-deworm", pvParkCBE, pvShedCastro, "2", "pc_care", "pc_deworming", istInstant(source, 15))
	seedWork(t, ctx, pool, "godel-hoof", pvParkCBE, pvShedGodel, "Part 3", "pc_care", "pc_hoof_trimming", istInstant(source, 11))
	seedWork(t, ctx, pool, "cpt-vacc", pvParkCPT, pvShedCPT, "", "vaccination", "vaccination_proof", istInstant(source, 9))
	seedWork(t, ctx, pool, "castro-inventory", pvParkCBE, pvShedCastro, "2", "pc_care", "inventory_vaccine", istInstant(source, 12))
	seedWork(t, ctx, pool, "castro-today", pvParkCBE, pvShedCastro, "2", "vaccination", "vaccination_proof", istInstant(today, 9))

	result, digests, err := repo.Materialize(ctx, pvTenant, source, today, now)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if result.Created != 2 || result.Widened != 0 {
		t.Fatalf("materialize result = %+v, want 2 created", result)
	}
	if len(result.ParksWithoutAssignee) != 1 || result.ParksWithoutAssignee[0] != pvParkCPT || result.PensSkipped != 1 {
		t.Fatalf("CPT (no head configured) must be reported, got %+v", result)
	}
	if len(digests) != 1 || digests[0].ParkID != pvParkCBE || digests[0].AssigneeID != pvDinakar || len(digests[0].Tasks) != 2 || digests[0].DueDate != today {
		t.Fatalf("digest = %+v", digests)
	}
	if digests[0].ParkName != "Coimbatore" {
		t.Fatalf("digest park name = %q", digests[0].ParkName)
	}

	// The list reads the two visits back for the head, with the pen labels composed through
	// oploc and both reasons on the Castro pen.
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvDinakar, States: domain.StatesForFilter(domain.FilterToDo), Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Rows) != 2 || page.StateCounts[domain.WorkStateScheduled] != 2 {
		t.Fatalf("list rows = %d counts = %v", len(page.Rows), page.StateCounts)
	}
	var castro, godel domain.Task
	for _, row := range page.Rows {
		switch row.ShedID {
		case pvShedCastro:
			castro = row
		case pvShedGodel:
			godel = row
		}
	}
	if castro.PenLabel != "Castro 2" || godel.PenLabel != "Godel 1 - Part 3" {
		t.Fatalf("pen labels = %q / %q", castro.PenLabel, godel.PenLabel)
	}
	if len(castro.Reasons) != 2 || castro.SourceDate != source || castro.DueDate != today || castro.PlannedDate != today || castro.ParkName != "Coimbatore" {
		t.Fatalf("castro task = %+v", castro)
	}
	if len(godel.Reasons) != 1 || godel.Reasons[0] != domain.ReasonHoofTrimming {
		t.Fatalf("godel reasons = %v", godel.Reasons)
	}
	// Nobody else sees them.
	otherPage, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvOther, Limit: 20})
	if err != nil || len(otherPage.Rows) != 0 {
		t.Fatalf("other person's list = %d rows, err %v", len(otherPage.Rows), err)
	}
	if n, err := repo.OpenCount(ctx, pvTenant, pvDinakar); err != nil || n != 2 {
		t.Fatalf("open count = %d err %v", n, err)
	}

	// A replay of the same day creates nothing. The digest can still be rebuilt from the open
	// tasks, so a transient push-queue failure after the first commit is retried on the next kernel
	// tick instead of being lost forever.
	replay, replayDigests, err := repo.Materialize(ctx, pvTenant, source, today, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.Created != 0 || replay.Widened != 0 || len(replayDigests) != 0 {
		t.Fatalf("replay must be a no-op, got %+v %+v", replay, replayDigests)
	}
	retryDigests, err := repo.DueDigestsForSourceDate(ctx, pvTenant, source)
	if err != nil {
		t.Fatalf("retry digests: %v", err)
	}
	if len(retryDigests) != 1 || retryDigests[0].ParkID != pvParkCBE || retryDigests[0].AssigneeID != pvDinakar || retryDigests[0].DueDate != today || len(retryDigests[0].Tasks) != 2 {
		t.Fatalf("retry digests = %+v", retryDigests)
	}
	// A late item for the same pen and day (an offline phone syncing after the tick) only
	// widens the open visit's reasons -- never a second task.
	seedWork(t, ctx, pool, "godel-ticks-late", pvParkCBE, pvShedGodel, "Part 3", "pc_care", "pc_ticks_removal", istInstant(source, 17))
	widened, _, err := repo.Materialize(ctx, pvTenant, source, today, now.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("widen: %v", err)
	}
	if widened.Created != 0 || widened.Widened != 1 {
		t.Fatalf("widen result = %+v", widened)
	}
	godelAfter, err := repo.GetTask(ctx, pvTenant, godel.TaskID)
	if err != nil || len(godelAfter.Reasons) != 2 {
		t.Fatalf("godel after widen = %+v err %v", godelAfter, err)
	}

	// Every created task announced itself exactly once, with an envelope the relay will accept.
	var created int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'pen_visit.created'`, pvTenant).Scan(&created); err != nil || created != 2 {
		t.Fatalf("pen_visit.created outbox rows = %d err %v", created, err)
	}
	if n := assertEnvelopesValid(t, ctx, pool, pvTenant); n != 2 {
		t.Fatalf("validated %d envelopes, want 2", n)
	}

	// Submit: the wrong person is refused, the right one completes it, an exact replay returns
	// the same row, a stale version is refused, and a second submit is 'already done'.
	actor := domain.Actor{UserID: pvDinakar}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: domain.Actor{UserID: pvOther}, TaskID: castro.TaskID, ProofRef: pvProof, RowVersion: castro.RowVersion, IdempotencyKey: "submit-other"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("other person's submit = %v, want ErrNotAssignee", err)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: castro.TaskID, ProofRef: pvProof, RowVersion: castro.RowVersion + 7, IdempotencyKey: "submit-stale"}); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale submit = %v, want ErrVersionConflict", err)
	}
	done, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: castro.TaskID, ProofRef: pvProof, RowVersion: castro.RowVersion, IdempotencyKey: "submit-1", TraceID: "trace-1"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if done.WorkState != domain.WorkStateCompleted || done.ProofRef == nil || *done.ProofRef != pvProof || done.SubmittedAt == nil || done.RowVersion != castro.RowVersion+1 {
		t.Fatalf("submitted task = %+v", done)
	}
	again, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: castro.TaskID, ProofRef: pvProof, RowVersion: castro.RowVersion, IdempotencyKey: "submit-1"})
	if err != nil || again.RowVersion != done.RowVersion {
		t.Fatalf("exact replay = %+v err %v", again, err)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: castro.TaskID, ProofRef: pvProof2, RowVersion: castro.RowVersion, IdempotencyKey: "submit-1"}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key, different proof = %v, want ErrIdempotencyConflict", err)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: castro.TaskID, ProofRef: pvProof2, RowVersion: 0, IdempotencyKey: "submit-2"}); !errors.Is(err, domain.ErrAlreadyDone) {
		t.Fatalf("second submit = %v, want ErrAlreadyDone", err)
	}
	var submitted, audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'pen_visit.submitted'`, pvTenant).Scan(&submitted); err != nil || submitted != 1 {
		t.Fatalf("pen_visit.submitted outbox rows = %d err %v", submitted, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND resource_type = 'pen_visit_task' AND action = 'pen_visit.submitted'`, pvTenant).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("submit audit rows = %d err %v", audits, err)
	}
	if n := assertEnvelopesValid(t, ctx, pool, pvTenant); n != 3 {
		t.Fatalf("validated %d envelopes after submit, want 3", n)
	}
	donePage, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvDinakar, States: domain.StatesForFilter(domain.FilterDone), Limit: 20})
	if err != nil || len(donePage.Rows) != 1 || donePage.StateCounts[domain.WorkStateCompleted] != 1 || donePage.StateCounts[domain.WorkStateScheduled] != 1 {
		t.Fatalf("done list = %d rows counts %v err %v", len(donePage.Rows), donePage.StateCounts, err)
	}

	// Two days later the Godel pen is still unvisited: the sweep carries it to today as delayed,
	// keeping the day it was owed; the completed one is untouched.
	later := istInstant("2026-09-09", 0).Add(5 * time.Minute)
	sweep, err := repo.SweepRollForward(ctx, pvTenant, later, 100, 10)
	if err != nil || sweep.RolledForward != 1 {
		t.Fatalf("sweep = %+v err %v", sweep, err)
	}
	godelLate, err := repo.GetTask(ctx, pvTenant, godel.TaskID)
	if err != nil {
		t.Fatalf("get godel: %v", err)
	}
	if godelLate.WorkState != domain.WorkStateDelayed || godelLate.DueDate != "2026-09-09" || godelLate.PlannedDate != today || godelLate.DelayedSince == nil || *godelLate.DelayedSince != today || godelLate.RolledFwd != 1 {
		t.Fatalf("rolled task = %+v", godelLate)
	}
	if got := domain.StateChip(godelLate, "2026-09-09"); got == "Due today" {
		t.Fatalf("a rolled visit must read as delayed, got %q", got)
	}

	// Keyset paging: page size 1 walks both rows without repeating or skipping.
	first, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvDinakar, States: []string{domain.WorkStateScheduled, domain.WorkStateDelayed, domain.WorkStateCompleted}, Limit: 1})
	if err != nil || len(first.Rows) != 1 || first.NextCursor == "" {
		t.Fatalf("page 1 = %+v err %v", first, err)
	}
	second, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvDinakar, States: []string{domain.WorkStateScheduled, domain.WorkStateDelayed, domain.WorkStateCompleted}, Limit: 1, Cursor: first.NextCursor})
	if err != nil || len(second.Rows) != 1 || second.NextCursor != "" || second.Rows[0].TaskID == first.Rows[0].TaskID {
		t.Fatalf("page 2 = %+v err %v", second, err)
	}
}

func TestPenVisitMaterializeCatchupPreservesOriginalOwedDate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedPenVisitFixture(t, ctx, pool)

	const source = "2026-09-05"
	const today = "2026-09-07"
	now := istInstant(today, 0).Add(5 * time.Minute)
	repo := NewRepository(pool, 10*time.Second).WithClock(func() time.Time { return now })
	seedWork(t, ctx, pool, "old-castro-vacc", pvParkCBE, pvShedCastro, "3", "vaccination", "vaccination_proof", istInstant(source, 10))

	result, digests, err := repo.Materialize(ctx, pvTenant, source, today, now)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if result.Created != 1 || len(digests) != 1 || len(digests[0].Tasks) != 1 {
		t.Fatalf("materialize result/digest = %+v %+v", result, digests)
	}
	task := digests[0].Tasks[0]
	if task.PlannedDate != "2026-09-06" || task.DueDate != today || task.WorkState != domain.WorkStateDelayed || task.RolledFwd != 1 {
		t.Fatalf("catch-up task lost delayed semantics: %+v", task)
	}
	if task.DelayedSince == nil || *task.DelayedSince != "2026-09-06" {
		t.Fatalf("delayed_since = %v", task.DelayedSince)
	}
}
