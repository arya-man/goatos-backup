package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// TestSeedMultiDoseKidCourseAcceptsBothDosesWithoutRestartOrSkip is R50-001: seed()
// sorts source cells by date (sort.SliceStable) and accumulates each accepted
// completion into acceptedHistoryByAnimalKey BEFORE classifying the next cell for
// that same animal, so a second (or later) dose for one AnimalKey sees its own
// prior accepted history when resolving schedule path.
//
// This animal's DOB puts its first ET+TT dose at ~10 weeks old (unambiguously kid
// path) and its second dose at ~18 weeks old -- inside the 16-19w
// "continuation-only" window (SchedulePathForGoat), where the goat's CURRENT
// stage tag is "Adult" (not a kid-management stage). Without the dose-1 history
// carried forward, dose 2 would misclassify onto the adult path -- either
// restarting the course (re-using the first birth-age dose code) or skipping the
// birth-age course entirely (landing on an adult wave / post_arrival dose code).
// With the fix, dose 2 must land on the birth-age course's SECOND wave dose code.
func TestSeedMultiDoseKidCourseAcceptsBothDosesWithoutRestartOrSkip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	pgCfg := platformpg.Config{QueryTimeout: 5 * time.Second}

	dob := time.Date(2025, 1, 1, 0, 0, 0, 0, loc)
	dose1Date := dob.AddDate(0, 0, 70)  // ~10 weeks: unambiguously kid
	dose2Date := dob.AddDate(0, 0, 126) // ~18 weeks: continuation-only zone

	const animalKey = "R50001-MULTIDOSE"
	goat := goatRecord{
		RFID:           animalKey,
		Farm:           "R50Farm",
		Shed:           "R50Shed",
		Stage:          "Adult", // CURRENT stage is Adult, not a kid tag -- the fix must rely on history, not the stage tag
		Age:            "Adult",
		Breed:          "Sojat",
		Gender:         "Female",
		DOB:            dob.Format("2006-01-02"),
		StageEntryDate: dob.Format("2006-01-02"), // entry == DOB: no DOB-after-entry violation
		OriginType:     "birth",
		Species:        "goat",
		Status:         "Alive",
	}
	cells := []vaccCell{
		{AnimalKey: animalKey, Vaccine: "ET+TT", DoseCode: "first", Sequence: 1, Value: dose1Date.Format("2006-01-02")},
		{AnimalKey: animalKey, Vaccine: "ET+TT", DoseCode: "booster", Sequence: 2, Value: dose2Date.Format("2006-01-02")},
	}

	st, err := seed(ctx, pool, pgCfg, defaultTenantID, loc, []goatRecord{goat}, cells, false, false, "", false, -1)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if st.Completed != 2 || st.CompletionsHistory != 2 {
		t.Fatalf("stats = %+v, want 2 completed history doses", st)
	}
	if st.Skipped != 0 || st.UnresolvedDatedFacts != 0 {
		t.Fatalf("stats = %+v, want zero skipped/unresolved (neither dose should be dropped)", st)
	}

	goatID := detUUID("goat", defaultTenantID, animalKey)
	rows, err := pool.Query(ctx, `
		SELECT pr.dose_code
		FROM obligation_instances oi
		JOIN protocol_rules pr ON pr.rule_id = oi.rule_id
		WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND oi.status='completed'
		ORDER BY oi.due_at`, defaultTenantID, goatID)
	if err != nil {
		t.Fatalf("query completed obligations: %v", err)
	}
	defer rows.Close()
	var doseCodes []string
	for rows.Next() {
		var doseCode string
		if err := rows.Scan(&doseCode); err != nil {
			t.Fatalf("scan dose_code: %v", err)
		}
		doseCodes = append(doseCodes, doseCode)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read completed obligations: %v", err)
	}

	if len(doseCodes) != 2 {
		t.Fatalf("completed obligations = %v, want exactly 2 (one per dose)", doseCodes)
	}
	// Neither dose collapsed onto the adult path (that would be the "skip" failure mode: the
	// birth-age course's obligation never gets created because the cell recorded under an
	// unrelated adult rule instead).
	for _, dc := range doseCodes {
		if !strings.Contains(dc, "_kid_") {
			t.Fatalf("completed dose_codes = %v, want both under the birth-age (_kid_) course, got adult/other dose_code %q", doseCodes, dc)
		}
	}
	// Dose 1 and dose 2 must land on DIFFERENT birth-age waves -- if the course had "restarted",
	// both would collapse onto the same (first) dose_code.
	if doseCodes[0] == doseCodes[1] {
		t.Fatalf("both doses mapped to the SAME dose_code %q -- course restarted instead of progressing to the second wave", doseCodes[0])
	}
	if doseCodes[0] != "et_tt_kid_4w" || doseCodes[1] != "et_tt_kid_7w" {
		t.Fatalf("completed dose_codes = %v, want [et_tt_kid_4w et_tt_kid_7w] in order", doseCodes)
	}
}

