import "server-only";

import type { ApiResult } from "@/lib/api/server";

export type WeighingRole = "leadership" | "director" | "operator";
export type WeighingCampaignState = "draft" | "published" | "in_progress" | "delayed" | "completed";
export type WeighingCategory = "individual_animal" | "per_shed_partition";
export type WeighingScopeStatus = "pending" | "in_progress" | "needs_review" | "completed" | "delayed";

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
  weeks: Array<{ key: string; label: string; state: WeighingCampaignState | "no_task" }>;
};

export function roleFromSearchParam(value: string | undefined): WeighingRole {
  if (value === "director" || value === "operator") return value;
  return "leadership";
}

export async function getWeighingPageData(role: WeighingRole): Promise<ApiResult<WeighingPageData>> {
  const campaign = fixtureCampaign(role);
  return {
    ok: true,
    data: {
      role,
      campaign,
      weeks: [
        { key: "2026-07-19", label: "19-25 Jul", state: "completed" },
        { key: "2026-07-26", label: "26 Jul-1 Aug", state: campaign.state },
        { key: "2026-08-02", label: "2-8 Aug", state: "no_task" },
      ],
    },
  };
}

function fixtureCampaign(role: WeighingRole): WeighingCampaign {
  const leadership = role === "leadership";
  const operator = role === "operator";
  return {
    id: "weigh-2026-07-26-cpt-kids",
    weekLabel: "26 Jul-1 Aug",
    weekStart: "2026-07-26",
    weekEnd: "2026-08-01",
    startBusinessDate: "2026-07-29",
    state: "in_progress",
    laneLabel: "Weekly kids / K-F",
    operatorName: "Amit Kumar",
    selectedScopes: 5,
    expectedAnimals: 250,
    individualExpected: 180,
    individualCompleted: 126,
    shedPartitionExpected: 2,
    shedPartitionCompleted: 1,
    remaining: 55,
    rolledForward: 20,
    wrongShedScans: 3,
    unavailableAnimals: 8,
    proofPending: 11,
    canCreate: leadership,
    canEdit: leadership,
    canPublish: leadership,
    canExecute: operator,
    reviewOnly: role === "director",
    scopes: [
      {
        id: "s1",
        parkName: "Channapatna",
        shedName: "Gandhi",
        partitionName: "Part 1",
        category: "individual_animal",
        expectedCount: 80,
        completedCount: 80,
        remainingCount: 0,
        unavailableCount: 0,
        wrongShedCount: 1,
        proofPendingCount: 0,
        status: "completed",
        operatorName: "Amit Kumar",
        plannedDate: "2026-07-29",
        effectiveDate: "2026-07-29",
      },
      {
        id: "s2",
        parkName: "Channapatna",
        shedName: "Gandhi",
        partitionName: "Part 2",
        category: "per_shed_partition",
        expectedCount: 1,
        completedCount: 1,
        remainingCount: 0,
        unavailableCount: 0,
        wrongShedCount: 0,
        proofPendingCount: 0,
        status: "completed",
        operatorName: "Amit Kumar",
        plannedDate: "2026-07-29",
        effectiveDate: "2026-07-29",
      },
      {
        id: "s3",
        parkName: "Channapatna",
        shedName: "Godel",
        partitionName: "Part 3",
        category: "individual_animal",
        expectedCount: 50,
        completedCount: 36,
        remainingCount: 14,
        unavailableCount: 3,
        wrongShedCount: 2,
        proofPendingCount: 7,
        status: "needs_review",
        operatorName: "Amit Kumar",
        plannedDate: "2026-07-30",
        effectiveDate: "2026-07-30",
      },
      {
        id: "s4",
        parkName: "Channapatna",
        shedName: "Nandi",
        partitionName: "Whole shed",
        category: "individual_animal",
        expectedCount: 50,
        completedCount: 10,
        remainingCount: 40,
        unavailableCount: 5,
        wrongShedCount: 0,
        proofPendingCount: 4,
        status: "in_progress",
        operatorName: "Amit Kumar",
        plannedDate: "2026-07-31",
        effectiveDate: "2026-08-02",
      },
      {
        id: "s5",
        parkName: "Channapatna",
        shedName: "Kaveri",
        partitionName: "Part 1",
        category: "per_shed_partition",
        expectedCount: 1,
        completedCount: 0,
        remainingCount: 1,
        unavailableCount: 0,
        wrongShedCount: 0,
        proofPendingCount: 1,
        status: "delayed",
        operatorName: "Amit Kumar",
        plannedDate: "2026-07-31",
        effectiveDate: "2026-08-02",
      },
    ],
    wrongShedRows: [
      {
        id: "w1",
        animalDisplayId: "CPT-KID-0142",
        rfid: "CPT-RFID-0142",
        expectedShed: "Gandhi",
        originalPartition: "Part 1",
        actualShed: "Godel",
        currentPartition: "Part 3",
        scannedAt: "2026-07-30T10:22:00+05:30",
        operatorName: "Amit Kumar",
      },
      {
        id: "w2",
        animalDisplayId: "CPT-KID-0198",
        rfid: "CPT-RFID-0198",
        expectedShed: "Godel",
        originalPartition: "Part 3",
        actualShed: "Nandi",
        currentPartition: "Whole shed",
        scannedAt: "2026-07-30T11:08:00+05:30",
        operatorName: "Amit Kumar",
      },
    ],
    missingRows: [
      {
        id: "m1",
        animalDisplayId: "CPT-KID-0207",
        expectedShed: "Godel / Part 3",
        currentTruth: "ICU shed",
        classification: "icu",
        checkedAt: "2026-07-30T18:15:00+05:30",
      },
      {
        id: "m2",
        animalDisplayId: "CPT-KID-0244",
        expectedShed: "Nandi / Whole shed",
        currentTruth: "Sold / transferred",
        classification: "sold_transferred",
        checkedAt: "2026-07-30T18:15:00+05:30",
      },
    ],
  };
}
