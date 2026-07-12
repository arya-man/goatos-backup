package postgres

import (
	"context"
	"errors"
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

func TestProcessIntegrityProductionQueryPlanUsesIndexes(t *testing.T) {
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
	// C35-002: the request path is projection-only. Validate the bounded projection read plan (indexed
	// lookup on the serving read model), NOT the canonical god-CTE — the canonical SQL now exists solely
	// for the off-request projector recompute.
	plan := explainPlan(t, ctx, tx, "EXPLAIN (COSTS OFF)\n"+processIntegrityProjectionRowsSQL, args...)
	plan += "\n" + explainPlan(t, ctx, tx, "EXPLAIN (COSTS OFF)\n"+processIntegrityProjectionCountsSQL, countQueryArgs(args)...)

	for _, forbidden := range []string{
		"Seq Scan on process_integrity_projection_rows",
		"Seq Scan on process_integrity_projection_summaries",
		"Seq Scan on process_integrity_projection_state",
	} {
		if strings.Contains(plan, forbidden) {
			t.Fatalf("process integrity projection plan used %q:\n%s", forbidden, plan)
		}
	}
	if !strings.Contains(plan, "Index Scan") &&
		!strings.Contains(plan, "Index Only Scan") &&
		!strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("process integrity projection plan did not use an index scan:\n%s", plan)
	}
}

func TestProcessIntegrityProjectionRecomputeServesHotReadsWithParity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	seedFeedProjectionException(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	q := domain.Query{
		TenantID:  piTenant,
		AsOf:      time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     50,
	}
	// C35-002: request reads are projection-only. Recompute publishes the serving version (compute-on-write,
	// same base CTE the canonical read used to run inline), then the request path serves an indexed read.
	recomputed, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: q.AsOf})
	if err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}

	projected, err := repo.ListRows(ctx, q)
	if err != nil {
		t.Fatalf("projected ListRows: %v", err)
	}
	if len(projected.Rows) == 0 {
		t.Fatal("projected read returned no rows after recompute")
	}
	if recomputed.Rows < int64(len(projected.Rows)) {
		t.Fatalf("projection rows=%d < served page rows=%d", recomputed.Rows, len(projected.Rows))
	}
	// The served read and the recompute run the same base CTE, so the serving signatures must be stable
	// across a second identical read (deterministic keyset order, no drift).
	projectedAgain, err := repo.ListRows(ctx, q)
	if err != nil {
		t.Fatalf("second projected ListRows: %v", err)
	}
	if !reflect.DeepEqual(rowStateSignature(projected.Rows), rowStateSignature(projectedAgain.Rows)) {
		t.Fatalf("projected rows not stable across reads\nfirst=%v\nsecond=%v", rowStateSignature(projected.Rows), rowStateSignature(projectedAgain.Rows))
	}
	if !reflect.DeepEqual(countSignature(projected.CountsByWorkState), countSignature(projectedAgain.CountsByWorkState)) {
		t.Fatalf("projected counts not stable across reads\nfirst=%v\nsecond=%v", projected.CountsByWorkState, projectedAgain.CountsByWorkState)
	}

	feed := domain.CategoryFeedDirection
	feedOnly, err := repo.ListRows(ctx, domain.Query{
		TenantID:  piTenant,
		Category:  &feed,
		AsOf:      q.AsOf,
		DueBefore: q.DueBefore,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("projected feed ListRows: %v", err)
	}
	if len(feedOnly.Rows) != 1 || feedOnly.Rows[0].Category != domain.CategoryFeedDirection {
		t.Fatalf("projected feed rows = %#v", feedOnly.Rows)
	}
}

