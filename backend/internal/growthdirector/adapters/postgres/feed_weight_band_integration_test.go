package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/growthdirector/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial coverage for the feed-by-weight-band read (maintainer request 2026-09-18).
// Every case below is a way the SQL can be quietly wrong while still returning plausible
// rows: a session duplicate doubling a pen's kg, a withdrawn lump weigh winning over the
// live one, a per-animal pen multiplying its feed rollup by every band, a second park's
// sheet leaking into the first park's scope, an animal weighed once counting as if it were
// on the General tab, or a sold animal eating today's feed.
func TestFeedWeightBandOneToManyPageBoundaryParkScopeStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedGrowthDirectorFixture(t, ctx, pool)

	const (
		issueOld    = "66666666-6666-4666-8666-666666660001"
		issueNormal = "66666666-6666-4666-8666-666666660002"
		issueExp    = "66666666-6666-4666-8666-666666660003"
		otherPark   = "00000000-0000-4000-8000-000000003002"
		issueOther  = "66666666-6666-4666-8666-666666660005"
	)
	issue := func(id, park, day, workflow, state string, lockedAt time.Time) {
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issues (feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at, locked_at, generation_input_fingerprint, idempotency_key, request_fingerprint, source_contract, source_contract_version, generated_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5, $6, $7::timestamptz, CASE WHEN $6 = 'locked' THEN $7::timestamptz ELSE NULL END, 'fp:' || $1, 'idem:' || $1, 'fp:' || $1, 'growthdirector-test', '1', 'test')`,
			id, gdTenant, park, day, workflow, state, lockedAt)
	}
	// An OLDER locked day must lose to the latest day even though it is locked.
	issue(issueOld, gdPark, "2026-07-14", "normal", "locked", day(13, 10))
	// The latest day: a normal issue and an experiment issue (the live unique index
	// allows one live issue per park, day and workflow, so the DISTINCT ON in the read
	// is defensive rather than exercised here).
	issue(issueNormal, gdPark, "2026-07-15", "normal", "locked", day(14, 10))
	issue(issueExp, gdPark, "2026-07-15", "experiment", "locked", day(14, 10))
	// The other park's sheet on the same day, out of scope for a park-A read.
	issue(issueOther, otherPark, "2026-07-15", "normal", "locked", day(14, 10))

	row := func(issueID, park, shedLabel, partition, tag, ration, arm, breed, workflow, item string, session int, kg, grams float64) {
		execGD(t, ctx, pool, `
INSERT INTO feed_direction_issue_rows (tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, partition_label, shed_tag, ration_group, experiment_arm, breed, session_no, head_count, head_count_informational, workflow, feed_item_label, quantity_kg, grams_per_head, session_total_kg, overdue_pending, row_seq, item_seq)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'CBE', $4::uuid, $5, $6, $7, $8, $9, $10, $11, 10, false, $12, $13, $14, $15, $14, false, 0, 1)`,
			gdTenant, issueID, park, gdShedG, shedLabel, partition, tag, ration, arm, breed, session, workflow, item, kg, grams)
	}
	// Gandhi 1 - Part 1 (the pen the fixture's scanned buckets weigh): TWO sessions of the
	// same item collapse to one item summing kg, plus a second item. 6 + 6 + 4 = 16 kg/day.
	row(issueNormal, gdPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Beetal x Sojat", "normal", "Mesha Kids Concentrate", 1, 6.0, 300)
	row(issueNormal, gdPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Beetal x Sojat", "normal", "Mesha Kids Concentrate", 2, 6.0, 300)
	row(issueNormal, gdPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Beetal x Sojat", "normal", "Dry Masoor Bhusa", 1, 4.0, 200)
	// A ZERO row on the same pen is not a positive row and must not appear as an item.
	row(issueNormal, gdPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Beetal x Sojat", "normal", "Baking Soda", 1, 0, 0)
	// The older locked day feeds the same pen a different amount: must be ignored.
	row(issueOld, gdPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Beetal x Sojat", "normal", "Mesha Kids Concentrate", 1, 77.0, 777)
	// Lump 1 (weighed whole in the fixture): an EXPERIMENT rollup with a numeric
	// partition, so the pen label composes with a space -- "Lump 1" is stored as shed
	// "Lump" + partition "1" here, the Castro shape.
	row(issueExp, gdPark, "Lump", "1", "K3", "Kid", "Arm A", "Sojat", "experiment", "Mesha Kids Concentrate", 1, 3.0, 120)
	// A fed pen nobody has weighed: a rollup with no evidence.
	row(issueNormal, gdPark, "Sumathi 1", "Part 4", "F2-Female", "Fattening", "", "Sojat", "normal", "Mesha Kids Concentrate", 1, 5.0, 250)
	// The other park's row: out of scope.
	row(issueOther, otherPark, "Gandhi 1", "Part 1", "F2-Male", "Fattening", "", "Sojat", "normal", "Mesha Kids Concentrate", 1, 50.0, 500)

	// Per-animal evidence on Gandhi 1 - Part 1, at the GENERAL TAB'S GRAIN: an animal counts
	// only with a prior weigh on an earlier date, banded on its latest. TAG-A 14 then 21 ->
	// 20_25; TAG-B 15 then 16 -> 15_20; TAG-C 17 then 18 -> 15_20; TAG-D is weighed ONCE and
	// must not appear anywhere, however heavy. Two bands come out.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-A", 14.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-A", 21.0, day(15, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-B", 15.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-B", 16.0, day(15, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-C", 17.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-C", 18.0, day(15, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-D", 36.0, day(15, 6), "pending")
	// STATUS BUCKETS: every verification status the table allows (pending, verified, rework)
	// is a real weight on the General tab and therefore here -- an unverified or bounced clip
	// is still a measurement. TAG-E (pending then rework) and TAG-F (rework then verified)
	// both count, both in 30_35.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-E", 20.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-E", 33.0, day(15, 6), "rework")
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-F", 20.0, day(8, 6), "rework")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-F", 33.0, day(15, 6), "verified")
	// GENDER FROM THE REGISTER: TAG-A resolves to a male, TAG-B to a female, TAG-C to
	// nothing. The 15_20 band therefore reads 1F (TAG-C unresolved counts nowhere) and
	// 20_25 reads 1M. TAG-B's goat is then SOLD inside the window: by default it drops
	// out of the 15_20 head count and average (leaving TAG-C alone at 18 kg) and rides
	// on the row as "+1 sold"; asked for, it counts again.
	seedGoatWithTag(t, ctx, pool, "77777777-0000-4000-8000-000000000601", "0601", "TAG-A", "Sojat", "male")
	seedGoatWithTag(t, ctx, pool, "77777777-0000-4000-8000-000000000602", "0602", "TAG-B", "Sojat", "female")
	execGD(t, ctx, pool, `UPDATE goats SET exited_at = $3::timestamptz, exit_reason = 'sold', lifecycle_status = 'sold' WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		gdTenant, "77777777-0000-4000-8000-000000000602", day(20, 9))
	// EXIT BUCKETS (Codex P1, 2026-09-18): an exit is ANY goats.exited_at, not only sold /
	// died. TAG-G (female, 22 then 23 kg -> 20_25) leaves the register as an INACTIVE record
	// with a blank reason: it must count as exited (other), never as died, and never vanish.
	seedScan(t, ctx, pool, gdBucketW1G, "TAG-G", 22.0, day(8, 6), "pending")
	seedScan(t, ctx, pool, gdBucketW2G, "TAG-G", 23.0, day(15, 6), "pending")
	seedGoatWithTag(t, ctx, pool, "77777777-0000-4000-8000-000000000603", "0603", "TAG-G", "Sojat", "female")
	execGD(t, ctx, pool, `UPDATE goats SET exited_at = $3::timestamptz, exit_reason = NULL, lifecycle_status = 'inactive' WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		gdTenant, "77777777-0000-4000-8000-000000000603", day(21, 9))
	// Lump 1's residents: 25 animals is the frozen census; the register holds three
	// females placed there (fewer than the census, which is the honest state after
	// moves) and nothing else, so the pen reads Female. The location is the undivided
	// "Lump 1" (the trailing digit is part of the name), so its partition is 'whole'
	// and the placement label composes to the bare name.
	for i, id := range []string{"77777777-0000-4000-8000-000000000611", "77777777-0000-4000-8000-000000000612", "77777777-0000-4000-8000-000000000613"} {
		seedGoatWithTag(t, ctx, pool, id, fmt.Sprintf("06%d", 11+i), fmt.Sprintf("LUMP-%d", i), "Sojat", "female")
		execGD(t, ctx, pool, `UPDATE goats SET current_location_id = $3::uuid, shed_id = $3::uuid WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, gdTenant, id, gdShedLump)
		execGD(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'whole', 'Lump 1')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`, gdTenant, id, gdShedLump)
	}
	// Pen-average evidence on Lump 1, at the General tab's grain: a pen counts only when
	// weighed on TWO dates in the window, on its latest live row. A W1 bucket for the same
	// pen carries the earlier weigh; W2 carries the live latest (20.8 -> 20_25, 25 head)
	// and a NEWER withdrawn row (27.5) that must lose.
	const bucketW1L = "77777777-0000-4000-8000-000000000306"
	execGD(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Lump 1', 'per_shed_partition', $5::uuid, 0)`,
		bucketW1L, gdCampaignW1, gdTenant, gdShedLump, gdOperator)
	execGD(t, ctx, pool, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count, proof_artifact_id, recorded_by, idempotency_key, accepted_at, withdrawn_at)
