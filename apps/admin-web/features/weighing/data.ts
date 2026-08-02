import "server-only";

import {
  getWeighingCampaigns,
  getWeighingPlannerCatalog,
  getWeighingPlannerParkBuckets,
  type ApiResult,
  type WeighingCampaign as ApiWeighingCampaign,
  type WeighingCampaignShed as ApiWeighingCampaignShed,
  type WeighingPlannerCatalogResponse,
  type WeighingPlannerPark as ApiWeighingPlannerPark,
  type WeighingPlannerShed as ApiWeighingPlannerShed,
} from "@/lib/api/server";

export type WeighingRole = "leadership" | "director" | "operator";
export type WeighingCampaignState =
  "draft" | "published" | "in_progress" | "delayed" | "completed" | "closed";
export type WeighingCategory = "individual_animal" | "per_shed_partition";
export type WeighingScopeStatus =
  "pending" | "in_progress" | "needs_review" | "completed" | "closed" | "delayed";

export type WeighingScopeRow = {
  id: string;
  parkName: string;
  shedName: string;
  partitionName: string;
  category: WeighingCategory;
  completedCount: number;
  /** True if completedCount is backed by a real per-shed captured count from the backend.
   *  False if it is fabricated (e.g., status="completed" → 1, else → 0).
   *  When false, render the element disabled-with-reason per AGENTS.md binding rules. */
  capturedCountIsBacked: boolean;
  wrongShedCount: number;
  proofPendingCount: number;
  readyToClose: boolean;
  status: WeighingScopeStatus;
  operatorName: string;
  plannedDate: string;
  effectiveDate: string;
};

export type WeighingWrongShedRow = {
  id: string;
  animalDisplayId: string;
  rfid: string;
  originalShed: string;
  originalPartition: string;
  actualShed: string;
  currentPartition: string;
  scannedAt: string;
  operatorName: string;
};

export type WeighingMissingRow = {
  id: string;
  animalDisplayId: string;
  originalShed: string;
  currentTruth: string;
  classification: string;
  checkedAt: string;
};

export type WeighingCampaign = {
  id: string;
  weekLabel: string;
  weekStart: string;
  weekEnd: string;
  startBusinessDate: string;
  state: WeighingCampaignState;
  laneLabel: string;
  operatorName: string;
  selectedScopes: number;
  individualCompleted: number;
  shedPartitionCompleted: number;
  wrongShedScans: number;
  proofPending: number;
  canCreate: boolean;
  canEdit: boolean;
  canPublish: boolean;
  canExecute: boolean;
  reviewOnly: boolean;
  scopes: WeighingScopeRow[];
  wrongShedRows: WeighingWrongShedRow[];
  missingRows: WeighingMissingRow[];
};

export type WeighingPageData = {
  role: WeighingRole;
  campaign: WeighingCampaign;
  planner: WeighingPlanner;
  weeks: Array<{
    key: string;
    label: string;
    state: WeighingCampaignState | "no_task";
  }>;
};

export type WeighingPlannerPark = {
  id: string;
  label: string;
  subtitle: string;
  kidCount: number;
  selected: boolean;
};

export type WeighingPlannerShed = {
  id: string;
  parkId: string;
  locationType: "shed" | "cohort" | "pen";
  label: string;
  subtitle: string;
  kidCount: number;
  selected: boolean;
  category: WeighingCategory;
};

export type WeighingPlannerOperator = {
  id: string;
  name: string;
  capabilityLabel: string;
  selected: boolean;
  disabled?: boolean;
};

export type WeighingPlanner = {
  weekLabel: string;
  periodStartDate: string;
  periodEndDate: string;
  startBusinessDate: string;
  plannedCapPerDay: number;
  lane: "weekly_kids";
  existingCampaignId?: string;
  existingCampaignState?: WeighingCampaignState;
  existingCampaignWeekLabel?: string;
  existingCampaignOperatorName?: string;
  existingCampaignShedCount?: number;
  editingCampaignId?: string;
  duplicateBlocked: boolean;
  parks: WeighingPlannerPark[];
  sheds: WeighingPlannerShed[];
  shedListTruncated: boolean;
  operators: WeighingPlannerOperator[];
  selectedParkId: string;
  selectedOperatorId: string;
  individualShedCount: number;
  individualKidCount: number;
  lumpsumShedCount: number;
  lumpsumKidCount: number;
};

