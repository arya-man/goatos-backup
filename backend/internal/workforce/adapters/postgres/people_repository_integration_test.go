package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Fixed identities for the People/HRMS create-person suite. Reuses the shared
// seed tenant like the other workforce integration suites.
const (
	peopleTenant = "00000000-0000-4000-8000-000000000001"
	peopleActor  = "91000000-0000-4000-8000-000000000021"
	peopleUser   = "91000000-0000-4000-8000-000000000022"
	peoplePark   = "92000000-0000-4000-8000-000000000021"
)

func seedPeoplePark(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'PPL', 'People Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, peoplePark, peopleTenant); err != nil {
		t.Fatalf("seed park: %v", err)
	}
}

func peopleCreateCommand(key string) ports.CreatePersonCommand {
	return ports.CreatePersonCommand{
		TenantID:        peopleTenant,
		ActorID:         peopleActor,
		IdempotencyKey:  key,
		UserID:          peopleUser,
		FirstName:       "Idem",
		LastName:        "Check",
		DisplayName:     "Idem Check",
		Email:           "idem-check@mesha.sg",
		NormalizedEmail: "idem-check@mesha.sg",
		Role:            "operator",
		ScopeType:       "park",
		ScopeID:         peoplePark,
		RoleHint:        "operator",
		ParkID:          peoplePark,
	}
}

