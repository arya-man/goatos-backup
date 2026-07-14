package postgres

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

const (
	piTenant        = "00000000-0000-4000-8000-000000000001"
	piParty         = "00000000-0000-4000-8000-000000001001"
	piPark          = "71000000-0000-4000-8000-000000000001"
	piShed          = "71000000-0000-4000-8000-000000000002"
	piStage         = "71000000-0000-4000-8000-000000000003"
	piGoat          = "71000000-0000-4000-8000-000000000004"
	piSOP           = "71000000-0000-4000-8000-000000000005"
	piSOPVersion    = "71000000-0000-4000-8000-000000000006"
	piProtocol      = "71000000-0000-4000-8000-000000000007"
	piVersion       = "71000000-0000-4000-8000-000000000008"
	piRule          = "71000000-0000-4000-8000-000000000009"
	piTask          = "71000000-0000-4000-8000-000000000010"
	piSub           = "71000000-0000-4000-8000-000000000011"
	piBatch         = "71000000-0000-4000-8000-000000000012"
	piObligation    = "71000000-0000-4000-8000-000000000013"
	piCompletion    = "71000000-0000-4000-8000-000000000014"
	piOperator      = "71000000-0000-4000-8000-000000000015"
	piParkHead      = "71000000-0000-4000-8000-000000000016"
	piVerifier      = "71000000-0000-4000-8000-000000000017"
	piProof         = "71000000-0000-4000-8000-000000000018"
	piBatchNext     = "71000000-0000-4000-8000-000000000019"
	piOblNext       = "71000000-0000-4000-8000-000000000020"
	piFeedException = "71000000-0000-4000-8000-000000000021"
)

func TestListRowsProjectsVaccinationProcessIntegrity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("got %d rows want 2: %#v", len(result.Rows), result.Rows)
	}
	row := result.Rows[0]
	if row.Category != domain.CategoryVaccination || row.ParkID != piPark || row.ShedID != piShed {
		t.Fatalf("scope/category = %+v", row)
	}
	if row.WorkState != domain.WorkStateVerificationPending || row.Severity != domain.SeverityWatch {
		t.Fatalf("state/severity = %s/%s", row.WorkState, row.Severity)
	}
	if row.SOPTaskState != domain.SOPStateSubmitted || row.ProofState != domain.ProofStateUploaded || row.VerificationState != domain.VerificationStatePending {
		t.Fatalf("sop/proof/verification = %s/%s/%s", row.SOPTaskState, row.ProofState, row.VerificationState)
	}
	if row.OwnerState != domain.OwnerStateAssigned || row.Owner.OperatorName == nil || *row.Owner.OperatorName != "Operator PI" {
		t.Fatalf("owner = %+v state=%s", row.Owner, row.OwnerState)
	}
	if row.Evidence.EvidenceCount != 1 || len(row.Evidence.ProofIDs) != 1 || row.Evidence.ProofIDs[0] != piProof {
		t.Fatalf("evidence = %+v", row.Evidence)
	}
	if row.NextAction != "Verifier to accept or reject proof" {
		t.Fatalf("next action = %q", row.NextAction)
	}
	if countFor(result.CountsByWorkState, domain.WorkStateVerificationPending) != 1 ||
		countFor(result.CountsByWorkState, domain.WorkStateScheduled) != 1 {
		t.Fatalf("counts = %+v", result.CountsByWorkState)
	}
}

func TestListRowsUsesKeysetCursorAfterFiltering(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	q := domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	}
	first, err := listAtAsOf(t, ctx, repo, q)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Rows) != 1 || first.NextCursor == nil {
		t.Fatalf("first page rows=%d cursor=%v", len(first.Rows), first.NextCursor)
	}
	cursor, err := domain.DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	q.Cursor = &cursor
	second, err := repo.ListRows(ctx, q)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Rows) != 1 {
		t.Fatalf("second page rows=%d want 1", len(second.Rows))
	}
	if first.Rows[0].RowID == second.Rows[0].RowID {
		t.Fatalf("cursor did not advance: %s", first.Rows[0].RowID)
	}
}

func TestListRowsUsesCursorAndKeepsFilteredTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	first, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if first.NextCursor == nil {
		t.Fatalf("first page has no next cursor")
	}
	cursor, err := domain.DecodeCursor(*first.NextCursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	second, err := repo.ListRows(ctx, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
		Cursor:    &cursor,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(first.Rows) != 1 || len(second.Rows) != 1 {
		t.Fatalf("rows first/second = %d/%d, want 1/1", len(first.Rows), len(second.Rows))
	}
	if first.Rows[0].RowID == second.Rows[0].RowID {
		t.Fatalf("cursor did not advance: %s", first.Rows[0].RowID)
	}
	if first.TotalCount != second.TotalCount || second.TotalCount < 2 {
		t.Fatalf("total count first/second = %d/%d, want same filtered total >=2", first.TotalCount, second.TotalCount)
	}
}

func TestListRowsProjectsFeedDirectionProjectionExceptionWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	seedFeedProjectionException(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	category := domain.CategoryFeedDirection
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		Category:  &category,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("got %d rows want 1: %#v", len(result.Rows), result.Rows)
	}
	row := result.Rows[0]
	if row.Category != domain.CategoryFeedDirection ||
		row.RowID != "feed_projection_exception:"+piFeedException ||
		row.WorkState != domain.WorkStateBlocked ||
		row.Severity != domain.SeverityBroken {
		t.Fatalf("feed exception row = %+v", row)
	}
	if row.GapType != "destination_shortage" || row.AnimalStage != "pregnant" ||
		row.ProtocolName != "Feed Direction Counts/Shifting" {
		t.Fatalf("feed exception classification = %+v", row)
	}
	if row.OwnerState != domain.OwnerStateMissing ||
		row.NextAction != "Review pregnant destination ration before Feed Direction generation" ||
		row.BlockerReason == nil ||
		!strings.Contains(*row.BlockerReason, "pregnant destination shed shortage") {
		t.Fatalf("feed exception work fields = %+v", row)
	}
	if row.ParkID != piPark || row.ShedID != piShed || row.Evidence.EvidenceCount != 1 ||
		row.ProofState != domain.ProofStateNotRequired || row.VerificationState != domain.VerificationStateNotReady {
		t.Fatalf("feed exception scope/evidence = %+v", row)
	}
	if countFor(result.CountsByWorkState, domain.WorkStateBlocked) != 1 {
		t.Fatalf("counts = %+v, want one blocked feed exception", result.CountsByWorkState)
	}
}

