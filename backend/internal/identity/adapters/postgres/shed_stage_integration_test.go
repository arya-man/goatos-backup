package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Whole-pen cohort reclassification regression suite.
//
// Every assertion here is a DB ROUND TRIP against the real production path -- the repository
// method the HTTP handler calls -- not a pure-Go formatter check and not a field-presence check.
// Both weaker forms passed while real partition output was wrong (operational-location defect
// classes 9 and OL-3), so this suite reads back `goats`, `goat_identity_events` and
// `outbox_messages` after the write and asserts the values that actually landed.

const (
	ssTenant = "00000000-0000-4000-8000-000000000001" // shared baseline tenant
	ssParty  = "00000000-0000-4000-8000-000000001001"
	ssPark   = "00000000-0000-4000-8000-000000003001" // CBE, shared baseline park
	ssActor  = "30000000-0000-4000-8000-000000000001"
)

// ssFixture is one park holding a PARTITIONED shed (Castro, pens 1 and 2) and an UNDIVIDED shed
// (Yashoda). The two-pen shape is the point: it is what proves the write is pen-scoped rather than
// building-scoped.
type ssFixture struct {
	castroShed  string
	yashodaShed string
}

func seedShedStageFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) ssFixture {
	t.Helper()
	insertShed := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, 'shed', $2, $3::uuid, 'active')
RETURNING location_id::text`, ssTenant, name, ssPark).Scan(&id); err != nil {
			t.Fatalf("seed shed %s: %v", name, err)
		}
		return id
	}
	f := ssFixture{castroShed: insertShed("Castro"), yashodaShed: insertShed("Yashoda")}

	// The partition CATALOG. partition_label is the DISPLAY label; normalized_label is the
	// matching key. Seeding both apart is deliberate -- a query that returns the key would render
	// "Castro - 1" as "Castro - 1" by luck here, so pen 2 uses the 'Part N' convention where the
	// two genuinely differ and a wrong column is visible.
	for _, pen := range []struct{ label, normalized string }{{"1", "1"}, {"Part 2", "2"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'manual')`, ssTenant, f.castroShed, pen.label, pen.normalized); err != nil {
			t.Fatalf("seed shed_partitions %s: %v", pen.label, err)
		}
	}
	return f
}

// seedStageVocabulary installs the tenant's cohort tags with the kid/adult band each one carries.
// K2 is a kid cohort and Mother is an adult one; ICU is CLINICAL and carries NULL, matching
// migration 000109's deliberate shape.
func seedStageVocabulary(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	rows := []struct {
		code, band string
	}{{"K2", "kid"}, {"F2-Male", "kid"}, {"Mother", "adult"}, {"Non-Pregnant", "adult"}}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status)
VALUES ($1::uuid, $2, $2, $3, 'active')`, ssTenant, row.code, row.band); err != nil {
			t.Fatalf("seed stage %s: %v", row.code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status)
VALUES ($1::uuid, 'icu', 'ICU', NULL, 'active')`, ssTenant); err != nil {
		t.Fatalf("seed clinical stage: %v", err)
	}
}

// seedStageGoat places one live animal in a pen with a starting cohort tag and band.
func seedStageGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, partitionLabel, stage, band string) string {
	t.Helper()
	var goatID string
	if err := pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex, age_band, management_stage)
VALUES ($1::uuid, 'alive', 'goat', $2::uuid, $3::uuid, $4::uuid, $3::uuid, 'Boer', 'female', $5, $6)
RETURNING goat_id::text`, ssTenant, ssParty, shedID, ssPark, band, stage).Scan(&goatID); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	if partitionLabel != "" {
		if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'seed')`, ssTenant, goatID, shedID, partitionLabel); err != nil {
			t.Fatalf("seed goat_shed_partitions: %v", err)
		}
	}
	return goatID
}

