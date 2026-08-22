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
