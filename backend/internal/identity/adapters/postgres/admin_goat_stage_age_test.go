package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THE STAGE A PERSON CHOOSES IS SAVED AS CHOSEN (maintainer instruction 2026-09-26). Register
// animal and the bulk upload used to rewrite it from the vaccination kid/adult age: a K-tagged
// animal older than 19 weeks became "Adult" -- a stage the farm does not have, so the save failed --
// and a young F2 or Warmup animal silently became K1. Now the stage is checked only against the
// age range written on it in Items & settings:
//
//   - inside the range, or a stage with no range (K3 onwards, F2, adults): accepted;
//   - plainly outside the range (a K1 six months old): refused, naming the range and the age;
//   - a birth: never refused, because the system pins its stage and the person did not choose it.
func TestAdminGoatStageIsCheckedAgainstItsOwnAgeRange(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	_ = seedShedStageFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, min_age_days, max_age_days, status) VALUES
($1::uuid, 'K0', 'Newborn kid', 0, 1, 'active'),
($1::uuid, 'K1', 'Milk training', 2, 7, 'active'),
($1::uuid, 'K3', 'Weaned kids', 43, NULL, 'active'),
($1::uuid, 'F2-Male', 'Fattening male', NULL, NULL, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE
  SET name = EXCLUDED.name, min_age_days = EXCLUDED.min_age_days, max_age_days = EXCLUDED.max_age_days, status = 'active'`, ssTenant); err != nil {
		t.Fatalf("seed stages: %v", err)
	}

	stageConflict := func(stage string, ageDays int, origin string) string {
		t.Helper()
		dob := time.Now().AddDate(0, 0, -ageDays)
		code := stage
		validation, err := repo.ValidateAdminGoatCreate(ctx, ports.ValidateAdminGoatCreateCommand{
			TenantID:        ssTenant,
			ManagementStage: &code,
			DOB:             &dob,
			OriginType:      origin,
		})
		if err != nil {
			t.Fatalf("validate %s at %d days: %v", stage, ageDays, err)
		}
		for _, c := range validation.Conflicts {
			if c.Field == "management_stage" {
				return c.Code + ": " + c.Message
			}
		}
		return ""
	}

	// A K3 at 22 weeks used to be rewritten to "Adult" and refused; K3 has no upper age.
	if got := stageConflict("K3", 154, "procured"); got != "" {
		t.Fatalf("a 22-week K3 must save as K3, got %q", got)
	}
	// A young F2-Male used to be silently rewritten to K1; F2 has no range at all.
	if got := stageConflict("F2-Male", 70, "procured"); got != "" {
		t.Fatalf("a 10-week F2-Male must save as F2-Male, got %q", got)
	}
	// Inside the range.
	if got := stageConflict("K1", 5, "procured"); got != "" {
		t.Fatalf("a 5-day K1 fits 2-7 days, got %q", got)
	}
	// Plainly outside the range: refused with the range and the age in farm words.
	got := stageConflict("K1", 180, "procured")
	if !strings.HasPrefix(got, "age_out_of_range: ") || !strings.Contains(got, "K1 (Milk training) is for animals 2 to 7 days old") || !strings.Contains(got, "180 days old") {
		t.Fatalf("a six-month K1 must be refused naming the range and age, got %q", got)
	}
	if got := stageConflict("K3", 20, "procured"); !strings.Contains(got, "43 days old or more") {
		t.Fatalf("a 20-day K3 is younger than the range, got %q", got)
	}
	// A birth recorded a few days late keeps its pinned K0.
	if got := stageConflict("K0", 4, "birth"); got != "" {
		t.Fatalf("a birth's pinned K0 is never refused on age, got %q", got)
	}
	// No date of birth: nothing to check.
	code := "K1"
	validation, err := repo.ValidateAdminGoatCreate(ctx, ports.ValidateAdminGoatCreateCommand{TenantID: ssTenant, ManagementStage: &code})
	if err != nil {
		t.Fatalf("validate without dob: %v", err)
	}
	for _, c := range validation.Conflicts {
		if c.Field == "management_stage" {
			t.Fatalf("no date of birth means no age check, got %+v", c)
		}
	}
}
