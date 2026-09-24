package postgres

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"testing"
	"time"
)

func TestCardMembershipIncludesOffPageAssignments(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	const batch2 = "70000000-0000-4000-8000-000000000802"
	const goat2 = "70000000-0000-4000-8000-000000000803"
	const obl2 = "70000000-0000-4000-8000-000000000805"
	insertProjectionGoat(t, ctx, pool, goat2, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, batch2, "planned")
	insertProjectionObligation(t, ctx, pool, obl2, batch2, goat2, "scheduled", "2026-06-25 00:00:00+00", "off-page-member")
	for _, b := range []string{testBatch, batch2} {
		execProjectionSQL(t, ctx, pool, "dated assignment", `INSERT INTO vaccination_drive_assignments (tenant_id,batch_id,planned_date,operator_id,park_id,shed_id,physical_shed,partition_label,animal_count) VALUES ($1,$2,'2026-06-24',$3,$4,$5,'K1 Shed','whole',1)`, testTenant, b, testOperator, testPark, testShed)
	}
	repo := NewRepository(pool, 10*time.Second)
	q := domain.ExecutionQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	page, combined, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.NextCursor == nil {
		t.Fatalf("fixture must straddle a page: %#v", page)
	}
	separate, err := repo.VaccinationExecutionCardSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for mode, summaries := range map[string]map[string]*domain.ShedCardSummary{"combined": combined, "separate": separate} {
		batches := map[string]bool{}
		for _, summary := range summaries {
			for _, member := range summary.RosterMemberships {
				if member.BatchID != nil {
					batches[*member.BatchID] = true
				}
				if member.PlannedDate != "2026-06-24" || !member.IncludeWhenOverdue {
					t.Fatalf("%s: wrong dated open membership: %#v", mode, member)
				}
			}
		}
		if !batches[testBatch] || !batches[batch2] {
			t.Fatalf("%s omitted off-page membership: %#v", mode, batches)
		}
	}
}
