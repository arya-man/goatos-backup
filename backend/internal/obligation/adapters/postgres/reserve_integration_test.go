package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestSM4cReservesStockPerBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000001"
	const lot = "d0000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-R', 'Reserve test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-09-30')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.reserve", Name: "Reserve", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	rows := []struct {
		key    string
		target string
	}{
		{key: "r1", target: "10000000-0000-4000-8000-000000000001"},
		{key: "r2", target: "10000000-0000-4000-8000-000000000002"},
	}
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, rows[0].target, rows[1].target)
	for _, row := range rows { // 2 goats, same scope cbe -> one batch
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.target, ScopeType: "park", ScopeID: cbePark,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: %v", row.key, err)
		}
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 1}
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	res, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 1 {
		t.Fatalf("batches: want 1, got %d", res.Batches)
	}

	var reserved string
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lot).Scan(&reserved); err != nil {
		t.Fatalf("read reserved: %v", err)
	}
	if reserved != "2" {
		t.Fatalf("reserved: want 2 (2 obligations x 1 dose), got %q", reserved)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 1 {
		t.Fatalf("expected 1 reserve movement, got %d", got)
	}

	// Idempotent re-sweep: no new batch, no double reserve.
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lot).Scan(&reserved); err != nil {
		t.Fatalf("read reserved 2: %v", err)
	}
	if reserved != "2" {
		t.Fatalf("re-sweep must not double-reserve, reserved=%q", reserved)
	}
}

// TestSM4cReservesShedScopedDriveFromParkStock proves a SHED-scoped vaccination drive reserves
// against PARK-held vaccine stock by rolling the reservation location up the location hierarchy —
// otherwise every real (shed-scoped) drive would stock-block because stock lives at the park.
func TestSM4cReservesShedScopedDriveFromParkStock(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000101"
	const lot = "d0000000-0000-4000-8000-000000000102"
	const shed = "00000000-0000-4000-8000-00000000a001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-RS', 'Reserve shed test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	// Stock is held at the PARK.
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-09-30')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}
	// A shed UNDER the park (the drive scope).
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
		 VALUES ($1, $2, 'shed', 'SHED-RS', 'Reserve Shed', 'active', $3)`, shed, tenantID, cbePark); err != nil {
		t.Fatalf("seed shed: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.reserve.shed", Name: "Reserve shed", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	rows := []struct {
		key    string
		target string
	}{
		{key: "rs1", target: "10000000-0000-4000-8000-000000000101"},
		{key: "rs2", target: "10000000-0000-4000-8000-000000000102"},
	}
	seedReserveGoats(t, ctx, pool, shed, cbePark, rows[0].target, rows[1].target)
	for _, row := range rows { // 2 goats, shed-scoped obligations -> one shed drive
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.target, ScopeType: "shed", ScopeID: shed,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: %v", row.key, err)
		}
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 1}
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	res, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore)
	if err != nil {
		t.Fatalf("sweep shed-scoped drive against park stock: %v", err)
	}
	if res.Batches != 1 {
		t.Fatalf("batches: want 1 shed drive, got %d", res.Batches)
	}

	var reserved string
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lot).Scan(&reserved); err != nil {
		t.Fatalf("read reserved: %v", err)
	}
	if reserved != "2" {
		t.Fatalf("shed-scoped drive must reserve from park stock (rollup), reserved=%q want 2", reserved)
	}
	var movedLoc string
	if err := pool.QueryRow(ctx, `SELECT location_id::text FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve' LIMIT 1`, tenantID).Scan(&movedLoc); err != nil {
		t.Fatalf("read movement location: %v", err)
	}
	if movedLoc != cbePark {
		t.Fatalf("reserve movement location = %s, want park %s (rolled up from shed)", movedLoc, cbePark)
	}
}

// reserveTestVersion seeds a vaccination protocol version + one rule and returns the version and rule
// ids. Mirrors the setup the other reserve tests use, factored out for the multi-lot tests.
func reserveTestVersion(t *testing.T, ctx context.Context, proto *protopg.Repository, code string) (versionID, ruleID string) {
	t.Helper()
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err = proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	return versionID, ruleID
}

func scanReserved(t *testing.T, ctx context.Context, pool *pgxpool.Pool, stockID string) string {
	t.Helper()
	var reserved string
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, stockID).Scan(&reserved); err != nil {
		t.Fatalf("read reserved for %s: %v", stockID, err)
	}
	return reserved
}

