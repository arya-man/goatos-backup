import "server-only";

import {
  getWeighingCampaigns,
  type ApiResult,
  type WeighingCampaign as ApiWeighingCampaign,
  type WeighingCampaignShed as ApiWeighingCampaignShed,
} from "@/lib/api/server";

export type WeighingRole = "leadership" | "director" | "operator";
export type WeighingCampaignState =
  "draft" | "published" | "in_progress" | "delayed" | "completed";
export type WeighingCategory = "individual_animal" | "per_shed_partition";
export type WeighingScopeStatus =
  "pending" | "in_progress" | "needs_review" | "completed" | "delayed";

export type WeighingScopeRow = {
  id: string;
  parkName: string;
  shedName: string;
  partitionName: string;
  category: WeighingCategory;
  expectedCount: number;
  completedCount: number;
  remainingCount: number;
  unavailableCount: number;
  wrongShedCount: number;
  proofPendingCount: number;
  status: WeighingScopeStatus;
  operatorName: string;
  plannedDate: string;
  effectiveDate: string;
};

export type WeighingWrongShedRow = {
  id: string;
  animalDisplayId: string;
  rfid: string;
  expectedShed: string;
  originalPartition: string;
  actualShed: string;
  currentPartition: string;
  scannedAt: string;
  operatorName: string;
};

