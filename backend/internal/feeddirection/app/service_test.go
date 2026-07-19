package app

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	testTenant = "00000000-0000-4000-8000-000000000001"
	testPark   = "00000000-0000-4000-8000-000000003001"
	shedA      = "00000000-0000-4000-8000-000000004001"
	shedB      = "00000000-0000-4000-8000-000000004002"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

type fakeConfigRepo struct {
	snapshot domain.ConfigSnapshot
	sheds    []ports.Shed

	// Call counters. These are the assertion for the read shape: the orchestration must issue a
	// CONSTANT number of reads regardless of how many sheds or grains are on the page. A counter
	// that scales with the page is the N+1 fan-out the scale rules ban -- and it is invisible to a
	// raw-driver check, because the query sits an adapter layer below the loop.
	snapshotCalls int
	shedCalls     int

	lastShedQuery ports.ShedScopeQuery
	lastAsOf      time.Time
	err           error
}

func (f *fakeConfigRepo) LoadConfigSnapshot(_ context.Context, _, _ string, asOf time.Time) (domain.ConfigSnapshot, error) {
	f.snapshotCalls++
	f.lastAsOf = asOf
	if f.err != nil {
		return domain.ConfigSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func (f *fakeConfigRepo) ListShedScope(_ context.Context, q ports.ShedScopeQuery) (ports.ShedScope, error) {
	f.shedCalls++
	f.lastShedQuery = q
	if f.err != nil {
		return ports.ShedScope{}, f.err
	}
	items := f.sheds
	if q.ShedID != "" {
		filtered := []ports.Shed{}
		for _, shed := range items {
			if shed.ShedID == q.ShedID {
				filtered = append(filtered, shed)
			}
		}
		items = filtered
	}
	return ports.ShedScope{Items: items}, nil
}

type fakeCountsReader struct {
	grains map[string][]domain.ShedGrain

	calls        int
	lastRequest  ports.ProjectedGrainsRequest
	requestedIDs [][]string
	err          error
}

func (f *fakeCountsReader) ProjectedGrainsForSheds(_ context.Context, req ports.ProjectedGrainsRequest) (map[string][]domain.ShedGrain, error) {
	f.calls++
	f.lastRequest = req
	f.requestedIDs = append(f.requestedIDs, append([]string{}, req.ShedIDs...))
	if f.err != nil {
		return nil, f.err
	}
	out := map[string][]domain.ShedGrain{}
	for _, id := range req.ShedIDs {
		if grains, ok := f.grains[id]; ok {
			out[id] = grains
		}
	}
	return out, nil
}

func testSnapshot() domain.ConfigSnapshot {
	return domain.ConfigSnapshot{
		ParkID:    testPark,
		ParkLabel: "CPT",
		ShedTagsByKey: map[string]domain.ShedTag{
			"non_pregnant": {Label: "Non-Pregnant", AppliesTo: domain.AppliesToAdult},
			"f2_male":      {Label: "F2-Male", AppliesTo: domain.AppliesToKid},
		},
		RationGroupByBreedKey: map[string]string{"beetal": "Beetal/Sirohi", "sirohi": "Beetal/Sirohi"},
		FeedItems:             []domain.FeedItem{{Label: "Concentrate", Key: "concentrate"}},
		RatesByKey: map[string]domain.RationRate{
			domain.RateKey("Beetal/Sirohi", "Non-Pregnant", "Concentrate"): {GramsPerHead: "200.000"},
			domain.RateKey("Kid", "F2-Male", "Concentrate"):                {GramsPerHead: "100.000"},
		},
		ShedFactorsByKey: map[string]string{},
		// Each session declares the slot it consists of. Without a declared slot the generator has
		// no authored list of what to pack and blocks the session -- it does not fall back to the
		// catalog, which is the whole of migration 000005's first half.
		Sessions: []domain.SessionTemplate{
			{
				SessionNo: 1, Label: "Morning", SplitFraction: "0.5000",
				Items: []domain.FeedItem{{Label: "Concentrate", Key: "concentrate"}},
			},
			{
				SessionNo: 2, Label: "Evening", SplitFraction: "0.5000",
				Items: []domain.FeedItem{{Label: "Concentrate", Key: "concentrate"}},
			},
		},
		ExperimentByShedID: map[string][]domain.ExperimentCell{},
	}
}

func newTestService() (*Service, *fakeConfigRepo, *fakeCountsReader) {
	config := &fakeConfigRepo{
		snapshot: testSnapshot(),
		sheds: []ports.Shed{
			{ShedID: shedA, Label: "Shed A"},
			{ShedID: shedB, Label: "Shed B"},
		},
	}
	counts := &fakeCountsReader{grains: map[string][]domain.ShedGrain{
		shedA: {
			{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10},
			{ManagementStage: "F2-Male", Breed: "Beetal", HeadCount: 6},
		},
		shedB: {
			{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 4},
		},
	}}
	return NewService(config, counts), config, counts
}

func targetDate() time.Time {
	return time.Date(2026, 7, 19, 0, 0, 0, 0, biztime.DefaultLocation())
}

// ---------------------------------------------------------------------------
// Read shape
// ---------------------------------------------------------------------------

// The orchestration must issue a CONSTANT number of reads: one shed page, one config snapshot, one
// batched grain read -- no matter how many sheds are on the page. Anything that scales with the
// page is the banned N+1 fan-out.
func TestPreviewIssuesAConstantNumberOfReadsRegardlessOfPageSize(t *testing.T) {
	t.Parallel()
	service, config, counts := newTestService()

	page, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID:   testTenant,
		ParkID:     testPark,
		TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if config.shedCalls != 1 {
		t.Fatalf("shed page reads = %d, want exactly 1", config.shedCalls)
	}
	if config.snapshotCalls != 1 {
		t.Fatalf("config snapshot reads = %d, want exactly 1 (once per request, never per shed)", config.snapshotCalls)
	}
	if counts.calls != 1 {
		t.Fatalf("projected-count reads = %d, want exactly 1 batched call for the whole page", counts.calls)
	}
	// The single call must name EVERY shed on the page -- that is what makes it a batch rather than
	// the first of N.
	if len(counts.requestedIDs) != 1 || len(counts.requestedIDs[0]) != 2 {
		t.Fatalf("batched shed ids = %v, want one call naming both sheds", counts.requestedIDs)
	}
	// Two sheds x two sessions; Shed A has two grains, Shed B one -> (2+1) x 2 = 6 rows.
	if len(page.Items) != 6 {
		t.Fatalf("rows = %d, want 6", len(page.Items))
	}
}

// A shed's grains must never straddle a page boundary. Paging by shed is what guarantees it: page 1
// carries ALL of shed A's rows, page 2 all of shed B's.
func TestPagingBySheDKeepsEachShedsGrainsWhole(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	ctx := context.Background()

	first, err := service.Preview(ctx, domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 1,
	})
	if err != nil {
		t.Fatalf("Preview page 1: %v", err)
	}
	if !first.HasMore {
		t.Fatal("has_more = false on a truncated page")
	}
	for _, row := range first.Items {
		if row.ShedID != shedA {
			t.Fatalf("page 1 contains shed %q; a page must not mix sheds when limit=1", row.ShedID)
		}
	}
	// Shed A's two grains x two sessions, all present on one page.
	if len(first.Items) != 4 {
		t.Fatalf("page 1 rows = %d, want all 4 of shed A's rows", len(first.Items))
	}
	// The session totals are therefore complete, not halved by a boundary.
	if first.Items[0].SessionTotalKg == "0.000" {
		t.Fatal("page 1 first row total is zero; a split shed would look like this")
	}

	second, err := service.Preview(ctx, domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 1, Offset: 1,
	})
	if err != nil {
		t.Fatalf("Preview page 2: %v", err)
	}
	if second.HasMore {
		t.Fatal("has_more = true on the last page")
	}
	for _, row := range second.Items {
		if row.ShedID != shedB {
			t.Fatalf("page 2 contains shed %q, want only shed B", row.ShedID)
		}
	}
}

