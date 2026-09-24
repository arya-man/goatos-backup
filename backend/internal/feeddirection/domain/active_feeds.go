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
// NEVER EMPTIES A SESSION OR A PEN. An empty session blocks every pen (see SessionTemplate.Items),
// and an experiment pen with no cells stops being an experiment pen -- the planner would silently
// feed it from the ration grid instead. Where dropping would empty one, it is left exactly as
// authored, so this can only remove lines, never change a workflow or unblock/block a pen.
func DropInactiveFeeds(snapshot *ConfigSnapshot) {
	active := make(map[string]bool, len(snapshot.FeedItems))
	for _, item := range snapshot.FeedItems {
		active[item.Key] = true
	}
	for i := range snapshot.Sessions {
		kept := make([]FeedItem, 0, len(snapshot.Sessions[i].Items))
		for _, item := range snapshot.Sessions[i].Items {
			if active[item.Key] {
				kept = append(kept, item)
			}
		}
		if len(kept) > 0 {
			snapshot.Sessions[i].Items = kept
		}
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
		}
	}
}
