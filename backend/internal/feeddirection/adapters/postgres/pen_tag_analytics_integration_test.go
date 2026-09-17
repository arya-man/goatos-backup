package postgres

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Status-wise grain proof, seeded through PersistIssue (the production write path).
//
// The adversarial shape: one Pregnant pen fed in TWO sessions with TWO feed items plus a BLOCKED
// third item, so a per-cell head count would read 10 animals as 40; the same pens on TWO days, so
// animals must add per day while the pen count stays one; a K2 pen; and a MIXED-tag pen that must be
// its own flagged row rather than folded into either stage.
func penTagCells() []domain.StoredCell {
	cell := func(shed, partition, tag string, session int32, heads int64, item string, qty *string, rowSeq, itemSeq int32) domain.StoredCell {
		c := domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: "Castro",
			PartitionLabel: partition, ShedTag: tag, Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: heads, Workflow: domain.WorkflowNormal,
			FeedItemLabel: item, FeedItemKey: item, QuantityKg: qty,
			SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: itemSeq,
		}
		if qty == nil {
			code := domain.BlockReasonNoRationRate
			detail := "no rate"
			c.BlockedReasonCode, c.BlockedReasonDetail = &code, &detail
		}
		return c
	}
	return []domain.StoredCell{
		// Pregnant pen, 10 animals: 1.0 kg concentrate + 0.5 kg hay in each of two sessions = 3.0 kg/day.
		cell(fdiShedA, "1", "Pregnant", 1, 10, "concentrate", kg("1.000"), 0, 0),
		cell(fdiShedA, "1", "Pregnant", 1, 10, "hay", kg("0.500"), 0, 1),
		cell(fdiShedA, "1", "Pregnant", 2, 10, "concentrate", kg("1.000"), 1, 0),
		cell(fdiShedA, "1", "Pregnant", 2, 10, "hay", kg("0.500"), 1, 1),
		cell(fdiShedA, "1", "Pregnant", 1, 10, "mineral", nil, 0, 2),
		// K2 pen, 4 animals, 0.8 kg/day.
		cell(fdiShedB, "", "K2", 1, 4, "concentrate", kg("0.800"), 2, 0),
		// Mixed pen, 6 animals, 1.2 kg/day.
		cell(fdiShedA, "2", "F2-Male + K3", 1, 6, "concentrate", kg("1.200"), 3, 0),
		// Fattening pen whose every cell is BLOCKED: nothing was directed, so it is not a category
		// fed on the sheet and must not appear with invented animals or a zero average.
		cell(fdiShedB, "3", "Fattening", 1, 9, "concentrate", nil, 4, 0),
	}
}

func seedPenTagIssues(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()
	for i, day := range []string{"2026-07-30", "2026-07-31"} {
		cmd := ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt:    time.Date(2026, 7, 29+i, 9, 0, 0, 0, biztime.DefaultLocation()),
			Fingerprint: "fp-pentag-" + day, IdempotencyKey: "issue:pentag:" + day, GeneratedBy: "test",
			Cells: penTagCells(),
		}
		if _, err := repo.PersistIssue(ctx, cmd); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
	}
}

