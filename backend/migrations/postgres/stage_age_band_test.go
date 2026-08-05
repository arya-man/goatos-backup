package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestStageAgeBandClassification pins the kid/adult classification that migration 000109 applies to
// the stage vocabulary, by executing the migration's OWN classification SQL rather than restating it.
//
// The classification is CONFIG, so a farm may add cohorts without a release — but the assignments for
// the cohorts that exist today are a maintainer decision, and two of them are easy to "fix" wrongly
// from plausible-looking evidence:
//
//   - F2/F2-Male/F2-Female are KID cohorts even though those animals run to 67 weeks old in the live
//     herd. Anyone re-deriving the band from age, or from min_age_days/max_age_days, flips 667
//     animals to adult against the farm's own record.
//   - Warmup is a KID cohort by maintainer decision 2026-08-05, deliberately CONTRADICTING the source
//     sheet, which labels its one live Warmup animal Adult. A future session re-deriving the
//     vocabulary from that sheet will silently reverse it.
//
// Both facts live in the migration header as prose, and prose is not a gate. This is the gate: the
// test seeds the vocabulary UNCLASSIFIED and lets the migration file's own statements do the
// classifying, so editing those statements is what makes it fail.
func TestStageAgeBandClassification(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	// The baseline tenant, so the vocabulary rows satisfy animal_stage_lookup_tenant_id_fkey.
	const tenantID = "00000000-0000-4000-8000-000000000001"

	// Seed every cohort with NO band. The migration already ran (on an empty vocabulary), so these
	// rows are unclassified until its statements are replayed below.
	for _, code := range []string{
		"K0", "K1", "K2", "K3", "F2", "F2-Male", "F2-Female", "Warmup",
		"Buck", "Mother", "Milking", "M0", "Pregnant", "Non-Pregnant",
		"ICU", "Quarantine",
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, status)
VALUES ($1::uuid, $2, $2, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE SET age_band = NULL`, tenantID, code); err != nil {
			t.Fatalf("seed stage %s: %v", code, err)
		}
	}

	// Replay the migration's Up section verbatim. Every statement in it is idempotent (ADD COLUMN IF
	// NOT EXISTS, DROP CONSTRAINT IF EXISTS, and UPDATEs guarded by IS DISTINCT FROM), and the goats
	// backfill is a no-op here because no goat carries these cohorts in this database.
	raw, err := os.ReadFile("000109_stage_age_band.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up, _, found := strings.Cut(string(raw), "-- +goose Down")
	if !found {
		t.Fatal("migration 000109 has no goose Down marker")
	}
	if _, err := pool.Exec(ctx, strings.Replace(up, "-- +goose Up", "", 1)); err != nil {
		t.Fatalf("replay migration 000109 Up: %v", err)
	}

	bandOf := func(code string) string {
		t.Helper()
		var band string
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(age_band, '') FROM animal_stage_lookup WHERE tenant_id=$1::uuid AND stage_code=$2`,
			tenantID, code).Scan(&band); err != nil {
			t.Fatalf("read stage %s: %v", code, err)
		}
		return band
	}

	for code, want := range map[string]string{
		"K0": "kid", "K1": "kid", "K2": "kid", "K3": "kid",
		// Fattening cohorts: kid regardless of age. Not derivable from min_age_days/max_age_days.
		"F2": "kid", "F2-Male": "kid", "F2-Female": "kid",
		// Maintainer override, deliberately against the source sheet.
		"Warmup": "kid",
		"Buck":   "adult", "Mother": "adult", "Milking": "adult",
		"M0": "adult", "Pregnant": "adult", "Non-Pregnant": "adult",
		// Clinical cohorts stay unclassified so a clinical placement never reclassifies an animal.
		"ICU": "", "Quarantine": "",
	} {
		if got := bandOf(code); got != want {
			t.Errorf("stage %s age_band=%q, want %q — see migration 000109's header before changing this", code, got, want)
		}
	}

	// The domain is closed: 'kid', 'adult', or NULL. Free text would let the source sheet's
	// 'Kid'/'Adult' casing back in and quietly split every consumer's comparison.
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, status, age_band)
VALUES ($1::uuid, 'BogusBand', 'BogusBand', 'active', 'Kid')`, tenantID); err == nil {
		t.Error("animal_stage_lookup accepted age_band='Kid', want the CHECK constraint to reject anything outside ('kid','adult')")
	}
}