func readStageAndBand(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) (stage, band string) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(management_stage, ''), COALESCE(age_band, '')
FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, ssTenant, goatID).Scan(&stage, &band); err != nil {
		t.Fatalf("read goat stage: %v", err)
	}
	return stage, band
}

func reclassifyCmd(shedID, partition, stage, key string) ports.ReclassifyShedStageCommand {
	cmd := ports.ReclassifyShedStageCommand{
		TenantID:             ssTenant,
		ActorID:              ssActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: key,
		IdempotencyScope:     "reclassifyShedStage",
		RequestHash:          "hash:" + key,
		TraceID:              "trace-" + key,
		ShedID:               shedID,
		ManagementStage:      stage,
		Reason:               "cohort moved up",
		OccurredAt:           time.Date(2026, time.August, 12, 9, 0, 0, 0, time.UTC),
	}
	if partition != "" {
		label := partition
		cmd.PartitionLabel = &label
	}
	return cmd
}

// TestReclassifyShedStageScopeHierarchyKeepsSiblingPensAndShedsUntouched is the SCOPE adversarial
// case: the write must reach exactly one pen.
//
// Castro - 1, Castro - 2 and Yashoda all hold K2 kids. Reclassifying Castro - 1 to Mother must move
// ONLY Castro - 1. A predicate that dropped the partition would take the whole building; one that
// dropped the shed would take the park. Both failures are invisible without a sibling in place,
// which is why all three exist here.
func TestReclassifyShedStageScopeHierarchyKeepsSiblingPensAndShedsUntouched(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	pen1 := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	pen2 := seedStageGoat(t, ctx, pool, f.castroShed, "Part 2", "K2", "kid")
	other := seedStageGoat(t, ctx, pool, f.yashodaShed, "", "K2", "kid")

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-scope"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.Reclassified != 1 || result.TotalLive != 1 {
		t.Fatalf("reclassified=%d total=%d, want 1/1 -- the write reached beyond the selected pen",
			result.Reclassified, result.TotalLive)
	}

	if stage, band := readStageAndBand(t, ctx, pool, pen1); stage != "Mother" || band != "adult" {
		t.Fatalf("selected pen goat = (%s, %s), want (Mother, adult)", stage, band)
	}
	for name, goatID := range map[string]string{"sibling pen Castro - 2": pen2, "other shed Yashoda": other} {
		if stage, band := readStageAndBand(t, ctx, pool, goatID); stage != "K2" || band != "kid" {
			t.Fatalf("%s was changed to (%s, %s) -- it is outside the selected pen and must be untouched", name, stage, band)
		}
	}
}

// TestReclassifyShedStageOneToManyKeepsCardinalityAtOneRowPerGoat is the CARDINALITY adversarial
// case for the preview aggregate.
//
// goat_shed_partitions is PK (tenant_id, goat_id), so the LEFT JOIN is 1:0..1 and a goat can never
// fan into two rows. This asserts that directly: the preview's bucket counts must sum to the true
// live head count of the pen, across MULTIPLE cohort dimensions (three distinct stage/band pairs)
// where a fan-out would inflate the total.
func TestReclassifyShedStageOneToManyKeepsCardinalityAtOneRowPerGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2-Male", "kid")
	seedStageGoat(t, ctx, pool, f.castroShed, "1", "Mother", "adult")

	preview, err := repo.PreviewReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-card"))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.TotalLive != 4 {
		t.Fatalf("total_live=%d, want 4 -- a join fan-out would inflate this", preview.TotalLive)
	}
	// Buckets are DISJOINT and must sum to the total; changing + unchanged is the same set
	// partitioned a second way and must sum to it too.
	sum := 0
	for _, bucket := range preview.CurrentStages {
		sum += bucket.Count
	}
	if sum != preview.TotalLive {
		t.Fatalf("bucket sum=%d, total_live=%d -- buckets must be disjoint and exhaustive", sum, preview.TotalLive)
	}
	if preview.Changing+preview.Unchanged != preview.TotalLive {
		t.Fatalf("changing(%d)+unchanged(%d) != total_live(%d)", preview.Changing, preview.Unchanged, preview.TotalLive)
	}
	if preview.Changing != 3 || preview.Unchanged != 1 {
		t.Fatalf("changing=%d unchanged=%d, want 3/1 -- the one animal already on Mother must not count as changing",
			preview.Changing, preview.Unchanged)
	}
}

