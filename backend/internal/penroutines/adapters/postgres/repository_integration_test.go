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
	prHead       = "00000000-0000-4000-8000-0000000f0301" // park_head at TENANT scope, HRMS home park CBE
	prSecond     = "00000000-0000-4000-8000-0000000f0302" // park_head at PARK scope on CBE
	prOperator   = "00000000-0000-4000-8000-0000000f0303" // operator grant on CBE -- not an assignable role
	prVerifier   = "00000000-0000-4000-8000-0000000f0304"
	prCPTHead    = "00000000-0000-4000-8000-0000000f0305" // park_head at TENANT scope, HRMS home park CPT
	prPCDirector = "00000000-0000-4000-8000-0000000f0306" // pc_director at TENANT scope, no home park
	prCXO        = "00000000-0000-4000-8000-0000000f0307" // ceo_internal at TENANT scope
	prExpired    = "00000000-0000-4000-8000-0000000f0308" // park_head on CBE whose grant has EXPIRED"
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
       (gen_random_uuid(), $1::uuid, $5::uuid, 'VER', 'The Verifier', 'active', NULL),
       (gen_random_uuid(), $1::uuid, $8::uuid, 'CPTH', 'CPT Park Head', 'active', $9::uuid),
       (gen_random_uuid(), $1::uuid, $10::uuid, 'PCD', 'PC Director', 'active', NULL),
       (gen_random_uuid(), $1::uuid, $11::uuid, 'CXO', 'The CXO', 'active', NULL),
       (gen_random_uuid(), $1::uuid, $12::uuid, 'EXP', 'Expired Head', 'active', $7::uuid)`,
		prTenant, prHead, prSecond, prOperator, prVerifier, prMemberHead, prParkCBE, prCPTHead, prParkCPT, prPCDirector, prCXO, prExpired)
	// The head also carries /people rows (a Do tick on Routines): who OWES a routine is decided by
	// the role grants below, never by these rows.
	exec(`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode) VALUES ($1::uuid, $2::uuid, 'parks')`, prTenant, prMemberHead)
	exec(`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id) VALUES ($1::uuid, $2::uuid, $3::uuid)`, prTenant, prMemberHead, prParkCBE)
	exec(`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities) VALUES ($1::uuid, $2::uuid, 'mobile', 'pen_routines', ARRAY['view','do'])`, prTenant, prMemberHead)
	// The role grants: the farm's two park heads hold park_head at TENANT scope (each covers only
	// his HRMS home park), a second CBE park head holds it at PARK scope, a PC director and a CXO
	// at tenant scope (both parks), an operator (never assignable) and an expired park head.
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, valid_to)
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $3::uuid, 'park_head', 'park', $5::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $4::uuid, 'operator', 'park', $5::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $6::uuid, 'park_head', 'tenant', $1::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $7::uuid, 'pc_director', 'tenant', $1::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $8::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day', NULL),
       ($1::uuid, $9::uuid, 'park_head', 'park', $5::uuid, 'active', now() - interval '2 days', now() - interval '1 day')`,
		prTenant, prHead, prSecond, prOperator, prParkCBE, prCPTHead, prPCDirector, prCXO, prExpired)
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

