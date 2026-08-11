package postgres

import (
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
// from the same `locations` catalog the vaccination/counts side reads. Weighing keeps that exact
// name as the operator identity; it only uses a stored legacy partition_label when reading rows
// written before the exact-shed cutover.
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
	parent := strings.TrimSpace(shed.DisplayName)
	shed.ParentShedName = parent
	shed.PartitionLabel = ""
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedID:   shed.LocationID,
		ShedName: parent,
	}.Display()
}

func applyShedPartitionDisplayWithStoredLabel(shed *domain.CampaignShed, storedPartitionLabel string) {
	applyShedPartitionDisplay(shed)
	partition := strings.TrimSpace(storedPartitionLabel)
	if partition == "" {
		return
	}
	parent := parentShedNameFromStoredPartitionDisplay(shed.DisplayName, shed.ParentShedName, partition)
	shed.ParentShedName = parent
	shed.PartitionLabel = partition
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedID:         shed.LocationID,
		ShedName:       parent,
		PartitionLabel: partition,
	}.Display()
}

func parentShedNameFromStoredPartitionDisplay(displayName, parsedParent, partition string) string {
	display := strings.TrimSpace(displayName)
	partition = strings.TrimSpace(partition)
	for _, suffix := range []string{" - " + partition, " " + partition} {
		if partition != "" && strings.HasSuffix(display, suffix) {
			parent := strings.TrimSpace(strings.TrimSuffix(display, suffix))
			if parent != "" {
				return parent
			}
		}
	}
	if strings.TrimSpace(parsedParent) != "" {
		return strings.TrimSpace(parsedParent)
	}
	return display
}

// applyPlannerShedPartitionDisplay is the PlannerShed twin of applyShedPartitionDisplay.
func applyPlannerShedPartitionDisplay(shed *domain.PlannerShed) {
	parent := strings.TrimSpace(shed.Name)
	shed.ParentShedName = parent
	shed.PartitionLabel = ""
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedID:   shed.LocationID,
		ShedName: parent,
	}.Display()
}

func applyPlannerShedOperationalDisplay(shed *domain.PlannerShed) {
	parent := strings.TrimSpace(shed.ParentShedName)
	if parent == "" {
		parent = strings.TrimSpace(shed.Name)
	}
	partition := strings.TrimSpace(shed.PartitionLabel)
	display := operationalLocationDisplay(shed.LocationID, parent, partition)
	shed.ParentShedName = parent
	shed.PartitionLabel = partition
	shed.Name = display
	shed.OperationalLocationDisplay = display
}

func operationalLocationDisplay(shedID, shedName, partitionLabel string) string {
	return oploc.OperationalLocation{
		ShedID:         strings.TrimSpace(shedID),
		ShedName:       strings.TrimSpace(shedName),
		PartitionLabel: strings.TrimSpace(partitionLabel),
	}.Display()
}

func matchesOperationalLocationDisplay(candidate, shedID, shedName, partitionLabel string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if strings.EqualFold(candidate, operationalLocationDisplay(shedID, shedName, partitionLabel)) {
		return true
	}
	partition := strings.TrimSpace(partitionLabel)
	if partition == "" {
		return false
	}
	return strings.EqualFold(candidate, strings.Join([]string{strings.TrimSpace(shedName), partition}, " "))
}

// applyLeadershipShedPartitionDisplay is the LeadershipShedVideos twin of
// applyShedPartitionDisplay. It exists because the OpenAPI schema marks
// operational_location_display REQUIRED on WeighingShedVideos: a required field the
// backend never emits is a contract the client cannot rely on, and that exact shape
// (schema declares it, Go never populates it) has shipped on this branch more than once.
func applyLeadershipShedPartitionDisplay(shed *domain.LeadershipShedVideos) {
	parent, parsedPartition := splitShedPartitionName(shed.ShedName)
	partition := strings.TrimSpace(shed.PartitionLabel)
	if partition == "" {
		partition = parsedPartition
	}
	parent = parentShedNameFromStoredPartitionDisplay(shed.ShedName, parent, partition)
	shed.PartitionLabel = partition
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedName:       parent,
		PartitionLabel: partition,
	}.Display()
}
