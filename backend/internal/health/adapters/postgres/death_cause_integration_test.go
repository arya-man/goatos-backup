package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THE RULE THE MAINTAINER STATED, proved against the database:
//
//	"sometimes we are generating multiple diseases for one animal. But if we close that in
//	 any one disease, all other diseases should be also gone, and it will be under that
//	 disease only."
//
// So: every open case closes, and exactly ONE is counted as the cause.
func TestDeathUnderOneDiseaseClosesTheOthersAndIsCountedUnderThatOneOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)

	// The animal is being treated for THREE things at once — the shape that made a cause of
	// death unanswerable before this existed.
	diagnoseFever(t, ctx, pool, healthGoat, "obs-three-way")
	for _, extra := range []struct{ rule, card, name, key string }{
		{"MASTITIS", "mastitis", "Mastitis", "extra-mastitis"},
		{"BLOAT", "bloat", "Bloat", "extra-bloat"},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT c.tenant_id, c.goat_id, c.health_protocol_version_id, $3, $4,
       c.age_band, c.start_date, c.duration_days, 'active', c.park_id, c.shed_id,
       $5, $6, $6
FROM health_cases c
WHERE c.tenant_id = $1::uuid AND c.goat_id = $2::uuid
LIMIT 1`, healthTenant, healthGoat, extra.card, extra.name, extra.rule, extra.key); err != nil {
			t.Fatalf("seed %s case: %v", extra.rule, err)
		}
	}
	assertHealthCount(t, ctx, pool, "open cases before the death",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND status='active'`, 3, healthTenant, healthGoat)

	// The operator names MASTITIS as what killed it.
	cause := domain.DeathCause{Key: "MASTITIS", Kind: domain.DeathCauseKindRegisterRule}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat, cause); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}

	// ALL THREE ARE GONE. None is left open for a course nobody can carry out.
	assertHealthCount(t, ctx, pool, "cases still open",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND status NOT IN ('closed_dead','recovered','canceled')`,
		0, healthTenant, healthGoat)
	assertHealthCount(t, ctx, pool, "cases closed by the death",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND status='closed_dead'`,
		3, healthTenant, healthGoat)

	// EXACTLY ONE is the cause, and it is the one named.
	assertHealthCount(t, ctx, pool, "cases marked as the cause",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND is_death_cause`,
		1, healthTenant, healthGoat)
	var causeRule string
	if err := pool.QueryRow(ctx, `
SELECT register_rule_id FROM health_cases
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND is_death_cause`, healthTenant, healthGoat).Scan(&causeRule); err != nil {
		t.Fatalf("read the cause case: %v", err)
	}
	if causeRule != "MASTITIS" {
		t.Fatalf("the death is counted under %q, want MASTITIS", causeRule)
	}

	// The animal's remaining treatment work stops with the cases.
	assertHealthCount(t, ctx, pool, "sessions still owed",
		`SELECT count(*) FROM health_treatment_sessions WHERE tenant_id=$1 AND goat_id=$2 AND status IN ('scheduled','due','in_progress','rework')`,
		0, healthTenant, healthGoat)
}

// THE RELAPSE. An animal treated for mastitis, recovered, treated for it AGAIN, and dead
// with both episodes on record has TWO cases carrying the same disease — and exactly one
// of them may wear the cause flag.
//
// This is the fixture that discriminates. Every other test here has one case per disease,
// so a mark-every-match implementation passes them all: it was written, the suite stayed
// green, and only this case turns it red. The correct behaviour picks the LATEST-started
// episode, which is the one the animal was in when it died; the pick cannot change the
// reported CAUSE, because every candidate carries the same disease by construction.
func TestARelapseLeavesExactlyOneCaseWearingTheCause(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, healthGoat, "obs-relapse")

	// Two MASTITIS episodes, an older one and the one it died in.
	for _, episode := range []struct{ key, start string }{
		{"relapse-old", "2026-08-01"},
		{"relapse-new", "2026-08-20"},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT c.tenant_id, c.goat_id, c.health_protocol_version_id, 'mastitis', 'Mastitis',
       c.age_band, $3::date, c.duration_days, 'active', c.park_id, c.shed_id,
       'MASTITIS', $4, $4
FROM health_cases c WHERE c.tenant_id=$1::uuid AND c.goat_id=$2::uuid LIMIT 1`,
			healthTenant, healthGoat, episode.start, episode.key); err != nil {
			t.Fatalf("seed %s: %v", episode.key, err)
		}
	}

	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat,
		domain.DeathCause{Key: "MASTITIS", Kind: domain.DeathCauseKindRegisterRule}); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}

	// ONE flag across two identical-disease episodes.
	assertHealthCount(t, ctx, pool, "cases marked as the cause",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND is_death_cause`,
		1, healthTenant, healthGoat)
	// And it is the episode the animal died in.
	var start time.Time
	if err := pool.QueryRow(ctx, `
SELECT start_date FROM health_cases
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND is_death_cause`, healthTenant, healthGoat).Scan(&start); err != nil {
		t.Fatalf("read the cause case: %v", err)
	}
	if got := start.Format("2006-01-02"); got != "2026-08-20" {
		t.Errorf("the cause is the episode started %s, want the latest one the animal died in", got)
	}
}

// A NORMAL death still closes every case and marks NO cause. This is the path every death
// took before causes existed, and it must be untouched: a farm that never uses the disease
// toggle sees exactly the behaviour it has always had.
func TestNormalDeathClosesEveryCaseAndNamesNoCause(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, healthGoat, "obs-normal-death")

	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat, domain.DeathCause{}); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}
	assertHealthCount(t, ctx, pool, "cases closed by the death",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND status='closed_dead'`,
		1, healthTenant, healthGoat)
	assertHealthCount(t, ctx, pool, "cases marked as a cause",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND is_death_cause`,
		0, healthTenant, healthGoat)
}

// A cause naming a disease the animal has NO case for is not an error. That is the normal
// shape for a death raised on the Counts form, where the operator names what they saw and
// the animal was never opened a case for it: the cause is still recorded on the ANIMAL, and
// no case is falsely marked.
func TestACauseWithNoMatchingCaseMarksNothingAndDoesNotFail(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, healthGoat, "obs-unmatched-cause")

	cause := domain.DeathCause{Key: "TETANUS", Kind: domain.DeathCauseKindRegisterRule}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat, cause); err != nil {
		t.Fatalf("a cause with no matching case must not fail the death: %v", err)
	}
	assertHealthCount(t, ctx, pool, "cases closed by the death",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND status='closed_dead'`,
		1, healthTenant, healthGoat)
	// The FEVER case must NOT be dressed up as tetanus.
	assertHealthCount(t, ctx, pool, "cases marked as the cause",
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1 AND goat_id=$2 AND is_death_cause`,
		0, healthTenant, healthGoat)
}

// The database refuses a second cause even if a future write path tries to set one, so the
// mortality board can never count one death under two diseases.
func TestOnlyOneCasePerAnimalCanBeTheCause(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, healthGoat, "obs-one-cause")
	if _, err := pool.Exec(ctx, `
INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name,
                          age_band, start_date, duration_days, status, park_id, shed_id,
                          register_rule_id, idempotency_key, request_fingerprint)
SELECT c.tenant_id, c.goat_id, c.health_protocol_version_id, 'mastitis', 'Mastitis',
       c.age_band, c.start_date, c.duration_days, 'active', c.park_id, c.shed_id,
       'MASTITIS', 'one-cause-second', 'one-cause-second'
FROM health_cases c WHERE c.tenant_id=$1::uuid AND c.goat_id=$2::uuid LIMIT 1`,
		healthTenant, healthGoat); err != nil {
		t.Fatalf("seed second case: %v", err)
	}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat,
		domain.DeathCause{Key: "MASTITIS", Kind: domain.DeathCauseKindRegisterRule}); err != nil {
		t.Fatalf("close: %v", err)
	}

	// A hand-written second cause is refused by the partial unique index.
	_, err := pool.Exec(ctx, `
UPDATE health_cases SET is_death_cause = true
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND NOT is_death_cause`, healthTenant, healthGoat)
	if err == nil {
		t.Fatal("a second cause of death was accepted for one animal")
	}
}
