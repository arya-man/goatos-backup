package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Sale-allocation regression suite: DB ROUND TRIPS against the production path, never
// field-presence or pure-Go formatter checks. Both weaker forms have passed on this
// repository while real output was wrong.

const saleDealA = "77777777-7777-4777-8777-777777777771"
const saleDealB = "77777777-7777-4777-8777-777777777772"

func saleAllocCmd(dealID string, rows []ports.SaleAllocationRow, key string) ports.RecordSaleAllocationsCommand {
	return ports.RecordSaleAllocationsCommand{
		TenantID:             ssTenant,
		ActorID:              ssActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: ssTenant + ":sale_allocation:" + dealID + ":" + key,
		RequestHash:          "hash-" + key,
		SalesDealID:          dealID,
		DeclaredAnimalCount:  len(rows),
		Rows:                 rows,
		Reason:               "Sold to buyer",
		OccurredAt:           time.Date(2026, 8, 20, 6, 0, 0, 0, time.UTC),
	}
}

func goatLifecycle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT lifecycle_status FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		ssTenant, goatID).Scan(&status); err != nil {
		t.Fatalf("read lifecycle: %v", err)
	}
	return status
}

func seedSaleAllocationDeal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dealID string, animals int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sales_deals (id, tenant_id, sale_date, farm, buyer_name, product_type, breed, animal_count, sales_value, status)
VALUES ($1::uuid, $2::uuid, '2026-08-20'::date, 'CBE', 'Synthetic buyer', 'Goat', 'Boer', $3, 1000, 'Deal Closed')
ON CONFLICT (id) DO UPDATE
SET animal_count = EXCLUDED.animal_count,
    male_count = NULL,
    female_count = NULL`, dealID, ssTenant, animals); err != nil {
		t.Fatalf("seed sale deal: %v", err)
	}
}

// THE WHOLE POINT OF THE FEATURE, proved end to end on the real write path: confirming a
// sale must BOTH record which animals it is made of AND take those animals out of the
// herd. A version that did one and not the other would look fine on the screen that
// checks its own half.
func TestConfirmingASaleRecordsTheAnimalsAndMarksThemSold(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "2", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 2)
	if _, err := pool.Exec(ctx, `
UPDATE goats SET sex = 'male'
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, ssTenant, one); err != nil {
		t.Fatalf("seed male sale animal: %v", err)
	}

	result, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two)},
	}, "confirm-a"))
	if err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}
	if result.Allocated != 2 {
		t.Fatalf("allocated = %d, want 2", result.Allocated)
	}

	// Half one: the herd knows they are gone, through the canonical exit.
	for _, id := range []string{one, two} {
		if got := goatLifecycle(t, ctx, pool, id); got != "sold" {
			t.Fatalf("goat %s lifecycle = %q, want sold", id, got)
		}
	}
	// Half two: the sale knows which animals it is made of, with its own snapshot.
	groups, err := repo.ListSaleAllocations(ctx, ssTenant, saleDealA)
	if err != nil {
		t.Fatalf("ListSaleAllocations: %v", err)
	}
	total := 0
	for _, g := range groups {
		total += g.Animals
	}
	if total != 2 {
		t.Fatalf("read back %d animals across %d group(s), want 2", total, len(groups))
	}
	// Two pens of one shed are two gather-list rows: an operator collects them pen by pen.
	if len(groups) != 2 {
		t.Fatalf("want two pen groups, got %+v", groups)
	}
	// The display is the canonical composition, so a numeric pen joins with a space.
	if groups[0].OperationalLocationDisplay != "Castro 1" {
		t.Fatalf("group display = %q, want %q", groups[0].OperationalLocationDisplay, "Castro 1")
	}
	var maleCount, femaleCount float64
	if err := pool.QueryRow(ctx, `
SELECT male_count, female_count
FROM sales_deals
WHERE tenant_id = $1::uuid AND id = $2::uuid`, ssTenant, saleDealA).Scan(&maleCount, &femaleCount); err != nil {
		t.Fatalf("read sale sex counts: %v", err)
	}
	if maleCount != 1 || femaleCount != 1 {
		t.Fatalf("sale sex counts = male %.0f female %.0f, want 1/1 from the tagged animals", maleCount, femaleCount)
	}

	// And the canonical exit fired its event, which is what cancels the animals' open
	// vaccination obligations. A hand-written bulk UPDATE would have skipped this and left
	// sold animals with live work scheduled against them.
	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'goat.exited'
  AND aggregate_id::text = ANY($2::text[])`, ssTenant, []string{one, two}).Scan(&events); err != nil {
		t.Fatalf("count exit events: %v", err)
	}
	if events != 2 {
		t.Fatalf("goat.exited events = %d, want one per animal", events)
	}
}

// STATUS MATRIX. goat_sale_allocations carries 'tagged' and 'released', and every read
// must place each row in exactly one bucket. A released tagging is HISTORY: it must
// disappear from the sale's gather list, and it must stop blocking the animal from being
// tagged to a later, corrected sale.
func TestSaleAllocationStatusMatrixPlacesEveryRowOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	kept := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	released := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 2)

	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: kept, RowVersion: goatRowVersion(t, pool, kept)},
		{GoatID: released, RowVersion: goatRowVersion(t, pool, released)},
	}, "confirm-matrix")); err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}

	if _, err := pool.Exec(ctx, `
