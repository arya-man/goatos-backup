package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The leadership execution table: EVERY pen-session the frozen sheet directed for one feed day, with
// its distribution-proof state -- including the pen-sessions NOBODY TOUCHED (maintainer decision
// 2026-08-26: operators were skipping proof uploads and no screen could show it).
//
// The fixture below is adversarial on every dimension the aggregate rules name, and each part is
// here because getting it wrong is a defect that has actually shipped in this module before:
//
//   - TWO FEED ITEMS on one pen-session, so a fan-out would duplicate the line.
//   - TWO PENS of the same shed, one fed and one not: keying on shed alone (the pre-000137 bug)
//     would report the untouched pen as fed on its sibling's video.
//   - TWO SESSIONS of the same pen, one fed and one not: a pen's morning and evening are separate
//     bags with separate videos (the 2026-08-11 restoration), so collapsing them hides half the work.
//   - TWO PARKS, so a scope leak is visible.
//   - ALL FOUR status buckets present at once, including a REWORK row that must not read as untouched.

const (
	fdcParkB  = "fd100000-0000-4000-8000-00000000300b"
	fdcShedB1 = "fd100000-0000-4000-8000-00000000400b"
)

// completionFixture seeds one feed day across two parks and returns the repository and that day.
func completionFixture(t *testing.T, ctx context.Context) (*Repository, time.Time) {
	t.Helper()
	repo, pool := setupIssueDB(t, ctx)
	feedDay := "2026-07-30"
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	// The completion write has an FK to locations for its shed, and validates the pen against the
	// partition CATALOG; the sheet needs neither. Seeding both also lets the assertions prove the
	// CANONICAL location name is preferred over the sheet's snapshot.
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($3::uuid, $1::uuid, 'shed', 'CASTRO', 'Castro', 'active', $2::uuid)
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdiPark, fdiShedA)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdcParkB)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($3::uuid, $1::uuid, 'shed', 'GODEL1', 'Godel 1', 'active', $2::uuid)
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdcParkB, fdcShedB1)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, '1', '1', 'active', 'manual'),
       ($1::uuid, $2::uuid, '2', '2', 'active', 'manual'),
       ($1::uuid, $3::uuid, 'Part 1', 'part 1', 'active', 'manual')