// TestReclassifyShedStageStatusBucketsExcludeNonLiveAnimals is the STATUS adversarial case.
//
// The pen holds one live animal plus one exited and one merged. Only the live one may be counted or
// written: a reclassification that retagged an exited animal would resurrect it in every downstream
// read that trusts management_stage, and vaccination would start generating obligations for it.
func TestReclassifyShedStageStatusBucketsExcludeNonLiveAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	live := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	// 'sold', not 'exited': goats_exited_lifecycle_check restricts a row carrying exited_at to
	// dead/sold/culled/transferred/lost/merged/inactive. 'exited' is not a lifecycle_status value.
	exited := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	merged := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")

	if _, err := pool.Exec(ctx, `
UPDATE goats SET lifecycle_status='sold', exited_at=now() WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		ssTenant, exited); err != nil {
		t.Fatalf("mark exited: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats SET merged_into_goat_id=$3::uuid WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		ssTenant, merged, live); err != nil {
		t.Fatalf("mark merged: %v", err)
	}

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-status"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.TotalLive != 1 || result.Reclassified != 1 {
		t.Fatalf("total_live=%d reclassified=%d, want 1/1 -- exited and merged animals must be excluded",
			result.TotalLive, result.Reclassified)
	}
	for name, goatID := range map[string]string{"exited": exited, "merged": merged} {
		if stage, _ := readStageAndBand(t, ctx, pool, goatID); stage != "K2" {
			t.Fatalf("%s animal was retagged to %s -- non-live animals must never be written", name, stage)
		}
	}
}

// TestReclassifyShedStagePaginationIsAbsentSoTheWholePenIsWritten is the PAGINATION adversarial
// case.
//
// The preview reports a WHOLE-SCOPE aggregate and the commit writes the WHOLE pen; neither may
// carry a LIMIT. A page-sized cap would silently leave part of the pen on the old cohort, which is
// exactly the split-pen state this action exists to remove. The pen is deliberately seeded past any
// plausible page size (25) so a stray LIMIT 10/20 shows up as a shortfall.
func TestReclassifyShedStagePaginationIsAbsentSoTheWholePenIsWritten(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	const penSize = 25
	for range penSize {
		seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	}

	preview, err := repo.PreviewReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-page"))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.TotalLive != penSize || preview.Changing != penSize {
		t.Fatalf("preview total=%d changing=%d, want %d/%d -- a page cap would truncate this",
			preview.TotalLive, preview.Changing, penSize, penSize)
	}

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-page-2"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.Reclassified != penSize {
		t.Fatalf("reclassified=%d, want %d -- the whole pen must move, never a page of it", result.Reclassified, penSize)
	}

	var stragglers int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM goats WHERE tenant_id=$1::uuid AND shed_id=$2::uuid AND management_stage <> 'Mother'`,
		ssTenant, f.castroShed).Scan(&stragglers); err != nil {
		t.Fatalf("count stragglers: %v", err)
	}
	if stragglers != 0 {
		t.Fatalf("%d animals left on the old cohort -- the pen is now split, which is the defect this action removes", stragglers)
	}
}

