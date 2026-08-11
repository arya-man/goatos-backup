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

// NOTE (2026-08-06): the display join no longer branches on whether a label is
// already worded ('Part 3') or bare ('1') -- BOTH now join with " - ", so the
// regex that used to select between a space form and a dash form is gone. The
// matching key still normalizes 'Part 3' and '3' to the same value; that is
// partPrefix above and is unaffected. Android keeps its own
// ALREADY_WORDED_PARTITION because partitionDisplayLabel (the standalone chip)
// still needs it; only the JOIN collapsed.

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
//	numeric convention:   "Castro - 2", "Gandhi - 3"
//	prefixed convention:  "Godel 1 - Part 3"
//
// The stored label is preserved verbatim rather than rewritten, so the text an
// operator reads on screen matches the text painted on the shed; only the
// SEPARATOR is ours.
//
// Why a dash and not a space (maintainer decision, 2026-08-06). The space form
// was unreadable for the majority of real sheds, because shed NAMES themselves
// end in a digit: "Godel 1" + partition "1" rendered "Godel 1 1", and
// "Godel 1" + "10" rendered "Godel 1 10" -- which a human cannot parse as
// shed "Godel 1" partition 10 rather than shed "Godel 1 1" partition 0, or
// "Godel 1 10" as a name in its own right. On live STG data this was not an
// edge case: 98 of 130 destination options (75%) had a digit-terminated shed
// name with a numeric partition. Joining with " - " makes the boundary explicit
// and, because worded labels already used the dash, collapses two formats into
// one.
//
// Keep this identical to PartitionLabel.kt (Android) and
// lib/operational-location.ts (admin-web); the same animal must never read two
// different ways across surfaces.
func (l OperationalLocation) Display() string {
	shed := strings.TrimSpace(l.ShedName)
	label := strings.TrimSpace(l.PartitionLabel)
	if shed == "" {
		// A missing shed name must still never surface the matching sentinel. Falling through with
		// the raw label here would print "whole" to a user -- the one string this type exists to
		// keep off screen. Matches the admin-web and Android helpers.
		if !IsPartitioned(label) {
			return ""
		}
		return label
	}
	if !IsPartitioned(label) {
		return shed
	}
	if strings.EqualFold(shed, label) || strings.HasSuffix(strings.ToLower(shed), " - "+strings.ToLower(label)) {
		return shed
	}
	return shed + " - " + label
}

// IsPartitioned reports whether this location names a real partition.
func (l OperationalLocation) IsPartitioned() bool { return IsPartitioned(l.PartitionLabel) }

// Key returns a stable identity for grouping and de-duplication: parent shed
// uuid plus the normalized partition key. Park is implied by ShedID.
func (l OperationalLocation) Key() string {
	return l.ShedID + "#" + NormalizePartition(l.PartitionLabel)
}

// shedPartitionSuffix matches the EXACTLY TWO partition-naming conventions the locations catalog
// uses, and nothing else: the prefixed form ("Godel 1 - Part 3") and the bare numeric form
// ("Castro 2"). An ordinary non-partitioned name ("Yashoda", "Ho Chi Minh") matches neither and
// passes through untouched.
//
// Order matters. The prefixed alternative is tried FIRST so "Godel 1 - Part 3" resolves to shed
// "Godel 1" + partition "Part 3", not shed "Godel" + partition "1" -- a shed name may itself end in
// a number, and a name carries at most one partition.
// ONLY the explicit worded convention is parseable from a NAME. The numeric convention is NOT.
//
// "Castro 2" (partition 2 of Castro) and "Mandela 1" (an ordinary shed whose NAME ends in 1) are
// indistinguishable as strings -- nothing in the text says which is which. The old pattern had a
// second alternative, `^(.+?)\s+(\d+)$`, that treated ANY trailing number as a partition, so it
// renamed every numbered shed on the farm:
//
//	"Yashoda 2"     -> "Yashoda - 2"
//	"Mandela 1"     -> "Mandela - 1"
//	"Godel 1"       -> "Godel - 1"
//	"Ho Chi Minh 1" -> "Ho Chi Minh - 1"
//
// Mandela 1 and Godel 1 are real, undivided sheds. A name-based guess cannot be made safe here;
// only the shed_partitions CATALOG knows which partitions exist, which is why callers that need
// the numeric convention resolve it from the catalog (see resolve.go / ShedScopedLocationSQL)
// rather than from the name.
//
// Pinned by numeric_name_check_test.go. Do not add the numeric alternative back.
var shedPartitionSuffix = regexp.MustCompile(`(?i)^(.+?)\s*-\s*part\s+(\S+)$`)

// SplitShedPartitionName parses a shed catalog display name into its physical shed name and
// partition label, returning an empty label when the name carries no partition.
//
// This is the single Go home of the naming rule AGENTS.md states as a STORAGE rule ("Gandhi 1,
// Gandhi 2, Gandhi 3 are one physical shed Gandhi with partitions 1, 2, 3"; "Godel 1 - Part 3 is
// physical shed Godel 1 with partition Part 3"). It lives here, next to NormalizePartition and
// Display, so a caller that needs to go from a catalog name to an operational location never
// re-derives the convention -- re-deriving it is how two callers end up disagreeing about whether
// "Godel 1 - Part 3" is one shed or two.
func SplitShedPartitionName(name string) (shedName, partitionLabel string) {
	trimmed := strings.TrimSpace(name)
	m := shedPartitionSuffix.FindStringSubmatch(trimmed)
	if m == nil {
		return trimmed, ""
	}
	if m[1] != "" {
		return strings.TrimSpace(m[1]), "Part " + m[2]
	}
	return strings.TrimSpace(m[3]), m[4]
}
