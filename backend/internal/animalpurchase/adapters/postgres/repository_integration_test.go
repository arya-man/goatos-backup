package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	"github.com/vgoats/goatos/backend/internal/animalpurchase/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	apTenant = "00000000-0000-4000-8000-000000000001"
	apUser   = "91000000-0000-4000-8000-000000000501"
	apCEO    = "91000000-0000-4000-8000-000000000502"
	apMember = "97000000-0000-4000-8000-000000000502"
)

// TestAnimalPurchaseWritePathWithDockerPostgres proves the whole candidate register on the real
// database (maintainer decision 2026-09-13): a load (+ exact replay, + same-key/different-payload
// conflict, + a reused load number), animals with lock-assigned sequence numbers and whole-load
// counts, the version-fenced once-only decision, and the outbox event the decision leaves behind
// -- accepted by the validator trigger, which is what the push rides on.
func TestAnimalPurchaseWritePathWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Animal Purchase Tenant', 'active') ON CONFLICT (tenant_id) DO NOTHING`, apTenant)
	// The harness template already seeds the CPT park; the load's park_id resolves to it.
	mustExec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
SELECT gen_random_uuid(), $1::uuid, 'park', 'CPT', 'Channapatna', 'active'
WHERE NOT EXISTS (SELECT 1 FROM locations WHERE tenant_id = $1::uuid AND location_type = 'park' AND upper(location_code) = 'CPT')`, apTenant)
	var vendorID string
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state)
VALUES ($1::uuid, 'Livestock Agent', 'Ramesh Traders', 'active', 'Karnataka') RETURNING vendor_id::text`, apTenant).Scan(&vendorID); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	mustExec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Ravi', 'Ravi', 'active', 'park_head')`, apMember, apTenant, apCEO)

	repo := NewRepository(pool, 10*time.Second)

	// ---- load ----
	write := domain.LoadWrite{LoadRef: "132", VendorID: vendorID, FarmLabel: "CPT", ExpectedCount: 40, Notes: "Sirohi females"}
	load, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, Write: write, ActorID: apUser, IdempotencyKey: "load-1"})
	if err != nil {
		t.Fatalf("CreateLoad: %v", err)
	}
	if load.VendorName != "Ramesh Traders" || load.FarmLabel != "CPT" || load.ParkID == "" || load.Counts.Total != 0 {
		t.Fatalf("load = %+v", load)
	}
	replay, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, Write: write, ActorID: apUser, IdempotencyKey: "load-1"})
	if err != nil || replay.LoadID != load.LoadID {
		t.Fatalf("exact replay must return the original load: %v / %+v", err, replay)
	}
	changed := write
	changed.Notes = "different"
	if _, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, Write: changed, ActorID: apUser, IdempotencyKey: "load-1"}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key, different payload must conflict, got %v", err)
	}
	if _, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, Write: write, ActorID: apUser, IdempotencyKey: "load-2"}); !errors.Is(err, ports.ErrLoadRefTaken) {
		t.Fatalf("a reused load number must be refused, got %v", err)
	}
	if _, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, Write: domain.LoadWrite{LoadRef: "133", VendorID: "00000000-0000-4000-8000-0000000000ff", FarmLabel: "CBE"}, ActorID: apUser, IdempotencyKey: "load-3"}); !errors.Is(err, ports.ErrVendorNotFound) {
		t.Fatalf("an unknown vendor must be refused, got %v", err)
	}

	// ---- animals ----
	add := func(key, sex string) domain.Candidate {
		t.Helper()
		c, err := repo.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser, QuestionnaireVersion: domain.QuestionnaireVersion, IdempotencyKey: key,
			Write: inspection(sex, "Sirohi", 22.5, "10000000-0000-4000-8000-00000000000"+key[len(key)-1:])})
		if err != nil {
			t.Fatalf("AddCandidate %s: %v", key, err)
		}
		return c
	}
	a1 := add("animal-1", "female")
	a2 := add("animal-2", "male")
	// Each successful add announces the new load snapshot in the same transaction.
	var candidateEvents int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type='procurement.animal_purchase.candidate_recorded' AND (payload->'payload'->>'load_id')=$2`, apTenant, load.LoadID).Scan(&candidateEvents); err != nil || candidateEvents != 2 {
		t.Fatalf("candidate events=%d err=%v", candidateEvents, err)
	}

	if a1.SeqNo != 1 || a2.SeqNo != 2 || a1.LoadRef != "132" || a1.Decision != domain.DecisionPending {
		t.Fatalf("sequence numbers: %+v / %+v", a1, a2)
	}
	// The questionnaire round-trips: typed derivations, the answers, the field verdict and the
	// media rows per slot (read back in ONE query per page).
	if a1.Species != "goat" || a1.Sex != "female" || a1.Breed != "Sirohi" || a1.TempTag != "GW-Sirohi" || a1.WeightKg == nil || *a1.WeightKg != 22.5 ||
		a1.RectalTempC == nil || *a1.RectalTempC != 39.0 || a1.FieldVerdict != domain.FieldVerdictSelected || a1.QuestionnaireVersion != domain.QuestionnaireVersion {
		t.Fatalf("typed derivations: %+v", a1)
	}
	if n := a1.Answers.Number("teeth"); n == nil || *n != 4 || len(a1.Media[domain.SlotAnimal]) != 1 || len(a1.Media[domain.SlotTeeth]) != 1 || len(a1.Media[domain.SlotUdder]) != 1 {
		t.Fatalf("answers/media round trip: answers=%v media=%v", a1.Answers, a1.Media)
	}
	if again, err := repo.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser, QuestionnaireVersion: domain.QuestionnaireVersion, IdempotencyKey: "animal-1",
		Write: inspection("female", "Sirohi", 22.5, "10000000-0000-4000-8000-000000000001")}); err != nil || again.CandidateID != a1.CandidateID {
		t.Fatalf("exact replay must return the original animal: %v / %+v", err, again)
	}
	page, err := repo.ListCandidates(ctx, apTenant, load.LoadID, domain.Cursor{}, 1)
	if err != nil || len(page.Candidates) != 1 || page.NextCursor == "" || page.Counts.Total != 2 || page.Counts.Pending != 2 {
		t.Fatalf("first page must hold one row, a cursor and WHOLE-load counts: %v / %+v", err, page)
	}
	next, _ := domain.DecodeCursor(page.NextCursor, domain.CursorKindCandidate)
	page2, err := repo.ListCandidates(ctx, apTenant, load.LoadID, next, 1)
	if err != nil || len(page2.Candidates) != 1 || page2.Candidates[0].SeqNo != 2 || page2.NextCursor != "" {
		t.Fatalf("second page: %v / %+v", err, page2)
	}
	loads, err := repo.ListLoads(ctx, apTenant, domain.Cursor{}, 20)
	if err != nil || len(loads.Loads) != 1 || loads.Loads[0].Counts.Total != 2 {
		t.Fatalf("load list must carry the whole-load counts: %v / %+v", err, loads)
	}

	// ---- decision ----
	decided, err := repo.Decide(ctx, ports.DecideParams{TenantID: apTenant, CandidateID: a1.CandidateID, ActorID: apCEO, IdempotencyKey: "decide-1",
		Write: domain.DecisionWrite{Decision: domain.DecisionAccepted, RowVersion: a1.RowVersion}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decided.Decision != domain.DecisionAccepted || decided.DecidedByName != "Ravi" || decided.DecidedAt == nil || decided.RowVersion != a1.RowVersion+1 {
		t.Fatalf("decided = %+v", decided)
	}
	if _, err := repo.Decide(ctx, ports.DecideParams{TenantID: apTenant, CandidateID: a1.CandidateID, ActorID: apCEO, IdempotencyKey: "decide-2",
		Write: domain.DecisionWrite{Decision: domain.DecisionRejected, RowVersion: decided.RowVersion}}); !errors.Is(err, ports.ErrAlreadyDecided) {
		t.Fatalf("a second decision must be refused, got %v", err)
	}
	if _, err := repo.Decide(ctx, ports.DecideParams{TenantID: apTenant, CandidateID: a2.CandidateID, ActorID: apCEO, IdempotencyKey: "decide-3",
		Write: domain.DecisionWrite{Decision: domain.DecisionRejected, RowVersion: a2.RowVersion + 5}}); !errors.Is(err, ports.ErrRowVersionMismatch) {
		t.Fatalf("a stale version must be refused, got %v", err)
	}
	review, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{Decision: domain.DecisionPending, Limit: 20})
	// The page is the pending chip; the counts are the WHOLE filter across every decision (one
	// accepted, one pending), which is what the chips beside the list show.
	if err != nil || len(review.Candidates) != 1 || review.Candidates[0].CandidateID != a2.CandidateID || review.Counts.Pending != 1 || review.Counts.Accepted != 1 || review.Counts.Total != 2 {
		t.Fatalf("review queue: %v / %+v", err, review)
	}
	var outbox int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2 AND aggregate_type = 'animal_purchase_candidate' AND aggregate_id = $3::uuid`,
		apTenant, DecidedEventType, a1.CandidateID).Scan(&outbox); err != nil || outbox != 1 {
		t.Fatalf("the decision must leave exactly one outbox event: %v / %d", err, outbox)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action LIKE 'procurement.animal_purchase.%'`, apTenant).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("load + 2 animals + decision must audit 4 rows: %v / %d", err, audits)
	}
}

// The four adversarial reads the aggregate guard requires, over the same seeded database: many
// candidates on one load, page boundaries, tenant scope, and every decision bucket. They run
// after the write-path test in one process so the migrated template is built once.
func TestAnimalPurchaseCountsOneToManyAndMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, load := seedLoadWithCandidates(t, ctx, pool, 5)
	loads, err := repo.ListLoads(ctx, apTenant, domain.Cursor{}, 20)
	if err != nil || len(loads.Loads) != 1 {
		t.Fatalf("a load with five candidates must still be ONE load row: %v / %d", err, len(loads.Loads))
	}
	if got := loads.Loads[0].Counts; got.Total != 5 || got.Pending != 5 {
		t.Fatalf("whole-load counts = %+v", got)
	}
	if load.Counts.Total != 5 {
		t.Fatalf("GetLoad counts = %+v", load.Counts)
	}
}

func TestAnimalPurchaseCountsSurvivePaginationPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, load := seedLoadWithCandidates(t, ctx, pool, 5)
	page1, err := repo.ListCandidates(ctx, apTenant, load.LoadID, domain.Cursor{}, 2)
	if err != nil || len(page1.Candidates) != 2 || page1.Counts.Total != 5 {
		t.Fatalf("page 1: %v / %d rows, counts %+v", err, len(page1.Candidates), page1.Counts)
	}
	c, _ := domain.DecodeCursor(page1.NextCursor, domain.CursorKindCandidate)
	page2, err := repo.ListCandidates(ctx, apTenant, load.LoadID, c, 2)
	if err != nil || len(page2.Candidates) != 2 || page2.Counts.Total != 5 || page2.Candidates[0].SeqNo != 3 {
		t.Fatalf("page 2 must keep the whole-load counts and continue after seq 2: %v / %+v", err, page2)
	}
	c, _ = domain.DecodeCursor(page2.NextCursor, domain.CursorKindCandidate)
	page3, err := repo.ListCandidates(ctx, apTenant, load.LoadID, c, 2)
	if err != nil || len(page3.Candidates) != 1 || page3.NextCursor != "" || page3.Counts.Total != 5 {
		t.Fatalf("last page: %v / %+v", err, page3)
	}
	review1, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{Decision: domain.DecisionPending, Limit: 3})
	if err != nil || len(review1.Candidates) != 3 || review1.Counts.Pending != 5 {
		t.Fatalf("review page 1 must count the whole filter: %v / %+v", err, review1.Counts)
	}
	rc, _ := domain.DecodeCursor(review1.NextCursor, domain.CursorKindReview)
	review2, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{Decision: domain.DecisionPending, Cursor: rc, Limit: 3})
	if err != nil || len(review2.Candidates) != 2 || review2.Counts.Pending != 5 || review2.NextCursor != "" {
		t.Fatalf("review page 2 must keep the whole-filter count after the cursor: %v / %+v", err, review2)
	}
}

func TestAnimalPurchaseReadsHonourTenantScopeHierarchy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, load := seedLoadWithCandidates(t, ctx, pool, 2)
	const otherTenant = "00000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Other', 'active') ON CONFLICT (tenant_id) DO NOTHING`, otherTenant); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetLoad(ctx, otherTenant, load.LoadID); !errors.Is(err, ports.ErrLoadNotFound) {
		t.Fatalf("another tenant must not see the load: %v", err)
	}
	if loads, err := repo.ListLoads(ctx, otherTenant, domain.Cursor{}, 20); err != nil || len(loads.Loads) != 0 {
		t.Fatalf("another tenant's load list must be empty: %v / %d", err, len(loads.Loads))
	}
	if review, err := repo.ListReview(ctx, otherTenant, ports.ReviewQuery{Decision: "", Limit: 20}); err != nil || review.Counts.Total != 0 {
		t.Fatalf("another tenant's review counts must be zero: %v / %+v", err, review.Counts)
	}
	if _, err := repo.ListCandidates(ctx, otherTenant, load.LoadID, domain.Cursor{}, 20); !errors.Is(err, ports.ErrLoadNotFound) {
		t.Fatalf("another tenant must not page the load's animals: %v", err)
	}
}

