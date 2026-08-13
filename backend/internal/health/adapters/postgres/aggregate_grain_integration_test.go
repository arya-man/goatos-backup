package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial grain coverage for the Health work-item read model.
//
// ListWorkItems joins health_treatment_sessions to health_cases, goats, two locations rows and a
// step_counts aggregate. Every one of those is a chance to multiply the row the operator counts,
// to let a page rewrite a whole-filter total, or to bucket a session twice. These tests set up the
// shapes where that actually goes wrong -- a session carrying MORE THAN ONE step, a filter holding
// MORE rows than one page, sessions landing on DIFFERENT business dates, and the full status
// matrix -- rather than a single happy-path row that cannot distinguish right from wrong.

const (
	grainGoatA = "72000000-0000-4000-8000-00000000000a"
	grainGoatB = "72000000-0000-4000-8000-00000000000b"
	grainGoatC = "72000000-0000-4000-8000-00000000000c"
)

// Two medication steps on the SAME day and session. If step_counts were joined raw instead of
// pre-aggregated, this one session would come back as two work items.
func seedGrainProtocolAndCases(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*Repository, time.Time) {
	t.Helper()
	seedHealthScope(t, ctx, pool)
	for i, id := range []string{grainGoatA, grainGoatB, grainGoatC} {
		if _, err := pool.Exec(ctx, `INSERT INTO goats (goat_id,tenant_id,display_id,species,sex,lifecycle_status,age_band,custodian_party_id,park_id,shed_id,current_location_id,origin_type,dob,entry_date)
VALUES ($1::uuid,$2::uuid,$3,'goat','female','alive','adult',$4::uuid,$5::uuid,$6::uuid,$6::uuid,'procured',DATE '2024-01-01',DATE '2024-01-01')`,
			id, healthTenant, fmt.Sprintf("G-7200%02d", i+1), healthParty, healthPark, healthShed); err != nil {
			t.Fatalf("seed grain goat %s: %v", id, err)
		}
	}

	medicine, dose := "Meloxicam", "1 ml"
	// Seq is unique per protocol VERSION (health_protocol_steps_health_protocol_version_id_seq_key),
	// not per day, so it numbers straight through the course.
	steps := []domain.ProtocolStep{
		{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "medication", MedicineName: &medicine, DosageText: &dose},
		{DayNo: 1, Session: domain.SessionMorning, Seq: 2, RecordType: "medication", MedicineName: &medicine, DosageText: &dose},
		{DayNo: 2, Session: domain.SessionMorning, Seq: 3, RecordType: "medication", MedicineName: &medicine, DosageText: &dose},
	}
	repo := NewRepository(pool, 20*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "grain-test", "grain-hash", []domain.SourceProtocol{{
		DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 0, Steps: steps,
	}}); err != nil {
		t.Fatalf("publish grain protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(loc)
	for i, id := range []string{grainGoatA, grainGoatB, grainGoatC} {
		if _, err := repo.OpenCase(ctx, domain.OpenCaseInput{
			TenantID: healthTenant, ActorID: healthActor, GoatID: id,
			DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
			IdempotencyKey: fmt.Sprintf("grain-open-%d", i), RequestFingerprint: fmt.Sprintf("grain-fp-%d", i),
		}); err != nil {
			t.Fatalf("open grain case %s: %v", id, err)
		}
	}
	return repo, today
}

func grainFilter(date time.Time, limit int) domain.ListFilter {
	return domain.ListFilter{
		TenantID: healthTenant, AgeBand: domain.AgeBandAdult,
		Date: date.Format("2006-01-02"), Limit: limit,
	}
}

// Cardinality: a session with two steps must stay ONE work item. The step fan-out is the join most
// likely to silently double an operator's work list.
func TestHealthWorkItemsOneToManyStepsDoNotMultiplyRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, today := seedGrainProtocolAndCases(t, ctx, pool)

	page, err := repo.ListWorkItems(ctx, grainFilter(today, 50))
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("items=%d, want 3 (one per goat); a steps fan-out would report 6", len(page.Items))
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		if seen[item.SessionID] {
			t.Fatalf("session %s appeared twice: the step_counts join multiplied the row grain", item.SessionID)
		}
		seen[item.SessionID] = true
		if item.StepCount != 2 {
			t.Fatalf("step_count=%d for session %s, want 2 counted steps on one row", item.StepCount, item.SessionID)
		}
	}
	if page.Summary.Total != 3 {
		t.Fatalf("summary total=%d, want 3 sessions", page.Summary.Total)
	}
}