UPDATE goat_sale_allocations
SET status='released', released_at=now(), released_by=$3::uuid, release_reason='corrected'
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, ssTenant, released, ssActor); err != nil {
		t.Fatalf("release allocation: %v", err)
	}

	groups, err := repo.ListSaleAllocations(ctx, ssTenant, saleDealA)
	if err != nil {
		t.Fatalf("ListSaleAllocations: %v", err)
	}
	total := 0
	for _, g := range groups {
		total += g.Animals
	}
	if total != 1 {
		t.Fatalf("a released tagging must leave the gather list: %d animal(s) still listed", total)
	}

	// The live-goat partial unique index must ignore the released row, or a corrected sale
	// could never be recorded for that animal.
	var live int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM goat_sale_allocations
WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND status='tagged'`, ssTenant, released).Scan(&live); err != nil {
		t.Fatalf("count live allocations: %v", err)
	}
	if live != 0 {
		t.Fatalf("released animal still holds %d live tagging(s)", live)
	}
}

// EXACT REPLAY. A retried confirmation must return the original result without exiting
// the animals a second time -- the second exit would fail on the stale row_version and
// surface as an error on a request the caller is entitled to retry.
func TestConfirmingTheSameSaleTwiceIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	rows := []ports.SaleAllocationRow{{GoatID: goat, RowVersion: goatRowVersion(t, pool, goat)}}
	seedSaleAllocationDeal(t, ctx, pool, saleDealB, 1)

	first, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealB, rows, "replay-key"))
	if err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	second, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealB, rows, "replay-key"))
	if err != nil {
		t.Fatalf("replay must succeed, got %v", err)
	}
	if first.Allocated != second.Allocated {
		t.Fatalf("replay changed the answer: %d then %d", first.Allocated, second.Allocated)
	}

	var allocations int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM goat_sale_allocations
WHERE tenant_id=$1::uuid AND sales_deal_id=$2::uuid`, ssTenant, saleDealB).Scan(&allocations); err != nil {
		t.Fatalf("count allocations: %v", err)
	}
	if allocations != 1 {
		t.Fatalf("replay wrote a second allocation row: %d", allocations)
	}
}