// THE REGRESSION TEST FOR THE PAGE-SCOPED-SUMMARY DEFECT.
//
// The summary used to be computed over the returned page, so the same park and day reported
// 129.800 kg of Concentrate over 4 sheds at limit=5 and 313.200 kg over 38 sheds unpaged. An
// operator reads the small number as the park total and packs a fraction of what the sheds need.
//
// This asserts the property that makes that impossible: every figure in the summary is IDENTICAL
// across three different page sizes, and equal to the whole-set values. It fails on the old
// behaviour -- at limit=1 the old summary reported one shed and one shed's worth of feed.
func TestPreviewSummaryIsInvariantToPageSize(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	summaries := make([]domain.PreviewSummary, 0, 3)
	for _, limit := range []int32{1, 2, 50} {
		page, err := service.Preview(context.Background(), domain.PreviewQuery{
			TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: limit,
		})
		if err != nil {
			t.Fatalf("Preview(limit=%d): %v", limit, err)
		}
		if page.Summary.Scope != domain.SummaryScopeFiltered {
			t.Fatalf("limit=%d scope = %q, want %q", limit, page.Summary.Scope, domain.SummaryScopeFiltered)
		}
		summaries = append(summaries, page.Summary)
	}

	want := summaries[len(summaries)-1]
	// The unpaged run is the whole set, so it is the reference every paged run must match. Both
	// sheds, both sessions, and the full store draw.
	if want.ShedCount != 2 {
		t.Fatalf("whole-set shed_count = %d, want 2", want.ShedCount)
	}
	if want.RowCount != 6 {
		t.Fatalf("whole-set row_count = %d, want 6 (3 grains x 2 sessions)", want.RowCount)
	}

	for i, got := range summaries {
		if got.ShedCount != want.ShedCount {
			t.Errorf("summary[%d].shed_count = %d, want %d -- the summary moved with the page size",
				i, got.ShedCount, want.ShedCount)
		}
		if got.RowCount != want.RowCount {
			t.Errorf("summary[%d].row_count = %d, want %d -- the summary moved with the page size",
				i, got.RowCount, want.RowCount)
		}
		if got.BlockedCount != want.BlockedCount {
			t.Errorf("summary[%d].blocked_count = %d, want %d -- blocked cells must be full-scope too",
				i, got.BlockedCount, want.BlockedCount)
		}
		if got.BlockedShedCount != want.BlockedShedCount {
			t.Errorf("summary[%d].blocked_shed_count = %d, want %d", i, got.BlockedShedCount, want.BlockedShedCount)
		}
		if len(got.TotalKgByFeedItem) != len(want.TotalKgByFeedItem) {
			t.Fatalf("summary[%d] has %d feed-item totals, want %d",
				i, len(got.TotalKgByFeedItem), len(want.TotalKgByFeedItem))
		}
		for j, total := range got.TotalKgByFeedItem {
			if total.FeedItem != want.TotalKgByFeedItem[j].FeedItem {
				t.Errorf("summary[%d] item %d = %q, want %q", i, j, total.FeedItem, want.TotalKgByFeedItem[j].FeedItem)
			}
			if total.QuantityKg != want.TotalKgByFeedItem[j].QuantityKg {
				t.Errorf("summary[%d] %s total = %s kg, want %s kg -- THIS IS THE DEFECT: the park total shrank with the page size",
					i, total.FeedItem, total.QuantityKg, want.TotalKgByFeedItem[j].QuantityKg)
			}
			if total.BlockedCells != want.TotalKgByFeedItem[j].BlockedCells {
				t.Errorf("summary[%d] %s blocked_cells = %d, want %d",
					i, total.FeedItem, total.BlockedCells, want.TotalKgByFeedItem[j].BlockedCells)
			}
		}
	}
}