// Pagination: the summary is a whole-filter aggregate. Paging must change rows only, never truth.
func TestHealthWorkItemsPaginationKeepsSummaryWholeFilter(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, today := seedGrainProtocolAndCases(t, ctx, pool)

	first, err := repo.ListWorkItems(ctx, grainFilter(today, 2))
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("page 1 items=%d, want the requested 2", len(first.Items))
	}
	if first.NextCursor == nil {
		t.Fatal("page 1 must expose a next cursor while a third session remains")
	}
	if first.Summary.Total != 3 {
		t.Fatalf("page 1 summary total=%d, want the whole-filter 3 and NOT the page size", first.Summary.Total)
	}

	next := grainFilter(today, 2)
	next.Cursor = *first.NextCursor
	second, err := repo.ListWorkItems(ctx, next)
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(second.Items) != 1 {
		t.Fatalf("page 2 items=%d, want the remaining 1", len(second.Items))
	}
	if second.Summary.Total != first.Summary.Total {
		t.Fatalf("summary moved across pages: page1=%d page2=%d", first.Summary.Total, second.Summary.Total)
	}
	for _, a := range first.Items {
		for _, b := range second.Items {
			if a.SessionID == b.SessionID {
				t.Fatalf("session %s served on both pages: the keyset cursor does not advance", a.SessionID)
			}
		}
	}
}

// Date: markers are keyed by business_date. Day 2 of the course belongs to tomorrow's marker, not
// to the day the case was opened.
func TestHealthMarkersFollowScheduledDateNotOpenDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, today := seedGrainProtocolAndCases(t, ctx, pool)

	page, err := repo.ListWorkItems(ctx, grainFilter(today, 50))
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	byDate := map[string]int{}
	for _, marker := range page.DateMarkers {
		if _, dup := byDate[marker.Date]; dup {
			t.Fatalf("business date %s emitted twice: markers are not grouped at day grain", marker.Date)
		}
		byDate[marker.Date] = marker.Count
	}
	todayKey := today.Format("2006-01-02")
	if byDate[todayKey] != 3 {
		t.Fatalf("marker[%s]=%d, want 3 day-1 sessions", todayKey, byDate[todayKey])
	}
	tomorrowKey := today.AddDate(0, 0, 1).Format("2006-01-02")
	if tomorrowKey[:7] == todayKey[:7] && byDate[tomorrowKey] != 3 {
		t.Fatalf("marker[%s]=%d, want the 3 day-2 sessions on their SCHEDULED date, not the open date", tomorrowKey, byDate[tomorrowKey])
	}
	for _, item := range page.Items {
		if item.BusinessDate != todayKey {
			t.Fatalf("filtered to %s but got a row dated %s", todayKey, item.BusinessDate)
		}
	}
}

// Status: every session lands in exactly one bucket, and the buckets reconstruct Total.
func TestHealthSummaryStatusBucketsAreDisjoint(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo, today := seedGrainProtocolAndCases(t, ctx, pool)

	page, err := repo.ListWorkItems(ctx, grainFilter(today, 50))
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	s := page.Summary
	buckets := s.Due + s.Scheduled + s.InProgress + s.Completed + s.Rework + s.Held + s.CanceledDeath
	if buckets != s.Total {
		t.Fatalf("status buckets sum to %d but Total=%d; a session is double-counted or dropped (%+v)", buckets, s.Total, s)
	}
	if s.Total != 3 {
		t.Fatalf("total=%d, want 3", s.Total)
	}
}