func seedReserveGoats(t *testing.T, ctx context.Context, pool *pgxpool.Pool, currentLocationID, parkID string, goatIDs ...string) {
	t.Helper()
	for _, goatID := range goatIDs {
		if _, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, sex, current_location_id, park_id)
				 VALUES ($1, $2, 'alive', 'clean', $3, 'female', $4, $5)`,
			goatID, tenantID, meshaParty, currentLocationID, parkID); err != nil {
			t.Fatalf("seed goat %s: %v", goatID, err)
		}
	}
}

func TestReserveRejectsLotExpiredBeforePlannedDriveDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000601"
	const lot = "d0000000-0000-4000-8000-000000000602"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-EXP', 'Expiry planned-date test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-08-14')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	versionID, ruleID := reserveTestVersion(t, ctx, proto, "vaccination.expiryplan")
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "park", TargetID: cbePark, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "exp1", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert obligation: %v", err)
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 1}
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := scanReserved(t, ctx, pool, lot); got != "0" {
		t.Fatalf("lot expired before planned date must not be reserved, reserved=%q want 0", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 0 {
		t.Fatalf("expired lot must record no reserve movement, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND context ? 'stock_block'`, tenantID); got != 1 {
		t.Fatalf("expired stock should mark the planned batch stock-blocked, got %d", got)
	}
}

// TestReserveSpansMultipleLotsSameLocation proves a reservation larger than the earliest-expiry lot
// consumes the next lot (FEFO) at the same location, instead of falsely stock-blocking when no single
// lot covers qty. lots: 3 (Aug) + 7 (Dec) at the park; qty 8 -> 3 from Aug then 5 from Dec.
func TestReserveSpansMultipleLotsSameLocation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000201"
	const lotEarly = "d0000000-0000-4000-8000-000000000202" // expires first -> consumed first
	const lotLate = "d0000000-0000-4000-8000-000000000203"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-ML', 'Multi-lot test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 3, 0, 'dose', DATE '2026-08-31'),
		        ($5, $2, $3, $4, 7, 0, 'dose', DATE '2026-12-31')`, lotEarly, tenantID, item, cbePark, lotLate); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	versionID, ruleID := reserveTestVersion(t, ctx, proto, "vaccination.multilot")
	rows := []struct {
		key    string
		target string
	}{
		{key: "ml1", target: "10000000-0000-4000-8000-000000000201"},
		{key: "ml2", target: "10000000-0000-4000-8000-000000000202"},
	}
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, rows[0].target, rows[1].target)
	for _, row := range rows { // 2 park-scoped obligations -> one batch
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.target, ScopeType: "park", ScopeID: cbePark,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: %v", row.key, err)
		}
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 4} // 2 obligations x 4 = qty 8
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := scanReserved(t, ctx, pool, lotEarly); got != "3" {
		t.Fatalf("earliest-expiry lot must be fully consumed first (FEFO), reserved=%q want 3", got)
	}
	if got := scanReserved(t, ctx, pool, lotLate); got != "5" {
		t.Fatalf("remainder must spill to the later lot, reserved=%q want 5", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 2 {
		t.Fatalf("expected 2 reserve movements (one per consumed lot), got %d", got)
	}
}

// TestReserveRollsUpToAncestorWhenNearestLocationInsufficient proves the reservation skips a nearer
// location that cannot fully cover qty and reserves from a farther ancestor that can, instead of
// splitting across levels or blocking. shed holds 2, park holds 10, qty 5 -> all 5 from the park.
func TestReserveRollsUpToAncestorWhenNearestLocationInsufficient(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000301"
	const shedLot = "d0000000-0000-4000-8000-000000000302"
	const parkLot = "d0000000-0000-4000-8000-000000000303"
	const shed = "00000000-0000-4000-8000-00000000b001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-RU', 'Rollup test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
		 VALUES ($1, $2, 'shed', 'SHED-RU', 'Rollup Shed', 'active', $3)`, shed, tenantID, cbePark); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 2, 0, 'dose', DATE '2026-09-30'),
		        ($5, $2, $3, $6, 10, 0, 'dose', DATE '2026-09-30')`, shedLot, tenantID, item, shed, parkLot, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	versionID, ruleID := reserveTestVersion(t, ctx, proto, "vaccination.rollup")
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "shed", TargetID: shed, ScopeType: "shed", ScopeID: shed,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "ru1", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert obligation: %v", err)
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 5} // 1 obligation x 5 = qty 5 (> shed's 2)
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := scanReserved(t, ctx, pool, shedLot); got != "0" {
		t.Fatalf("nearer shed lot cannot cover qty and must be left untouched, reserved=%q want 0", got)
	}
	if got := scanReserved(t, ctx, pool, parkLot); got != "5" {
		t.Fatalf("reservation must roll up to the park lot that covers qty, reserved=%q want 5", got)
	}
}

// TestReserveBlocksWhenNoLocationCoversQty proves a hard block (not a partial reserve) when stock
// exists but no single ancestor covers qty. shed 2 + park 2, qty 5 -> blocked, nothing reserved.
func TestReserveBlocksWhenNoLocationCoversQty(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000401"
	const shedLot = "d0000000-0000-4000-8000-000000000402"
	const parkLot = "d0000000-0000-4000-8000-000000000403"
	const shed = "00000000-0000-4000-8000-00000000c001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-BL', 'Block test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
		 VALUES ($1, $2, 'shed', 'SHED-BL', 'Block Shed', 'active', $3)`, shed, tenantID, cbePark); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 2, 0, 'dose', DATE '2026-09-30'),
		        ($5, $2, $3, $6, 2, 0, 'dose', DATE '2026-09-30')`, shedLot, tenantID, item, shed, parkLot, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	versionID, ruleID := reserveTestVersion(t, ctx, proto, "vaccination.block")
	const goat = "10000000-0000-4000-8000-000000000401"
	seedReserveGoats(t, ctx, pool, shed, cbePark, goat)
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goat, ScopeType: "shed", ScopeID: shed,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "bl1", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert obligation: %v", err)
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 5} // qty 5 > any single location's 2
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("sweep should mark the batch blocked and continue, got error: %v", err)
	}
	if got := scanReserved(t, ctx, pool, shedLot); got != "0" {
		t.Fatalf("blocked reservation must reserve nothing, shed reserved=%q want 0", got)
	}
	if got := scanReserved(t, ctx, pool, parkLot); got != "0" {
		t.Fatalf("blocked reservation must reserve nothing, park reserved=%q want 0", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 0 {
		t.Fatalf("blocked reservation must record no reserve movement, got %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND context ? 'stock_block'`, tenantID); got != 1 {
		t.Fatalf("blocked reservation must mark the batch stock-blocked, got %d", got)
	}
}