export function roleFromSearchParam(value: string | undefined): WeighingRole {
  if (value === "director" || value === "operator") return value;
  return "leadership";
}

export async function getWeighingPageData(
  role: WeighingRole,
  selectedWeek?: string,
  selectedCampaignId?: string,
  selectedParkId?: string,
): Promise<ApiResult<WeighingPageData>> {
  const result = await getAllWeighingCampaigns();
  if (!result.ok) return result;
  const selectedItem = selectCampaign(result.data.items, selectedWeek, selectedCampaignId);
  const campaign =
    selectedItem
      ? campaignFromApi(selectedItem, role)
      : emptyCampaign(role);
  const plannerWeek = selectedWeek || campaign.weekStart || currentWeekStart();
  const catalogResult = await getWeighingPlannerCatalog(plannerWeek);
  if (!catalogResult.ok) return catalogResult;
  // The catalog is PARK grain and carries no sheds. The planner only ever renders the
  // SELECTED park's buckets, so read exactly that park's page instead of every shed of
  // every park (which is what the flattened catalog used to hand back).
  const selectedPark = selectPlannerPark(catalogResult.data.parks, selectedItem, selectedParkId);
  const bucketsResult = selectedPark
    ? await getSelectedParkBuckets(selectedPark.park_id, plannerWeek)
    : ({ ok: true, data: { sheds: [], truncated: false } } as ApiResult<{ sheds: ApiWeighingPlannerShed[]; truncated: boolean }>);
  if (!bucketsResult.ok) return bucketsResult;
  const planner = plannerFromCatalog(
    catalogResult.data,
    bucketsResult.data.sheds,
    bucketsResult.data.truncated,
    plannerWeek,
    selectedItem,
    campaign,
    selectedCampaignId,
    selectedParkId,
  );
  const weeks = weeksFromCampaigns(result.data.items, campaign);
  return {
    ok: true,
    data: {
      role,
      campaign,
      planner,
      weeks,
    },
  };
}

// getSelectedParkBuckets reads ONE park's buckets. A real park holds 76+ sheds, so the
// default 100-row page usually settles it in a single request; the loop exists only so a
// larger park still resolves, and it is hard-capped.
async function getSelectedParkBuckets(
  parkId: string,
  periodStartDate: string,
): Promise<ApiResult<{ sheds: ApiWeighingPlannerShed[]; truncated: boolean }>> {
  const sheds: ApiWeighingPlannerShed[] = [];
  let cursor: string | undefined;
  let truncated = false;
  for (let page = 0; page < 5; page += 1) { // scale-guard:ignore: bounded to ONE park's sheds (76+ in the real data) with a 5-page hard cap; this is the planner's selection list, not a KPI drained from a paginated endpoint; serial-await: allow cursor pagination must stay sequential
    const result = await getWeighingPlannerParkBuckets(parkId, periodStartDate, cursor);
    if (!result.ok) return result;
    sheds.push(...(result.data.sheds ?? []));
    cursor = result.data.next_cursor || undefined;
    if (!cursor) break;
    if (page === 4) { // loop exits on page 5, if cursor still exists, we hit the cap
      truncated = true;
    }
  }
  return { ok: true, data: { sheds, truncated } };
}

async function getAllWeighingCampaigns(): Promise<ApiResult<{ items: ApiWeighingCampaign[] }>> {
  const items: ApiWeighingCampaign[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 20; page += 1) { // scale-guard:ignore: admin Weighing page is intentionally hidden; this drains only campaign headers with a 20-page hard cap until a summary endpoint replaces it; serial-await: allow cursor pagination must stay sequential
    const result = await getWeighingCampaigns({ cursor, limit: 100 });
    if (!result.ok) return result;
    items.push(...result.data.items);
    cursor = result.data.next_cursor || undefined;
    if (!cursor) break;
  }
  return { ok: true, data: { items } };
}

