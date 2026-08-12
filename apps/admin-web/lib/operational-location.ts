// Shared operational-location display helper. Every screen that shows a shed/partition
// location must render through this — never re-derive the "is this partitioned" or
// "which convention" logic locally. See contracts/openapi/{app,admin}-api.yaml for the
// canonical field names this consumes: partition_label, source_shed_name,
// operational_location_display.
//
// Display rule (maintainer contract):
//   no partition               -> bare shed name, e.g. "Yashoda"
//   shed name present -> exact shed name as-is. `partition_label` is compatibility metadata and
//                        must not be appended. The backend must send "Castro 2" or
//                        "Mandela 2 Part 1" as the shed/display string when that is the real shed.
//
// null / "" / the literal string "whole" (case-insensitive) are all treated as
// "non-partitioned" and must NEVER themselves appear in the rendered label — a
// non-partitioned shed named "Yashoda" must render as "Yashoda", never "Yashoda whole".

export interface OperationalLocationInput {
  shedName: string | null | undefined;
  partitionLabel?: string | null;
  sourceShedName?: string | null;
}

const NON_PARTITION_SENTINELS = new Set(["", "whole"]);

function isPartitioned(rawPartitionLabel: string | null | undefined): rawPartitionLabel is string {
  const trimmed = (rawPartitionLabel ?? "").trim();
  return !NON_PARTITION_SENTINELS.has(trimmed.toLowerCase());
}

/**
 * Renders the user-facing operational location label for a shed (optionally partitioned).
 * If the backend already provides `operational_location_display`, prefer that field
 * directly instead of calling this helper — it exists for surfaces that only receive
 * the raw shed_name/partition_label pair. The partition field is compatibility metadata and must
 * not alter a present shed name.
 */
export function operationalLocationLabel({ shedName, partitionLabel, sourceShedName }: OperationalLocationInput): string {
  const shed = (shedName ?? "").trim();
  const rawPartition = (partitionLabel ?? "").trim();
  // sourceShedName is the RAW partition-bearing name a row was normalized FROM ("Castro 1"), so it
  // ALREADY contains the partition. Using it as the shed and then appending partitionLabel produced
  // "Castro 1 1" -- the same defect class as the "Godel 1 1" bug this convention exists to prevent.
  // It is a display value in its own right, never a prefix. Go's oploc.Display() deliberately never
  // reads this field and Kotlin's helper has no such parameter; this keeps the three in agreement.
  if (!shed) {
    const raw = (sourceShedName ?? "").trim();
    if (raw) return raw;
  }
  if (!isPartitioned(rawPartition)) return shed;
  // With no shed name there is nothing to prefix, so return the partition alone rather than
  // concatenating onto an empty string -- that produced a leading space (" 2") or a leading dash
  // (" - Part 3") and made admin-web render this edge case differently from Android for the same
  // animal. Matches PartitionLabel.kt and oploc.Display().
  if (!shed) return rawPartition;
  return shed;
}

/** True when the given partition label represents a real (non-whole) partition. */
export function hasOperationalPartition(partitionLabel: string | null | undefined): boolean {
  return isPartitioned(partitionLabel);
}