func TestAnimalPurchaseReviewStatusMatrixEveryStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, load := seedLoadWithCandidates(t, ctx, pool, 4)
	all, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{Limit: 20})
	if err != nil || len(all.Candidates) != 4 {
		t.Fatalf("seed: %v / %d", err, len(all.Candidates))
	}
	decide := func(i int, decision, key string) {
		t.Helper()
		c := all.Candidates[i]
		if _, err := repo.Decide(ctx, ports.DecideParams{TenantID: apTenant, CandidateID: c.CandidateID, ActorID: apCEO, IdempotencyKey: key,
			Write: domain.DecisionWrite{Decision: decision, RowVersion: c.RowVersion}}); err != nil {
			t.Fatalf("decide %d: %v", i, err)
		}
	}
	decide(0, domain.DecisionAccepted, "m-1")
	decide(1, domain.DecisionRejected, "m-2")
	decide(2, domain.DecisionRejected, "m-3")
	want := map[string]int{domain.DecisionPending: 1, domain.DecisionAccepted: 1, domain.DecisionRejected: 2, "": 4}
	for decision, n := range want {
		page, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{Decision: decision, Limit: 20})
		if err != nil || len(page.Candidates) != n {
			t.Fatalf("decision %q: %v / %d rows want %d", decision, err, len(page.Candidates), n)
		}
		// The whole-filter counts are the SAME numbers whichever bucket is being listed.
		if page.Counts.Pending+page.Counts.Accepted+page.Counts.Rejected != page.Counts.Total {
			t.Fatalf("buckets must partition the total: %+v", page.Counts)
		}
	}
	reloaded, err := repo.GetLoad(ctx, apTenant, load.LoadID)
	if err != nil || reloaded.Counts != (domain.DecisionCounts{Total: 4, Pending: 1, Accepted: 1, Rejected: 2}) {
		t.Fatalf("load counts after decisions = %+v (%v)", reloaded.Counts, err)
	}
	// The recorded-on window narrows the page AND the whole-filter counts together: everything
	// was recorded just now, so a window ending yesterday is empty and one starting yesterday
	// is the full set, whichever decision chip is picked.
	yesterday := time.Now().Add(-24 * time.Hour)
	if page, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{To: yesterday, Limit: 20}); err != nil || len(page.Candidates) != 0 || page.Counts.Total != 0 {
		t.Fatalf("window ending yesterday: %v / %d rows, counts %+v", err, len(page.Candidates), page.Counts)
	}
	if page, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{From: yesterday, Decision: domain.DecisionRejected, Limit: 20}); err != nil || len(page.Candidates) != 2 || page.Counts.Rejected != 2 {
		t.Fatalf("window from yesterday, rejected chip: %v / %d rows, counts %+v", err, len(page.Candidates), page.Counts)
	}
	if page, err := repo.ListReview(ctx, apTenant, ports.ReviewQuery{From: yesterday, Limit: 20}); err != nil || page.Counts.Total != 4 {
		t.Fatalf("window from yesterday, every decision: %v counts %+v", err, page.Counts)
	}
}