function selectCampaign(
  items: ApiWeighingCampaign[],
  selectedWeek?: string,
  selectedCampaignId?: string,
): ApiWeighingCampaign | undefined {
  if (selectedCampaignId) {
    const byCampaign = items.find((item) => item.campaign_id === selectedCampaignId);
    if (byCampaign) return byCampaign;
  }
  if (selectedWeek) {
    const byWeek = items.find((item) => item.period_start_date === selectedWeek);
    if (byWeek) return byWeek;
  }
  return items[0];
}

function weeksFromCampaigns(
  items: ApiWeighingCampaign[],
  selected: WeighingCampaign,
): WeighingPageData["weeks"] {
  const liveWeeks = items
    .slice()
    .sort((a, b) => a.period_start_date.localeCompare(b.period_start_date))
    .map((item) => ({
      key: item.period_start_date,
      label: weekRangeLabel(item.period_start_date, item.period_end_date),
      state: item.status === "canceled" ? "delayed" as const : item.status,
    }));
  if (liveWeeks.length > 0) return liveWeeks;
  return [
    {
      key: selected.weekStart || "empty",
      label: selected.weekLabel,
      state: "no_task",
    },
  ];
}

function weekRangeLabel(start: string, end: string): string {
  const startDate = parseYmd(start);
  const endDate = parseYmd(end);
  if (!startDate || !endDate) return `${start} to ${end}`;
  const startLabel = new Intl.DateTimeFormat("en", { day: "numeric", month: "short" }).format(startDate);
  const endLabel = new Intl.DateTimeFormat("en", { day: "numeric", month: "short" }).format(endDate);
  return `${startLabel}-${endLabel}`;
}

function parseYmd(value: string): Date | null {
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) return null;
  return new Date(Date.UTC(year, month - 1, day));
}

