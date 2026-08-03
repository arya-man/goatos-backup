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
  /** FACT 1 of 2 (backend-owned, animals_weighed_count): ANIMALS this bucket has a recorded
   *  weight for, submitted or not. A plain count, NEVER a numerator: weighing is free-flow, so
   *  there is no expected-animal total to take a share of. */
  weighedCount: number;
  /** FACT 2 of 2 (backend-owned, animals_submitted_count): the subset of weighedCount that has
   *  been SUBMITTED for verification. Always shown alongside weighedCount as "N weighed ·
   *  N submitted" —
   *  never alone, and never divided into the other. When work exists and this is 0 the row also
   *  carries a "Not submitted" chip, mirroring the operator's own Submit button. */
  submittedCount: number;
  /** True if the two counts above are backed by real backend facts (never fabricated).
   *  When false, render the element disabled-with-reason per AGENTS.md binding rules. */
  weighedCountIsBacked: boolean;
  proofPendingCount: number;
  readyToClose: boolean;
  status: WeighingScopeStatus;
  operatorName: string;
  plannedDate: string;
  effectiveDate: string;
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
  proofPending: number;
  canCreate: boolean;
  canEdit: boolean;
  canPublish: boolean;
  canExecute: boolean;
  reviewOnly: boolean;
  scopes: WeighingScopeRow[];
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
  // scheduled means ANOTHER open task already holds this shed on the requested
  // weigh date (the task being edited is excluded server-side). It is shed-grain
  // and date-scoped -- the only thing that actually blocks planning this bucket.
  scheduled: boolean;
  scheduledReason?: string;
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
  // existingTaskCount is how many tasks this park already holds in the selected
  // week. It is INFORMATION, never a gate: a park-week may legitimately hold
  // several tasks, because the capture category is a per-shed property and the
  // leftover sheds are planned as their own task. Availability is decided per
  // shed (WeighingPlannerShed.scheduled), not per park-week.
  existingTaskCount: number;
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
  const selectedItem = selectCampaign(result.data.items, selectedWeek, selectedCampaignId, selectedParkId);
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
  // The task being EDITED is excluded from the "already scheduled" check, or its own
  // buckets would read back as taken and the edit screen would disable exactly the
  // sheds it owns. When planning a NEW task nothing is excluded, so every other
  // task's buckets -- including a sibling task in the same park-week -- correctly
  // read as taken and cannot be double-booked.
  const editedCampaignId =
    selectedCampaignId && selectedItem?.campaign_id === selectedCampaignId ? selectedCampaignId : undefined;
  const bucketsResult = selectedPark
    ? await getSelectedParkBuckets(selectedPark.park_id, plannerWeek, editedCampaignId)
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
  excludeCampaignId?: string,
): Promise<ApiResult<{ sheds: ApiWeighingPlannerShed[]; truncated: boolean }>> {
  const sheds: ApiWeighingPlannerShed[] = [];
  let cursor: string | undefined;
  let truncated = false;
  for (let page = 0; page < 5; page += 1) { // scale-guard:ignore: bounded to ONE park's sheds (76+ in the real data) with a 5-page hard cap; this is the planner's selection list, not a KPI drained from a paginated endpoint; serial-await: allow cursor pagination must stay sequential
    const result = await getWeighingPlannerParkBuckets(parkId, periodStartDate, cursor, 100, excludeCampaignId);
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
  selectedParkId?: string,
): ApiWeighingCampaign | undefined {
  // A campaign from Park A must NEVER be selected as "the current campaign" while
  // Park B is displayed (W14). When an explicit park is on the URL, every lookup
  // below is scoped to that park's own campaigns only.
  const scoped = selectedParkId
    ? items.filter((item) => item.park_id === selectedParkId)
    : items;
  if (selectedCampaignId) {
    const byCampaign = scoped.find((item) => item.campaign_id === selectedCampaignId);
    if (byCampaign) return byCampaign;
    // selectedCampaignId belongs to a different park than the one on screen (or does
    // not exist) -- fall through to week/park-default lookup instead of ever
    // returning a cross-park campaign.
  }
  if (selectedWeek) {
    const byWeek = scoped.find((item) => item.period_start_date === selectedWeek);
    if (byWeek) return byWeek;
  }
  if (selectedParkId) {
    // Explicit park selected and nothing matched: this park is campaign-less for the
    // requested week/campaign. Never default to another park's campaign (items[0]).
    return scoped[0];
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
    // The backend already reports, per shed and scoped to the requested weigh date,
    // whether ANOTHER open task holds this bucket -- including a sibling task in the
    // same park-week. That per-shed fact is the real availability, and it is what
    // decides whether this shed may be planned, so it is rendered disabled with the
    // reason instead of being offered and then rejected by the API with a 409.
    const scheduled = Boolean(shed.scheduled);
    const scheduledBy = shed.scheduled_operator_display_name?.trim();
    return {
      id: shed.location_id,
      parkId: selectedPark?.park_id ?? "",
      locationType: campaignShed?.location_type ?? "shed",
      label: shed.name,
      subtitle: `${selectedPark?.name ?? ""} kid shed`,
      kidCount: shed.kid_count,
      // A shed another task already owns must never come back pre-ticked -- the
      // "nothing chosen yet, so select all" default would otherwise guarantee a
      // conflict the moment a park holds a second task.
      selected: scheduled
        ? false
        : selectedShedIds.size > 0
          ? selectedShedIds.has(shed.location_id)
          : true,
      category: campaignShed?.weighing_category ?? "individual_animal",
      scheduled,
      scheduledReason: scheduled
        ? scheduledBy
          ? `Already scheduled on this date by ${scheduledBy}`
          : "Already scheduled on this date by another task"
        : undefined,
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
    existingTaskCount: editingCampaignId ? 0 : (selectedPark?.existing_campaign_count ?? (existing ? 1 : 0)),
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
  // CROSS-SURFACE COUNT PARITY. These tiles used to read progress.individual_completed_count /
  // per_scope_completed_count, which is a THIRD derivation of "how many animals" living on the
  // same page as the shed rows' weighed/submitted pair -- and it disagreed with them twice over:
  // the individual tile excludes per_shed_partition buckets entirely (a 40-animal lump-sum proof
  // rendered "0 captured"), while the per-shed tile counted BUCKETS under the same word the tile
  // beside it used for ANIMALS. Both tiles now sum the SAME backend-owned facts the rows render,
  // so one business fact has exactly one definition on this screen.
  const individualCompleted = scopes
    .filter((scope) => scope.category === "individual_animal")
    .reduce((sum, scope) => sum + scope.weighedCount, 0);
  const shedPartitionCompleted = scopes
    .filter((scope) => scope.category !== "individual_animal")
    .reduce((sum, scope) => sum + scope.weighedCount, 0);
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
    proofPending,
    canCreate: leadership,
    canEdit: leadership,
    canPublish: leadership && item.status === "draft",
    canExecute: operator,
    reviewOnly: role === "director",
    scopes,
  };
}

function scopeFromApi(
  campaign: ApiWeighingCampaign,
  shed: ApiWeighingCampaignShed,
): WeighingScopeRow {
  // Weighing is free-flow: report what the bucket ACTUALLY holds, never a share of an
  // expected total that does not exist. TWO named backend facts, never one ambiguous number:
  // animals_weighed_count is what has been put on the scale, animals_submitted_count is what
  // has been sent for verification. The mobile task-detail card and the director Operators
  // screen render the SAME two fields, so no two surfaces can answer "how many were weighed"
  // — or "how many of those were submitted" — differently.
  const weighedCount = shed.animals_weighed_count;
  const submittedCount = shed.animals_submitted_count;

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
    weighedCount,
    submittedCount,
    // Backed by the backend's animals_weighed_count / animals_submitted_count.
    // expected_animal_count stays rejected (maintainer 2026-07-31) and is still never
    // divided into anything.
    weighedCountIsBacked: true,
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
    proofPending: 0,
    canCreate: leadership,
    canEdit: false,
    canPublish: false,
    canExecute: operator,
    reviewOnly: role === "director",
    scopes: [],
  };
}
