// Shared operational-location display helper. Every screen that shows a shed/partition
// location must render through this — never re-derive the "is this partitioned" or
// "which convention" logic locally. See contracts/openapi/{app,admin}-api.yaml for the
// canonical field names this consumes: partition_label, source_shed_name,
// operational_location_display.
//
// Display rule (maintainer contract):
//   no partition               -> bare shed name, e.g. "Yashoda"
//   numeric convention  ('2')  -> "<shed> <label>", e.g. "Castro 2"
//   prefixed convention ('Part 3') -> "<shed> - <label>", e.g. "Godel 1 - Part 3"
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
 * the raw shed_name/partition_label pair (e.g. a locally composed row).
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
  // One separator for every partition, worded or bare (maintainer decision, 2026-08-06). The old
  // space form was unreadable wherever the shed NAME ends in a digit: "Godel 1" + "1" read
  // "Godel 1 1" and "Godel 1" + "10" read "Godel 1 10". That was 98 of 130 real STG destination
  // options (75%), not an edge case. Worded labels already used the dash, so two formats became
  // one. Keep identical to oploc.Display() (Go) and PartitionLabel.kt (Android).
  return `${shed} - ${rawPartition}`;
}

/** True when the given partition label represents a real (non-whole) partition. */
export function hasOperationalPartition(partitionLabel: string | null | undefined): boolean {
  return isPartitioned(partitionLabel);
}
