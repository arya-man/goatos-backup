export interface CommandBoardDriveOption {
  driveBatchId: string;
  driveName: string;
  label: string;
  // Park of the drive. Optional because the command-board API does not carry it yet; while it
  // is absent the park stays out of the rendered label rather than being guessed.
  parkId?: string | null;
  parkName?: string | null;
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
  parkId: string;
  parkName: string;
  dateKeys: string[];
  targetCount: number;
  doseCount: number;
  shedNames: string[];
  batchIds: string[];
}

export interface ScheduledDriveCampaign {
  key: string;
  name: string;
  parkId: string;
  parkName: string;
  dateKeys: string[];
  targetCount: number;
  doseCount: number;
  shedNames: string[];
  batchIds: string[];
  treatments: ScheduledDriveRow[];
}

const MONTH_NAMES = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

function dateKey(value?: string | null): string {
  const match = value?.match(/^(\d{4})-(\d{2})-(\d{2})/);
  return match ? `${match[1]}-${match[2]}-${match[3]}` : "";
}

function datePart(key: string): { year: number; month: number; day: number } | null {
  const [year, month, day] = key.split("-").map(Number);
  return year && month && day ? { year, month, day } : null;
}

export function formatDateSpan(min?: string | null, max?: string | null): string {
  const firstKey = dateKey(min);
  const lastKey = dateKey(max) || firstKey;
  const first = datePart(firstKey);
  const last = datePart(lastKey);
  if (!first) return last ? `${last.day} ${MONTH_NAMES[last.month - 1]} ${last.year}` : "";
  if (!last || firstKey === lastKey) return `${first.day} ${MONTH_NAMES[first.month - 1]} ${first.year}`;
  if (first.year === last.year && first.month === last.month) {
    return `${first.day}–${last.day} ${MONTH_NAMES[first.month - 1]} ${first.year}`;
  }
  if (first.year === last.year) {
    return `${first.day} ${MONTH_NAMES[first.month - 1]}–${last.day} ${MONTH_NAMES[last.month - 1]} ${first.year}`;
  }
  return `${first.day} ${MONTH_NAMES[first.month - 1]} ${first.year}–${last.day} ${MONTH_NAMES[last.month - 1]} ${last.year}`;
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
    // Park belongs in the key: two parks routinely run the same vaccine over the same window,
    // and a park-less key summed CBE's and CPT's animal counts into a single row that was then
    // shown under whichever park's name the label happened to hardcode.
    const parkId = option.parkId ?? "";
    const parkName = option.parkName ?? "";
    const key = `${parkId}|${name}|${dateKey(option.windowStart)}|${dateKey(option.windowEnd)}`;
    let row = rows.get(key);
    if (!row) {
      row = { key, driveName: name, parkId, parkName, dateKeys: [], targetCount: 0, doseCount: 0, shedNames: [], batchIds: [] };
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

// The park prefix used to be the literal "CPT", which mislabelled every other park's drive.
// It now comes from the drive row, and is simply omitted while the API does not supply it --
// no park name is better than the wrong one.
export function commonDriveName(driveName: string, parkName?: string | null): string {
  const isAnnualPoxTreatment = driveName === "Blue Tongue + Sheep Pox" || driveName === "Goat Pox";
  const vaccineLabel = isAnnualPoxTreatment ? "Annual Pox + Blue Tongue" : driveName;
  const prefix = parkName?.trim() ? `${parkName.trim()} ` : "";
  return `${prefix}Adult ${vaccineLabel}`;
}

function campaignIdentity(row: ScheduledDriveRow): { key: string; name: string } {
  const isAnnualPoxTreatment = row.driveName === "Blue Tongue + Sheep Pox" || row.driveName === "Goat Pox";
  if (isAnnualPoxTreatment) {
    const month = (row.dateKeys[0] ?? "").slice(0, 7);
    return {
      key: `${row.parkId}|adult-annual-pox-blue-tongue|${month}`,
      name: commonDriveName(row.driveName, row.parkName),
    };
  }
  return { key: row.key, name: commonDriveName(row.driveName, row.parkName) };
}

export function scheduledDriveCampaigns(rows: ScheduledDriveRow[]): ScheduledDriveCampaign[] {
  const campaigns = new Map<string, ScheduledDriveCampaign>();
  rows.forEach((row) => {
    const identity = campaignIdentity(row);
    let campaign = campaigns.get(identity.key);
    if (!campaign) {
      campaign = {
        key: identity.key,
        name: identity.name,
        parkId: row.parkId,
        parkName: row.parkName,
        dateKeys: [],
        targetCount: 0,
        doseCount: 0,
        shedNames: [],
        batchIds: [],
        treatments: [],
      };
      campaigns.set(identity.key, campaign);
    }
    campaign.treatments.push(row);
    campaign.targetCount += row.targetCount;
    campaign.doseCount += row.doseCount;
    row.dateKeys.forEach((date) => {
      if (!campaign!.dateKeys.includes(date)) campaign!.dateKeys.push(date);
    });
    row.shedNames.forEach((shed) => {
      if (!campaign!.shedNames.includes(shed)) campaign!.shedNames.push(shed);
    });
    row.batchIds.forEach((batchID) => {
      if (!campaign!.batchIds.includes(batchID)) campaign!.batchIds.push(batchID);
    });
  });
  return Array.from(campaigns.values()).map((campaign) => ({
    ...campaign,
    dateKeys: campaign.dateKeys.sort(),
    shedNames: campaign.shedNames.sort(),
    treatments: campaign.treatments.sort((a, b) => (a.dateKeys[0] ?? "").localeCompare(b.dateKeys[0] ?? "")),
  })).sort((a, b) => (a.dateKeys[0] ?? "").localeCompare(b.dateKeys[0] ?? ""));
}

// Selector identity for one drive row.
//
// The command-board API returns drive options at (batch, park) grain: a batch whose obligations
// span two parks is genuinely two operator days in two places and arrives as two rows sharing one
// driveBatchId. The selector used to carry the batch id alone, so those two rows collided -- they
// rendered with the same React key, both matched the selected `value` (so both showed as selected),
// and choosing either one narrowed the board by batch only, folding the other park's animals into
// the answer. The park therefore belongs in the identity, not just in the label.
export function driveSelectionValue(driveBatchId: string, parkId?: string | null): string {
  return `${driveBatchId}~${parkId ?? ""}`;
}

export function parseDriveSelectionValue(value: string): { driveBatchId: string; parkId: string } {
  const separator = value.indexOf("~");
  if (separator < 0) return { driveBatchId: value, parkId: "" };
  return { driveBatchId: value.slice(0, separator), parkId: value.slice(separator + 1) };
}

// Resolves a URL selection to exactly one drive row. A selection that matches no row, or that is
// still park-blind (an older link carrying only the batch) while the batch exists in more than one
// park, is deliberately UNRESOLVED: the board then stays on the honest all-drives answer instead of
// silently picking one of the two parks.
export function resolveSelectedDrive(
  options: CommandBoardDriveOption[],
  driveBatchId?: string,
  parkId?: string,
): CommandBoardDriveOption | undefined {
  if (!driveBatchId) return undefined;
  const matches = options.filter(
    (option) =>
      option.driveBatchId === driveBatchId && (!parkId || (option.parkId ?? "") === parkId),
  );
  return matches.length === 1 ? matches[0] : undefined;
}
