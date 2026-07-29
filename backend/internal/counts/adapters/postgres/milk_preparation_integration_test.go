package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestMilkPreparationUsesExactCohortGrainAndWholeScopeSummary(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)

	for i := 0; i < 2; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(60+i), goatDisplayID(60+i), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	}
	insertBreakdownGoat(t, ctx, pool, goatUUID(70), goatDisplayID(70), "female", "Beetal", "alive", "K2", strp(countsPark), strp(countsShedA), nil)
	for i := 0; i < 3; i++ {
		insertBreakdownGoat(t, ctx, pool, goatUUID(80+i), goatDisplayID(80+i), "male", "Beetal", "alive", "K3", strp(countsPark), strp(countsShedA), nil)
	}
	// This animal is in the same shed but outside the milk-preparation membership set.
	insertBreakdownGoat(t, ctx, pool, goatUUID(90), goatDisplayID(90), "male", "Beetal", "alive", "Adult", strp(countsPark), strp(countsShedA), nil)

	asOf := time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC) // 2026-07-30 in India.
	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 1, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	if len(got.Items) != 1 || !got.HasMore {
		t.Fatalf("page items=%d has_more=%v, want one item and more", len(got.Items), got.HasMore)
	}
	if got.Summary.Scope != "filtered" || got.Summary.CohortCount != 3 || got.Summary.HeadCount != 6 || got.Summary.ShedCount != 1 {
		t.Fatalf("whole-scope summary=%+v", got.Summary)
	}
	if got.Summary.TotalRequiredML != 4000 || got.Summary.CitricAcidGrams != 22 {
		t.Fatalf("quantity summary=%+v, want 4000 ml and 22 g", got.Summary)
	}
	if len(got.FarmTasks) != 1 {
		t.Fatalf("farm tasks=%+v, want one whole-scope farm-day task", got.FarmTasks)
	}
	farmTask := got.FarmTasks[0]
	if farmTask.ParkID != countsPark || farmTask.CohortCount != 3 || farmTask.HeadCount != 6 {
		t.Fatalf("farm-day task grain=%+v", farmTask)
	}
	if farmTask.TotalRequiredML != 4000 || farmTask.CitricAcidGrams != 22 || farmTask.VerificationStatus != domain.MilkPreparationVerificationNotSubmitted {
		t.Fatalf("farm-day task direction/status=%+v", farmTask)
	}
	if got.PreparationDate != biztime.BusinessDate(asOf) || got.FeedingDate != "2026-07-31" {
		t.Fatalf("dates preparation=%s feeding=%s", got.PreparationDate, got.FeedingDate)
	}

	emptyPark := "00000000-0000-4000-8000-000000000099"
	empty, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: &emptyPark, Limit: 10, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation empty park: %v", err)
	}
	if len(empty.Items) != 0 || len(empty.FarmTasks) != 0 || empty.HasMore || empty.Summary.HeadCount != 0 || empty.Summary.TotalRequiredML != 0 {
		t.Fatalf("empty park response=%+v", empty)
	}
}

func TestMilkPreparationVerificationStateIsOneTaskPerFarm(t *testing.T) {
	ctx := context.Background()
	repo, pool := newBreakdownRepo(t, ctx)
	insertBreakdownGoat(t, ctx, pool, goatUUID(160), goatDisplayID(160), "female", "Beetal", "alive", "K1", strp(countsPark), strp(countsShedA), nil)
	insertBreakdownGoat(t, ctx, pool, goatUUID(161), goatDisplayID(161), "female", "Beetal", "alive", "K2", strp(countsPark), strp(countsShedB), nil)

	if _, err := pool.Exec(ctx, `INSERT INTO milk_preparation_completions
(tenant_id, park_id, shed_id, preparation_date, feeding_date, submitted_by)
VALUES ($1::uuid,$2::uuid,NULL,'2026-07-30','2026-07-31','90000000-0000-4000-8000-000000000101')`,
		countsTenant, countsPark); err != nil {
		t.Fatalf("insert farm completion: %v", err)
	}

	got, err := repo.GetMilkPreparation(ctx, domain.MilkPreparationQuery{
		TenantID: countsTenant, ParkID: strp(countsPark), Limit: 10,
		AsOf: time.Date(2026, 7, 29, 20, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("GetMilkPreparation: %v", err)
	}
	if len(got.FarmTasks) != 1 {
		t.Fatalf("farm tasks=%+v", got.FarmTasks)
	}
	if got.FarmTasks[0].VerificationStatus != domain.MilkPreparationVerificationPending {
		t.Fatalf("farm status=%+v", got.FarmTasks[0])
	}
	if got.Summary.PendingVerificationFarmCount != 1 || got.Summary.NotSubmittedFarmCount != 0 || got.Summary.ParkCount != 1 {
		t.Fatalf("farm summary buckets=%+v", got.Summary)
	}
}