// The packing worklist drives what an operator draws from the store before walking the park, so a
// page-scoped store draw sends them out with a fraction of the load. Same invariance contract.
func TestPackingSummaryIsInvariantToPageSize(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	summaries := make([]domain.PackingSummary, 0, 3)
	for _, limit := range []int32{1, 2, 50} {
		page, err := service.PackingWorklist(context.Background(), domain.PackingQuery{
			TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: limit,
		})
		if err != nil {
			t.Fatalf("PackingWorklist(limit=%d): %v", limit, err)
		}
		if page.Summary.Scope != domain.SummaryScopeFiltered {
			t.Fatalf("limit=%d scope = %q, want %q", limit, page.Summary.Scope, domain.SummaryScopeFiltered)
		}
		summaries = append(summaries, page.Summary)
	}

	want := summaries[len(summaries)-1]
	if want.ShedCount != 2 {
		t.Fatalf("whole-set shed_count = %d, want 2", want.ShedCount)
	}
	if want.LineCount != 4 {
		t.Fatalf("whole-set line_count = %d, want 4 (2 sheds x 2 sessions)", want.LineCount)
	}

	for i, got := range summaries {
		if got.ShedCount != want.ShedCount || got.LineCount != want.LineCount {
			t.Errorf("summary[%d] = %d sheds / %d lines, want %d / %d -- the worklist summary moved with the page size",
				i, got.ShedCount, got.LineCount, want.ShedCount, want.LineCount)
		}
		if got.BlockedCount != want.BlockedCount || got.BlockedLineCount != want.BlockedLineCount {
			t.Errorf("summary[%d] blocked = %d cells / %d lines, want %d / %d",
				i, got.BlockedCount, got.BlockedLineCount, want.BlockedCount, want.BlockedLineCount)
		}
		for j, total := range got.TotalKgByFeedItem {
			if total.QuantityKg != want.TotalKgByFeedItem[j].QuantityKg {
				t.Errorf("summary[%d] %s store draw = %s kg, want %s kg -- the draw shrank with the page size",
					i, total.FeedItem, total.QuantityKg, want.TotalKgByFeedItem[j].QuantityKg)
			}
		}
	}
}

