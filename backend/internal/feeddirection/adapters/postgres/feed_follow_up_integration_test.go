package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Grain proofs for the Feed follow-up read: does the sheet react when animals
// are bought, sold or lost?
//
// The sheet side is seeded through PersistIssue (the production write path),
// never by hand-inserting rollup rows. The herd side is seeded directly,
// because those are another module's INPUT facts -- an exit, a sale tag, a load
// intake -- and this read only ever reads them.
//
// THE FIXTURE CARRIES THE DEFECT THAT SHIPPED FIRST. Castro's partition is the
// bare numeric "1" and Godel 1's is the WORDED "Part 3", because the live
// database stores `part 3` in feed_direction_issue_rows.partition_key while
// feed_config_norm('Part 3') is `part_3`. Keying the herd side with the
// normalizer matched NOTHING and every worded pen read as "no sheet ever
// reached this pen". A fixture with only bare-numeric partitions passes that
// bug; this one does not.

const (
	ffuParty = "fd100000-0000-4000-8000-0000000090a1"
	ffuLoad  = "fd100000-0000-4000-8000-0000000090b1"
)

// ffuSeedHerd puts one animal in a pen and gives it a tag. `pen` is the HUMAN
// partition label ("1", "Part 3", or "" for an undivided shed).
func ffuSeedHerd(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shed, pen, tag string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed herd: %v\nsql: %s", err, sql)
		}
	}
	// parties is tenant-less by schema; the custodian is only here because
	// goats.custodian_party_id is NOT NULL.
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Fixture', 'active') ON CONFLICT (party_id) DO NOTHING`, ffuParty)
	// The shed as a LOCATION. PersistIssue carries shed labels on its own rows
	// and needs no location row, but goats.shed_id is a foreign key -- and the
	// causes read resolves a pen's name from here when the sheet never covered
	// it, which is the gap the first live run showed.
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active') ON CONFLICT (location_id) DO NOTHING`,
		shed, fdiTenant, "Shed "+shed[len(shed)-4:], fdiPark)
	exec(`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, custodian_party_id, park_id, shed_id)
VALUES ($1::uuid, $2::uuid, 'female', 'alive', $3::uuid, $4::uuid, $5::uuid)
ON CONFLICT (goat_id) DO NOTHING`, goatID, fdiTenant, ffuParty, fdiPark, shed)
	if pen != "" {
		exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'fixture')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET partition_label = EXCLUDED.partition_label`,
			fdiTenant, goatID, shed, pen)
	}
	exec(`INSERT INTO goat_identifiers
  (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, lower($3), 'tenant', 'active', now(), 'v1')`,
		fdiTenant, goatID, tag)
}

// Two times of day, used to prove the read IGNORES them: the recorded instant
// on live data is a batch entry stamp (all 154 sale rows share ten instants,
// one per day, mostly evening), so a morning and an evening entry on the same
// day must produce the identical check.
const (
	ffuMorning = "10:00"
	ffuEvening = "19:47"
)

func ffuMarkDied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, day, atTime string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE goats
SET lifecycle_status='dead', exit_reason='died', exited_at=($2::date + $4::time) AT TIME ZONE 'Asia/Kolkata'
WHERE tenant_id=$3::uuid AND goat_id=$1::uuid`, goatID, day, fdiTenant, atTime); err != nil {
		t.Fatalf("mark died: %v", err)
	}
}

func ffuMarkSold(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shed, pen, tag, day, atTime string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO goat_sale_allocations
  (tenant_id, goat_id, sales_deal_id, park_id, shed_id, partition_label, tag_number, status, allocated_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, gen_random_uuid(), $3::uuid, $4::uuid, $5, $6, 'tagged',
        ($7::date + $8::time) AT TIME ZONE 'Asia/Kolkata', 'ffu:' || $2)`,
		fdiTenant, goatID, fdiPark, shed, pen, tag, day, atTime); err != nil {
		t.Fatalf("mark sold: %v", err)
	}
}

func ffuMarkPurchased(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, day, atTime string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("mark purchased: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'ffu-load') ON CONFLICT (load_id) DO NOTHING`, ffuLoad, fdiTenant, ffuParty)
	exec(`INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, current_state, intake_accepted_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'accepted_herd_intake', ($4::date + $5::time) AT TIME ZONE 'Asia/Kolkata')`,
		fdiTenant, ffuLoad, goatID, day, atTime)
}