// TestReclassifyShedStageMultipleDimensionsCarryAgeBandAndEmitOneEventPerChangedGoat proves the
// two things the maintainer asked for by name: the whole pen changes, and kid/adult follows.
//
// It also asserts the event grain, because vaccination re-derives each animal's schedule from
// goat.stage_changed: exactly one event per animal that ACTUALLY changed, and none for the animal
// that already carried the target tag.
func TestReclassifyShedStageMultipleDimensionsCarryAgeBandAndEmitOneEventPerChangedGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	kidA := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	kidB := seedStageGoat(t, ctx, pool, f.castroShed, "1", "F2-Male", "kid")
	alreadyAdult := seedStageGoat(t, ctx, pool, f.castroShed, "1", "Mother", "adult")

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-band"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.Reclassified != 2 || result.Unchanged != 1 {
		t.Fatalf("reclassified=%d unchanged=%d, want 2/1", result.Reclassified, result.Unchanged)
	}

	// The headline requirement: kids became adults because the TAG says adult.
	for _, goatID := range []string{kidA, kidB} {
		stage, band := readStageAndBand(t, ctx, pool, goatID)
		if stage != "Mother" || band != "adult" {
			t.Fatalf("goat = (%s, %s), want (Mother, adult) -- age_band must ride along with the cohort tag", stage, band)
		}
	}

	// Event grain: one per CHANGED animal, none for the animal that was already correct.
	for name, expect := range map[string]struct {
		goatID string
		want   int
	}{
		"changed kidA":   {kidA, 1},
		"changed kidB":   {kidB, 1},
		"already Mother": {alreadyAdult, 0},
	} {
		var events int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='goat.stage_changed' AND aggregate_id=$2::uuid`,
			ssTenant, expect.goatID).Scan(&events); err != nil {
			t.Fatalf("count events: %v", err)
		}
		if events != expect.want {
			t.Fatalf("%s has %d goat.stage_changed events, want %d -- re-eventing an unchanged animal makes vaccination re-evaluate for nothing",
				name, events, expect.want)
		}
	}

	// The response's location display is the DB round trip, not a formatter unit test: '1' is the
	// display label seeded in the catalog, so the pen reads "Castro - 1".
	if result.OperationalLocationDisplay != "Castro - 1" {
		t.Fatalf("operational_location_display=%q, want %q", result.OperationalLocationDisplay, "Castro - 1")
	}
}

// TestReclassifyShedStageRepairsSameStageStaleAgeBand covers the silent stale-band case: a goat
// can already carry the target cohort label while its durable age_band is still wrong. The command
// must repair that row because vaccination and health schedules read age_band, not just the label.
func TestReclassifyShedStageRepairsSameStageStaleAgeBand(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	stale := seedStageGoat(t, ctx, pool, f.castroShed, "1", "Mother", "kid")

	preview, err := repo.PreviewReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-stale-preview"))
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Changing != 1 || preview.Unchanged != 0 {
		t.Fatalf("preview changing=%d unchanged=%d, want 1/0 for a same-stage stale age_band", preview.Changing, preview.Unchanged)
	}

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-stale-band"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.Reclassified != 1 || result.Unchanged != 0 {
		t.Fatalf("reclassified=%d unchanged=%d, want 1/0 for a stale age_band repair", result.Reclassified, result.Unchanged)
	}
	if stage, band := readStageAndBand(t, ctx, pool, stale); stage != "Mother" || band != "adult" {
		t.Fatalf("goat = (%s, %s), want (Mother, adult) -- same-stage stale age_band must be repaired", stage, band)
	}

	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='goat.stage_changed' AND aggregate_id=$2::uuid`,
		ssTenant, stale).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Fatalf("%d stage-change events, want 1 so downstream vaccination rechecks the repaired band", events)
	}
}

