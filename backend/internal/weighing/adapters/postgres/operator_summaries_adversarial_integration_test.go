package postgres

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Adversarial regression tests for operatorSummaries (operator_summaries.go).
//
// The projection-review marker on that read makes three load-bearing claims. A
// marker is a claim, not a proof, so each claim gets a fixture built SPECIFICALLY
// to break it, and each test was confirmed to go RED against the corresponding
// deliberate break in the production SQL:
//
//	cardinality — an inactive duplicate workforce_members row must not split or
//	              double a person's numbers (RED when op.status='active' is dropped)
//	pagination  — the roll-up is a whole-filter aggregate bounded by
//	              domain.MaxOperatorSummaries, never derived from a page of rows
//	              (RED when the summary is scoped to the page's campaign ids, and
//	              RED when the cap is not the cap)
//	status      — the four state counts partition the person's buckets across
//	              EVERY status the CHECK constraint admits, with canceled excluded
//	              (RED when cs.status <> 'canceled' is dropped)

// -----------------------------------------------------------------------------
// 1. CARDINALITY
// -----------------------------------------------------------------------------

// TestOperatorSummaryOneToManyJoinsDoNotMultiply is the adversarial fixture for
// the marker's join_cardinality claim.
//
// workforce_members has NO plain unique key on (tenant_id, user_id). Its only
// uniqueness is workforce_members_active_user_unique_idx, a PARTIAL index that
// applies WHERE status='active'. The real schema therefore permits — and an HRMS
// re-hire or a corrected roster row produces — a SECOND row for the same person
// with status<>'active'. The LEFT JOIN in operatorSummaries is 1:N against that
// table unless op.status='active' holds it to one row.
//
// Two shapes of the same defect are seeded, because they fail differently:
//
//	operator A's stale row carries a DIFFERENT display_name. The query groups on
//	(operator_user_id, op.display_name), so the unfiltered join SPLITS A into two
//	summary rows — one person appearing twice on the Operators screen, each row
//	telling a different half-truth.
//
//	operator B's stale row carries the SAME display_name. The group does not split;
//	instead every bucket row is matched twice, so ShedCount, every state count and
//	both animal facts DOUBLE. "2 sheds, 3 animals weighed" silently reads as
//	"4 sheds, 6 animals weighed".
//
// Each operator's buckets are also spread across TWO campaigns, so the 1:1
// weighing_campaigns join is exercised at the same time: two campaigns must SUM to
// one person's total, never multiply it.
func TestOperatorSummaryOneToManyJoinsDoNotMultiply(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	operatorA := lcpUUID(17001)
	operatorB := lcpUUID(17002)
	osGrantPark(t, ctx, pool, operatorA, repoPark)
	osGrantPark(t, ctx, pool, operatorB, repoPark)

	// A: active row plus an inactive row under a DIFFERENT name -> would split.
	osInsertWorkforceMember(t, ctx, pool, operatorA, "OS-A-ACTIVE", "AAA Anita Active", "active")
	osInsertWorkforceMember(t, ctx, pool, operatorA, "OS-A-STALE", "AAA Anita Stale", "inactive")
	// B: active row plus an inactive row under the SAME name -> would double.
	osInsertWorkforceMember(t, ctx, pool, operatorB, "OS-B-ACTIVE", "BBB Bela", "active")
	osInsertWorkforceMember(t, ctx, pool, operatorB, "OS-B-STALE", "BBB Bela", "left")

	shedA1, shedA2 := lcpUUID(17011), lcpUUID(17012)
	shedB1, shedB2 := lcpUUID(17013), lcpUUID(17014)
	lsInsertShed(t, ctx, pool, shedA1, repoPark, "OS Shed A1", 901)
	lsInsertShed(t, ctx, pool, shedA2, repoPark, "OS Shed A2", 902)
	lsInsertShed(t, ctx, pool, shedB1, repoPark, "OS Shed B1", 903)
	lsInsertShed(t, ctx, pool, shedB2, repoPark, "OS Shed B2", 904)

	campaignOne, campaignTwo := lcpUUID(17021), lcpUUID(17022)
	lcpInsertCampaign(t, ctx, pool, campaignOne, repoPark, "2026-09-07", domain.StatusPublished, operatorA)
	lcpInsertCampaign(t, ctx, pool, campaignTwo, repoPark, "2026-09-14", domain.StatusPublished, operatorA)

	bucketA1, bucketA2 := lcpUUID(17031), lcpUUID(17032)
	bucketB1, bucketB2 := lcpUUID(17033), lcpUUID(17034)
	lcpInsertBucket(t, ctx, pool, bucketA1, campaignOne, shedA1, domain.CategoryIndividualAnimal, operatorA, 0, "pending")
	lcpInsertBucket(t, ctx, pool, bucketA2, campaignTwo, shedA2, domain.CategoryPerShedPartition, operatorA, 0, "pending")
	lcpInsertBucket(t, ctx, pool, bucketB1, campaignOne, shedB1, domain.CategoryIndividualAnimal, operatorB, 0, "pending")
	lcpInsertBucket(t, ctx, pool, bucketB2, campaignTwo, shedB2, domain.CategoryIndividualAnimal, operatorB, 0, "pending")

	// Real recorded work, so the animal facts are non-zero and a doubled join is
	// visible in them too rather than only in the shed count.
	proofA2, proofB1 := lcpUUID(17041), lcpUUID(17042)
	lcpInsertProof(t, ctx, pool, proofA2, shedA2)
	lcpInsertProof(t, ctx, pool, proofB1, shedB1)

	const lumpSumHead = 7
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID:        repoTenant,
		CampaignID:      campaignTwo,
		CampaignShedID:  bucketA2,
		AnimalCount:     lumpSumHead,
		WeightKg:        350,
		ProofArtifactID: proofA2,
		RecordedBy:      operatorA,
		IdempotencyKey:  "os:card:lump:a2",
	}); err != nil {
		t.Fatalf("record lump sum for operator A: %v", err)
	}
	for i := 0; i < 3; i++ {
		lcpCaptureAs(t, ctx, pool, repo, campaignOne, bucketB1, proofB1,
			fmt.Sprintf("os-card-b-%d", i), fmt.Sprintf("os:card:capture:b:%d", i), operatorB)
	}

	summaries, err := repo.operatorSummaries(ctx, repoTenant, "", repoPark)
	if err != nil {
		t.Fatalf("operator summaries: %v", err)
	}

	// The split shape: exactly ONE row may exist per operator, whatever the roster
	// table holds. osSummary fatals when a person appears twice.
	gotA := osSummary(t, summaries, operatorA)
	gotB := osSummary(t, summaries, operatorB)

	// The doubling shape: the numbers themselves.
	osAssertSummary(t, "operator A", gotA, domain.OperatorSummary{
		OperatorUserID:      operatorA,
		OperatorDisplayName: "AAA Anita Active",
		ShedCount:           2,
		NotStartedCount:     1, // bucketA1, untouched
		// bucketA2: a lump-sum proof IS the submission, so the write path moves the
		// bucket straight to 'completed' — it never sits in 'in_progress'.
		SubmittedCount:      1,
		AnimalsWeighedCount: lumpSumHead,
		// A standing lump-sum proof IS the submission (see
		// weighed_vs_submitted_integration_test.go).
		AnimalsSubmittedCount: lumpSumHead,
	})
	osAssertSummary(t, "operator B", gotB, domain.OperatorSummary{
		OperatorUserID:      operatorB,
		OperatorDisplayName: "BBB Bela",
		ShedCount:           2,
		// Both buckets are still 'pending': free-flow captures do not move a bucket's
		// status, Submit does. That is precisely the mid-shift state the two animal
		// facts below exist to make visible.
		NotStartedCount:     2,
		AnimalsWeighedCount: 3,
		// The mid-shift state: recorded, NOT submitted.
		AnimalsSubmittedCount: 0,
	})

	// Independently of the exact expected numbers above, the structural invariant:
	// the four state counts must still partition the person's buckets. A join that
	// multiplied rows would keep this identity true, which is exactly why the
	// absolute assertions above are the ones that catch doubling — but a join that
	// multiplied only ONE side would break it here.
	osAssertStatePartition(t, "operator A", gotA)
	osAssertStatePartition(t, "operator B", gotB)

	// Guard against a vacuous pass: if the adversarial roster rows were silently
	// rejected, the fixture would not be adversarial at all.
	osAssertRosterRowCount(t, ctx, pool, operatorA, 2)
	osAssertRosterRowCount(t, ctx, pool, operatorB, 2)
}