func TestProcessIntegrityProjectionRecomputePrunesStaleGenerationRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("initial RecomputeProjection: %v", err)
	}
	execPI(t, ctx, pool, "insert stale projection row", `
INSERT INTO process_integrity_projection_rows (
  tenant_id, category, row_id, sort_priority, process_key, obligation_id, due_at,
  expected_count, work_state, severity, owner_state, process_intact,
  projection_version, projected_at, updated_at
) VALUES (
  $1::uuid, 'vaccination', 'stale-row', 99, 'stale-row', 'stale-obligation',
  $2::timestamptz, 1, 'due', 'watch', 'assigned', false, -1, now(), now()
)`, piTenant, asOf)

	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("second RecomputeProjection: %v", err)
	}
	var staleRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM process_integrity_projection_rows WHERE tenant_id = $1::uuid AND row_id = 'stale-row'`, piTenant).Scan(&staleRows); err != nil {
		t.Fatalf("count stale projection rows: %v", err)
	}
	if staleRows != 0 {
		t.Fatalf("stale projection rows = %d, want pruned", staleRows)
	}
}

func TestProcessIntegrityProjectionKeepsUnbatchedHistoryOutOfNextCycle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	const (
		historyObl  = "71000000-0000-4000-8000-000000000050"
		historyComp = "71000000-0000-4000-8000-000000000051"
		nextObl     = "71000000-0000-4000-8000-000000000052"
	)
	execPI(t, ctx, pool, "completed unbatched history obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, completed_at, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-02-21 03:30:00+00',
		   'completed', TIMESTAMPTZ '2026-02-21 03:30:00+00', 'pi-history-obl', 50)`,
		historyObl, piTenant, piVersion, piRule, piGoat, piShed)
	execPI(t, ctx, pool, "accepted unbatched history completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, administered_at, status, idempotency_key, recorded_by)
		 VALUES ($1, $2, $3, $4, TIMESTAMPTZ '2026-02-21 03:30:00+00', 'accepted', 'pi-history-comp', $5)`,
		historyComp, piTenant, historyObl, piGoat, piOperator)
	execPI(t, ctx, pool, "scheduled unbatched next-cycle obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
		   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
		 VALUES ($1, $2, $3, $4, 'goat', $5, 'shed', $6, TIMESTAMPTZ '2026-12-20 03:30:00+00',
		   'scheduled', 'pi-next-obl', 51)`,
		nextObl, piTenant, piVersion, piRule, piGoat, piShed)

	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 7, 11, 11, 30, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}
	var projectedHistoryRows int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM process_integrity_projection_rows
WHERE tenant_id = $1::uuid
  AND obligation_id = $2::text`, piTenant, historyObl).Scan(&projectedHistoryRows); err != nil {
		t.Fatalf("count projected history rows: %v", err)
	}
	if projectedHistoryRows != 0 {
		t.Fatalf("old completed history rows must stay out of the hot Action Center projection, got %d", projectedHistoryRows)
	}
	category := domain.CategoryVaccination
	result, err := repo.ListRows(ctx, domain.Query{
		TenantID:  piTenant,
		Category:  &category,
		AsOf:      asOf,
		DueBefore: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("ListRows: %v", err)
	}
	if row := rowByObligationID(result.Rows, historyObl); row != nil {
		t.Fatalf("completed history row should be hidden from default Action Center, got %#v", row)
	}
	next := rowByObligationID(result.Rows, nextObl)
	if next == nil {
		t.Fatalf("next-cycle row missing: %#v", result.Rows)
	}
	if next.WorkState != domain.WorkStateScheduled {
		t.Fatalf("next-cycle work_state = %s, want scheduled (row=%#v)", next.WorkState, next)
	}
	if next.CompletedCount != 0 || next.ExpectedCount != 1 {
		t.Fatalf("next-cycle counts completed/expected = %d/%d, want 0/1", next.CompletedCount, next.ExpectedCount)
	}
	for _, row := range result.Rows {
		if row.DueAt.Equal(time.Date(2026, 2, 21, 3, 30, 0, 0, time.UTC)) && row.WorkState == domain.WorkStateOverdue {
			t.Fatalf("historical completion due date leaked as overdue row: %#v", row)
		}
	}
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

func TestProcessIntegrityProjectionPruneExhaustsMultipleBatches(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("initial RecomputeProjection: %v", err)
	}
	execPI(t, ctx, pool, "insert stale projection rows spanning multiple prune batches", `