// NO-CLOBBER. The picker captures a row_version; if the animal changed between the pick
// and the confirm -- moved, treated, quarantined by someone else -- the confirm must
// refuse rather than sell it anyway.
func TestAStaleRowVersionRefusesTheConfirm(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	stale := goatRowVersion(t, pool, goat) - 1

	if _, err := repo.RecordSaleAllocations(ctx, saleAllocCmd(saleDealA,
		[]ports.SaleAllocationRow{{GoatID: goat, RowVersion: stale}}, "stale-key")); err == nil {
		t.Fatal("a stale row_version must refuse the confirm")
	}
	if got := goatLifecycle(t, ctx, pool, goat); got != "alive" {
		t.Fatalf("the animal must be untouched after a refused confirm, got %q", got)
	}
	var allocations int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM goat_sale_allocations WHERE tenant_id=$1::uuid`, ssTenant).Scan(&allocations); err != nil {
		t.Fatalf("count allocations: %v", err)
	}
	if allocations != 0 {
		t.Fatalf("a refused confirm must write nothing, found %d allocation(s)", allocations)
	}
}

// THE PICKER'S GATE INPUTS come back from the read the app layer judges. A quarantined
// animal must arrive carrying the quarantine, or the gate has nothing to refuse on.
func TestSaleCandidateReadCarriesTheGateInputs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	healthy := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	quarantined := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET lifecycle_status='quarantine' WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		ssTenant, quarantined); err != nil {
		t.Fatalf("quarantine goat: %v", err)
	}

	rows, err := repo.ReadSaleCandidateRows(ctx, ssTenant, "", []string{healthy, quarantined})
	if err != nil {
		t.Fatalf("ReadSaleCandidateRows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want both animals, got %d", len(rows))
	}
	if rows[quarantined].State.LifecycleStatus != "quarantine" {
		t.Fatalf("quarantine did not reach the gate: %+v", rows[quarantined].State)
	}
	if rows[healthy].State.LifecycleStatus != "alive" {
		t.Fatalf("healthy animal state = %+v", rows[healthy].State)
	}
	// The pen must travel with the animal, or the gather list cannot name where to go.
	if rows[healthy].PartitionLabel != "1" || rows[healthy].ShedName != "Castro" {
		t.Fatalf("location did not reach the picker: %+v", rows[healthy])
	}
}

// THE PICKER'S PAGED LIST, exercised with EVERY filter absent -- which is exactly how the
// drawer first opens, and the case that shipped broken.
//
// The bug this pins: `$n::text = ” OR g.goat_id > $n::uuid` reads as short-circuiting,
// and does not. Postgres evaluates the cast of the constant regardless, so an empty shed
// filter or an empty cursor failed the whole query with `invalid input syntax for type
// uuid: ""` -- a 500 on the very first call. It survived review and the by-ids tests
// because nothing exercised THIS query with blank optional filters.
func TestListSaleCandidatesWorksWithEveryOptionalFilterAbsent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	// The baseline tenant already holds animals in this park, so every assertion below
	// looks for THIS animal by id rather than trusting a position in the list.
	mine := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")

	find := func(rows []ports.SaleCandidateRow) *ports.SaleCandidateRow {
		for i := range rows {
			if rows[i].GoatID == mine {
				return &rows[i]
			}
		}
		return nil
	}

	// No shed, no pens, no query, no cursor: the drawer's opening state.
	rows, _, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
		TenantID: ssTenant, ParkID: ssPark, IncludeBlocked: true, Limit: 50,
	})
	if err != nil {
		t.Fatalf("the picker must open with no filters set: %v", err)
	}
	row := find(rows)
	if row == nil {
		t.Fatalf("the seeded animal is missing from %d unfiltered row(s)", len(rows))
	}
	if row.ShedName != "Castro" || row.PartitionLabel != "1" {
		t.Fatalf("location did not travel with the row: %+v", row)
	}

	// And with each optional filter supplied, so the fix cannot be "always ignore them".
	shedOnly, _, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
		TenantID: ssTenant, ParkID: ssPark, ShedID: f.castroShed, IncludeBlocked: true, Limit: 10,
	})
	if err != nil {
		t.Fatalf("shed filter: %v", err)
	}
	if find(shedOnly) == nil {
		t.Fatal("the shed filter dropped its own shed's animal")
	}
	penOnly, _, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
		TenantID: ssTenant, ParkID: ssPark, ShedID: f.castroShed,
		PartitionLabels: []string{"1"}, IncludeBlocked: true, Limit: 10,
	})
	if err != nil {
		t.Fatalf("pen filter: %v", err)
	}
	if find(penOnly) == nil {
		t.Fatal("the pen filter dropped its own pen's animal")
	}
	// A pen the animal is not in must exclude it, or the filter is decorative.
	otherPen, _, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
		TenantID: ssTenant, ParkID: ssPark, ShedID: f.castroShed,
		PartitionLabels: []string{"2"}, IncludeBlocked: true, Limit: 10,
	})
	if err != nil {
		t.Fatalf("other pen filter: %v", err)
	}
	if find(otherPen) != nil {
		t.Fatal("pen 2 returned pen 1's animal")
	}
}