// TestProcessIntegrityCanonicalListQueryPlanUsesIndexes proves the 5k-50k canonical LIST read
// (processIntegrityCanonicalRowsSQL) lands on the tenant+due_at indexes of the canonical base tables, not a
// sequential scan of obligation_instances/goats/etc. The aggregate/summary plan is proven separately, at
// scale, by TestProcessIntegrityCanonicalAggregateQueryPlanUsesIndexesAtScale.
func TestProcessIntegrityCanonicalListQueryPlanUsesIndexes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set enable_seqscan: %v", err)
	}

	q := normalizeQuery(domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     200,
	})
	args := queryArgs(q)
	plan := explainPlan(t, ctx, tx, "EXPLAIN (COSTS OFF)\n"+processIntegrityCanonicalRowsSQL, args...)

	for _, forbidden := range []string{
		"Seq Scan on obligation_instances",
		"Seq Scan on protocol_versions",
		"Seq Scan on protocol_definitions",
		"Seq Scan on protocol_rules",
		"Seq Scan on goats",
		"Seq Scan on obligation_batches",
		"Seq Scan on sop_tasks",
		"Seq Scan on sop_submissions",
		"Seq Scan on vaccination_completions",
		"Seq Scan on locations",
		"Seq Scan on workforce_members",
	} {
		if strings.Contains(plan, forbidden) {
			t.Fatalf("canonical list plan used %q:\n%s", forbidden, plan)
		}
	}
	if !strings.Contains(plan, "Index Scan") &&
		!strings.Contains(plan, "Index Only Scan") &&
		!strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("canonical list plan did not use an index scan:\n%s", plan)
	}
}

// TestProcessIntegrityCanonicalAggregateQueryPlanUsesIndexesAtScale is the 500k-ENVELOPE GATE for the two
// non-keyset canonical aggregates (CountByWorkState + Protocol Adherence summary) at the ADR upper bound
// (~500k obligation rows, operational-kernel-5k-50k-scale-envelope.md step 4). A green plan on the 5k
// regression fixture, or a plan under enable_seqscan=off, only proves an index EXISTS — not that the planner
// CHOOSES it at scale or that cost stays bounded; the real risk is a plan that is fine small and
// catastrophic large. We load ~500k canonical obligations, ANALYZE so the planner uses real statistics, then
// EXPLAIN (ANALYZE, BUFFERS) BOTH aggregates WITHOUT forcing enable_seqscan off and read the executed plan.
// Each must (a) reach obligation_instances through an index path, never a Seq Scan, (b) touch only the
// bounded due window at that scan (actual rows << 500k), and (c) keep estimated total cost far below a
// full-table-scan aggregate — proving the aggregate stays index-bound at scale rather than degrading to a
// compute-on-read full sequential scan.
func TestProcessIntegrityCanonicalAggregateQueryPlanUsesIndexesAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	seedProcessIntegrityLargeObligationFixture(t, ctx, pool, 500_000)

	// Refresh planner statistics after the bulk load so the plan reflects the ~500k-row reality, not the
	// stale near-empty estimate. This mirrors the mandatory post-seed ANALYZE contract in AGENTS.md.
	execPI(t, ctx, pool, "analyze canonical tables at scale",
		`ANALYZE obligation_instances, obligation_batches, vaccination_completions, sop_tasks, sop_submissions, goats, locations, workforce_members, protocol_versions, protocol_rules, protocol_definitions`)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	// A real operator window is a bounded date range, not the whole tenant history. Anchoring due_after +
	// due_before to a narrow window makes the tenant+due_at index genuinely selective over the ~500k-row
	// table (a single-tenant fixture makes the tenant column alone non-selective), which is exactly the
	// access pattern the aggregate must ride at scale.
	dueAfter := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	scaleQuery := domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueAfter:  &dueAfter,
		DueBefore: time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	}
	countArgs := countQueryArgs(queryArgs(normalizeQuery(scaleQuery)))

	// 500k-ENVELOPE GATE. Both non-keyset aggregates (CountByWorkState + Protocol Adherence summary) must
	// stay index-bound at the ADR upper bound. We EXPLAIN (ANALYZE, BUFFERS) each WITHOUT enable_seqscan=off,
	// so the index path is the planner's OWN choice on real ~500k statistics, and we read the real executed
	// plan (actual rows + estimated cost) rather than merely proving an index EXISTS. Two thresholds bound
	// the plan:
	//   - obligationRowCeiling: the driving obligation_instances scan must touch only the bounded due window
	//     (~1.4k rows/day here), NEVER the whole ~500k table. 50k is a huge margin over the window yet an
	//     order of magnitude below a full-table scan, so a compute-on-read regression that drops the
	//     tenant+due_at selectivity trips it even if PG still labels the node an "Index Scan".
	//   - costCeiling: the estimated total cost must sit far below a 500k sequential-scan-based aggregate
	//     (a full seq scan of the obligation table alone costs well over ~15k here plus the group/sort).
	const (
		obligationRowCeiling = 50_000.0
		costCeiling          = 13_000.0
	)
	for _, agg := range []struct {
		name string
		sql  string
	}{
		{"processIntegrityCanonicalCountsSQL", processIntegrityCanonicalCountsSQL},
		{"processIntegrityCanonicalAdherenceSummarySQL", processIntegrityCanonicalAdherenceSummarySQL},
	} {
		res := explainAnalyzeJSON(t, ctx, tx, agg.sql, countArgs...)
		assertAggregateIndexBoundAtScale(t, agg.name, res, costCeiling, obligationRowCeiling)
	}

	// The aggregate must also RUN within the request budget at scale and stay a bounded, page-independent
	// window total (one count per work_state), not a per-row fan-out.
	repo := NewRepository(pool, 10*time.Second)
	counts, err := repo.CountByWorkState(ctx, scaleQuery)
	if err != nil {
		t.Fatalf("CountByWorkState at scale: %v", err)
	}
	var total int64
	for _, c := range counts {
		total += c.Count
	}
	if total == 0 {
		t.Fatalf("aggregate returned zero rows at scale: %+v", counts)
	}
}

