package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/feedconfig/domain"
	"github.com/vgoats/goatos/backend/internal/feedconfig/ports"
)

// Pen-grain proofs for the experiment write paths, against the REAL schema.
//
// The fixture in repository_integration_test.go uses UNDIVIDED sheds, so every pen predicate in it
// is trivially satisfied: one shed, one 'whole' pen, and shed-scope and pen-scope are the same set.
// Every defect below needs a shed with MORE THAN ONE pen to show itself, which is why these live in
// their own file with their own fixture.
//
// What only a database can prove here, and what a fake or a pure-Go formatter test cannot:
//
//	GENERATED-COLUMN AGREEMENT -- partition_key is computed by Postgres. A Go twin of that
//	                     expression can disagree with it, and the disagreement is invisible until a
//	                     real row exists to miss.
//	CATALOG VALIDATION -- shed_partitions is the pen catalog. Whether a label resolves against it is
//	                     a join, not a string check.
//	BLAST RADIUS     -- "this write touched ONE pen" can only be shown by reading the SIBLING pens
//	                     afterwards and finding them unchanged.

const (
	// A SUBDIVIDED shed: two catalogued pens whose labels carry a separator, which is the case the
	// drifted key twin got wrong.
	fcPennedShed = "00000000-0000-4000-8000-000000004004"
	fcPenA       = "Part 3"
	fcPenB       = "Part 4"
)

// seedPennedShed adds a subdivided shed to the fixture park and catalogs its two pens.
func seedPennedShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	// A name the migration baseline does not already use. The baseline ships the real farm's
	// locations, INCLUDING the legacy alias rows named 'Godel 1 - Part 3' -- separate shed rows that
	// duplicate a (shed + pen) location. Naming the fixture shed 'Godel 1' put two rows on the same
	// operational-location display and made this test assert against whichever won the map, which is
	// a fixture defect, not a product one. See the note on the undivided-shed assertion below for
	// the real-environment hazard those alias rows carry.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($3::uuid, $1::uuid, 'shed', 'CPT-S4', 'Kepler 7', $2::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, fcTenant, fcPark, fcPennedShed); err != nil {
		t.Fatalf("seed penned shed: %v", err)
	}
	// normalized_label carries the CATALOG's normalization, which strips a leading "part " -- a
	// different vocabulary from feed_experiment_config.partition_key (feed_config_norm, giving
	// 'part_3'). Seeding it the catalog's way is what makes this fixture able to catch a validation
	// that compares across the two by mistake.
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source)
VALUES
  ($1::uuid, $2::uuid, $3, regexp_replace(lower(btrim($3)), '^part[[:space:]]+', ''), 'manual'),
  ($1::uuid, $2::uuid, $4, regexp_replace(lower(btrim($4)), '^part[[:space:]]+', ''), 'manual')
ON CONFLICT DO NOTHING`, fcTenant, fcPennedShed, fcPenA, fcPenB); err != nil {
		t.Fatalf("seed shed partitions: %v", err)
	}
}

func setupPennedDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool := setupFeedConfigDB(t, ctx)
	seedPennedShed(t, ctx, pool)
	return pool
}

// penCommand authors one cell of one pen of the subdivided fixture shed.
func penCommand(key, pen, item, kg string) domain.UpsertExperimentConfigCommand {
	return domain.UpsertExperimentConfigCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
			IdempotencyKey: key, RequestFingerprint: "fp-" + key,
		},
		ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: pen,
		FeedItemLabel: item, AbsoluteKg: kg, ExperimentCategory: "Arm A",
	}
}

// penCells reads (feed item -> kg) for one pen, keyed the way the DATABASE keys it.
func penCells(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pen string) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT feed_item_label, absolute_kg::text
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid
  AND partition_key = CASE WHEN btrim($3) = '' THEN 'whole' ELSE feed_config_norm($3) END`,
		fcTenant, fcPennedShed, pen)
	if err != nil {
		t.Fatalf("read pen cells: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var item, kg string
		if err := rows.Scan(&item, &kg); err != nil {
			t.Fatalf("scan pen cell: %v", err)
		}
		out[item] = kg
	}
	return out
}