INSERT INTO process_integrity_projection_rows (
  tenant_id, category, row_id, sort_priority, process_key, obligation_id, due_at,
  expected_count, work_state, severity, owner_state, process_intact,
  projection_version, projected_at, updated_at
)
SELECT
  $1::uuid, 'vaccination', 'stale-row-' || g::text, 99, 'stale-row-' || g::text,
  'stale-obligation-' || g::text, $2::timestamptz, 1, 'due', 'watch', 'assigned', false,
  -1, now(), now()
FROM generate_series(1, 7) AS g`, piTenant, asOf)

	if err := repo.pruneOldProjectionRowsWithBatchSize(ctx, piTenant, 2); err != nil {
		t.Fatalf("pruneOldProjectionRowsWithBatchSize: %v", err)
	}

	var staleRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*)::int FROM process_integrity_projection_rows WHERE tenant_id = $1::uuid AND projection_version = -1`, piTenant).Scan(&staleRows); err != nil {
		t.Fatalf("count stale projection rows: %v", err)
	}
	if staleRows != 0 {
		t.Fatalf("stale projection rows = %d, want all batches pruned", staleRows)
	}
	var servingRows int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*)::int
FROM process_integrity_projection_rows rows
JOIN process_integrity_projection_state state
  ON state.tenant_id = rows.tenant_id
 AND state.serving_projection_version = rows.projection_version
WHERE rows.tenant_id = $1::uuid`, piTenant).Scan(&servingRows); err != nil {
		t.Fatalf("count serving projection rows: %v", err)
	}
	if servingRows == 0 {
		t.Fatal("serving projection rows were pruned")
	}
}

func TestProcessIntegrityProjectionPruneReturnsDatabaseAndCancellationErrors(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	if err := repo.pruneOldProjectionRowsWithBatchSize(ctx, "not-a-uuid", 10); err == nil {
		t.Fatal("invalid tenant prune returned nil, want database error")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := repo.pruneOldProjectionRowsWithBatchSize(canceled, piTenant, 10); err == nil {
		t.Fatal("canceled prune returned nil, want observable cancellation error")
	}
}

func TestProcessIntegrityProjectionRefusesStaleAndRebuildStates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}
	q := normalizeQuery(domain.Query{TenantID: piTenant, AsOf: asOf, DueBefore: asOf.Add(24 * time.Hour), Limit: 10})
	execPI(t, ctx, pool, "mark projection stale", `
UPDATE process_integrity_projection_state
SET serving_state = 'stale',
    freshness_status = 'yellow',
    as_of = $2::timestamptz
WHERE tenant_id = $1::uuid`, piTenant, asOf.Add(-24*time.Hour))
	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionStale) {
		t.Fatalf("stale projection error=%v, want ErrProjectionStale", err)
	}
	execPI(t, ctx, pool, "mark projection rebuilding", `
UPDATE process_integrity_projection_state
SET serving_state = 'rebuilding',
    freshness_status = 'yellow',
    as_of = $2::timestamptz,
    row_count = (SELECT COUNT(*) FROM process_integrity_projection_rows WHERE tenant_id = $1::uuid)
WHERE tenant_id = $1::uuid`, piTenant, asOf.Add(-10*time.Minute))
	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionStale) {
		t.Fatalf("rebuilding projection error=%v, want ErrProjectionStale", err)
	}
	execPI(t, ctx, pool, "mark projection failed", `
UPDATE process_integrity_projection_state
SET serving_state = 'failed',
    freshness_status = 'red'
WHERE tenant_id = $1::uuid`, piTenant)
	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionStale) {
		t.Fatalf("failed projection error=%v, want ErrProjectionStale", err)
	}
	execPI(t, ctx, pool, "clear serving projection while rebuilding", `
UPDATE process_integrity_projection_state
SET serving_state = 'rebuilding',
    freshness_status = 'unknown',
    serving_projection_version = NULL