// seedProcessIntegrityLargeObligationFixture bulk-loads count distinct canonical obligations via one
// set-based INSERT ... SELECT generate_series. They target a dedicated scale goat (its own target_id) so the
// UNIQUE(tenant, protocol_version, rule, target_type, target, due_at) dup guard never collides with the
// hand-seeded rows, and each row gets a distinct due_at + id/idempotency_key/sequence. The join tables stay
// tiny, so the fixture stresses the obligation_instances index path specifically — the dominant cost of the
// aggregate at scale.
func seedProcessIntegrityLargeObligationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, count int) {
	t.Helper()
	const piScaleGoat = "71000000-0000-4000-8000-000000000090"
	execPI(t, ctx, pool, "scale goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, shed_id, management_stage, health_status)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		piScaleGoat, piTenant, piParty, piShed, piPark)
	// due_at = 2026-06-14 00:00:00+00 + g minutes gives each obligation a unique due_at; the first ~44k land
	// inside the aggregate's due window (through 2026-07-15) and the tail sits beyond it, so the plan must
	// prune the ~500k-row table to the window on the tenant+due_at index. sequence starts above the
	// hand-seeded rows.
	execPI(t, ctx, pool, "bulk canonical obligations at scale", `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id,
  target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  gen_random_uuid(),
  $1::uuid,
  $2::uuid,
  $3::uuid,
  'goat',
  $4::uuid,
  'shed',
  $5::uuid,
  TIMESTAMPTZ '2026-06-14 00:00:00+00' + (g || ' minutes')::interval,
  'scheduled',
  'pi-scale-' || g::text,
  1000 + g
FROM generate_series(1, $6::int) AS g`,
		piTenant, piVersion, piRule, piScaleGoat, piShed, count)
}

func TestListRowsGroupsUnbatchedVaccinationByBusinessDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	const (
		piGoatSameDay      = "71000000-0000-4000-8000-000000000070"
		piOblSameDayEarly  = "71000000-0000-4000-8000-000000000071"
		piOblSameDayLater  = "71000000-0000-4000-8000-000000000072"
		piSameDayEarlyDue  = "2026-07-10 19:15:00+00" // 2026-07-11 00:45 IST
		piSameDayLaterDue  = "2026-07-11 12:00:00+00" // 2026-07-11 17:30 IST
		piSameDayQueryFrom = "2026-07-10 18:00:00+00"
	)
	execPI(t, ctx, pool, "same-day second goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, shed_id, management_stage, health_status)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		piGoatSameDay, piTenant, piParty, piShed, piPark)
	execPI(t, ctx, pool, "same-day early unbatched obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, $7::timestamptz, 'scheduled', 'pi-same-day-early', 30)`,
		piOblSameDayEarly, piTenant, piVersion, piRule, piGoat, piShed, piSameDayEarlyDue)
	execPI(t, ctx, pool, "same-day later unbatched obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, $7::timestamptz, 'scheduled', 'pi-same-day-later', 31)`,
		piOblSameDayLater, piTenant, piVersion, piRule, piGoatSameDay, piShed, piSameDayLaterDue)

	repo := NewRepository(pool, 5*time.Second)
	category := domain.CategoryVaccination
	dueAfter := time.Date(2026, 7, 10, 18, 0, 0, 0, time.UTC)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		Category:  &category,
		DueAfter:  &dueAfter,
		AsOf:      time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows: %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("same IST business-day unbatched obligations should group to one row, got %d rows: %#v", len(result.Rows), result.Rows)
	}
	row := result.Rows[0]
	if row.ExpectedCount != 2 || !row.DueAt.Equal(time.Date(2026, 7, 10, 19, 15, 0, 0, time.UTC)) {
		t.Fatalf("grouped same-day row expected_count/due_at = %d/%s, want 2/%s", row.ExpectedCount, row.DueAt.Format(time.RFC3339), piSameDayEarlyDue)
	}
	if result.TotalCount != 1 || countFor(result.CountsByWorkState, domain.WorkStateScheduled) != 1 {
		t.Fatalf("same-day grouping counts = total %d states %+v, want one scheduled row", result.TotalCount, result.CountsByWorkState)
	}
	if !row.DueAt.After(dueAfter) {
		t.Fatalf("test setup drift: due_at %s should be after query lower bound %s", row.DueAt, piSameDayQueryFrom)
	}
}

// TestProcessIntegrityRequestPathReadsCanonicalWithoutProjection is the 5k-50k envelope contract: the
// request path serves directly from the canonical obligation/SOP/proof/completion tables. With canonical
// source data present but NO projection ever recomputed (no serving version, projection tables empty), every
// request-path read must SUCCEED and return the reconstructed rows — a canonical read cannot be stale
// relative to the canonical write, so there is no projection-unavailable/stale gate to trip.
func TestProcessIntegrityRequestPathReadsCanonicalWithoutProjection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seeds canonical source rows only; there is no projection recompute path anymore (the
	// process_integrity_projection_* tables and projector were dropped, migrations 000187/000188).
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	q := domain.Query{TenantID: piTenant, AsOf: asOf, DueBefore: asOf.Add(7 * 24 * time.Hour), Limit: 10}

	list, err := repo.ListRows(ctx, q)
	if err != nil {
		t.Fatalf("ListRows without any projection: unexpected err %v", err)
	}
	if len(list.Rows) == 0 {
		t.Fatalf("canonical ListRows returned no rows without a projection: %#v", list)
	}
	if list.Projection.ServingState != "canonical" || list.Projection.Stale {
		t.Fatalf("canonical read must report a live-canonical, non-stale projection marker: %+v", list.Projection)
	}

	counts, err := repo.CountByWorkState(ctx, q)
	if err != nil {
		t.Fatalf("CountByWorkState without any projection: unexpected err %v", err)
	}
	if len(counts) == 0 {
		t.Fatalf("canonical CountByWorkState returned no counts without a projection")
	}

	// Adherence (summary) and single-row drilldown also serve canonically with no projection.
	adherenceQ := q
	adherenceQ.IncludeCompleted = true
	adherenceQ.IncludeAdherenceSummary = true
	adherence, err := repo.ListRows(ctx, adherenceQ)
	if err != nil {
		t.Fatalf("adherence ListRows without any projection: unexpected err %v", err)
	}
	if adherence.AdherenceSummary.ExpectedCount == 0 {
		t.Fatalf("canonical adherence summary must be populated without a projection: %+v", adherence.AdherenceSummary)
	}
	// Single-row drilldown resolves an existing row id straight from canonical data (the seeded rows are
	// batch-grained, so use a real row id from the list rather than an assumed obligation-grained id).
	drilldownID := list.Rows[0].RowID
	if _, found, err := repo.GetRow(ctx, q, drilldownID); err != nil || !found {
		t.Fatalf("GetRow(%q) without any projection: found=%v err=%v, want found with no error", drilldownID, found, err)
	}
}

func TestProcessIntegrityCanonicalHotRowsDoNotComputeWindowTotal(t *testing.T) {
	// The canonical LIST page must not force a full filtered scan for a window total; the total comes from
	// the separate bounded aggregate, never a COUNT(*) OVER on the page.
	upperCanonical := strings.ToUpper(processIntegrityCanonicalRowsSQL)
	if strings.Contains(upperCanonical, "COUNT(*) OVER") || strings.Contains(upperCanonical, "COUNT(1) OVER") {
		t.Fatal("canonical hot row page must not force a full filtered scan for a window total")
	}
}

func seedFeedProjectionException(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execPI(t, ctx, pool, "feed projection exception",
		`INSERT INTO count_projection_exceptions (
		   count_projection_exception_id, tenant_id, exception_type, source_key, grain_key,
		   park_id, shed_id, breed_key, stage_tag, severity, status, owner_ref,
		   work_state, due_at, next_action, evidence_link, blocker_reason, evidence_json
		 ) VALUES (
		   $1::uuid, $2::uuid, 'destination_shortage', 'shifting:pregnant-risk', 'shed:pregnant:beetal',
		   $3::uuid, $4::uuid, 'beetal', 'pregnant', 'critical', 'open', NULL,
		   'blocked', TIMESTAMPTZ '2026-06-24 13:00:00+00',
		   'Review pregnant destination ration before Feed Direction generation',
		   '/feed-direction/counts-projection/exceptions/' || $1::text,
		   'pregnant destination shed shortage after shifting; underfeeding can cause abortion risk',
		   '{"head_count":40,"pregnant_count":12,"risk":"underfeed_abortion"}'::jsonb
		 )`,
		piFeedException, piTenant, piPark, piShed)
}

