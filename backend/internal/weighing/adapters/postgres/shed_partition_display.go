package postgres

import (
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Weighing is an ISOLATED module (see AGENTS.md "Weighing Is ISOLATED — No Herd, No
// Vaccination, No Exceptions"): it must never join goats, goat_identifiers, or
// goat_shed_partitions to learn a shed's partition. The only location-shaped data it is allowed
// to touch is `locations` itself (already flattened into weighing_campaign_sheds.display_name /
// PlannerShed.Name at bucket-creation time).
//
// A partition-bearing shed's catalog name already carries the operational suffix in EXACTLY the
// two conventions oploc documents ("Castro 2", "Godel 1 - Part 3"), because that name is sourced
// from the same `locations` catalog the vaccination/counts side reads. This parses that suffix
// back out so weighing can carry parent-shed name + partition label + a normalized operational
// display without reading a single animal-scoped row.
// splitShedPartitionName delegates to oploc.SplitShedPartitionName, the single Go home of the
// catalog naming rule. It was a private regex here until 2026-08-07; the feed seeder needed the
// same parse, and two copies of this convention is exactly how one caller comes to believe
// "Godel 1 - Part 3" is shed "Godel" while the other reads shed "Godel 1".
//
// Weighing's isolation is untouched: oploc is a platform primitive over NAMES, and this still
// reads only the already-flattened locations catalog name -- no goats, no goat_shed_partitions.
func splitShedPartitionName(name string) (parentShedName, partitionLabel string) {
	return oploc.SplitShedPartitionName(name)
}

// applyShedPartitionDisplay stamps ParentShedName/PartitionLabel/OperationalLocationDisplay on a
// CampaignShed from its already-loaded DisplayName. OperationalLocationDisplay is composed
// through oploc.OperationalLocation.Display() so it follows the exact same rendering rule as
// every other module and can never show the "whole" sentinel.
func applyShedPartitionDisplay(shed *domain.CampaignShed) {
	parent, partition := splitShedPartitionName(shed.DisplayName)
	shed.ParentShedName = parent
	shed.PartitionLabel = partition
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedID:         shed.LocationID,
		ShedName:       parent,
		PartitionLabel: partition,
	}.Display()
}

// applyPlannerShedPartitionDisplay is the PlannerShed twin of applyShedPartitionDisplay.
func applyPlannerShedPartitionDisplay(shed *domain.PlannerShed) {
	parent, partition := splitShedPartitionName(shed.Name)
	shed.ParentShedName = parent
	shed.PartitionLabel = partition
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedID:         shed.LocationID,
		ShedName:       parent,
		PartitionLabel: partition,
	}.Display()
}
