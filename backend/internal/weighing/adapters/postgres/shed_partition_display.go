package postgres

import (
	"regexp"
	"strings"

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
var weighingPartitionSuffix = regexp.MustCompile(`(?i)^(.+?)\s*-\s*part\s+(\S+)$|^(.+?)\s+(\d+)$`)

// splitShedPartitionName parses a shed catalog display name into (parent shed base name,
// partition label). partitionLabel is "" when the name carries no recognizable partition suffix
// -- ordinary non-partitioned shed names ("Yashoda", "Ho Chi Minh") never match and pass through
// unchanged.
func splitShedPartitionName(name string) (parentShedName, partitionLabel string) {
	trimmed := strings.TrimSpace(name)
	m := weighingPartitionSuffix.FindStringSubmatch(trimmed)
	if m == nil {
		return trimmed, ""
	}
	if m[1] != "" {
		return strings.TrimSpace(m[1]), "Part " + m[2]
	}
	return strings.TrimSpace(m[3]), m[4]
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