func TestQueryArgsShapeMatchesRowsAndCountQueries(t *testing.T) {
	args := queryArgs(normalizeQuery(domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	}))
	if len(args) != rowsQueryArgCount {
		t.Fatalf("rows args = %d, want %d", len(args), rowsQueryArgCount)
	}
	// 5k-50k envelope: the request path executes the canonical LIST/AGGREGATE SQL, so the arg contract is
	// validated against the SQL that actually runs. The canonical LIST uses the full 19-arg keyset contract;
	// the counts and adherence aggregates use the 15-arg (countQueryArgs) prefix with no keyset args.
	if rowsPlaceholders := maxPlaceholder(processIntegrityCanonicalRowsSQL); rowsPlaceholders != rowsQueryArgCount {
		t.Fatalf("canonical rows query placeholders = %d, want rows arg count %d", rowsPlaceholders, rowsQueryArgCount)
	}
	if countPlaceholders := maxPlaceholder(processIntegrityCanonicalCountsSQL); countPlaceholders != countQueryArgCount {
		t.Fatalf("canonical count query placeholders = %d, want count arg count %d", countPlaceholders, countQueryArgCount)
	}
	if summaryPlaceholders := maxPlaceholder(processIntegrityCanonicalAdherenceSummarySQL); summaryPlaceholders != countQueryArgCount {
		t.Fatalf("canonical adherence summary placeholders = %d, want count arg count %d", summaryPlaceholders, countQueryArgCount)
	}
	if len(countQueryArgs(args)) != countQueryArgCount {
		t.Fatalf("count args = %d, want %d", len(countQueryArgs(args)), countQueryArgCount)
	}
	if countQueryArgCount >= rowsQueryArgCount {
		t.Fatalf("count args must be a strict prefix of rows args: count=%d rows=%d", countQueryArgCount, rowsQueryArgCount)
	}
}

func maxPlaceholder(sql string) int {
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1)
	maxArg := 0
	for _, match := range matches {
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if n > maxArg {
			maxArg = n
		}
	}
	return maxArg
}

func TestListRowsSurfacesTaskReworkAsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	execPI(t, ctx, pool, "rework task",
		`UPDATE sop_tasks SET state = 'rework_requested' WHERE tenant_id = $1 AND task_id = $2`,
		piTenant, piTask)
	execPI(t, ctx, pool, "clear recorded completion",
		`UPDATE vaccination_completions SET status = 'reversed' WHERE tenant_id = $1 AND completion_id = $2`,
		piTenant, piCompletion)

	repo := NewRepository(pool, 5*time.Second)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	if len(result.Rows) == 0 {
		t.Fatal("expected rows")
	}
	row := result.Rows[0]
	if row.WorkState != domain.WorkStateRejected || row.SOPTaskState != domain.SOPStateRework {
		t.Fatalf("state = %s sop=%s, want rejected/rework row: %+v", row.WorkState, row.SOPTaskState, row)
	}
	if countFor(result.CountsByWorkState, domain.WorkStateRejected) != 1 {
		t.Fatalf("counts = %+v, want rejected count", result.CountsByWorkState)
	}
}

func TestListRowsSurfacesCanonicalDeferredObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	execPI(t, ctx, pool, "defer next obligation",
		`UPDATE obligation_instances SET status = 'deferred' WHERE tenant_id = $1 AND obligation_id = $2`,
		piTenant, piOblNext)
	execPI(t, ctx, pool, "deferred event",
		`INSERT INTO obligation_status_events (tenant_id, obligation_id, event_type, occurred_at, idempotency_key)
		 VALUES ($1, $2, 'deferred', TIMESTAMPTZ '2026-06-24 09:00:00+00', 'pi-deferred-next')`,
		piTenant, piOblNext)

	state := domain.WorkStateDeferred
	repo := NewRepository(pool, 5*time.Second)
	result, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		WorkState: &state,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRows() error = %v", err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("got %d deferred rows want 1: %#v", len(result.Rows), result.Rows)
	}
	row := result.Rows[0]
	if row.WorkState != domain.WorkStateDeferred || row.GapType != "deferred_explained" || row.DeferredCount != 1 {
		t.Fatalf("deferred row = %+v", row)
	}
	if countFor(result.CountsByWorkState, domain.WorkStateDeferred) != 1 {
		t.Fatalf("counts = %+v, want deferred count", result.CountsByWorkState)
	}
}

// TestProcessIntegrityAsOfReconstruction proves item-2 point-in-time correctness for CT/AC/PA/WF:
//   - a 'completed' obligation finalized AFTER as_of re-buckets to its open state (overdue), not completed,
//     and its accepted dose is not counted until as_of passes the completion;
//   - an old completed-after-as_of obligation is still PULLED at as_of (gate is as_of-aware), then drops
//     out as genuine closed history once as_of passes its completion;
//   - SOP-submission evidence submitted AFTER as_of is not seen (evidence count bounded by as_of).
func TestProcessIntegrityAsOfReconstruction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	const (
		piBatchRecent = "71000000-0000-4000-8000-000000000030"
		piOblRecent   = "71000000-0000-4000-8000-000000000031"
		piCompRecent  = "71000000-0000-4000-8000-000000000032"
		piBatchOld    = "71000000-0000-4000-8000-000000000033"
		piOblOld      = "71000000-0000-4000-8000-000000000034"
		piCompOld     = "71000000-0000-4000-8000-000000000035"
	)

	// Recent: due 2026-06-22 (inside the closed-history window), completed_at 2026-06-30 (AFTER the
	// as_of-before probe). Accepted dose administered 2026-06-30.
	execPI(t, ctx, pool, "recent completed batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'completed', DATE '2026-06-22', $5)`,
		piBatchRecent, piTenant, piVersion, piShed, piOperator)
	execPI(t, ctx, pool, "recent completed obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-22 00:00:00+00', 'completed', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'pi-obl-recent', 3)`,
		piOblRecent, piTenant, piVersion, piRule, piBatchRecent, piGoat, piShed)
	execPI(t, ctx, pool, "recent accepted dose",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-30 10:00:00+00', 'accepted', 'pi-comp-recent', $6)`,
		piCompRecent, piTenant, piOblRecent, piBatchRecent, piGoat, piOperator)

	// Old: due 2026-05-01 (OUTSIDE the closed-history window), completed_at 2026-06-30. Only the as_of-aware
	// gate (completed_at > as_of) should pull it at the as_of-before probe.
	execPI(t, ctx, pool, "old completed batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'completed', DATE '2026-05-01', $5)`,
		piBatchOld, piTenant, piVersion, piShed, piOperator)
	execPI(t, ctx, pool, "old completed obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-05-01 00:00:00+00', 'completed', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'pi-obl-old', 4)`,
		piOblOld, piTenant, piVersion, piRule, piBatchOld, piGoat, piShed)
	execPI(t, ctx, pool, "old accepted dose",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-30 10:00:00+00', 'accepted', 'pi-comp-old', $6)`,
		piCompOld, piTenant, piOblOld, piBatchOld, piGoat, piOperator)

	repo := NewRepository(pool, 5*time.Second)
	dueBefore := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	// as_of BEFORE the completions: both completed-after-as_of rows must read as overdue (re-bucketed), not
	// completed, and their accepted dose must not count.
	before, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID: piTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRows(before): %v", err)
	}
	recent := rowByBatchSubstr(before.Rows, piBatchRecent)
	old := rowByBatchSubstr(before.Rows, piBatchOld)
	if recent == nil {
		t.Fatalf("recent completed-after-as_of row missing before completion: %#v", before.Rows)
	}
	if old == nil {
		t.Fatalf("old completed-after-as_of row missing before completion (as_of-aware gate must pull it): %#v", before.Rows)
	}
	if recent.WorkState != domain.WorkStateOverdue || old.WorkState != domain.WorkStateOverdue {
		t.Fatalf("before completion: want both overdue, got recent=%s old=%s", recent.WorkState, old.WorkState)
	}
	if recent.CompletedCount != 0 || recent.VerificationState == domain.VerificationStateAccepted {
		t.Fatalf("before completion: recent must not read completed/accepted, got completed=%d verification=%s", recent.CompletedCount, recent.VerificationState)
	}
	if countFor(before.CountsByWorkState, domain.WorkStateCompleted) != 0 {
		t.Fatalf("before completion: completed count want 0, got %+v", before.CountsByWorkState)
	}

	// as_of AFTER the completions: the recent row now reads completed with its accepted dose; the old row
	// drops out as genuine closed history (completed before as_of, due long ago, includeCompleted=false).
	after, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID: piTenant, AsOf: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRows(after): %v", err)
	}
	recentAfter := rowByBatchSubstr(after.Rows, piBatchRecent)
	if recentAfter == nil || recentAfter.WorkState != domain.WorkStateCompleted {
		t.Fatalf("after completion: recent want completed, got %#v", recentAfter)
	}
	if recentAfter.CompletedCount != 1 || recentAfter.VerificationState != domain.VerificationStateAccepted {
		t.Fatalf("after completion: recent want completed=1 verification=accepted, got completed=%d verification=%s", recentAfter.CompletedCount, recentAfter.VerificationState)
	}
	if rowByBatchSubstr(after.Rows, piBatchOld) != nil {
		t.Fatalf("after completion: old row should drop out as closed history, still present: %#v", after.Rows)
	}

	// Evidence bounding: at an as_of BEFORE the base submission (09:00) and completion (10:00), the base
	// drive's submitted proof must not be seen.
	preEvidence, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID: piTenant, AsOf: time.Date(2026, 6, 24, 8, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRows(preEvidence): %v", err)
	}
	base := rowByBatchSubstr(preEvidence.Rows, piBatch)
	if base == nil {
		t.Fatalf("base drive row missing at pre-evidence as_of: %#v", preEvidence.Rows)
	}
	if base.Evidence.EvidenceCount != 0 || len(base.Evidence.ProofIDs) != 0 {
		t.Fatalf("pre-evidence as_of: base evidence must be unseen (submitted after as_of), got count=%d ids=%v", base.Evidence.EvidenceCount, base.Evidence.ProofIDs)
	}
}

