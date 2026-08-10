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
	snapshot  domain.ConfigSnapshot
	sheds     []ports.Shed
	parks     []ports.Park
	parkCalls int

	// Call counters. These are the assertion for the read shape: the orchestration must issue a
	// CONSTANT number of reads regardless of how many sheds or grains are on the page. A counter
	// that scales with the page is the N+1 fan-out the scale rules ban -- and it is invisible to a
	// raw-driver check, because the query sits an adapter layer below the loop.
	snapshotCalls        int
	shedCalls            int
	sessionTemplateCalls int

	lastShedQuery ports.ShedScopeQuery
	shedQueries   []ports.ShedScopeQuery
	lastAsOf      time.Time
	err           error
}

func (f *fakeConfigRepo) ListParks(_ context.Context, _ string) ([]ports.Park, error) {
	f.parkCalls++
	if f.err != nil {
		return nil, f.err
	}
	if f.parks != nil {
		return f.parks, nil
	}
	// Default single-park tenant so an explicit-park request and default-park resolution both work.
	return []ports.Park{{ParkID: testPark, Label: "CPT"}}, nil
}

func (f *fakeConfigRepo) LoadConfigSnapshot(_ context.Context, _, _ string, asOf time.Time) (domain.ConfigSnapshot, error) {
	f.snapshotCalls++
	f.lastAsOf = asOf
	if f.err != nil {
		return domain.ConfigSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func (f *fakeConfigRepo) ListSessionTemplates(_ context.Context, _, _ string, _ time.Time) ([]domain.SessionTemplate, error) {
	f.sessionTemplateCalls++
	if f.err != nil {
		return nil, f.err
	}
	// Mirror the snapshot's own session split so the filter vocabulary matches what the sheet can
	// contain, without counting as a snapshot read.
	return f.snapshot.Sessions, nil
}

func (f *fakeConfigRepo) ListShedScope(_ context.Context, q ports.ShedScopeQuery) (ports.ShedScope, error) {
	f.shedCalls++
	f.lastShedQuery = q
	f.shedQueries = append(f.shedQueries, q)
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
		ExperimentByLocation: map[string][]domain.ExperimentCell{},
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
	// Pinned to targetDate() rather than time.Now(): these fixtures test generation shape (paging,
	// summaries, error propagation), not the past-date regeneration guard, so "today" must track
	// the fixed target date regardless of when the suite actually runs.
	return NewService(config, counts).WithNowFunc(func() time.Time { return targetDate() }), config, counts
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

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID:   testTenant,
		ParkID:     testPark,
		TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	// Two shed-scope reads, both page-size-independent: one inside generate (the filtered scope the
	// sheet is built from) and one in buildFilters (the park's UNFILTERED shed catalog, the farm/shed
	// filter vocabulary). Neither scales with page size, which is what this test guards against.
	if config.shedCalls != 2 {
		t.Fatalf("shed reads = %d, want exactly 2 (generate scope + filter vocabulary)", config.shedCalls)
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
	// Two sheds x two sessions = 4 rows. The sheet is ONE row per operational location per session,
	// so shed A's two ration grains arrive merged rather than as two rows.
	if len(page.Items) != 4 {
		t.Fatalf("rows = %d, want 4 (2 pens x 2 sessions)", len(page.Items))
	}
}

// THE END-TO-END PROOF OF THE SHEET GRAIN (maintainer decision 2026-08-10). Shed A holds two ration
// grains -- 10 adult Beetal on the 200 g/head rate and 6 kids on the 100 g/head rate. It used to
// print as TWO rows per session, which an operator had to re-add at the pen door and which pointed
// at ONE completion between them. It is now one row whose columns name both cohorts and whose
// quantity is their sum.
func TestPreviewServesOneMergedRowPerPenPerSession(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	var shedARows []domain.DirectionRow
	for _, row := range page.Items {
		if row.ShedID == shedA {
			shedARows = append(shedARows, row)
		}
	}
	if len(shedARows) != 2 {
		t.Fatalf("shed A rows = %d, want 2 (one per session, NOT one per ration grain): %+v", len(shedARows), shedARows)
	}

	row := shedARows[0]
	if row.HeadCount != 16 {
		t.Errorf("head count = %d, want 16 (10 adults + 6 kids in the pen)", row.HeadCount)
	}
	// Both cohorts named, dominant first -- the operator still sees what is standing in the pen.
	if row.ShedTag != "Non-Pregnant + F2-Male" {
		t.Errorf("shed tag = %q, want %q", row.ShedTag, "Non-Pregnant + F2-Male")
	}
	if row.RationGroup != "Beetal/Sirohi + Kid" {
		t.Errorf("ration group = %q, want %q", row.RationGroup, "Beetal/Sirohi + Kid")
	}
	// 10 x 200 g x 0.5 = 1000 g, plus 6 x 100 g x 0.5 = 300 g -> 1.300 kg for the pen this session.
	if row.SessionTotalKg != "1.300" {
		t.Errorf("session total = %q, want 1.300 (both grains summed)", row.SessionTotalKg)
	}
	if len(row.Items) != 1 || row.Items[0].QuantityKg == nil || *row.Items[0].QuantityKg != "1.300" {
		t.Errorf("items = %+v, want a single merged Concentrate cell of 1.300", row.Items)
	}
	if row.Blocked {
		t.Error("row reports a gap; every cell in this fixture is authored")
	}
}

// A shed's grains must never straddle a page boundary. Paging by shed is what guarantees it: page 1
// carries ALL of shed A's rows, page 2 all of shed B's.
func TestPagingBySheDKeepsEachShedsGrainsWhole(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	ctx := context.Background()

	first, err := service.Preview(ctx, domain.PreviewQuery{Draft: true,
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
	// Shed A's merged row for each of the two sessions, both present on one page. A shed's sessions
	// must not straddle a boundary either: two half-sheets each read as a complete instruction.
	if len(first.Items) != 2 {
		t.Fatalf("page 1 rows = %d, want both of shed A's session rows", len(first.Items))
	}
	// The session totals are therefore complete, not halved by a boundary.
	if first.Items[0].SessionTotalKg == "0.000" {
		t.Fatal("page 1 first row total is zero; a split shed would look like this")
	}

	second, err := service.Preview(ctx, domain.PreviewQuery{Draft: true,
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
		page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
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
	// The summary describes exactly what the sheet renders: one row per pen per session.
	if want.RowCount != 4 {
		t.Fatalf("whole-set row_count = %d, want 4 (2 pens x 2 sessions)", want.RowCount)
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
		page, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
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
	// A LINE IS A PEN-DAY since 2026-08-10, so the same 2 sheds x 2 sessions is 2 lines, not 4. The
	// store draw asserted below is unchanged by that -- it still counts both sessions of both sheds.
	if want.LineCount != 2 {
		t.Fatalf("whole-set line_count = %d, want 2 (2 sheds, one pen-day bag each)", want.LineCount)
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

// The packing worklist serves ONE LINE PER PEN-DAY carrying every session (maintainer decision
// 2026-08-10), and there is no session filter to narrow it with. The test snapshot is 2 sheds x 2
// sessions (Morning=1, Evening=2), which used to be 4 lines and is now 2.
//
// The summary follows: LineCount counts pen-days, so it halves, while the store draw
// TotalKgByFeedItem must NOT -- the packer still carries out the morning bag AND the evening bag.
// That pairing is the point of the test. A fold that iterated the row instead of its sessions would
// halve the draw too and send the crew out with half the feed.
func TestPackingWorklistServesOneLinePerPenDayCarryingEverySession(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	q := domain.PackingQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 50}
	all, err := service.PackingWorklist(context.Background(), q)
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	if len(all.Items) != 2 {
		t.Fatalf("worklist = %d lines, want 2 (2 sheds x 1 pen-day each, NOT 2 sheds x 2 sessions)", len(all.Items))
	}
	if all.Summary.LineCount != 2 {
		t.Fatalf("summary line_count = %d, want 2 -- a line is a pen-day", all.Summary.LineCount)
	}
	for _, row := range all.Items {
		if len(row.Sessions) != 2 {
			t.Fatalf("pen-day %s carries %d sessions, want 2 -- merging the cards must not drop a session's bag",
				row.ShedID, len(row.Sessions))
		}
		if row.Sessions[0].SessionNo != 1 || row.Sessions[1].SessionNo != 2 {
			t.Fatalf("pen-day %s session order = %d,%d, want 1,2",
				row.ShedID, row.Sessions[0].SessionNo, row.Sessions[1].SessionNo)
		}
	}

	// The store draw counts BOTH sessions of every pen. Recomputed here from the served sessions so
	// the assertion is independent of the summary's own fold rather than restating it.
	wantByItem := map[string]int64{}
	for _, row := range all.Items {
		for _, session := range row.Sessions {
			for _, item := range session.Items {
				if item.QuantityKg == nil {
					continue
				}
				wantByItem[item.FeedItem] += kgToGrams(t, *item.QuantityKg)
			}
		}
	}
	if len(wantByItem) == 0 {
		t.Fatal("fixture served no resolved quantities; the draw assertion would be vacuous")
	}
	for _, total := range all.Summary.TotalKgByFeedItem {
		if grams := kgToGrams(t, total.QuantityKg); grams != wantByItem[total.FeedItem] {
			t.Fatalf("store draw for %s = %q, want the sum of BOTH sessions (%d g) -- a day-level fold would under-report it",
				total.FeedItem, total.QuantityKg, wantByItem[total.FeedItem])
		}
	}
}

// The feed read exposes the served park's session split as backend-owned filter vocabulary, on both
// surfaces, so the client renders its session picker from the contract and holds no session list of
// its own (the golden frontend rule).
func TestFeedFiltersExposeSessionVocabulary(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	preview, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	packing, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	want := []domain.FeedFilterSession{{SessionNo: 1, Label: "Morning"}, {SessionNo: 2, Label: "Evening"}}
	for name, got := range map[string][]domain.FeedFilterSession{
		"preview": preview.Filters.Sessions,
		"packing": packing.Filters.Sessions,
	} {
		if len(got) != len(want) {
			t.Fatalf("%s sessions = %+v, want %+v", name, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s sessions[%d] = %+v, want %+v", name, i, got[i], want[i])
			}
		}
	}
}

// The summary must equal the rows an operator can page through to check it. If the two ever
// disagree the number is unverifiable, which is how the original defect survived review.
func TestSummaryTotalsEqualTheSumOfEveryPagedRow(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	unpaged, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}

	// Walk the whole result one shed at a time and re-add it by hand.
	byItem := map[string]int64{}
	rows := 0
	for offset := int32(0); ; offset++ {
		page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
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

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), ShedID: shedB,
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	// The GENERATION shed-scope read must carry the shed filter. buildFilters issues a second,
	// deliberately UNFILTERED shed read afterwards (the farm/shed filter vocabulary must list every
	// shed, not just the selected one), so assert against the set of reads rather than the last one.
	pushedFilter := false
	for _, q := range config.shedQueries {
		if q.ShedID == shedB {
			pushedFilter = true
		}
	}
	if !pushedFilter {
		t.Fatalf("shed filter was not pushed into any shed scope read: %+v", config.shedQueries)
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
	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
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
			query: domain.PreviewQuery{Draft: true, ParkID: testPark, TargetDate: targetDate()},
			want:  ports.ErrParkRequired,
		},
		{
			name:  "missing target date",
			query: domain.PreviewQuery{Draft: true, TenantID: testTenant, ParkID: testPark},
			want:  ports.ErrInvalidTargetDate,
		},
		{
			// A present-but-invalid paging value is REJECTED, never clamped to a default the caller
			// never asked for.
			name:  "limit above the bound",
			query: domain.PreviewQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: MaxShedPageLimit + 1},
			want:  ports.ErrInvalidPaging,
		},
		{
			name:  "negative limit",
			query: domain.PreviewQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: -1},
			want:  ports.ErrInvalidPaging,
		},
		{
			name:  "offset above the bound",
			query: domain.PreviewQuery{Draft: true, TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Offset: MaxShedPageOffset + 1},
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
	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
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
	service := NewService(config, &fakeCountsReader{}).WithNowFunc(func() time.Time { return targetDate() })
	if _, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	}); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the repository error to propagate", err)
	}
}

// TestMissingParkDefaultsToTenantsFirstPark pins the mobile-first behavior: an omitted park_id is
// resolved to the tenant's first park (never a tenant-wide sheet, which would mix parks), so the
// client's first load has a sheet to render instead of a park-required error. The served park is
// echoed back in the filter options so the client shows the right park as active.
func TestMissingParkDefaultsToTenantsFirstPark(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview with no park should default to the first park, got: %v", err)
	}
	if page.Filters.ServedParkID != testPark {
		t.Fatalf("served park = %q, want the defaulted first park %q", page.Filters.ServedParkID, testPark)
	}
	if len(page.Filters.Parks) == 0 {
		t.Fatal("filter options must carry the farm vocabulary")
	}
}

// TestMissingParkWithNoParksFailsClosed keeps the closed-fail for a tenant that genuinely has no
// parks: there is no park to default to, so a sheet must not be fabricated.
func TestMissingParkWithNoParksFailsClosed(t *testing.T) {
	t.Parallel()
	config := &fakeConfigRepo{parks: []ports.Park{}}
	service := NewService(config, &fakeCountsReader{}).WithNowFunc(func() time.Time { return targetDate() })
	if _, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, TargetDate: targetDate(),
	}); !errors.Is(err, ports.ErrParkRequired) {
		t.Fatalf("err = %v, want ErrParkRequired when the tenant has no parks", err)
	}
}

func TestParkNotFoundPropagates(t *testing.T) {
	t.Parallel()
	config := &fakeConfigRepo{err: ports.ErrParkNotFound}
	service := NewService(config, &fakeCountsReader{}).WithNowFunc(func() time.Time { return targetDate() })
	if _, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
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

	preview, err := service.Preview(ctx, domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	packing, err := service.PackingWorklist(ctx, domain.PackingQuery{Draft: true,
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

	// The bag is now per PEN-DAY while the sheet stays per-session, so the comparable figure is the
	// bag's SESSION total. The pen-day total is checked separately below as the sum of them, which is
	// what stops the merge from quietly losing or double-counting a session.
	sessionsSeen := 0
	for _, line := range packing.Items {
		var dayGrams int64
		for _, session := range line.Sessions {
			sessionsSeen++
			want, ok := expected[key{line.ShedID, session.SessionNo}]
			if !ok {
				t.Fatalf("packing carries shed %s session %d, which the preview never emitted",
					line.ShedID, session.SessionNo)
			}
			grams := kgToGrams(t, session.TotalKg)
			if grams != want {
				t.Fatalf("shed %s session %d packing total = %d g, preview sum = %d g",
					line.ShedID, session.SessionNo, grams, want)
			}
			dayGrams += grams
		}
		if got := kgToGrams(t, line.TotalKg); got != dayGrams {
			t.Fatalf("shed %s pen-day total = %d g, but its sessions sum to %d g", line.ShedID, got, dayGrams)
		}
		if line.Status != domain.PackingStatusReady {
			t.Fatalf("status = %q, want %q", line.Status, domain.PackingStatusReady)
		}
	}
	// Every (shed, session) the preview emitted is accounted for on exactly one bag -- so merging the
	// cards dropped nothing.
	if sessionsSeen != len(expected) {
		t.Fatalf("packing carried %d shed-sessions across its bags, want %d (one per preview shed-session)",
			sessionsSeen, len(expected))
	}
}

func TestPackingWorklistPagesByShedToo(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	page, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
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

// TestPreviewRejectsPastBusinessDate is the P1 follow-up interim safety test: until an immutable
// per-park/date config+count snapshot exists (see ports.ErrPastDateRegenerationBlocked), the
// LIVE-COMPUTE (Draft) path of Preview and PackingWorklist must REJECT a target date before "today"
// rather than silently recomputing that historical day's direction from today's live
// herd/experiment/shed-scope state. The serve path (Draft=false) is unaffected: it reads the frozen
// issued sheet, so a past feed day is served, not regenerated.
func TestPreviewRejectsPastBusinessDate(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	// "Today" is pinned to targetDate() by newTestService; a day before it must be rejected.
	pastDate := targetDate().AddDate(0, 0, -1)

	_, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: pastDate,
	})
	if !errors.Is(err, ports.ErrPastDateRegenerationBlocked) {
		t.Fatalf("Preview(past date) err = %v, want ErrPastDateRegenerationBlocked", err)
	}
}

// TestPackingWorklistRejectsPastBusinessDate is the PackingWorklist twin of the Preview guard
// above -- both live-compute (Draft) surfaces recompute from the same live state, so both must
// reject the same way.
func TestPackingWorklistRejectsPastBusinessDate(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()
	pastDate := targetDate().AddDate(0, 0, -1)

	_, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: pastDate,
	})
	if !errors.Is(err, ports.ErrPastDateRegenerationBlocked) {
		t.Fatalf("PackingWorklist(past date) err = %v, want ErrPastDateRegenerationBlocked", err)
	}
}

// TestPreviewAllowsTodayAndFutureBusinessDate proves the guard is scoped to STRICTLY-past dates:
// live-computing (Draft) today's or a future day's direction is the normal, allowed operation.
func TestPreviewAllowsTodayAndFutureBusinessDate(t *testing.T) {
	t.Parallel()
	service, _, _ := newTestService()

	for _, tc := range []struct {
		name string
		date time.Time
	}{
		{"today", targetDate()},
		{"future", targetDate().AddDate(0, 0, 3)},
	} {
		if _, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
			TenantID: testTenant, ParkID: testPark, TargetDate: tc.date,
		}); err != nil {
			t.Fatalf("Preview(%s) unexpected error: %v", tc.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Park filter vocabulary vs. the caller's own scope
// ---------------------------------------------------------------------------

const testParkB = "00000000-0000-4000-8000-000000003002"

// twoParkService is a tenant with TWO parks, so "the whole catalog" and "the caller's own park" are
// distinguishable answers. A single-park fixture cannot fail this test.
func twoParkService() (*Service, *fakeConfigRepo) {
	service, config, _ := newTestService()
	config.parks = []ports.Park{{ParkID: testPark, Label: "CPT"}, {ParkID: testParkB, Label: "CBE"}}
	return service, config
}

// TestFeedFiltersOfferOnlyTheCallersAuthorizedParks is the fix for the defect where a park-scoped
// operator's farm dropdown listed EVERY active park in the tenant. The route already clamps a
// REQUESTED park to the caller's grant (403 park_scope_forbidden), so the extra options were dead
// choices: picking one produced an error rather than a sheet. An option a principal cannot open must
// not be offered at all -- a dropdown is a statement about what this person may do.
func TestFeedFiltersOfferOnlyTheCallersAuthorizedParks(t *testing.T) {
	t.Parallel()
	service, _ := twoParkService()

	preview, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
		AuthorizedParkIDs: []string{testPark},
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	packing, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
		AuthorizedParkIDs: []string{testPark},
	})
	if err != nil {
		t.Fatalf("PackingWorklist: %v", err)
	}
	for name, got := range map[string][]domain.FeedFilterPark{
		"preview": preview.Filters.Parks,
		"packing": packing.Filters.Parks,
	} {
		if len(got) != 1 || got[0].ParkID != testPark {
			t.Fatalf("%s parks = %+v, want only the caller's authorized park %q", name, got, testPark)
		}
	}
}

// TestFeedFiltersOfferEveryParkForATenantWideCaller is the other half: a CEO/director holding a
// tenant-wide grant reaches the service with NO authorized-park list (the resolver returns an empty
// set for tenant-wide scope), and must still see the whole catalog. Without this, the narrowing
// above would silently blank leadership's own farm picker.
func TestFeedFiltersOfferEveryParkForATenantWideCaller(t *testing.T) {
	t.Parallel()
	service, _ := twoParkService()

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if len(page.Filters.Parks) != 2 {
		t.Fatalf("tenant-wide parks = %+v, want the whole catalog (2 parks)", page.Filters.Parks)
	}
}

// TestDefaultParkIsChosenFromTheCallersAuthorizedParks closes the sibling gap in the default-park
// pick. resolveParkID defaulted an omitted park_id to the tenant's FIRST park, which for an operator
// scoped to the second park is a park they may not read -- the caller would be served, and would
// then be shown, someone else's farm.
func TestDefaultParkIsChosenFromTheCallersAuthorizedParks(t *testing.T) {
	t.Parallel()
	service, _ := twoParkService()

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, TargetDate: targetDate(),
		AuthorizedParkIDs: []string{testParkB},
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.Filters.ServedParkID != testParkB {
		t.Fatalf("served park = %q, want the caller's own park %q, never the tenant's first park", page.Filters.ServedParkID, testParkB)
	}
}