// TestReclassifyShedStageRendersDisplayLabelNotNormalizedKey pins operational-location defect
// class 1: partition_label ('Part 2') is the human label and normalized_label ('2') is a matching
// key. Selecting the key renders "Castro - 2" where the farm says "Castro - Part 2". That defect
// shipped, was fixed, and was then reintroduced by another module hours later.
func TestReclassifyShedStageRendersDisplayLabelNotNormalizedKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedStageGoat(t, ctx, pool, f.castroShed, "Part 2", "K2", "kid")

	// Requested by the NORMALIZED key on purpose: the caller may send either form, and the answer
	// must still be the display label.
	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "2", "Mother", "key-label"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.PartitionLabel != "Part 2" {
		t.Fatalf("partition_label=%q, want %q -- normalized_label is a matching key and must never be rendered", result.PartitionLabel, "Part 2")
	}
	if result.OperationalLocationDisplay != "Castro - Part 2" {
		t.Fatalf("operational_location_display=%q, want %q", result.OperationalLocationDisplay, "Castro - Part 2")
	}
}

// TestReclassifyShedStageRejectsClinicalTargetAndWritesNothing is the medical-safety case.
//
// A clinical state is set by the animal's own health workflow. Marking a whole pen "icu" from a
// census screen would fabricate a diagnosis for every animal in it and defer their vaccinations.
// The rejection must also write NOTHING -- a partial write here is worse than the rejection.
func TestReclassifyShedStageRejectsClinicalTargetAndWritesNothing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	goatID := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")

	_, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "icu", "key-clinical"))
	if !errors.Is(err, ports.ErrClinicalDestinationTag) {
		t.Fatalf("err = %v, want ErrClinicalDestinationTag", err)
	}
	if stage, band := readStageAndBand(t, ctx, pool, goatID); stage != "K2" || band != "kid" {
		t.Fatalf("goat = (%s, %s) after a rejected clinical target, want it untouched at (K2, kid)", stage, band)
	}
	var events int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type='goat.stage_changed'`,
		ssTenant).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Fatalf("%d stage-change events emitted for a rejected write -- the transaction must roll back whole", events)
	}
}

// TestReclassifyShedStageExactReplayDoesNotWriteTwice is the idempotency case.
//
// This write applies immediately with no approval step, so the Idempotency-Key is the only thing
// between a double-clicked button and a second round of stage-change events. An exact replay must
// return the original answer and emit nothing new.
func TestReclassifyShedStageExactReplayDoesNotWriteTwice(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	goatID := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")

	cmd := reclassifyCmd(f.castroShed, "1", "Mother", "key-replay")
	first, err := repo.ReclassifyShedStage(ctx, cmd)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := repo.ReclassifyShedStage(ctx, cmd)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.Reclassified != first.Reclassified {
		t.Fatalf("replay reported %d reclassified, first reported %d -- a replay must return the original answer",
			second.Reclassified, first.Reclassified)
	}

	var events, versions int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='goat.stage_changed' AND aggregate_id=$2::uuid`,
		ssTenant, goatID).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 1 {
		t.Fatalf("%d stage-change events after a replay, want 1", events)
	}
	if err := pool.QueryRow(ctx, `
SELECT row_version FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`, ssTenant, goatID).Scan(&versions); err != nil {
		t.Fatalf("read row_version: %v", err)
	}
	if versions != 2 {
		t.Fatalf("row_version=%d after a replay, want 2 (one write, not two)", versions)
	}
}

// TestReclassifyShedStageSameKeyDifferentPenIsRejected proves the key is bound to the REQUEST, not
// merely held: reusing it against a different pen must conflict rather than silently replay the
// first pen's answer while the second pen is left unchanged.
func TestReclassifyShedStageSameKeyDifferentPenIsRejected(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	pen2Goat := seedStageGoat(t, ctx, pool, f.castroShed, "Part 2", "K2", "kid")

	if _, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-shared")); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Same stored key, different pen => different request hash.
	conflicting := reclassifyCmd(f.castroShed, "Part 2", "Mother", "key-shared")
	conflicting.RequestHash = "hash:different-pen"
	if _, err := repo.ReclassifyShedStage(ctx, conflicting); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
	if stage, _ := readStageAndBand(t, ctx, pool, pen2Goat); stage != "K2" {
		t.Fatalf("pen 2 goat = %s -- a rejected key reuse must write nothing", stage)
	}
}

