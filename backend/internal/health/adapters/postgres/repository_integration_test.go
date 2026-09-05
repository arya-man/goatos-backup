package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	healthTenant = "71000000-0000-4000-8000-000000000001"
	healthActor  = "71000000-0000-4000-8000-000000000002"
	healthParty  = "71000000-0000-4000-8000-000000000003"
	healthPark   = "71000000-0000-4000-8000-000000000004"
	healthShed   = "71000000-0000-4000-8000-000000000005"
	healthGoat   = "71000000-0000-4000-8000-000000000006"
)

func TestHealthCourseMedicineAndDeathLifecycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine := "Meloxicam"
	dose := "1 ml"
	steps := make([]domain.ProtocolStep, 0, 3)
	for day := 1; day <= 3; day++ {
		steps = append(steps, domain.ProtocolStep{
			DayNo: day, Session: domain.SessionMorning, Seq: day,
			RecordType: "medication", MedicineName: &medicine, DosageText: &dose,
		})
	}
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-v1", []domain.SourceProtocol{{
		DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult,
		DurationDays: 0, Steps: steps,
	}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(loc)
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
		IdempotencyKey: "health-open-fever", RequestFingerprint: "open-fingerprint",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	if opened.DurationDays != 3 || opened.SessionCount != 3 {
		t.Fatalf("course=%+v, want configured default 3 days and 3 sessions", opened)
	}
	replay, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
		IdempotencyKey: "health-open-fever", RequestFingerprint: "open-fingerprint",
	})
	if err != nil || !replay.IdempotentReplay || replay.CaseID != opened.CaseID {
		t.Fatalf("open replay=%+v err=%v", replay, err)
	}

	completed, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		IdempotencyKey: "health-complete-day-1", RequestFingerprint: "complete-fingerprint",
	})
	if err != nil || completed.MedicationCount != 1 {
		t.Fatalf("complete=%+v err=%v", completed, err)
	}
	assertHealthCount(t, ctx, pool, "medicine history", `SELECT count(*) FROM health_medicine_administrations WHERE tenant_id=$1 AND goat_id=$2`, 1, healthTenant, healthGoat)

	if err := repo.HoldForDeathReview(ctx, healthTenant, healthGoat); err != nil {
		t.Fatalf("hold: %v", err)
	}
	tomorrow := today.AddDate(0, 0, 1).Format("2006-01-02")
	held, err := repo.ListWorkItems(ctx, domain.ListFilter{TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Date: tomorrow, Limit: 20})
	if err != nil || len(held.Items) != 1 || held.Items[0].Status != "held" {
		t.Fatalf("held page=%+v err=%v", held, err)
	}
	if err := repo.ResumeAfterDeathRejected(ctx, healthTenant, healthGoat); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat, domain.DeathCause{}); err != nil {
		t.Fatalf("close dead: %v", err)
	}
	defaultPage, err := repo.ListWorkItems(ctx, domain.ListFilter{TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Date: tomorrow, Limit: 20})
	if err != nil || len(defaultPage.Items) != 0 || defaultPage.Summary.Total != 0 {
		t.Fatalf("dead goat surfaced tomorrow: page=%+v err=%v", defaultPage, err)
	}
	canceled, err := repo.ListWorkItems(ctx, domain.ListFilter{TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Date: tomorrow, Status: "canceled_death", Limit: 20})
	if err != nil || len(canceled.Items) != 1 || canceled.Items[0].Status != "canceled_death" {
		t.Fatalf("canceled audit page=%+v err=%v", canceled, err)
	}
	assertHealthCount(t, ctx, pool, "preserved completed medicine", `SELECT count(*) FROM health_medicine_administrations WHERE tenant_id=$1 AND goat_id=$2`, 1, healthTenant, healthGoat)
}

func TestHealthFilterOptionsIncludeUnusedPublishedProtocol(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-unused", []domain.SourceProtocol{{
		DiseaseKey: "pneumonia", DisplayName: "Pneumonia", AgeBand: domain.AgeBandAdult,
		DurationDays: 3,
	}}); err != nil {
		t.Fatalf("publish unused protocol: %v", err)
	}

	page, err := repo.ListWorkItems(ctx, domain.ListFilter{
		TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Date: "2026-07-30", Limit: 1,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("items=%d, want no existing cases", len(page.Items))
	}
	if len(page.FilterOptions.Diseases) != 1 || page.FilterOptions.Diseases[0].Key != "pneumonia" {
		t.Fatalf("disease options=%+v, want unused published pneumonia protocol", page.FilterOptions.Diseases)
	}
}

func seedHealthScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (tenant_id,name,status) VALUES ($1::uuid,'Health Test','active') ON CONFLICT DO NOTHING`, []any{healthTenant}},
		{`INSERT INTO parties (party_id,party_type,display_name,status) VALUES ($1::uuid,'org','Health Custodian','active') ON CONFLICT DO NOTHING`, []any{healthParty}},
		{`INSERT INTO locations (location_id,tenant_id,location_type,location_code,name,status) VALUES ($2::uuid,$1::uuid,'park','CPT','CPT','active') ON CONFLICT DO NOTHING`, []any{healthTenant, healthPark}},
		{`INSERT INTO locations (location_id,tenant_id,location_type,location_code,name,parent_location_id,status) VALUES ($3::uuid,$1::uuid,'shed','CPT-H1','Health Shed',$2::uuid,'active') ON CONFLICT DO NOTHING`, []any{healthTenant, healthPark, healthShed}},
		{`INSERT INTO goats (goat_id,tenant_id,display_id,species,sex,lifecycle_status,age_band,custodian_party_id,park_id,shed_id,current_location_id,origin_type,dob,entry_date) VALUES ($1::uuid,$2::uuid,'G-710001','goat','female','alive','adult',$3::uuid,$4::uuid,$5::uuid,$5::uuid,'procured',DATE '2024-01-01',DATE '2024-01-01')`, []any{healthGoat, healthTenant, healthParty, healthPark, healthShed}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed health scope: %v", err)
		}
	}
}

func assertHealthCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s=%d, want %d", label, got, want)
	}
}

// TestGetWorkItemPartitionLabelDisplayMatchesDBRoundTrip proves that collapsing the
// partition-label subquery in GetWorkItem's SELECT from three evaluations to one
// (via a LATERAL join, composed in Go through oploc.OperationalLocation.Display()
// instead of a hand-rolled SQL CASE) left the OUTPUT STRING unchanged: an
// unpartitioned shed still renders the bare shed name, and a shed with exactly one
// active partition still renders "<shed> - <partition>".
func TestGetWorkItemPartitionLabelDisplayMatchesDBRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine := "Meloxicam"
	dose := "1 ml"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-partition-test", "hash-partition-v1", []domain.SourceProtocol{{
		DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult,
		DurationDays: 0, Steps: []domain.ProtocolStep{{
			DayNo: 1, Session: domain.SessionMorning, Seq: 1,
			RecordType: "medication", MedicineName: &medicine, DosageText: &dose,
		}},
	}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(loc)
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
		IdempotencyKey: "health-partition-open", RequestFingerprint: "health-partition-open-fp",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}

	// Unpartitioned: the shed carries no active shed_partitions row, so the display
	// must be the bare shed name.
	unpartitioned, err := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	if err != nil {
		t.Fatalf("get work item (unpartitioned): %v", err)
	}
	if unpartitioned.OperationalLocationDisplay != "Health Shed" {
		t.Fatalf("unpartitioned display=%q, want %q", unpartitioned.OperationalLocationDisplay, "Health Shed")
	}
	if unpartitioned.PartitionLabel != "" {
		t.Fatalf("unpartitioned partition label=%q, want empty", unpartitioned.PartitionLabel)
	}

	// Partitioned: exactly one active, non-'whole' partition on the shed. Per the
	// operational-location convention this must render "Health Shed - Part 3".
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, 'Part 3', '3', 'active', 'manual')`, healthTenant, healthShed); err != nil {
		t.Fatalf("seed shed partition: %v", err)
	}

	partitioned, err := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	if err != nil {
		t.Fatalf("get work item (partitioned): %v", err)
	}
	if partitioned.PartitionLabel != "Part 3" {
		t.Fatalf("partitioned partition label=%q, want %q", partitioned.PartitionLabel, "Part 3")
	}
	if partitioned.OperationalLocationDisplay != "Health Shed - Part 3" {
		t.Fatalf("partitioned display=%q, want %q", partitioned.OperationalLocationDisplay, "Health Shed - Part 3")
	}
}