// penStatuses reads (feed item -> status) for one pen.
func penStatuses(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pen string) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT feed_item_label, status
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid
  AND partition_key = CASE WHEN btrim($3) = '' THEN 'whole' ELSE feed_config_norm($3) END`,
		fcTenant, fcPennedShed, pen)
	if err != nil {
		t.Fatalf("read pen statuses: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var item, status string
		if err := rows.Scan(&item, &status); err != nil {
			t.Fatalf("scan pen status: %v", err)
		}
		out[item] = status
	}
	return out
}

// TestEditingACellOfAPenWhoseLabelHasASeparator is the GOAT-FEED-006 regression, and it is the
// reason a Go twin of a generated column is banned here.
//
// partition_key is GENERATED as feed_config_norm(partition_label), which collapses runs of
// whitespace/underscore/hyphen to '_'. The Go helper that claimed to mirror it returned
// lower(btrim(label)) instead. For a pen labelled with a separator the two disagree --
//
//	Go twin       'Part 3' -> 'part 3'
//	the column    'Part 3' -> 'part_3'
//
// -- so the write path's FOR UPDATE lookup could never match an existing cell of such a pen. It fell
// through to the insert branch, which has no ON CONFLICT, and died on the natural-key unique index.
// Editing an authored kg was a 500 for 130 of the 175 cells in the live data; purely numeric pens
// ('2', '3') agreed under both spellings, which is how it survived review.
//
// Asserting the OUTPUT of a real round trip, not the helper: a unit test on the formatter passed
// while this was broken, because the formatter was never the disagreeing side on its own.
func TestEditingACellOfAPenWhoseLabelHasASeparator(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	first, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-sep-insert", fcPenA, "Concentrate", "1.500"))
	if err != nil {
		t.Fatalf("insert cell on a separator-bearing pen: %v", err)
	}
	if first.Outcome != domain.OutcomeInserted {
		t.Fatalf("first write outcome = %q, want %q", first.Outcome, domain.OutcomeInserted)
	}

	// THE FAILING STEP before the fix: a unique-violation error, not a correction.
	second, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-sep-edit", fcPenA, "Concentrate", "2.250"))
	if err != nil {
		t.Fatalf("edit an existing cell on a separator-bearing pen: %v", err)
	}
	if second.Outcome != domain.OutcomeCorrected {
		t.Fatalf("edit outcome = %q, want %q", second.Outcome, domain.OutcomeCorrected)
	}

	cells := penCells(t, ctx, pool, fcPenA)
	if len(cells) != 1 {
		t.Fatalf("pen holds %d cells after an edit, want 1 (a second row means the edit inserted)", len(cells))
	}
	if cells["Concentrate"] != "2.250" {
		t.Fatalf("kg = %q, want 2.250", cells["Concentrate"])
	}
}

// TestExperimentWriteLandsOnTheAddressedPenOnly proves the blast radius of an authored cell.
func TestExperimentWriteLandsOnTheAddressedPenOnly(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-iso-a", fcPenA, "Concentrate", "1.000")); err != nil {
		t.Fatalf("author pen A: %v", err)
	}
	if _, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-iso-b", fcPenB, "Concentrate", "9.000")); err != nil {
		t.Fatalf("author pen B: %v", err)
	}

	// Two pens of ONE shed, same feed item, different quantities. Under a shed-grain natural key
	// these would be one row and the second write would overwrite the first.
	if got := penCells(t, ctx, pool, fcPenA)["Concentrate"]; got != "1.000" {
		t.Fatalf("pen A kg = %q, want 1.000 (pen B's write leaked onto it)", got)
	}
	if got := penCells(t, ctx, pool, fcPenB)["Concentrate"]; got != "9.000" {
		t.Fatalf("pen B kg = %q, want 9.000", got)
	}
}

// TestSetExperimentStatusRetiresOnePenAndLeavesItsSiblingsAlone is the GOAT-FEED-002 regression.
//
// The screen groups rows by PEN and captions this control with the pen's own display name, while
// the write was scoped to the SHED. Clicking it on one pen of Godel 1 retired all ten. Because
// membership-with-status-active IS the experiment workflow flag, the nine unnamed pens silently
// dropped back to the per-head ration grid at roughly 2.2x their authored quantity -- a real change
// to what those animals are fed, applied by a button that named one pen.
func TestSetExperimentStatusRetiresOnePenAndLeavesItsSiblingsAlone(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	for _, item := range []string{"Concentrate", "Hybrid"} {
		if _, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-st-a-"+item, fcPenA, item, "1.000")); err != nil {
			t.Fatalf("author pen A %s: %v", item, err)
		}
		if _, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-st-b-"+item, fcPenB, item, "2.000")); err != nil {
			t.Fatalf("author pen B %s: %v", item, err)
		}
	}

	if _, err := repo.SetExperimentShedStatus(ctx, domain.SetExperimentShedStatusCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
			IdempotencyKey: "pen-st-retire-a", RequestFingerprint: "fp-pen-st-retire-a",
		},
		ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: fcPenA,
		Status: domain.ExperimentStatusRetired,
	}); err != nil {
		t.Fatalf("retire pen A: %v", err)
	}

	// Every cell of the NAMED pen is retired -- within a pen the flip stays all-or-nothing, because
	// a half-active pen reads as enrolled while being fed only its active subset.
	for item, status := range penStatuses(t, ctx, pool, fcPenA) {
		if status != domain.ExperimentStatusRetired {
			t.Fatalf("pen A %s = %q, want retired", item, status)
		}
	}
	// ...and NOT ONE cell of the sibling pen moved.
	siblings := penStatuses(t, ctx, pool, fcPenB)
	if len(siblings) != 2 {
		t.Fatalf("sibling pen holds %d cells, want 2", len(siblings))
	}
	for item, status := range siblings {
		if status != domain.ExperimentStatusActive {
			t.Fatalf("sibling pen %s = %q, want active — retiring one pen retired the whole shed", item, status)
		}
	}
}

// TestEditingOnePenDoesNotReactivateOrRestampItsSiblings covers the two shed-wide side effects that
// rode along with a pen-scoped edit: reactivation, and the head-count/arm metadata sweep.
//
// The metadata sweep is the more destructive of the two. head_count and experiment_category were
// treated as SHED-level facts, which was true before this table became partition-aware and is now
// false -- each of Godel 1's pens carries its own arm and head count. A shed-wide sweep flattened
// every pen onto whichever one was edited, overwriting hand-keyed authored data with no way back.
func TestEditingOnePenDoesNotReactivateOrRestampItsSiblings(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	countA, countB := int32(10), int32(15)
	authored := func(key, pen, item, kg, arm string, head *int32) domain.UpsertExperimentConfigCommand {
		cmd := penCommand(key, pen, item, kg)
		cmd.ExperimentCategory = arm
		cmd.HeadCount = head
		return cmd
	}
	if _, err := repo.UpsertExperimentConfig(ctx, authored("pen-md-a", fcPenA, "Concentrate", "1.000", "Sheep M NEW", &countA)); err != nil {
		t.Fatalf("author pen A: %v", err)
	}
	if _, err := repo.UpsertExperimentConfig(ctx, authored("pen-md-b", fcPenB, "Concentrate", "2.000", "B+S Goat F NEW", &countB)); err != nil {
		t.Fatalf("author pen B: %v", err)
	}

	// Retire the sibling, then edit the OTHER pen. Neither the retirement nor the arm/head count of
	// the sibling may move.
	if _, err := repo.SetExperimentShedStatus(ctx, domain.SetExperimentShedStatusCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
			IdempotencyKey: "pen-md-retire-b", RequestFingerprint: "fp-pen-md-retire-b",
		},
		ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: fcPenB,
		Status: domain.ExperimentStatusRetired,
	}); err != nil {
		t.Fatalf("retire pen B: %v", err)
	}
	if _, err := repo.UpsertExperimentConfig(ctx, authored("pen-md-a2", fcPenA, "Concentrate", "3.000", "Sheep M NEW", &countA)); err != nil {
		t.Fatalf("edit pen A: %v", err)
	}

	if got := penStatuses(t, ctx, pool, fcPenB)["Concentrate"]; got != domain.ExperimentStatusRetired {
		t.Fatalf("sibling pen status = %q, want retired — editing pen A un-retired pen B", got)
	}
	var arm string
	var head *int32
	if err := pool.QueryRow(ctx, `
