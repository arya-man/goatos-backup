package postgres

import (
	"context"
	"testing"
	"time"

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
	}
}

func TestDirectedPenTagsAverageFeedPerAnimalPerPenTag(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
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
	window := domain.DirectedAnalyticsQuery{
		DateFrom:         time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:           time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
		DirectedSections: []domain.DirectedSection{domain.DirectedSectionPenTags},
	}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("pen tags: %v", err)
	}
	if len(got.Days) != 0 || len(got.Items) != 0 {
		t.Errorf("pen_tags alone must not compute days/items, got %d days %d items", len(got.Days), len(got.Items))
	}
	want := []domain.DirectedPenTag{
		{PenTagLabel: "Pregnant", DirectedKg: "6.000", HeadDays: 20, FeedDays: 2, Pens: 1, AvgAnimals: "10", PerHeadGrams: "300.0"},
		{PenTagLabel: "F2-Male + K3", Mixed: true, DirectedKg: "2.400", HeadDays: 12, FeedDays: 2, Pens: 1, AvgAnimals: "6", PerHeadGrams: "200.0"},
		{PenTagLabel: "K2", DirectedKg: "1.600", HeadDays: 8, FeedDays: 2, Pens: 1, AvgAnimals: "4", PerHeadGrams: "200.0"},
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
		if g != want[i] {
			t.Errorf("row %d: want %+v, got %+v", i, want[i], g)
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
