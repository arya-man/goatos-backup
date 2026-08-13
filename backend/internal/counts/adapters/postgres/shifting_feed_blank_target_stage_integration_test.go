package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A high-priority movement into an EMPTY (or mixed, or non-writable) destination pen is raised with
// a BLANK target_management_stage. That is not a missing input: counts/domain.ResolveShiftingDestinationStage
// returns "" to mean "each animal keeps its own stage", and the phone never asks the raiser for one
// (maintainer decision 2026-08-03). Pricing the ration off that blank field hard-blocked every such
// movement with "selected destination management stage is missing" -- naming a choice the operator
// was never offered. The ration is now priced against each animal's OWN current stage, which is
// exactly the cohort it keeps (maintainer decision 2026-08-12).
func TestHighPriorityShiftingPricesRationPerAnimalStageWhenTargetStageIsBlank(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	adultGoat := "00000000-0000-4000-8000-00000000d101"
	growerGoat := "00000000-0000-4000-8000-00000000d102"
	goatIDs := []string{adultGoat, growerGoat}
	// TWO cohorts in one movement, so a per-event stage cannot pass by accident: each animal must be
	// priced on its own grid and the two summed.
	seedApprovalGoatWithStage(t, ctx, pool, adultGoat, countsShedA, "Adult")
	seedApprovalGoatWithStage(t, ctx, pool, growerGoat, countsShedA, "Grower")
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "verify-high-blank-stage", goatIDs)
	seedBlankTargetStageFeedConfig(t, ctx, pool, goatIDs, eventID)
	if _, _, err := approveShifting(repo, ctx, "verify-high-blank-stage", approvalID, eventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	// The destination pen is empty, so the raise recorded no target stage at all.
	var rawTarget string
	if err := pool.QueryRow(ctx, `
SELECT coalesce(target_management_stage, '') FROM shifting_events
WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID).Scan(&rawTarget); err != nil {
		t.Fatalf("read target stage: %v", err)
	}
	if rawTarget != "" {
		t.Fatalf("fixture target_management_stage=%q, want blank -- this test is about the keep-current case", rawTarget)
	}

	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement: %v", err)
	}
	requirement := requirements[eventID]
	if requirement.Status != "ready" {
		t.Fatalf("status=%q blocked_reason=%q, want ready -- a blank target stage means keep-current, not missing config",
			requirement.Status, requirement.BlockedReason)
	}
	if len(requirement.Items) != 1 {
		t.Fatalf("items=%+v, want exactly one Concentrate instruction", requirement.Items)
	}
	// 250g for the Adult + 150g for the Grower. A single-stage fallback would read 500 or 300; a
	// destination-stage fallback would read one of those too. Only per-animal pricing gives 400.
	if got := requirement.Items[0].QuantityGrams; got != "400.0000" {
		t.Fatalf("quantity=%s, want 400.0000 (Adult 250g + Grower 150g priced on their own stages)", got)
	}
	if requirement.AnimalCount != 2 {
		t.Fatalf("animal_count=%d, want 2", requirement.AnimalCount)
	}
	if requirement.Fingerprint == "" {
		t.Fatal("fingerprint is empty; completion compares it and would reject the movement as changed config")
	}

	// The fingerprint must be STABLE across identical reads. Its string_agg used to order by breed
	// alone, which is no longer unique once one breed appears under two stages -- an unstable order
	// would tell the phone feed_config_changed for a config nobody touched.
	for i := 0; i < 5; i++ {
		again, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
		if err != nil {
			t.Fatalf("re-resolve feed requirement: %v", err)
		}
		if again[eventID].Fingerprint != requirement.Fingerprint {
			t.Fatalf("fingerprint changed between identical reads (%s vs %s) -- completion would fail with feed_config_changed",
				again[eventID].Fingerprint, requirement.Fingerprint)
		}
	}
}

// Experiment membership is owned by the complete destination operational location, not by its
// parent shed. A partitioned shed may mix ordinary and experiment pens: an experiment allocation
// for pen 1 must not block a high-priority movement into ordinary pen 10. This is the exact live
// Yashoda 10 failure from 2026-08-13.
func TestHighPriorityShiftingExperimentConfigIsScopedToDestinationPartition(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000d104"
	goatIDs := []string{goatID}
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "Adult")
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "verify-high-sibling-experiment", goatIDs)
	seedBlankTargetStageFeedConfig(t, ctx, pool, goatIDs, eventID)
	if _, _, err := approveShifting(repo, ctx, "verify-high-sibling-experiment", approvalID, eventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE shifting_events
SET destination_partition_label='10'
WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, countsTenant, eventID); err != nil {
		t.Fatalf("set destination partition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_experiment_config (
  tenant_id, park_id, shed_id, partition_label, feed_item_label,
  absolute_kg, head_count, experiment_category, status
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, '1', 'Experiment Concentrate',
  5, 10, 'Sibling pen experiment', 'active'
)`, countsTenant, countsPark, countsShedB); err != nil {
		t.Fatalf("seed sibling experiment: %v", err)
	}

	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement with sibling experiment: %v", err)
	}
	if requirement := requirements[eventID]; requirement.Status != "ready" {
		t.Fatalf("status=%q blocked_reason=%q, want ready -- experiment pen 1 must not leak onto destination pen 10",
			requirement.Status, requirement.BlockedReason)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO feed_experiment_config (
  tenant_id, park_id, shed_id, partition_label, feed_item_label,
  absolute_kg, head_count, experiment_category, status
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, '10', 'Experiment Concentrate',
  5, 10, 'Destination pen experiment', 'active'
)`, countsTenant, countsPark, countsShedB); err != nil {
		t.Fatalf("seed exact-destination experiment: %v", err)
	}
	requirements, err = loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement with exact experiment: %v", err)
	}
	requirement := requirements[eventID]
	if requirement.Status != "blocked" {
		t.Fatalf("status=%q, want blocked -- exact destination experiment must remain fail-closed", requirement.Status)
	}
	want := "destination shed uses experiment feed config; no stage-matched ration may be guessed"
	if requirement.BlockedReason != want {
		t.Fatalf("blocked_reason=%q, want %q", requirement.BlockedReason, want)
	}
}

// An animal with no stage on either side is a herd-data gap, and that DOES still block -- there is
// no cohort to price against. The message must not blame the raiser for a selection.
func TestHighPriorityShiftingStillBlocksWhenAnimalHasNoStageAtAll(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newRealIdentityApprovalRepo(t, pool)

	goatID := "00000000-0000-4000-8000-00000000d103"
	goatIDs := []string{goatID}
	seedApprovalGoatWithStage(t, ctx, pool, goatID, countsShedA, "")
	seedShedProfile(t, ctx, pool, countsShedB, "adult")

	eventID, approvalID := submitShiftingApproval(t, ctx, repo, "verify-high-no-stage", goatIDs)
	seedBlankTargetStageFeedConfig(t, ctx, pool, goatIDs, eventID)
	if _, _, err := approveShifting(repo, ctx, "verify-high-no-stage", approvalID, eventID, goatIDs); err != nil {
		t.Fatalf("approve shifting: %v", err)
	}

	requirements, err := loadShiftingFeedRequirements(ctx, pool, countsTenant, []string{eventID}, time.Now())
	if err != nil {
		t.Fatalf("resolve feed requirement: %v", err)
	}
	requirement := requirements[eventID]
	if requirement.Status != "blocked" {
		t.Fatalf("status=%q, want blocked -- an animal with no stage has no ration grid", requirement.Status)
	}
	want := "one or more movement animals have no management stage to price a ration against"
	if requirement.BlockedReason != want {
		t.Fatalf("blocked_reason=%q, want %q -- the operator must not be told they failed to select a stage", requirement.BlockedReason, want)
	}
}

// Same feed config as the shipped high-priority fixture, but the movement carries NO target stage
// (the empty-destination case) and the grid covers both Adult and Grower.
func seedBlankTargetStageFeedConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatIDs []string, eventID string) {
	t.Helper()
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE goats SET breed='Beetal' WHERE tenant_id=$1::uuid AND goat_id = ANY($2::uuid[])`, []any{countsTenant, goatIDs}},
		{`UPDATE shifting_events SET priority='high', management_stage_mode='keep_current', target_management_stage='' WHERE tenant_id=$1::uuid AND shifting_event_id=$2::uuid`, []any{countsTenant, eventID}},
		{`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status) VALUES ($1::uuid, 'Adult', 'adult', 'active')`, []any{countsTenant}},
		{`INSERT INTO feed_shed_tags (tenant_id, shed_tag_label, applies_to, status) VALUES ($1::uuid, 'Grower', 'adult', 'active')`, []any{countsTenant}},
		{`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label) VALUES ($1::uuid, 'Beetal', 'Beetal/Sirohi')`, []any{countsTenant}},
		{`INSERT INTO feed_session_templates (tenant_id, park_id, session_no, session_label, split_fraction, status) VALUES ($1::uuid, $2::uuid, 1, 'Morning', 1.0, 'active')`, []any{countsTenant, countsPark}},
		{`INSERT INTO feed_session_template_items (tenant_id, park_id, session_no, slot_no, feed_item_label, status, valid_from) VALUES ($1::uuid, $2::uuid, 1, 1, 'Concentrate', 'active', CURRENT_DATE)`, []any{countsTenant, countsPark}},
		{`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from) VALUES ($1::uuid, $2::uuid, 'Beetal/Sirohi', 'Adult', 'Concentrate', 250, CURRENT_DATE)`, []any{countsTenant, countsPark}},
		{`INSERT INTO feed_ration_rates (tenant_id, park_id, ration_group_label, shed_tag_label, feed_item_label, grams_per_head, valid_from) VALUES ($1::uuid, $2::uuid, 'Beetal/Sirohi', 'Grower', 'Concentrate', 150, CURRENT_DATE)`, []any{countsTenant, countsPark}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("seed blank-target-stage feed config: %v", err)
		}
	}
}
