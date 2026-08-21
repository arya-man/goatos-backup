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

func TestDirectedAnalyticsOneToManyGrainProofs(t *testing.T) {
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

// Execution + experiment rollups on the real schema. Completion/task rows are
// inserted directly at their own natural grain — this is a package read-model
// test of the status counting, not an E2E of the proof write paths (those have
// their own verification-gate integration tests).
func TestExecutionAnalyticsStatusMatrixAndExperimentArms(t *testing.T) {
	// Aggregate guard proof for analytics.go: MultipleDimensions MultiPage
	// CohortScope EveryStatus.
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	// Transport FKs onto locations: register the shed.
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($2::uuid, $1::uuid, 'shed', 'CASTRO', 'Castro', 'active', $3::uuid)
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, fdiShedA, fdiPark)

	verifiedAt := time.Date(2026, 7, 30, 11, 0, 0, 0, biztime.DefaultLocation())
	createdAt := verifiedAt.Add(-90 * time.Minute)
	// Packing: one verified (90 min latency), one awaiting, one rework.
	exec(`INSERT INTO feed_packing_completions
  (tenant_id, park_id, shed_id, session_no, target_date, workflow, status, packing_proof_ref, verified_at, created_at, idempotency_key)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-30', 'normal', 'completed', 'proof-1', $4, $5, 'k1'),
  ($1::uuid, $2::uuid, $3::uuid, 2, DATE '2026-07-30', 'normal', 'pending_verification', 'proof-2', NULL, $5, 'k2'),
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-31', 'normal', 'rework', NULL, NULL, $5, 'k3')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt, createdAt)
	// Distribution: one verified (30 min latency) — daily median over 90 and 30 is 60.
	exec(`INSERT INTO feed_distribution_completions
  (tenant_id, park_id, shed_id, session_no, target_date, workflow, status, distribution_proof_ref, water_proof_ref, feed_weight_proof_ref, verified_at, created_at, idempotency_key)
VALUES
  ($1::uuid, $2::uuid, $3::uuid, 1, DATE '2026-07-30', 'normal', 'completed', 'd-1', 'w-1', 'f-1', $4, $5, 'k4')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt, verifiedAt.Add(-30*time.Minute))
	// Transport: one completed, one still due.
	exec(`INSERT INTO feed_transport_tasks (tenant_id, park_id, shed_id, business_date, scheduled_at, status)
VALUES ($1::uuid, $2::uuid, $3::uuid, DATE '2026-07-30', $4, 'completed'),
       ($1::uuid, $2::uuid, $3::uuid, DATE '2026-07-31', $4, 'due')`,
		fdiTenant, fdiPark, fdiShedA, verifiedAt)

	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 31, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	got, err := repo.ExecutionAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("ExecutionAnalytics: %v", err)
	}
	if len(got.Days) != 2 {
		t.Fatalf("want 2 execution days, got %+v", got.Days)
	}
	d1, d2 := got.Days[0], got.Days[1]
	if d1.Date != "2026-07-30" || d1.PackingVerified != 1 || d1.PackingAwaiting != 1 || d1.DistributionVerified != 1 || d1.TransportCompleted != 1 {
		t.Errorf("day1 counts wrong: %+v", d1)
	}
	if d1.MedianVerifyLatencyMinutes == nil || *d1.MedianVerifyLatencyMinutes != 60 {
		t.Errorf("day1 median latency: want 60 (median of 90 and 30), got %v", d1.MedianVerifyLatencyMinutes)
	}
	if d2.Date != "2026-07-31" || d2.PackingRework != 1 || d2.TransportOpen != 1 || d2.MedianVerifyLatencyMinutes != nil {
		t.Errorf("day2 counts wrong: %+v", d2)
	}

	// Experiment series through the production write path: two arms, one with
	// two pens of one shed — pens count by pen-grain, kg is the authored total.
	expCell := func(partition, arm, qty string, rowSeq int32) domain.StoredCell {
		return domain.StoredCell{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: partition, ShedTag: "Non-Pregnant", Breed: "Beetal",
			ExperimentArm: arm, SessionNo: 1, SessionLabel: "S",
			HeadCount: 7, HeadCountInformational: true, Workflow: domain.WorkflowExperiment,
			FeedItemLabel: "Mesha TMR", FeedItemKey: "mesha tmr", QuantityKg: kg(qty),
			SessionTotalKg: qty, RowSeq: rowSeq,
		}
	}
	cmd := ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowExperiment,
		IssuedAt: verifiedAt, Fingerprint: "fp-exp-arms",
		IdempotencyKey: "issue:exp:" + fdiTenant, GeneratedBy: "test",
		Cells: []domain.StoredCell{
			expCell("1", "Mesha TMR — adult", "40.000", 0),
			expCell("2", "Mesha TMR — adult", "35.000", 1),
			expCell("3", "Sorghum pellet mix", "20.000", 2),
		},
	}
	if _, err := repo.PersistIssue(ctx, cmd); err != nil {
		t.Fatalf("persist experiment: %v", err)
	}
	expWindow := window
	expWindow.WastageDay = time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation())
	exp, err := repo.ExperimentAnalytics(ctx, fdiTenant, expWindow)
	if err != nil {
		t.Fatalf("ExperimentAnalytics: %v", err)
	}
	// The series is BY FEED ITEM (maintainer decision 2026-08-19) — all three
	// pens feed the same item, so one row carries the day's authored total.
	if len(exp.Items) != 1 {
		t.Fatalf("want 1 feed-item row, got %+v", exp.Items)
	}
	it := exp.Items[0]
	if it.FeedDay != "2026-07-30" || it.FeedItemLabel != "Mesha TMR" || it.Kg != "95.000" {
		t.Errorf("item row: want Mesha TMR 95.000 on 2026-07-30, got %+v", it)
	}
	// The per-pen wastage table derives its pen list from the SAME sheet: three
	// pens, none with a video yet, statuses honestly blank with kg blank —
	// never a fabricated zero.
	if exp.WastageDay != "2026-07-30" {
		t.Errorf("wastage day echo: %q", exp.WastageDay)
	}
	if len(exp.WastagePens) != 3 {
		t.Fatalf("want 3 wastage pens, got %+v", exp.WastagePens)
	}
	for _, p := range exp.WastagePens {
		if p.LifecycleStatus != "" || p.WastageKg != "" {
			t.Errorf("pen %s: want blank status/kg before any submit, got %+v", p.OperationalLocationDisplay, p)
		}
	}
	if exp.WastagePens[0].OperationalLocationDisplay != "Castro 1" {
		t.Errorf("pen display: want 'Castro 1' (numeric pen, space form), got %q", exp.WastagePens[0].OperationalLocationDisplay)
	}
}