ON CONFLICT DO NOTHING`, fdiTenant, fdiShedA, fdcShedB1)

	cell := func(parkID, parkLabel, shedID, shedLabel, partition string, session int32, rowSeq int32) domain.StoredCell {
		label := "Morning"
		if session == 2 {
			label = "Evening"
		}
		return domain.StoredCell{
			ParkID: parkID, ParkLabel: parkLabel, ShedID: shedID, ShedLabel: shedLabel,
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: label,
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Concentrate", FeedItemKey: "concentrate", QuantityKg: kg("1.000"),
			SessionTotalKg: "1.000", RowSeq: rowSeq, ItemSeq: 0,
		}
	}
	// A SECOND feed item on Castro 1 morning: the sheet is one row per CELL, so the expected set
	// must collapse the item side before the join or that pen-session would list twice.
	hay := cell(fdiPark, "CBE", fdiShedA, "Castro", "1", 1, 0)
	hay.FeedItemLabel, hay.FeedItemKey, hay.ItemSeq = "Hay", "hay", 1

	persist := func(parkID string, cells []domain.StoredCell) {
		t.Helper()
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: parkID, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-completion-" + parkID,
			IdempotencyKey: "issue:" + fdiTenant + ":" + parkID + ":" + feedDay + ":normal",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("PersistIssue %s: %v", parkID, err)
		}
	}
	persist(fdiPark, []domain.StoredCell{
		cell(fdiPark, "CBE", fdiShedA, "Castro", "1", 1, 0), hay,
		cell(fdiPark, "CBE", fdiShedA, "Castro", "1", 2, 1),
		cell(fdiPark, "CBE", fdiShedA, "Castro", "2", 1, 2),
	})
	persist(fdcParkB, []domain.StoredCell{
		cell(fdcParkB, "CPT", fdcShedB1, "Godel 1", "Part 1", 1, 0),
		cell(fdcParkB, "CPT", fdcShedB1, "Godel 1", "Part 1", 2, 1),
	})

	target := biztime.BusinessDayStart(time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()))
	complete := func(parkID, shedID, partition string, session int32, key string) string {
		t.Helper()
		res, err := repo.CompleteDistribution(ctx, ports.CompleteDistributionParams{
			TenantID: fdiTenant, ParkID: parkID, ShedID: shedID, PartitionLabel: partition,
			SessionNo: session, TargetDate: target, Workflow: domain.WorkflowNormal,
			FeedWeightProofRef:   "weight-" + key,
			DistributionProofRef: "feed-" + key,
			WaterProofRef:        "water-" + key,
			IdempotencyKey:       "complete-" + key, ActorType: "operator", TraceID: "t-" + key,
		})
		if err != nil {
			t.Fatalf("CompleteDistribution %s: %v", key, err)
		}
		return res.CompletionID
	}
	// Castro 1 morning: filmed, awaiting a verdict. Castro 2 morning: filmed and BOUNCED.
	// Godel 1 - Part 1 morning: approved. Everything else nobody touched.
	complete(fdiPark, fdiShedA, "1", 1, "a1m")
	bounced := complete(fdiPark, fdiShedA, "2", 1, "a2m")
	if _, err := repo.BounceDistributionForRework(ctx, ports.BounceDistributionParams{
		TenantID: fdiTenant, CompletionID: bounced, Reason: "the water not visible", TraceID: "t-bounce",
	}); err != nil {
		t.Fatalf("BounceDistributionForRework: %v", err)
	}
	approved := complete(fdcParkB, fdcShedB1, "Part 1", 1, "b1m")
	if _, err := repo.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
		TenantID: fdiTenant, CompletionID: approved, VerifiedBy: "", TraceID: "t-approve",
	}); err != nil {
		t.Fatalf("ApplyVerifiedDistribution: %v", err)
	}
	return repo, target
}

// completionRead runs the completion arm with the given narrowing.
func completionRead(
	t *testing.T, ctx context.Context, repo *Repository, target time.Time,
	mutate func(*domain.DirectedAnalyticsQuery),
) domain.ExecutionAnalytics {
	t.Helper()
	q := domain.DirectedAnalyticsQuery{
		DateFrom:      time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:        time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		Sections:      []domain.ExecutionSection{domain.ExecutionSectionDistributionCompletions},
		CompletionDay: target,
	}
	if mutate != nil {
		mutate(&q)
	}
	got, err := repo.ExecutionAnalytics(ctx, fdiTenant, q)
	if err != nil {
		t.Fatalf("ExecutionAnalytics(%+v): %v", q, err)
	}
	return got
}

func completionKeys(rows []domain.DistributionCompletionRow) map[string]domain.DistributionCompletionRow {
	out := map[string]domain.DistributionCompletionRow{}
	for _, row := range rows {
		out[row.OperationalLocationDisplay+"|"+row.SessionLabel] = row
	}
	return out
}

// CARDINALITY: two feed items on one pen-session must not become two lines, and a pen nobody fed
// must still occupy one.
func TestDistributionCompletionTableOneToManyGrainProofs(t *testing.T) {
	ctx := context.Background()
	repo, target := completionFixture(t, ctx)
	got := completionRead(t, ctx, repo, target, nil)

	byKey := completionKeys(got.DistributionCompletions)
	// FIVE pen-sessions from SIX sheet cells: the second feed item on Castro 1 morning collapses.
	if len(got.DistributionCompletions) != 5 || len(byKey) != 5 {
		t.Fatalf("want 5 distinct pen-sessions, got %d rows / %d keys: %+v",
			len(got.DistributionCompletions), len(byKey), byKey)
	}

	// THE SIBLING-PEN AND SIBLING-SESSION PROOF: Castro 1 EVENING was never fed, and neither Castro
	// 1 morning's video nor Castro 2's may close it out.
	untouched, ok := byKey["Castro 1|Evening"]
	if !ok {
		t.Fatalf("an untouched pen-session must still occupy a line: %+v", byKey)
	}
	if untouched.Status != domain.DistributionCompletionNotStarted {
		t.Errorf("Castro 1 evening = %q, want not_started", untouched.Status)
	}
	if untouched.SubmittedAt != nil || untouched.SubmittedByName != "" {
		t.Errorf("an untouched pen-session carries no submitter: %+v", untouched)
	}
	if len(untouched.Proofs) != len(domain.DistributionSlotOrder) {
		t.Fatalf("every row carries all three slots, got %d", len(untouched.Proofs))
	}
	for _, slot := range untouched.Proofs {
		if slot.ProofRef != "" {
			t.Errorf("untouched slot %q must carry no reference, got %q", slot.FieldKey, slot.ProofRef)
		}
	}

	// The fed sibling is unaffected, and its proofs are the ones its OWN completion names.
	fed := byKey["Castro 1|Morning"]
	if fed.Status != domain.DistributionCompletionAwaitingVerification || fed.SubmittedAt == nil {
		t.Errorf("Castro 1 morning: %+v", fed)
	}
	if fed.Proofs[0].ProofRef != "weight-a1m" || fed.Proofs[2].ProofRef != "water-a1m" {
		t.Errorf("Castro 1 morning proof refs: %+v", fed.Proofs)
	}
	// Location display is the canonical composition, both halves, on both naming shapes.
	if fed.ShedLabel != "Castro" {
		t.Errorf("shed label = %q, want the canonical locations name", fed.ShedLabel)
	}
	if _, ok := byKey["Godel 1 - Part 1|Morning"]; !ok {
		t.Errorf("a worded partition must render dashed: %+v", byKey)
	}
}

// STATUS: all four buckets at once, rework distinct from untouched, and totals that ignore the
// status filter.
func TestDistributionCompletionTableStatusBuckets(t *testing.T) {
	ctx := context.Background()
	repo, target := completionFixture(t, ctx)
	got := completionRead(t, ctx, repo, target, nil)
	byKey := completionKeys(got.DistributionCompletions)

	want := map[string]string{
		"Castro 1|Morning":         domain.DistributionCompletionAwaitingVerification,
		"Castro 1|Evening":         domain.DistributionCompletionNotStarted,
		"Castro 2|Morning":         domain.DistributionCompletionRework,
		"Godel 1 - Part 1|Morning": domain.DistributionCompletionCompleted,
		"Godel 1 - Part 1|Evening": domain.DistributionCompletionNotStarted,
	}
	for key, status := range want {
		row, ok := byKey[key]
		if !ok {
			t.Fatalf("%s missing: %+v", key, byKey)
		}
		if row.Status != status {
			t.Errorf("%s = %q, want %q", key, row.Status, status)
		}
	}
	// A BOUNCED pen-session is its own bucket and carries the verifier's sentence -- it must never
	// read as a pen nobody went to.
	if byKey["Castro 2|Morning"].ReworkReason != "the water not visible" {
		t.Errorf("rework reason = %q", byKey["Castro 2|Morning"].ReworkReason)
	}

	if got.CompletionTotals != (domain.CompletionStatusTotals{
		NotStarted: 2, AwaitingVerification: 1, Rework: 1, Completed: 1,
	}) {
		t.Errorf("day totals = %+v, want 2/1/1/1", got.CompletionTotals)
	}

	// A status filter narrows the ROWS and leaves the counts and the filter vocabulary alone.
	reworkOnly := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
		q.CompletionStatus = domain.DistributionCompletionRework
	})
	if len(reworkOnly.DistributionCompletions) != 1 ||
		reworkOnly.DistributionCompletions[0].Status != domain.DistributionCompletionRework {
		t.Fatalf("status filter must return only the bounced pen-session: %+v", reworkOnly.DistributionCompletions)
	}
	if reworkOnly.CompletionTotals != got.CompletionTotals {
		t.Errorf("the four counts must ignore the status filter: %+v vs %+v",
			reworkOnly.CompletionTotals, got.CompletionTotals)
	}
	if len(reworkOnly.CompletionFilterOptions) != len(got.CompletionFilterOptions) {
		t.Errorf("filter options must ignore the filters themselves: %+v", reworkOnly.CompletionFilterOptions)
	}

	// A status nobody defines is REFUSED: answering it with every row would misstate the day under
	// the heading of the filter the caller asked for.
	if _, err := repo.ExecutionAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: target, DateTo: target,
		Sections:         []domain.ExecutionSection{domain.ExecutionSectionDistributionCompletions},
		CompletionDay:    target,
		CompletionStatus: "pending",
	}); !errors.Is(err, domain.ErrInvalidCompletionStatus) {
		t.Errorf("an unknown status filter must be refused, got %v", err)
	}
}

// PAGINATION: the rows are a PAGE, the totals are not, and the pages are disjoint and complete.
func TestDistributionCompletionTablePageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, target := completionFixture(t, ctx)

	page := func(limit, offset int) domain.ExecutionAnalytics {
		return completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
			q.CompletionLimit, q.CompletionOffset = limit, offset
		})
	}
	first := page(2, 0)
	if len(first.DistributionCompletions) != 2 || !first.DistributionCompletionsHasMore {
		t.Fatalf("page 1 of 2 over 5: %d rows, hasMore=%v",
			len(first.DistributionCompletions), first.DistributionCompletionsHasMore)
	}
	// The counts describe the whole day whatever the page size -- summaries are whole-filter
	// aggregates, never page-local (operational read-model contract).
	if first.CompletionTotals != (domain.CompletionStatusTotals{
		NotStarted: 2, AwaitingVerification: 1, Rework: 1, Completed: 1,
	}) {
		t.Errorf("totals must not follow the page: %+v", first.CompletionTotals)
	}
	last := page(2, 4)
	if len(last.DistributionCompletions) != 1 || last.DistributionCompletionsHasMore {
		t.Fatalf("last page: %d rows, hasMore=%v",
			len(last.DistributionCompletions), last.DistributionCompletionsHasMore)
	}

	// Disjoint and complete across the three pages: a stable ORDER BY is what makes paging
	// meaningful, and without one a row can appear on two pages or on none.
	seen := map[string]bool{}
	for _, offset := range []int{0, 2, 4} {
		for _, row := range page(2, offset).DistributionCompletions {
			key := row.OperationalLocationDisplay + "|" + row.SessionLabel
			if seen[key] {
				t.Errorf("%s appeared on two pages", key)
			}
			seen[key] = true
		}
	}
	if len(seen) != 5 {
		t.Errorf("the pages must cover all five pen-sessions, saw %d", len(seen))
	}

	// A page the caller cannot have is REFUSED, not quietly answered with page one.
	if _, err := repo.ExecutionAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: target, DateTo: target,
		Sections:        []domain.ExecutionSection{domain.ExecutionSectionDistributionCompletions},
		CompletionDay:   target,
		CompletionLimit: domain.MaxCompletionPageSize + 1,
	}); !errors.Is(err, domain.ErrCompletionPageOutOfRange) {
		t.Errorf("an over-size page must be refused, got %v", err)
	}
}

// SCOPE: the caller's authorized park set bounds the read, and the table's own farm filter narrows
// within it. A scope leak here would show one farm's misses on another farm's screen.
func TestDistributionCompletionTableParkScope(t *testing.T) {
	ctx := context.Background()
	repo, target := completionFixture(t, ctx)

	scoped := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
		q.ParkIDs = []uuid.UUID{uuid.MustParse(fdiPark)}
	})
	if len(scoped.DistributionCompletions) != 3 {
		t.Fatalf("a CBE-scoped caller sees only CBE's three pen-sessions, got %+v",
			completionKeys(scoped.DistributionCompletions))
	}
	for _, row := range scoped.DistributionCompletions {
		if row.ParkID != fdiPark {
			t.Errorf("scope leak: %+v", row)
		}
	}
	// The totals and the filter vocabulary are scoped too -- a count that ignored the grant would
	// tell a park-scoped reader about work they cannot see.
	if scoped.CompletionTotals.Completed != 0 {
		t.Errorf("CPT's approved pen must not be counted for a CBE-scoped caller: %+v", scoped.CompletionTotals)
	}
	for _, opt := range scoped.CompletionFilterOptions {
		if opt.ParkID != fdiPark {
			t.Errorf("filter options leak another park: %+v", opt)
		}
	}

	// The table's OWN farm filter narrows a tenant-wide caller the same way.
	filtered := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
		q.CompletionParkID = fdcParkB
	})
	if len(filtered.DistributionCompletions) != 2 {
		t.Fatalf("the farm filter must return CPT's two pen-sessions, got %+v",
			completionKeys(filtered.DistributionCompletions))
	}
	// ...and the shed filter within it, keyed by shed id because shed NAMES repeat across farms.
	shedFiltered := completionRead(t, ctx, repo, target, func(q *domain.DirectedAnalyticsQuery) {
		q.CompletionShedID = fdiShedA
	})
	if len(shedFiltered.DistributionCompletions) != 3 {
		t.Fatalf("the shed filter must return Castro's three pen-sessions, got %+v",
			completionKeys(shedFiltered.DistributionCompletions))
	}
	// The filter VOCABULARY stays the whole day's, or a select could never be widened back.
	if len(shedFiltered.CompletionFilterOptions) != 2 {
		t.Errorf("options must ignore the place filters, got %+v", shedFiltered.CompletionFilterOptions)
	}
}