VALUES
  ($1::uuid, $8::uuid, $9::uuid, 450.0, 18.0, 25, $4::uuid, $5::uuid, 'fb:lump:first', $10::timestamptz, NULL),
  ($1::uuid, $2::uuid, $3::uuid, 520.0, 20.8, 25, $4::uuid, $5::uuid, 'fb:lump:live', $6::timestamptz, NULL),
  ($1::uuid, $2::uuid, $3::uuid, 687.5, 27.5, 25, $4::uuid, $5::uuid, 'fb:lump:withdrawn-newer', $7::timestamptz, $7::timestamptz)`,
		gdTenant, gdCampaignW2, gdBucketW2L, gdProof, gdOperator, day(15, 6), day(16, 6), gdCampaignW1, bucketW1L, day(8, 6))

	from, to := gdWindow()
	repo := NewRepository(pool, 30*time.Second)
	got, err := repo.GetFeedWeightBandSource(ctx, gdTenant, []string{gdPark}, from, to, "", "", "")
	if err != nil {
		t.Fatalf("GetFeedWeightBandSource: %v", err)
	}
	if got.FeedDay != "2026-07-15" {
		t.Fatalf("feed day: want the latest locked day 2026-07-15, got %q", got.FeedDay)
	}
	// Positive rows: 3 on Part 1 + 1 on Lump 1 + 1 on Sumathi = 5. Not the zero row, not
	// the older day, not the other park.
	if got.PositiveRows != 5 || got.CollapsedItems != 4 {
		t.Fatalf("stages: want 5 positive rows collapsing to 4 items, got %d / %d", got.PositiveRows, got.CollapsedItems)
	}
	if len(got.Rollups) != 3 {
		t.Fatalf("rollups: want 3 (Part 1, Lump 1, Sumathi), got %d: %+v", len(got.Rollups), got.Rollups)
	}
	byPen := map[string]int{}
	for i, r := range got.Rollups {
		byPen[r.Pen] = i
	}
	part1, ok := got.Rollups[byPen["Gandhi 1 - Part 1"]], true
	if _, found := byPen["Gandhi 1 - Part 1"]; !found {
		ok = false
	}
	if !ok {
		t.Fatalf("no rollup for Gandhi 1 - Part 1: %+v", got.Rollups)
	}
	if part1.KgPerDay != 16.0 || len(part1.Items) != 2 {
		t.Fatalf("Part 1: want 16 kg/day over 2 items (sessions summed, zero row dropped), got %v over %d", part1.KgPerDay, len(part1.Items))
	}
	// ONE-TO-MANY: a per-animal pen fans out to one evidence row per band and no more --
	// the feed rollup is carried once per band, never once per scanned tag.
	if len(part1.Evidence) != 3 {
		t.Fatalf("Part 1 evidence: want 3 bands (15_20, 20_25, 30_35), got %+v", part1.Evidence)
	}
	bands := map[string]ports.FeedWeightEvidence{}
	for _, e := range part1.Evidence {
		if e.Source != "per_animal" {
			t.Fatalf("Part 1 evidence must be per_animal, got %+v", e)
		}
		bands[e.Band] = e
	}
	// 15_20 holds TAG-C only by default: TAG-B was sold and rides as exited=1, and its
	// female count leaves with it; once-weighed TAG-D is nowhere.
	if bands["15_20"].Animals != 1 || bands["15_20"].ExitedAnimals != 1 || bands["20_25"].Animals != 1 || bands["20_25"].ExitedAnimals != 1 || bands["30_35"].Animals != 2 {
		t.Fatalf("Part 1 bands: want 15_20=1 (+1 exited), 20_25=1 (+1 exited) and 30_35=2 (pending/rework/verified all count), got %v", bands)
	}
	// The buckets: TAG-B sold, TAG-G other (inactive, blank reason) -- and other is NOT died.
	if e := bands["15_20"]; e.ExitedSold != 1 || e.ExitedDied != 0 {
		t.Fatalf("15_20 exit buckets: want sold=1 died=0, got %+v", e)
	}
	if e := bands["20_25"]; e.ExitedSold != 0 || e.ExitedDied != 0 || e.ExitedAnimals-e.ExitedSold-e.ExitedDied != 1 {
		t.Fatalf("20_25 exit buckets: an inactive record is exited=1 in the OTHER bucket, got %+v", e)
	}
	if bands["15_20"].AverageWeightKg != 18.0 {
		t.Fatalf("15_20 average must exclude the sold animal (18.0), got %v", bands["15_20"].AverageWeightKg)
	}
	if bands["15_20"].FemaleCount != 0 || bands["15_20"].MaleCount != 0 || bands["20_25"].MaleCount != 1 || bands["20_25"].FemaleCount != 0 {
		t.Fatalf("Part 1 sexes must come from the register per band over on-farm animals (15_20: TAG-C unresolved; 20_25: 1M), got %+v", bands)
	}
	// The General-tab figures: five paired animals (sold TAG-B included, as that tab counts it;
	// once-weighed TAG-D excluded), 25 lump animals in a pen weighed on two dates.
	if got.IndividualAnimalsWeighed != 6 || got.LumpSumAnimalsWeighed != 25 {
		t.Fatalf("weighing-side totals must match the General tab grain (6 individual, 25 lump), got %d / %d", got.IndividualAnimalsWeighed, got.LumpSumAnimalsWeighed)
	}
	// Both exits are listed with their last weigh in the window, newest exit first; the
	// inactive one carries its stored (blank) reason and lifecycle so the screen can bucket it.
	if len(got.Exited) != 2 || got.Exited[0].Tag != "TAG-G" || got.Exited[0].LifecycleStatus != "inactive" || got.Exited[0].ExitReason != "" || got.Exited[0].LastWeightKg != 23.0 {
		t.Fatalf("exit list: want TAG-G (inactive, other) first, got %+v", got.Exited)
	}
	if x := got.Exited[1]; x.Tag != "TAG-B" || x.Pen != "Gandhi 1 - Part 1" || x.LifecycleStatus != "sold" || x.LastWeighedAt == nil || x.LastWeightKg != 16.0 {
		t.Fatalf("exit list: want TAG-B sold, last weighed 16.0 in Gandhi 1 - Part 1, got %+v", x)
	}
	// The same row carries the include-exited variant: TAG-B counted again, 15_20 = 2 at
	// 17.0 with 1F.
	if e := bands["15_20"]; e.AnimalsAll != 2 || e.AverageWeightKgAll != 17.0 || e.FemaleCountAll != 1 || e.ExitedAnimals != 1 {
		t.Fatalf("all-variant: want 15_20 = 2 at 17.0 with 1F (+1 exited), got %+v", e)
	}
	lump := got.Rollups[byPen["Lump 1"]]
	if len(lump.Evidence) != 1 || lump.Evidence[0].Source != "pen_average" || lump.Evidence[0].Band != "20_25" || lump.Evidence[0].Animals != 25 {
		t.Fatalf("Lump 1: want one pen_average row in 20_25 for 25 animals (live row, not the newer withdrawn one), got %+v", lump.Evidence)
	}
	if lump.Evidence[0].FemaleCount != 3 || lump.Evidence[0].MaleCount != 0 {
		t.Fatalf("Lump 1 sex must come from the goats placed in the pen (3F, 0M), got %+v", lump.Evidence[0])
	}
	if lump.Workflow != "experiment" || lump.ExperimentArm != "Arm A" {
		t.Fatalf("Lump 1 must keep its experiment workflow and arm: %+v", lump)
	}
	if len(got.Rollups[byPen["Sumathi 1 - Part 4"]].Evidence) != 0 {
		t.Fatalf("an unweighed pen must carry no evidence: %+v", got.Rollups[byPen["Sumathi 1 - Part 4"]])
	}

	// SEX FILTER through the weighing module's own resolver: "male" keeps TAG-A's 20_25 row
	// and drops the 15_20 band (TAG-C resolves to no animal, so it is claimed by neither
	// side); the lump pen's residents are all female, so it drops too.
	male, err := repo.GetFeedWeightBandSource(ctx, gdTenant, []string{gdPark}, from, to, "male", "", "")
	if err != nil {
		t.Fatalf("GetFeedWeightBandSource (male): %v", err)
	}
	if male.IndividualAnimalsWeighed != 1 || male.LumpSumAnimalsWeighed != 0 {
		t.Fatalf("male: want 1 individual and 0 lump at the General tab grain, got %d / %d", male.IndividualAnimalsWeighed, male.LumpSumAnimalsWeighed)
	}
	for _, r := range male.Rollups {
		for _, e := range r.Evidence {
			if r.Pen != "Gandhi 1 - Part 1" || e.Band != "20_25" || e.Animals != 1 {
				t.Fatalf("male: only TAG-A's 20_25 row may survive (TAG-C/E/F resolve to no animal), got %s %+v", r.Pen, e)
			}
		}
	}

	// PARK SCOPE: the other park's sheet answers only when that park is in scope, and then
	// its Gandhi 1 - Part 1 is a DIFFERENT pen from park A's (keyed on park_id).
	both, err := repo.GetFeedWeightBandSource(ctx, gdTenant, []string{gdPark, otherPark}, from, to, "", "", "")
	if err != nil {
		t.Fatalf("GetFeedWeightBandSource (both parks): %v", err)
	}
	if both.PositiveRows != 6 || len(both.Rollups) != 4 {
		t.Fatalf("both parks: want 6 positive rows and 4 rollups, got %d / %d", both.PositiveRows, len(both.Rollups))
	}
	// The window bounds the WEIGHINGS only: a window ending before every weigh leaves the
	// feed rollups in place with no evidence, never drops the sheet.
	windowed, err := repo.GetFeedWeightBandSource(ctx, gdTenant, []string{gdPark}, day(1, 0), day(2, 0), "", "", "")
	if err != nil {
		t.Fatalf("GetFeedWeightBandSource (window): %v", err)
	}
	if len(windowed.Rollups) != 3 {
		t.Fatalf("windowed: rollups must survive an empty weighing window, got %d", len(windowed.Rollups))
	}
	for _, r := range windowed.Rollups {
		if len(r.Evidence) != 0 {
			t.Fatalf("windowed: no weigh falls in the window, got evidence on %s: %+v", r.Pen, r.Evidence)
		}
	}
}