// TestProcessIntegrityBoundsVerificationByVerifiedAt proves the verification-timestamp edge for CT/AC/PA/WF:
// a dose administered before as_of but accepted (verified_at) after as_of reads as verification_pending,
// not completed/accepted, until as_of passes verified_at.
func TestProcessIntegrityBoundsVerificationByVerifiedAt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	const (
		piVerBatch = "71000000-0000-4000-8000-000000000040"
		piVerObl   = "71000000-0000-4000-8000-000000000041"
		piVerComp  = "71000000-0000-4000-8000-000000000042"
	)
	execPI(t, ctx, pool, "verify batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-20', $5)`,
		piVerBatch, piTenant, piVersion, piShed, piOperator)
	execPI(t, ctx, pool, "verify obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-20 00:00:00+00', 'completed', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'pi-verify-obl', 5)`,
		piVerObl, piTenant, piVersion, piRule, piVerBatch, piGoat, piShed)
	// Dose administered 2026-06-20 (before as_of) but ACCEPTED/verified 2026-06-30 (after the before-probe).
	execPI(t, ctx, pool, "verify completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, verified_at, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-20 09:00:00+00', 'accepted', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'pi-verify-comp', $6)`,
		piVerComp, piTenant, piVerObl, piVerBatch, piGoat, piOperator)

	repo := NewRepository(pool, 5*time.Second)
	dueBefore := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)

	before, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID: piTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRows(before verify): %v", err)
	}
	rb := rowByBatchSubstr(before.Rows, piVerBatch)
	if rb == nil {
		t.Fatalf("verify row missing before: %#v", before.Rows)
	}
	if rb.WorkState != domain.WorkStateVerificationPending || rb.VerificationState == domain.VerificationStateAccepted {
		t.Fatalf("before verify: want verification_pending and not accepted, got work=%s verification=%s", rb.WorkState, rb.VerificationState)
	}

	after, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID: piTenant, AsOf: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), DueBefore: dueBefore, Limit: 50,
	})
	if err != nil {
		t.Fatalf("ListRows(after verify): %v", err)
	}
	ra := rowByBatchSubstr(after.Rows, piVerBatch)
	if ra == nil || ra.WorkState != domain.WorkStateCompleted || ra.VerificationState != domain.VerificationStateAccepted {
		t.Fatalf("after verify: want completed + accepted, got %#v", ra)
	}
}