// TestReserveRetryDoesNotDoubleReserveAcrossLots proves the per-batch idempotency guard holds even when
// the original reservation spanned multiple lots: a re-sweep reserves nothing more and adds no movement.
func TestReserveRetryDoesNotDoubleReserveAcrossLots(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000501"
	const lotEarly = "d0000000-0000-4000-8000-000000000502"
	const lotLate = "d0000000-0000-4000-8000-000000000503"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-RT', 'Retry test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 3, 0, 'dose', DATE '2026-08-31'),
		        ($5, $2, $3, $4, 7, 0, 'dose', DATE '2026-12-31')`, lotEarly, tenantID, item, cbePark, lotLate); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	versionID, ruleID := reserveTestVersion(t, ctx, proto, "vaccination.retry")
	rows := []struct {
		key    string
		target string
	}{
		{key: "rt1", target: "10000000-0000-4000-8000-000000000501"},
		{key: "rt2", target: "10000000-0000-4000-8000-000000000502"},
	}
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, rows[0].target, rows[1].target)
	for _, row := range rows {
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.target, ScopeType: "park", ScopeID: cbePark,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: %v", row.key, err)
		}
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 4} // qty 8 across the 3 + 7 lots
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := scanReserved(t, ctx, pool, lotEarly); got != "3" {
		t.Fatalf("first sweep reserved=%q want 3 on early lot", got)
	}
	if got := scanReserved(t, ctx, pool, lotLate); got != "5" {
		t.Fatalf("first sweep reserved=%q want 5 on late lot", got)
	}

	// Re-sweep: the per-batch guard must reserve nothing more on EITHER lot and add no movement.
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if got := scanReserved(t, ctx, pool, lotEarly); got != "3" {
		t.Fatalf("re-sweep double-reserved early lot, reserved=%q want 3", got)
	}
	if got := scanReserved(t, ctx, pool, lotLate); got != "5" {
		t.Fatalf("re-sweep double-reserved late lot, reserved=%q want 5", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 2 {
		t.Fatalf("re-sweep must add no reserve movement, got %d want 2", got)
	}
}