// ffuLock drives the production LOCK transition for a feed day, the same one
// the transport cutoff runs. The follow-up read judges LOCKED directions only,
// so a fixture that only issues is a direction still being corrected -- which
// is exactly the live state that must NOT be judged.
func ffuLock(t *testing.T, ctx context.Context, repo *Repository, feedDay string) {
	t.Helper()
	if _, err := repo.LockIssue(ctx, ports.LockIssueCommand{
		TenantID: fdiTenant, ParkID: fdiPark, FeedDay: feedDay, Workflow: domain.WorkflowNormal,
		LockedAt: time.Date(2026, 7, 29, 15, 30, 0, 0, biztime.DefaultLocation()),
	}); err != nil {
		t.Fatalf("lock %s: %v", feedDay, err)
	}
}

// ffuCell is one sheet cell at a pen, with the head count the sheet packs for.
func ffuCell(shed, shedLabel, pen string, session int32, heads int64, qty string, rowSeq int32) domain.StoredCell {
	q := qty
	return domain.StoredCell{
		ParkID: fdiPark, ParkLabel: "CBE", ShedID: shed, ShedLabel: shedLabel,
		PartitionLabel: pen, ShedTag: "Non-Pregnant", Breed: "Beetal",
		RationGroup: "Beetal/Sirohi", SessionNo: session, SessionLabel: "S",
		HeadCount: heads, Workflow: domain.WorkflowNormal,
		FeedItemLabel: "Masur Busa", FeedItemKey: "masur busa", QuantityKg: &q,
		SessionTotalKg: "0.000", RowSeq: rowSeq, ItemSeq: 0,
	}
}

func ffuWindow(from, to string) domain.DirectedAnalyticsQuery {
	parse := func(v string) time.Time {
		d, err := time.ParseInLocation("2006-01-02", v, biztime.DefaultLocation())
		if err != nil {
			panic(err)
		}
		return d
	}
	return domain.DirectedAnalyticsQuery{DateFrom: parse(from), DateTo: parse(to)}
}

func ffuRowByPen(t *testing.T, got domain.FeedFollowUp, display string) domain.FeedFollowUpPenRow {
	t.Helper()
	for _, row := range got.Rows {
		if row.OperationalLocationDisplay == display {
			return row
		}
	}
	names := []string{}
	for _, row := range got.Rows {
		names = append(names, row.OperationalLocationDisplay)
	}
	t.Fatalf("no pen row for %q; got %v", display, names)
	return domain.FeedFollowUpPenRow{}
}