// The summary must equal the rows an operator can page through to check it. If the two ever
// disagree the number is unverifiable, which is how the original defect survived review.
func TestSummaryTotalsEqualTheSumOfEveryPagedRow(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	unpaged, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	// Walk the whole result one shed at a time and re-add it by hand.
	byItem := map[string]int64{}
	rows := 0
	for offset := int32(0); ; offset++ {
		page, err := service.Preview(context.Background(), domain.PreviewQuery{
			TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 1, Offset: offset,
		})
		if err != nil {
			t.Fatalf("Preview(offset=%d): %v", offset, err)
		}
		for _, row := range page.Items {
			rows++
			for _, item := range row.Items {
				if item.QuantityKg == nil {
					continue
				}
				grams, ok := new(big.Rat).SetString(*item.QuantityKg)
				if !ok {
					t.Fatalf("unparseable quantity %q", *item.QuantityKg)
				}
				grams.Mul(grams, new(big.Rat).SetInt64(1000))
				byItem[item.FeedItem] += grams.Num().Int64() / grams.Denom().Int64()
			}
		}
		if !page.HasMore {
			break
		}
	}

	if int32(rows) != unpaged.Summary.RowCount {
		t.Fatalf("walked %d rows but summary reports row_count = %d", rows, unpaged.Summary.RowCount)
	}
	for _, total := range unpaged.Summary.TotalKgByFeedItem {
		want := domain.GramsToKgString(byItem[total.FeedItem])
		if total.QuantityKg != want {
			t.Errorf("summary %s = %s kg but the paged rows add to %s kg", total.FeedItem, total.QuantityKg, want)
		}
	}
}

func TestShedFilterNarrowsToOneShed(t *testing.T) {
	t.Parallel()
	service, config, _ := newTestService()

	page, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), ShedID: shedB,
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if config.lastShedQuery.ShedID != shedB {
		t.Fatalf("shed filter was not pushed into the shed scope read: %+v", config.lastShedQuery)
	}
	for _, row := range page.Items {
		if row.ShedID != shedB {
			t.Fatalf("row for shed %q leaked past the filter", row.ShedID)
		}
	}
}

// ---------------------------------------------------------------------------
// Business calendar
// ---------------------------------------------------------------------------

// Per AGENTS.md, UTC never defines a Goat OS business day. An instant late in the UTC day is
// already the NEXT day in India, and generating that sheet for the wrong day means a shed is fed
// the wrong ration.
func TestTargetDateIsResolvedInAsiaKolkataNotUTC(t *testing.T) {
	t.Parallel()
	service, config, counts := newTestService()

	// 2026-07-19T20:00:00Z is 2026-07-20 01:30 IST.
	instant := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)
	page, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: instant,
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.TargetDate != "2026-07-20" {
		t.Fatalf("target_date = %q, want \"2026-07-20\" (the IST business day, not the UTC one)", page.TargetDate)
	}
	// The same business day must reach BOTH downstream reads, or the config snapshot and the counts
	// projection would describe different days.
	if got := biztime.BusinessDate(config.lastAsOf); got != "2026-07-20" {
		t.Fatalf("config snapshot as-of = %q, want \"2026-07-20\"", got)
	}
	if got := biztime.BusinessDate(counts.lastRequest.TargetDate); got != "2026-07-20" {
		t.Fatalf("counts target date = %q, want \"2026-07-20\"", got)
	}
	if config.lastAsOf.Location().String() != biztime.DefaultTimezone {
		t.Fatalf("as-of location = %s, want %s", config.lastAsOf.Location(), biztime.DefaultTimezone)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestQueryValidationFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		query domain.PreviewQuery
		want  error
	}{
		{
			name:  "missing tenant",
			query: domain.PreviewQuery{ParkID: testPark, TargetDate: targetDate()},
			want:  ports.ErrParkRequired,
		},
		{
			// The ration grid, the session split and the dispatch clock are ALL park-scoped, so a
			// tenant-wide generation would mix two parks' rations into one document.
			name:  "missing park",
			query: domain.PreviewQuery{TenantID: testTenant, TargetDate: targetDate()},
			want:  ports.ErrParkRequired,
		},
		{
			name:  "missing target date",
			query: domain.PreviewQuery{TenantID: testTenant, ParkID: testPark},
			want:  ports.ErrInvalidTargetDate,
		},
		{
			// A present-but-invalid paging value is REJECTED, never clamped to a default the caller
			// never asked for.
			name:  "limit above the bound",
			query: domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: MaxShedPageLimit + 1},
			want:  ports.ErrInvalidPaging,
		},
		{
			name:  "negative limit",
			query: domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: -1},
			want:  ports.ErrInvalidPaging,
		},
		{
			name:  "offset above the bound",
			query: domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Offset: MaxShedPageOffset + 1},
			want:  ports.ErrInvalidPaging,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, config, counts := newTestService()
			if _, err := service.Preview(context.Background(), tc.query); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// A rejected query must not have touched the database at all.
			if config.shedCalls != 0 || config.snapshotCalls != 0 || counts.calls != 0 {
				t.Fatal("a rejected query issued reads")
			}
		})
	}
}