// -----------------------------------------------------------------------------
// 2. PAGINATION
// -----------------------------------------------------------------------------

// TestOperatorSummaryIsWholeFilterAcrossEveryPageBoundary is the adversarial
// fixture for the marker's pagination claim.
//
// The repo rule is that a summary is a WHOLE-FILTER aggregate unless it is
// explicitly named page_*: pagination changes which ROWS a caller sees, never what
// the summary says. operatorSummaries is reached through listCampaigns, which IS
// paged — so the property under test is that the OperatorSummaries block is
// byte-for-byte identical on every page of every page size.
//
// This is the defect the read exists to prevent: grouping the loaded page on the
// client produced per-person totals that described the page and changed as the
// reader scrolled.
func TestOperatorSummaryIsWholeFilterAcrossEveryPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	operator := lcpUUID(17101)
	osGrantPark(t, ctx, pool, operator, repoPark)
	osInsertWorkforceMember(t, ctx, pool, operator, "OS-PAGE-1", "PAGE Priya", "active")

	shed := lcpUUID(17111)
	lsInsertShed(t, ctx, pool, shed, repoPark, "OS Page Shed", 910)

	// Enough campaigns that a page size of 1, 2 or 3 genuinely splits them.
	const campaigns = 5
	for i := 0; i < campaigns; i++ {
		campaignID := lcpUUID(17121 + i)
		lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, osDate(i), domain.StatusPublished, operator)
		lcpInsertBucket(t, ctx, pool, lcpUUID(17141+i), campaignID, shed, domain.CategoryIndividualAnimal, operator, 0, "pending")
	}

	// The truth: one unpaged read over the whole filter.
	whole, err := repo.ListCampaigns(ctx, repoTenant, repoPark, "", 100)
	if err != nil {
		t.Fatalf("whole-filter list: %v", err)
	}
	want := whole.OperatorSummaries
	// Anti-vacuity: the assertion below compares summaries, so an empty or
	// operator-less summary block would make every comparison trivially true.
	truth := osSummary(t, want, operator)
	if truth.ShedCount != campaigns {
		t.Fatalf("whole-filter shed_count=%d, want %d: fixture did not build the multi-page shape", truth.ShedCount, campaigns)
	}
	if len(whole.Items) < campaigns {
		t.Fatalf("whole-filter items=%d, want >= %d", len(whole.Items), campaigns)
	}

	for _, limit := range []int{1, 2, 3, 4, 100} {
		cursor := ""
		pages := 0
		seenRows := 0
		for {
			page, err := repo.ListCampaigns(ctx, repoTenant, repoPark, cursor, limit)
			if err != nil {
				t.Fatalf("limit=%d page=%d: %v", limit, pages, err)
			}
			pages++
			seenRows += len(page.Items)
			if !reflect.DeepEqual(page.OperatorSummaries, want) {
				t.Fatalf("limit=%d page=%d: operator summaries differ from the whole-filter truth.\n got=%+v\nwant=%+v\n"+
					"a summary that changes with the page is a page total wearing a person's name",
					limit, pages, page.OperatorSummaries, want)
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
			if pages > 200 {
				t.Fatalf("limit=%d: cursor walk did not terminate", limit)
			}
		}
		// Anti-vacuity per limit: a page size of 1 must actually have produced
		// several pages, otherwise "identical across pages" proves nothing.
		if limit < campaigns && pages < 2 {
			t.Fatalf("limit=%d produced %d page(s); the boundary was never crossed", limit, pages)
		}
		if seenRows < campaigns {
			t.Fatalf("limit=%d walked %d rows, want >= %d", limit, seenRows, campaigns)
		}
	}
}