WHERE tenant_id = $1::uuid`, piTenant)
	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("no serving projection error=%v, want ErrProjectionUnavailable", err)
	}
}

// TestProcessIntegrityRequestPathRefusesCanonicalReplayWhenProjectionUnavailable is the C35-002
// regression: canonical obligation/SOP/proof/completion data is present (seedProcessIntegrityProjection),
// but no projection has been recomputed, so there is no serving version. Every request-path read MUST
// return domain.ErrProjectionUnavailable — never silently reconstruct the answer from the raw tables via
// the god-CTE. Before the fix, ListRows/CountByWorkState fell through to listRowsCanonical and returned
// rows; after it they surface the honest typed error and do exactly ONE bounded lookup against the
// projection-state table.
func TestProcessIntegrityRequestPathRefusesCanonicalReplayWhenProjectionUnavailable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Seeds canonical source rows only; no RecomputeProjection => no serving projection version.
	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	q := domain.Query{TenantID: piTenant, AsOf: asOf, DueBefore: asOf.Add(24 * time.Hour), Limit: 10}

	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("ListRows without serving projection: err = %v, want ErrProjectionUnavailable (no canonical replay)", err)
	}
	if _, err := repo.CountByWorkState(ctx, q); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("CountByWorkState without serving projection: err = %v, want ErrProjectionUnavailable", err)
	}
	// Adherence (IncludeCompleted) and single-row drilldown must also refuse canonical replay.
	adherenceQ := q
	adherenceQ.IncludeCompleted = true
	adherenceQ.IncludeAdherenceSummary = true
	if _, err := repo.ListRows(ctx, adherenceQ); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("adherence ListRows without serving projection: err = %v, want ErrProjectionUnavailable", err)
	}
	if _, _, err := repo.GetRow(ctx, q, "obligation:"+piObligation); !errors.Is(err, domain.ErrProjectionUnavailable) {
		t.Fatalf("GetRow without serving projection: err = %v, want ErrProjectionUnavailable", err)
	}

	// After a recompute publishes a serving version, the same read is served from the projection.
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}
	if _, err := repo.ListRows(ctx, q); err != nil {
		t.Fatalf("ListRows after recompute: unexpected err %v", err)
	}
}

func TestProcessIntegrityProjectionFailsClosedWhenOverAge(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}
	execPI(t, ctx, pool, "mark projection stale", `
UPDATE process_integrity_projection_state
SET serving_state = 'stale',
    freshness_status = 'yellow',
    as_of = $2::timestamptz