func TestAbsentLimitTakesTheDeclaredDefault(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	page, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	// The limit is now applied when the page is sliced out of the shed scope rather than pushed into
	// the shed query, so the echoed page limit is what proves the default was taken.
	if page.Limit != DefaultShedPageLimit {
		t.Fatalf("limit = %d, want the declared default %d", page.Limit, DefaultShedPageLimit)
	}
}

func TestRepositoryErrorsPropagateRatherThanReturningAnEmptyPage(t *testing.T) {
	t.Parallel()
	// An empty page for a failed read looks exactly like "nothing to feed today", which is the one
	// answer a feed surface must never give by accident.
	sentinel := errors.New("boom")
	config := &fakeConfigRepo{err: sentinel}
	service := NewService(config, &fakeCountsReader{})
	if _, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	}); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the repository error to propagate", err)
	}
}

func TestParkNotFoundPropagates(t *testing.T) {
	t.Parallel()
	config := &fakeConfigRepo{err: ports.ErrParkNotFound}
	service := NewService(config, &fakeCountsReader{})
	if _, err := service.Preview(context.Background(), domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	}); !errors.Is(err, ports.ErrParkNotFound) {
		t.Fatalf("err = %v, want ErrParkNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Packing worklist
// ---------------------------------------------------------------------------

// The worklist must be built from the SAME generated rows as the preview -- not a second,
// independently rounded computation -- so the bag a packer fills matches the sheet that was
// printed.
func TestPackingWorklistAgreesWithThePreviewExactly(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	ctx := context.Background()

	preview, err := service.Preview(ctx, domain.PreviewQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	packing, err := service.PackingWorklist(ctx, domain.PackingQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}

	// Sum the preview's per-grain quantities per (shed, session) and compare with the packing line.
	type key struct {
		shed    string
		session int32
	}
	expected := map[key]int64{}
	for _, row := range preview.Items {
		for _, item := range row.Items {
			if item.QuantityKg == nil {
				continue
			}
			expected[key{row.ShedID, row.SessionNo}] += kgToGrams(t, *item.QuantityKg)
		}
	}

	if len(packing.Items) != len(expected) {
		t.Fatalf("packing lines = %d, want %d (one per shed per session)", len(packing.Items), len(expected))
	}
	for _, line := range packing.Items {
		want := expected[key{line.ShedID, line.SessionNo}]
		grams := kgToGrams(t, line.TotalKg)
		if grams != want {
			t.Fatalf("shed %s session %d packing total = %d g, preview sum = %d g",
				line.ShedID, line.SessionNo, grams, want)
		}
		if line.Status != domain.PackingStatusReady {
			t.Fatalf("status = %q, want %q", line.Status, domain.PackingStatusReady)
		}
	}
}

func TestPackingWorklistPagesByShedToo(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	page, err := service.PackingWorklist(context.Background(), domain.PackingQuery{
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 1,
	})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	if !page.HasMore {
		t.Fatal("has_more = false on a truncated page")
	}
	for _, line := range page.Items {
		if line.ShedID != shedA {
			t.Fatalf("page 1 contains shed %q, want only shed A", line.ShedID)
		}
	}
}

// kgToGrams parses a fixed-scale kg string back to whole grams for test comparisons, using exact
// arithmetic rather than a float round trip -- comparing feed quantities through float64 is exactly
// how a "matching" total ends up off by a gram.
func kgToGrams(t *testing.T, kg string) int64 {
	t.Helper()
	rat, ok := domain.ParseDecimal(kg)
	if !ok {
		t.Fatalf("could not parse quantity %q", kg)
	}
	rat.Mul(rat, new(big.Rat).SetInt64(1000))
	if !rat.IsInt() {
		t.Fatalf("quantity %q is not a whole number of grams", kg)
	}
	return rat.Num().Int64()
}
