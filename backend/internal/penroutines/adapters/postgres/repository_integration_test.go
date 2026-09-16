package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	prTenant     = "00000000-0000-4000-8000-0000000f0001"
	prParkCBE    = "00000000-0000-4000-8000-0000000f0101"
	prParkCPT    = "00000000-0000-4000-8000-0000000f0102"
	prShedCastro = "00000000-0000-4000-8000-0000000f0201" // partitioned: pens 1 and 2
	prShedYash   = "00000000-0000-4000-8000-0000000f0202" // undivided, EMPTY
	prShedGodel  = "00000000-0000-4000-8000-0000000f0203" // partitioned: Part 3
	prShedCPT    = "00000000-0000-4000-8000-0000000f0204"
	prHead       = "00000000-0000-4000-8000-0000000f0301" // ticked Do on Routines, park scope CBE
	prSecond     = "00000000-0000-4000-8000-0000000f0302" // no /people rows; park_head grant on CBE
	prOperator   = "00000000-0000-4000-8000-0000000f0303" // no /people rows; operator grant
	prVerifier   = "00000000-0000-4000-8000-0000000f0304"
	prMemberHead = "00000000-0000-4000-8000-0000000f0401"
	prGoatA      = "00000000-0000-4000-8000-0000000f0501"
	prGoatB      = "00000000-0000-4000-8000-0000000f0502"
	prGoatC      = "00000000-0000-4000-8000-0000000f0503"
	prPhoto      = "00000000-0000-4000-8000-0000000f0601"
	prVideo      = "00000000-0000-4000-8000-0000000f0602"
)

// assertEnvelopesValid validates every pen_routine.* outbox envelope against the SAME schema
// the relay validates against before publish. A rejected envelope is a SILENT drop, so the
// shape is pinned here.
func assertEnvelopesValid(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	schema := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json")
	validator, err := outboxapp.NewEnvelopeValidator(schema)
	if err != nil {
		t.Fatalf("envelope validator: %v", err)
	}
	rows, err := pool.Query(ctx, `SELECT event_type, payload FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type LIKE 'pen_routine.%'`, prTenant)
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var eventType string
		var payload []byte
		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatal(err)
		}
		if err := validator.Validate(payload); err != nil {
			t.Fatalf("%s envelope rejected by the relay schema: %v\n%s", eventType, err, payload)
		}
		counts[eventType]++
	}
	return counts
}

// seedRoutineFixture: one tenant, two parks, a partitioned shed with two pens (Castro 1 with a
// live goat, Castro 2 with a live goat), an EMPTY undivided shed (Yashoda), a partitioned shed
// with one pen (Godel 1 - Part 3, with a live goat), a CPT shed, the people, and two finished
// in-app-camera proofs (a photo and a video).
func seedRoutineFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Pen Routine Test', 'active') ON CONFLICT DO NOTHING`, prTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'Coimbatore', 'active'), ($3::uuid, $1::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, prTenant, prParkCBE, prParkCPT)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($2::uuid, $1::uuid, 'shed', 'S-CASTRO', 'Castro', 'active', $5::uuid, 1),
       ($3::uuid, $1::uuid, 'shed', 'S-YASH', 'Yashoda', 'active', $5::uuid, 2),
       ($4::uuid, $1::uuid, 'shed', 'S-GODEL', 'Godel 1', 'active', $5::uuid, 3),
       ($6::uuid, $1::uuid, 'shed', 'S-CPT', 'Gandhi', 'active', $7::uuid, 1)
