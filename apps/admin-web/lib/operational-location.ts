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
  const shed = (shedName ?? "").trim() || (sourceShedName ?? "").trim();
  const rawPartition = (partitionLabel ?? "").trim();
  if (!isPartitioned(rawPartition)) return shed;
  return /^part\b/i.test(rawPartition) ? `${shed} - ${rawPartition}` : `${shed} ${rawPartition}`;
}

/** True when the given partition label represents a real (non-whole) partition. */
export function hasOperationalPartition(partitionLabel: string | null | undefined): boolean {
  return isPartitioned(partitionLabel);
}
