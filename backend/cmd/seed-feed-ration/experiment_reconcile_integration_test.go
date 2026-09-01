package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// seedExperiments' RECONCILE path, executed against a real database.
//
// The unit tests around this command cover the arithmetic (workbookGramsPerHead) and the generator
// covers what the rates mean, but nothing executed the batch itself -- and the batch is where a
// statement can be well-formed Go and still be a broken query. It was: the UPDATE referenced a
// parameter the Queue call never passed, so the FIRST workbook-owned row that needed correcting
// failed the whole transaction and a re-seed could no longer reconcile anything. It reached review
// green because every test in this package stops short of the SQL.
//
// The test drives the real function twice: once into an empty table (insert), once over the row it
// just wrote with a CHANGED quantity (update). The second pass is the one that used to explode.
func TestSeedExperimentsReconcilesAnExistingWorkbookRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant = "f2390000-0000-4000-8000-000000000001"
		park   = "f2390000-0000-4000-8000-000000003001"
		shed   = "f2390000-0000-4000-8000-000000004001"
	)

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec failed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Seed Experiment Reconcile', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenant)
	exec(`INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, NULL, 'park', 'F239-P', 'F239 Park', 'active'),
       ($3::uuid, $1::uuid, $2::uuid, 'shed', 'F239-S', 'Mandela 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, tenant, park, shed)

	// A PARTITIONED pen, because the parameter this regression is about is the partition key: an
	// undivided shed sends a blank label and would have taken a different branch of the CASE.
	var partyID string
	if err := pool.QueryRow(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES (gen_random_uuid(), 'org', 'F239 Custodian', 'active')
RETURNING party_id::text`).Scan(&partyID); err != nil {
		t.Fatalf("insert custodian party: %v", err)
	}
	exec(`INSERT INTO goats (tenant_id, species, sex, lifecycle_status, custodian_party_id, shed_id)
SELECT $1::uuid, 'goat', 'female', 'alive', $2::uuid, $3::uuid FROM generate_series(1, 10)`, tenant, partyID, shed)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label)
SELECT $1::uuid, goat_id, $2::uuid, 'Mandela 1', 'Part 3' FROM goats WHERE tenant_id = $1::uuid`, tenant, shed)

	shedIDs := map[string]string{"CBE" + "\x1f" + configNormKey("Mandela 1"): shed}
	run := func(kg float64) *stats {
		t.Helper()
		st := &stats{}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		rows := []experimentShed{{
			Farm:     "CBE",
			Shed:     "Mandela 1 - Part 3",
			Count:    10,
			Category: "Sheep M NEW",
			Kg:       map[string]float64{"Dry Masoor Bhusa": kg},
		}}
		if err := seedExperiments(ctx, tx, tenant, rows, shedIDs, st); err != nil {
			t.Fatalf("seedExperiments(%v kg): %v", kg, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		return st
	}
	readRate := func() string {
		t.Helper()
		var grams string
		if err := pool.QueryRow(ctx, `
SELECT grams_per_head::text FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND partition_key = feed_config_norm('Part 3')`,
			tenant, shed).Scan(&grams); err != nil {
			t.Fatalf("read rate: %v", err)
		}
		return grams
	}

	// FIRST PASS -- insert. 2 kg across the pen's 10 live animals.
	if st := run(2); st.ExperimentRowsInserted != 1 {
		t.Fatalf("first pass inserted %d rows, want 1", st.ExperimentRowsInserted)
	}
	if got := readRate(); got != "200.000" {
		t.Fatalf("inserted rate = %q, want 200.000", got)
	}

	// SECOND PASS -- the workbook figure changed, so the existing row must be CORRECTED in place.
	// This is the statement that referenced a parameter it was never passed.
	if st := run(3); st.ExperimentRowsUpdated != 1 {
		t.Fatalf("second pass updated %d rows, want 1 -- a re-seed must reconcile a workbook row it already owns", st.ExperimentRowsUpdated)
	}
	if got := readRate(); got != "300.000" {
		t.Fatalf("corrected rate = %q, want 300.000", got)
	}

	// THIRD PASS -- unchanged input is a no-op, which is what makes a re-seed safe to repeat.
	if st := run(3); st.ExperimentRowsUpdated != 0 || st.ExperimentRowsInserted != 0 {
		t.Fatalf("third pass wrote (inserted=%d updated=%d), want a no-op", st.ExperimentRowsInserted, st.ExperimentRowsUpdated)
	}

	// A CELL THE APP OWNS IS NEVER RE-ASSERTED, and the reconcile path is where that guarantee lives.
	exec(`UPDATE feed_experiment_config SET source = 'app', grams_per_head = 999.000
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`, tenant, shed)
	run(4)
	if got := readRate(); got != "999.000" {
		t.Fatalf("app-authored rate = %q, want the untouched 999.000 -- a re-seed must not discard a rate the farm corrected on screen", got)
	}

	var _ pgx.Tx
}