ON CONFLICT (location_id) DO NOTHING`, prTenant, prShedCastro, prShedYash, prShedGodel, prParkCBE, prShedCPT, prParkCPT)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
VALUES ($1::uuid, $2::uuid, '1', '1', 'manual', 1), ($1::uuid, $2::uuid, '2', '2', 'manual', 2),
       ($1::uuid, $3::uuid, 'Part 3', '3', 'manual', 1)
ON CONFLICT DO NOTHING`, prTenant, prShedCastro, prShedGodel)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Pen Routine Custodian', 'active') ON CONFLICT DO NOTHING`, prTenant)
	for _, g := range []struct{ id, shed, partition string }{{prGoatA, prShedCastro, "1"}, {prGoatB, prShedCastro, "2"}, {prGoatC, prShedGodel, "Part 3"}} {
		exec(`INSERT INTO goats (goat_id, tenant_id, species, breed, sex, lifecycle_status, age_band, custodian_party_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'goat', 'Boer', 'female', 'alive', 'adult', $2::uuid, $3::uuid, $4::uuid)`, g.id, prTenant, prParkCBE, g.shed)
		exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label) VALUES ($1::uuid, $2::uuid, $3::uuid, 'seed', $4)`, prTenant, g.id, g.shed, g.partition)
	}
	exec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_location_id)
VALUES ($6::uuid, $1::uuid, $2::uuid, 'HEAD', 'Park Head', 'active', $7::uuid),
       (gen_random_uuid(), $1::uuid, $3::uuid, 'SEC', 'Second Person', 'active', $7::uuid),
       (gen_random_uuid(), $1::uuid, $4::uuid, 'OPR', 'An Operator', 'active', $7::uuid),
       (gen_random_uuid(), $1::uuid, $5::uuid, 'VER', 'The Verifier', 'active', NULL)`, prTenant, prHead, prSecond, prOperator, prVerifier, prMemberHead, prParkCBE)
	// The head is decided by /people rows: a Do tick on Routines and a park scope of CBE.
	exec(`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode) VALUES ($1::uuid, $2::uuid, 'parks')`, prTenant, prMemberHead)
	exec(`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, prTenant, prMemberHead, prParkCBE)
	exec(`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities) VALUES ($1::uuid, $2::uuid, 'mobile', 'pen_routines', ARRAY['view','do'])`, prTenant, prMemberHead)
	// The second person and the operator have NO /people rows: the role grant decides.
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'park', $4::uuid, 'active', now() - interval '1 day'),
       ($1::uuid, $3::uuid, 'operator', 'park', $4::uuid, 'active', now() - interval '1 day')`, prTenant, prSecond, prOperator, prParkCBE)
	for _, p := range []struct{ id, mime, kind string }{{prPhoto, "image/jpeg", "photo"}, {prVideo, "video/mp4", "video"}} {
		exec(`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, size_bytes, upload_state, scope_type, scope_id, subject_type, proof_type, uploaded_by, metadata)
VALUES ($1::uuid, $2::uuid, 'local', 'routines/' || $1::text, $3, 4096, 'completed', 'task', $1::uuid, 'other', $4, $5::uuid, '{"capture_source":"in_app_camera"}'::jsonb)`, p.id, prTenant, p.mime, p.kind, prHead)
	}
}

