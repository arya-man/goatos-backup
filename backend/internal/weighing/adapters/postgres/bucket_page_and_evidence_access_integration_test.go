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

// TestListCampaignShedsEvaluatesAccessInsideTheQuery proves the bucket page's authorization arms
// are real SQL predicates rather than a Go-side post-filter or an app-layer pre-check.
//
// The park authority used to be resolved in the app layer by a separate CampaignParkID read
// before the buckets were paged. park_id is mutable, so those two statements could disagree
// about which park was approved, and a lost race handed over another park's buckets AND the
// display names of the operators assigned to them. Having the predicate in the same statement is
// what makes that impossible -- but only if the query really refuses an unauthorized park, which
// a fake cannot prove.
func TestListCampaignShedsEvaluatesAccessInsideTheQuery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The CPT bucket needs an operator scoped to CPT: weighing_operator_park_bound_guard refuses a
	// cross-park assignment outright, so a fixture without this grant would be rejected by the
	// database rather than by the predicate under test.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOtherOp, lcpParkCPT)

	// One task in each park. lcpParkCBE is the park the caller below is authorized in.
	here := lcpUUID(19001)
	lcpInsertCampaign(t, ctx, pool, here, lcpParkCBE, "2026-06-15", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(19011), here, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	elsewhere := lcpUUID(19002)
	lcpInsertCampaign(t, ctx, pool, elsewhere, lcpParkCPT, "2026-06-22", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, lcpUUID(19021), elsewhere, lcpShedCPT, domain.CategoryIndividualAnimal, repoOtherOp, 3, "pending")

	authorizedHere := ports.CampaignAccess{AuthorizedParkIDs: []string{lcpParkCBE}}
	page, err := repo.ListCampaignSheds(ctx, repoTenant, here, "", 20, authorizedHere)
	if err != nil {
		t.Fatalf("ListCampaignSheds in an authorized park: %v", err)
	}
	if len(page.Items) != 1 || page.TotalCount != 1 {
		t.Fatalf("ListCampaignSheds in an authorized park returned %d buckets / total %d, want 1/1", len(page.Items), page.TotalCount)
	}

	// The refusal is ErrNotFound, not an empty page: an empty page would be indistinguishable
	// from a task with no buckets and would disagree with the task header, which 404s.
	foreign, err := repo.ListCampaignSheds(ctx, repoTenant, elsewhere, "", 20, authorizedHere)
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("ListCampaignSheds in an UNAUTHORIZED park err = %v (%d buckets), want ErrNotFound -- CROSS-PARK ROSTER LEAK", err, len(foreign.Items))
	}
	if len(foreign.Items) != 0 || foreign.TotalCount != 0 {
		t.Fatalf("ListCampaignSheds leaked %d buckets / total %d from an unauthorized park", len(foreign.Items), foreign.TotalCount)
	}

	// An empty park set is "authorized for nothing", not "unrestricted": the array arm must not
	// collapse into a match-everything, and a NULL-valued arm would make the disjunction NULL and
	// silently drop rows instead of admitting them.
	if _, err := repo.ListCampaignSheds(ctx, repoTenant, here, "", 20, ports.CampaignAccess{}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("ListCampaignSheds with no authority at all err = %v, want ErrNotFound", err)
	}

	// Tenant-wide authority reaches either park.
	for _, campaignID := range []string{here, elsewhere} {
		if _, err := repo.ListCampaignSheds(ctx, repoTenant, campaignID, "", 20, ports.CampaignAccess{Unrestricted: true}); err != nil {
			t.Fatalf("ListCampaignSheds for a tenant-wide caller on %s: %v", campaignID, err)
		}
	}
}

