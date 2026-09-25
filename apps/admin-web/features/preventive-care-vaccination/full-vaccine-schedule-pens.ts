import { hasOperationalPartition, operationalLocationLabel } from "../../lib/operational-location.ts";

// One pen inside an operator-day of the Full Schedule. A pen IS (park, shed_id, partition): the
// schedule used to fold every assignment row into its BUILDING by name, so the chip read "Gandhi 84"
// (Gandhi 1 + Gandhi 3), "Godel 1 120", and "Yashoda 2" for two animals that live in Yashoda 3 (the
// trailing "2" was the animal count beside a bare building name). Each pen now keeps its own row,
// keyed by stable identity and named by the backend-composed operational_location_display.
export type SchedulePen = {
  key: string;
  shedId?: string;
  partitionLabel?: string;
  display: string;
  animals: number;
};

export type SchedulePenSource = {
  parkId: string;
  shedId?: string | null;
  physicalShed: string;
  partitionLabel?: string | null;
  partition_label?: string | null;
  operational_location_display?: string | null;
};

function rawPartition(row: SchedulePenSource): string | undefined {
  const label = (row.partition_label ?? row.partitionLabel ?? "").trim();
  return hasOperationalPartition(label) ? label : undefined;
}

// Stable identity: park + shed id + normalized partition. The shed NAME is used only when an old row
// carries no shed id, and even then it is scoped to its park (names repeat across parks).
export function schedulePenKey(row: SchedulePenSource): string {
  const partition = (rawPartition(row) ?? "").toLowerCase().replace(/^part\s+/, "");
  const shed = row.shedId ? `id:${row.shedId}` : `name:${row.physicalShed.trim()}`;
  return `${row.parkId}|${shed}|${partition}`;
}

export function schedulePenDisplay(row: SchedulePenSource): string {
  const composed = row.operational_location_display?.trim();
  if (composed) return composed;
  return operationalLocationLabel({ shedName: row.physicalShed, partitionLabel: rawPartition(row) });
}

export function addSchedulePen(pens: SchedulePen[], row: SchedulePenSource, animals: number): void {
  const key = schedulePenKey(row);
  let pen = pens.find((item) => item.key === key);
  if (!pen) {
    pen = {
      key,
      shedId: row.shedId ?? undefined,
      partitionLabel: rawPartition(row),
      display: schedulePenDisplay(row),
      animals: 0,
    };
    pens.push(pen);
  }
  pen.animals += animals;
}