// TestCreatePersonTransactionIsAtomicAndIdempotentWithDockerPostgres pins the
// whole onboarding write contract against real Postgres:
//
//   - first call writes member + active park-scoped grant + active allowlist
//     row + audit in ONE transaction;
//   - an EXACT replay (same key + fingerprint) returns the ORIGINAL person and
//     runs no side effects — still exactly one row of each;
//   - a same-key/different-payload replay is rejected with
//     ports.ErrIdempotencyConflict;
//   - a NEW key for the same email is refused with ports.ErrDuplicateEmail and
//     writes nothing.
func TestCreatePersonTransactionIsAtomicAndIdempotentWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	seedPeoplePark(t, ctx, pool)

	preflight, err := repo.PreflightCreatePerson(ctx, ports.PreflightCreatePersonCommand{
		TenantID:        peopleTenant,
		IdempotencyKey:  "people-key-1",
		NormalizedEmail: "idem-check@mesha.sg",
		FirstName:       "Idem",
		LastName:        "Check",
		Role:            "operator",
		ScopeType:       "park",
		ScopeID:         peoplePark,
	})
	if err != nil {
		t.Fatalf("PreflightCreatePerson before first create: %v", err)
	}
	if preflight.Replay != nil {
		t.Fatalf("first preflight unexpectedly replayed an existing person")
	}

	first, err := repo.CreatePerson(ctx, peopleCreateCommand("people-key-1"))
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if first.Email == nil || *first.Email != "idem-check@mesha.sg" {
		t.Fatalf("created person email = %v", first.Email)
	}
	if first.ParkLabel == nil || *first.ParkLabel != "People Park" {
		t.Fatalf("created person park = %v", first.ParkLabel)
	}

	assertCounts := func(stage string) {
		t.Helper()
		var members, grants, allowed, audits int
		if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM workforce_members WHERE tenant_id = $1::uuid AND lower(email) = 'idem-check@mesha.sg'),
  (SELECT count(*) FROM user_scope_grants WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND role = 'operator' AND scope_type = 'park' AND status = 'active'),
  (SELECT count(*) FROM auth_allowed_emails WHERE tenant_id = $1::uuid AND normalized_email = 'idem-check@mesha.sg' AND status = 'active'),
  (SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action = 'workforce.person.created')`,
			peopleTenant, peopleUser).Scan(&members, &grants, &allowed, &audits); err != nil {
			t.Fatalf("%s: count rows: %v", stage, err)
		}
		if members != 1 || grants != 1 || allowed != 1 || audits != 1 {
			t.Fatalf("%s: want exactly one member/grant/allowlist/audit row, got %d/%d/%d/%d", stage, members, grants, allowed, audits)
		}
	}
	assertCounts("after first create")

	// Exact replay: original result, zero new side effects.
	replay, err := repo.CreatePerson(ctx, peopleCreateCommand("people-key-1"))
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if replay.PersonID != first.PersonID {
		t.Fatalf("replay returned a different person: %s != %s", replay.PersonID, first.PersonID)
	}
	assertCounts("after exact replay")

	// Same key, different payload: rejected, nothing written.
	conflicting := peopleCreateCommand("people-key-1")
	conflicting.FirstName = "Other"
	conflicting.Email = "other@mesha.sg"
	conflicting.NormalizedEmail = "other@mesha.sg"
	if _, err := repo.CreatePerson(ctx, conflicting); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same-key/different-payload must return ErrIdempotencyConflict, got %v", err)
	}
	assertCounts("after idempotency conflict")

	// New key, same email: duplicate refused before any insert.
	duplicate := peopleCreateCommand("people-key-2")
	if _, err := repo.CreatePerson(ctx, duplicate); !errors.Is(err, ports.ErrDuplicateEmail) {
		t.Fatalf("duplicate email must return ErrDuplicateEmail, got %v", err)
	}
	assertCounts("after duplicate email attempt")

	// Proof statistics: verification-item grain, keyed on operator_id =
	// workforce_members.user_id, withdrawn excluded from EVERY number, and the
	// rejection rate over decided items only (1 rejected / 2 decided = 50%).
	for i, status := range []string{"approved", "rejected", "pending", "withdrawn"} {
		reason := ""
		if status == "rejected" {
			reason = "blurred clip"
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (
  tenant_id, vertical, module, category, source_module, source_ref_type, source_ref_id,
  media_refs, status, verdict_reason, operator_id, captured_at, idempotency_key
) VALUES (
  $1::uuid, 'weighing', 'weighing', 'weighing_proof', 'weighing', 'weighing_observation',
  gen_random_uuid(), '["media"]'::jsonb, $2, nullif($3, ''), $4::uuid, now(), 'people-stats-' || $5::int::text
)`, peopleTenant, status, reason, peopleUser, i); err != nil {
			t.Fatalf("seed verification item %s: %v", status, err)
		}
	}
	listed, _, err := repo.ListPeople(ctx, ports.ListPeopleParams{TenantID: peopleTenant, Search: "idem-check", Limit: 5})
	if err != nil {
		t.Fatalf("ListPeople for stats: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("want the one created person, got %d rows", len(listed))
	}
	stats := listed[0]
	if stats.ProofUploads != 3 || stats.ProofApproved != 1 || stats.ProofRejected != 1 || stats.ProofPending != 1 {
		t.Fatalf("stats = uploads %d approved %d rejected %d pending %d; want 3/1/1/1 (withdrawn excluded)",
			stats.ProofUploads, stats.ProofApproved, stats.ProofRejected, stats.ProofPending)
	}
	if stats.ProofRejectionPct == nil || *stats.ProofRejectionPct != 50 {
		t.Fatalf("rejection pct = %v, want 50 (1 rejected of 2 decided)", stats.ProofRejectionPct)
	}

	deactivated, err := repo.SetOperatorStatus(ctx, ports.StatusCommand{
		TenantID:   peopleTenant,
		ActorID:    peopleActor,
		OperatorID: first.PersonID,
		Reason:     "people_hrms_admin_action",
		RowVersion: first.RowVersion,
		Status:     "inactive",
	})
	if err != nil {
		t.Fatalf("deactivate person: %v", err)
	}
	if deactivated.Status != "inactive" {
		t.Fatalf("deactivated status = %q, want inactive", deactivated.Status)
	}
	var activeGrants, activeAllowlist int
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM user_scope_grants WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'),
  (SELECT count(*) FROM auth_allowed_emails WHERE tenant_id = $1::uuid AND normalized_email = 'idem-check@mesha.sg' AND status = 'active')`,
		peopleTenant, peopleUser).Scan(&activeGrants, &activeAllowlist); err != nil {
		t.Fatalf("count active access after deactivate: %v", err)
	}
	if activeGrants != 0 || activeAllowlist != 0 {
		t.Fatalf("deactivate must remove active grants and allowlist access, got grants=%d allowlist=%d", activeGrants, activeAllowlist)
	}

	reactivated, err := repo.SetOperatorStatus(ctx, ports.StatusCommand{
		TenantID:   peopleTenant,
		ActorID:    peopleActor,
		OperatorID: first.PersonID,
		Reason:     "people_hrms_admin_action",
		RowVersion: deactivated.RowVersion,
		Status:     "active",
	})
	if err != nil {
		t.Fatalf("reactivate person: %v", err)
	}
	if reactivated.Status != "active" {
		t.Fatalf("reactivated status = %q, want active", reactivated.Status)
	}
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM user_scope_grants WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'),
  (SELECT count(*) FROM auth_allowed_emails WHERE tenant_id = $1::uuid AND normalized_email = 'idem-check@mesha.sg' AND status = 'active')`,
		peopleTenant, peopleUser).Scan(&activeGrants, &activeAllowlist); err != nil {
		t.Fatalf("count active access after reactivate: %v", err)
	}
	if activeGrants != 1 || activeAllowlist != 1 {
		t.Fatalf("reactivate must restore active grants and allowlist access, got grants=%d allowlist=%d", activeGrants, activeAllowlist)
	}
}

