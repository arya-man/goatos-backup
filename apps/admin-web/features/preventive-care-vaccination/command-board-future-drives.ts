import type { AppApiComponents } from "@goatos/api-client";

type ApiCommandBoardDriveOption = AppApiComponents["schemas"]["VaccinationCommandBoardDriveOption"];
type CommandBoardShedLocation = {
  shedId: string;
  shedName: string;
  partition_label?: string | null;
  operational_location_display?: string | null;
};
type CommandBoardOperatorDay = {
  date: string;
  targetCount: number;
  doseCount: number;
};

export type CommandBoardDriveOption = ApiCommandBoardDriveOption & {
  driveName?: string;
  parkId?: string | null;
  parkName?: string | null;
  plannedDate?: string | null;
  targetCount?: number;
  doseCount?: number;
  shedNames?: string[];
  shedIds?: string[];
  shedLocations?: CommandBoardShedLocation[];
  operatorDays?: CommandBoardOperatorDay[];
  // Set when the option's counts were reconstructed from the projection matrices instead of being
  // carried by the API. Such an option describes the WHOLE campaign, not one operator day, so the
  // fold below must not add it to its siblings.
  derivedFromMatrix?: boolean;
};

export interface ScheduledDriveRow {
  key: string;
  driveName: string;
  parkId: string;
  parkName: string;
  dateKeys: string[];
  targetCount: number;
  doseCount: number;
  // Operational shed display names.
  shedNames: string[];
  // Stable shed ids for the same sheds, in the same scope. Dedup keys on these, never on
  // shedNames: names repeat across parks (two Castro, two Gandhi), so a name-keyed dedup
  // silently merges two parks' sheds into one row.
  shedIds: string[];
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
  const sameMonthRun = parts.length > 1
    && parts.every((part) => part.year === parts[0].year && part.month === parts[0].month)
    && parts.every((part, index) => index === 0 || part.day === parts[index - 1].day + 1);
  if (sameMonthRun) {
    return `${parts[0].day}–${parts[parts.length - 1].day} ${MONTH_NAMES[parts[0].month - 1]} ${parts[0].year}`;
  }
  return parts.map((part) => `${part.day} ${MONTH_NAMES[part.month - 1]} ${part.year}`).join(", ");
}

export function scheduledDriveRows(options: CommandBoardDriveOption[]): ScheduledDriveRow[] {
  const rows = new Map<string, ScheduledDriveRow>();
  options.filter((option) => option.status === "planned").forEach((option) => {
    const name = option.driveName || option.label;
    // Park belongs in the key: two parks routinely run the same vaccine over the same window, and a
    // park-less key summed both parks' animal counts into a single row that was then shown under
    // whichever park's name the label happened to carry.
    const parkId = option.parkId ?? "";
    const parkName = option.parkName ?? "";
    const key = `${parkId}|${name}|${dateKey(option.windowStart)}|${dateKey(option.windowEnd)}`;
    let row: ScheduledDriveRow | undefined = rows.get(key);
    if (!row) {
      // Row is keyed by (parkId, drive name, window dates). ShedIds (from the API) are used for deduplication.
      // This prevents name collisions across parks where identical shed names can exist.
      row = { key, driveName: name, parkId, parkName, dateKeys: [], targetCount: 0, doseCount: 0, shedIds: [], shedNames: [], batchIds: [] };
      rows.set(key, row);
    }
    // rows.set above guarantees this; the local alias keeps TS from widening it back to undefined.
    const driveRow: ScheduledDriveRow = row;
    const planned = dateKey(option.plannedDate) || dateKey(option.windowStart);
    if (planned && !driveRow.dateKeys.includes(planned)) driveRow.dateKeys.push(planned);
    // Genuine API rows are one executable operator day each, so they sum. Matrix-derived options all
    // reconstruct the same campaign-wide total, so summing them would multiply it by the number of
    // days; the campaign total is the max, not the sum.
    if (option.derivedFromMatrix) {
      driveRow.targetCount = Math.max(driveRow.targetCount, option.targetCount ?? 0);
      driveRow.doseCount = Math.max(driveRow.doseCount, option.doseCount ?? 0);
    } else {
      driveRow.targetCount += option.targetCount ?? 0;
      driveRow.doseCount += option.doseCount ?? 0;
    }
    driveRow.batchIds.push(option.driveBatchId);
    // Collect shed IDs and names within this row's scope (park + drive + window). Row is scoped to
    // (parkId, driveName, windowStart, windowEnd). Use shedIds for deduplication to prevent
    // name collisions; keep shedNames for display. Sort both for stable output.
    const shedIdSet = new Set(driveRow.shedIds);
    const shedNameSet = new Set(driveRow.shedNames);
    if (option.shedLocations?.length) {
      option.shedLocations.forEach((location) => {
        shedIdSet.add(`${location.shedId}|${location.partition_label ?? ""}`);
        shedNameSet.add(location.operational_location_display || location.shedName);
      });
    } else {
      (option.shedIds ?? []).forEach((id: string) => {
        shedIdSet.add(id);
      });
      (option.shedNames ?? []).forEach((name: string) => {
        shedNameSet.add(name);
      });
    }
    driveRow.shedIds = Array.from(shedIdSet);
    driveRow.shedNames = Array.from(shedNameSet);
  });
  return Array.from(rows.values()).map((row) => ({
    ...row,
    dateKeys: row.dateKeys.sort(),
    shedIds: row.shedIds.sort(),
    shedNames: row.shedNames.sort(),
  })).sort((a, b) => (a.dateKeys[0] ?? "").localeCompare(b.dateKeys[0] ?? ""));
}

