package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Grain + display proofs for the per-pen feed-mix rollup behind the Feed
// Analytics overview table, seeded through PersistIssue (the production write
// path) — never by hand-inserting rollup rows.
//
// The fixture is the adversarial shape the operational-location rules demand:
// a bare-numeric partition (must render "Castro 1", space form), a WORDED
// partition ("Godel 1" + "Part 3" must render "Godel 1 - Part 3", dash form),
// an undivided shed (bare name, no separator, no 'whole' leak), TWO sessions of
// one pen×item (must sum, never duplicate the row), TWO days (must sum across
// the window), and a BLOCKED cell (contributes nothing, never a fabricated 0).
func TestShedFeedAnalyticsPenGrainAndDisplayRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	shedGodel := "fd100000-0000-4000-8000-000000004003"
	cell := func(shed, shedLabel, partition string, session int32, item, key string, qty *string, rowSeq, itemSeq int32) domain.StoredCell {
		c := domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: shedLabel,
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
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
	cells := []domain.StoredCell{
		// Castro partition 1, two sessions of Masur Busa: ONE row, kg summed.
		cell(fdiShedA, "Castro", "1", 1, "Masur Busa", "masur busa", kg("2.000"), 0, 0),
		cell(fdiShedA, "Castro", "1", 2, "Masur Busa", "masur busa", kg("1.000"), 1, 0),
		// Same pen, second item — smaller kg, so it must sort AFTER Masur Busa.
		cell(fdiShedA, "Castro", "1", 1, "Kids concentrate", "kids concentrate", kg("0.500"), 0, 1),
		// Same pen, BLOCKED item: no kg, must be absent from the mix entirely.
		cell(fdiShedA, "Castro", "1", 1, "Hay", "hay", nil, 0, 2),
		// Worded partition of a digit-terminated shed name: the dash convention.
		cell(shedGodel, "Godel 1", "Part 3", 1, "Masur Busa", "masur busa", kg("4.000"), 2, 0),
		// Undivided shed: bare name, and the 'whole' sentinel must never leak.
		cell(fdiShedB, "Yashoda", "", 1, "Kids concentrate", "kids concentrate", kg("1.500"), 3, 0),
	}

	persist := func(feedDay, fingerprint string) {
		t.Helper()
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":shedfeed",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", feedDay, err)
		}
	}
	persist("2026-07-30", "fp-sf-day1")
	persist("2026-07-31", "fp-sf-day2")

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.ShedFeedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("ShedFeedAnalytics: %v", err)
	}

	if len(got.Rows) != 3 {
		t.Fatalf("want 3 pen rows (Castro 1, Godel 1 - Part 3, Yashoda), got %d: %+v", len(got.Rows), got.Rows)
	}
	byDisplay := map[string]domain.ShedFeedPenRow{}
	for _, row := range got.Rows {
		byDisplay[row.OperationalLocationDisplay] = row
	}

	// ---- Display round trip: the three separator conventions. ----
	castro, ok := byDisplay["Castro 1"]
	if !ok {
		t.Fatalf(`bare-numeric partition must render "Castro 1" (space form); got displays %v`, shedFeedDisplays(byDisplay))
	}
	godel, ok := byDisplay["Godel 1 - Part 3"]
	if !ok {
		t.Fatalf(`worded partition must render "Godel 1 - Part 3" (dash form); got displays %v`, shedFeedDisplays(byDisplay))
	}
	yashoda, ok := byDisplay["Yashoda"]
	if !ok {
		t.Fatalf(`undivided shed must render bare "Yashoda" — never "Yashoda whole"; got displays %v`, shedFeedDisplays(byDisplay))
	}
	if yashoda.PartitionLabel != "" {
		t.Errorf("undivided shed partition_label: want empty, got %q", yashoda.PartitionLabel)
	}

	// ---- Pen grain: sessions and days sum into ONE row per pen×item. ----
	// Castro 1 Masur Busa: (2.0 + 1.0) × 2 days = 6.0; Kids concentrate 0.5 × 2 = 1.0.
	if len(castro.Items) != 2 {
		t.Fatalf("Castro 1 mix: want 2 items (blocked Hay absent), got %+v", castro.Items)
	}
	if castro.Items[0].FeedItemLabel != "Masur Busa" || castro.Items[0].DirectedKg != "6.000" {
		t.Errorf("Castro 1 first item: want Masur Busa 6.000 (largest kg first), got %+v", castro.Items[0])
	}
	if castro.Items[1].FeedItemLabel != "Kids concentrate" || castro.Items[1].DirectedKg != "1.000" {
		t.Errorf("Castro 1 second item: want Kids concentrate 1.000, got %+v", castro.Items[1])
	}
	if castro.DirectedKg != "7.000" {
		t.Errorf("Castro 1 total: want 7.000 across items, got %q", castro.DirectedKg)
	}
	if godel.DirectedKg != "8.000" {
		t.Errorf("Godel 1 - Part 3 total: want 8.000 (4.0 × 2 days), got %q", godel.DirectedKg)
	}

	// ---- Parity with the per-item chart series: summing an item across pens
	// must equal the directed read's window total for that item. ----
	directed, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("DirectedAnalytics: %v", err)
	}
	directedMasur := 0.0
	for _, it := range directed.Items {
		if it.FeedItemKey == "masur busa" {
			directedMasur += shedFeedFloat(t, it.DirectedKg)
		}
	}
	penMasur := 0.0
	for _, row := range got.Rows {
		for _, it := range row.Items {
			if it.FeedItemKey == "masur busa" {
				penMasur += shedFeedFloat(t, it.DirectedKg)
			}
		}
	}
	if penMasur != directedMasur {
		t.Errorf("cross-read parity: pens sum Masur Busa to %v, directed series says %v", penMasur, directedMasur)
	}

	// ---- Park filter: a park the fixture never fed returns empty, not zeros. ----
	filtered, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		ParkIDs: []uuid.UUID{uuid.New()}, DateFrom: window.DateFrom, DateTo: window.DateTo,
	})
	if err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(filtered.Rows) != 0 {
		t.Errorf("foreign park filter: want empty, got %+v", filtered.Rows)
	}
}

