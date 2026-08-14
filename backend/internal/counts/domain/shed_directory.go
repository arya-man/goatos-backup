package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

var partitionDigits = regexp.MustCompile(`\d+`)

// partitionSortKey orders pen labels NUMERICALLY where they carry a number.
//
// "Part 2" before "Part 10" is not cosmetic: a reader scanning a ten-pen shed for a gap reads the
// order as the farm's own numbering, and string order ("Part 1", "Part 10", "Part 2") looks like
// missing pens. Labels with no number keep their text order, after the numbered ones.
func partitionSortKey(label string) string {
	digits := partitionDigits.FindString(label)
	if digits == "" {
		return "9:" + label
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return "9:" + label
	}
	return fmt.Sprintf("0:%09d:%s", n, label)
}

// The Sheds directory: the farm's physical sheds, side by side across parks.
//
// It is a CONFIGURATION read, not a census one. Counts Breakdown answers "how many animals are
// standing where"; this answers "what did we build, what cohort is it configured for, and how many
// head is it meant to hold". Those are different questions with different sources -- the census
// reads `goats`, this reads `locations` + `shed_profiles` -- and a shed with zero animals still
// belongs here.
//
// GRAIN is the OPERATIONAL LOCATION -- a pen where the shed has pens ("Godel 1 - Part 3"), the
// bare shed where it has none ("Q1") -- which is the grain the farm's own Sheds DB sheet records
// and the grain a capacity is configured at (shed_partitions.capacity, or shed_profiles.capacity
// for a shed with no pens).
//
// Rows are PAIRED ACROSS PARKS: one row per operational-location LABEL with one cell per park,
// because the farm runs the same shed names in both parks (Castro, Gandhi, Godel 1, Mandela 1,
// Yashoda) and reads them side by side. That pairing is the ONLY place a name is used as a key,
// and it happens HERE, in Go, over rows the query already keyed by (park_id, shed_id, pen) -- see
// PivotShedDirectory. The SQL never groups by name, which is what keeps the operational-location
// rule's park-merge defect (two different sheds collapsing into one row) impossible rather than
// merely avoided.

// ShedDirectoryPark is one park column-pair in the directory.
//
// It carries no column HEADERS: those are table copy and belong to the admin-web page contract,
// which compiles them from the same live park rows (adminui compileShedDirectoryColumns). Emitting
// them here too would put one visible string behind two sources that could drift apart.
type ShedDirectoryPark struct {
	ParkID    string `json:"park_id"`
	ParkCode  string `json:"park_code"`
	ParkLabel string `json:"park_label"`
}

// ShedDirectoryCell is one park's version of a shed. A park that has no shed of this name has no
// cell at all, which is how the renderer tells "this park does not have this shed" apart from
// "this park has it but has not configured it yet".
type ShedDirectoryCell struct {
	ShedID string `json:"shed_id"`
	// Tag is the configured cohort (`animal_stage_lookup.stage_code`: "Non-Pregnant", "F2-Male",
	// "Buck", "Quarantine"), or "" when the shed has no profile yet.
	Tag string `json:"tag"`
	// Capacity is the head count the shed is configured to hold, or nil when it has never been
	// configured. Nil and 0 are different facts and must not be collapsed: 0 means someone
	// recorded that the shed holds nothing, nil means nobody has recorded anything.
	Capacity *int32 `json:"capacity"`
}

// ShedDirectoryRow is one operational location across every park that has one by that label.
type ShedDirectoryRow struct {
	ShedName string `json:"shed_name"`
	// PartitionLabel is the pen's HUMAN label ("Part 3", "2"), or "" for a shed with no pens.
	// Never normalized_label, which is a matching key and must not reach a screen.
	PartitionLabel string `json:"partition_label"`
	// OperationalLocationDisplay is oploc.OperationalLocation{...}.Display(): "Godel 1 - Part 3"
	// for a pen, bare "Q1" for a shed with none, never a synthetic "Q1 whole". This is what the
	// screen renders, and it is composed backend-side so no client re-derives it.
	OperationalLocationDisplay string `json:"operational_location_display"`
	// Cells is keyed by park_id -- never by park code or park name, so a renamed or re-coded park
	// cannot silently re-associate a shed with the wrong column.
	Cells map[string]ShedDirectoryCell `json:"cells"`
}