WHERE tenant_id = $1::uuid`, piTenant, asOf.Add(-24*time.Hour))
	_, err := repo.ListRows(ctx, domain.Query{
		TenantID:  piTenant,
		AsOf:      asOf,
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     1,
	})
	if !errors.Is(err, domain.ErrProjectionStale) {
		t.Fatalf("ListRows stale projection error=%v, want ErrProjectionStale", err)
	}
}

func TestProcessIntegrityHistoricalReadRequiresExactSnapshot(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	requestedAsOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: requestedAsOf.Add(30 * time.Second)}); err != nil {
		t.Fatalf("RecomputeProjection(newer): %v", err)
	}
	q := domain.Query{
		TenantID:       piTenant,
		AsOf:           requestedAsOf,
		HistoricalAsOf: true,
		DueBefore:      requestedAsOf.Add(24 * time.Hour),
		Limit:          10,
	}
	if _, err := repo.ListRows(ctx, q); !errors.Is(err, domain.ErrProjectionStale) {
		t.Fatalf("newer snapshot historical read error=%v, want ErrProjectionStale", err)
	}
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: requestedAsOf}); err != nil {
		t.Fatalf("RecomputeProjection(exact): %v", err)
	}
	if _, err := repo.ListRows(ctx, q); err != nil {
		t.Fatalf("exact historical snapshot read: %v", err)
	}
}

func TestProcessIntegrityProjectionReadPlanDoesNotReplayCanonicalTables(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedProcessIntegrityProjection(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: piTenant, AsOf: asOf}); err != nil {
		t.Fatalf("RecomputeProjection: %v", err)
	}

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
		AsOf:      asOf,
		DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Limit:     200,
	})
	args := queryArgs(q)
	plan := explainPlan(t, ctx, tx, "EXPLAIN (COSTS OFF)\n"+processIntegrityProjectionRowsSQL, args...)
	plan += "\n" + explainPlan(t, ctx, tx, "EXPLAIN (COSTS OFF)\n"+processIntegrityProjectionCountsSQL, countQueryArgs(args)...)
	lowerPlan := strings.ToLower(plan)
	if !strings.Contains(lowerPlan, "process_integrity_projection_rows") {
		t.Fatalf("projection hot read plan must read process_integrity_projection_rows:\n%s", plan)
	}
	if !strings.Contains(lowerPlan, "process_integrity_projection_summaries") {
		t.Fatalf("summary/count hot read must use preaggregated summary rows:\n%s", plan)
	}
	for _, forbidden := range []string{
		"obligation_instances",
		"obligation_status_events",
		"vaccination_completions",
		"sop_tasks",
		"sop_submissions",
		"goats",
		"protocol_versions",
		"protocol_rules",
		"workforce_members",
	} {
		if strings.Contains(lowerPlan, forbidden) {
			t.Fatalf("projection hot read plan replays %s:\n%s", forbidden, plan)
		}
	}
	if !strings.Contains(plan, "Index Scan") &&
		!strings.Contains(plan, "Bitmap Index Scan") &&
		!strings.Contains(plan, "Index Only Scan") {
		t.Fatalf("projection hot read plan did not use an index:\n%s", plan)
	}
}

func TestProcessIntegrityProjectionInsertSQLUpsertsRows(t *testing.T) {
	if !strings.Contains(processIntegrityProjectionInsertSQL, "ON CONFLICT (tenant_id, projection_version, row_id) DO UPDATE") {
		t.Fatal("projection insert SQL must upsert by tenant/version/row so recompute builds a new serving generation before flipping")
	}
}

func TestProcessIntegrityHotRowsDoNotComputeWindowTotalAndSummariesUseBusinessDayGrain(t *testing.T) {
	upperRows := strings.ToUpper(processIntegrityProjectionRowsSQL)
	if strings.Contains(upperRows, "COUNT(*) OVER") || strings.Contains(upperRows, "COUNT(1) OVER") {
		t.Fatal("hot row page must not force a full filtered scan for a window total")
	}
	if !strings.Contains(processIntegrityProjectionSummaryInsertSQL, "due_business_date") ||
		!strings.Contains(processIntegrityProjectionSummaryInsertSQL, "AT TIME ZONE 'Asia/Kolkata'") {
		t.Fatal("summary projector must collapse due timestamps to the IST business-date grain")
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
	// C35-002: request reads are projection-only, so the arg contract is validated against the projection
	// SQL the request path actually executes.
	if rowsPlaceholders := maxPlaceholder(processIntegrityProjectionRowsSQL); rowsPlaceholders != rowsQueryArgCount {
		t.Fatalf("projection rows query placeholders = %d, want rows arg count %d", rowsPlaceholders, rowsQueryArgCount)
	}
	if countPlaceholders := maxPlaceholder(processIntegrityProjectionCountsSQL); countPlaceholders != countQueryArgCount {
		t.Fatalf("projection count query placeholders = %d, want count arg count %d", countPlaceholders, countQueryArgCount)
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

// listAtAsOf recomputes the tenant projection at q.AsOf (compute-on-write) and then reads it back. After
// C35-002 request reads no longer reconstruct point-in-time state from canonical tables; the as_of
// reconstruction lives in the projector (same base CTE). Point-in-time tests therefore prove the
// behaviour THROUGH the projector + projection read path that production uses.
func listAtAsOf(t *testing.T, ctx context.Context, repo *Repository, q domain.Query) (domain.ListResult, error) {
	t.Helper()
	if _, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: q.TenantID, AsOf: q.AsOf}); err != nil {
		t.Fatalf("RecomputeProjection(asOf=%s): %v", q.AsOf, err)
	}
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