// The per-pen DAY series behind the overview's bar charts (maintainer request
// 2026-09-14: one chart per pen, seven bars, each the average per animal).
// Adversarial on the denominator, which is where a per-head figure goes wrong:
// Castro 1 holds TWO breed grains (Beetal 10 head, Sirohi 5 head) and the Beetal
// grain is fed in TWO sessions, so the day's heads must be 15 -- each grain's head
// count taken ONCE, never once per session (30) or once per item cell -- and the
// day's kg is every cell summed (2.0 + 1.0 + 0.5 = 3.5), giving 233.3 g/animal.
// A day where every cell is BLOCKED is ABSENT from the series rather than a zero
// bar; a day authored 0 kg is PRESENT at 0 with per-head "0.0".
func TestShedFeedAnalyticsDaySeriesDividesByPenHeadsOncePerDay(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 8, 5, 9, 0, 0, 0, biztime.DefaultLocation())

	cell := func(breed string, heads int64, session int32, qty *string, rowSeq int32) domain.StoredCell {
		c := domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: breed,
			RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
			HeadCount: heads, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Masur Busa", FeedItemKey: "masur busa", QuantityKg: qty,
			SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: 0,
		}
		if qty == nil {
			code := domain.BlockReasonNoRationRate
			detail := "no rate"
			c.BlockedReasonCode, c.BlockedReasonDetail = &code, &detail
		}
		return c
	}
	persist := func(feedDay, fingerprint string, cells []domain.StoredCell) {
		t.Helper()
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":shedfeedday",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", feedDay, err)
		}
	}
	// Day 1: two grains, one of them fed twice -> 3.5 kg over 15 head.
	persist("2026-08-06", "fp-sfd-1", []domain.StoredCell{
		cell("Beetal", 10, 1, kg("2.000"), 0),
		cell("Beetal", 10, 2, kg("1.000"), 1),
		cell("Sirohi", 5, 1, kg("0.500"), 2),
	})
	// Day 2: everything blocked -> the day is absent.
	persist("2026-08-07", "fp-sfd-2", []domain.StoredCell{
		cell("Beetal", 10, 1, nil, 0),
		cell("Sirohi", 5, 1, nil, 1),
	})
	// Day 3: authored zero -> present at 0.
	persist("2026-08-08", "fp-sfd-3", []domain.StoredCell{
		cell("Beetal", 10, 1, kg("0.000"), 0),
	})

	got, err := repo.ShedFeedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 8, 6, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 8, 8, 0, 0, 0, 0, biztime.DefaultLocation()),
	})
	if err != nil {
		t.Fatalf("ShedFeedAnalytics: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].OperationalLocationDisplay != "Castro 1" {
		t.Fatalf("want the one pen Castro 1, got %+v", got.Rows)
	}
	days := got.Rows[0].Days
	if len(days) != 2 {
		t.Fatalf("want 2 days (the all-blocked day absent), got %+v", days)
	}
	if days[0].FeedDay != "2026-08-06" || days[0].DirectedKg != "3.500" || days[0].HeadCount != 15 || days[0].PerHeadGrams != "233.3" {
		t.Errorf("day 1: want 3.500 kg over 15 head = 233.3 g/animal (heads once per grain per day), got %+v", days[0])
	}
	if days[1].FeedDay != "2026-08-08" || days[1].DirectedKg != "0.000" || days[1].HeadCount != 10 || days[1].PerHeadGrams != "0.0" {
		t.Errorf("day 3: want an authored-zero day present at 0.000 kg / 10 head / 0.0 g, got %+v", days[1])
	}
}

func shedFeedDisplays(m map[string]domain.ShedFeedPenRow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func shedFeedFloat(t *testing.T, raw string) float64 {
	t.Helper()
	var v float64
	if _, err := fmt.Sscan(raw, &v); err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return v
}