// TestDirectedAnalyticsParkScopeFilter pins the scope rule on its own: a park
// the fixture never fed returns EMPTY series, never fabricated zeros, and the
// unfiltered read is unchanged by asking twice (no hidden cursor state).
func TestDirectedAnalyticsParkScopeFilter(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	cmd := ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-07-30", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-scope",
		IdempotencyKey: "issue:scope:" + fdiTenant, GeneratedBy: "test", Cells: analyticsCells(),
	}
	if _, err := repo.PersistIssue(ctx, cmd); err != nil {
		t.Fatalf("persist: %v", err)
	}
	window := domain.DirectedAnalyticsQuery{
		DateFrom: time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
		DateTo:   time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation()),
	}
	foreign := window
	foreign.ParkIDs = []uuid.UUID{uuid.New()}
	got, err := repo.DirectedAnalytics(ctx, fdiTenant, foreign)
	if err != nil {
		t.Fatalf("foreign park: %v", err)
	}
	if len(got.Days) != 0 || len(got.Items) != 0 {
		t.Errorf("foreign park must be empty, got %+v", got)
	}
	own := window
	own.ParkIDs = []uuid.UUID{uuid.MustParse(fdiPark)}
	scoped, err := repo.DirectedAnalytics(ctx, fdiTenant, own)
	if err != nil {
		t.Fatalf("own park: %v", err)
	}
	all, err := repo.DirectedAnalytics(ctx, fdiTenant, window)
	if err != nil {
		t.Fatalf("unfiltered: %v", err)
	}
	if len(scoped.Days) != 1 || len(all.Days) != 1 || scoped.Days[0] != all.Days[0] {
		t.Errorf("single-park tenant: scoped and unfiltered must agree, got %+v vs %+v", scoped.Days, all.Days)
	}
}