// KEYSET PAGING: a cursor must advance and never repeat or skip a row.
func TestSaleCandidatePagingAdvancesWithoutRepeatingARow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	// The baseline park already holds animals, so the assertion is that MINE all appear
	// exactly once -- not that the park contains only what this test seeded.
	mine := map[string]bool{}
	for range 5 {
		mine[seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")] = true
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 40; page++ {
		rows, next, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
			TenantID: ssTenant, ParkID: ssPark, IncludeBlocked: true, Limit: 2, Cursor: cursor,
		})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, row := range rows {
			if seen[row.GoatID] {
				t.Fatalf("animal %s came back on two pages", row.GoatID)
			}
			seen[row.GoatID] = true
		}
		if next == nil {
			break
		}
		cursor = *next
	}
	for id := range mine {
		if !seen[id] {
			t.Fatalf("animal %s was skipped by the keyset paging", id)
		}
	}
}

// THE EMPTY-PICKER BUG, pinned. The farm's pens exist twice in `locations`: the canonical
// shed plus its pen catalog, AND old rows literally named "Castro 1"/"Castro 2" that are
// still active and still location_type='shed'. Those alias rows hold NO animals and NO
// catalogued pens, so offering them in the shed dropdown gave an operator a choice that
// could only ever return an empty list -- which reads as a broken screen, not as a wrong
// pick. Reported from the running app.
func TestSaleLocationPickerHidesLegacyAliasShedsAndKeepsRealPens(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	// The canonical shed's pen catalog. An EMPTY pen is catalogued deliberately: a write
	// picker answers "where may animals be", so a pen holding nobody today must stay
	// selectable as a destination for this sale's animals.
	for _, label := range []string{"1", "2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $3, 'active', 'manual')
ON CONFLICT DO NOTHING`, ssTenant, f.castroShed, label); err != nil {
			t.Fatalf("seed pen %s: %v", label, err)
		}
	}
	// The legacy alias row: same park, named for the pen, no catalog of its own.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, 'shed', 'Castro 1', $2::uuid, 'active')`, ssTenant, ssPark); err != nil {
		t.Fatalf("seed alias shed: %v", err)
	}

	catalog, err := repo.ListSaleLocations(ctx, ssTenant)
	if err != nil {
		t.Fatalf("ListSaleLocations: %v", err)
	}

	// PEN-WISE. The dropdown offers "Castro 1" and "Castro - Part 2" -- the places people
	// stand -- and NOT a bare "Castro", which would make the operator work out which of
	// its pens they meant.
	labels := map[string]bool{}
	castroPens := 0
	for _, loc := range catalog.Locations {
		if labels[loc.OperationalLocationDisplay] {
			t.Fatalf("location %q listed twice: %+v", loc.OperationalLocationDisplay, catalog.Locations)
		}
		labels[loc.OperationalLocationDisplay] = true
		if loc.ShedID == f.castroShed {
			castroPens++
			if loc.PartitionLabel == "" {
				t.Fatalf("a subdivided shed must not offer its bare parent: %+v", loc)
			}
		}
	}
	// The legacy alias shed is gone entirely.
	for label := range labels {
		if label == "Castro 1" && castroPens == 0 {
			t.Fatalf("the legacy alias shed is still offered: %v", catalog.Locations)
		}
	}
	if castroPens != 2 {
		t.Fatalf("Castro must contribute one entry per catalogued pen, got %d: %+v", castroPens, catalog.Locations)
	}
	// HUMAN labels composed by the canonical helper: a numeric pen joins with a space,
	// a worded one with a dash. The fixture catalogues "Part 2", and the "2" seeded above
	// collides with it on normalized_label -- correctly, because they are ONE pen -- so
	// the scrubbed key must never appear as a separate entry (rule 5a).
	if !labels["Castro 1"] {
		t.Fatalf("numeric pen must render with a space: %v", catalog.Locations)
	}
	if !labels["Castro - Part 2"] {
		t.Fatalf("worded pen must render with a dash: %v", catalog.Locations)
	}
	if labels["Castro 2"] {
		t.Fatalf("the picker leaked the normalized matching key as its own entry: %v", catalog.Locations)
	}
	if len(catalog.Parks) == 0 {
		t.Fatal("the picker needs at least one park")
	}
}