func penTagWindow(fromDay, toDay int) domain.DirectedAnalyticsQuery {
	return domain.DirectedAnalyticsQuery{
		DateFrom:         time.Date(2026, 7, fromDay, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:           time.Date(2026, 7, toDay, 0, 0, 0, 0, biztime.DefaultLocation()),
		DirectedSections: []domain.DirectedSection{domain.DirectedSectionPenTags},
	}
}

// OneToMany: sessions × feed items per pen, and pens × days, must not multiply animals.
func TestDirectedPenTagsOneToManySessionsItemsAndDays(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	seedPenTagIssues(t, ctx, repo)
	// Concentrate has a reached load at ₹40/kg before the window; hay has no load, so it adds kg but
	// no rupees. Pregnant: 2.0 kg/day concentrate × ₹40 = ₹80/day; K2: 0.8 × 40 = ₹32; mixed: ₹48.
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status,
                            delivery_status, reached_on)
VALUES ($1, $2, 'CBE', 'concentrate', 901, DATE '2026-07-01', 1000, 40, 40000,
        0, DATE '2026-07-01', 'Test Vendor', 'Pending', 'reached', DATE '2026-07-01')`,
		fdiTenant, fdiPark); err != nil {
		t.Fatalf("insert load: %v", err)
	}
	window := penTagWindow(30, 31)
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("pen tags: %v", err)
	}
	if len(got.Days) != 0 || len(got.Items) != 0 {
		t.Errorf("pen_tags alone must not compute days/items, got %d days %d items", len(got.Days), len(got.Items))
	}
	want := []domain.DirectedPenTag{
		{PenTagLabel: "Pregnant", DirectedKg: "6.000", HeadDays: 20, FeedDays: 2, Pens: 1, AvgAnimals: "10", PerHeadGrams: "300.0", PerHeadKg: "0.30", RupeesPerDay: "80"},
		{PenTagLabel: "F2-Male + K3", Mixed: true, DirectedKg: "2.400", HeadDays: 12, FeedDays: 2, Pens: 1, AvgAnimals: "6", PerHeadGrams: "200.0", PerHeadKg: "0.20", RupeesPerDay: "48"},
		{PenTagLabel: "K2", DirectedKg: "1.600", HeadDays: 8, FeedDays: 2, Pens: 1, AvgAnimals: "4", PerHeadGrams: "200.0", PerHeadKg: "0.20", RupeesPerDay: "32"},
	}
	if len(got.PenTags) != len(want) {
		t.Fatalf("pen tags: want %d rows, got %+v", len(want), got.PenTags)
	}
	for i := range want {
		g := got.PenTags[i]
		// The key is feed_config_norm's output and is owned by that SQL function, not this read.
		if g.PenTagKey == "" {
			t.Errorf("row %d: empty pen tag key", i)
		}
		g.PenTagKey = ""
		days := g.Days
		g.Days = nil
		if !reflect.DeepEqual(g, want[i]) {
			t.Errorf("row %d: want %+v, got %+v", i, want[i], g)
		}
		// The card's line: one point per day, each day carrying that day's kg per animal and spend.
		wantDays := []domain.DirectedPenTagDay{
			{FeedDay: "2026-07-30", PerHeadKg: want[i].PerHeadKg, Rupees: want[i].RupeesPerDay},
			{FeedDay: "2026-07-31", PerHeadKg: want[i].PerHeadKg, Rupees: want[i].RupeesPerDay},
		}
		if !reflect.DeepEqual(days, wantDays) {
			t.Errorf("row %d days: want %+v, got %+v", i, wantDays, days)
		}
	}

	daysOnly := window
	daysOnly.DirectedSections = []domain.DirectedSection{domain.DirectedSectionDays, domain.DirectedSectionItems}
	general, err := repo.DirectedAnalytics(ctx, fdiTenant, daysOnly)
	if err != nil {
		t.Fatalf("general: %v", err)
	}
	if len(general.PenTags) != 0 || len(general.Days) != 2 {
		t.Errorf("days+items must not compute pen tags, got %d pen tags, %d days", len(general.PenTags), len(general.Days))
	}
}

// ParkScope: a caller scoped to another park sees no pen tags; its own park sees the same rows as
// the unrestricted read.
func TestDirectedPenTagsParkScope(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	seedPenTagIssues(t, ctx, repo)
	foreign := penTagWindow(30, 31)
	foreign.ParkIDs = []uuid.UUID{uuid.New()}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, foreign)
	if err != nil {
		t.Fatalf("foreign park: %v", err)
	}
	if len(got.PenTags) != 0 {
		t.Errorf("foreign park must see no pen tags, got %+v", got.PenTags)
	}
	own := penTagWindow(30, 31)
	own.ParkIDs = []uuid.UUID{uuid.MustParse(fdiPark)}
	scoped, err := repo.DirectedAnalytics(ctx, fdiTenant, own)
	if err != nil {
		t.Fatalf("own park: %v", err)
	}
	all, err := repo.DirectedAnalytics(ctx, fdiTenant, penTagWindow(30, 31))
	if err != nil {
		t.Fatalf("unrestricted: %v", err)
	}
	if len(scoped.PenTags) != len(all.PenTags) || len(all.PenTags) == 0 {
		t.Fatalf("own park and unrestricted must agree, got %d vs %d", len(scoped.PenTags), len(all.PenTags))
	}
	for i := range all.PenTags {
		if !reflect.DeepEqual(scoped.PenTags[i], all.PenTags[i]) {
			t.Errorf("row %d: scoped %+v != unrestricted %+v", i, scoped.PenTags[i], all.PenTags[i])
		}
	}
}

// PageBoundary: the read has no page input, so the whole window must equal the union of its
// single-day windows — splitting the window can never change a category's kg, animal-days,
// or days fed.
func TestDirectedPenTagsWindowSplitPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	seedPenTagIssues(t, ctx, repo)
	whole, err := repo.DirectedAnalytics(ctx, fdiTenant, penTagWindow(30, 31))
	if err != nil {
		t.Fatalf("whole: %v", err)
	}
	type sums struct {
		kg              float64
		heads, feedDays int64
	}
	split := map[string]sums{}
	for _, day := range []int{30, 31} {
		got, err := repo.DirectedAnalytics(ctx, fdiTenant, penTagWindow(day, day))
		if err != nil {
			t.Fatalf("day %d: %v", day, err)
		}
		for _, row := range got.PenTags {
			kgValue, _ := strconv.ParseFloat(row.DirectedKg, 64)
			acc := split[row.PenTagKey]
			acc.kg += kgValue
			acc.heads += row.HeadDays
			acc.feedDays += row.FeedDays
			split[row.PenTagKey] = acc
		}
	}
	if len(split) != len(whole.PenTags) {
		t.Fatalf("split windows found %d pen tags, whole window %d", len(split), len(whole.PenTags))
	}
	for _, row := range whole.PenTags {
		kgValue, _ := strconv.ParseFloat(row.DirectedKg, 64)
		acc := split[row.PenTagKey]
		if acc.heads != row.HeadDays || acc.feedDays != row.FeedDays || acc.kg != kgValue {
			t.Errorf("%s: whole %+v != split %+v", row.PenTagLabel, row, acc)
		}
	}
}

// StatusMatrix: every pen tag directed on the sheet is exactly one row, and a tag whose every cell
// was BLOCKED (nothing directed) is absent rather than reported with animals and zero grams.
func TestDirectedPenTagsStatusMatrixBlockedTagIsAbsent(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	seedPenTagIssues(t, ctx, repo)
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, penTagWindow(30, 31))
	if err != nil {
		t.Fatalf("pen tags: %v", err)
	}
	seen := map[string]int{}
	for _, row := range got.PenTags {
		seen[row.PenTagLabel]++
	}
	for _, label := range []string{"Pregnant", "K2", "F2-Male + K3"} {
		if seen[label] != 1 {
			t.Errorf("%s: want exactly one row, got %d", label, seen[label])
		}
	}
	if seen["Fattening"] != 0 {
		t.Errorf("an all-blocked pen tag must be absent, got %d rows", seen["Fattening"])
	}
}