// evidenceOneQuestionWithProof: the one question wants its own single photo; nothing task-wide.
func evidenceOneQuestionWithProof() domain.Evidence {
	return domain.NormalizeEvidence(domain.Evidence{
		Questions: []domain.Question{{
			ID: "cleaned", Kind: domain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true,
			Proof: &domain.QuestionProof{Kind: domain.QuestionProofPhoto, Count: domain.QuestionProofSingle},
		}},
		Presence: domain.PresenceOff,
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

	// --- role holders per park: the ONE resolution the drawer, the lists and the kernel share ---
	holders := func(park string) map[string][]string {
		t.Helper()
		_, roles, err := repo.Catalog(ctx, prTenant, park)
		if err != nil {
			t.Fatalf("role holders %s: %v", park, err)
		}
		if len(roles) != len(domain.AssignableRoles) {
			t.Fatalf("every assignable role must be answered, got %+v", roles)
		}
		out := map[string][]string{}
		for _, r := range roles {
			for _, p := range r.People {
				out[r.Role] = append(out[r.Role], p.UserID)
			}
		}
		return out
	}
	cbe, cpt := holders(prParkCBE), holders(prParkCPT)
	if len(cbe[domain.RoleParkHead]) != 2 || cbe[domain.RoleParkHead][0] != prHead || cbe[domain.RoleParkHead][1] != prSecond {
		t.Fatalf("CBE park heads = %v (want the tenant-scoped head whose home is CBE and the park-scoped one; never the CPT head nor the expired grant)", cbe[domain.RoleParkHead])
	}
	if len(cpt[domain.RoleParkHead]) != 1 || cpt[domain.RoleParkHead][0] != prCPTHead {
		t.Fatalf("CPT park heads = %v", cpt[domain.RoleParkHead])
	}
	for _, park := range []map[string][]string{cbe, cpt} {
		if len(park[domain.RolePCDirector]) != 1 || park[domain.RolePCDirector][0] != prPCDirector || len(park[domain.RoleCXO]) != 1 || park[domain.RoleCXO][0] != prCXO || len(park[domain.RoleFeedDirector]) != 0 {
			t.Fatalf("tenant-scoped directors and the CXO cover both parks: %v", park)
		}
	}
	// The pen catalog lists every ACTIVE pen of the park, empty ones included, flagged.
	pens, _, err := repo.Catalog(ctx, prTenant, prParkCBE)
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
	// on the after_work day), "weekly" the Monday-only count, "afterWork" the day-after check --
	// all three for the Park Head role -- and "orphan" a CPT routine for a role nobody holds. ---
	const started = "2026-09-01"
	daily, err := repo.CreateRoutine(ctx, write("create-daily"), domain.Definition{
		ParkID: prParkCBE, Name: "Pen cleaning", Instruction: "Sweep and check the water.", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1, 3}, NotifyTime: "07:00", ReviewKind: domain.ReviewVerifier,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 1), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: started,
	})
	if err != nil {
		t.Fatalf("create daily: %v", err)
	}
	if daily.CurrentVersion != 1 || daily.RowVersion != 1 || len(daily.People) != 2 || daily.People[0].DisplayName != "Park Head" || daily.People[0].RoleKey != domain.RoleParkHead || daily.People[1].UserID != prSecond || daily.ParkName != "Coimbatore" || daily.StartDate != started || len(daily.AssigneeRoles) != 1 {
		t.Fatalf("daily = %+v", daily)
	}
	// An exact replay of the create returns the same routine, no second row.
	again, err := repo.CreateRoutine(ctx, write("create-daily"), domain.Definition{
		ParkID: prParkCBE, Name: "Pen cleaning", Instruction: "Sweep and check the water.", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1, 3}, NotifyTime: "07:00", ReviewKind: domain.ReviewVerifier,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 1), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: started,
	})
	if err != nil || again.RoutineID != daily.RoutineID {
		t.Fatalf("create replay: %v / %s vs %s", err, again.RoutineID, daily.RoutineID)
	}
	// The same name again in the same park is refused.
	if _, err := repo.CreateRoutine(ctx, write("create-daily-dup"), domain.Definition{
		ParkID: prParkCBE, Name: "pen cleaning", ScopeKind: domain.ScopeAllPens, CadenceKind: domain.CadenceDaily, NotifyTime: "07:00",
		ReviewKind: domain.ReviewNone, Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: started,
	}); !errors.Is(err, ports.ErrNameTaken) {
		t.Fatalf("duplicate name err = %v", err)
	}
	weekly, err := repo.CreateRoutine(ctx, write("create-weekly"), domain.Definition{
		ParkID: prParkCBE, Name: "Monday count", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCastro, Partition: "2"}, {ShedID: prShedYash}},
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1}, NotifyTime: "08:30", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: started,
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
		// The 2026-09-18 per-question proof: the question itself wants one photo; no task-wide
		// capture is asked for, so the two pools can be told apart on the round trip below.
		ReviewKind: domain.ReviewNone, Evidence: evidenceOneQuestionWithProof(), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: started,
	})
	if err != nil {
		t.Fatalf("create after_work: %v", err)
	}
	if got := afterWork.Evidence.Questions[0].Proof; got == nil || got.Kind != domain.QuestionProofPhoto || got.Count != domain.QuestionProofSingle {
		t.Fatalf("question proof did not round-trip through the evidence document: %+v", afterWork.Evidence.Questions[0])
	}
	orphan, err := repo.CreateRoutine(ctx, write("create-orphan"), domain.Definition{
		ParkID: prParkCPT, Name: "CPT check", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCPT}},
		CadenceKind: domain.CadenceDaily, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: []string{domain.RoleBreedingDirector}, StartDate: started,
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
	if len(result.RoutinesWithoutAssignee) != 1 || result.RoutinesWithoutAssignee[0].RoutineID != orphan.RoutineID || result.PensSkipped != 1 ||
		len(result.RoutinesWithoutAssignee[0].Roles) != 1 || result.RoutinesWithoutAssignee[0].Roles[0] != domain.RoleBreedingDirector {
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
	// Its question owes a photo of its own: a submit without it, or with the photo left
	// task-wide, is refused; a photo naming the question completes it and the question id
	// survives the proof_refs round trip.
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2After.TaskID, Answers: answers, RowVersion: castro2After.RowVersion, IdempotencyKey: "submit-after-noproof"}); !errors.Is(err, domain.ErrQuestionProofMissing) {
		t.Fatalf("question proof missing err = %v", err)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2After.TaskID, Answers: answers, Proofs: proofs, RowVersion: castro2After.RowVersion, IdempotencyKey: "submit-after-taskwide"}); !errors.Is(err, domain.ErrProofCount) {
		t.Fatalf("task-wide photo on a routine asking none err = %v", err)
	}
	questionProof := []domain.ProofItem{{Ref: prPhoto, Kind: domain.ProofKindPhoto, QuestionID: "cleaned"}}
	done, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: head, TaskID: castro2After.TaskID, Answers: answers, Proofs: questionProof, RowVersion: castro2After.RowVersion, IdempotencyKey: "submit-after"})
	if err != nil || done.Status != domain.StatusCompleted || done.WorkState != domain.WorkStateCompleted || done.LeftAt != nil {
		t.Fatalf("review none submit = %+v / %v", done, err)
	}
	if len(done.Proofs) != 1 || done.Proofs[0].QuestionID != "cleaned" {
		t.Fatalf("question id lost on the proof round trip: %+v", done.Proofs)
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
	edited.AssigneeRoles = []string{domain.RolePCDirector}
	v2, err := repo.UpdateRoutine(ctx, write("update-daily"), edited)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v2.CurrentVersion != 2 || v2.RowVersion != 2 || len(v2.Evidence.Questions) != 2 || len(v2.AssigneeRoles) != 1 || len(v2.People) != 1 || v2.People[0].UserID != prPCDirector {
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
	// The edit moved the routine from the Park Head role to the PC Director: a park head no
	// longer sees its open tasks (roles are read live), the director now does -- with no task
	// rewritten.
	if page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prSecond, Limit: 50}); err != nil {
		t.Fatalf("park head list after edit: %v", err)
	} else {
		for _, row := range page.Rows {
			if row.RoutineID == daily.RoutineID {
				t.Fatalf("a park head still lists the re-assigned routine: %+v", row)
			}
		}
	}
	if page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prPCDirector, Limit: 50}); err != nil || len(page.Rows) != 5 {
		t.Fatalf("PC director list after edit = %d / %v, want the daily routine's 5 open tasks", len(page.Rows), err)
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
	rows, _, err := repo.ListRoutinesAndParks(ctx, ports.RoutineListParams{TenantID: prTenant, ParkID: prParkCBE, Today: today})
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
			if len(d.Tasks) != 5 || d.NotifyTime != "07:00" || d.ParkName != "Coimbatore" || len(d.AssigneeIDs) != 1 || d.AssigneeIDs[0] != prPCDirector {
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

// TestPenRoutineRoleResolutionTwoParkHeadsDirectorCXOParkScopeEveryNDaysPostgresPaths pins the
// 2026-09-17 revision (docs/decisions/pen-routines.md) on a real database: routines are for
// ROLES and the resolution is per park. The two tenant-scoped park heads (the live
// Chandrakant / Dinakar shape) each see ONLY their home park's tasks; a tenant-scoped PC director
// sees both parks; a CXO sees and completes a whole-park every-3-days task that raised exactly
// ONE shed-less row; a routine whose role nobody holds raises nothing and is named.
func TestPenRoutineRoleResolutionTwoParkHeadsDirectorCXOParkScopeEveryNDaysPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)

	const today = "2026-09-16"
	const tomorrow = "2026-09-17"
	const threeDaysAgo = "2026-09-13"
	now := istInstant(today, 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: prTenant, ActorID: prCXO, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	bothRoles := []string{domain.RoleParkHead, domain.RolePCDirector}
	cbeRoutine, err := repo.CreateRoutine(ctx, write("cbe"), domain.Definition{
		ParkID: prParkCBE, Name: "Water trough", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCastro, Partition: "2"}},
		CadenceKind: domain.CadenceDaily, StartDate: threeDaysAgo, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: bothRoles,
	})
	if err != nil {
		t.Fatalf("create CBE routine: %v", err)
	}
	cptRoutine, err := repo.CreateRoutine(ctx, write("cpt"), domain.Definition{
		ParkID: prParkCPT, Name: "Water trough", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedCPT}},
		CadenceKind: domain.CadenceDaily, StartDate: threeDaysAgo, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: bothRoles,
	})
	if err != nil {
		t.Fatalf("create CPT routine: %v", err)
	}
	store, err := repo.CreateRoutine(ctx, write("store"), domain.Definition{
		ParkID: prParkCBE, Name: "Medicine store", Instruction: "Count the vials.", ScopeKind: domain.ScopePark,
		CadenceKind: domain.CadenceEveryNDays, IntervalDays: 3, StartDate: threeDaysAgo, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 0), AssigneeRoles: []string{domain.RoleCXO},
	})
	if err != nil {
		t.Fatalf("create park routine: %v", err)
	}
	if store.ScopeKind != domain.ScopePark || store.IntervalDays != 3 || store.StartDate != threeDaysAgo || len(store.People) != 1 || store.People[0].UserID != prCXO || store.People[0].RoleKey != domain.RoleCXO {
		t.Fatalf("park routine = %+v", store)
	}
	nobody, err := repo.CreateRoutine(ctx, write("nobody"), domain.Definition{
		ParkID: prParkCBE, Name: "Feed store", ScopeKind: domain.ScopeSelectedPens, Pens: []domain.PenRef{{ShedID: prShedYash}},
		CadenceKind: domain.CadenceDaily, StartDate: threeDaysAgo, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
		Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeRoles: []string{domain.RoleFeedDirector},
	})
	if err != nil {
		t.Fatalf("create nobody routine: %v", err)
	}
	// The DB refuses a whole-park routine that follows work, whatever the app layer does.
	if _, err := pool.Exec(ctx, `UPDATE pen_routine_definitions SET cadence_kind = 'after_work', after_work_kinds = ARRAY['deworming'] WHERE routine_id = $1::uuid`, store.RoutineID); err == nil {
		t.Fatal("the schema must refuse scope 'park' with cadence 'after_work'")
	}

	result, err := repo.Materialize(ctx, prTenant, today, today, now)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if result.Created != 3 {
		t.Fatalf("materialize = %+v, want 3 (CBE pen, CPT pen, ONE whole-park task)", result)
	}
	if len(result.RoutinesWithoutAssignee) != 1 || result.RoutinesWithoutAssignee[0].RoutineID != nobody.RoutineID || result.RoutinesWithoutAssignee[0].Roles[0] != domain.RoleFeedDirector {
		t.Fatalf("a routine for a role nobody holds must be named: %+v", result.RoutinesWithoutAssignee)
	}
	var parkRows, nullShed int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE shed_id IS NULL AND partition_label IS NULL AND shed_key = 'park') FROM pen_routine_tasks WHERE tenant_id = $1::uuid AND routine_id = $2::uuid`, prTenant, store.RoutineID).Scan(&parkRows, &nullShed); err != nil || parkRows != 1 || nullShed != 1 {
		t.Fatalf("whole-park rows = %d (null shed %d) / %v, want exactly one shed-less task", parkRows, nullShed, err)
	}
	var nobodyRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pen_routine_tasks WHERE tenant_id = $1::uuid AND routine_id = $2::uuid`, prTenant, nobody.RoutineID).Scan(&nobodyRows); err != nil || nobodyRows != 0 {
		t.Fatalf("a routine nobody holds raised %d rows / %v", nobodyRows, err)
	}
	if replay, err := repo.Materialize(ctx, prTenant, today, today, now); err != nil || replay.Created != 0 || replay.Widened != 0 {
		t.Fatalf("replay = %+v / %v, want nothing inserted", replay, err)
	}
	// every_n_days from 13 Sep: raises on the 16th, not on the 17th.
	next, err := repo.Materialize(ctx, prTenant, tomorrow, tomorrow, now.Add(24*time.Hour))
	if err != nil || next.Created != 2 {
		t.Fatalf("tomorrow = %+v / %v, want only the two daily pens", next, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pen_routine_tasks WHERE tenant_id = $1::uuid AND routine_id = $2::uuid`, prTenant, store.RoutineID).Scan(&parkRows); err != nil || parkRows != 1 {
		t.Fatalf("every 3 days must not raise the next day: %d rows / %v", parkRows, err)
	}

	mine := func(user string) []domain.Task {
		t.Helper()
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: user, Limit: 50})
		if err != nil {
			t.Fatalf("list %s: %v", user, err)
		}
		return page.Rows
	}
	parksOf := func(rows []domain.Task) map[string]int {
		out := map[string]int{}
		for _, r := range rows {
			out[r.ParkID]++
		}
		return out
	}
	// The two tenant-scoped park heads: each sees ONLY his home park (two days of one pen).
	if got := parksOf(mine(prHead)); len(got) != 1 || got[prParkCBE] != 2 {
		t.Fatalf("CBE head (tenant scope, home CBE) sees %v, want only CBE", got)
	}
	if got := parksOf(mine(prCPTHead)); len(got) != 1 || got[prParkCPT] != 2 {
		t.Fatalf("CPT head (tenant scope, home CPT) sees %v, want only CPT", got)
	}
	if got := parksOf(mine(prSecond)); len(got) != 1 || got[prParkCBE] != 2 {
		t.Fatalf("park-scoped CBE head sees %v", got)
	}
	// A tenant-scoped PC director covers both parks.
	if got := parksOf(mine(prPCDirector)); got[prParkCBE] != 2 || got[prParkCPT] != 2 {
		t.Fatalf("PC director sees %v, want both parks", got)
	}
	// Neither an expired grant nor a non-assignable role sees anything.
	for _, user := range []string{prExpired, prOperator, prVerifier} {
		if rows := mine(user); len(rows) != 0 {
			t.Fatalf("%s must see nothing, got %d", user, len(rows))
		}
		if n, err := repo.OpenCount(ctx, prTenant, user); err != nil || n != 0 {
			t.Fatalf("%s open count = %d / %v", user, n, err)
		}
	}
	if n, err := repo.OpenCount(ctx, prTenant, prCPTHead); err != nil || n != 2 {
		t.Fatalf("CPT head open count = %d / %v", n, err)
	}
	// The CPT head cannot work the CBE pen.
	cbeTask := mine(prHead)[0]
	if cbeTask.RoutineID != cbeRoutine.RoutineID {
		t.Fatalf("CBE head's task = %+v", cbeTask)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: domain.Actor{UserID: prCPTHead}, TaskID: cbeTask.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "yes"}), RowVersion: cbeTask.RowVersion, IdempotencyKey: "cpt-head-on-cbe"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("CPT head submitting a CBE task err = %v, want ErrNotAssignee", err)
	}
	if rows := mine(prCPTHead); rows[0].RoutineID != cptRoutine.RoutineID {
		t.Fatalf("CPT head's task = %+v", rows[0])
	}

	// The CXO: sees the whole-park task (and no pen task), checks in, submits, done.
	cxoRows := mine(prCXO)
	if len(cxoRows) != 1 {
		t.Fatalf("CXO list = %+v, want the one whole-park task", cxoRows)
	}
	parkTask := cxoRows[0]
	step := domain.StepFor(parkTask, domain.Actor{UserID: prCXO}, today)
	if !parkTask.IsParkTask() || parkTask.ShedID != "" || parkTask.PenLabel != "" || step.Title != "Medicine store · Coimbatore" || step.PresenceLine != "Check in to start" || !step.CanCheckIn || parkTask.CadenceLine != "Every 3 days" || len(parkTask.AssigneeIDs) != 1 || parkTask.AssigneeNames[0] != "The CXO" {
		t.Fatalf("park task = %+v / step %+v", parkTask, step)
	}
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: domain.Actor{UserID: prHead}, TaskID: parkTask.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "yes"}), RowVersion: parkTask.RowVersion, IdempotencyKey: "head-on-cxo"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("a park head submitting the CXO's task err = %v", err)
	}
	digests, err := repo.DueDigests(ctx, prTenant, today)
	if err != nil {
		t.Fatalf("digests: %v", err)
	}
	var storeDigest *ports.DueDigest
	for i := range digests {
		if digests[i].RoutineID == store.RoutineID {
			storeDigest = &digests[i]
		}
	}
	if storeDigest == nil || len(storeDigest.Tasks) != 1 || !storeDigest.Tasks[0].IsParkTask() || len(storeDigest.AssigneeIDs) != 1 || storeDigest.AssigneeIDs[0] != prCXO {
		t.Fatalf("park digest = %+v", storeDigest)
	}
	entered, err := repo.RecordPresence(ctx, ports.PresenceParams{TenantID: prTenant, Actor: domain.Actor{UserID: prCXO}, TaskID: parkTask.TaskID, EventType: domain.PresenceEnter, CapturedAt: now, RowVersion: parkTask.RowVersion, IdempotencyKey: "cxo-enter"})
	if err != nil {
		t.Fatalf("CXO check-in: %v", err)
	}
	if domain.PresenceLine(entered) != "Checked in since 9:00 am" {
		t.Fatalf("park presence line = %q", domain.PresenceLine(entered))
	}
	done, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: domain.Actor{UserID: prCXO}, TaskID: parkTask.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "yes"}), RowVersion: entered.RowVersion, IdempotencyKey: "cxo-submit"})
	if err != nil || done.Status != domain.StatusCompleted || done.WorkState != domain.WorkStateCompleted || done.SubmittedBy != prCXO {
		t.Fatalf("CXO submit = %+v / %v", done, err)
	}

	// The routines table previews who holds each routine's roles, labelled by role.
	rows, _, err := repo.ListRoutinesAndParks(ctx, ports.RoutineListParams{TenantID: prTenant, ParkID: prParkCBE, Today: today})
	if err != nil {
		t.Fatalf("list routines: %v", err)
	}
	for _, row := range rows {
		if row.Definition.RoutineID != cbeRoutine.RoutineID {
			continue
		}
		roleOf := map[string]string{}
		for _, p := range row.Definition.People {
			roleOf[p.UserID] = p.RoleKey
		}
		if len(roleOf) != 3 || roleOf[prHead] != domain.RoleParkHead || roleOf[prSecond] != domain.RoleParkHead || roleOf[prPCDirector] != domain.RolePCDirector || len(row.Definition.AssigneeRoles) != 2 {
			t.Fatalf("CBE routine people = %+v roles %v", row.Definition.People, row.Definition.AssigneeRoles)
		}
	}

	// Every envelope -- including the shed-less whole-park task's -- passes the relay schema.
	counts := assertEnvelopesValid(t, ctx, pool)
	if counts[EventRoutineCreated] != 5 || counts[EventRoutineSubmitted] != 1 {
		t.Fatalf("outbox counts = %v", counts)
	}
}