export type WeighingMissingRow = {
  id: string;
  animalDisplayId: string;
  expectedShed: string;
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
  expectedAnimals: number;
  individualExpected: number;
  individualCompleted: number;
  shedPartitionExpected: number;
  shedPartitionCompleted: number;
  remaining: number;
  rolledForward: number;
  wrongShedScans: number;
  unavailableAnimals: number;
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
  duplicateBlocked: boolean;
  parks: WeighingPlannerPark[];
  sheds: WeighingPlannerShed[];
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
): Promise<ApiResult<WeighingPageData>> {
  const result = await getWeighingCampaigns();
  if (!result.ok) return result;
  const campaign =
    result.data.items.length > 0
      ? campaignFromApi(result.data.items[0], role)
      : emptyCampaign(role);
  const planner = defaultPlanner(campaign);
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

function defaultPlanner(campaign?: WeighingCampaign): WeighingPlanner {
  const sheds: WeighingPlannerShed[] = [
    { id: "11111111-1111-4111-8111-000000000101", locationType: "shed", label: "Castro 1", subtitle: "kid shed", kidCount: 80, selected: true, category: "per_shed_partition" },
    { id: "11111111-1111-4111-8111-000000000102", locationType: "shed", label: "Castro 2", subtitle: "kid shed", kidCount: 64, selected: true, category: "per_shed_partition" },
    { id: "11111111-1111-4111-8111-000000000103", locationType: "shed", label: "Godel 2 · Part 1", subtitle: "kid shed", kidCount: 92, selected: false, category: "individual_animal" },
    { id: "11111111-1111-4111-8111-000000000104", locationType: "shed", label: "Godel 2 · Part 2", subtitle: "kid shed", kidCount: 88, selected: false, category: "individual_animal" },
    { id: "11111111-1111-4111-8111-000000000105", locationType: "shed", label: "Gandhi 1", subtitle: "kid shed", kidCount: 78, selected: true, category: "individual_animal" },
    { id: "11111111-1111-4111-8111-000000000106", locationType: "shed", label: "Gandhi 2", subtitle: "kid shed", kidCount: 64, selected: false, category: "individual_animal" },
    { id: "11111111-1111-4111-8111-000000000107", locationType: "shed", label: "Godel 1 · Part 3", subtitle: "kid shed", kidCount: 50, selected: false, category: "individual_animal" },
  ];
  const selected = sheds.filter((shed) => shed.selected);
  const individual = selected.filter((shed) => shed.category === "individual_animal");
  const lumpsum = selected.filter((shed) => shed.category === "per_shed_partition");
  return {
    weekLabel: "Week 31 · 27 Jul - 2 Aug",
    periodStartDate: "2026-07-27",
    periodEndDate: "2026-08-02",
    startBusinessDate: "2026-07-29",
    plannedCapPerDay: 100,
    lane: "weekly_kids",
    existingCampaignId: campaign && campaign.id !== "empty" ? campaign.id : undefined,
    existingCampaignState: campaign && campaign.id !== "empty" ? campaign.state : undefined,
    existingCampaignWeekLabel: campaign && campaign.id !== "empty" ? campaign.weekLabel : undefined,
    existingCampaignOperatorName: campaign && campaign.id !== "empty" ? campaign.operatorName : undefined,
    existingCampaignShedCount: campaign && campaign.id !== "empty" ? campaign.selectedScopes : undefined,
    duplicateBlocked: Boolean(campaign && campaign.id !== "empty"),
    selectedParkId: "11111111-1111-4111-8111-000000000001",
    selectedOperatorId: "30303030-3030-4303-8303-303030303030",
    parks: [
      { id: "11111111-1111-4111-8111-000000000001", label: "CPT · Channapatna", subtitle: "Castro, Gandhi, Godel", kidCount: 516, selected: true },
      { id: "11111111-1111-4111-8111-000000000002", label: "CBE · Coimbatore", subtitle: "Castro 1 / 2 / 3", kidCount: 286, selected: false },
    ],
    sheds,
    operators: [
      { id: "30303030-3030-4303-8303-303030303030", name: "Amit Kumar", capabilityLabel: "weighing.execute", selected: true },
      { id: "20202020-2020-4202-8202-202020202020", name: "Dinakar", capabilityLabel: "planner / monitor only", selected: false, disabled: true },
    ],
    individualShedCount: individual.length,
    individualKidCount: individual.reduce((sum, shed) => sum + shed.kidCount, 0),
    lumpsumShedCount: lumpsum.length,
    lumpsumKidCount: lumpsum.reduce((sum, shed) => sum + shed.kidCount, 0),
  };
}

function campaignFromApi(
  item: ApiWeighingCampaign,
  role: WeighingRole,
): WeighingCampaign {
  const leadership = role === "leadership";
  const operator = role === "operator";
  const progress = item.progress;
  const scopes = (item.sheds ?? []).map((shed) => scopeFromApi(item, shed));
  const individualExpected = progress.individual_expected_count;
  const individualCompleted = progress.individual_completed_count;
  const shedPartitionExpected = progress.per_scope_expected_count;
  const shedPartitionCompleted = progress.per_scope_completed_count;
  const expectedAnimals = individualExpected + shedPartitionExpected;
  const completed = individualCompleted + shedPartitionCompleted;

  return {
    id: item.campaign_id,
    weekLabel: weekRangeLabel(item.period_start_date, item.period_end_date),
    weekStart: item.period_start_date,
    weekEnd: item.period_end_date,
    startBusinessDate: item.start_business_date,
    state: item.status === "canceled" ? "delayed" : item.status,
    laneLabel: "Weekly kids",
    operatorName: "Operator not reported by API",
    selectedScopes: scopes.length,
    expectedAnimals,
    individualExpected,
    individualCompleted,
    shedPartitionExpected,
    shedPartitionCompleted,
    remaining: progress.remaining_count,
    rolledForward: item.status === "delayed" ? progress.remaining_count : 0,
    wrongShedScans: progress.wrong_shed_count,
    unavailableAnimals: progress.missing_count,
    proofPending: 0,
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
  const completedCount =
    shed.status === "completed" ? shed.expected_animal_count : 0;
  return {
    id: shed.campaign_shed_id,
    parkName: "Park not reported by API",
    shedName: shed.display_name,
    partitionName: shed.location_type,
    category: shed.weighing_category,
    expectedCount: shed.expected_animal_count,
    completedCount,
    remainingCount: Math.max(0, shed.expected_animal_count - completedCount),
    unavailableCount: 0,
    wrongShedCount: 0,
    proofPendingCount: 0,
    status:
      shed.status === "pending"
        ? "pending"
        : shed.status === "canceled"
          ? "delayed"
          : shed.status,
    operatorName: "Operator not reported by API",
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
    expectedAnimals: 0,
    individualExpected: 0,
    individualCompleted: 0,
    shedPartitionExpected: 0,
    shedPartitionCompleted: 0,
    remaining: 0,
    rolledForward: 0,
    wrongShedScans: 0,
    unavailableAnimals: 0,
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
