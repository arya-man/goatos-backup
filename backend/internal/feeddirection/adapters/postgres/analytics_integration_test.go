package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Grain proofs for the Feed Analytics rollup, seeded through PersistIssue (the
// production write path) — never by hand-inserting rollup rows.
//
// The adversarial fixture is the one the aggregate rules demand: one pen-grain
// fed in TWO sessions (head-days must not double), a shed split into TWO
// partitions (two grains, heads add), a BLOCKED cell (contributes nothing), an
// AUTHORED ZERO (contributes 0 kg but keeps its heads), TWO dates, and an
// EXPERIMENT issue that must be invisible to the directed rollup.

func analyticsCells() []domain.StoredCell {
	cell := func(shed, partition, breed string, session int32, heads int64, item, key string, qty *string, rowSeq, itemSeq int32) domain.StoredCell {
		c := domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: "Castro",
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: breed,
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: heads, Workflow: domain.WorkflowNormal,
			FeedItemLabel: item, FeedItemKey: key, QuantityKg: qty,
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
		// Shed A partition 1: 10 heads, both sessions carry Concentrate 1.0 kg each.
		// Head-days for the grain must read 10, kg must read 2.0.
		cell(fdiShedA, "1", "Beetal", 1, 10, "Concentrate", "concentrate", kg("1.000"), 0, 0),
		cell(fdiShedA, "1", "Beetal", 2, 10, "Concentrate", "concentrate", kg("1.000"), 1, 0),
		// Shed A partition 2: a DIFFERENT grain of the same shed — 5 more heads.
		cell(fdiShedA, "2", "Beetal", 1, 5, "Concentrate", "concentrate", kg("0.500"), 2, 0),
		// Shed A partition 1 also gets Hay: BLOCKED — no kg, and its presence must
		// not disturb the concentrate figures.
		cell(fdiShedA, "1", "Beetal", 1, 10, "Hay", "hay", nil, 0, 1),
		// Shed B: authored ZERO of Milk — 0 kg is a real instruction; 8 heads count.
		cell(fdiShedB, "", "Sojat", 1, 8, "Milk", "milk", kg("0.000"), 3, 0),
	}
}

func TestDirectedAnalyticsGrainProofs(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	persist := func(feedDay, workflow, fingerprint string, cells []domain.StoredCell) {
		t.Helper()
		cmd := ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: workflow,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":" + workflow,
			GeneratedBy:    "test", Cells: cells,
		}
		if _, err := repo.PersistIssue(ctx, cmd); err != nil {
			t.Fatalf("persist %s/%s: %v", feedDay, workflow, err)
		}
	}

	persist("2026-07-30", domain.WorkflowNormal, "fp-day1", analyticsCells())
	// Day 2: same sheet again, so the series has two points.
	persist("2026-07-31", domain.WorkflowNormal, "fp-day2", analyticsCells())
	// An experiment issue the SAME day — absolute kg, informational heads. The
	// directed rollup must not see one gram of it.
	exp := []domain.StoredCell{{
		ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
		PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
		ExperimentArm: "Mesha TMR", SessionNo: 1, SessionLabel: "S",
		HeadCount: 999, HeadCountInformational: true, Workflow: domain.WorkflowExperiment,
		FeedItemLabel: "Mesha TMR", FeedItemKey: "mesha tmr", QuantityKg: kg("500.000"),
		SessionTotalKg: "500.000",
	}}
	persist("2026-07-30", domain.WorkflowExperiment, "fp-exp", exp)

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("DirectedAnalytics: %v", err)
	}

	// ---- Day totals: two days, identical figures. ----
	if len(got.Days) != 2 {
		t.Fatalf("want 2 day totals, got %d: %+v", len(got.Days), got.Days)
	}
	day := got.Days[0]
	if day.FeedDay != "2026-07-30" {
		t.Fatalf("day order: %+v", got.Days)
	}
	// kg = 1.0 + 1.0 + 0.5 + 0 (authored zero) and NOTHING from the blocked cell
	// or the 500 kg experiment sheet.
	if day.DirectedKg != "2.500" {
		t.Errorf("day kg: want 2.500 (blocked adds nothing, experiment invisible), got %q", day.DirectedKg)
	}
	// Heads: partition1 (10, counted ONCE across sessions and items) +
	// partition2 (5) + shed B (8) — never the experiment's 999.
	if day.HeadDays != 23 {
		t.Errorf("day head-days: want 23 (pen-grain once, partitions apart), got %d", day.HeadDays)
	}
	// 2.5 kg × 1000 / 23 heads = 108.7 g.
	if day.PerHeadGrams != "108.7" {
		t.Errorf("day per-head: want 108.7, got %q", day.PerHeadGrams)
	}

	// ---- Per-item series for day 1. ----
	items := map[string]domain.DirectedDayItem{}
	for _, it := range got.Items {
		if it.FeedDay == "2026-07-30" {
			items[it.FeedItemKey] = it
		}
	}
	conc, ok := items["concentrate"]
	if !ok {
		t.Fatalf("concentrate series missing: %+v", got.Items)
	}
	if conc.DirectedKg != "2.500" || conc.HeadDays != 15 {
		t.Errorf("concentrate: want 2.500 kg over 15 heads (two sessions of pen 1 are ONE grain; pen 2 adds 5), got %q kg over %d", conc.DirectedKg, conc.HeadDays)
	}
	// 2.5 kg × 1000 / 15 = 166.7 g per head — divided by the heads fed THIS item.
	if conc.PerHeadGrams != "166.7" {
		t.Errorf("concentrate per-head: want 166.7, got %q", conc.PerHeadGrams)
	}
	milk, ok := items["milk"]
	if !ok {
		t.Fatalf("authored-zero milk series missing — a 0 kg instruction is real: %+v", got.Items)
	}
	if milk.DirectedKg != "0.000" || milk.HeadDays != 8 || milk.PerHeadGrams != "0.0" {
		t.Errorf("milk: want 0.000 kg, 8 heads, 0.0 g, got %+v", milk)
	}
	hay, ok := items["hay"]
	if !ok {
		t.Fatalf("blocked hay grain missing from the item series: %+v", got.Items)
	}
	if hay.DirectedKg != "0" || hay.PerHeadGrams != "" {
		t.Errorf("hay (every cell blocked): want kg \"0\" and per-head \"\", got %+v", hay)
	}
	if _, leaked := items["mesha tmr"]; leaked {
		t.Errorf("experiment feed item leaked into the directed rollup")
	}

	// ---- Park filter: a park the fixture never fed returns empty, not zeros. ----
	other := uuid.New()
	filtered, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		ParkIDs: []uuid.UUID{other}, DateFrom: window.DateFrom, DateTo: window.DateTo,
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(filtered.Days) != 0 || len(filtered.Items) != 0 {
		t.Errorf("foreign park filter: want empty, got %+v", filtered)
	}
}
