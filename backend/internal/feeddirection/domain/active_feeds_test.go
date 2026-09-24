package domain

import "testing"

func TestDropInactiveFeedsRemovesRetiredLinesButNeverEmptiesASessionOrPen(t *testing.T) {
	snapshot := ConfigSnapshot{
		FeedItems: []FeedItem{{Key: "dry_masoor_bhusa"}, {Key: "mesha_kids_concentrate"}},
		Sessions: []SessionTemplate{
			{SessionNo: 1, Items: []FeedItem{{Key: "dry_masoor_bhusa"}, {Key: "baking_soda"}}},
			// Only a retired feed: left as authored rather than emptied.
			{SessionNo: 2, Items: []FeedItem{{Key: "baking_soda"}}},
		},
		ExperimentByLocation: map[string][]ExperimentCell{
			"pen-a": {{FeedItemKey: "mesha_kids_concentrate"}, {FeedItemKey: "concentrate"}},
			"pen-b": {{FeedItemKey: "concentrate"}},
		},
	}
	DropInactiveFeeds(&snapshot)

	if got := snapshot.Sessions[0].Items; len(got) != 1 || got[0].Key != "dry_masoor_bhusa" {
		t.Errorf("session 1 = %+v, want only the active feed", got)
	}
	if got := snapshot.Sessions[1].Items; len(got) != 1 || got[0].Key != "baking_soda" {
		t.Errorf("session 2 must not be emptied: %+v", got)
	}
	if got := snapshot.ExperimentByLocation["pen-a"]; len(got) != 1 || got[0].FeedItemKey != "mesha_kids_concentrate" {
		t.Errorf("pen-a = %+v, want only the active feed", got)
	}
	if got := snapshot.ExperimentByLocation["pen-b"]; len(got) != 1 {
		t.Errorf("pen-b must stay an experiment pen, not be emptied: %+v", got)
	}
}
