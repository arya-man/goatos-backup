export interface CommandBoardDriveOption {
  driveBatchId: string;
  driveName: string;
  label: string;
  status: string;
  plannedDate?: string | null;
  windowStart?: string | null;
  windowEnd?: string | null;
  targetCount: number;
  doseCount: number;
  shedNames: string[];
}

export interface ScheduledDriveRow {
  key: string;
  driveName: string;
  dateKeys: string[];
  targetCount: number;
  doseCount: number;
  shedNames: string[];
  batchIds: string[];
}

const MONTH_NAMES = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function dateKey(value?: string | null): string {
  const match = value?.match(/^(\d{4})-(\d{2})-(\d{2})/);
  return match ? `${match[1]}-${match[2]}-${match[3]}` : "";
}

export function formatScheduledDriveDates(keys: string[]): string {
  const parts = keys.map((key) => {
    const [year, month, day] = key.split("-").map(Number);
    return { year, month, day };
  }).filter((part) => part.year && part.month && part.day);
  if (parts.length === 0) return "—";
  if (parts.length === 2 && parts[0].year === parts[1].year && parts[0].month === parts[1].month && parts[1].day === parts[0].day + 1) {
    return `${parts[0].day}–${parts[1].day} ${MONTH_NAMES[parts[0].month - 1]} ${parts[0].year}`;
  }
  return parts.map((part) => `${part.day} ${MONTH_NAMES[part.month - 1]} ${part.year}`).join(", ");
}

export function scheduledDriveRows(options: CommandBoardDriveOption[]): ScheduledDriveRow[] {
  const rows = new Map<string, ScheduledDriveRow>();
  options.filter((option) => option.status === "planned").forEach((option) => {
    const name = option.driveName || option.label;
    const key = `${name}|${dateKey(option.windowStart)}|${dateKey(option.windowEnd)}`;
    let row = rows.get(key);
    if (!row) {
      row = { key, driveName: name, dateKeys: [], targetCount: 0, doseCount: 0, shedNames: [], batchIds: [] };
      rows.set(key, row);
    }
    const planned = dateKey(option.plannedDate) || dateKey(option.windowStart);
    if (planned && !row.dateKeys.includes(planned)) row.dateKeys.push(planned);
    row.targetCount += option.targetCount;
    row.doseCount += option.doseCount;
    row.batchIds.push(option.driveBatchId);
    option.shedNames.forEach((shed) => {
      if (!row.shedNames.includes(shed)) row.shedNames.push(shed);
    });
  });
  return Array.from(rows.values()).map((row) => ({
    ...row,
    dateKeys: row.dateKeys.sort(),
    shedNames: row.shedNames.sort(),
  })).sort((a, b) => (a.dateKeys[0] ?? "").localeCompare(b.dateKeys[0] ?? ""));
}
