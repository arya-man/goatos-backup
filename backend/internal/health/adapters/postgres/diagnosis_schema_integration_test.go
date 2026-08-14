package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Migration 000162 carries two maintainer decisions as CONSTRAINTS rather than as
// application checks. A rule enforced only in Go is a rule any future writer can
// bypass by writing a row; a CHECK cannot be bypassed.
//
// These tests prove the constraints actually fire. A constraint that has never
// rejected anything is not proven to reject anything.

// diagnosisSchemaFixture reuses the package's health scope (tenant, park, shed,
// one alive adult goat) and publishes one treatment card so a case has something
// to point at.
func diagnosisSchemaFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (tenantID, goatID, versionID string) {
	t.Helper()
	seedHealthScope(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-diagnosis",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 3,
		}}); err != nil {
		t.Fatalf("publish card: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT health_protocol_version_id::text FROM health_protocol_versions
WHERE tenant_id=$1::uuid AND disease_key='fever' AND age_band='adult' AND status='published'`,
		healthTenant).Scan(&versionID); err != nil {
		t.Fatalf("read published card: %v", err)
	}
	return healthTenant, healthGoat, versionID
}

// Decision D: only a Type F course carries a closure day count. The other three
// close on a test, on Director review, or when containment is cleared, and
// giving one of them a duration stamps an end date the clinical contract
// explicitly refuses to state.
func TestCaseDurationMustMatchExitType(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	tenantID, goatID, versionID := diagnosisSchemaFixture(t, ctx, pool)

	insert := func(exitType string, duration *int, key string) error {
		_, err := pool.Exec(ctx, `
INSERT INTO health_cases
 (tenant_id,goat_id,health_protocol_version_id,disease_key,disease_name,age_band,
  start_date,duration_days,exit_type,status,diagnosed_by,idempotency_key,request_fingerprint)
VALUES ($1::uuid,$2::uuid,$3::uuid,'fever','Fever','adult',
  current_date,$4,$5,'active',$2::uuid,$6,'fp')`,
			tenantID, goatID, versionID, duration, exitType, key)
		return err
	}
	three := 3

	t.Run("type F with a duration is accepted", func(t *testing.T) {
		if err := insert("F", &three, "ok-f"); err != nil {
			t.Fatalf("a fixed course with a duration must be accepted: %v", err)
		}
	})

	t.Run("type F without a duration is rejected", func(t *testing.T) {
		// A fixed course with no day count can never close on its calendar.
		err := insert("F", nil, "bad-f")
		requireConstraintViolation(t, err, "health_cases_duration_matches_exit_type")
	})

	for _, exitType := range []string{"T", "V", "Supportive"} {
		t.Run("type "+exitType+" without a duration is accepted", func(t *testing.T) {
			if err := insert(exitType, nil, "ok-"+exitType); err != nil {
				t.Fatalf("%s must be storable with no duration: %v", exitType, err)
			}
		})
		t.Run("type "+exitType+" with a duration is rejected", func(t *testing.T) {
			err := insert(exitType, &three, "bad-"+exitType)
			requireConstraintViolation(t, err, "health_cases_duration_matches_exit_type")
		})
	}

	t.Run("an unknown exit type is rejected", func(t *testing.T) {
		err := insert("fixed", &three, "bad-unknown")
		if err == nil {
			t.Fatal("an unknown exit type must be rejected, not stored")
		}
	})
}

// The advisory boundary, as constraints: a run is confirmed only with a named
// confirmer and a timestamp, an invalid form can never be confirmed, and a
// rejected run names its reason.
func TestDiagnosisRunConfirmationConstraints(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	tenantID, goatID, _ := diagnosisSchemaFixture(t, ctx, pool)

	insert := func(valid bool, rejectReason *string, status string, confirmedBy *string, key string) error {
		confirmationKey := confirmedBy
		confirmationFingerprint := confirmedBy
		_, err := pool.Exec(ctx, `
INSERT INTO health_diagnosis_runs
 (tenant_id,goat_id,register_version,observed_by,business_date,form,proposal,
  valid,reject_reason,scope,status,confirmed_by,confirmed_at,confirmation_idempotency_key,
  confirmation_fingerprint,idempotency_key,request_fingerprint)
VALUES ($1::uuid,$2::uuid,'adult-1',$2::uuid,current_date,'{}'::jsonb,'{}'::jsonb,
  $3,$4,'adult',$5,$6::uuid,CASE WHEN $6 IS NULL THEN NULL ELSE now() END,$7,$8,$9,'fp')`,
			tenantID, goatID, valid, rejectReason, status, confirmedBy, confirmationKey, confirmationFingerprint, key)
		return err
	}
	reason := "not_eating_with_feed"

	t.Run("a proposed run needs no confirmer", func(t *testing.T) {
		if err := insert(true, nil, "proposed", nil, "ok-proposed"); err != nil {
			t.Fatalf("a proposed run must store: %v", err)
		}
	})

	t.Run("a confirmed run needs a confirmer", func(t *testing.T) {
		err := insert(true, nil, "confirmed", nil, "bad-confirmed")
		requireConstraintViolation(t, err, "health_diagnosis_runs_confirmation_complete")
	})

	t.Run("a proposed run must not carry a confirmer", func(t *testing.T) {
		err := insert(true, nil, "proposed", &goatID, "bad-proposed-confirmer")
		requireConstraintViolation(t, err, "health_diagnosis_runs_confirmation_empty_until_confirmed")
	})

	t.Run("an invalid form can never be confirmed", func(t *testing.T) {
		// A contradictory observation is not diagnosed at all, so there is nothing
		// a Director could legitimately confirm.
		err := insert(false, &reason, "confirmed", &goatID, "bad-invalid-confirmed")
		if err == nil {
			t.Fatal("an invalid run must not be storable as confirmed")
		}
	})

	t.Run("a rejected run names its reason", func(t *testing.T) {
		err := insert(false, nil, "proposed", nil, "bad-no-reason")
		requireConstraintViolation(t, err, "health_diagnosis_runs_reject_reason_matches_valid")
	})

	t.Run("a valid run carries no reject reason", func(t *testing.T) {
		err := insert(true, &reason, "proposed", nil, "bad-reason-on-valid")
		requireConstraintViolation(t, err, "health_diagnosis_runs_reject_reason_matches_valid")
	})
}

// A confirmation opens at most one course per disease. Re-confirming is an
// idempotent replay, never a second concurrent course for the same illness on
// the same animal.
func TestOneCasePerDiseasePerDiagnosisRun(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	tenantID, goatID, versionID := diagnosisSchemaFixture(t, ctx, pool)

	var runID string
	if err := pool.QueryRow(ctx, `
INSERT INTO health_diagnosis_runs
 (tenant_id,goat_id,register_version,observed_by,business_date,form,proposal,valid,scope,idempotency_key,request_fingerprint)
VALUES ($1::uuid,$2::uuid,'adult-1',$2::uuid,current_date,'{}'::jsonb,'{}'::jsonb,true,'adult','run-1','fp')
RETURNING health_diagnosis_run_id::text`, tenantID, goatID).Scan(&runID); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	openCase := func(key string) error {
		_, err := pool.Exec(ctx, `
INSERT INTO health_cases
 (tenant_id,goat_id,health_protocol_version_id,disease_key,disease_name,age_band,start_date,
  duration_days,exit_type,status,diagnosed_by,idempotency_key,request_fingerprint,health_diagnosis_run_id)
VALUES ($1::uuid,$2::uuid,$3::uuid,'fever','Fever','adult',current_date,3,'F','active',$2::uuid,$4,'fp',$5::uuid)`,
			tenantID, goatID, versionID, key, runID)
		return err
	}

	if err := openCase("case-1"); err != nil {
		t.Fatalf("first case must open: %v", err)
	}
	err := openCase("case-2")
	if err == nil {
		t.Fatal("a second course for the same disease on the same run must be rejected")
	}
	if !strings.Contains(err.Error(), "health_cases_run_disease_uq") {
		t.Errorf("want the run/disease uniqueness violation, got: %v", err)
	}
}

func requireConstraintViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected constraint %s to reject this row", constraint)
	}
	if !strings.Contains(err.Error(), constraint) {
		t.Errorf("expected violation of %s, got: %v", constraint, err)
	}
}