// TestReclassifyShedStageUndividedShedMatchesAnimalsWithNoPartitionRow covers the shape that has no
// goat_shed_partitions row at all. Those animals must match a 'whole' request via the COALESCE in
// the scope predicate, and the display must be the bare shed name -- never "Yashoda whole".
func TestReclassifyShedStageUndividedShedMatchesAnimalsWithNoPartitionRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	goatID := seedStageGoat(t, ctx, pool, f.yashodaShed, "", "K2", "kid")

	result, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.yashodaShed, "", "Mother", "key-whole"))
	if err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if result.Reclassified != 1 {
		t.Fatalf("reclassified=%d, want 1 -- an animal with no partition row must match a whole-shed request", result.Reclassified)
	}
	if result.OperationalLocationDisplay != "Yashoda" {
		t.Fatalf("operational_location_display=%q, want %q -- 'whole' is a matching key and must never render",
			result.OperationalLocationDisplay, "Yashoda")
	}
	if stage, band := readStageAndBand(t, ctx, pool, goatID); stage != "Mother" || band != "adult" {
		t.Fatalf("goat = (%s, %s), want (Mother, adult)", stage, band)
	}
}

// TestReclassifyShedStageEmptyPenIsReportedNotSilentlySucceeded: an empty pen almost always means
// the operator picked the wrong one. Reporting zero changes as success hides that.
func TestReclassifyShedStageEmptyPenIsReportedNotSilentlySucceeded(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	if _, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-empty")); !errors.Is(err, ports.ErrReclassifyEmptyScope) {
		t.Fatalf("err = %v, want ErrReclassifyEmptyScope", err)
	}
}

// TestReclassifyShedStageUnknownPartitionFailsClosed: the pen is validated against the CATALOG, so
// a partition that does not exist is rejected rather than matching zero animals and reading as an
// empty pen. The two are different operator problems and must not report the same way.
func TestReclassifyShedStageUnknownPartitionFailsClosed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")

	_, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "9", "Mother", "key-unknown"))
	if !errors.Is(err, ports.ErrInvalidReference) {
		t.Fatalf("err = %v, want ErrInvalidReference for a partition absent from the catalog", err)
	}
}

// penCohort reads the CONFIGURED cohort of one pen from the catalog, which is what the Sheds
// directory renders -- deliberately not the animals' stage, so a test cannot pass by looking at the
// half of the write it already asserts elsewhere.
func penCohort(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, partition string) string {
	t.Helper()
	var code string
	err := pool.QueryRow(ctx, `
SELECT COALESCE(a.stage_code, '')
FROM shed_partitions sp
LEFT JOIN animal_stage_lookup a ON a.tenant_id = sp.tenant_id AND a.animal_stage_id = sp.animal_stage_id
WHERE sp.tenant_id = $1::uuid AND sp.shed_id = $2::uuid AND sp.partition_label = $3`,
		ssTenant, shedID, partition).Scan(&code)
	if err != nil {
		t.Fatalf("read pen cohort: %v", err)
	}
	return code
}