// A pen is ONE row however many sheet cells, sessions and days it has, and its
// head count is the pen's mouths -- never the sum of the cells that repeat it.
//
// This is the OneToMany proof: the sheet side fans out per session and per day,
// and the herd side fans out per animal, so both many-sides are present at once
// on one pen.
func TestFeedFollowUpOneToManyLockedDirectionRowsStayOnePenRow(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	// Three animals in Castro 1; two are sold on the same day.
	sold1 := "fd100000-0000-4000-8000-00000000a001"
	sold2 := "fd100000-0000-4000-8000-00000000a002"
	kept := "fd100000-0000-4000-8000-00000000a003"
	for _, g := range []struct{ id, tag string }{{sold1, "RF-A1"}, {sold2, "RF-A2"}, {kept, "RF-A3"}} {
		ffuSeedHerd(t, ctx, pool, g.id, fdiShedA, "1", g.tag)
	}
	ffuMarkSold(t, ctx, pool, sold1, fdiShedA, "1", "RF-A1", "2026-07-30", ffuMorning)
	ffuMarkSold(t, ctx, pool, sold2, fdiShedA, "1", "RF-A2", "2026-07-30", ffuMorning)

	persist := func(day string, heads int64, qty string) {
		t.Helper()
		// TWO sessions of the one pen: the head count repeats on both by
		// generation, so a SUM over cells would read 2x the mouths.
		cells := []domain.StoredCell{
			ffuCell(fdiShedA, "Castro", "1", 1, heads, qty, 0),
			ffuCell(fdiShedA, "Castro", "1", 2, heads, qty, 1),
		}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffu",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}
	persist("2026-07-30", 3, "3.000")
	persist("2026-07-31", 1, "1.000")
	// The deadline sheet: two days past the sale, the first that must carry it.
	persist("2026-08-01", 1, "1.000")

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("want ONE pen row however many cells/sessions/days, got %d", len(got.Rows))
	}
	row := ffuRowByPen(t, got, "Castro 1")
	if row.Sold != 2 {
		t.Errorf("sold: want 2 animals, got %d", row.Sold)
	}
	// 3 mouths across two sessions must read 3, not 6.
	if row.HeadBefore != 3 || row.HeadAfter != 1 {
		t.Errorf("mouths: want 3 -> 1 (MAX per grain, never SUM over sessions), got %d -> %d", row.HeadBefore, row.HeadAfter)
	}
	if row.Days[0].BeforeDay != "2026-07-30" || row.Days[0].AfterDay != "2026-08-01" {
		t.Errorf("readings %q -> %q, want the event day against two days later",
			row.Days[0].BeforeDay, row.Days[0].AfterDay)
	}
	if row.Status != domain.FeedFollowUpFollowed {
		t.Errorf("status: want followed (the sheet dropped from 3 to 1), got %q", row.Status)
	}
	if len(row.Days) != 1 {
		t.Fatalf("want one event day, got %d", len(row.Days))
	}
	day := row.Days[0]
	if day.Sold != 2 || len(day.Events) != 1 || day.Events[0].Animals != 2 {
		t.Errorf("one sold event of 2 animals expected, got %+v", day.Events)
	}
	if day.Events[0].TagsTotal != 2 || len(day.Events[0].Tags) != 2 {
		t.Errorf("both tags expected on the event, got %+v", day.Events[0])
	}
}