// TestSeedDobNullCountGateFailureRollsBackAnimalStageLookup is R50-003: the
// -expect-null-false-dob count gate is checked inside the SAME "animal seed tx"
// transaction that seedAnimalStageLookup's catalog writes now run in (moved there
// specifically so a failed gate rolls back both together -- see the comment at
// that call site in seed()). Forcing the gate to fail (by demanding a count that
// does not match reality) must roll back the freshly (re)inserted
// animal_stage_lookup rows together with the rest of that transaction, not leave
// them durably committed while only the seed_runs sentinel records the failure.
func TestSeedDobNullCountGateFailureRollsBackAnimalStageLookup(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	pgCfg := platformpg.Config{QueryTimeout: 5 * time.Second}

	// No goats/cells at all: the actual DobNulled count is 0. Demanding 1 forces the count gate
	// at the end of the animal seed tx to fail.
	_, err = seed(ctx, pool, pgCfg, defaultTenantID, loc, nil, nil, false, false, "", false, 1)
	if err == nil {
		t.Fatal("seed with mismatched -expect-null-false-dob = nil error, want count gate failure")
	}
	if !strings.Contains(err.Error(), "dob_nulled count gate failed") {
		t.Fatalf("seed error = %v, want dob_nulled count gate failure", err)
	}

	if got := countRows(t, ctx, pool, `SELECT count(*) FROM animal_stage_lookup WHERE tenant_id=$1`, defaultTenantID); got != 0 {
		t.Fatalf("animal_stage_lookup rows survived a rolled-back seed tx: %d rows, want 0", got)
	}
	// The seed_runs sentinel IS expected to survive -- it lives on the pool, outside the rolled-back
	// tx, and is the deliberate audit trail of the failure. That survivor is not itself proof of a
	// correct rollback; animal_stage_lookup emptiness above is the actual regression guard.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM seed_runs WHERE tenant_id=$1`, defaultTenantID); got == 0 {
		t.Fatal("expected the seed_runs sentinel row to survive even though the data tx rolled back")
	}
}

// TestPersistDobDispositionProofWritesEmptyArrayNotNullAndRollsBackWithTx is
// R50-005: encoding/json marshals a nil Go slice as the JSON literal `null`. seed()
// declares `var dobDispositions []dobDisposition` and never re-assigns it when
// nothing was nulled, so persistDobDispositionProofInTx must explicitly normalize
// a nil `entries` to an empty, non-nil slice before marshaling -- otherwise a
// clean run's audit proof reads `"dob_dispositions": null`, indistinguishable
// from "this run never recorded a disposition list at all". The write must also
// be a normal participant in the caller's transaction: rolled back with it, not a
// side-channel that survives independently.
func TestPersistDobDispositionProofWritesEmptyArrayNotNullAndRollsBackWithTx(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const runID = "aaaaaaaa-0000-4000-8000-0000000000f5"
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-dob-proof-test','active') ON CONFLICT (tenant_id) DO NOTHING`, defaultTenantID)
	mustExec(t, ctx, pool,
		`INSERT INTO seed_runs (seed_run_id, tenant_id, command, state) VALUES ($1,$2,'seed-vaccination-real','loading')`,
		runID, defaultTenantID)

	// Rollback path: a write inside an uncommitted tx that is then rolled back must leave
	// seed_runs.detail untouched -- proving this is a normal transactional write, not a
	// side-channel independent of the caller's tx outcome.
	txRollback, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback tx: %v", err)
	}
	defer func() { _ = txRollback.Rollback(ctx) }()
	if err := persistDobDispositionProofInTx(ctx, txRollback, runID, nil); err != nil {
		t.Fatalf("persist (to be rolled back): %v", err)
	}
	if err := txRollback.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var detailAfterRollback string
	if err := pool.QueryRow(ctx, `SELECT detail::text FROM seed_runs WHERE seed_run_id=$1`, runID).Scan(&detailAfterRollback); err != nil {
		t.Fatalf("read detail after rollback: %v", err)
	}
	if strings.Contains(detailAfterRollback, "dob_dispositions") {
		t.Fatalf("detail carries dob_dispositions after rollback, want untouched default: %s", detailAfterRollback)
	}

	// Commit path: nil entries (nothing nulled) must persist as [] , never null.
	txCommit, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin commit tx: %v", err)
	}
	defer func() { _ = txCommit.Rollback(ctx) }()
	if err := persistDobDispositionProofInTx(ctx, txCommit, runID, nil); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if err := txCommit.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var dispositions string
	if err := pool.QueryRow(ctx, `SELECT detail->>'dob_dispositions' FROM seed_runs WHERE seed_run_id=$1`, runID).Scan(&dispositions); err != nil {
		t.Fatalf("read committed detail: %v", err)
	}
	if strings.TrimSpace(dispositions) != "[]" {
		t.Fatalf("detail.dob_dispositions = %q, want empty array []", dispositions)
	}
}