SELECT experiment_category, head_count
FROM feed_experiment_config
WHERE tenant_id = $1::uuid AND shed_id = $2::uuid AND partition_key = feed_config_norm($3)`,
		fcTenant, fcPennedShed, fcPenB).Scan(&arm, &head); err != nil {
		t.Fatalf("read sibling metadata: %v", err)
	}
	if arm != "B+S Goat F NEW" {
		t.Fatalf("sibling arm = %q, want its own — pen A's edit restamped it", arm)
	}
	if head == nil || *head != countB {
		t.Fatalf("sibling head count = %v, want %d — pen A's edit restamped it", head, countB)
	}
}

// TestExperimentWriteRejectsAPenThatIsNotInTheShedCatalog is the GOAT-FEED-004 regression.
//
// The write path validated the park and the shed and then took the pen on trust, so a typo authored
// quantities against a pen that exists nowhere, and a blank on a subdivided shed authored a phantom
// whole-shed row beside the real pens. Neither is reachable by any operational location, so the
// direction generator never reads those rows: the author sees a saved kg that will never be fed.
func TestExperimentWriteRejectsAPenThatIsNotInTheShedCatalog(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	cases := []struct {
		name string
		pen  string
		want error
	}{
		{"a pen the catalog does not hold", "Part 99", ports.ErrPartitionNotFound},
		{"no pen named on a subdivided shed", "", ports.ErrPartitionRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-bad-"+tc.name, tc.pen, "Concentrate", "1.000"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("single-cell write err = %v, want %v", err, tc.want)
			}
			_, err = repo.UpsertExperimentConfigBatch(ctx, domain.UpsertExperimentConfigBatchCommand{
				WriteIdentity: domain.WriteIdentity{
					TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
					IdempotencyKey: "pen-bad-batch-" + tc.name, RequestFingerprint: "fp-pen-bad-batch-" + tc.name,
				},
				ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: tc.pen,
				ExperimentCategory: "Arm A",
				Cells:              []domain.ExperimentBatchCell{{FeedItemLabel: "Concentrate", AbsoluteKg: "1.000"}},
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("batch write err = %v, want %v", err, tc.want)
			}

			// And nothing was written by either attempt.
			var rows int
			if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_experiment_config WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`,
				fcTenant, fcPennedShed).Scan(&rows); err != nil {
				t.Fatalf("count rows: %v", err)
			}
			if rows != 0 {
				t.Fatalf("rejected write left %d row(s) behind", rows)
			}
		})
	}
}