// A WORDED partition ("Part 3") must resolve to its own sheet, exactly as a
// bare-numeric one does. This is the defect the first live run hit: the stored
// partition_key is `part 3` while feed_config_norm('Part 3') is `part_3`, so a
// normalizer-keyed join found no sheet at all and the pen read as pending.
func TestFeedFollowUpMultipleDimensionsKeepWordedAndNumericPensApart(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	godel := "fd100000-0000-4000-8000-000000004003"

	numeric := "fd100000-0000-4000-8000-00000000b001"
	worded := "fd100000-0000-4000-8000-00000000b002"
	undivided := "fd100000-0000-4000-8000-00000000b003"
	ffuSeedHerd(t, ctx, pool, numeric, fdiShedA, "1", "RF-B1")
	ffuSeedHerd(t, ctx, pool, worded, godel, "Part 3", "RF-B2")
	ffuSeedHerd(t, ctx, pool, undivided, fdiShedB, "", "RF-B3")
	ffuMarkDied(t, ctx, pool, numeric, "2026-07-30", ffuMorning)
	ffuMarkDied(t, ctx, pool, worded, "2026-07-30", ffuMorning)
	ffuMarkDied(t, ctx, pool, undivided, "2026-07-30", ffuMorning)

	persist := func(day string, heads int64) {
		t.Helper()
		cells := []domain.StoredCell{
			ffuCell(fdiShedA, "Castro", "1", 1, heads, "1.000", 0),
			ffuCell(godel, "Godel 1", "Part 3", 1, heads, "2.000", 1),
			ffuCell(fdiShedB, "Yashoda", "", 1, heads, "3.000", 2),
		}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-dim-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffudim",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}
	persist("2026-07-30", 9)
	persist("2026-08-01", 8)

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	for _, display := range []string{"Castro 1", "Godel 1 - Part 3", "Yashoda"} {
		row := ffuRowByPen(t, got, display)
		if row.Died != 1 {
			t.Errorf("%s: want 1 death, got %d", display, row.Died)
		}
		// Every one of the three pen shapes must find its own sheet. A pen
		// that cannot is reported pending, which is how the worded-partition
		// defect showed itself.
		if row.HeadBefore != 9 || row.HeadAfter != 8 {
			t.Errorf("%s: want the pen's OWN sheet 9 -> 8, got %d -> %d", display, row.HeadBefore, row.HeadAfter)
		}
		if row.Status != domain.FeedFollowUpFollowed {
			t.Errorf("%s: want followed, got %q -- a pen whose sheet cannot be found reads pending", display, row.Status)
		}
	}
	if len(got.Rows) != 3 {
		t.Fatalf("want 3 pen rows kept apart, got %d", len(got.Rows))
	}
}

// The read is a WHOLE-WINDOW aggregate with no paging: every pen that had an
// event is served, and the totals range over exactly the served rows. A client
// that pages does so over the bounded set it was given.
func TestFeedFollowUpPaginationIsWholeWindowAndTotalsMatchTheServedRows(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	godel := "fd100000-0000-4000-8000-000000004003"

	a := "fd100000-0000-4000-8000-00000000c001"
	b := "fd100000-0000-4000-8000-00000000c002"
	ffuSeedHerd(t, ctx, pool, a, fdiShedA, "1", "RF-C1")
	ffuSeedHerd(t, ctx, pool, b, godel, "Part 3", "RF-C2")
	ffuMarkSold(t, ctx, pool, a, fdiShedA, "1", "RF-C1", "2026-07-30", ffuMorning)
	ffuMarkPurchased(t, ctx, pool, b, "2026-07-30", ffuMorning)

	cells := []domain.StoredCell{
		ffuCell(fdiShedA, "Castro", "1", 1, 5, "1.000", 0),
		ffuCell(godel, "Godel 1", "Part 3", 1, 7, "2.000", 1),
	}
	for _, day := range []string{"2026-07-30", "2026-07-31", "2026-08-01"} {
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-pg-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffupg",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("want every pen with an event, got %d", len(got.Rows))
	}
	sold, purchased, start, end := 0, 0, 0, 0
	for _, row := range got.Rows {
		sold += row.Sold
		purchased += row.Purchased
		start += row.HeadBefore
		end += row.HeadAfter
	}
	if got.Totals.Sold != sold || got.Totals.Purchased != purchased {
		t.Errorf("totals must range over exactly the served rows: totals sold=%d purchased=%d vs rows sold=%d purchased=%d",
			got.Totals.Sold, got.Totals.Purchased, sold, purchased)
	}
	if got.Totals.StartAnimals != start || got.Totals.EndAnimals != end {
		t.Errorf("walk ends must sum the served rows: %d/%d vs %d/%d",
			got.Totals.StartAnimals, got.Totals.EndAnimals, start, end)
	}
	// The walk does not have to close, and the remainder must be carried.
	want := end - (start + purchased - sold - got.Totals.Died)
	if got.Totals.Unexplained != want {
		t.Errorf("unexplained: want %d carried openly, got %d", want, got.Totals.Unexplained)
	}
}

// A park-scoped caller sees their park and nothing else -- the scope predicate
// binds BOTH reads, so a sale in another park cannot leak in through the herd
// side while the sheet side is filtered.
func TestFeedFollowUpParkScopeBindsBothReadsIncludingTheHerdSide(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	g := "fd100000-0000-4000-8000-00000000d001"
	ffuSeedHerd(t, ctx, pool, g, fdiShedA, "1", "RF-D1")
	ffuMarkSold(t, ctx, pool, g, fdiShedA, "1", "RF-D1", "2026-07-30", ffuMorning)

	cells := []domain.StoredCell{ffuCell(fdiShedA, "Castro", "1", 1, 4, "1.000", 0)}
	for _, day := range []string{"2026-07-30", "2026-07-31", "2026-08-01"} {
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-scope-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffuscope",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}

	// In scope: the park that owns the pen.
	inScope := ffuWindow("2026-07-30", "2026-07-31")
	inScope.ParkIDs = []uuid.UUID{uuid.MustParse(fdiPark)}
	got, err := repo.FeedFollowUp(ctx, fdiTenant, inScope)
	if err != nil {
		t.Fatalf("FeedFollowUp in scope: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0].Sold != 1 {
		t.Fatalf("own park must be visible, got %+v", got.Rows)
	}

	// Out of scope: a park the caller is not authorized for.
	outScope := ffuWindow("2026-07-30", "2026-07-31")
	outScope.ParkIDs = []uuid.UUID{uuid.MustParse("fd100000-0000-4000-8000-00000000ffff")}
	other, err := repo.FeedFollowUp(ctx, fdiTenant, outScope)
	if err != nil {
		t.Fatalf("FeedFollowUp out of scope: %v", err)
	}
	if len(other.Rows) != 0 {
		t.Fatalf("another park's sale must not leak through the herd side, got %+v", other.Rows)
	}
	if other.Totals.Sold != 0 {
		t.Errorf("totals must be scoped too, got sold=%d", other.Totals.Sold)
	}
}

// Every pen lands in exactly ONE verdict bucket, the buckets sum to the rows,
// and each bucket means what it says.
func TestFeedFollowUpStatusBucketsStayDisjointUnderTheTwoDayWindow(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())
	godel := "fd100000-0000-4000-8000-000000004003"

	ignored := "fd100000-0000-4000-8000-00000000e001" // sheet does not move -> not_followed
	acted := "fd100000-0000-4000-8000-00000000e002"   // sheet moves        -> followed
	lastDay := "fd100000-0000-4000-8000-00000000e003" // no sheet after it  -> pending
	ffuSeedHerd(t, ctx, pool, ignored, fdiShedA, "1", "RF-E1")
	ffuSeedHerd(t, ctx, pool, acted, godel, "Part 3", "RF-E2")
	ffuSeedHerd(t, ctx, pool, lastDay, fdiShedB, "", "RF-E3")
	ffuMarkDied(t, ctx, pool, ignored, "2026-07-30", ffuMorning)
	ffuMarkDied(t, ctx, pool, acted, "2026-07-30", ffuMorning)
	// AFTER the cut-off on the window's last sheet day: the 31st is already
	// packed, and the sheet that could carry it (Aug 2) does not exist.
	ffuMarkDied(t, ctx, pool, lastDay, "2026-07-31", ffuEvening)

	persist := func(day string, castroHeads, godelHeads, yashodaHeads int64) {
		t.Helper()
		cells := []domain.StoredCell{
			ffuCell(fdiShedA, "Castro", "1", 1, castroHeads, "1.000", 0),
			ffuCell(godel, "Godel 1", "Part 3", 1, godelHeads, "2.000", 1),
			ffuCell(fdiShedB, "Yashoda", "", 1, yashodaHeads, "3.000", 2),
		}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-status-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffustatus",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}
	// Castro holds its head count across the death; Godel drops by one on the
	// deadline sheet. Yashoda's death lands on the 31st, whose deadline (Aug 2)
	// has no sheet at all -- pending.
	persist("2026-07-30", 6, 6, 6)
	persist("2026-07-31", 6, 6, 6)
	persist("2026-08-01", 6, 5, 6)

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	want := map[string]string{
		"Castro 1":         domain.FeedFollowUpNotFollowed,
		"Godel 1 - Part 3": domain.FeedFollowUpFollowed,
		"Yashoda":          domain.FeedFollowUpPending,
	}
	for display, status := range want {
		if row := ffuRowByPen(t, got, display); row.Status != status {
			t.Errorf("%s: want %q, got %q", display, status, row.Status)
		}
	}
	// Disjoint and complete: the three buckets sum to the row count.
	sum := got.Totals.Followed + got.Totals.NotFollowed + got.Totals.Pending
	if sum != len(got.Rows) {
		t.Errorf("verdict buckets must partition the rows: %d+%d+%d = %d, rows = %d",
			got.Totals.Followed, got.Totals.NotFollowed, got.Totals.Pending, sum, len(got.Rows))
	}
	if got.Totals.NotFollowed != 1 || got.Totals.Followed != 1 || got.Totals.Pending != 1 {
		t.Errorf("want one pen in each bucket, got followed=%d not_followed=%d pending=%d",
			got.Totals.Followed, got.Totals.NotFollowed, got.Totals.Pending)
	}
	// The not-followed pen is the finding: it names the animal that is gone.
	castro := ffuRowByPen(t, got, "Castro 1")
	if len(castro.Days) != 1 || castro.Days[0].Unexplained != 1 {
		t.Errorf("an ignored death must read as one over-fed mouth, got %+v", castro.Days)
	}
}

// THE FARM CLOCK, end to end (maintainer correction 2026-09-23). The recorded
// instant is a batch data-entry stamp, not when the animal left -- so an
// EVENING entry must still see a cut the farm made the next morning, and the
// read must reach PAST the requested window to find the sheet that answers it.
//
// The old rule read that stamp, decided the next day's sheet was already
// packed, and compared two sheets that both already carried the change.
func TestFeedFollowUpEveningEntryStillSeesTheCutAndReachesPastTheWindow(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	g := "fd100000-0000-4000-8000-00000000f001"
	ffuSeedHerd(t, ctx, pool, g, fdiShedA, "1", "RF-F1")
	// Entered at 19:47 on the LAST day of the window the reader asks for.
	ffuMarkSold(t, ctx, pool, g, fdiShedA, "1", "RF-F1", "2026-07-31", ffuEvening)

	persist := func(day string, heads int64) {
		t.Helper()
		cells := []domain.StoredCell{ffuCell(fdiShedA, "Castro", "1", 1, heads, "1.000", 0)}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-late-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffulate",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}
	persist("2026-07-31", 5)
	persist("2026-08-01", 4) // the farm cut it the very next day
	persist("2026-08-02", 4)

	// The window ENDS on the entry day; the answering sheet is outside it.
	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	row := ffuRowByPen(t, got, "Castro 1")
	if len(row.Days) != 1 {
		t.Fatalf("want one check, got %d: %+v", len(row.Days), row.Days)
	}
	check := row.Days[0]
	if check.EventDate != "2026-07-31" {
		t.Fatalf("event date = %q, want the day the animal moved", check.EventDate)
	}
	if check.BeforeDay != "2026-07-31" || check.AfterDay != "2026-08-02" {
		t.Fatalf("readings %q -> %q, want the entry day against two days later", check.BeforeDay, check.AfterDay)
	}
	if check.HeadBefore != 5 || check.HeadAfter != 4 {
		t.Fatalf("mouths %d -> %d, want 5 -> 4", check.HeadBefore, check.HeadAfter)
	}
	if check.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- the farm cut the feed on 08-01", check.Status)
	}
	// The walk's ends stay inside the RANGE THE READER ASKED FOR, never the
	// lookahead days the verdict needed.
	if got.Totals.EndAnimals != 5 {
		t.Errorf("walk end = %d, want 5 (the 31st, the last day of the asked range)", got.Totals.EndAnimals)
	}
}

// A PEN THAT EMPTIES LEAVES THE SHEET, end to end (maintainer question,
// 2026-09-23): "if all animals in shed sold then automatically nothing will be
// packed right" -- yes, and that is the feed reducing, not a missing verdict.
//
// The pen's rows stop while the park keeps issuing sheets for its other pens,
// which is exactly the live shape of CBE Godel 1 - Part 5.
func TestFeedFollowUpEmptiedPenLeavesTheSheetAndReadsAsFedNothing(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	g := "fd100000-0000-4000-8000-00000000e101"
	ffuSeedHerd(t, ctx, pool, g, fdiShedA, "1", "RF-E101")
	ffuMarkSold(t, ctx, pool, g, fdiShedA, "1", "RF-E101", "2026-07-30", ffuEvening)

	// Day 1 and 2 carry the pen. From day 3 it is gone, but the park still
	// issues a sheet -- another shed keeps being fed.
	persist := func(day string, withPen bool) {
		t.Helper()
		cells := []domain.StoredCell{ffuCell(fdiShedB, "Yashoda", "", 1, 20, "9.000", 0)}
		if withPen {
			cells = append(cells, ffuCell(fdiShedA, "Castro", "1", 1, 4, "5.000", 1))
		}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-empty-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffuempty",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		ffuLock(t, ctx, repo, day)
	}
	persist("2026-07-30", true)
	persist("2026-07-31", true)
	persist("2026-08-01", false) // the pen is empty; nothing is packed for it

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	row := ffuRowByPen(t, got, "Castro 1")
	if len(row.Days) != 1 {
		t.Fatalf("want one check, got %d", len(row.Days))
	}
	check := row.Days[0]
	if check.Status != domain.FeedFollowUpFollowed {
		t.Fatalf("status = %q, want followed -- an empty pen is fed nothing, which IS the reduction", check.Status)
	}
	if check.AfterDay != "2026-08-01" {
		t.Fatalf("after day = %q, want the first sheet the pen is absent from", check.AfterDay)
	}
	if check.HeadAfter != 0 || check.KgAfter != "0" {
		t.Fatalf("after reading = %d head / %q kg, want nothing fed", check.HeadAfter, check.KgAfter)
	}
}

// AN UNLOCKED FEED DIRECTION IS NOT JUDGED (maintainer correction 2026-09-23).
// A day's direction is issued the morning before, amended at the correction
// time and LOCKED at the transport cutoff. Until it locks it is still being
// corrected, so neither its numbers nor a pen's ABSENCE from it are evidence.
//
// This is the live shape that produced a wrong answer: CPT's direction for
// 23 Sep sat at `issued` carrying 25 pens against 42 the day before, and
// reading a pen's absence from it reported "fed nothing" for a pen holding 16
// animals.
func TestFeedFollowUpUnlockedDirectionIsNotJudged(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupIssueDB(t, ctx)
	issuedAt := time.Date(2026, 7, 29, 9, 0, 0, 0, biztime.DefaultLocation())

	g := "fd100000-0000-4000-8000-00000000e201"
	ffuSeedHerd(t, ctx, pool, g, fdiShedA, "1", "RF-E201")
	ffuMarkSold(t, ctx, pool, g, fdiShedA, "1", "RF-E201", "2026-07-30", ffuEvening)

	write := func(day string, heads int64, lock bool) {
		t.Helper()
		cells := []domain.StoredCell{ffuCell(fdiShedA, "Castro", "1", 1, heads, "5.000", 0)}
		if _, err := repo.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: fdiTenant, ParkID: fdiPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: issuedAt, Fingerprint: "fp-ffu-unlocked-" + day,
			IdempotencyKey: "issue:" + fdiTenant + ":" + fdiPark + ":" + day + ":ffuunlocked",
			GeneratedBy:    "test", Cells: cells,
		}); err != nil {
			t.Fatalf("persist %s: %v", day, err)
		}
		if lock {
			ffuLock(t, ctx, repo, day)
		}
	}
	write("2026-07-30", 4, true)
	// The deadline day exists but is STILL BEING CORRECTED.
	write("2026-08-01", 4, false)

	got, err := repo.FeedFollowUp(ctx, fdiTenant, ffuWindow("2026-07-30", "2026-07-31"))
	if err != nil {
		t.Fatalf("FeedFollowUp: %v", err)
	}
	row := ffuRowByPen(t, got, "Castro 1")
	check := row.Days[0]
	if check.Status != domain.FeedFollowUpPending {
		t.Fatalf("status = %q, want pending -- an unlocked direction is not final", check.Status)
	}
	if check.AfterDay != "" || check.KgAfter != "" {
		t.Fatalf("no reading may be taken from an unlocked direction: day=%q kg=%q", check.AfterDay, check.KgAfter)
	}
}