function plannerFromCatalog(
  catalog: WeighingPlannerCatalogResponse,
  parkSheds: ApiWeighingPlannerShed[],
  shedListTruncated: boolean,
  periodStartDate: string,
  selectedItem: ApiWeighingCampaign | undefined,
  campaign: WeighingCampaign,
  selectedCampaignId?: string,
  selectedParkId?: string,
): WeighingPlanner {
  const selectedPark = selectPlannerPark(catalog.parks, selectedItem, selectedParkId);
  const selectedShedIds = new Set((selectedItem?.sheds ?? []).map((shed) => shed.location_id));
  const selectedOperatorId = selectedItem?.operator_user_id || catalog.operators[0]?.user_id || "";
  // parkSheds are the SELECTED park's buckets only, so every row here belongs to it.
  const sheds: WeighingPlannerShed[] = parkSheds.map((shed) => {
    const campaignShed = selectedItem?.sheds?.find((item) => item.location_id === shed.location_id);
    return {
      id: shed.location_id,
      parkId: selectedPark?.park_id ?? "",
      locationType: campaignShed?.location_type ?? "shed",
      label: shed.name,
      subtitle: `${selectedPark?.name ?? ""} kid shed`,
      kidCount: shed.kid_count,
      selected: selectedShedIds.size > 0 ? selectedShedIds.has(shed.location_id) : true,
      category: campaignShed?.weighing_category ?? "individual_animal",
    };
  });
  const selected = sheds.filter((shed) => shed.selected && shed.parkId === selectedPark?.park_id);
  const individual = selected.filter((shed) => shed.category === "individual_animal");
  const lumpsum = selected.filter((shed) => shed.category === "per_shed_partition");
  const existing = selectedPark?.existing_campaign;
  const editingCampaignId = selectedCampaignId && selectedItem?.campaign_id === selectedCampaignId
    ? selectedCampaignId
    : undefined;
  const periodEndDate = selectedItem?.period_end_date ?? addDays(periodStartDate, 6);
  return {
    weekLabel: weekRangeLabel(periodStartDate, periodEndDate),
    periodStartDate,
    periodEndDate,
    startBusinessDate: selectedItem?.start_business_date ?? periodStartDate,
    plannedCapPerDay: selectedItem?.planned_cap_per_day ?? 100,
    lane: "weekly_kids",
    existingCampaignId: existing?.campaign_id ?? (campaign.id !== "empty" ? campaign.id : undefined),
    existingCampaignState: existing ? campaignState(existing.status) : (campaign.id !== "empty" ? campaign.state : undefined),
    existingCampaignWeekLabel: existing ? weekRangeLabel(existing.period_start_date, existing.period_end_date) : (campaign.id !== "empty" ? campaign.weekLabel : undefined),
    existingCampaignOperatorName: operatorName(catalog, existing?.operator_user_id ?? selectedItem?.operator_user_id),
    existingCampaignShedCount: existing?.shed_count ?? (campaign.id !== "empty" ? campaign.selectedScopes : undefined),
    editingCampaignId,
    duplicateBlocked: !editingCampaignId && Boolean(existing || campaign.id !== "empty"),
    selectedParkId: selectedPark?.park_id ?? "",
    selectedOperatorId,
    // EVERY park the catalog returned. The subtitle is the park-grain shed count the
    // backend computed, never a join of the shed rows that happened to be fetched.
    parks: catalog.parks.map((park) => ({
      id: park.park_id,
      label: park.name,
      subtitle: `${park.shed_count} kid sheds`,
      kidCount: park.kid_count,
      selected: park.park_id === selectedPark?.park_id,
    })),
    sheds,
    shedListTruncated,
    operators: catalog.operators.map((operator) => ({
      id: operator.user_id,
      name: operator.display_name,
      capabilityLabel: operator.display_code,
      selected: operator.user_id === selectedOperatorId,
    })),
    individualShedCount: individual.length,
    individualKidCount: individual.reduce((sum, shed) => sum + shed.kidCount, 0),
    lumpsumShedCount: lumpsum.length,
    lumpsumKidCount: lumpsum.reduce((sum, shed) => sum + shed.kidCount, 0),
  };
}

function selectPlannerPark(
  parks: ApiWeighingPlannerPark[],
  selectedItem: ApiWeighingCampaign | undefined,
  selectedParkId?: string,
): ApiWeighingPlannerPark | undefined {
  // User-selected park (from form/URL) takes precedence over campaign/default.
  if (selectedParkId) {
    return parks.find((park) => park.park_id === selectedParkId) ?? parks[0];
  }
  if (selectedItem) {
    return parks.find((park) => park.park_id === selectedItem.park_id) ?? parks[0];
  }
  return parks.find((park) => park.existing_campaign) ?? parks[0];
}

function operatorName(catalog: WeighingPlannerCatalogResponse, operatorId?: string): string | undefined {
  if (!operatorId) return undefined;
  return catalog.operators.find((operator) => operator.user_id === operatorId)?.display_name;
}

function campaignState(status: ApiWeighingCampaign["status"]): WeighingCampaignState {
  return status === "canceled" ? "delayed" : status;
}

function currentWeekStart(): string {
  const now = new Date();
  const date = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  const day = date.getUTCDay();
  const diff = day === 0 ? -6 : 1 - day;
  date.setUTCDate(date.getUTCDate() + diff);
  return date.toISOString().slice(0, 10);
}