// SUBSTRING SEARCH. An operator reading a tag off an animal works from whatever part of
// the number is legible -- the middle three digits, the last four -- so a prefix-only
// match finds nothing and reads as a broken search box.
//
// The identifier side searches EVERY active identifier the animal holds, not just the one
// the picker displays: an animal is routinely found by an older tag that is still on it.
func TestFindATagMatchesAnywhereInTheIdentifier(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")

	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value,
	                              normalized_value, scope_key, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', '901007000504830', '901007000504830', 'tenant', 'active', now(), 'v1'),
       ($1::uuid, $2::uuid, 'animal_identifier_2', '901007000505274', '901007000505274', 'tenant', 'active', now(), 'v1')`,
		ssTenant, goat); err != nil {
		t.Fatalf("seed rfids: %v", err)
	}

	find := func(q string) []ports.SaleCandidateRow {
		rows, _, err := repo.ListSaleCandidates(ctx, ports.ListSaleCandidatesParams{
			TenantID: ssTenant, ParkID: ssPark, Query: q, IncludeBlocked: true, Limit: 50,
		})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		var hits []ports.SaleCandidateRow
		for _, row := range rows {
			if row.GoatID == goat {
				hits = append(hits, row)
			}
		}
		return hits
	}

	for _, q := range []string{
		"901007000504830", // the whole tag
		"9010",            // the first digits, which prefix search already handled
		"0700050",         // the MIDDLE, which it did not
		"4830",            // the LAST FOUR, the way a worn tag is usually read
		"5274",            // the secondary tag must find the same animal too
	} {
		if len(find(q)) != 1 {
			t.Fatalf("searching %q must find the animal", q)
		}
	}
	// The displayed row must name both tags. Search already matched both before this fix;
	// the regression was that a secondary-tag search returned a row showing only the
	// primary, which made the result look like the wrong animal.
	bySecondary := find("5274")
	if len(bySecondary) != 1 {
		t.Fatalf("secondary-tag search returned %d rows, want 1", len(bySecondary))
	}
	if bySecondary[0].TagNumber != "901007000504830" || bySecondary[0].SecondaryTagNumber != "901007000505274" {
		t.Fatalf("candidate tags = (%q, %q), want both active identifiers",
			bySecondary[0].TagNumber, bySecondary[0].SecondaryTagNumber)
	}
	// A string the animal genuinely does not carry must NOT match, or the search is
	// matching everything and the operator cannot trust it.
	if len(find("777777")) != 0 {
		t.Fatal("search matched an identifier the animal does not carry")
	}
}

// THE FEED DIRECTOR'S EVENT, proved on the real confirm path: ONE goat.sale_allocated per confirm
// (not one per animal), committed in the same transaction as the exits, whose payload already
// carries the pen-by-pen breakdown with the canonical pen display -- and the batch reader the
// feed-day reminder uses groups the same rows the same way, bounded by its trailing window.
func TestConfirmingASaleEmitsOneEventWithOneToManyPenGroupsAndTheBatchReaderMatchesIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	three := seedStageGoat(t, ctx, pool, f.castroShed, "2", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 3)

	cmd := saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two)},
		{GoatID: three, RowVersion: goatRowVersion(t, pool, three)},
	}, "confirm-feed")
	if _, err := repo.RecordSaleAllocations(ctx, cmd); err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}

	// One event per CONFIRM. Three animals, three goat.exited, exactly one goat.sale_allocated.
	var payloadRaw []byte
	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int, min(payload::text)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'goat.sale_allocated' AND aggregate_id = $2::uuid`,
		ssTenant, saleDealA).Scan(&events, &payloadRaw); err != nil {
		t.Fatalf("count sale events: %v", err)
	}
	if events != 1 {
		t.Fatalf("goat.sale_allocated events = %d, want exactly one per confirm", events)
	}
	var envelope struct {
		AggregateType   string `json:"aggregate_type"`
		SubjectType     string `json:"subject_type"`
		IdempotencyKey  string `json:"idempotency_key"`
		TraceID         string `json:"trace_id"`
		VisibilityScope struct {
			TenantID string  `json:"tenant_id"`
			ParkID   *string `json:"park_id"`
		} `json:"visibility_scope"`
		Payload struct {
			SalesDealID string `json:"sales_deal_id"`
			AllocatedAt string `json:"allocated_at"`
			Animals     int    `json:"animals"`
			Pens        []struct {
				Display  string `json:"operational_location_display"`
				ParkName string `json:"park_name"`
				Animals  int    `json:"animals"`
			} `json:"pens"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payloadRaw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.AggregateType != "sales_deal" || envelope.SubjectType != "sales_deal" {
		t.Fatalf("aggregate/subject = %q/%q, want sales_deal", envelope.AggregateType, envelope.SubjectType)
	}
	if envelope.VisibilityScope.TenantID != ssTenant || envelope.VisibilityScope.ParkID == nil {
		t.Fatalf("visibility scope = %+v, want tenant and the single park", envelope.VisibilityScope)
	}
	if envelope.TraceID == "" {
		t.Fatal("trace_id must never be blank: the relay rejects the envelope and the push silently never fires")
	}
	if envelope.IdempotencyKey != saleAllocatedIdempotencyKey(saleDealA, cmd.OccurredAt) {
		t.Fatalf("idempotency key = %q", envelope.IdempotencyKey)
	}
	p := envelope.Payload
	if p.SalesDealID != saleDealA || p.Animals != 3 || len(p.Pens) != 2 {
		t.Fatalf("payload = %+v, want deal %s, 3 animals across 2 pens", p, saleDealA)
	}
	// Pen display is the canonical composition (space for a numeric pen), and the counts are
	// ANIMALS per pen: two in Castro 1, one in Castro 2.
	if p.Pens[0].Display != "Castro 1" || p.Pens[0].Animals != 2 || p.Pens[1].Display != "Castro 2" || p.Pens[1].Animals != 1 {
		t.Fatalf("pens = %+v", p.Pens)
	}
	if p.Pens[0].ParkName == "" {
		t.Fatal("park_name must be resolved in the payload; the push has no lookup at consume time")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.AllocatedAt); err != nil {
		t.Fatalf("allocated_at %q: %v", p.AllocatedAt, err)
	}

	// The reminder's read groups the same rows the same way...
	batches, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, cmd.OccurredAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches: %v", err)
	}
	if len(batches) != 1 || batches[0].SalesDealID != saleDealA || batches[0].Animals != 3 || len(batches[0].Pens) != 2 {
		t.Fatalf("batches = %+v", batches)
	}
	if batches[0].Pens[0].OperationalLocationDisplay != "Castro 1" || batches[0].Pens[0].Animals != 2 {
		t.Fatalf("batch pens = %+v", batches[0].Pens)
	}
	// ...keyed on the SAME instant the event was keyed on, so the reminder key derived from the
	// stored allocated_at equals the notice's batch identity.
	if saleAllocatedIdempotencyKey(saleDealA, batches[0].AllocatedAt) != envelope.IdempotencyKey {
		t.Fatalf("stored allocated_at %s does not reproduce the event key %q", batches[0].AllocatedAt, envelope.IdempotencyKey)
	}
}

// STATUS MATRIX for the batch reader: goat_sale_allocations carries 'tagged' and 'released', and a
// released tagging is HISTORY -- it must leave the batch's animal count and drop its pen when the
// pen is empty, exactly as the gather list already does. A reader that counted every row would tell
// the Feed Director to reduce feed for an animal that is still standing in the pen.
func TestSaleAllocationBatchReaderStatusMatrixExcludesReleasedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.castroShed, "2", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 2)
	cmd := saleAllocCmd(saleDealA, []ports.SaleAllocationRow{
		{GoatID: one, RowVersion: goatRowVersion(t, pool, one)},
		{GoatID: two, RowVersion: goatRowVersion(t, pool, two)},
	}, "confirm-status")
	if _, err := repo.RecordSaleAllocations(ctx, cmd); err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}
	// Release the Castro 2 tagging the way the release path does: status + released_at, row kept.
	if _, err := pool.Exec(ctx, `
UPDATE goat_sale_allocations
SET status = 'released', released_at = now(), release_reason = 'test'
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, ssTenant, two); err != nil {
		t.Fatalf("release allocation: %v", err)
	}

	batches, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, cmd.OccurredAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches: %v", err)
	}
	if len(batches) != 1 || batches[0].Animals != 1 || len(batches[0].Pens) != 1 {
		t.Fatalf("batches = %+v, want one batch of 1 animal in 1 pen after the release", batches)
	}
	if batches[0].Pens[0].OperationalLocationDisplay != "Castro 1" || batches[0].Pens[0].Animals != 1 {
		t.Fatalf("pen = %+v, want Castro 1 with 1 animal", batches[0].Pens[0])
	}
}

