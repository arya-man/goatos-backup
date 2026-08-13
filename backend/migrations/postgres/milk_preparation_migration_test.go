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
  tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid,
  DATE '2026-07-29', DATE '2026-07-30',
  '90000000-0000-4000-8000-000000000101'
)
RETURNING completion_id::text
`, tenantID, parkID, shedID).Scan(&completionID); err != nil {
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

func TestMilkPreparationMigrationMakesShedDayTheOnlyActiveTaskGrain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	var tenantID, parkID, shedA, shedB string
	rows, err := pool.Query(ctx, `
SELECT tenant_id::text, parent_location_id::text, location_id::text
FROM public.locations
WHERE location_type = 'shed' AND parent_location_id IS NOT NULL
ORDER BY parent_location_id, location_id`)
	if err != nil {
		t.Fatalf("list sheds: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tenant, park, shed string
		if err := rows.Scan(&tenant, &park, &shed); err != nil {
			t.Fatalf("scan shed: %v", err)
		}
		if shedA == "" {
			tenantID, parkID, shedA = tenant, park, shed
			continue
		}
		if tenant == tenantID && park == parkID {
			shedB = shed
			break
		}
	}
	if shedB == "" {
		t.Skip("fixture has no park with two sheds")
	}

	insert := func(shed, day string) error {
		_, err := pool.Exec(ctx, `INSERT INTO public.milk_preparation_completions
(tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by)
VALUES ($1::uuid,$2::uuid,$3::uuid,$4::date,$4::date + 1,'90000000-0000-4000-8000-000000000101')`,
			tenantID, parkID, shed, day)
		return err
	}
	if err := insert(shedA, "2026-07-29"); err != nil {
		t.Fatalf("shed A: %v", err)
	}
	if err := insert(shedB, "2026-07-29"); err != nil {
		t.Fatalf("shed B same park/day: %v", err)
	}
	if err := insert(shedA, "2026-07-29"); err == nil {
		t.Fatal("duplicate shed-day was accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.milk_preparation_completions
(tenant_id,park_id,preparation_date,feeding_date,submitted_by)
VALUES ($1::uuid,$2::uuid,'2026-07-30','2026-07-31','90000000-0000-4000-8000-000000000101')`, tenantID, parkID); err == nil {
		t.Fatal("active completion without shed was accepted")
	}
}