// seedLoadWithCandidates writes one load with n pending candidates through the production
// write path and returns the repository and the load as read back.
func seedLoadWithCandidates(t *testing.T, ctx context.Context, pool *pgxpool.Pool, n int) (*Repository, domain.Load) {
	t.Helper()
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	mustExec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Animal Purchase Tenant', 'active') ON CONFLICT (tenant_id) DO NOTHING`, apTenant)
	mustExec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
SELECT gen_random_uuid(), $1::uuid, 'park', 'CPT', 'Channapatna', 'active'
WHERE NOT EXISTS (SELECT 1 FROM locations WHERE tenant_id = $1::uuid AND location_type = 'park' AND upper(location_code) = 'CPT')`, apTenant)
	var vendorID string
	if err := pool.QueryRow(ctx, `INSERT INTO procurement_vendors (tenant_id, record_type, business_name, status, state)
VALUES ($1::uuid, 'Livestock Agent', 'Ramesh Traders', 'active', 'Karnataka') RETURNING vendor_id::text`, apTenant).Scan(&vendorID); err != nil {
		t.Fatalf("seed vendor: %v", err)
	}
	mustExec(`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'Ravi', 'Ravi', 'active', 'park_head') ON CONFLICT DO NOTHING`, apMember, apTenant, apCEO)
	repo := NewRepository(pool, 10*time.Second)
	load, err := repo.CreateLoad(ctx, ports.CreateLoadParams{TenantID: apTenant, ActorID: apUser, IdempotencyKey: "seed-load",
		Write: domain.LoadWrite{LoadRef: "SEED", VendorID: vendorID, FarmLabel: "CPT", ExpectedCount: n}})
	if err != nil {
		t.Fatalf("seed load: %v", err)
	}
	for i := 1; i <= n; i++ {
		if _, err := repo.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser, QuestionnaireVersion: domain.QuestionnaireVersion, IdempotencyKey: fmt.Sprintf("seed-animal-%d", i),
			Write: inspection("female", fmt.Sprintf("B%d", i), 20, fmt.Sprintf("10000000-0000-4000-8000-0000000000%02d", i))}); err != nil {
			t.Fatalf("seed animal %d: %v", i, err)
		}
	}
	load, err = repo.GetLoad(ctx, apTenant, load.LoadID)
	if err != nil {
		t.Fatalf("seed reload: %v", err)
	}
	return repo, load
}

// inspection builds a complete questionnaire write for a female goat with the given proof refs
// in the animal slot; teeth and udder slots get their own refs derived from the first.
func inspection(sex, breed string, weight float64, animalRefs ...string) domain.CandidateWrite {
	j := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	a := domain.Answers{
		"species": j("goat"), "goat_id": j("GW-" + breed), "well_fed": j("yes"), "teeth": j(4), "sex": j(sex),
		"weight_kg": j(weight), "rectal_temp_c": j(39.0),
		"anaemic": j("no"), "mouth_breathing": j("no"), "watery_eyes": j("no"), "eye_colour": j("no"), "nasal_discharge": j("no"),
		"face_scabs": j("no"), "acidosis": j("no"), "diarrhea": j("no"), "ticks_hair_loss": j("no"), "wounds": j("no"),
		"body_scabs": j("no"), "lumps": j("no"), "arthritis": j("no"), "udder_state": j([]string{"normal"}),
		"field_verdict": j("selected"), "breed": j(breed),
	}
	if sex == "female" {
		a["pregnant"] = j("no")
		a["lactating"] = j("no")
		a["teats"] = j("2")
		a["teat_discharge"] = j("no")
	}
	// Every capture belongs to exactly one animal (unique on proof_ref), so the teeth and udder
	// refs are derived per animal from its own animal ref, never shared across the seed.
	base := animalRefs[0]
	w := domain.CandidateWrite{Catalog: domain.SeededCatalog(), Answers: a, Media: domain.MediaRefs{
		domain.SlotTeeth: {"teeth-" + base}, domain.SlotAnimal: animalRefs, domain.SlotUdder: {"udder-" + base},
	}}
	// The service normalizes (deriving the typed columns) before the repository is reached;
	// this test drives the repository directly, so it does the same.
	w.Normalize()
	return w
}

// TestBreedSuggestionsComeFromTheLIVEHerd pins the live-herd predicate in sqlBreedSuggestions.
//
// The picklist an inspector picks a breed from is built from the farm's own animals, and it is
// deliberately built from the LIVING ones: a breed the farm no longer keeps should stop being
// offered. Nothing tested that. Neutralising `g.lifecycle_status = 'alive'` in repository.go left
// every test in this package green, because every fixture animal here is alive -- so the filter
// could have been deleted and no one would have known.
//
// The test seeds one breed on a live animal and a DIFFERENT breed on a dead one, which is the only
// arrangement that can tell the two readings apart.
func TestBreedSuggestionsComeFromTheLIVEHerd(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo, _ := seedLoadWithCandidates(t, ctx, pool, 1)

	custodian := "00000000-0000-4000-8000-0000000009c0"
	if _, err := pool.Exec(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Breed Suggestion Custodian', 'active') ON CONFLICT (party_id) DO NOTHING`, custodian); err != nil {
		t.Fatalf("seed custodian: %v", err)
	}
	insert := func(id, breed, lifecycle string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id,
                   origin_type, dob, entry_date, exited_at, exit_reason)
VALUES ($1::uuid, $2::uuid, 'goat', 'female', $3, $4, $5::uuid, 'procured', DATE '2026-01-01', DATE '2026-01-01',
        CASE WHEN $4 <> 'alive' THEN now() END,
        CASE WHEN $4 <> 'alive' THEN 'died' END)
ON CONFLICT (goat_id) DO NOTHING`, id, apTenant, breed, lifecycle, custodian); err != nil {
			t.Fatalf("seed %s goat: %v", lifecycle, err)
		}
	}
	insert("00000000-0000-4000-8000-0000000009a1", "Kept Malai", "alive")
	insert("00000000-0000-4000-8000-0000000009a2", "Retired Sirohi", "dead")

	got, err := repo.BreedSuggestions(ctx, apTenant)
	if err != nil {
		t.Fatalf("breed suggestions: %v", err)
	}
	var sawLive, sawDead bool
	for _, b := range got {
		switch b {
		case "Kept Malai":
			sawLive = true
		case "Retired Sirohi":
			sawDead = true
		}
	}
	if !sawLive {
		t.Fatalf("a living animal's breed is missing from the suggestions: %v", got)
	}
	if sawDead {
		t.Fatalf("a DEAD animal's breed is being offered to the inspector: %v", got)
	}
}