func TestCreatePersonExactReplayWhileStartedIsInFlightWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	seedPeoplePark(t, ctx, pool)

	cmd := peopleCreateCommand("people-key-in-flight")
	fingerprint := personCreateFingerprint(cmd.TenantID, cmd.NormalizedEmail, cmd.FirstName, cmd.LastName, cmd.Role, cmd.ScopeType, cmd.ScopeID, cmd.DepartmentID, cmd.DesignationGrade)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := reserveIdempotency(ctx, tx, cmd.TenantID, "create_person", cmd.IdempotencyKey, fingerprint); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reserve in-flight key: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit in-flight reservation: %v", err)
	}

	if _, err := repo.PreflightCreatePerson(ctx, ports.PreflightCreatePersonCommand{
		TenantID:        cmd.TenantID,
		IdempotencyKey:  cmd.IdempotencyKey,
		NormalizedEmail: cmd.NormalizedEmail,
		FirstName:       cmd.FirstName,
		LastName:        cmd.LastName,
		Role:            cmd.Role,
		ScopeType:       cmd.ScopeType,
		ScopeID:         cmd.ScopeID,
	}); !errors.Is(err, ports.ErrIdempotencyInFlight) {
		t.Fatalf("preflight exact replay while started must return ErrIdempotencyInFlight, got %v", err)
	}
	if _, err := repo.CreatePerson(ctx, cmd); !errors.Is(err, ports.ErrIdempotencyInFlight) {
		t.Fatalf("create exact replay while started must return ErrIdempotencyInFlight, got %v", err)
	}
}

// TestListPeopleKeysetPagesInNameOrderWithDockerPostgres pins the directory's
// keyset pagination: stable (lower(display_name), member_id) ordering, a
// next_cursor exactly when more rows remain, and no row skipped or repeated
// across pages.
func TestListPeopleKeysetPagesInNameOrderWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	seedPeoplePark(t, ctx, pool)

	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, 'people-page-' || $2, $2, 'active', 'operator', $3::uuid)`,
			peopleTenant, name, peoplePark); err != nil {
			t.Fatalf("seed member %s: %v", name, err)
		}
	}

	params := ports.ListPeopleParams{TenantID: peopleTenant, ParkID: peoplePark, Limit: 2}
	firstPage, cursor, err := repo.ListPeople(ctx, params)
	if err != nil {
		t.Fatalf("ListPeople page 1: %v", err)
	}
	if len(firstPage) != 2 || cursor == "" {
		t.Fatalf("page 1: want 2 rows + cursor, got %d rows cursor=%q", len(firstPage), cursor)
	}
	if firstPage[0].DisplayName != "alpha" || firstPage[1].DisplayName != "bravo" {
		t.Fatalf("page 1 order = %s,%s", firstPage[0].DisplayName, firstPage[1].DisplayName)
	}

	params.Cursor = cursor
	secondPage, next, err := repo.ListPeople(ctx, params)
	if err != nil {
		t.Fatalf("ListPeople page 2: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].DisplayName != "charlie" || next != "" {
		t.Fatalf("page 2: want [charlie] and no cursor, got %d rows next=%q", len(secondPage), next)
	}

	params.Cursor = "not-a-cursor"
	if _, _, err := repo.ListPeople(ctx, params); !errors.Is(err, ports.ErrInvalidFilter) {
		t.Fatalf("garbage cursor must return ErrInvalidFilter, got %v", err)
	}
}