// TestDirectedAnalyticsWindowSplitPageBoundary pins page-size invariance the
// way this read expresses it: there IS no page input, so the whole-window
// figures must equal the union of per-day windows — splitting the window can
// never change a day's numbers.
func TestDirectedAnalyticsWindowSplitPageBoundary(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	for i, day := range []string{"2026-07-30", "2026-07-31"} {
		cmd := ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fmt.Sprintf("fp-split-%d", i),
			IdempotencyKey: "issue:split:" + day, GeneratedBy: "test", Cells: analyticsCells(),
		}
		if _, err := repo.PersistIssue(ctx, cmd); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
	}
	at := func(d string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", d, biztime.DefaultLocation())
		if err != nil {
			t.Fatalf("parse %s: %v", d, err)
		}
		return parsed
	}
	whole, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: at("2026-07-30"), DateTo: at("2026-07-31")})
	if err != nil {
		t.Fatalf("whole: %v", err)
	}
	if len(whole.Days) != 2 {
		t.Fatalf("want 2 days, got %+v", whole.Days)
	}
	for i, d := range []string{"2026-07-30", "2026-07-31"} {
		single, err := repo.DirectedAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: at(d), DateTo: at(d)})
		if err != nil {
			t.Fatalf("single %s: %v", d, err)
		}
		if len(single.Days) != 1 || single.Days[0] != whole.Days[i] {
			t.Errorf("window split changed %s: %+v vs %+v", d, single.Days, whole.Days[i])
		}
	}
}

