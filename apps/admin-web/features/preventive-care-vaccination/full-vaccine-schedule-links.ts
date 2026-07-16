type ScheduleDateSource = {
  lastDose?: string;
  nextDue?: string;
};

type ScheduleCohortDateSource = ScheduleDateSource & {
  cells?: ScheduleDateSource[];
};

function isInYear(iso: string | undefined, year: number): boolean {
  if (!iso) return false;
  const date = new Date(iso);
  return !Number.isNaN(date.getTime()) && date.getFullYear() === year;
}

function dateOnly(iso: string | undefined): string | undefined {
  if (!iso) return undefined;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return undefined;
  return iso.slice(0, 10);
}

export function vaccinationScheduleCellAsOf(cell: ScheduleDateSource | undefined, year: number): string | undefined {
  if (!cell) return undefined;
  if (isInYear(cell.nextDue, year)) return dateOnly(cell.nextDue);
  if (isInYear(cell.lastDose, year)) return dateOnly(cell.lastDose);
  return undefined;
}

export function vaccinationScheduleCohortAsOf(row: ScheduleCohortDateSource, year: number): string | undefined {
  if (isInYear(row.nextDue, year)) return dateOnly(row.nextDue);
  if (isInYear(row.lastDose, year)) return dateOnly(row.lastDose);
  for (const cell of row.cells ?? []) {
    const asOf = vaccinationScheduleCellAsOf(cell, year);
    if (asOf) return asOf;
  }
  return undefined;
}