// The park prefix used to be the literal "CPT", which mislabelled every other park's drive. It now
// comes from the drive row, and is simply omitted while the API does not supply it -- no park name
// is better than the wrong one.
export function commonDriveName(driveName: string, parkName?: string | null): string {
  const isAnnualPoxTreatment = driveName === "Blue Tongue + Sheep Pox" || driveName === "Goat Pox";
  const prefix = parkName?.trim() ? `${parkName.trim()} ` : "";
  return isAnnualPoxTreatment ? `${prefix}Adult Annual Pox + Blue Tongue` : `${prefix}Adult ${driveName}`;
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

export function executedDriveCampaigns(options: CommandBoardDriveOption[]): ScheduledDriveCampaign[] {
  return options
    .filter((option) => option.status !== "planned")
    .map((option) => {
      const name = option.driveName || option.label;
      const days = (option.operatorDays ?? [])
        .filter((day) => dateKey(day.date))
        .map((day) => ({ ...day, date: dateKey(day.date) }))
        .sort((a, b) => a.date.localeCompare(b.date));
      const dateKeys = days.length > 0
        ? days.map((day) => day.date)
        : [dateKey(option.plannedDate) || dateKey(option.windowStart)].filter(Boolean);
      const treatments: ScheduledDriveRow[] = (days.length > 0 ? days : dateKeys.map((date) => ({
        date,
        targetCount: option.targetCount,
        doseCount: option.doseCount,
      }))).map((day) => ({
        key: `${option.driveBatchId}|${option.parkId ?? ""}|${day.date}`,
        driveName: name,
        parkId: option.parkId ?? "",
        parkName: option.parkName ?? "",
        dateKeys: [day.date],
        targetCount: day.targetCount ?? 0,
        doseCount: day.doseCount ?? 0,
        shedNames: [...(option.shedNames ?? [])].sort(),
        shedIds: [...(option.shedLocations?.map((location) => `${location.shedId}|${location.partition_label ?? ""}`) ?? option.shedIds ?? [])].sort(),
        batchIds: [option.driveBatchId],
      }));
      const shedNames = option.shedLocations?.length
        ? option.shedLocations.map((location) => location.operational_location_display || location.shedName)
        : option.shedNames ?? [];
      return {
        key: `${option.driveBatchId}|${option.parkId ?? ""}`,
        name: commonDriveName(name, option.parkName),
        parkId: option.parkId ?? "",
        parkName: option.parkName ?? "",
        dateKeys,
        targetCount: option.targetCount ?? 0,
        doseCount: option.doseCount ?? 0,
        shedNames: [...shedNames].sort(),
        batchIds: [option.driveBatchId],
        treatments,
      };
    })
    .sort((a, b) => (b.dateKeys[0] ?? "").localeCompare(a.dateKeys[0] ?? ""));
}

function campaignSortDate(campaign: ScheduledDriveCampaign): string {
  return campaign.dateKeys[0] ?? "";
}

export function sortDriveCampaignsChronological(campaigns: ScheduledDriveCampaign[]): ScheduledDriveCampaign[] {
  return [...campaigns].sort((a, b) => {
    const aDate = campaignSortDate(a);
    const bDate = campaignSortDate(b);
    const dateOrder = aDate.localeCompare(bDate);
    if (dateOrder !== 0) return dateOrder;
    return a.name.localeCompare(b.name);
  });
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
  const exactMatches = options.filter(
    (option) =>
      option.driveBatchId === driveBatchId && (!parkId || (option.parkId ?? "") === parkId),
  );
  if (exactMatches.length === 1) return exactMatches[0];

  // Older/skinny API catalogues may omit parkId even though the URL carries it. If the batch itself
  // is unique, keep the user's selection instead of falling back to the all-drives board. Ambiguous
  // same-batch multi-park catalogues still fail closed above.
  const batchMatches = options.filter(
    (option) => option.driveBatchId === driveBatchId && !(option.parkId ?? ""),
  );
  return batchMatches.length === 1 ? batchMatches[0] : undefined;
}