// The Stock tab's per-farm Mesha-concentrate table: purchases at their
// (farm, item, batch) natural key, consumption from LOCKED sheets only, and
// output STRINGS asserted on a real DB round trip (operational-location rule 9).
func TestStockFarmItemsMeshaTablePerFarm(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	insertPurchase := func(farm, label string, batch int64, date, qty string, parkID *string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status)
VALUES ($1, $2, $3, $4, $5, $6::date, $7::numeric, 41.5266, 1000, 0, DATE '2026-08-10', 'Navaladi', 'Paid')`,
			fdiTenant, parkID, farm, label, batch, date, qty); err != nil {
			t.Fatalf("insert purchase %s/%s#%d: %v", farm, label, batch, err)
		}
	}
	park := fdiPark
	// Two loads of one Mesha item at one farm: first-purchase must read the
	// older date while last-load carries the newer batch's details.
	insertPurchase("CBE", "Mesha Kids Goat Concentrate", 298, "2026-06-20", "1550.000", &park)
	insertPurchase("CBE", "Mesha Kids Goat Concentrate", 330, "2026-08-08", "1150.000", &park)
	// A row whose simple expected balance goes negative: the stock check must
	// compare ledger stock against the shortage, not just echo the shortage.
	insertPurchase("CBE", "Mesha Kids Sheep Concentrate", 329, "2026-08-08", "1200.000", &park)
	// A Mesha item purchased but NEVER directed: directed columns stay empty.
	insertPurchase("CBE", "Mesha Adult Concentrate Sheep", 331, "2026-08-08", "400.000", &park)
	// A NON-Mesha item: must not appear in the table at all.
	insertPurchase("CBE", "Concentrate", 328, "2026-08-01", "3000.000", &park)
	// A farm the importer could not resolve to a park: row still serves, bare.
	insertPurchase("XYZ", "Mesha Kids Goat Concentrate", 5, "2026-08-01", "10.000", nil)

	issuedAt := time.Date(2026, 8, 10, 9, 0, 0, 0, biztime.DefaultLocation())
	persistDay := func(feedDay, fingerprint string, qty string) {
		t.Helper()
		cells := []domain.StoredCell{{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Mesha Kids Goat Concentrate", FeedItemKey: "mesha_kids_goat_concentrate",
			QuantityKg: kg(qty), SessionTotalKg: qty,
		}}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":stockfarm",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", feedDay, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay,
			Workflow: domain.WorkflowNormal, LockedAt: issuedAt,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s = (%v, %v)", feedDay, lock.Outcome, err)
		}
	}
	persistKidsSheepDay := func(feedDay, fingerprint string, qty string) {
		t.Helper()
		cells := []domain.StoredCell{{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Mesha Kids Sheep Concentrate", FeedItemKey: "mesha_kids_sheep_concentrate",
			QuantityKg: kg(qty), SessionTotalKg: qty,
		}}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: fingerprint,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + feedDay + ":stockfarm-sheep",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", feedDay, err)
		}
		if lock, err := repo.LockIssue(ctx, ports.LockIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay,
			Workflow: domain.WorkflowNormal, LockedAt: issuedAt,
		}); err != nil || lock.Outcome != "locked" {
			t.Fatalf("lock %s = (%v, %v)", feedDay, lock.Outcome, err)
		}
	}
	// Four locked days: 20, 22, 30, 32 kg. The burn-rate window is the 3 MOST
	// RECENT locked days (matching the farm's legacy stock sheet, maintainer
	// decision 2026-08-21), so avg = (22+30+32)/3 = 28.0 — the Aug 11 day falls
	// OUT of the average while remaining the consumption-start date.
	persistDay("2026-08-11", "fp-sf-1", "20.000")
	persistDay("2026-08-12", "fp-sf-2", "22.000")
	persistDay("2026-08-13", "fp-sf-3", "30.000")
	persistDay("2026-08-14", "fp-sf-4", "32.000")
	persistKidsSheepDay("2026-08-11", "fp-sf-sheep-1", "200.000")
	persistKidsSheepDay("2026-08-12", "fp-sf-sheep-2", "200.000")
	persistKidsSheepDay("2026-08-13", "fp-sf-sheep-3", "200.000")
	persistKidsSheepDay("2026-08-14", "fp-sf-sheep-4", "200.000")
	// An ISSUED (unlocked) earlier day must not move the consumption start.
	if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: "2026-08-10", Workflow: domain.WorkflowNormal,
		IssuedAt: issuedAt, Fingerprint: "fp-sf-0",
		IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":2026-08-10:stockfarm",
		GeneratedBy:    "test", Cells: []domain.StoredCell{{
			ParkID: fdiPark, ParkLabel: "CBE", ShedID: fdiShedA, ShedLabel: "Castro",
			PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
			RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
			HeadCount: 10, Workflow: domain.WorkflowNormal,
			FeedItemLabel: "Mesha Kids Goat Concentrate", FeedItemKey: "mesha_kids_goat_concentrate",
			QuantityKg: kg("99.000"), SessionTotalKg: "99.000",
		}},
	}); err != nil {
		t.Fatalf("persist issued day: %v", err)
	}

	got, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{})
	if err != nil {
		t.Fatalf("StockAnalytics: %v", err)
	}
	if len(got.FarmItems) != 4 {
		t.Fatalf("want 4 farm rows (Mesha only, non-Mesha excluded), got %d: %+v", len(got.FarmItems), got.FarmItems)
	}
	// Ordered by feed item then farm.
	sheep := got.FarmItems[0]
	if sheep.FeedItemLabel != "Mesha Adult Concentrate Sheep" || sheep.FarmLabel != "CBE" {
		t.Fatalf("row 0: %+v", sheep)
	}
	if sheep.FirstDirectedDay != "" || sheep.AvgDailyKg != "" {
		t.Errorf("never-directed item must serve empty consumption fields, got %q / %q", sheep.FirstDirectedDay, sheep.AvgDailyKg)
	}
	kids := got.FarmItems[1]
	if kids.FeedItemLabel != "Mesha Kids Goat Concentrate" || kids.FarmLabel != "CBE" {
		t.Fatalf("row 1: %+v", kids)
	}
	if kids.FirstPurchaseDate != "2026-06-20" {
		t.Errorf("first purchase: want 2026-06-20, got %q", kids.FirstPurchaseDate)
	}
	if kids.FirstDirectedDay != "2026-08-11" {
		t.Errorf("consumption from locked sheets only: want 2026-08-11, got %q", kids.FirstDirectedDay)
	}
	if kids.AvgDailyKg != "28.0" {
		t.Errorf("avg over the 3 most recent locked days: want 28.0 ((22+30+32)/3, day 1 outside the window), got %q", kids.AvgDailyKg)
	}
	if kids.LastLoadBatchNo != 330 || kids.LastLoadDate != "2026-08-08" ||
		kids.LastLoadQuantityKg != "1150.0" || kids.LastLoadVendor != "Navaladi" {
		t.Errorf("last load details: %+v", kids)
	}
	if kids.ExpectedStockKg != "898.0" || kids.LedgerStockKg != "2596.0" {
		t.Errorf("stock reconciliation fields: %+v", kids)
	}
	kidsSheep := got.FarmItems[2]
	if kidsSheep.FeedItemLabel != "Mesha Kids Sheep Concentrate" || kidsSheep.FarmLabel != "CBE" {
		t.Fatalf("row 2: %+v", kidsSheep)
	}
	if kidsSheep.ExpectedStockKg != "-600.0" || kidsSheep.LedgerStockKg != "400.0" || kidsSheep.StockVarianceKg != "-200.0" {
		t.Errorf("negative expected must compare ledger against shortage, got %+v", kidsSheep)
	}
	orphan := got.FarmItems[3]
	if orphan.FarmLabel != "XYZ" || orphan.FirstDirectedDay != "" {
		t.Errorf("park-less farm must serve with empty consumption, got %+v", orphan)
	}

	t.Run("FarmItemsOneToManyLoadsStayOneRowPerFarmItem", func(t *testing.T) {
		if kids.FirstPurchaseDate != "2026-06-20" || kids.LastLoadBatchNo != 330 {
			t.Fatalf("multi-load row must preserve first purchase and latest load: %+v", kids)
		}
		if kids.LastLoadQuantityKg != "1150.0" || kids.ExpectedStockKg != "898.0" {
			t.Fatalf("expected stock must use latest load quantity, not summed historical purchases: %+v", kids)
		}
	})

	t.Run("FarmItemsMultiPageBoundaryReturnsAllMeshaRows", func(t *testing.T) {
		if len(got.FarmItems) != 4 {
			t.Fatalf("farm item table is unpaginated and bounded; want all 4 Mesha rows, got %d", len(got.FarmItems))
		}
	})

	t.Run("FarmItemsParkScopeKeepsUnresolvedFarmBare", func(t *testing.T) {
		if orphan.FarmLabel != "XYZ" || orphan.FeedItemKey != "mesha_kids_goat_concentrate" || orphan.FirstDirectedDay != "" {
			t.Fatalf("park scope join must not borrow CBE directed rows for unresolved farms: %+v", orphan)
		}
	})

	// Stock cards are PER FARM: the same item bought at two farms must never
	// collapse into one combined balance (maintainer decision 2026-08-21).
	t.Run("StockCardsAreParkScopedNeverCombined", func(t *testing.T) {
		var kidsCards []domain.StockItem
		for _, it := range got.Items {
			if it.FeedItemKey == "mesha_kids_goat_concentrate" {
				kidsCards = append(kidsCards, it)
			}
		}
		if len(kidsCards) != 2 {
			t.Fatalf("want one card per farm (CBE + XYZ), got %d: %+v", len(kidsCards), kidsCards)
		}
		byFarm := map[string]domain.StockItem{}
		for _, it := range kidsCards {
			byFarm[it.FarmLabel] = it
		}
		cbe, xyz := byFarm["CBE"], byFarm["XYZ"]
		// CBE: 1550+1150 purchased, 104 kg locked-directed -> 2596.0 in store.
		if cbe.BalanceKg != "2596.0" || cbe.AvgDailyKg != "28.0" {
			t.Errorf("CBE card must hold only CBE's store: %+v", cbe)
		}
		if kids.LedgerStockKg != cbe.BalanceKg {
			t.Errorf("farm table ledger must match stock card balance, table=%q card=%q", kids.LedgerStockKg, cbe.BalanceKg)
		}
		// XYZ resolves to no park: its 10 kg stays whole, no burn rate.
		if xyz.BalanceKg != "10.000" && xyz.BalanceKg != "10.0" {
			t.Errorf("XYZ card: %+v", xyz)
		}
		if xyz.AvgDailyKg != "" || xyz.DaysLeft != nil {
			t.Errorf("park-less farm has no directed burn rate: %+v", xyz)
		}
	})

	// OneToMany: two loads of the same (farm, item) collapsed to ONE row above —
	// re-assert the collapse survives a third load on the SAME purchase date
	// (batch_no alone must break the tie for last-load).
	t.Run("OneToManyLoadsSameDateTieBreaksOnBatch", func(t *testing.T) {
		insertPurchase("CBE", "Mesha Kids Goat Concentrate", 340, "2026-08-08", "50.000", &park)
		again, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{})
		if err != nil {
			t.Fatalf("StockAnalytics: %v", err)
		}
		if len(again.FarmItems) != 4 {
			t.Fatalf("a third load must not add a row: %+v", again.FarmItems)
		}
		if again.FarmItems[1].LastLoadBatchNo != 340 {
			t.Errorf("same-date tie must break on batch_no: %+v", again.FarmItems[1])
		}
	})

	// PageBoundary/window independence: the farm table is a whole-ledger
	// aggregate — the page's expenditure date window must not move it.
	t.Run("PageBoundaryFreeWindowIndependence", func(t *testing.T) {
		day := time.Date(2026, 8, 12, 0, 0, 0, 0, biztime.DefaultLocation())
		narrow, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: day, DateTo: day})
		if err != nil {
			t.Fatalf("StockAnalytics narrow: %v", err)
		}
		if len(narrow.FarmItems) != 3 || narrow.FarmItems[1].FirstDirectedDay != "2026-08-11" {
			t.Errorf("date window must not change the farm table: %+v", narrow.FarmItems)
		}
	})

	// ParkScope: a caller scoped to another park sees neither this park's
	// purchases nor the park-less XYZ farm row.
	t.Run("ParkScopeFilterExcludesOtherParksAndParklessFarms", func(t *testing.T) {
		other := uuid.New()
		scoped, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{ParkIDs: []uuid.UUID{other}})
		if err != nil {
			t.Fatalf("StockAnalytics scoped: %v", err)
		}
		if len(scoped.FarmItems) != 0 {
			t.Errorf("foreign park scope must serve zero farm rows, got %+v", scoped.FarmItems)
		}
	})
}

func TestStockExpenditureStatusBucketsPriceSameItemAtEachFarmLoad(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)

	parkCBE := fdiPark
	parkCPT := "fd100000-0000-4000-8000-000000003002"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, fdiTenant, parkCPT); err != nil {
		t.Fatalf("seed CPT park: %v", err)
	}

	insertPurchase := func(parkID, farm string, batch int64, date string, perKg string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status)
VALUES ($1, $2, $3, 'Mesha Kids Goat Concentrate', $4, $5::date, 1000, $6::numeric, 1000,
        0, DATE '2026-08-01', 'Farm vendor', 'Paid')`,
			fdiTenant, parkID, farm, batch, date, perKg); err != nil {
			t.Fatalf("insert purchase %s: %v", farm, err)
		}
	}
	// CPT has the later load. The old item-only lateral lookup picked this
	// cheaper rate for BOTH farms under all-farms scope.
	insertPurchase(parkCBE, "CBE", 10, "2026-08-01", "100")
	insertPurchase(parkCPT, "CPT", 11, "2026-08-02", "7")

	persist := func(parkID, farm, issueID, shedID string) {
		t.Helper()
		issuedAt := time.Date(2026, 8, 10, 9, 0, 0, 0, biztime.DefaultLocation())
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: parkID, FeedDay: "2026-08-15", Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-price-" + farm,
			IdempotencyKey: "issue:" + fdiTenant + ":" + parkID + ":2026-08-15:price",
			GeneratedBy:    "test",
			Cells: []domain.StoredCell{{
				ParkID: parkID, ParkLabel: farm, ShedID: shedID, ShedLabel: farm + " Shed",
				PartitionLabel: "1", ShedTag: "Non-Pregnant", Breed: "Beetal",
				RationGroup: "Beetal/Sirohi", SessionNo: 1, SessionLabel: "Morning",
				HeadCount: 10, Workflow: domain.WorkflowNormal,
				FeedItemLabel: "Mesha Kids Goat Concentrate", FeedItemKey: "mesha_kids_goat_concentrate",
				QuantityKg: kg("10.000"), SessionTotalKg: "10.000",
			}},
		}); err != nil {
			t.Fatalf("persist %s issue %s: %v", farm, issueID, err)
		}
	}
	persist(parkCBE, "CBE", "price-cbe", fdiShedA)
	persist(parkCPT, "CPT", "price-cpt", fdiShedB)

	day := time.Date(2026, 8, 15, 0, 0, 0, 0, biztime.DefaultLocation())
	got, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{DateFrom: day, DateTo: day})
	if err != nil {
		t.Fatalf("StockAnalytics: %v", err)
	}
	if len(got.Expenditure) != 1 {
		t.Fatalf("want one expenditure day, got %+v", got.Expenditure)
	}
	// CBE: 10 kg × ₹100 = 1000; CPT: 10 kg × ₹7 = 70. The all-farms total
	// must sum farm-priced rows, not reprice CBE at CPT's later load.
	if got.Expenditure[0].Rupees != "1070" {
		t.Fatalf("all-farms expenditure must price each farm at its own latest load, got %+v", got.Expenditure[0])
	}
	if got.Spend.ThisYear != "1070" {
		t.Fatalf("spend summary must use the same farm-grain pricing, got %+v", got.Spend)
	}

	cbeScoped, err := repo.StockAnalytics(ctx, fdiTenant, domain.DirectedAnalyticsQuery{
		DateFrom: day,
		DateTo:   day,
		ParkIDs:  []uuid.UUID{uuid.MustParse(parkCBE)},
	})
	if err != nil {
		t.Fatalf("StockAnalytics CBE scoped: %v", err)
	}
	if len(cbeScoped.Expenditure) != 1 || cbeScoped.Expenditure[0].Rupees != "1000" {
		t.Fatalf("CBE scope must keep only CBE price and kg, got %+v", cbeScoped.Expenditure)
	}
}