// TestProcessIntegrityAsOfTerminalEventReconstruction proves item-1 point-in-time correctness for the
// missed/waived terminal reconstruction. It pins the four cases the bounded-but-not-naive fix must handle:
//   - a missed event AT OR BEFORE as_of reads as missed (blocked);
//   - a missed event ONLY AFTER as_of (future-only) re-buckets to its open state (overdue) — it was not yet
//     terminal at as_of;
//   - NO terminal history at all falls back to the current stored status (still missed/blocked) — the
//     explicit no-event fallback that the naive `MAX() FILTER (occurred_at <= as_of)` would have collapsed
//     into the future-only case;
//   - a churn (missed at/before as_of AND another missed after as_of) reads as missed, NOT open: the latest
//     terminal event AT OR BEFORE as_of wins, instead of the old unbounded MAX() picking the future event.
func TestProcessIntegrityAsOfTerminalEventReconstruction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	const (
		// missed event proven at/before as_of -> missed
		piTeBatchBefore = "71000000-0000-4000-8000-000000000050"
		piTeOblBefore   = "71000000-0000-4000-8000-000000000051"
		piTeEvtBefore   = "71000000-0000-4000-8000-000000000052"
		// missed event only after as_of (future-only) -> open (overdue)
		piTeBatchFuture = "71000000-0000-4000-8000-000000000053"
		piTeOblFuture   = "71000000-0000-4000-8000-000000000054"
		piTeEvtFuture   = "71000000-0000-4000-8000-000000000055"
		// no terminal history at all -> trust stored status (missed)
		piTeBatchNoEvt = "71000000-0000-4000-8000-000000000056"
		piTeOblNoEvt   = "71000000-0000-4000-8000-000000000057"
		// churn: missed at/before AND missed after as_of -> missed (latest at/before wins)
		piTeBatchChurn  = "71000000-0000-4000-8000-000000000058"
		piTeOblChurn    = "71000000-0000-4000-8000-000000000059"
		piTeEvtChurnOld = "71000000-0000-4000-8000-000000000060"
		piTeEvtChurnNew = "71000000-0000-4000-8000-000000000061"
	)

	// All four obligations: due in early June (well before as_of, so an open re-bucket reads overdue), current
	// stored status 'missed', no completion. due_at varies per obligation to satisfy the dup guard
	// UNIQUE(tenant, protocol_version, rule, target_type, target, due_at).
	seedMissed := func(batch, obl, key, due string, seq int) {
		execPI(t, ctx, pool, "te missed batch "+key,
			`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
			 VALUES ($1, $2, $3, 'shed', $4, 'planned', $5::date, $6)`,
			batch, piTenant, piVersion, piShed, due, piOperator)
		execPI(t, ctx, pool, "te missed obligation "+key,
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
			   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
			 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, ($8::date)::timestamptz, 'missed', $9, $10)`,
			obl, piTenant, piVersion, piRule, batch, piGoat, piShed, due, "pi-te-"+key, seq)
	}
	seedEvent := func(eventID, obl string, occurred string, key string) {
		execPI(t, ctx, pool, "te status event "+key,
			`INSERT INTO obligation_status_events (obligation_event_id, tenant_id, obligation_id, event_type, occurred_at, recorded_at, idempotency_key)
			 VALUES ($1, $2, $3, 'missed', $4::timestamptz, $4::timestamptz, $5)`,
			eventID, piTenant, obl, occurred, "pi-te-evt-"+key)
	}

	seedMissed(piTeBatchBefore, piTeOblBefore, "before", "2026-06-10", 6)
	seedEvent(piTeEvtBefore, piTeOblBefore, "2026-06-20 09:00:00+00", "before") // <= as_of

	seedMissed(piTeBatchFuture, piTeOblFuture, "future", "2026-06-11", 7)
	seedEvent(piTeEvtFuture, piTeOblFuture, "2026-06-30 09:00:00+00", "future") // > as_of

	seedMissed(piTeBatchNoEvt, piTeOblNoEvt, "noevt", "2026-06-12", 8) // no event row at all

	seedMissed(piTeBatchChurn, piTeOblChurn, "churn", "2026-06-13", 9)
	seedEvent(piTeEvtChurnOld, piTeOblChurn, "2026-06-20 09:00:00+00", "churn-old") // <= as_of
	seedEvent(piTeEvtChurnNew, piTeOblChurn, "2026-06-30 09:00:00+00", "churn-new") // > as_of

	repo := NewRepository(pool, 5*time.Second)
	res, err := listAtAsOf(t, ctx, repo, domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		Limit:     50,
	})
	if err != nil {
		t.Fatalf("ListRows: %v", err)
	}

	mustState := func(batch string, want domain.WorkState) {
		t.Helper()
		row := rowByBatchSubstr(res.Rows, batch)
		if row == nil {
			t.Fatalf("row for batch %s missing: %#v", batch, res.Rows)
		}
		if row.WorkState != want {
			t.Fatalf("batch %s: want work_state %s, got %s (gap=%s)", batch, want, row.WorkState, row.GapType)
		}
	}

	// missed proven at/before as_of -> missed, not dependency/stock blocked.
	mustState(piTeBatchBefore, domain.WorkStateMissed)
	// future-only terminal event -> still open at as_of -> overdue.
	mustState(piTeBatchFuture, domain.WorkStateOverdue)
	// no terminal history -> trust stored missed status.
	mustState(piTeBatchNoEvt, domain.WorkStateMissed)
	// churn: latest terminal at/before as_of wins -> missed (NOT overdue).
	mustState(piTeBatchChurn, domain.WorkStateMissed)
}

// listAtAsOf reads the canonical request path directly at q.AsOf. Under the 5k-50k envelope the request
// path reconstructs point-in-time state inline from the canonical tables (same base CTE the projector uses),
// so point-in-time tests exercise exactly the production read — no projection recompute in between.
func listAtAsOf(t *testing.T, ctx context.Context, repo *Repository, q domain.Query) (domain.ListResult, error) {
	t.Helper()
	return repo.ListRows(ctx, q)
}

func rowByBatchSubstr(rows []domain.Row, batchID string) *domain.Row {
	for i := range rows {
		if strings.Contains(rows[i].RowID, batchID) {
			return &rows[i]
		}
	}
	return nil
}

func rowByObligationID(rows []domain.Row, obligationID string) *domain.Row {
	for i := range rows {
		if rows[i].ObligationID == obligationID {
			return &rows[i]
		}
	}
	return nil
}

func rowStateSignature(rows []domain.Row) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.RowID+"|"+row.Category+"|"+string(row.WorkState)+"|"+string(row.Severity)+"|"+row.NextAction)
	}
	return out
}

func countSignature(counts []domain.CountByWorkState) map[domain.WorkState]int64 {
	out := map[domain.WorkState]int64{}
	for _, count := range counts {
		out[count.WorkState] = count.Count
	}
	return out
}

func explainPlan(t *testing.T, ctx context.Context, q pgx.Tx, sql string, args ...any) string {
	t.Helper()
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("explain query: %v", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan rows: %v", err)
	}
	return strings.Join(lines, "\n")
}

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so the 500k-envelope EXPLAIN helper can run on
// either a pooled connection or a rolled-back transaction.
type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// explainPlanNode is one node of an EXPLAIN (FORMAT JSON) plan tree. Only the fields the 500k-envelope gate
// asserts on are decoded.
type explainPlanNode struct {
	NodeType     string            `json:"Node Type"`
	RelationName string            `json:"Relation Name"`
	TotalCost    float64           `json:"Total Cost"`
	PlanRows     float64           `json:"Plan Rows"`
	ActualRows   float64           `json:"Actual Rows"`
	Plans        []explainPlanNode `json:"Plans"`
}

// explainAnalyzeResult is one top-level EXPLAIN (ANALYZE, FORMAT JSON) result object.
type explainAnalyzeResult struct {
	Plan          explainPlanNode `json:"Plan"`
	ExecutionTime float64         `json:"Execution Time"`
}

// explainAnalyzeJSON runs EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on a parameterized query and returns the
// executed plan tree (real statistics: actual rows + estimated cost). ANALYZE means the query is actually
// run, so the returned actual-row counts are ground truth, not planner guesses. It is used by the
// 500k-envelope gate WITHOUT enable_seqscan disabled so the access path is the planner's own choice.
func explainAnalyzeJSON(t *testing.T, ctx context.Context, q pgxQuerier, sql string, args ...any) explainAnalyzeResult {
	t.Helper()
	rows, err := q.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"+sql, args...)
	if err != nil {
		t.Fatalf("explain analyze query: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			t.Fatalf("explain analyze: no plan row: %v", err)
		}
		t.Fatalf("explain analyze: no plan row returned")
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		t.Fatalf("scan explain json: %v", err)
	}
	rows.Close()
	var parsed []explainAnalyzeResult
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal explain json: %v\nraw=%s", err, string(raw))
	}
	if len(parsed) == 0 {
		t.Fatalf("explain analyze: empty plan array\nraw=%s", string(raw))
	}
	return parsed[0]
}

// collectRelationScans walks the plan tree collecting every node whose base relation is rel (e.g. every
// scan of obligation_instances, which the base CTE may reference more than once).
func collectRelationScans(n explainPlanNode, rel string, out *[]explainPlanNode) {
	if n.RelationName == rel {
		*out = append(*out, n)
	}
	for _, c := range n.Plans {
		collectRelationScans(c, rel, out)
	}
}

// planHasIndexAccess reports whether any node in the tree uses an index access path (Index Scan, Index Only
// Scan, or Bitmap Index Scan).
func planHasIndexAccess(n explainPlanNode) bool {
	if strings.Contains(n.NodeType, "Index") {
		return true
	}
	for _, c := range n.Plans {
		if planHasIndexAccess(c) {
			return true
		}
	}
	return false
}

// assertAggregateIndexBoundAtScale enforces the 500k-envelope thresholds on one executed aggregate plan:
// the driving obligation_instances scan must be an index path (no Seq Scan), must touch only the bounded
// due window (actual rows below obligationRowCeiling, i.e. NOT the whole ~500k table), the plan must use an
// index access path overall, and the estimated total cost must stay below costCeiling.
func assertAggregateIndexBoundAtScale(t *testing.T, label string, res explainAnalyzeResult, costCeiling, obligationRowCeiling float64) {
	t.Helper()
	root := res.Plan
	var oblScans []explainPlanNode
	collectRelationScans(root, "obligation_instances", &oblScans)
	if len(oblScans) == 0 {
		t.Fatalf("%s @500k: obligation_instances not referenced in executed plan; cannot prove index-bound access", label)
	}
	var maxObligationRows float64
	for _, s := range oblScans {
		if strings.Contains(s.NodeType, "Seq Scan") {
			t.Fatalf("%s @500k: obligation_instances hit a %q (compute-on-read regression); rootCost=%.0f execTime=%.1fms",
				label, s.NodeType, root.TotalCost, res.ExecutionTime)
		}
		if s.ActualRows > maxObligationRows {
			maxObligationRows = s.ActualRows
		}
	}
	if !planHasIndexAccess(root) {
		t.Fatalf("%s @500k: no index access path anywhere in executed plan", label)
	}
	if maxObligationRows > obligationRowCeiling {
		t.Fatalf("%s @500k: obligation_instances scan touched %.0f rows (> ceiling %.0f) — lost due-window selectivity, effectively a full scan",
			label, maxObligationRows, obligationRowCeiling)
	}
	if root.TotalCost > costCeiling {
		t.Fatalf("%s @500k: estimated total cost %.0f exceeds envelope ceiling %.0f (aggregate no longer bounded at scale)",
			label, root.TotalCost, costCeiling)
	}
	t.Logf("%s @500k index-bound: rootCost=%.0f rootActualRows=%.0f maxObligationScanRows=%.0f execTime=%.1fms",
		label, root.TotalCost, root.ActualRows, maxObligationRows, res.ExecutionTime)
}

func seedProcessIntegrityProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execPI(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARK-PI', 'Process Park', 'active')`,
		piPark, piTenant)
	execPI(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-PI', 'Process Shed', $3, 'active')`,
		piShed, piTenant, piPark)
	execPI(t, ctx, pool, "stage",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1', 'K1 kids', 'active')`,
		piStage, piTenant)
	execPI(t, ctx, pool, "shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		piShed, piTenant, piStage)
	execPI(t, ctx, pool, "shed ops",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		piTenant, piShed)
	execPI(t, ctx, pool, "operator",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'OP-PI', 'Operator PI', 'active', 'operator', $3)`,
		piOperator, piTenant, piShed)
	execPI(t, ctx, pool, "park head",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'PH-PI', 'Park Head PI', 'active', 'park_head', $3)`,
		piParkHead, piTenant, piPark)
	execPI(t, ctx, pool, "verifier",
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
		 VALUES ($1, $2, 'VER-PI', 'Verifier PI', 'active', 'verifier', $3)`,
		piVerifier, piTenant, piPark)
	execPI(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
		piGoat, piTenant, piParty, piShed, piPark)
	execPI(t, ctx, pool, "sop definition",
		`INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
		 VALUES ($1, $2, 'vaccination.pi', 'Vaccination PI', 'Process-integrity regression SOP.', 'active')`,
		piSOP, piTenant)
	execPI(t, ctx, pool, "sop version",
		`INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, validation_report)
		 VALUES ($1, $2, $3, 1, 'v1', 'published',
		   '{"schema_version":"goatos.sop-form.v1","fields":[]}'::jsonb,
		   '{"required":true,"subject_scope":"batch","types":["video"],"minimum_count":1}'::jsonb,
		   '{"valid":true,"errors":[],"warnings":[]}'::jsonb)`,
		piSOPVersion, piTenant, piSOP)
	execPI(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination.pi', 'Rabies PI', 'vaccination', 'active')`,
		piProtocol, piTenant)
	execPI(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy, sop_version_id)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', DATE '2026-06-01', '{}'::jsonb,
		   '{"required":true,"subject_scope":"batch","types":["video"],"minimum_count":1}'::jsonb, $4)`,
		piVersion, piTenant, piProtocol, piSOPVersion)
	execPI(t, ctx, pool, "protocol rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy, sop_version_id)
		 VALUES ($1, $2, $3, 'D1', 1, 'birth_age', '{}'::jsonb,
		   '{"required":true,"subject_scope":"batch","types":["video"],"minimum_count":1}'::jsonb, $4)`,
		piRule, piTenant, piVersion, piSOPVersion)
	execPI(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions
		 SET status = 'published', published_at = COALESCE(published_at, now()), updated_at = now()
		 WHERE tenant_id = $1 AND protocol_version_id = $2`,
		piTenant, piVersion)
	execPI(t, ctx, pool, "sop task",
		`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, assigned_to, scope_type, scope_id, due_at)
		 VALUES ($1, $2, $3, $4, 'vaccination_drive', 'Vaccination PI drive', 'submitted', $5, 'shed', $6, TIMESTAMPTZ '2026-06-24 00:00:00+00')`,
		piTask, piTenant, piSOP, piSOPVersion, piOperator, piShed)
	execPI(t, ctx, pool, "submission",
		`INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, proof_refs, state, submitted_at)
		 VALUES ($1, $2, $3, $4, $5, 'pi-submission', '{}'::jsonb,
		   jsonb_build_array(jsonb_build_object('proof_id', $6::text)), 'submitted', TIMESTAMPTZ '2026-06-24 09:00:00+00')`,
		piSub, piTenant, piTask, piSOPVersion, piOperator, piProof)
	execPI(t, ctx, pool, "batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, sop_task_id, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'in_progress', DATE '2026-06-24', $5, $6)`,
		piBatch, piTenant, piVersion, piShed, piTask, piOperator)
	execPI(t, ctx, pool, "obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, sop_task_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, $6, 'goat', $7, 'shed', $8, TIMESTAMPTZ '2026-06-24 00:00:00+00', 'in_progress', 'pi-obligation', 1)`,
		piObligation, piTenant, piVersion, piRule, piBatch, piTask, piGoat, piShed)
	execPI(t, ctx, pool, "completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, $5, TIMESTAMPTZ '2026-06-24 10:00:00+00', 'recorded', 'pi-completion', $6)`,
		piCompletion, piTenant, piObligation, piBatch, piGoat, piOperator)
	execPI(t, ctx, pool, "next batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date, conducted_by)
		 VALUES ($1, $2, $3, 'shed', $4, 'planned', DATE '2026-06-26', $5)`,
		piBatchNext, piTenant, piVersion, piShed, piOperator)
	execPI(t, ctx, pool, "next obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, $5, 'goat', $6, 'shed', $7, TIMESTAMPTZ '2026-06-26 00:00:00+00', 'scheduled', 'pi-obligation-next', 2)`,
		piOblNext, piTenant, piVersion, piRule, piBatchNext, piGoat, piShed)
}

