package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// A TASK THAT ALREADY HOLDS WEIGHED WORK CANNOT BE MOVED.
//
// A weighed bucket records the day it was actually weighed, and its proof hangs
// off that day, so a task move must not drag it along -- that would falsify when
// the work happened. The bucket upsert therefore refuses to touch a 'completed'
// or 'closed' row. Letting the MOVE succeed anyway was the silent half of the
// same bug: the campaign landed on the new date while the finished bucket kept
// the old one, so the shed belonged to neither day's task and the screen still
// said saved.
//
// The move is refused and the sheds are NAMED. An ordinary edit -- same date,
// same park, sheds added or dropped -- is untouched.
func TestUpdateCampaignRefusesToMoveATaskThatAlreadyHasWeighedSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	base := domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition},
		},
	}

	// Nothing is finished yet: moving the task is ordinary planning and must work.
	movedEarly := base
	movedEarly.StartBusinessDate = "2026-07-30"
	movedEarly.IdempotencyKey = "move:before-any-work"
	if _, err := repo.UpdateCampaign(ctx, repoCampaign, movedEarly); err != nil {
		t.Fatalf("moving a task with no finished work must be allowed: %v", err)
	}

	// The operator weighs one shed. That bucket now owns the day it happened on.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='completed', completed_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope)

	// Moving the DATE is now refused, and the refusal names the weighed shed.
	movedLate := base
	movedLate.StartBusinessDate = "2026-08-03"
	movedLate.IdempotencyKey = "move:after-work"
	_, err := repo.UpdateCampaign(ctx, repoCampaign, movedLate)
	if !errors.Is(err, ports.ErrFinishedShedBlocksReschedule) {
		t.Fatalf("moving a task with weighed sheds: err=%v, want ErrFinishedShedBlocksReschedule", err)
	}
	conflict := &ports.FinishedShedConflict{}
	if !errors.As(err, &conflict) {
		t.Fatalf("err %v does not carry the weighed bucket names", err)
	}
	if len(conflict.Sheds) == 0 || conflict.WeighDate != "2026-07-30" {
		t.Fatalf("conflict=%+v, want the sheds named and the date they were weighed on", conflict)
	}

	// And nothing moved: the refusal happens before any write.
	var campaignDate, shedDate string
	if err := pool.QueryRow(ctx, `
SELECT c.start_business_date::text, s.start_business_date::text
FROM weighing_campaigns c
JOIN weighing_campaign_sheds s ON s.tenant_id=c.tenant_id AND s.campaign_shed_id=$2::uuid
WHERE c.tenant_id=$1::uuid AND c.campaign_id=$3::uuid`, repoTenant, repoAnimalScope, repoCampaign).
		Scan(&campaignDate, &shedDate); err != nil {
		t.Fatalf("read dates: %v", err)
	}
	if campaignDate != "2026-07-30" || shedDate != campaignDate {
		t.Fatalf("after the refusal campaign=%s shed=%s, want both still on 2026-07-30 -- a refused move must write nothing", campaignDate, shedDate)
	}

	// Moving the PARK is refused for the same reason.
	movedPark := base
	movedPark.StartBusinessDate = "2026-07-30"
	movedPark.ParkID = lsParkCPT
	movedPark.IdempotencyKey = "move:other-park"
	if _, err := repo.UpdateCampaign(ctx, repoCampaign, movedPark); !errors.Is(err, ports.ErrFinishedShedBlocksReschedule) {
		t.Fatalf("moving the task's park with weighed sheds: err=%v, want ErrFinishedShedBlocksReschedule", err)
	}

	// An ORDINARY edit on the same task still works: same date, same park, one shed
	// dropped. Finished work blocks a MOVE, not every edit.
	sameDay := base
	sameDay.StartBusinessDate = "2026-07-30"
	sameDay.IdempotencyKey = "edit:same-day-drop-shed"
	sameDay.Sheds = []domain.CreateCampaignShed{
		{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
	}
	if _, err := repo.UpdateCampaign(ctx, repoCampaign, sameDay); err != nil {
		t.Fatalf("an ordinary same-day edit must still be allowed on a task with finished sheds: %v", err)
	}
}