// ShedDirectory is the whole bounded catalog. There is no paging: the tenant has ~120 operational
// locations, and that number is governed by how many pens the business builds, not by herd size or
// event volume.
type ShedDirectory struct {
	Parks     []ShedDirectoryPark `json:"parks"`
	Items     []ShedDirectoryRow  `json:"items"`
	TotalRows int64               `json:"total_rows"`
}

// ShedDirectoryEntry is one flat (park, shed, pen) row as the repository reads it, before the
// pivot.
type ShedDirectoryEntry struct {
	ParkID         string
	ParkCode       string
	ParkLabel      string
	ShedID         string
	ShedName       string
	PartitionLabel string
	Tag            string
	Capacity       *int32
}

// PivotShedDirectory turns the flat (park, shed, pen) rows into one row per operational location.
//
// Two rules it exists to keep:
//
//  1. A park column appears only if the query returned a shed for it, and the columns are ordered
//     by park label so the pairs are stable across calls.
//  2. Two sheds with the SAME name in the SAME park cannot both occupy one cell. That should be
//     impossible (the alias exclusion removes the legacy duplicates), so rather than silently
//     keeping the last one, the extra shed gets its own row keyed by name -- visible, not merged.
//     Merging is the exact defect the operational-location rule bans; dropping it would hide a
//     real data problem.
func PivotShedDirectory(entries []ShedDirectoryEntry) ShedDirectory {
	out := ShedDirectory{Parks: []ShedDirectoryPark{}, Items: []ShedDirectoryRow{}}

	parkIndex := map[string]int{}
	for _, entry := range entries {
		if _, seen := parkIndex[entry.ParkID]; seen {
			continue
		}
		parkIndex[entry.ParkID] = len(out.Parks)
		out.Parks = append(out.Parks, ShedDirectoryPark{
			ParkID:    entry.ParkID,
			ParkCode:  entry.ParkCode,
			ParkLabel: entry.ParkLabel,
		})
	}
	sort.SliceStable(out.Parks, func(i, j int) bool { return out.Parks[i].ParkLabel < out.Parks[j].ParkLabel })

	rowIndex := map[string]int{}
	for _, entry := range entries {
		display := oploc.OperationalLocation{ShedName: entry.ShedName, PartitionLabel: entry.PartitionLabel}.Display()
		idx, seen := rowIndex[display]
		if seen {
			if _, taken := out.Items[idx].Cells[entry.ParkID]; taken {
				// Same label, same park, two locations. Give the second one its own row instead of
				// overwriting the first: a duplicate is a data problem to look at, not to hide.
				seen = false
			}
		}
		if !seen {
			idx = len(out.Items)
			rowIndex[display] = idx
			out.Items = append(out.Items, ShedDirectoryRow{
				ShedName:                   entry.ShedName,
				PartitionLabel:             entry.PartitionLabel,
				OperationalLocationDisplay: display,
				Cells:                      map[string]ShedDirectoryCell{},
			})
		}
		out.Items[idx].Cells[entry.ParkID] = ShedDirectoryCell{
			ShedID:   entry.ShedID,
			Tag:      entry.Tag,
			Capacity: entry.Capacity,
		}
	}
	// Sorted by shed then PEN NUMBER, so "Part 2" precedes "Part 10" -- plain string order puts
	// Part 10 second and makes a ten-pen shed read as though pens were missing.
	sort.SliceStable(out.Items, func(i, j int) bool {
		if out.Items[i].ShedName != out.Items[j].ShedName {
			return out.Items[i].ShedName < out.Items[j].ShedName
		}
		return partitionSortKey(out.Items[i].PartitionLabel) < partitionSortKey(out.Items[j].PartitionLabel)
	})

	out.TotalRows = int64(len(out.Items))
	return out
}
