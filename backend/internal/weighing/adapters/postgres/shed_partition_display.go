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
// A partition-bearing shed's catalog name is the operational shed name itself: "Castro 2",
// "Godel 1 - Part 3", "Mandela 2 Part 1". Weighing keeps that exact name as the operator
// identity; it only tolerates a stored legacy partition_label when reading rows written before the
// exact-shed cutover.
//
// Weighing's isolation is untouched: oploc is a platform primitive over names, and this still
// reads only the already-flattened locations catalog name -- no goats, no goat_shed_partitions.
func splitShedPartitionName(name string) (parentShedName, partitionLabel string) {
	return oploc.SplitShedPartitionName(name)
}

// applyShedPartitionDisplay stamps ParentShedName/PartitionLabel/OperationalLocationDisplay on a
// CampaignShed from its already-loaded DisplayName. DisplayName is already the exact shed name:
// "Castro 2" and "Godel 1 - Part 3" are not rebuilt from a parent shed plus partition label.
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
	// Stored partition_label is compatibility/history metadata. Keeping it blank in the read model
	// prevents callers from rendering "Castro 2 2" or stripping "Godel 1 - Part 3" back to "Godel 1".
	_ = storedPartitionLabel
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
	display := operationalLocationDisplay(shed.LocationID, parent, "")
	shed.ParentShedName = parent
	shed.PartitionLabel = ""
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
	_ = partitionLabel
	return false
}

// applyLeadershipShedPartitionDisplay is the LeadershipShedVideos twin of
// applyShedPartitionDisplay. It exists because the OpenAPI schema marks
// operational_location_display REQUIRED on WeighingShedVideos: a required field the
// backend never emits is a contract the client cannot rely on, and that exact shape
// (schema declares it, Go never populates it) has shipped on this branch more than once.
func applyLeadershipShedPartitionDisplay(shed *domain.LeadershipShedVideos) {
	exact := strings.TrimSpace(shed.ShedName)
	shed.PartitionLabel = ""
	shed.OperationalLocationDisplay = oploc.OperationalLocation{
		ShedName: exact,
	}.Display()
}