// PARK SCOPE: every pen of a batch carries the park the animal stood in when it was TAGGED -- the
// snapshot on the allocation row, not the goat's current row (which the exit has already rewritten)
// -- and the event's visibility scope names that same park. The reminder reads the park's clock
// off this id, so a blank or wrong park here silently moves the feed day.
func TestSaleAllocationBatchReaderParkScopeIsTheSnapshotPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	var parkID string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(park_id::text, '') FROM goats WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		ssTenant, one).Scan(&parkID); err != nil {
		t.Fatalf("read goat park: %v", err)
	}
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 1)
	cmd := saleAllocCmd(saleDealA, []ports.SaleAllocationRow{{GoatID: one, RowVersion: goatRowVersion(t, pool, one)}}, "confirm-park")
	if _, err := repo.RecordSaleAllocations(ctx, cmd); err != nil {
		t.Fatalf("RecordSaleAllocations: %v", err)
	}

	batches, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, cmd.OccurredAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches: %v", err)
	}
	if len(batches) != 1 || len(batches[0].Pens) != 1 {
		t.Fatalf("batches = %+v", batches)
	}
	var snapshotPark string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(park_id::text, '') FROM goat_sale_allocations WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		ssTenant, one).Scan(&snapshotPark); err != nil {
		t.Fatalf("read allocation park: %v", err)
	}
	if snapshotPark != parkID || batches[0].Pens[0].ParkID != snapshotPark {
		t.Fatalf("pen park = %q, allocation snapshot = %q, goat park = %q: all three must agree", batches[0].Pens[0].ParkID, snapshotPark, parkID)
	}
	var scopePark string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(payload->'visibility_scope'->>'park_id', '')
FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'goat.sale_allocated' AND aggregate_id = $2::uuid`,
		ssTenant, saleDealA).Scan(&scopePark); err != nil {
		t.Fatalf("read event scope: %v", err)
	}
	if scopePark != parkID {
		t.Fatalf("event visibility_scope.park_id = %q, want the snapshot park %q", scopePark, parkID)
	}
}

// The reminder's read is a TRAILING WINDOW, not a paged list: `since` is a hard lower bound and the
// batches come back oldest first, one per confirm, so a second confirm of a different sale is a
// second batch and a since past the first confirm returns only the second. No cursor exists to
// advance, and none is needed -- the window is bounded by time and served by the 000274 index.
func TestSaleAllocationBatchReaderTrailingWindowNeedsNoPagination(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)

	one := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2", "adult")
	two := seedStageGoat(t, ctx, pool, f.yashodaShed, "", "F2", "adult")
	seedSaleAllocationDeal(t, ctx, pool, saleDealA, 1)
	seedSaleAllocationDeal(t, ctx, pool, saleDealB, 1)
	first := saleAllocCmd(saleDealA, []ports.SaleAllocationRow{{GoatID: one, RowVersion: goatRowVersion(t, pool, one)}}, "confirm-w1")
	if _, err := repo.RecordSaleAllocations(ctx, first); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	second := saleAllocCmd(saleDealB, []ports.SaleAllocationRow{{GoatID: two, RowVersion: goatRowVersion(t, pool, two)}}, "confirm-w2")
	second.OccurredAt = first.OccurredAt.Add(2 * time.Hour)
	if _, err := repo.RecordSaleAllocations(ctx, second); err != nil {
		t.Fatalf("second confirm: %v", err)
	}

	both, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, first.OccurredAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches(both): %v", err)
	}
	if len(both) != 2 || both[0].SalesDealID != saleDealA || both[1].SalesDealID != saleDealB {
		t.Fatalf("both = %+v, want deal A then deal B oldest first", both)
	}
	if !both[1].AllocatedAt.After(both[0].AllocatedAt) {
		t.Fatalf("batches must come back oldest first: %v then %v", both[0].AllocatedAt, both[1].AllocatedAt)
	}
	onlySecond, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, first.OccurredAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches(later): %v", err)
	}
	if len(onlySecond) != 1 || onlySecond[0].SalesDealID != saleDealB {
		t.Fatalf("a since after the first confirm must read only the second, got %+v", onlySecond)
	}
	none, err := repo.ListRecentSaleAllocationBatches(ctx, ssTenant, second.OccurredAt.Add(time.Second))
	if err != nil {
		t.Fatalf("ListRecentSaleAllocationBatches(none): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("a since after every confirm must read nothing, got %+v", none)
	}
}
