package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

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
	age := 8
	weight := 22.5
	add := func(key, sex string) domain.Candidate {
		t.Helper()
		c, err := repo.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser, IdempotencyKey: key,
			Write: domain.CandidateWrite{Species: "goat", Sex: sex, Breed: "Sirohi", AgeMonths: &age, WeightKg: &weight, Condition: "healthy", VideoProofRef: "10000000-0000-4000-8000-00000000000" + key[len(key)-1:]}})
		if err != nil {
			t.Fatalf("AddCandidate %s: %v", key, err)
		}
		return c
	}
	a1 := add("animal-1", "female")
	a2 := add("animal-2", "male")
	if a1.SeqNo != 1 || a2.SeqNo != 2 || a1.LoadRef != "132" || a1.Decision != domain.DecisionPending {
		t.Fatalf("sequence numbers: %+v / %+v", a1, a2)
	}
	if again, err := repo.AddCandidate(ctx, ports.AddCandidateParams{TenantID: apTenant, LoadID: load.LoadID, ActorID: apUser, IdempotencyKey: "animal-1",
		Write: domain.CandidateWrite{Species: "goat", Sex: "female", Breed: "Sirohi", AgeMonths: &age, WeightKg: &weight, Condition: "healthy", VideoProofRef: "10000000-0000-4000-8000-000000000001"}}); err != nil || again.CandidateID != a1.CandidateID {
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
	if err != nil || len(review.Candidates) != 1 || review.Candidates[0].CandidateID != a2.CandidateID || review.Counts.Pending != 1 || review.Counts.Total != 1 {
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