// seedWork raises one verification item -- what a PC Care submit leaves behind -- for a pen.
func seedWork(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key, parkID, shedID, partition, category string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, partition_label, captured_at, created_at, idempotency_key)
VALUES (gen_random_uuid(), $1::uuid, 'preventive_care', 'pc_care', $5, 'pc_care', gen_random_uuid(), gen_random_uuid(), 'pc_care_task', gen_random_uuid(), '["proof"]'::jsonb, 'pending', $2::uuid, $3::uuid, NULLIF($4, ''), $6::timestamptz, $6::timestamptz, $7)`,
		prTenant, parkID, shedID, partition, category, at, "pen-routine-test:"+key); err != nil {
		t.Fatalf("seed work %s: %v", key, err)
	}
}

func istInstant(date string, hour int) time.Time {
	loc, _ := time.LoadLocation("Asia/Kolkata")
	d, _ := time.ParseInLocation("2006-01-02", date, loc)
	return d.Add(time.Duration(hour) * time.Hour)
}

func evidenceOneQuestion(presence string, photos int) domain.Evidence {
	return domain.NormalizeEvidence(domain.Evidence{
		Questions: []domain.Question{{ID: "cleaned", Kind: domain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true}},
		Photo:     domain.ProofRule{Min: photos, Max: photos},
		Presence:  presence,
	})
}

func rawAnswers(t *testing.T, m map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for k, v := range m {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[k] = b
	}
	return out
}

// TestPenRoutineLifecycleOneToManyParkScopePaginationStatusMatrixPostgresPaths drives the
// production path end to end on a real database. The name carries the adversarial shapes it
// pins: OneToMany (one routine -> many pens -> one task each; two work items in one pen -> one
// after_work task), ParkScope (a routine in CPT with nobody assigned raises nothing and is
// named; an all_pens routine sees only its own park's pens), Pagination (a keyset page
// boundary on the assignee's list), StatusMatrix (the web Today summary counts every
// (work_state, status) bucket the gate and the clock can reach).
func TestPenRoutineLifecycleOneToManyParkScopePaginationStatusMatrixPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)

	const monday = "2026-09-14"
	const yesterday = "2026-09-15"
	const today = "2026-09-16" // a Wednesday
	now := istInstant(today, 7).Add(5 * time.Minute)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	head := domain.Actor{UserID: prHead}
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: prTenant, ActorID: prHead, IdempotencyKey: key, TraceID: "trace-" + key}
	}

	// --- eligibility: the ticked head and the role-granted park head, never the operator ---
	people, err := repo.EligiblePeople(ctx, prTenant, prParkCBE)
	if err != nil {
		t.Fatalf("eligible people: %v", err)
	}
	eligible := map[string]bool{}
	for _, p := range people {
		eligible[p.UserID] = true
	}
	if !eligible[prHead] || !eligible[prSecond] || eligible[prOperator] || eligible[prVerifier] {
		t.Fatalf("eligible = %+v", people)
	}
	// The pen catalog lists every ACTIVE pen of the park, empty ones included, flagged.
	pens, err := repo.CatalogPens(ctx, prTenant, prParkCBE)
	if err != nil {
		t.Fatalf("catalog pens: %v", err)
	}
	labels := map[string]bool{}
	for _, p := range pens {
		labels[p.Label] = p.Occupied
	}
	if len(pens) != 4 || !labels["Castro 1"] || !labels["Castro 2"] || labels["Yashoda"] || !labels["Godel 1 - Part 3"] {
		t.Fatalf("catalog pens = %+v", pens)
	}

	// --- authoring: four routines. "daily" is the Mon + Wed pen cleaning (the fixture's business
	// dates are Mon 14, Tue 15 and Wed 16, so it raises on the catch-up day and today and NOT
	// on the after_work day), "weekly" the Monday-only count, "afterWork" the day-after check,
	// and "orphan" a CPT routine with nobody assigned. ---
	daily, err := repo.CreateRoutine(ctx, write("create-daily"), domain.Definition{
		ParkID: prParkCBE, Name: "Pen cleaning", Instruction: "Sweep and check the water.", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1, 3}, NotifyTime: "07:00", ReviewKind: domain.ReviewVerifier,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 1), AssigneeIDs: []string{prHead, prSecond},
	})
	if err != nil {
		t.Fatalf("create daily: %v", err)
	}
	if daily.CurrentVersion != 1 || daily.RowVersion != 1 || len(daily.Assignees) != 2 || daily.Assignees[0].DisplayName != "Park Head" || daily.ParkName != "Coimbatore" {
		t.Fatalf("daily = %+v", daily)
	}
	// An exact replay of the create returns the same routine, no second row.
	again, err := repo.CreateRoutine(ctx, write("create-daily"), domain.Definition{
		ParkID: prParkCBE, Name: "Pen cleaning", Instruction: "Sweep and check the water.", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1, 3}, NotifyTime: "07:00", ReviewKind: domain.ReviewVerifier,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 1), AssigneeIDs: []string{prHead, prSecond},
	})
	if err != nil || again.RoutineID != daily.RoutineID {
		t.Fatalf("create replay: %v / %s vs %s", err, again.RoutineID, daily.RoutineID)
	}
	// The same name again in the same park is refused.
	if _, err := repo.CreateRoutine(ctx, write("create-daily-dup"), domain.Definition{
		ParkID: prParkCBE, Name: "pen cleaning", ScopeKind: domain.ScopeAllPens, CadenceKind: domain.CadenceDaily, NotifyTime: "07:00",
		ReviewKind: domain.ReviewNone, Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeIDs: []string{prHead},
	}); !errors.Is(err, ports.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	weekly, err := repo.CreateRoutine(ctx, write("create-weekly"), domain.Definition{
		ParkID: prParkCBE, Name: "Monday count", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCastro, Partition: "2"}, {ShedID: prShedYash}},
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1}, NotifyTime: "08:30", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeIDs: []string{prHead},
	})
	if err != nil {
		t.Fatalf("create weekly: %v", err)
	}
	if len(weekly.Pens) != 2 || weekly.Pens[0].Label != "Castro 2" || weekly.Pens[1].Label != "Yashoda" {
		t.Fatalf("weekly pens = %+v", weekly.Pens)
	}
	afterWork, err := repo.CreateRoutine(ctx, write("create-after"), domain.Definition{
		ParkID: prParkCBE, Name: "After deworming", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceAfterWork, AfterWorkKinds: []string{domain.WorkDeworming, domain.WorkHoofTrimming}, DueOffsetDays: 1, NotifyTime: "07:00",
		ReviewKind: domain.ReviewNone, Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeIDs: []string{prHead},
	})
	if err != nil {
		t.Fatalf("create after_work: %v", err)
	}
	orphan, err := repo.CreateRoutine(ctx, write("create-orphan"), domain.Definition{
		ParkID: prParkCPT, Name: "CPT check", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCPT}},
		CadenceKind: domain.CadenceDaily, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeIDs: nil,
	})
	if err != nil {
		t.Fatalf("create orphan: %v", err)
	}

	// --- materialize today (Wednesday): the pen-cleaning routine raises one task per OCCUPIED
	// pen (Yashoda is empty and skipped), the Monday-only weekly does not raise, the after_work
	// routine has no work yesterday yet, the CPT routine has nobody and is named.
	result, err := repo.Materialize(ctx, prTenant, today, today, now)
	if err != nil {
		t.Fatalf("materialize today: %v", err)
	}
	if result.Created != 3 || result.Widened != 0 {
		t.Fatalf("materialize today = %+v, want 3 created (Castro 1, Castro 2, Godel 1 - Part 3)", result)
	}
	if len(result.RoutinesWithoutAssignee) != 1 || result.RoutinesWithoutAssignee[0].RoutineID != orphan.RoutineID || result.PensSkipped != 1 {
		t.Fatalf("orphan routine must be named, got %+v", result)
	}
	// A replay tick inserts nothing and widens nothing.
	replay, err := repo.Materialize(ctx, prTenant, today, today, now)
	if err != nil || replay.Created != 0 || replay.Widened != 0 {
		t.Fatalf("replay = %+v / %v", replay, err)
	}
	// Monday's occurrences, materialized late (catch-up): the weekly's two pens AND the
	// pen-cleaning's three (it raises on Mondays too), all born delayed and due today.
	late, err := repo.Materialize(ctx, prTenant, monday, today, now)
	if err != nil || late.Created != 5 {
		t.Fatalf("monday catch-up = %+v / %v", late, err)
	}
	// Yesterday: Castro 2 dewormed AND hoof-trimmed (two items, one pen -> ONE task carrying
	// both kinds), Godel 1 - Part 3 vaccinated (not a kind of this routine -> nothing), an
	// unmapped inventory item in Castro 1 (nothing).
	seedWork(t, ctx, pool, "castro2-deworm", prParkCBE, prShedCastro, "2", "pc_deworming", istInstant(yesterday, 10))
	seedWork(t, ctx, pool, "castro2-hoof", prParkCBE, prShedCastro, "2", "pc_hoof_trimming", istInstant(yesterday, 15))
	seedWork(t, ctx, pool, "godel-vacc", prParkCBE, prShedGodel, "Part 3", "vaccination_proof", istInstant(yesterday, 11))
	seedWork(t, ctx, pool, "castro1-inventory", prParkCBE, prShedCastro, "1", "inventory_vaccine", istInstant(yesterday, 12))
	aw, err := repo.Materialize(ctx, prTenant, yesterday, today, now)
	if err != nil || aw.Created != 1 {
		t.Fatalf("after_work materialize = %+v / %v", aw, err)
	}

	// --- the head's list: 9 open tasks (4 scheduled, 5 delayed), keyset-paged, counts whole-list ---
	first, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, Limit: 5})
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(first.Rows) != 5 || first.NextCursor == "" || first.StateCounts[domain.WorkStateScheduled] != 4 || first.StateCounts[domain.WorkStateDelayed] != 5 {
		t.Fatalf("page 1 rows=%d next=%q counts=%v", len(first.Rows), first.NextCursor, first.StateCounts)
	}
	second, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, Limit: 5, Cursor: first.NextCursor})
	if err != nil || len(second.Rows) != 4 || second.NextCursor != "" {
		t.Fatalf("page 2 rows=%d next=%q err=%v", len(second.Rows), second.NextCursor, err)
	}
	all := append(first.Rows, second.Rows...)
	var castro2Daily, castro2After, castro2Weekly, yashWeekly domain.Task
	for _, row := range all {
		switch {
		case row.RoutineID == daily.RoutineID && row.PenLabel == "Castro 2" && row.PlannedDate == today:
			castro2Daily = row
		case row.RoutineID == afterWork.RoutineID:
			castro2After = row
		case row.RoutineID == weekly.RoutineID && row.PenLabel == "Castro 2":
			castro2Weekly = row
		case row.RoutineID == weekly.RoutineID && row.PenLabel == "Yashoda":
			yashWeekly = row
		}
	}
	if castro2Daily.TaskID == "" || castro2Daily.DueDate != today || castro2Daily.WorkState != domain.WorkStateScheduled || castro2Daily.CadenceLine != "Every Mon, Wed" || len(castro2Daily.AssigneeIDs) != 2 || castro2Daily.AssigneeNames[0] != "Park Head" {
		t.Fatalf("castro2 daily = %+v", castro2Daily)
	}
	if castro2After.TaskID == "" || len(castro2After.TriggerKinds) != 2 || castro2After.PenLabel != "Castro 2" || castro2After.SourceDate != yesterday || castro2After.PlannedDate != today || domain.ReasonLine(castro2After, today) != "After deworming, hoof trimming yesterday" {
		t.Fatalf("after_work task = %+v (reason %q)", castro2After, domain.ReasonLine(castro2After, today))
	}
	if castro2Weekly.WorkState != domain.WorkStateDelayed || castro2Weekly.PlannedDate != monday || castro2Weekly.DueDate != today || castro2Weekly.DelayedSince == nil || *castro2Weekly.DelayedSince != monday || castro2Weekly.RolledFwd != 2 {
		t.Fatalf("late weekly task = %+v", castro2Weekly)
	}
	if yashWeekly.PenLabel != "Yashoda" || yashWeekly.Partition != "" {
		t.Fatalf("undivided pen label = %+v", yashWeekly)
	}
	// The operator is on no routine: nothing listed, nothing owed.
	if n, err := repo.OpenCount(ctx, prTenant, prOperator); err != nil || n != 0 {
		t.Fatalf("operator open count = %d / %v", n, err)
	}
	if n, err := repo.OpenCount(ctx, prTenant, prHead); err != nil || n != 9 {
		t.Fatalf("head open count = %d / %v", n, err)
	}

	// --- presence gate on the daily task: submit before check-in is refused ---
	answers := rawAnswers(t, map[string]any{"cleaned": "yes"})
	proofs := []domain.ProofItem{{Ref: prPhoto, Kind: domain.ProofKindPhoto}}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, RowVersion: castro2Daily.RowVersion, IdempotencyKey: "submit-early"}); !errors.Is(err, domain.ErrPresenceMissing) {
		t.Fatalf("submit before check-in err = %v, want ErrPresenceMissing", err)
	}
	// A wrong answer shape is refused before anything is written.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "maybe"}), Proofs: proofs, IdempotencyKey: "submit-bad"}); !errors.Is(err, domain.ErrAnswerInvalid) {
		t.Fatalf("bad answer err = %v", err)
	}
	// Check in.
	entered, err := repo.RecordPresence(ctx, ports.PresenceParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, EventType: domain.PresenceEnter, CapturedAt: now, RowVersion: castro2Daily.RowVersion, IdempotencyKey: "enter-1"})
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	if !entered.InPen(head) || entered.RowVersion != castro2Daily.RowVersion+1 || entered.EnteredBy != prHead {
		t.Fatalf("entered = %+v", entered)
	}
	// A second enter while in the pen is refused; a replay of the same punch returns the row.
	if _, err := repo.RecordPresence(ctx, ports.PresenceParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, EventType: domain.PresenceEnter, CapturedAt: now, IdempotencyKey: "enter-2"}); !errors.Is(err, domain.ErrPresenceState) {
		t.Fatalf("double enter err = %v", err)
	}
	if again, err := repo.RecordPresence(ctx, ports.PresenceParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, EventType: domain.PresenceEnter, CapturedAt: now, RowVersion: castro2Daily.RowVersion, IdempotencyKey: "enter-1"}); err != nil || again.RowVersion != entered.RowVersion {
		t.Fatalf("enter replay = %+v / %v", again, err)
	}
	// A wrong photo count is refused.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: nil, RowVersion: entered.RowVersion, IdempotencyKey: "submit-noproof"}); !errors.Is(err, domain.ErrProofCount) {
		t.Fatalf("no proof err = %v", err)
	}
	// Someone who is not an assignee reads as not theirs.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: domain.Actor{UserID: prOperator}, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, IdempotencyKey: "submit-stranger"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("stranger err = %v", err)
	}
	// Submit: verifier review -> pending, work state untouched, left_at stamped, the leave row written.
	submitted, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, RowVersion: entered.RowVersion, CapturedAt: now.Add(10 * time.Minute), IdempotencyKey: "submit-1"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if submitted.Status != domain.StatusPendingVerification || submitted.WorkState != domain.WorkStateScheduled || submitted.LeftAt == nil || submitted.SubmittedBy != prHead || submitted.Answers["cleaned"] != "yes" || len(submitted.Proofs) != 1 {
		t.Fatalf("submitted = %+v", submitted)
	}
	var punches int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pen_routine_task_presence WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, prTenant, castro2Daily.TaskID).Scan(&punches); err != nil || punches != 2 {
		t.Fatalf("presence rows = %d / %v, want enter + leave", punches, err)
	}
	// Exact replay returns the same row; a same-key different-payload replay is refused.
	if again, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, RowVersion: entered.RowVersion, IdempotencyKey: "submit-1"}); err != nil || again.RowVersion != submitted.RowVersion {
		t.Fatalf("submit replay = %+v / %v", again, err)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "no"}), Proofs: proofs, IdempotencyKey: "submit-1"}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("payload conflict err = %v", err)
	}
	// The gate holds while pending.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, IdempotencyKey: "submit-again"}); !errors.Is(err, domain.ErrInReview) {
		t.Fatalf("in review err = %v", err)
	}
	// Reject -> rework, then approve after a redo -> completed on BOTH dimensions.
	bounced, err := repo.BounceForRework(ctx, ports.VerdictParams{TenantID: prTenant, TaskID: castro2Daily.TaskID, VerifiedBy: prVerifier, Reason: "Water trough not visible", TraceID: "v1"})
	if err != nil || !bounced.Applied || bounced.Task.Status != domain.StatusRework || bounced.Task.ReworkReason != "Water trough not visible" {
		t.Fatalf("bounce = %+v / %v", bounced, err)
	}
	// Redo: still checked-out, presence required again.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, RowVersion: bounced.Task.RowVersion, IdempotencyKey: "submit-redo-early"}); !errors.Is(err, domain.ErrPresenceMissing) {
		t.Fatalf("redo before re-entering err = %v", err)
	}
	reentered, err := repo.RecordPresence(ctx, ports.PresenceParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, EventType: domain.PresenceEnter, CapturedAt: now.Add(time.Hour), IdempotencyKey: "enter-3"})
	if err != nil {
		t.Fatalf("re-enter: %v", err)
	}
	redone, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2Daily.TaskID, Answers: answers, Proofs: proofs, RowVersion: reentered.RowVersion, IdempotencyKey: "submit-redo"})
	if err != nil || redone.Status != domain.StatusPendingVerification || redone.ReworkReason != "" {
		t.Fatalf("redo = %+v / %v", redone, err)
	}
	approved, err := repo.ApplyVerified(ctx, ports.VerdictParams{TenantID: prTenant, TaskID: castro2Daily.TaskID, VerifiedBy: prVerifier, TraceID: "v2"})
	if err != nil || !approved.Applied || approved.Task.Status != domain.StatusCompleted || approved.Task.WorkState != domain.WorkStateCompleted || approved.Task.VerifiedBy != prVerifier {
		t.Fatalf("approve = %+v / %v", approved, err)
	}
	if again, err := repo.ApplyVerified(ctx, ports.VerdictParams{TenantID: prTenant, TaskID: castro2Daily.TaskID, VerifiedBy: prVerifier}); err != nil || again.Applied {
		t.Fatalf("approve replay must apply nothing: %+v / %v", again, err)
	}

	// --- review none: the after_work task completes on submit, no presence asked ---
	done, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2After.TaskID, Answers: answers, RowVersion: castro2After.RowVersion, IdempotencyKey: "submit-after"})
	if err != nil || done.Status != domain.StatusCompleted || done.WorkState != domain.WorkStateCompleted || done.LeftAt != nil {
		t.Fatalf("review none submit = %+v / %v", done, err)
	}

	// --- version pinning: an edit writes version 2; the open task keeps version 1's form ---
	edited := daily
	edited.Instruction = "Sweep, check the water, count the feed left."
	edited.Evidence = domain.NormalizeEvidence(domain.Evidence{
		Questions: []domain.Question{
			{ID: "cleaned", Kind: domain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true},
			{ID: "feed_left", Kind: domain.QuestionNumber, Title: "Feed left (kg)", Required: false},
		},
		Photo: domain.ProofRule{Min: 1, Max: 2}, Presence: domain.PresenceRequired,
	})
	edited.AssigneeIDs = []string{prHead}
	v2, err := repo.UpdateRoutine(ctx, write("update-daily"), edited)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v2.CurrentVersion != 2 || v2.RowVersion != 2 || len(v2.Evidence.Questions) != 2 || len(v2.AssigneeIDs) != 1 {
		t.Fatalf("v2 = %+v", v2)
	}
	// A stale row version is refused.
	if _, err := repo.UpdateRoutine(ctx, write("update-stale"), edited); !errors.Is(err, ports.ErrRoutineVersionConflict) {
		t.Fatalf("stale update err = %v", err)
	}
	var castro1Daily domain.Task
	for _, row := range all {
		if row.RoutineID == daily.RoutineID && row.PenLabel == "Castro 1" && row.PlannedDate == today {
			castro1Daily = row
		}
	}
	pinned, err := repo.GetTask(ctx, prTenant, castro1Daily.TaskID)
	if err != nil || pinned.RoutineVersion != 1 || len(pinned.Evidence.Questions) != 1 || pinned.Instruction != "Sweep and check the water." {
		t.Fatalf("open task must keep its pinned form: %+v / %v", pinned, err)
	}
	// The second person was unassigned by the edit: their list is empty and they may not submit.
	if page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prSecond}); err != nil || len(page.Rows) != 0 {
		t.Fatalf("unassigned list = %d / %v", len(page.Rows), err)
	}
	// --- StatusMatrix: the Today table's summary counts every bucket over the same predicate ---
	// Castro 2 daily (today): completed (verified); Castro 2 after_work: completed (review none);
	// Castro 1 daily + Godel daily (today): scheduled/open (due); Monday's three daily and two
	// weekly: delayed/open.
	park, err := repo.ListForPark(ctx, ports.ParkListParams{TenantID: prTenant, ParkID: prParkCBE, BusinessDate: today, Limit: 3})
	if err != nil {
		t.Fatalf("park list: %v", err)
	}
	if len(park.Rows) != 3 || park.NextCursor == "" {
		t.Fatalf("park page = %d rows, next %q", len(park.Rows), park.NextCursor)
	}
	if park.Summary != (ports.ParkSummary{Due: 2, Delayed: 5, InReview: 0, SentBack: 0, Done: 2}) {
		t.Fatalf("park summary = %+v", park.Summary)
	}
	rest, err := repo.ListForPark(ctx, ports.ParkListParams{TenantID: prTenant, ParkID: prParkCBE, BusinessDate: today, Limit: 6, Cursor: park.NextCursor})
	if err != nil || len(rest.Rows) != 6 || rest.NextCursor != "" {
		t.Fatalf("park page 2 = %d rows / %v", len(rest.Rows), err)
	}
	// Filtered to one routine, the summary narrows with the rows.
	one, err := repo.ListForPark(ctx, ports.ParkListParams{TenantID: prTenant, ParkID: prParkCBE, BusinessDate: today, RoutineID: weekly.RoutineID})
	if err != nil || len(one.Rows) != 2 || one.Summary != (ports.ParkSummary{Delayed: 2}) {
		t.Fatalf("routine-filtered park list = %d rows, summary %+v / %v", len(one.Rows), one.Summary, err)
	}
	// The other park sees none of it.
	if other, err := repo.ListForPark(ctx, ports.ParkListParams{TenantID: prTenant, ParkID: prParkCPT, BusinessDate: today}); err != nil || len(other.Rows) != 0 || other.Summary != (ports.ParkSummary{}) {
		t.Fatalf("CPT park list = %+v / %v", other, err)
	}
	// The routines table: open_today and delayed per routine.
	rows, err := repo.ListRoutines(ctx, ports.RoutineListParams{TenantID: prTenant, ParkID: prParkCBE, Today: today})
	if err != nil {
		t.Fatalf("list routines: %v", err)
	}
	byName := map[string]ports.RoutineListRow{}
	for _, row := range rows {
		byName[row.Definition.Name] = row
	}
	if len(rows) != 3 || byName["Pen cleaning"].OpenToday != 5 || byName["Pen cleaning"].Delayed != 3 || byName["Monday count"].OpenToday != 2 || byName["Monday count"].Delayed != 2 || byName["After deworming"].OpenToday != 0 {
		t.Fatalf("routine rows = %+v", rows)
	}

	// --- digests for today: per routine, only routines with open work ---
	digests, err := repo.DueDigests(ctx, prTenant, today)
	if err != nil {
		t.Fatalf("digests: %v", err)
	}
	if len(digests) != 2 {
		t.Fatalf("digests = %+v, want the daily and the weekly", digests)
	}
	for _, d := range digests {
		switch d.RoutineID {
		case daily.RoutineID:
			if len(d.Tasks) != 5 || d.NotifyTime != "07:00" || d.ParkName != "Coimbatore" || len(d.AssigneeIDs) != 1 {
				t.Fatalf("daily digest = %+v", d)
			}
		case weekly.RoutineID:
			if len(d.Tasks) != 2 || d.NotifyTime != "08:30" {
				t.Fatalf("weekly digest = %+v", d)
			}
		default:
			t.Fatalf("unexpected digest %+v", d)
		}
	}

	// Pausing stops the next occurrence; open tasks stay.
	paused, err := repo.SetRoutineStatus(ctx, write("pause"), daily.RoutineID, domain.StatusPaused, v2.RowVersion)
	if err != nil || paused.Status != domain.StatusPaused {
		t.Fatalf("pause = %+v / %v", paused, err)
	}
	tomorrow := "2026-09-17"
	next, err := repo.Materialize(ctx, prTenant, tomorrow, tomorrow, now.Add(24*time.Hour))
	if err != nil || next.Created != 0 {
		t.Fatalf("paused routine must raise nothing: %+v / %v", next, err)
	}

	// --- roll-forward: the day after, every open task due today slides to 'delayed' ---
	sweep, err := repo.SweepRollForward(ctx, prTenant, istInstant(tomorrow, 0).Add(5*time.Minute), 200, 50)
	if err != nil || sweep.RolledForward != 7 {
		t.Fatalf("sweep = %+v / %v, want 7 (two daily today + Monday's three daily + two weekly still open)", sweep, err)
	}
	rolled, err := repo.GetTask(ctx, prTenant, castro1Daily.TaskID)
	if err != nil || rolled.WorkState != domain.WorkStateDelayed || rolled.DueDate != tomorrow || rolled.DelayedSince == nil || *rolled.DelayedSince != today {
		t.Fatalf("rolled = %+v / %v", rolled, err)
	}
	if domain.StateChip(rolled, tomorrow) != "Delayed since 16/09/2026" {
		t.Fatalf("chip = %q", domain.StateChip(rolled, tomorrow))
	}

	// --- every emitted envelope passes the relay's schema ---
	counts := assertEnvelopesValid(t, ctx, pool)
	if counts[EventRoutineCreated] != 9 || counts[EventRoutineSubmitted] != 3 || counts[EventRoutineVerified] != 1 {
		t.Fatalf("outbox counts = %v", counts)
	}
}