// TestExperimentWriteRejectsAPenLabelOnAnUndividedShed is the other direction of the same rule: the
// fixture's plain sheds have no catalog entries at all, so naming a pen on one describes a place
// that does not exist.
func TestExperimentWriteRejectsAPenLabelOnAnUndividedShed(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	cmd := penCommand("pen-on-undivided", "Part 3", "Concentrate", "1.000")
	cmd.ShedID = fcShed // an undivided shed in the same park
	if _, err := repo.UpsertExperimentConfig(ctx, cmd); !errors.Is(err, ports.ErrPartitionNotFound) {
		t.Fatalf("err = %v, want %v", err, ports.ErrPartitionNotFound)
	}

	// A blank label on that same shed is the legitimate whole-shed case and must still work.
	ok := penCommand("pen-on-undivided-blank", "", "Concentrate", "1.000")
	ok.ShedID = fcShed
	if _, err := repo.UpsertExperimentConfig(ctx, ok); err != nil {
		t.Fatalf("whole-shed write on an undivided shed: %v", err)
	}
}

// TestUpsertExperimentConfigBatchWritesEveryCellOrNone proves the atomicity the batch endpoint
// exists for.
//
// Enrolling a pen through N single-cell posts can half-succeed, and a half-succeeded enrolment is
// worse than none: membership is the workflow flag, so the pen is ON the experiment and fed only
// the subset that landed.
func TestUpsertExperimentConfigBatchWritesEveryCellOrNone(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	head := int32(12)
	cells := []domain.ExperimentBatchCell{
		{FeedItemLabel: "Concentrate", AbsoluteKg: "1.500"},
		{FeedItemLabel: "Hybrid", AbsoluteKg: "0"},
		{FeedItemLabel: "COFS", AbsoluteKg: "12.250"},
	}
	if _, err := repo.UpsertExperimentConfigBatch(ctx, domain.UpsertExperimentConfigBatchCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
			IdempotencyKey: "pen-batch-ok", RequestFingerprint: "fp-pen-batch-ok",
		},
		ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: fcPenA,
		HeadCount: &head, ExperimentCategory: "Arm A", Cells: cells,
	}); err != nil {
		t.Fatalf("batch enrol: %v", err)
	}

	got := penCells(t, ctx, pool, fcPenA)
	if len(got) != len(cells) {
		t.Fatalf("pen holds %d cells, want %d", len(got), len(cells))
	}
	// An authored 0 is real configuration -- "this arm deliberately gets none of this item" -- and
	// must survive the round trip as a row rather than being dropped as falsy.
	if got["Hybrid"] != "0.000" {
		t.Fatalf("authored zero = %q, want 0.000 (an authored 0 is not an absent cell)", got["Hybrid"])
	}
	if got["COFS"] != "12.250" {
		t.Fatalf("COFS = %q, want 12.250", got["COFS"])
	}

	// A batch naming a pen that does not exist writes NOTHING -- not even the cells that would
	// otherwise be valid, and not onto the pen enrolled above.
	if _, err := repo.UpsertExperimentConfigBatch(ctx, domain.UpsertExperimentConfigBatchCommand{
		WriteIdentity: domain.WriteIdentity{
			TenantID: fcTenant, ActorRef: "tester", EffectiveFrom: "2026-08-09",
			IdempotencyKey: "pen-batch-bad", RequestFingerprint: "fp-pen-batch-bad",
		},
		ParkID: fcPark, ShedID: fcPennedShed, PartitionLabel: "Part 99",
		ExperimentCategory: "Arm A", Cells: cells,
	}); !errors.Is(err, ports.ErrPartitionNotFound) {
		t.Fatalf("bad-pen batch err = %v, want %v", err, ports.ErrPartitionNotFound)
	}
	var total int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM feed_experiment_config WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`,
		fcTenant, fcPennedShed).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if total != len(cells) {
		t.Fatalf("shed holds %d rows after a rejected batch, want %d", total, len(cells))
	}
}

// TestListPensReturnsTheHumanLabelAndItsConfiguredFlag pins the READ the enroller depends on.
//
// Two things it must never do, both of which have shipped in this codebase before: return
// normalized_label ('3') where the human label ('Part 3') belongs, and compose the display by
// rejoining the shed name itself. The assertion is on the OUTPUT STRING of a real round trip --
// field-presence assertions and pure-Go formatter tests both passed while real output was wrong.
func TestListPensReturnsTheHumanLabelAndItsConfiguredFlag(t *testing.T) {
	ctx := context.Background()
	pool := setupPennedDB(t, ctx)
	repo := fcRepo(pool)

	if _, err := repo.UpsertExperimentConfig(ctx, penCommand("pen-list-a", fcPenA, "Concentrate", "1.000")); err != nil {
		t.Fatalf("author pen A: %v", err)
	}

	page, err := repo.ListPens(ctx, domain.PenQuery{
		TenantID: fcTenant, ParkID: fcPark,
		Page: domain.Page{Limit: 50},
	})
	if err != nil {
		t.Fatalf("list pens: %v", err)
	}

	byDisplay := map[string]domain.Pen{}
	for _, pen := range page.Items {
		byDisplay[pen.OperationalLocationDisplay] = pen
	}

	// The authored pen, spelled the way the farm spells it and joined the canonical way.
	penA, ok := byDisplay["Kepler 7 - Part 3"]
	if !ok {
		t.Fatalf("pen A missing; got displays %v", keysOf(byDisplay))
	}
	if penA.PartitionLabel != fcPenA {
		t.Fatalf("partition label = %q, want %q (normalized_label is a matching key, never display)",
			penA.PartitionLabel, fcPenA)
	}
	if !penA.HasExperimentConfig {
		t.Fatalf("pen A has an authored cell but reads as unconfigured")
	}

	// Its EMPTY sibling is still a real place and must be offerable. A catalog derived from
	// per-animal data would hide it.
	penB, ok := byDisplay["Kepler 7 - Part 4"]
	if !ok {
		t.Fatalf("empty sibling pen missing; got displays %v", keysOf(byDisplay))
	}
	if penB.HasExperimentConfig {
		t.Fatalf("pen B has no authored cell but reads as configured")
	}

	// An UNDIVIDED shed appears exactly once, under its bare name -- never with a dangling
	// separator and never as the 'whole' sentinel.
	if _, ok := byDisplay["CPT Shed 1"]; !ok {
		t.Fatalf("undivided shed missing from the catalog; got displays %v", keysOf(byDisplay))
	}
	// A shed WITH pens is offered only as its pens, never additionally as itself: a bare
	// 'Kepler 7' option would be a whole-shed location that does not exist, and enrolling it would
	// author the phantom 'whole' row requirePartitionInShed now rejects.
	for display := range byDisplay {
		if display == "Kepler 7" {
			t.Fatalf("subdivided shed listed as a bare shed as well as by pen")
		}
	}

	// KNOWN ENVIRONMENT HAZARD, deliberately not asserted here because it is not this query's to
	// fix: the baseline also carries legacy alias rows that are THEMSELVES sheds named
	// 'Godel 1 - Part 3'. In STG all 120 of them are status='inactive', so the active-only filter
	// above excludes them and the catalog is clean. Nothing in migrations performs that
	// deactivation, so on a freshly migrated database they are active and would be offered here as
	// bare sheds alongside the real pens. That same fresh database also gets an EMPTY
	// shed_partitions catalog, because migration 000112 seeds pens by reading aliases that are
	// inactive -- so the gap is in environment setup, upstream of this read.

}

func keysOf(m map[string]domain.Pen) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
