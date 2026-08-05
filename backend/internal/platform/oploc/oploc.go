// Package oploc is the single source of truth for OperationalLocation:
// the ground location of an animal or a unit of work.
//
//	OperationalLocation = park + physical_shed + optional partition_label
//
// Physical sheds stay normalized: goats.shed_id always points at the parent
// physical building (Castro), and goat_shed_partitions.partition_label carries
// the actual sub-location ('1', '2', 'Part 3'). Raw partition-bearing names are
// never modelled as separate canonical buildings -- see
// docs/decisions/scale-anti-patterns.md. This package exists so that
// normalization never leaks into product surfaces: every read model, API
// response, and UI label must answer "where is this animal?" with the partition
// included when one exists.
//
// Not every shed has partitions. A non-partitioned shed renders as the bare shed
// name ("Yashoda"), never as a synthetic "Yashoda whole".
package oploc

import (
	"regexp"
	"strings"
)

// WholeSentinel is the stored marker for "this row covers the whole shed".
// It is a matching key only. It must never reach a user-facing label.
const WholeSentinel = "whole"

// partPrefix matches the "Part " prefix used by the Godel/Mandela/Sumathi label
// convention so that 'Part 3' and '3' compare equal. It mirrors the SQL
// normalizer byte for byte and must not drift from it.
var partPrefix = regexp.MustCompile(`^part[[:space:]]+`)

// alreadyWordedPartition matches a label that already reads as a partition
// phrase, so joining it to the shed name should use the dash form rather than a
// bare space. This is DISPLAY-only and is deliberately wider than partPrefix:
// it also covers the plural/range form ('Parts 1-3'), which the matching key
// leaves alone. Kept identical to the Android helper's ALREADY_WORDED_PARTITION
// (`^parts?\b`) so the same label can never render two different ways on the
// phone and on the web.
var alreadyWordedPartition = regexp.MustCompile(`(?i)^parts?\b`)

// NormalizePartition reduces a raw partition label to its comparison key.
// It mirrors, exactly, the SQL used by the operator execution reads:
//
//	regexp_replace(lower(btrim(COALESCE(partition_label, 'whole'))), '^part[[:space:]]+', '')
//
// NULL, "", and "whole" all collapse to WholeSentinel, because all three mean
// "not partitioned" somewhere in the existing data.
func NormalizePartition(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return WholeSentinel
	}
	return partPrefix.ReplaceAllString(trimmed, "")
}

// IsPartitioned reports whether a raw label denotes a real partition.
func IsPartitioned(raw string) bool {
	return NormalizePartition(raw) != WholeSentinel
}

// SamePartition reports whether two raw labels denote the same partition,
// tolerating the 'Part 3' vs '3' convention split.
func SamePartition(a, b string) bool {
	return NormalizePartition(a) == NormalizePartition(b)
}

// OperationalLocation is the shared shape every location-bearing read model and
// API response must carry. ShedID is the parent physical shed and is the only
// safe grouping key: shed *names* repeat across parks (there are two "Castro"),
// so grouping or filtering by name silently merges parks.
type OperationalLocation struct {
	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name"`
	ShedID   string `json:"shed_id"`
	ShedName string `json:"shed_name"`

	// PartitionLabel is the raw stored label ('1', 'Part 3'), or nil/empty for a
	// non-partitioned shed. Never rendered as "whole".
	PartitionLabel string `json:"partition_label,omitempty"`

	// SourceShedName is the original partition-bearing name the row was
	// normalized from ("Castro 1"), retained for traceability. Not a display
	// field.
	SourceShedName string `json:"source_shed_name,omitempty"`
}

// Display renders the user-facing operational location.
//
//	non-partitioned:      "Yashoda"
//	numeric convention:   "Castro 2", "Gandhi 3"
//	prefixed convention:  "Godel 1 - Part 3"
//
// The two conventions are preserved as stored rather than rewritten, so the
// label an operator reads on screen matches the label painted on the shed.
func (l OperationalLocation) Display() string {
	shed := strings.TrimSpace(l.ShedName)
	label := strings.TrimSpace(l.PartitionLabel)
	if shed == "" {
		return label
	}
	if !IsPartitioned(label) {
		return shed
	}
	if alreadyWordedPartition.MatchString(label) {
		return shed + " - " + label
	}
	return shed + " " + label
}

// IsPartitioned reports whether this location names a real partition.
func (l OperationalLocation) IsPartitioned() bool { return IsPartitioned(l.PartitionLabel) }

// Key returns a stable identity for grouping and de-duplication: parent shed
// uuid plus the normalized partition key. Park is implied by ShedID.
func (l OperationalLocation) Key() string {
	return l.ShedID + "#" + NormalizePartition(l.PartitionLabel)
}