func execPI(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func countFor(counts []domain.CountByWorkState, state domain.WorkState) int64 {
	for _, c := range counts {
		if c.WorkState == state {
			return c.Count
		}
	}
	return 0
}

// TestProcessIntegrityCanonicalAggregateGrainAdversarial proves the canonical AGGREGATE
// (CountByWorkState / adherence summary over processIntegrityCanonicalCountsSQL and
// processIntegrityCanonicalAdherenceSummarySQL) keeps a correct grain and identity under the
// aggregate-projection review cases: one-to-many fan-out, page boundaries, due-vs-execution date shift,
// scope hierarchy, and the full status matrix. The invariant: the aggregate counts the same park/shed/
// business-date grain the LIST renders — exactly once per grain, independent of underlying obligation
// fan-out and independent of the LIST page size.
func TestProcessIntegrityCanonicalAggregateGrainAdversarial(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)

	const (
		piAdvGoatB   = "71000000-0000-4000-8000-000000000080"
		piAdvGoatC   = "71000000-0000-4000-8000-000000000081"
		piAdvOblA    = "71000000-0000-4000-8000-000000000082" // same-day scheduled, goat piGoat
		piAdvOblB    = "71000000-0000-4000-8000-000000000083" // same-day scheduled, goat piAdvGoatB -> same grain
		piAdvOblDate = "71000000-0000-4000-8000-000000000084" // completed-after-as_of -> re-buckets to overdue
	)
	// Two more goats in the same shed for the fan-out and date-shift grains.
	for _, g := range []string{piAdvGoatB, piAdvGoatC} {
		execPI(t, ctx, pool, "adversarial goat",
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, health_status)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1', 'healthy')`,
			g, piTenant, piParty, piShed, piPark)
	}
	// OneToMany fan-out: two unbatched scheduled obligations, same shed, same IST business day (2026-06-28),
	// different goats -> ONE scheduled grain with expected_count 2. The aggregate must count the grain once.
	execPI(t, ctx, pool, "same-day scheduled A",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-06-28 04:00:00+00', 'scheduled', 'pi-adv-a', 40)`,
		piAdvOblA, piTenant, piVersion, piRule, piGoat, piShed)
	execPI(t, ctx, pool, "same-day scheduled B",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-06-28 09:00:00+00', 'scheduled', 'pi-adv-b', 41)`,
		piAdvOblB, piTenant, piVersion, piRule, piAdvGoatB, piShed)
	// DateShift: due 2026-06-20 but completed_at 2026-06-30 (AFTER as_of 2026-06-24) -> the as_of-effective
	// status re-buckets to overdue even though the stored status is completed. It must count once under
	// overdue, keyed by its execution/completion recency, not its due date.
	execPI(t, ctx, pool, "date-shift completed-after-asof",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-06-20 00:00:00+00', 'completed', TIMESTAMPTZ '2026-06-30 10:00:00+00', 'pi-adv-date', 42)`,
		piAdvOblDate, piTenant, piVersion, piRule, piAdvGoatC, piShed)

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	baseQuery := func(limit int) domain.Query {
		return domain.Query{TenantID: piTenant, AsOf: asOf, DueBefore: dueBefore, Limit: limit}
	}

	list, err := repo.ListRows(ctx, baseQuery(100))
	if err != nil {
		t.Fatalf("ListRows: %v", err)
	}
	counts, err := repo.CountByWorkState(ctx, baseQuery(1))
	if err != nil {
		t.Fatalf("CountByWorkState: %v", err)
	}
	grainsByState := map[domain.WorkState]int64{}
	for _, row := range list.Rows {
		grainsByState[row.WorkState]++
	}

	t.Run("OneToMany fan-out grain counted once", func(t *testing.T) {
		// The two same-day scheduled obligations must render as ONE grain with expected_count 2.
		var fanoutGrain *domain.Row
		for i := range list.Rows {
			if list.Rows[i].WorkState == domain.WorkStateScheduled && list.Rows[i].ExpectedCount == 2 {
				fanoutGrain = &list.Rows[i]
				break
			}
		}
		if fanoutGrain == nil {
			t.Fatalf("same-day scheduled obligations did not fan into one grain of expected_count 2: %#v", list.Rows)
		}
		// The aggregate scheduled bucket must equal the number of scheduled GRAINS in the list, not the
		// number of underlying obligations (which would double-count the fan-out).
		if got := countFor(counts, domain.WorkStateScheduled); got != grainsByState[domain.WorkStateScheduled] {
			t.Fatalf("scheduled aggregate double-counted fan-out: aggregate=%d list grains=%d", got, grainsByState[domain.WorkStateScheduled])
		}
	})

	t.Run("PageBoundary total independent of page size", func(t *testing.T) {
		countsSmall, err := repo.CountByWorkState(ctx, baseQuery(1))
		if err != nil {
			t.Fatalf("CountByWorkState(limit 1): %v", err)
		}
		countsLarge, err := repo.CountByWorkState(ctx, baseQuery(100))
		if err != nil {
			t.Fatalf("CountByWorkState(limit 100): %v", err)
		}
		if !reflect.DeepEqual(countSignature(countsSmall), countSignature(countsLarge)) {
			t.Fatalf("aggregate window total changed with page size: limit1=%v limit100=%v", countsSmall, countsLarge)
		}
		var total int64
		for _, c := range countsLarge {
			total += c.Count
		}
		if total != int64(len(list.Rows)) {
			t.Fatalf("aggregate total %d != rendered grain count %d", total, len(list.Rows))
		}
	})

	t.Run("DateShift execution-date re-bucket counted once", func(t *testing.T) {
		var overdueGrains int
		for _, row := range list.Rows {
			if row.WorkState == domain.WorkStateOverdue && row.ObligationID == piAdvOblDate {
				overdueGrains++
			}
		}
		if overdueGrains != 1 {
			t.Fatalf("completed-after-as_of obligation must re-bucket to exactly one overdue grain, got %d", overdueGrains)
		}
		if countFor(counts, domain.WorkStateOverdue) < 1 {
			t.Fatalf("aggregate overdue bucket missing the re-bucketed row: %+v", counts)
		}
	})

	t.Run("ScopeHierarchy ParkScope and shed scope match membership", func(t *testing.T) {
		park := piPark
		parkScoped, err := repo.CountByWorkState(ctx, func() domain.Query { q := baseQuery(1); q.ParkID = &park; return q }())
		if err != nil {
			t.Fatalf("park-scoped CountByWorkState: %v", err)
		}
		// Single park in the fixture: park scope must equal the unscoped total.
		if !reflect.DeepEqual(countSignature(parkScoped), countSignature(counts)) {
			t.Fatalf("park scope changed the total for a single-park fixture: park=%v all=%v", parkScoped, counts)
		}
		shed := piShed
		shedScoped, err := repo.CountByWorkState(ctx, func() domain.Query { q := baseQuery(1); q.ShedID = &shed; return q }())
		if err != nil {
			t.Fatalf("shed-scoped CountByWorkState: %v", err)
		}
		if !reflect.DeepEqual(countSignature(shedScoped), countSignature(counts)) {
			t.Fatalf("shed scope changed the total for a single-shed fixture: shed=%v all=%v", shedScoped, counts)
		}
	})

	t.Run("StatusMatrix every rendered bucket appears in the aggregate", func(t *testing.T) {
		aggregate := countSignature(counts)
		for state, grains := range grainsByState {
			if aggregate[state] != grains {
				t.Fatalf("status matrix mismatch for %s: aggregate=%d rendered grains=%d", state, aggregate[state], grains)
			}
		}
	})
}