// TestListCampaignShedsAdmitsAnAssigneeOutsideTheirParkSetAndNarrowsToTheirOwnBuckets pins the
// arm that keeps a Growth Director out of a 404 on their own work (2c78f87f1) on the bucket page
// as well as the task header, and pins that the arm ADMITS without WIDENING.
//
// The count must agree with the rows: it carries the same access clause, so a caller who can see
// one bucket is not told the task has three.
func TestListCampaignShedsAdmitsAnAssigneeOutsideTheirParkSetAndNarrowsToTheirOwnBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'tenant', $1::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOperator)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOtherOp, lcpParkCPT)

	task := lcpUUID(19101)
	lcpInsertCampaign(t, ctx, pool, task, lcpParkCPT, "2026-07-13", domain.StatusPublished, repoOperator)
	mine := lcpUUID(19111)
	lcpInsertBucket(t, ctx, pool, mine, task, lcpShedCPT, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	// A SECOND bucket on the same task, assigned to somebody else. It is the whole point: an
	// assignment must open the caller's own bucket and nothing beside it. The shed id is any
	// second distinct location -- the park-bound guard binds the OPERATOR to the CAMPAIGN's
	// park, not the bucket's shed, which is why the existing bucket-status matrix fixture mixes
	// shed ids into a CPT campaign the same way.
	lcpInsertBucket(t, ctx, pool, lcpUUID(19112), task, lcpShedTwo, domain.CategoryIndividualAnimal, repoOtherOp, 4, "pending")

	page, err := repo.ListCampaignSheds(ctx, repoTenant, task, "", 20, ports.CampaignAccess{
		// Park authority for a DIFFERENT park, plus the assignment. The assignment alone must admit.
		AuthorizedParkIDs: []string{lcpParkCBE},
		AssigneeUserID:    repoOperator,
	})
	if err != nil {
		t.Fatalf("ListCampaignSheds on the assignee's OWN task outside their park set err = %v, want nil -- a director 404'd from their own buckets", err)
	}
	if len(page.Items) != 1 || page.Items[0].CampaignShedID != mine {
		t.Fatalf("ListCampaignSheds buckets=%+v, want only the caller's own bucket %s -- an assignment must not open the whole task", page.Items, mine)
	}
	if page.TotalCount != 1 {
		t.Fatalf("ListCampaignSheds total=%d, want 1 -- the header count must range over the same set the pages can reach, or it discloses another operator's bucket count", page.TotalCount)
	}

	// Park authority over the task's OWN park sees both buckets: the arms are alternatives, and
	// the assignee arm narrows only when it is the one doing the admitting.
	oversight, err := repo.ListCampaignSheds(ctx, repoTenant, task, "", 20, ports.CampaignAccess{
		AuthorizedParkIDs: []string{lcpParkCPT},
	})
	if err != nil {
		t.Fatalf("ListCampaignSheds for a monitor of the task's own park: %v", err)
	}
	if len(oversight.Items) != 2 || oversight.TotalCount != 2 {
		t.Fatalf("ListCampaignSheds for park authority returned %d buckets / total %d, want 2/2", len(oversight.Items), oversight.TotalCount)
	}
}

// TestGetLeadershipShedVideosEvaluatesAccessInsideTheQuery proves the evidence read's park arm is
// a SQL predicate on the campaign row the head query already joins.
//
// It used to be an app-layer check against a park fetched by a preceding CampaignParkID
// statement, so a task that moved park in between was authorized as its old park and had its
// proof footage served from its new one. There is no assignee arm here on purpose: leadership
// evidence review is WeighingMonitor-only.
func TestGetLeadershipShedVideosEvaluatesAccessInsideTheQuery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, repoOtherOp, lcpParkCPT)

	here := lcpUUID(19201)
	hereBucket := lcpUUID(19211)
	lcpInsertCampaign(t, ctx, pool, here, lcpParkCBE, "2026-08-03", domain.StatusPublished, repoOperator)
	lcpInsertBucket(t, ctx, pool, hereBucket, here, lcpShedOne, domain.CategoryIndividualAnimal, repoOperator, 2, "pending")
	elsewhere := lcpUUID(19202)
	elsewhereBucket := lcpUUID(19221)
	lcpInsertCampaign(t, ctx, pool, elsewhere, lcpParkCPT, "2026-08-10", domain.StatusPublished, repoOtherOp)
	lcpInsertBucket(t, ctx, pool, elsewhereBucket, elsewhere, lcpShedCPT, domain.CategoryIndividualAnimal, repoOtherOp, 3, "pending")

	authorizedHere := ports.CampaignAccess{AuthorizedParkIDs: []string{lcpParkCBE}}
	if _, err := repo.GetLeadershipShedVideos(ctx, repoTenant, here, hereBucket, "", 0, authorizedHere); err != nil {
		t.Fatalf("GetLeadershipShedVideos in an authorized park: %v", err)
	}
	if _, err := repo.GetLeadershipShedVideos(ctx, repoTenant, elsewhere, elsewhereBucket, "", 0, authorizedHere); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetLeadershipShedVideos in an UNAUTHORIZED park err = %v, want ErrNotFound -- CROSS-PARK EVIDENCE LEAK", err)
	}

	// Authorized for nothing must admit nothing -- an empty uuid[] arm must not collapse into a
	// match-everything, and it must not evaluate to NULL and drop the row it should have kept.
	if _, err := repo.GetLeadershipShedVideos(ctx, repoTenant, here, hereBucket, "", 0, ports.CampaignAccess{}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetLeadershipShedVideos with no authority at all err = %v, want ErrNotFound", err)
	}
	// An assignee arm is NOT an admission on this surface: proof review is monitor-only.
	if _, err := repo.GetLeadershipShedVideos(ctx, repoTenant, elsewhere, elsewhereBucket, "", 0,
		ports.CampaignAccess{AssigneeUserID: repoOtherOp}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("GetLeadershipShedVideos admitted a bucket on the assignee arm (err = %v); this surface's role gate is WeighingMonitor alone", err)
	}

	for _, pair := range [][2]string{{here, hereBucket}, {elsewhere, elsewhereBucket}} {
		if _, err := repo.GetLeadershipShedVideos(ctx, repoTenant, pair[0], pair[1], "", 0, ports.CampaignAccess{Unrestricted: true}); err != nil {
			t.Fatalf("GetLeadershipShedVideos for a tenant-wide caller on %s: %v", pair[0], err)
		}
	}
}