// TestOperatorSummaryMultiPageCapBoundsAndIsDeterministic proves the OTHER half of
// the pagination claim: "pagination=NONE by design, bounded instead by
// domain.MaxOperatorSummaries".
//
// An unpaged read is only safe if the bound actually bounds. This seeds MORE
// operators than the cap in a park of their own and asserts (a) the result is
// capped, and (b) the cap is DETERMINISTIC — it keeps the first N by the read's
// own ORDER BY (display_name, then operator id), not an arbitrary N. An arbitrary
// truncation would make the Operators screen show a different subset of people on
// every refresh, which is worse than paging.
func TestOperatorSummaryMultiPageCapBoundsAndIsDeterministic(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// A park of its own, so the cap is measured against exactly the operators this
	// test seeds and not against the shared fixture's people.
	capPark := lcpUUID(17201)
	osInsertPark(t, ctx, pool, capPark, "OS Cap Park")
	capShed := lcpUUID(17202)
	lsInsertShed(t, ctx, pool, capShed, capPark, "OS Cap Shed", 920)

	overflow := domain.MaxOperatorSummaries + 5
	names := make([]string, 0, overflow)
	for i := 0; i < overflow; i++ {
		operator := lcpUUID(17300 + i)
		name := fmt.Sprintf("capop-%02d", i)
		names = append(names, name)
		osGrantPark(t, ctx, pool, operator, capPark)
		osInsertWorkforceMember(t, ctx, pool, operator, fmt.Sprintf("OS-CAP-%02d", i), name, "active")
		campaignID := lcpUUID(17400 + i)
		lcpInsertCampaign(t, ctx, pool, campaignID, capPark, osDate(i), domain.StatusPublished, operator)
		lcpInsertBucket(t, ctx, pool, lcpUUID(17500+i), campaignID, capShed, domain.CategoryIndividualAnimal, operator, 0, "pending")
	}

	// Anti-vacuity: the fixture must actually exceed the cap.
	if overflow <= domain.MaxOperatorSummaries {
		t.Fatalf("fixture seeds %d operators, which does not exceed the cap of %d", overflow, domain.MaxOperatorSummaries)
	}

	first, err := repo.operatorSummaries(ctx, repoTenant, "", capPark)
	if err != nil {
		t.Fatalf("operator summaries: %v", err)
	}
	if len(first) != domain.MaxOperatorSummaries {
		t.Fatalf("summaries=%d, want exactly the cap %d (seeded %d operators): the bound must bound",
			len(first), domain.MaxOperatorSummaries, overflow)
	}

	// Deterministic: the read's ORDER BY is display_name ASC, operator id ASC, so
	// the survivors are the alphabetically first MaxOperatorSummaries names.
	sort.Strings(names)
	wantNames := names[:domain.MaxOperatorSummaries]
	gotNames := make([]string, 0, len(first))
	for _, s := range first {
		gotNames = append(gotNames, s.OperatorDisplayName)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Fatalf("capped set is not the deterministic head of the ordering.\n got=%v\nwant=%v", gotNames, wantNames)
	}

	// Stable across repeated reads: same rows, same order.
	second, err := repo.operatorSummaries(ctx, repoTenant, "", capPark)
	if err != nil {
		t.Fatalf("operator summaries (second read): %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("capped set changed between two identical reads:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

// -----------------------------------------------------------------------------
// 3. STATUS
// -----------------------------------------------------------------------------

// TestOperatorSummaryStatusBucketsCoverEveryStatus is the adversarial fixture for
// the marker's disjoint/exhaustive claim.
//
// The marker asserts that the shed status CHECK constraint (migration 000058)
// admits exactly {pending, in_progress, completed, closed, canceled}, that
// 'canceled' is excluded from membership, and that the four FILTERs partition the
// remaining four one-to-one so their sum IS ShedCount.
//
// The test therefore (a) reads the CHECK constraint out of the live catalog and
// fails if the admitted set is not exactly the five it covers — so a migration
// that adds a sixth status cannot silently fall through an uncounted crack — and
// (b) seeds one bucket in EVERY one of those statuses, not only the ones ordinary
// fixtures happen to produce, plus a live bucket under a CANCELED CAMPAIGN, which
// the WHERE clause excludes for the same reason.
func TestOperatorSummaryStatusBucketsCoverEveryStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// (a) The schema's own list of statuses, not a list copied into the test.
	admitted := osShedStatusesFromCheckConstraint(t, ctx, pool)
	covered := []string{"canceled", "closed", "completed", "in_progress", "pending"}
	if !reflect.DeepEqual(admitted, covered) {
		t.Fatalf("weighing_campaign_sheds_status_check admits %v, but this test covers %v.\n"+
			"A status the summary does not bucket is a bucket that vanishes from the state ladder — "+
			"add a FILTER in operator_summaries.go and extend this test.", admitted, covered)
	}

	operator := lcpUUID(17601)
	osGrantPark(t, ctx, pool, operator, repoPark)
	osInsertWorkforceMember(t, ctx, pool, operator, "OS-STATUS-1", "STATUS Sunita", "active")
	shed := lcpUUID(17602)
	lsInsertShed(t, ctx, pool, shed, repoPark, "OS Status Shed", 930)

	// (b) One bucket per admitted status, each in its own campaign so the
	// (tenant, campaign, location) uniqueness is respected.
	for i, status := range covered {
		campaignID := lcpUUID(17610 + i)
		lcpInsertCampaign(t, ctx, pool, campaignID, repoPark, osDate(i), domain.StatusPublished, operator)
		lcpInsertBucket(t, ctx, pool, lcpUUID(17620+i), campaignID, shed, domain.CategoryIndividualAnimal, operator, 0, status)
	}
	// A LIVE bucket whose CAMPAIGN is canceled. Retracted work at either grain is
	// not this person's work.
	canceledCampaign := lcpUUID(17650)
	lcpInsertCampaign(t, ctx, pool, canceledCampaign, repoPark, osDate(90), domain.StatusPublished, operator)
	lcpInsertBucket(t, ctx, pool, lcpUUID(17651), canceledCampaign, shed, domain.CategoryIndividualAnimal, operator, 0, "pending")
	execWeighingTestSQL(t, ctx, pool,
		`UPDATE weighing_campaigns SET status='canceled' WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`,
		repoTenant, canceledCampaign)

	// Anti-vacuity: 6 buckets exist on the row table; only 4 may be counted.
	osAssertBucketRowCount(t, ctx, pool, operator, len(covered)+1)

	got := osSummary(t, osSummaries(t, ctx, repo, repoPark), operator)

	// Exactly one bucket in each live state, and canceled counted nowhere.
	osAssertSummary(t, "status matrix", got, domain.OperatorSummary{
		OperatorUserID:      operator,
		OperatorDisplayName: "STATUS Sunita",
		ShedCount:           4,
		NotStartedCount:     1,
		CapturingCount:      1,
		SubmittedCount:      1,
		AcceptedCount:       1,
	})
	// Disjoint AND exhaustive: the identity the marker claims.
	osAssertStatePartition(t, "status matrix", got)
	// Stated explicitly rather than only implied by ShedCount==4: two buckets
	// exist that the projection must refuse — the canceled bucket and the bucket
	// under the canceled campaign.
	if got.ShedCount != len(covered)+1-2 {
		t.Fatalf("shed_count=%d, want %d: canceled buckets and buckets of canceled campaigns are retracted work and belong to no state",
			got.ShedCount, len(covered)+1-2)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// osDate spaces campaigns out by week so the
// (tenant, park, period_type, cadence, period_start_date) uniqueness holds.
func osDate(i int) string {
	return time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC).AddDate(0, 0, 7*i).Format("2006-01-02")
}

func osInsertPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID, name string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, 'active')
ON CONFLICT (location_id) DO UPDATE SET name=EXCLUDED.name`, parkID, repoTenant, name)
}

func osGrantPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, parkID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, repoTenant, userID, parkID)
}

func osInsertWorkforceMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, code, name, status string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'operator')`, repoTenant, userID, code, name, status)
}