function addDays(value: string, days: number): string {
  const date = parseYmd(value);
  if (!date) return value;
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

function campaignFromApi(
  item: ApiWeighingCampaign,
  role: WeighingRole,
): WeighingCampaign {
  const leadership = role === "leadership";
  const operator = role === "operator";
  const progress = item.progress;
  const scopes = (item.sheds ?? []).map((shed) => scopeFromApi(item, shed));
  const individualCompleted = progress.individual_completed_count;
  const shedPartitionCompleted = progress.per_scope_completed_count;
  // Aggregate campaign-level attention state from real bucket values.
  const proofPending = scopes.reduce((sum, scope) => sum + scope.proofPendingCount, 0);

  // Campaign-level operator: use the first shed's operator if available.
  // If no sheds, or operator_display_name is empty, use a fallback based on whether
  // a campaign operator_user_id exists (roster gap) or not (unassigned).
  let campaignOperatorName = "";
  if (scopes.length > 0 && scopes[0].operatorName) {
    campaignOperatorName = scopes[0].operatorName;
  } else if (item.operator_user_id) {
    campaignOperatorName = "Roster gap (operator not found)";
  } else {
    campaignOperatorName = "Unassigned";
  }

  return {
    id: item.campaign_id,
    weekLabel: weekRangeLabel(item.period_start_date, item.period_end_date),
    weekStart: item.period_start_date,
    weekEnd: item.period_end_date,
    startBusinessDate: item.start_business_date,
    state: item.status === "canceled" ? "delayed" : item.status,
    laneLabel: "Weekly kids",
    operatorName: campaignOperatorName,
    selectedScopes: scopes.length,
    individualCompleted,
    shedPartitionCompleted,
    wrongShedScans: progress.wrong_shed_count,
    proofPending,
    canCreate: leadership,
    canEdit: leadership,
    canPublish: leadership && item.status === "draft",
    canExecute: operator,
    reviewOnly: role === "director",
    scopes,
    wrongShedRows: [],
    missingRows: [],
  };
}

function scopeFromApi(
  campaign: ApiWeighingCampaign,
  shed: ApiWeighingCampaignShed,
): WeighingScopeRow {
  // Weighing is free-flow: only show completion status, not expected vs actual.
  // completedCount is derived from status, not from API count fields.
  const completedCount = shed.status === "completed" ? 1 : 0; // 1 = scope is done, 0 = still open

  // operator_display_name is backend-resolved. Empty WITH a non-empty operator_user_id means roster gap.
  let operatorDisplay = shed.operator_display_name?.trim() || "";
  if (!operatorDisplay) {
    if (shed.operator_user_id) {
      operatorDisplay = "Roster gap (operator not found)";
    } else {
      operatorDisplay = "Unassigned";
    }
  }

  return {
    id: shed.campaign_shed_id,
    parkName: "Park not reported by API",
    shedName: shed.display_name,
    partitionName: shed.location_type,
    category: shed.weighing_category,
    completedCount,
    // Weighing is free-flow: no per-shed captured count exists on WeighingCampaignShed.
    // expected_animal_count exists but was explicitly rejected (maintainer 2026-07-31).
    // The count is fabricated (status="completed" → 1, else → 0) and should render
    // disabled-with-reason per AGENTS.md binding rule for un-backed UI.
    capturedCountIsBacked: false,
    wrongShedCount: 0,
    proofPendingCount: shed.pending_verification_count,
    readyToClose: shed.ready_to_close,
    status:
      shed.status === "pending"
        ? "pending"
        : shed.status === "canceled"
          ? "delayed"
          : shed.status,
    operatorName: operatorDisplay,
    plannedDate: campaign.start_business_date,
    effectiveDate: campaign.start_business_date,
  };
}

function emptyCampaign(role: WeighingRole): WeighingCampaign {
  const leadership = role === "leadership";
  const operator = role === "operator";
  return {
    id: "empty",
    weekLabel: "No campaign",
    weekStart: "",
    weekEnd: "",
    startBusinessDate: "",
    state: "draft",
    laneLabel: "Weekly kids",
    operatorName: "Unassigned",
    selectedScopes: 0,
    individualCompleted: 0,
    shedPartitionCompleted: 0,
    wrongShedScans: 0,
    proofPending: 0,
    canCreate: leadership,
    canEdit: false,
    canPublish: false,
    canExecute: operator,
    reviewOnly: role === "director",
    scopes: [],
    wrongShedRows: [],
    missingRows: [],
  };
}