// TestReclassifyShedStageWritesThePensOwnCohortAndLeavesSiblingsAlone is the pen-grain half of the
// write, added when the Sheds directory made a pen's tag editable (migration 000161).
//
// The animals moving is asserted by the scope test above; what this pins is that the pen's own
// CONFIGURED tag moves with them, in the same command, WITHOUT touching a sibling pen or the parent
// shed's profile. That last part is load-bearing: shifting resolves a destination cohort from
// shed_profiles, so a pen edit that quietly rewrote the shed's profile would change which stage an
// animal adopts when it is moved -- a business rule under the maintainer lock.
func TestReclassifyShedStageWritesThePensOwnCohortAndLeavesSiblingsAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	seedStageGoat(t, ctx, pool, f.castroShed, "Part 2", "K2", "kid")

	if _, err := pool.Exec(ctx, `
UPDATE shed_partitions sp SET animal_stage_id = a.animal_stage_id
FROM animal_stage_lookup a
WHERE sp.tenant_id = $1::uuid AND sp.shed_id = $2::uuid AND a.tenant_id = sp.tenant_id AND a.stage_code = 'K2'`,
		ssTenant, f.castroShed); err != nil {
		t.Fatalf("seed pen cohorts: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, row_version)
SELECT $2::uuid, $1::uuid, a.animal_stage_id, 1 FROM animal_stage_lookup a
WHERE a.tenant_id = $1::uuid AND a.stage_code = 'K2'`, ssTenant, f.castroShed); err != nil {
		t.Fatalf("seed shed profile: %v", err)
	}

	if _, err := repo.ReclassifyShedStage(ctx, reclassifyCmd(f.castroShed, "1", "Mother", "key-pen-cohort")); err != nil {
		t.Fatalf("reclassify: %v", err)
	}

	if got := penCohort(t, ctx, pool, f.castroShed, "1"); got != "Mother" {
		t.Fatalf("edited pen cohort = %q, want Mother -- the animals moved but the pen's tag did not", got)
	}
	if got := penCohort(t, ctx, pool, f.castroShed, "Part 2"); got != "K2" {
		t.Fatalf("sibling pen cohort = %q, want K2 -- the write reached beyond the selected pen", got)
	}

	var shedProfileStage string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(a.stage_code, '') FROM shed_profiles p
LEFT JOIN animal_stage_lookup a ON a.tenant_id = p.tenant_id AND a.animal_stage_id = p.animal_stage_id
WHERE p.tenant_id = $1::uuid AND p.location_id = $2::uuid`, ssTenant, f.castroShed).Scan(&shedProfileStage); err != nil {
		t.Fatalf("read shed profile: %v", err)
	}
	if shedProfileStage != "K2" {
		t.Fatalf("shed profile = %q, want K2 -- a pen edit must not rewrite the SHED cohort that shifting reads", shedProfileStage)
	}
}

// TestReclassifyShedStageConfigureEmptyRecordsTheTagWithoutAnimals pins the two intents the
// ConfigureEmpty flag separates.
//
// Without it, an empty pen is the Counts Breakdown drawer's "you picked the wrong pen" error. With
// it, the Sheds directory records a tag for a pen standing empty before animals arrive -- 12 of the
// live tenant's 116 pens are in that state -- and reports zero animals reclassified rather than
// pretending it moved some.
func TestReclassifyShedStageConfigureEmptyRecordsTheTagWithoutAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)

	refused := reclassifyCmd(f.castroShed, "1", "Mother", "key-empty-refused")
	if _, err := repo.ReclassifyShedStage(ctx, refused); !errors.Is(err, ports.ErrReclassifyEmptyScope) {
		t.Fatalf("err = %v, want ErrReclassifyEmptyScope -- the drawer's intent must still fail closed", err)
	}
	if got := penCohort(t, ctx, pool, f.castroShed, "1"); got != "" {
		t.Fatalf("pen cohort = %q after a REFUSED command, want empty -- the failed write left state behind", got)
	}

	accepted := reclassifyCmd(f.castroShed, "1", "Mother", "key-empty-configured")
	accepted.ConfigureEmpty = true
	result, err := repo.ReclassifyShedStage(ctx, accepted)
	if err != nil {
		t.Fatalf("configure empty pen: %v", err)
	}
	if result.Reclassified != 0 || result.TotalLive != 0 {
		t.Fatalf("reclassified=%d total=%d, want 0/0 -- an empty pen must not report animals it did not move",
			result.Reclassified, result.TotalLive)
	}
	if got := penCohort(t, ctx, pool, f.castroShed, "1"); got != "Mother" {
		t.Fatalf("pen cohort = %q, want Mother -- configuring an empty pen recorded nothing", got)
	}
}
