// Vaccination shed/execution endpoints serve the current operational view only.
// Preserve park scoping, but never forward the URL's historical top-bar as_of:
// the backend correctly rejects historical as_of, and the UI must not turn that into an empty board.
export function vaccinationCurrentViewScope(scope: { parkId?: string }): { parkId?: string } {
  return scope.parkId ? { parkId: scope.parkId } : {};
}

// A pen board row is ONE pen: (shed_id, partition_label). The drilldown route is keyed by the
// physical shed id, so the partition must ride along or "Gandhi 2" opens the whole Gandhi building
// (Gandhi 1 + 2 + 3). The label is the stored partition_label, sent verbatim -- never a display
// string -- and an undivided pen sends none.
export function penDetailParams(
  row: { partitionLabel?: string | null },
  ret: string,
): Record<string, string | undefined> {
  const partition = row.partitionLabel?.trim();
  return { partition_label: partition || undefined, ret };
}
