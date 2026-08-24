package postgres

import (
	"context"
	"strconv"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestMilkPreparationMigrationAcceptsOnlyTwoOrFiveProofs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	var tenantID, parkID, shedID string
	if err := pool.QueryRow(ctx, `
SELECT tenant_id::text, parent_location_id::text, location_id::text
FROM public.locations
WHERE location_type = 'shed' AND parent_location_id IS NOT NULL
ORDER BY location_id
LIMIT 1
`).Scan(&tenantID, &parkID, &shedID); err != nil {
		t.Fatalf("load seeded shed: %v", err)
	}

	var completionID string
	if err := pool.QueryRow(ctx, `
INSERT INTO public.milk_preparation_completions (
  tenant_id, park_id, preparation_date, feeding_date, submitted_by
) VALUES (
  $1::uuid, $2::uuid,
  DATE '2026-07-29', DATE '2026-07-30',
  '90000000-0000-4000-8000-000000000101'
)
RETURNING completion_id::text
`, tenantID, parkID).Scan(&completionID); err != nil {
		t.Fatalf("insert completion: %v", err)
	}

	insertAttempt := func(attempt int, refs string) error {
		_, err := pool.Exec(ctx, `
INSERT INTO public.milk_preparation_proof_attempts (
  tenant_id, completion_id, attempt_no, goat_milk_used, proof_refs,
  submitted_by, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2::uuid, $3, false, $4::jsonb,
  '90000000-0000-4000-8000-000000000101', $5, $6
)
`, tenantID, completionID, attempt, refs, "milk-proof-attempt-"+strconv.Itoa(attempt), "fingerprint-"+strconv.Itoa(attempt))
		return err
	}

	if err := insertAttempt(1, `{"heat":"proof-1","mix":"proof-2"}`); err != nil {
		t.Fatalf("two-proof attempt was rejected: %v", err)
	}
	if err := insertAttempt(2, `{"heat":"proof-1","mix":"proof-2","milk":"proof-3","acid":"proof-4","finish":"proof-5"}`); err != nil {
		t.Fatalf("five-proof attempt was rejected: %v", err)
	}
	if err := insertAttempt(3, `{"heat":"proof-1","mix":"proof-2","finish":"proof-3"}`); err == nil {
		t.Fatal("three-proof attempt passed the database constraint")
	}
}

// Milk Preparation is ONE farm-day action (migration 000097). The shed-grain rows written by
// 000056/000057 stay as immutable history and are retired; an ACTIVE completion carries no shed at
// all, and there is exactly one per park per preparation date.
func TestMilkPreparationMigrationMakesFarmDayTheOnlyActiveTaskGrain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	var tenantID, parkID, shedID string
	if err := pool.QueryRow(ctx, `
SELECT tenant_id::text, parent_location_id::text, location_id::text
FROM public.locations
WHERE location_type = 'shed' AND parent_location_id IS NOT NULL
ORDER BY parent_location_id, location_id
LIMIT 1`).Scan(&tenantID, &parkID, &shedID); err != nil {
		t.Fatalf("load a seeded shed: %v", err)
	}

	insertFarmDay := func(day string) error {
		_, err := pool.Exec(ctx, `INSERT INTO public.milk_preparation_completions
(tenant_id, park_id, preparation_date, feeding_date, submitted_by)
VALUES ($1::uuid,$2::uuid,$3::date,$3::date + 1,'90000000-0000-4000-8000-000000000101')`,
			tenantID, parkID, day)
		return err
	}
	if err := insertFarmDay("2026-07-29"); err != nil {
		t.Fatalf("farm-day completion: %v", err)
	}
	if err := insertFarmDay("2026-07-29"); err == nil {
		t.Fatal("a second active completion for the same park-day was accepted")
	}
	if err := insertFarmDay("2026-07-30"); err != nil {
		t.Fatalf("next day is its own completion: %v", err)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO public.milk_preparation_completions
(tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by)
VALUES ($1::uuid,$2::uuid,$3::uuid,'2026-07-31','2026-08-01','90000000-0000-4000-8000-000000000101')`,
		tenantID, parkID, shedID); err == nil {
		t.Fatal("an ACTIVE shed-grain completion was accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.milk_preparation_completions
(tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by, status)
VALUES ($1::uuid,$2::uuid,$3::uuid,'2026-07-31','2026-08-01','90000000-0000-4000-8000-000000000101','retired')`,
		tenantID, parkID, shedID); err != nil {
		t.Fatalf("retired shed-grain history must still be writable: %v", err)
	}
}
