package domain

// DropInactiveFeeds takes every feed the catalog does not list as ACTIVE off the sheet's session
// slots and experiment cells (maintainer decision 2026-09-24: a feed that is not an active item in
// Configuration > Items and categories is removed from the sheet completely). snapshot.FeedItems is
// the active catalog, so it is the membership test.
//
// Before this, a retired feed a session still declared (Baking Soda) or an experiment pen still
// carried (Concentrate, Mesha Adult Concentrate Sheep) rode onto every sheet, packing card and
// phone at 0 kg -- lines for a feed the farm no longer has.
//
// UNCONDITIONAL, AND AN EMPTIED SESSION OR PEN BLOCKS (maintainer decision 2026-09-25). This used
// to leave a session or pen whose feeds were ALL retired exactly as authored, so the sheet still
// served the retired feed while Feed Config -- which hides retired feeds -- showed nothing there.
// Now the retired feed always comes off, and:
//
//   - a session left empty is marked AllFeedsRetired; an empty session already blocks every pen it
//     serves (materializeRow), and the flag only names the reason;
//   - an experiment pen left empty is recorded in ExperimentAllRetired and STAYS an experiment pen,
//     blocked with that reason. Deleting its cells alone would make the planner treat it as a
//     normal pen and feed it from the ration grid, which nobody authored for it.
func DropInactiveFeeds(snapshot *ConfigSnapshot) {
	active := make(map[string]bool, len(snapshot.FeedItems))
	for _, item := range snapshot.FeedItems {
		active[item.Key] = true
	}
	for i := range snapshot.Sessions {
		declared := snapshot.Sessions[i].Items
		kept := make([]FeedItem, 0, len(declared))
		for _, item := range declared {
			if active[item.Key] {
				kept = append(kept, item)
			}
		}
		snapshot.Sessions[i].Items = kept
		snapshot.Sessions[i].AllFeedsRetired = len(declared) > 0 && len(kept) == 0
	}
	for key, cells := range snapshot.ExperimentByLocation {
		kept := make([]ExperimentCell, 0, len(cells))
		for _, cell := range cells {
			if active[cell.FeedItemKey] {
				kept = append(kept, cell)
			}
		}
		if len(kept) > 0 {
			snapshot.ExperimentByLocation[key] = kept
			continue
		}
		delete(snapshot.ExperimentByLocation, key)
		if len(cells) > 0 {
			if snapshot.ExperimentAllRetired == nil {
				snapshot.ExperimentAllRetired = map[string]bool{}
			}
			snapshot.ExperimentAllRetired[key] = true
		}
	}
}
