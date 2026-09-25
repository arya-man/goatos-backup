package domain

import (
	"strings"
	"testing"
)

// A retired feed never reaches a sheet, and a session or experiment pen left with nothing to feed
// BLOCKS with that reason (maintainer decision 2026-09-25). The previous rule left an all-retired
// session or pen exactly as authored, so the sheet kept serving a retired feed Feed Config no
// longer showed.
func TestDropInactiveFeedsRemovesEveryRetiredLineAndMarksWhatItEmptied(t *testing.T) {
	snapshot := ConfigSnapshot{
		FeedItems: []FeedItem{{Key: "dry_masoor_bhusa"}, {Key: "mesha_kids_concentrate"}},
		Sessions: []SessionTemplate{
			{SessionNo: 1, Items: []FeedItem{{Key: "dry_masoor_bhusa"}, {Key: "baking_soda"}}},
			{SessionNo: 2, Items: []FeedItem{{Key: "baking_soda"}}},
			// Authored empty: still the "no feed items" gap, not a retired one.
			{SessionNo: 3},
		},
		ExperimentByLocation: map[string][]ExperimentCell{
			"pen-a": {{FeedItemKey: "mesha_kids_concentrate"}, {FeedItemKey: "concentrate"}},
			"pen-b": {{FeedItemKey: "concentrate"}},
		},
	}
	DropInactiveFeeds(&snapshot)

	if got := snapshot.Sessions[0]; len(got.Items) != 1 || got.Items[0].Key != "dry_masoor_bhusa" || got.AllFeedsRetired {
		t.Errorf("session 1 = %+v, want only the active feed and no retired flag", got)
	}
	if got := snapshot.Sessions[1]; len(got.Items) != 0 || !got.AllFeedsRetired {
		t.Errorf("session 2 = %+v, want emptied and flagged all-retired", got)
	}
	if got := snapshot.Sessions[2]; got.AllFeedsRetired {
		t.Errorf("an authored-empty session is not a retired one: %+v", got)
	}
	if got := snapshot.ExperimentByLocation["pen-a"]; len(got) != 1 || got[0].FeedItemKey != "mesha_kids_concentrate" {
		t.Errorf("pen-a = %+v, want only the active feed", got)
	}
	if _, ok := snapshot.ExperimentByLocation["pen-b"]; ok {
		t.Errorf("pen-b still carries a retired feed: %+v", snapshot.ExperimentByLocation["pen-b"])
	}
	if !snapshot.ExperimentAllRetired["pen-b"] || snapshot.ExperimentAllRetired["pen-a"] {
		t.Errorf("ExperimentAllRetired = %+v, want only pen-b", snapshot.ExperimentAllRetired)
	}
}

// The production path: a session whose only feed is retired serves NO line of it and blocks every
// pen with the retired reason -- never the retired feed at any quantity.
func TestRetiredOnlySessionBlocksItsPensAndNeverServesTheRetiredFeed(t *testing.T) {
	cfg := withRate(testConfig(), "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "300")
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Hybrid", "200")
	cfg.Sessions[1].Items = []FeedItem{{Label: "Baking Soda", Key: "baking_soda"}}
	DropInactiveFeeds(&cfg)

	pen := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10})
	rows := generate(cfg, pen, 2)
	if len(rows) != 1 || !rows[0].Blocked {
		t.Fatalf("rows = %+v, want one blocked evening row", rows)
	}
	for _, item := range rows[0].Items {
		if item.FeedItem == "Baking Soda" {
			t.Errorf("the retired feed reached the sheet: %+v", item)
		}
		if item.Status != QuantityBlocked || item.QuantityKg != nil {
			t.Errorf("%s = %+v, want blocked with no number", item.FeedItem, item)
		}
		if item.BlockedReason == nil || item.BlockedReason.Code != BlockReasonAllFeedsRetired ||
			!strings.Contains(item.BlockedReason.Detail, "is retired") {
			t.Errorf("%s reason = %+v, want %s", item.FeedItem, item.BlockedReason, BlockReasonAllFeedsRetired)
		}
	}
	// The morning session, whose feeds are active, is untouched.
	if morning := generate(cfg, pen, 1); len(morning) != 1 || morning[0].Blocked {
		t.Errorf("morning = %+v, want an unblocked row", morning)
	}
}

// An experiment pen whose every feed is retired STAYS an experiment pen and blocks. Falling through
// to the normal planner would feed it the ration grid's 300 g, a quantity nobody authored for it.
func TestRetiredOnlyExperimentPenBlocksInsteadOfFallingBackToTheRationGrid(t *testing.T) {
	cfg := withRate(testConfig(), "Beetal/Sirohi", "Non-Pregnant", "Concentrate", "300")
	cfg = withRate(cfg, "Beetal/Sirohi", "Non-Pregnant", "Hybrid", "200")
	pen := shed(ShedGrain{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10})
	pen.PartitionLabel = "2"
	cfg.ExperimentByLocation[ExperimentLocationKey(pen.ShedID, pen.PartitionLabel)] = []ExperimentCell{{
		FeedItemLabel: "Old Feed", FeedItemKey: "old_feed", Basis: ExperimentBasisGramsPerHead,
		GramsPerHead: "250", Category: "Arm A",
	}}
	DropInactiveFeeds(&cfg)

	rows := generate(cfg, pen, 0)
	if len(rows) != len(cfg.Sessions) {
		t.Fatalf("rows = %d, want one per session", len(rows))
	}
	for _, row := range rows {
		if row.Workflow != WorkflowExperiment {
			t.Errorf("session %d workflow = %q, want experiment (it fell back to the ration grid)", row.SessionNo, row.Workflow)
		}
		if !row.Blocked || row.SessionTotalKg != GramsToKgString(0) {
			t.Errorf("session %d = blocked %v total %s, want blocked with nothing to pack", row.SessionNo, row.Blocked, row.SessionTotalKg)
		}
		for _, item := range row.Items {
			if item.FeedItem == "Old Feed" || item.QuantityKg != nil {
				t.Errorf("session %d item %+v, want no retired feed and no number", row.SessionNo, item)
			}
			if item.BlockedReason == nil || item.BlockedReason.Code != BlockReasonAllFeedsRetired ||
				!strings.Contains(item.BlockedReason.Detail, "Shed 1 2") {
				t.Errorf("session %d reason = %+v, want the retired reason naming the pen", row.SessionNo, item.BlockedReason)
			}
		}
	}
}
