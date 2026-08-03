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

// TestCampaignByIDEvaluatesAccessInsideTheQuery proves the authorization arms are real SQL
// predicates and not a Go-side post-filter.
//
// The park authority used to be checked in the app layer by a separate CampaignParkID read
// before the row was fetched. park_id is mutable, so those two statements could disagree about
// which park was approved. Having the predicate in the same query is what makes that
// impossible -- and it is only true if the query actually refuses an unauthorized park, which is
// what this pins in the database rather than against a fake.
func TestCampaignByIDEvaluatesAccessInsideTheQuery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The CPT bucket needs an operator scoped to CPT: weighing_operator_park_bound_guard refuses
	// a cross-park assignment outright, so a fixture without this grant would be rejected by the
	// database rather than by the predicate under test.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOtherOp, lcpParkCPT)

	// One task in each park. lcpParkCBE is the park the operator is granted in.
	here := lcpUUID(17001)
	lcpInsertCampaign(t, ctx, pool, here, lcpParkCBE, "2026-06-01", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(17011), here, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	elsewhere := lcpUUID(17002)
	lcpInsertCampaign(t, ctx, pool, elsewhere, lcpParkCPT, "2026-06-08", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(17021), elsewhere, lcpShedCPT, domain.CategoryIndividualAnimal, repoOtherOp, 3, "pending")

	authorizedHere := ports.CampaignAccess{AuthorizedParkIDs: []string{lcpParkCBE}}
	if _, err := repo.CampaignByID(ctx, repoTenant, here, authorizedHere); err != nil {
		t.Fatalf("CampaignByID in an authorized park: %v", err)
	}
	if _, err := repo.CampaignByID(ctx, repoTenant, elsewhere, authorizedHere); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("CampaignByID in an UNAUTHORIZED park err = %v, want ErrNotFound -- CROSS-PARK LEAK", err)
	}

	// An empty park set is "authorized for nothing", not "unrestricted": the array arm must not
	// collapse into a match-everything, and a NULL-valued arm would make the whole disjunction
	// NULL and silently drop rows the assignee arm should have admitted.
	if _, err := repo.CampaignByID(ctx, repoTenant, here, ports.CampaignAccess{}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("CampaignByID with no authority at all err = %v, want ErrNotFound", err)
	}

	// Tenant-wide authority admits either park.
	for _, campaignID := range []string{here, elsewhere} {
		if _, err := repo.CampaignByID(ctx, repoTenant, campaignID, ports.CampaignAccess{Unrestricted: true}); err != nil {
			t.Fatalf("CampaignByID for a tenant-wide caller on %s: %v", campaignID, err)
		}
	}
}

// TestCampaignByIDAdmitsAnAssigneeOutsideTheirParkSet pins the arm that keeps a Growth Director
// out of a 404 on their own work (the fix in 2c78f87f1), now that both arms live in one query.
//
// The actor's park authority covers only park CBE, while their assignment is on a CPT task. The
// arms are alternatives, so the assignment admits the task on its own -- and the task comes back
// narrowed to their own bucket, because being assigned somewhere is not oversight of the park.
func TestCampaignByIDAdmitsAnAssigneeOutsideTheirParkSet(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The grant the read re-proves is a TENANT grant here: the assignee arm requires an active
	// grant covering the task's park, which is defence in depth against a stray cross-park row.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'tenant', $1::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator)

	task := lcpUUID(18001)
	lcpInsertCampaign(t, ctx, pool, task, lcpParkCPT, "2026-07-06", domain.StatusPublished, repoOperator)
	mine := lcpUUID(18011)
	lcpInsertBucket(t, ctx, pool, mine, task, lcpShedCPT, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")

	// Park authority for a DIFFERENT park, plus the assignment. The assignment alone must admit.
	campaign, err := repo.CampaignByID(ctx, repoTenant, task, ports.CampaignAccess{
		AuthorizedParkIDs: []string{lcpParkCBE},
		AssigneeUserID:    repoOperator,
	})
	if err != nil {
		t.Fatalf("CampaignByID on the assignee's OWN task outside their park set err = %v, want nil -- a director 404'd from their own work", err)
	}
	if len(campaign.Sheds) != 1 || campaign.Sheds[0].CampaignShedID != mine {
		t.Fatalf("CampaignByID buckets=%+v, want only the caller's own bucket %s -- an assignment must not open the whole task", campaign.Sheds, mine)
	}
}
