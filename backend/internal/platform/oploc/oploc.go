// Package oploc is the single source of truth for OperationalLocation:
// the ground location of an animal or a unit of work.
//
//	OperationalLocation = park + exact physical shed
//
// If a place has parts, each part is the operational shed. Legacy rows may still
// carry a group shed plus partition label during compatibility reads; this
// package exists so those reads render the same exact shed label everywhere.
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
var numericLabel = regexp.MustCompile(`^[0-9]+$`)

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
// API response must carry. ShedID is the exact operational shed whenever the
// caller has already crossed the partition-is-shed cutover. Legacy callers may
// pass a group shed plus PartitionLabel only as a compatibility bridge.
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
// After the partition-is-shed cutover, ShedName is already the exact physical
// shed name. PartitionLabel is compatibility/history metadata and must not be
// appended to live display text.
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
	return shed
}

// IsPartitioned reports whether this location names a real partition.
func (l OperationalLocation) IsPartitioned() bool { return IsPartitioned(l.PartitionLabel) }

// Key returns a stable identity for grouping and de-duplication. ShedID is the
// exact physical shed, so compatibility partition metadata is deliberately not
// part of the live identity.
func (l OperationalLocation) Key() string {
	return l.ShedID
}

// shedPartitionSuffix matches the legacy worded convention from pre-cutover partition evidence,
// and nothing else: the prefixed form ("Godel 1 - Part 3"). Active location display must treat
// that full string as the shed name; this parser exists only for compatibility imports that still
// need to interpret old group-shed plus label data.
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

// SplitShedPartitionName parses a legacy partition-bearing name into a group shed name and
// partition label, returning an empty label when the name carries no legacy partition marker.
//
// Do not call this to render or identify a live location. After the exact-shed cutover, names such
// as "Castro 2" and "Godel 1 - Part 3" are exact shed names. Live code should carry that shed id
// and name directly; this function is only for old imported strings whose group/label bridge has
// not yet been eliminated.
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