func osSummaries(t *testing.T, ctx context.Context, repo *Repository, parkID string) []domain.OperatorSummary {
	t.Helper()
	summaries, err := repo.operatorSummaries(ctx, repoTenant, "", parkID)
	if err != nil {
		t.Fatalf("operator summaries: %v", err)
	}
	return summaries
}

// osSummary returns the ONE summary row for an operator. Finding zero or more than
// one is itself a failure: the projection's grain is one row per operator, so a
// duplicate row is the cardinality defect, not a detail of lookup.
func osSummary(t *testing.T, summaries []domain.OperatorSummary, operatorID string) domain.OperatorSummary {
	t.Helper()
	matches := make([]domain.OperatorSummary, 0, 2)
	for _, s := range summaries {
		if s.OperatorUserID == operatorID {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("operator %s has %d summary rows, want exactly 1 (grain is one row per operator): %+v",
			operatorID, len(matches), matches)
	}
	return matches[0]
}

func osAssertSummary(t *testing.T, label string, got, want domain.OperatorSummary) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s summary mismatch:\n got=%+v\nwant=%+v", label, got, want)
	}
}

// osAssertStatePartition pins the identity the marker claims: the four state
// counts are disjoint and exhaustive over the person's buckets. ReworkCount is
// deliberately NOT in the sum — it is a flag that overlaps the four.
func osAssertStatePartition(t *testing.T, label string, s domain.OperatorSummary) {
	t.Helper()
	sum := s.NotStartedCount + s.CapturingCount + s.SubmittedCount + s.AcceptedCount
	if sum != s.ShedCount {
		t.Fatalf("%s: not_started+capturing+submitted+accepted = %d, but shed_count = %d; "+
			"the four state counts must partition the person's buckets", label, sum, s.ShedCount)
	}
}

func osAssertRosterRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx,
		`SELECT count(*)::int FROM workforce_members WHERE tenant_id=$1::uuid AND user_id=$2::uuid`,
		repoTenant, userID).Scan(&got); err != nil {
		t.Fatalf("count workforce rows: %v", err)
	}
	if got != want {
		t.Fatalf("workforce_members rows for %s = %d, want %d: the duplicate-roster fixture did not land, "+
			"so this test would prove nothing", userID, got, want)
	}
}

func osAssertBucketRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, operatorID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx,
		`SELECT count(*)::int FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND operator_user_id=$2::uuid`,
		repoTenant, operatorID).Scan(&got); err != nil {
		t.Fatalf("count bucket rows: %v", err)
	}
	if got != want {
		t.Fatalf("weighing_campaign_sheds rows for %s = %d, want %d: the status fixture did not land", operatorID, got, want)
	}
}

// osShedStatusesFromCheckConstraint reads the statuses the LIVE schema admits, so
// the status test is anchored to the migration rather than to a list a future
// reader could forget to extend.
func osShedStatusesFromCheckConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	var def string
	if err := pool.QueryRow(ctx, `
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
JOIN pg_class t ON t.oid = c.conrelid
WHERE t.relname = 'weighing_campaign_sheds'
  AND c.conname = 'weighing_campaign_sheds_status_check'`).Scan(&def); err != nil {
		t.Fatalf("read shed status check constraint: %v", err)
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(def, "'") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || strings.ContainsAny(trimmed, "(),=") {
			continue
		}
		seen[trimmed] = true
	}
	out := make([]string, 0, len(seen))
	for status := range seen {
		out = append(out, status)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("parsed no statuses out of constraint def %q", def)
	}
	return out
}
