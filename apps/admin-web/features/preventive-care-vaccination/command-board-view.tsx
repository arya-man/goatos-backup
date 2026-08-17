"use client";
import { useEffect, useMemo, useState, useTransition } from "react";
import { X } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import type { AppApiComponents } from "@goatos/api-client";
import type { AdminUiPageContract } from "@/lib/admin-ui-contract";
import { copy, optionGroup } from "@/lib/admin-ui-contract";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  commonDriveName,
  driveSelectionValue,
  executedDriveCampaigns,
  formatDateSpan,
  formatScheduledDriveDates,
  parseDriveSelectionValue,
  scheduledDriveCampaigns,
  scheduledDriveRows,
  sortDriveCampaignsNewestFirst,
  type CommandBoardDriveOption,
} from "./command-board-future-drives";

// Build colored grid heatmap from flat shed-dose matrix
interface GridCell {
  doseRule: string;
  state: string;
  animalCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  minDueDate?: string | null;
  maxDueDate?: string | null;
}

interface ShedGridRow {
  shedName: string;
  shedId: string;
  partitionLabel: string | null;
  operational_location_display: string | null;
  cells: Record<string, GridCell>;
}

interface AdministeredDateRange {
  min?: string | null;
  max?: string | null;
}

function mergeAdministeredDateRange(
  ranges: Record<string, AdministeredDateRange>,
  vaccine: string,
  min?: string | null,
  max?: string | null,
) {
  const nextMin = min?.slice(0, 10) ?? "";
  const nextMax = (max ?? min)?.slice(0, 10) ?? "";
  if (!nextMin && !nextMax) return;
  const current = ranges[vaccine] ?? {};
  if (nextMin && (!current.min || nextMin < current.min.slice(0, 10))) current.min = min;
  if (nextMax && (!current.max || nextMax > current.max.slice(0, 10))) current.max = max ?? min;
  ranges[vaccine] = current;
}

// Stage -> cohort row, by DECLARED backend membership. Never prefix matching, and Adults is no
// longer a catch-all.
//
// Prefix matching with a trailing catch-all put F2-Female/F2-Male in the adult herd (372 against a
// true 324) and left ICU-Kid — a KID carrying a health-state prefix — in Adults as well. Both are
// the same failure: a label the ladder did not recognise fell through to the last rung and
// silently inflated it.
//
// An unmapped stage now returns ITSELF, so it renders as its own visible row. Warmup is an
// arrival/acclimation state that is neither adult nor kid by label, and it stays visible under its
// own name rather than being guessed into a cohort.
function cohortBucket(managementStage: string, stageMap: Map<string, string>): string {
  const stage = (managementStage || "").trim();
  return stageMap.get(stage.toUpperCase()) ?? stage;
}

interface CohortPivotRow {
  cohort: string;
  animals: number;
  // Three DISJOINT buckets from the backend, by WHO OWES THE NEXT MOVE: the operator (pending),
  // the verifier (submitted), nobody (verified). The cell must show all three — showing only
  // "pending" is what made a fully vaccinated, fully submitted park read identically to a park
  // nobody had touched, and left the CEO with "40 pending" under "40 awaiting verification".
  pending: Record<string, number>;
  submitted: Record<string, number>;
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
  // Per-vaccine day split and dose-sequence exceptions, both backend-owned. The grid shows the
  // exception COUNT (a clean 324 and a 321-with-3-missing must not read alike) and the drilldown
  // shows the days and the animals.
  days: Record<string, CohortDay[]>;
  exceptions: Record<string, { count: number; goats: CohortAnimal[] }>;
  // The real management stages that fold into this rung. They are CEO-level noise in the grid, so
  // they live in the drilldown only — the grid stays one row per cohort.
  members: CohortMember[];
}

interface CohortMember {
  label: string;
  animals: number;
  pending: Record<string, number>;
  submitted: Record<string, number>;
  verified: Record<string, number>;
  administeredDates: Record<string, AdministeredDateRange>;
  exceptions: Record<string, { count: number; goats: CohortAnimal[] }>;
}

type CohortDay = { date: string; animalCount: number };
type CohortAnimal = { goatId: string; displayId: string; tag?: string };

interface CohortCellInput {
  cohort: { parkName: string; managementStage: string; sex: string; animalCount: number };
  vaccineLabel: string;
  pendingCount: number;
  submittedCount?: number;
  verifiedCount: number;
  minAdministeredDate?: string | null;
  maxAdministeredDate?: string | null;
  administeredDays?: CohortDay[];
  missingPriorDoseCount?: number;
  missingPriorDoseGoats?: CohortAnimal[];
}

// Day counts of the same vaccine coming from several (stage, sex) cohorts land on the same cohort
// row, so identical dates ADD rather than overwrite — otherwise "1 Jul: 237" would silently become
// whichever sub-cohort was folded last.
//
// CALLER CONTRACT: only ever feed this the day rows of ONE vaccine label. Repeated dates across
// sub-cohorts are DIFFERENT animals dosed on the same day and must sum; repeated dates from the
// same sub-cohort would double it. The backend already emits one day list per cohort x dose, so
// the caller must not merge two dose codes into one call.
function mergeDays(target: Record<string, CohortDay[]>, vaccine: string, days?: CohortDay[]) {
  if (!days?.length) return;
  const list = target[vaccine] ?? [];
  days.forEach((day) => {
    const found = list.find((candidate) => candidate.date === day.date);
    if (found) found.animalCount += day.animalCount;
    else list.push({ date: day.date, animalCount: day.animalCount });
  });
  list.sort((a, b) => a.date.localeCompare(b.date));
  target[vaccine] = list;
}

function mergeExceptions(
  target: Record<string, { count: number; goats: CohortAnimal[] }>,
  vaccine: string,
  count?: number,
  goats?: CohortAnimal[],
) {
  if (!count) return;
  const current = target[vaccine] ?? { count: 0, goats: [] };
  current.count += count;
  (goats ?? []).forEach((goat) => {
    if (!current.goats.some((candidate) => candidate.goatId === goat.goatId)) current.goats.push(goat);
  });
  target[vaccine] = current;
}

// One matrix per FARM: leadership reads this farmwise, so Channapatna and Coimbatore never
// merge into one set of rows. Farms are ordered by name for a stable read.
function buildCohortFarms(
  matrix: CohortCellInput[],
  ladder: string[],
  stageMap: Map<string, string>,
): Array<{ farm: string; vaccines: string[]; rows: CohortPivotRow[] }> {
  const byFarm = new Map<string, CohortCellInput[]>();
  matrix.forEach((cell) => {
    const farm = cell.cohort.parkName || "";
    const bucket = byFarm.get(farm);
    if (bucket) bucket.push(cell);
    else byFarm.set(farm, [cell]);
  });
  return Array.from(byFarm.entries())
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([farm, cells]) => ({ farm, ...buildCohortPivot(cells, ladder, stageMap) }));
}

function buildCohortPivot(
  matrix: CohortCellInput[],
  ladder: string[],
  stageMap: Map<string, string>,
): { vaccines: string[]; rows: CohortPivotRow[] } {
  const vaccines = Array.from(new Set(matrix.map((c) => c.vaccineLabel).filter(Boolean))).sort();
  // Declared rows PLUS any stage this farm holds that the map does not classify. An unmapped stage
  // gets its own visible row instead of disappearing into another cohort, so a newly introduced or
  // still-undefined label (Warmup today) is something the CEO can see and ask about.
  const unmapped = Array.from(
    new Set(
      matrix
        .map((cell) => cohortBucket(cell.cohort.managementStage, stageMap))
        .filter((row) => row && !ladder.includes(row)),
    ),
  ).sort();
  const rows = [...ladder, ...unmapped].map((cohort) => {
    const pending: Record<string, number> = {};
    const submitted: Record<string, number> = {};
    const verified: Record<string, number> = {};
    const administeredDates: Record<string, AdministeredDateRange> = {};
    const days: Record<string, CohortDay[]> = {};
    const exceptions: Record<string, { count: number; goats: CohortAnimal[] }> = {};
    const members = new Map<string, CohortMember>();
    // Animals are per (stage, sex) cohort and the source repeats a cohort once per vaccine, so
    // head counts accumulate per DISTINCT cohort key — summing the rows directly would multiply
    // the head count by the number of vaccines.
    const counted = new Set<string>();
    let animals = 0;
    matrix.forEach((cell) => {
      if (cohortBucket(cell.cohort.managementStage, stageMap) !== cohort) return;
      const key = `${cell.cohort.managementStage}|${cell.cohort.sex}`;
      pending[cell.vaccineLabel] = (pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      submitted[cell.vaccineLabel] = (submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
      verified[cell.vaccineLabel] = (verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;
      mergeAdministeredDateRange(
        administeredDates,
        cell.vaccineLabel,
        cell.minAdministeredDate,
        cell.maxAdministeredDate,
      );
      mergeDays(days, cell.vaccineLabel, cell.administeredDays);
      mergeExceptions(exceptions, cell.vaccineLabel, cell.missingPriorDoseCount, cell.missingPriorDoseGoats);

      let member = members.get(key);
      if (!member) {
        member = {
          label: `${cell.cohort.managementStage} · ${cell.cohort.sex}`,
          animals: 0,
          pending: {},
          submitted: {},
          verified: {},
          administeredDates: {},
          exceptions: {},
        };
        members.set(key, member);
      }
      mergeExceptions(member.exceptions, cell.vaccineLabel, cell.missingPriorDoseCount, cell.missingPriorDoseGoats);
      member.pending[cell.vaccineLabel] = (member.pending[cell.vaccineLabel] ?? 0) + cell.pendingCount;
      member.submitted[cell.vaccineLabel] =
        (member.submitted[cell.vaccineLabel] ?? 0) + (cell.submittedCount ?? 0);
      member.verified[cell.vaccineLabel] = (member.verified[cell.vaccineLabel] ?? 0) + cell.verifiedCount;
      mergeAdministeredDateRange(
        member.administeredDates,
        cell.vaccineLabel,
        cell.minAdministeredDate,
        cell.maxAdministeredDate,
      );

      if (!counted.has(key)) {
        counted.add(key);
        animals += cell.cohort.animalCount;
        member.animals = cell.cohort.animalCount;
      }
    });
    return {
      cohort,
      animals,
      pending,
      submitted,
      verified,
      administeredDates,
      days,
      exceptions,
      members: Array.from(members.values()).sort((a, b) => b.animals - a.animals),
    };
  });
  return { vaccines, rows };
}

function buildShedGrid(
  matrix: Array<{
    shedId: string;
    shedName: string;
    partition_label?: string | null;
    operational_location_display?: string | null;
    doseRule: string;
    state: string;
    animalCount: number;
    minAdministeredDate?: string | null;
    maxAdministeredDate?: string | null;
    minDueDate?: string | null;
    maxDueDate?: string | null;
  }>
): {
  byDose: string[];
  byShed: ShedGridRow[];
} {
  const doseSet = new Set<string>();
  // BUG FIX: Key by shedId + partition_label (using shedId|partition_label format) instead of shedName.
  // Two same-named sheds in different parks and two partitions of the same shed must remain as separate rows
  // with separate animal counts. Keying by shedName alone caused them to merge, silently summing counts.
  const shedMap = new Map<string, ShedGridRow>();

  matrix.forEach((cell) => {
    doseSet.add(cell.doseRule);
    const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, {
        shedName: cell.shedName,
        shedId: cell.shedId,
        partitionLabel: cell.partition_label ?? null,
        operational_location_display: cell.operational_location_display ?? null,
        cells: {},
      });
    }
    shedMap.get(shedKey)!.cells[cell.doseRule] = {
      doseRule: cell.doseRule,
      state: cell.state,
      animalCount: cell.animalCount,
      minAdministeredDate: cell.minAdministeredDate,
      maxAdministeredDate: cell.maxAdministeredDate,
      minDueDate: cell.minDueDate,
      maxDueDate: cell.maxDueDate,
    };
  });

  return {
    byDose: Array.from(doseSet),
    byShed: Array.from(shedMap.values()),
  };
}

// Derived from the generated client rather than hand-declared. Local mirrors of the response
// schema are why the compiler stayed green while closedWithoutDose and driveOptionsTruncated --
// both REQUIRED by the contract -- were dropped before they reached the render. Deriving makes the
// next dropped field a type error instead of a silent hole in the page.
type CommandBoardResponse = AppApiComponents["schemas"]["VaccinationCommandBoardResponse"];
type CohortCell = CommandBoardResponse["cohortMatrix"][number];
type ShedDoseCell = CommandBoardResponse["shedDoseMatrix"][number];
type CommandBoardKpis = CommandBoardResponse["kpis"] & {
  missedNotGiven?: number;
  closedWithoutDose?: number;
};
type CommandBoardExtras = {
  kpis: CommandBoardKpis;
  shedVaccineMatrix?: ShedVaccineCell[];
  shedVaccineColumns?: Array<{ code: string; label: string }>;
  closedWithoutDoseAnimals?: ClosedWithoutDoseAnimal[];
  driveOptionsTruncated?: boolean;
};
// driveOptions is the one field the view widens: enrichDriveOptions reconstructs counts the skinny
// API catalogue omits and tags them, so the rendered option carries more than the wire schema does.
type CommandBoard = Omit<CommandBoardResponse, "driveOptions" | "kpis"> & CommandBoardExtras & {
  driveOptions?: CommandBoardDriveOption[];
};

type ShedVaccineCell = {
  shedId: string;
  shedName: string;
  partition_label?: string | null;
  operational_location_display?: string | null;
  parkName?: string | null;
  vaccineCode: string;
  state: "behind" | "verifying" | "ok" | "not_planned";
  behindAnimals: number;
  verifyingAnimals?: number;
  totalAnimals: number;
  proofVideos?: Array<{ path: string }>;
  flaggedAnimals?: Array<{
    goatId: string;
    tag?: string | null;
    tag2?: string | null;
    displayId: string;
    partitionLabel?: string | null;
    dueAt?: string | null;
  }>;
};

type ClosedWithoutDoseAnimal = {
  goatId: string;
  tag1?: string | null;
  tag2?: string | null;
  displayId: string;
  operational_location_display: string;
  parkName?: string | null;
  vaccineLabel: string;
  reason: string;
};

interface CommandBoardViewProps {
  board: CommandBoard;
  pageContract: AdminUiPageContract;
  driveBatchId?: string;
  // Park of the selected drive. The API's drive-option grain is (batch, park), so the batch id
  // alone does not identify a row once the same batch runs in two parks.
  driveParkId?: string;
}

const STATUS_KEYS = ["verified", "awaiting", "overdue", "scheduled"] as const;
type StatusKey = (typeof STATUS_KEYS)[number];

function keyDate(value?: string | null): string {
  return value?.match(/^(\d{4}-\d{2}-\d{2})/)?.[1] ?? "";
}

function splitDriveDoseRules(label: string): string[] {
  const head = label.split(" — ")[0] ?? label;
  return head.split(" + ").map((part) => part.trim()).filter(Boolean);
}

function enrichDriveOptions(
  options: CommandBoardDriveOption[],
  matrix: ShedDoseCell[],
  cohortMatrix: CohortCell[],
  targetCap: number,
): CommandBoardDriveOption[] {
  const defaultParkId = cohortMatrix.find((cell) => cell.cohort.parkId)?.cohort.parkId ?? "";
  const defaultParkName = cohortMatrix.find((cell) => cell.cohort.parkName)?.cohort.parkName ?? "";
  return options.map((option) => {
    if (Number.isFinite(option.targetCount) && option.shedNames) return option;
    const doseRules = splitDriveDoseRules(option.driveName || option.label);
    const start = keyDate(option.windowStart || option.plannedDate);
    const end = keyDate(option.windowEnd || option.windowStart || option.plannedDate);
    const cells = matrix.filter((cell) => {
      if (!doseRules.includes(cell.doseRule)) return false;
      const date =
        option.status === "planned"
          ? keyDate(cell.minDueDate)
          : keyDate(cell.minAdministeredDate);
      const expectedState = option.status === "planned" ? "scheduled" : "verified";
      if (cell.state !== expectedState || !date) return false;
      if (option.status !== "planned") return true;
      return (!start || date >= start) && (!end || date <= end);
    });
    // BUG FIX (2026-08-07): OL-2 partition collapse. Key by shedId + partition_label instead of shedName.
    // Two same-named sheds across parks and two partitions of one shed must contribute separate counts.
    // Keying by shedName alone merged them, silently summing counts from disjoint physical locations.
    const byShedKey = new Map<string, { shedId: string; shedName: string; partitionLabel: string | null; operationalLocationDisplay: string | null; animalCount: number }>();
    cells.forEach((cell) => {
      const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
      const existing = byShedKey.get(shedKey);
      if (!existing || cell.animalCount > existing.animalCount) {
        byShedKey.set(shedKey, {
          shedId: cell.shedId,
          shedName: cell.shedName,
          partitionLabel: cell.partition_label ?? null,
          operationalLocationDisplay: cell.operational_location_display ?? null,
          animalCount: cell.animalCount,
        });
      }
    });
    const shedNames = Array.from(byShedKey.values())
      .map((entry) => entry.operationalLocationDisplay || entry.shedName)
      .sort();
    const shedLocations = Array.from(byShedKey.values())
      .map((entry) => ({
        shedId: entry.shedId,
        shedName: entry.shedName,
        ...(entry.partitionLabel ? { partition_label: entry.partitionLabel } : {}),
        operational_location_display: entry.operationalLocationDisplay || entry.shedName,
      }))
      .sort((a, b) => `${a.shedId}|${a.partition_label ?? ""}`.localeCompare(`${b.shedId}|${b.partition_label ?? ""}`));
    const doseCount = cells.reduce((sum, cell) => sum + (cell.animalCount ?? 0), 0);
    let targetCount = 0;
    if ((option.driveName || option.label).includes(" + ")) {
      targetCount = Array.from(byShedKey.values()).reduce((sum, entry) => sum + entry.animalCount, 0);
    } else {
      targetCount = doseCount;
    }
    return {
      ...option,
      driveName: option.driveName || option.label,
      parkId: option.parkId ?? defaultParkId,
      parkName: option.parkName ?? defaultParkName,
      plannedDate: option.plannedDate ?? option.windowStart,
      targetCount: targetCap > 0 ? Math.min(targetCount, targetCap) : targetCount,
      doseCount,
      shedNames,
      shedIds: shedLocations.map((location) => location.shedId),
      shedLocations,
      derivedFromMatrix: true,
    };
  }).filter((option) => option.status !== "planned" || (option.targetCount ?? 0) > 0);
}

// One selected cohort × dose cell, resolved entirely from the row already rendered.
interface SelectedCohortCell {
  key: string;
  farm: string;
  cohort: string;
  vaccine: string;
  animals: number;
  pending: number;
  submitted: number;
  verified: number;
  dateSpan: string;
  days: CohortDay[];
  exceptionCount: number;
  exceptionGoats: CohortAnimal[];
  members: Array<{
    label: string;
    animals: number;
    pending: number;
    submitted: number;
    verified: number;
    exceptions: number;
    dateSpan: string;
  }>;
}

// Reading order and the "not adult" qualifier are backend-owned (the cohort row-order option
// group), so the grid never re-sorts business rows or invents its own qualifier text.
function cohortRowOrder(pageContract: AdminUiPageContract): string[] {
  return optionGroup(pageContract, "command_board_cohort_row_order").map((option) => option.label);
}

function rowQualifier(pageContract: AdminUiPageContract, cohort: string): string {
  const match = optionGroup(pageContract, "command_board_cohort_row_order").find((option) => option.label === cohort);
  return match?.title ?? "";
}

export function CommandBoardView({ board, pageContract, driveBatchId, driveParkId }: CommandBoardViewProps) {
  // Vaccine + status filters operate on the fetched payload. Drive scope is a server read, but
  // blank selection deliberately keeps the all-drives board so leadership sees the full programme.
  const router = useRouter();
  const searchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const currentSearch = searchParams?.toString() ?? "";
  const [optimisticDrive, setOptimisticDrive] = useState<{ from: string; value: string } | null>(null);
  const driveOptions = useMemo(
    () => enrichDriveOptions(board.driveOptions ?? [], board.shedDoseMatrix ?? [], board.cohortMatrix ?? [], board.kpis.targets),
    [board.driveOptions, board.shedDoseMatrix, board.cohortMatrix, board.kpis.targets],
  );
  const futureDrives = useMemo(() => scheduledDriveRows(driveOptions), [driveOptions]);
  const executedCampaigns = useMemo(() => executedDriveCampaigns(driveOptions), [driveOptions]);
  const selectedDrive = optimisticDrive?.from === currentSearch
    ? optimisticDrive.value
    : driveBatchId
      ? driveSelectionValue(driveBatchId, driveParkId)
      : "";

  const selectDrive = (next: string) => {
    setOptimisticDrive({ from: currentSearch, value: next });
    const params = new URLSearchParams(currentSearch);
    const selection = next ? parseDriveSelectionValue(next) : undefined;
    if (selection?.driveBatchId) params.set("cb_drive", selection.driveBatchId); else params.delete("cb_drive");
    if (selection?.parkId) params.set("cb_drive_park", selection.parkId); else params.delete("cb_drive_park");
    const query = params.toString();
    startTransition(() => {
      router.push(query ? `?${query}` : "?", { scroll: false });
    });
  };

  const vaccineOptions = useMemo(() => {
    const labels = (board.cohortMatrix ?? []).map((c) => c.vaccineLabel).filter(Boolean);
    return Array.from(new Set(labels)).sort();
  }, [board]);
  const [vaccine, setVaccine] = useState<string>("");
  // EMPTY means "no filter, show everything" — it does NOT mean "hide everything". The chips used
  // to initialise to the full set and a click DELETED that status, so pressing "Overdue" hid the
  // overdue cells and left the other three: a control that reads "show me this" did the exact
  // opposite. Selecting into an empty set makes the chip mean what its label says, and keeps the
  // unfiltered board reachable by deselecting rather than by re-selecting all four.
  const [statuses, setStatuses] = useState<Set<StatusKey>>(new Set());
  const statusVisible = (key: string) => statuses.size === 0 || statuses.has(key as StatusKey);
  // Cell drilldown is client-local overlay state: the cohort row already carries its sub-cohorts,
  // so opening a cell must not re-run the route (local-overlay rule).
  const [selectedCell, setSelectedCell] = useState<SelectedCohortCell | null>(null);
  // Client-local overlay state: the animals are already in the rendered payload, so opening the
  // drawer must not re-run the route.
  const [closedDrawerOpen, setClosedDrawerOpen] = useState(false);
  // The behind cell's animals travel IN the board payload, so opening a red cell is a local
  // overlay, not a second fetch (local-overlay rule).
  const [selectedShedVaccine, setSelectedShedVaccine] = useState<ShedVaccineCell | null>(null);
  const closedAnimals = board.closedWithoutDoseAnimals ?? [];
  const futureCampaigns = useMemo(
    () => statusVisible("scheduled") ? scheduledDriveCampaigns(futureDrives) : [],
    [futureDrives, statuses],
  );
  const driveCampaigns = useMemo(
    () => sortDriveCampaignsNewestFirst([...executedCampaigns, ...futureCampaigns]),
    [executedCampaigns, futureCampaigns],
  );

  const view = useMemo(() => {
    const matchesVaccine = (label?: string) => !vaccine || (label ?? "").startsWith(vaccine);
    return {
    ...board,
    shedVaccineMatrix: board.shedVaccineMatrix ?? [],
    shedVaccineColumns: board.shedVaccineColumns ?? [],
    shedDoseMatrix: (board.shedDoseMatrix ?? []).filter(
      (c) => matchesVaccine(c.doseRule) && statusVisible(c.state),
    ),
    cohortMatrix: (board.cohortMatrix ?? []).filter((c) => matchesVaccine(c.vaccineLabel)),
    verificationQueue: (board.verificationQueue ?? []).filter((r) => matchesVaccine(r.doseRule)),
    };
  }, [board, vaccine, statuses]);

  const toggleStatus = (key: StatusKey) => setStatuses((prev) => {
    const next = new Set(prev);
    if (next.has(key)) next.delete(key); else next.add(key);
    return next;
  });

  const openCohortDrawer = (cell: SelectedCohortCell) => {
    setClosedDrawerOpen(false);
    setSelectedCell(cell);
  };

  const openClosedDrawer = () => {
    setSelectedCell(null);
    setClosedDrawerOpen(true);
  };

  // Escape closes whichever drawer is open, from ANYWHERE on the page. An onKeyDown handler on the
  // drawer element only fires once focus is already inside it, so pressing Escape after opening a
  // drawer by mouse did nothing and the scrim kept swallowing the next click.
  useEffect(() => {
    if (!selectedCell && !closedDrawerOpen && !selectedShedVaccine) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      setSelectedCell(null);
      setClosedDrawerOpen(false);
      setSelectedShedVaccine(null);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [selectedCell, closedDrawerOpen, selectedShedVaccine]);

  const filterBar = (
    <div className="cbm-filters">
      <div className="cbm-filter-row">
        <label className="cbm-filter-label" htmlFor="cbm-vaccine">
          {copy(pageContract, "command_board.filter.vaccine")}
        </label>
        <select id="cbm-vaccine" className="cbm-select" value={vaccine} onChange={(e) => setVaccine(e.target.value)}>
          <option value="">{copy(pageContract, "command_board.filter.all_vaccines")}</option>
          {vaccineOptions.map((v) => (
            <option key={v} value={v}>{v}</option>
          ))}
        </select>
        <label className="cbm-filter-label" htmlFor="cbm-drive">
          {copy(pageContract, "command_board.filter.operator_day")}
        </label>
        <select
          id="cbm-drive"
          className="cbm-select cbm-select-wide"
          value={selectedDrive}
          onChange={(e) => selectDrive(e.target.value)}
          disabled={driveOptions.length === 0}
          aria-disabled={driveOptions.length === 0}
          aria-busy={isPending}
          title={
            driveOptions.length === 0
              ? copy(pageContract, "command_board.filter.no_drives")
              : isPending
                ? copy(pageContract, "state.loading")
                : undefined
          }
        >
          <option value="">{copy(pageContract, "command_board.filter.all_common_drives")}</option>
          {driveCampaigns.map((campaign) => (
            <optgroup
              key={campaign.key}
              label={`${campaign.name} · ${formatScheduledDriveDates(campaign.dateKeys)} · ${campaign.targetCount} animals`}
            >
              {campaign.treatments.map((drive, index) => (
                  <option
                    key={`${driveSelectionValue(drive.batchIds[0] ?? drive.key, drive.parkId)}|${drive.dateKeys.join(",")}`}
                    value={driveSelectionValue(drive.batchIds[0] ?? drive.key, drive.parkId)}
                  >
                    {`Operator day ${index + 1} · ${formatScheduledDriveDates(drive.dateKeys)} · ${drive.targetCount} animals`}
                  </option>
                ))}
            </optgroup>
          ))}
        </select>
        {/* The catalogue is bounded, so a drive past the bound is otherwise indistinguishable from a
            drive that was never planned. Say the picker is partial rather than let it read as the
            whole programme. */}
        {board.driveOptionsTruncated && (
          <span className="cbm-filter-note" role="status">
            {copy(pageContract, "command_board.filter.drives_truncated")}
          </span>
        )}
      </div>
      <div className="cbm-filter-row">
        {STATUS_KEYS.map((key) => (
          <button
            key={key}
            type="button"
            className={`cbm-chip cbm-chip-${key}${statuses.has(key) ? " cbm-chip-on" : ""}`}
            aria-pressed={statuses.has(key)}
            onClick={() => toggleStatus(key)}
          >
            <i />
            {copy(pageContract, `command_board.shed_matrix.state.${key}`)}
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <section className="card cbm">
      <div className="hd">
        <h2>{copy(pageContract, "section.command_board.title")}</h2>
      </div>
      <div className="bd">
        {filterBar}
        {/* KPI Row - 5 cards with colored stripes */}
        <div className="cbm-kpi-row">
          <div className={`kpi mut ${view.kpis.targets > 0 ? "mut" : "mut"}`}>
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.targets")}</div>
            <div className="val">{view.kpis.targets}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.targets_dl")}</div>
          </div>
          {/* Missed sits FIRST, immediately after the roster total and ahead of Verified, because
              it is the one tile that reports a failure rather than progress. It is also the tile
              whose absence made the board wrong: 137 animals holding a missed dose were being
              counted as Verified while Overdue read 0. */}
          <div className="kpi danger">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.missed")}</div>
            <div className="val">{view.kpis.missedNotGiven}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.missed_dl")}</div>
          </div>
          <div className="kpi ok">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.verified")}</div>
            <div className="val">{view.kpis.dosesVerified}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.verified_dl")}</div>
          </div>
          <div className="kpi warn">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.awaiting_verification")}</div>
            <div className="val">{view.kpis.awaitingVerification}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.awaiting_dl")}</div>
          </div>
          <div className="kpi danger">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.overdue")}</div>
            <div className="val">{view.kpis.overdueNotGiven}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.overdue_dl")}</div>
          </div>
          <div className="kpi info">
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.scheduled_ahead")}</div>
            <div className="val">{view.kpis.scheduledAhead}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.scheduled_dl")}</div>
          </div>
          {/* The five buckets are a disjoint, EXHAUSTIVE partition of targets. Rendering only four
              left the tiles summing to less than the total, so a reader could not tell a projection
              bug from animals whose obligations genuinely closed with no dose. */}
          {/* The tile is the START of the CEO's question, not the end: "3 closed with no dose" is
              followed every time by "which animals, and why". It opens the record drawer with that
              list rather than dead-ending on a number. Disabled-with-reason at zero, so the
              affordance never promises a list that does not exist. */}
          <div
            className={`kpi mut${closedAnimals.length > 0 ? " kpi-clickable" : ""}`}
            role={closedAnimals.length > 0 ? "button" : undefined}
            tabIndex={closedAnimals.length > 0 ? 0 : undefined}
            aria-disabled={closedAnimals.length === 0 ? true : undefined}
            title={
              closedAnimals.length > 0
                ? copy(pageContract, "command_board.kpi.closed_without_dose_open")
                : copy(pageContract, "command_board.kpi.closed_without_dose_empty")
            }
            onClick={() => closedAnimals.length > 0 && openClosedDrawer()}
            onKeyDown={(e) => {
              if ((e.key === "Enter" || e.key === " ") && closedAnimals.length > 0) {
                e.preventDefault();
                setClosedDrawerOpen(true);
              }
            }}
          >
            <div className="stripe"></div>
            <div className="lbl">{copy(pageContract, "command_board.kpi.closed_without_dose")}</div>
            <div className="val">{view.kpis.closedWithoutDose}</div>
            <div className="dl">{copy(pageContract, "command_board.kpi.closed_without_dose_dl")}</div>
          </div>
        </div>

        {/* Shed × Vaccine, dose collapsed, red/green only.
            This sits ABOVE the dose-qualified matrix on purpose. The dose matrix answers "how much
            of each dose", which is the follow-up; this one answers "is anything behind at all",
            which is the question actually asked walking into a shed. It is deliberately not
            filtered by the status chips: the chips select cell STATES of the dose matrix, and a
            red/green shed roll-up filtered to "scheduled" would be a contradiction. It carries no
            counts and no future dates by design — a count invites reconciling it against the dose
            matrix, and the two use different grains. */}
        {view.shedVaccineMatrix.length > 0 && view.shedVaccineColumns.length > 0 && (() => {
          // Keyed by the operational location, not just parent shed. Partitioned sheds emit one
          // backend row per physical pen, so shedId alone would overwrite sibling partitions.
          const cellsByShed = new Map<string, Map<string, typeof view.shedVaccineMatrix[number]>>();
          const shedOrder: string[] = [];
          const shedLabel = new Map<string, { name: string; park?: string }>();
          const nameCount = new Map<string, Set<string>>();
          view.shedVaccineMatrix.forEach((cell) => {
            const opKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
            let row = cellsByShed.get(opKey);
            if (!row) {
              row = new Map();
              cellsByShed.set(opKey, row);
              shedOrder.push(opKey);
              shedLabel.set(opKey, { name: cell.operational_location_display || cell.shedName, park: cell.parkName ?? undefined });
            }
            row.set(cell.vaccineCode, cell);
            const ids = nameCount.get(cell.operational_location_display || cell.shedName) ?? new Set<string>();
            ids.add(opKey);
            nameCount.set(cell.operational_location_display || cell.shedName, ids);
          });
          // Counts sheds needing ANY attention, not just red ones. Counting only "behind" made the
          // summary read "every shed is up to date on every vaccine" while three sheds sat amber
          // with 137 doses waiting on a verifier -- the line directly contradicted the grid above it.
          const flaggedSheds = shedOrder.filter((shedId) =>
            Array.from(cellsByShed.get(shedId)?.values() ?? []).some(
              (c) => c.state === "behind" || c.state === "verifying",
            ),
          ).length;
          return (
            <div className="cbm-shed-section cbm-sv">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.shed_vaccine.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.shed_vaccine.meta")}</span>
              </div>
              <div className="cbm-hm">
                <table className="cbm-heat cbm-sv-heat">
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "command_board.shed_vaccine.column.shed")}</th>
                      {/* Header text is SERVER copy: the label travels with the column so the
                          client holds no vaccine-name table of its own. Falling back to the code
                          keeps an unlabelled catalogue vaccine visible instead of blank. */}
                      {view.shedVaccineColumns.map((column) => (
                        <th key={column.code}>{column.label || column.code}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {shedOrder.map((shedPartitionKey) => {
                      const label = shedLabel.get(shedPartitionKey);
                      // Park is shown ONLY when the shed name is ambiguous in this payload, so the
                      // row stays as short as the ask demanded until ambiguity forces otherwise.
                      const ambiguous = (nameCount.get(label?.name ?? "")?.size ?? 0) > 1;
                      return (
                        <tr key={shedPartitionKey}>
                          <td className="cbm-sv-shed">
                            {label?.name}
                            {ambiguous && label?.park ? <span className="cbm-sv-shed-park">{label.park}</span> : null}
                          </td>
                          {view.shedVaccineColumns.map((column) => {
                            const code = column.code;
                            const cell = cellsByShed.get(shedPartitionKey)?.get(code);
                            const state = cell?.state ?? "not_planned";
                            const behind = cell?.behindAnimals ?? 0;
                            const openable = (state === "behind" || state === "verifying") && cell !== undefined;
                            return (
                              <td
                                key={code}
                                className={`cbm-sv-cell cbm-sv-${state}`}
                                role={openable ? "button" : undefined}
                                tabIndex={openable ? 0 : undefined}
                                onClick={() => openable && setSelectedShedVaccine(cell)}
                                onKeyDown={(e) => {
                                  if (openable && (e.key === "Enter" || e.key === " ")) {
                                    e.preventDefault();
                                    setSelectedShedVaccine(cell);
                                  }
                                }}
                                title={
                                  state === "behind"
                                    ? `${behind} of ${cell?.totalAnimals ?? 0} behind`
                                    : state === "verifying"
                                      ? `${cell?.verifyingAnimals ?? 0} of ${cell?.totalAnimals ?? 0} given, video verification pending`
                                    : state === "ok"
                                      ? `${cell?.totalAnimals ?? 0} on track`
                                      : copy(pageContract, "command_board.shed_vaccine.state.not_planned")
                                }
                              >
                                {/* Only the RED cell carries a mark, and it is the NUMBER. A grid
                                    of bright dots on every clean cell competed with the red for
                                    attention and made the one thing worth finding HARDER to find,
                                    while telling the reader nothing they could act on. Clean cells
                                    are now a quiet tick and unplanned ones a dash, so the eye lands
                                    on red first and the row still says "checked, fine" rather than
                                    "no data". */}
                                {state === "behind" ? (
                                  <span className="cbm-sv-count">
                                    {behind}
                                    <i>{copy(pageContract, "command_board.shed_vaccine.cell.behind_unit")}</i>
                                  </span>
                                ) : state === "verifying" ? (
                                  <span className="cbm-sv-verifying">
                                    {cell?.verifyingAnimals ?? 0}
                                    <i>{copy(pageContract, "command_board.shed_vaccine.cell.verifying_unit")}</i>
                                  </span>
                                ) : state === "ok" ? (
                                  <span className="cbm-sv-tick" aria-hidden="true">✓</span>
                                ) : (
                                  <span className="cbm-sv-none" aria-hidden="true">–</span>
                                )}
                                <span className="sr-only">
                                  {copy(pageContract, `command_board.shed_vaccine.state.${state}`)}
                                </span>
                              </td>
                            );
                          })}
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <div className="cbm-legend cbm-sv-legend">
                <span className="cbm-sv-behind"><i></i>{copy(pageContract, "command_board.shed_vaccine.state.behind")}</span>
                <span className="cbm-sv-verifying"><i></i>{copy(pageContract, "command_board.shed_vaccine.state.verifying")}</span>
                <span className="cbm-sv-ok"><i></i>{copy(pageContract, "command_board.shed_vaccine.state.ok")}</span>
                <span className="cbm-sv-not_planned"><i></i>{copy(pageContract, "command_board.shed_vaccine.state.not_planned")}</span>
                <span className="cbm-meta">
                  {flaggedSheds > 0
                    ? `${flaggedSheds} / ${shedOrder.length} ${copy(pageContract, "command_board.shed_vaccine.summary_behind")}`
                    : copy(pageContract, "command_board.shed_vaccine.summary_clean")}
                </span>
              </div>
            </div>
          );
        })()}

        {/* Vaccine × Shed status - colored grid heatmap */}
        {view.shedDoseMatrix.length > 0 && (() => {
          const grid = buildShedGrid(view.shedDoseMatrix);
          // Queue age keyed by the same (shed, dose) grain the matrix cells use, so the number
          // lands on the cell it describes rather than being matched by position.
          const queueAgeDays = new Map<string, number>();
          (view.verificationQueue ?? []).forEach((q) => {
            if (q.daysInQueue !== undefined && q.daysInQueue !== null) {
              // Key by shedId + partition to match the grid's row keys.
              const shedKey = `${q.shedId}|${q.partition_label ?? ""}`;
              queueAgeDays.set(`${shedKey}|${q.doseRule}`, q.daysInQueue);
            }
          });
          return (
            <div className="cbm-shed-section">
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.shed_matrix.title")}</h3>
                <span className="cbm-meta">{copy(pageContract, "command_board.shed_matrix.meta")}</span>
              </div>
              <div className="cbm-hm">
                <table className="cbm-heat">
                  <thead>
                    <tr>
                      <th className="cbm-rowh">{copy(pageContract, "command_board.shed_matrix.column.shed")}</th>
                      {grid.byDose.map((dose) => (
                        <th key={dose}>{dose}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {grid.byShed.map((row) => {
                      // Use shedId + partition for unique keying; render via operational_location_display or helper.
                      const shedKey = `${row.shedId}|${row.partitionLabel ?? ""}`;
                      const shedLabel = row.operational_location_display || operationalLocationLabel({
                        shedName: row.shedName,
                        partitionLabel: row.partitionLabel,
                      });
                      return (
                        <tr key={shedKey}>
                          <th className="cbm-rowh">{shedLabel}</th>
                          {grid.byDose.map((dose) => {
                            const cell = row.cells[dose];
                            if (!cell) {
                              return <td key={dose} className="cbm-cell cbm-na">—</td>;
                            }
                            // Completed cells show the operator's actual administration date. Verification
                            // can happen days later and must never replace the medical date. Scheduled and
                            // overdue cells continue to show their rule-derived due date.
                            const dateStr = cell.state === "verified" || cell.state === "awaiting"
                              ? formatDateSpan(cell.minAdministeredDate, cell.maxAdministeredDate)
                              : formatDateSpan(cell.minDueDate, cell.maxDueDate);
                            // An awaiting cell also carries how long it has been sitting with the
                            // verifier — the one fact the removed queue table added.
                            const waiting = cell.state === "awaiting" ? queueAgeDays.get(`${shedKey}|${dose}`) : undefined;
                            return (
                              <td
                                key={dose}
                                className={`cbm-cell cbm-${cell.state}`}
                                title={`${shedLabel} · ${cell.animalCount} animals${
                                  waiting !== undefined ? ` · ${waiting}${copy(pageContract, "command_board.shed_matrix.waiting_suffix")}` : ""
                                }`}
                              >
                              {cell.animalCount}
                              <small>
                                {dateStr}
                                {waiting !== undefined ? ` · ${waiting}${copy(pageContract, "command_board.shed_matrix.waiting_suffix")}` : ""}
                              </small>
                            </td>
                            );
                          })}
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <div className="cbm-legend">
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.verified")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.awaiting")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.overdue")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.scheduled")}</span>
                <span><i></i>{copy(pageContract, "command_board.shed_matrix.legend.not_scoped")}</span>
              </div>
            </div>
          );
        })()}

        {/* Cohort matrix, FARMWISE: one table per farm, cohort ladder down the side, vaccines
            across the top, pending count in the cell (red when > 0) with the verified count
            beneath it so closure is readable without subtracting from the head count. */}
        {(() => {
          // MATCHING ladder (catch-all last) and READING order are different backend lists: the
          // Adults catch-all must stay last for bucketing, but the CEO reads Adults before the
          // not-adult cohorts.
          const ladder = optionGroup(pageContract, "command_board_cohort_ladder").map((o) => o.label);
          // Declared stage -> row membership. Adults is a normal rung here, never a fallback.
          const stageMap = new Map(
            optionGroup(pageContract, "command_board_cohort_stage_map").map((o) => [o.key.toUpperCase(), o.label]),
          );
          const readingOrder = cohortRowOrder(pageContract);
          const farms = buildCohortFarms(view.cohortMatrix, ladder, stageMap).map((farmBlock) => ({
            ...farmBlock,
            rows: [...farmBlock.rows].sort((a, b) => {
              const ai = readingOrder.indexOf(a.cohort);
              const bi = readingOrder.indexOf(b.cohort);
              return (ai < 0 ? readingOrder.length : ai) - (bi < 0 ? readingOrder.length : bi);
            }),
          }));
          return (
            <div className="cbm-cohort-section">
              {/* Header carries the title only. The matrix explains itself through the cells and
                  the drilldown; the CEO does not read a paragraph of legend first. */}
              <div className="cbm-section-head">
                <h3>{copy(pageContract, "command_board.cohort_matrix.title")}</h3>
              </div>
              {farms.length === 0 ? (
                <div className="cbm-empty">{copy(pageContract, "command_board.cohort_matrix.empty")}</div>
              ) : (
                farms.map(({ farm, vaccines, rows }) => (
                  <div key={farm} className="cbm-farm-block">
                    <h4 className="cbm-farm-name">{farm || copy(pageContract, "command_board.cohort_matrix.no_farm")}</h4>
                    <div className="cbm-hm">
                      <table className="cbm-heat cbm-cohort-heat">
                        <thead>
                          <tr>
                            <th className="cbm-rowh">{copy(pageContract, "command_board.cohort_matrix.column.stage")}</th>
                            {vaccines.map((v) => (
                              <th key={v}>{v}</th>
                            ))}
                            <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                          </tr>
                        </thead>
                        <tbody>
                          {rows.map((row) => {
                            // Three DISJOINT buckets, rendered together: the big number is what
                            // the OPERATOR still owes, and the sub-line carries what the VERIFIER
                            // owes (submitted) plus what is closed (verified). Showing pending
                            // alone made a fully vaccinated, fully submitted park read identically
                            // to an untouched one, and contradicted the "awaiting verification"
                            // KPI directly above this table.
                            const label = row.cohort;
                            const present = row.animals > 0;
                            const animals = row.animals;
                            const pendingOf = row.pending;
                            const submittedOf = row.submitted;
                            const verifiedOf = row.verified;
                            const administeredDatesOf = row.administeredDates;
                            const cells =
                              vaccines.map((v) => {
                                const pending = pendingOf[v];
                                if (!present || pending === undefined) {
                                  return <td key={v} className="cbm-cell cbm-na">—</td>;
                                }
                                const awaiting = submittedOf[v] ?? 0;
                                const done = verifiedOf[v] ?? 0;
                                // Nothing owed and nothing done: stay neutral rather than
                                // pretend work was completed. `awaiting` is part of the guard —
                                // a submitted-but-unverified cell is real work and must render.
                                if (pending === 0 && awaiting === 0 && done === 0) {
                                  return <td key={v} className="cbm-cell cbm-na">—</td>;
                                }
                                const pendingWord = copy(pageContract, "command_board.cohort_matrix.pending_word");
                                const submittedWord = copy(pageContract, "command_board.cohort_matrix.submitted_word");
                                const verifiedWord = copy(pageContract, "command_board.cohort_matrix.verified_word");
                                const administered = administeredDatesOf[v];
                                const administeredDate = formatDateSpan(administered?.min, administered?.max);
                                // The headline number is the count of the state the cell colour
                                // denotes, so colour and number can never disagree.
                                const headline = pending > 0 ? pending : awaiting > 0 ? awaiting : done;
                                // Actual medical dates belong to the VERIFIED doses only; show the
                                // honest "date unavailable" rather than borrowing the drive's
                                // planned date.
                                const dateSuffix = administeredDate
                                  ? ` · ${administeredDate}`
                                  : done > 0
                                    ? ` · ${copy(pageContract, "command_board.cohort_matrix.date_unavailable")}`
                                    : "";
                                const exception = row.exceptions[v];
                                const exceptionCount = exception?.count ?? 0;
                                const exceptionWord = copy(
                                  pageContract,
                                  exceptionCount === 1
                                    ? "command_board.cohort_matrix.exception_word_one"
                                    : "command_board.cohort_matrix.exception_word",
                                );
                                // Only the buckets that carry work are spelled out. A CEO cell that
                                // prints "0 pending · 0 submitted · 324 verified" makes the reader
                                // subtract zeroes to find the one fact that matters.
                                const parts: string[] = [];
                                if (pending > 0) parts.push(`${pending} ${pendingWord}`);
                                if (awaiting > 0) parts.push(`${awaiting} ${submittedWord}`);
                                if (done > 0) parts.push(`${done} ${verifiedWord}`);
                                const cellKey = `${farm}|${label}|${v}`;
                                const selection: SelectedCohortCell = {
                                  key: cellKey,
                                  farm,
                                  cohort: label,
                                  vaccine: v,
                                  animals,
                                  pending,
                                  submitted: awaiting,
                                  verified: done,
                                  dateSpan: administeredDate,
                                  days: row.days[v] ?? [],
                                  exceptionCount,
                                  exceptionGoats: exception?.goats ?? [],
                                  members: row.members.map((member) => ({
                                    label: member.label,
                                    animals: member.animals,
                                    pending: member.pending[v] ?? 0,
                                    submitted: member.submitted[v] ?? 0,
                                    verified: member.verified[v] ?? 0,
                                    exceptions: member.exceptions[v]?.count ?? 0,
                                    dateSpan: formatDateSpan(member.administeredDates[v]?.min, member.administeredDates[v]?.max),
                                  })),
                                };
                                // Colour follows who owes the next move; an exception rides ON TOP of
                                // that colour as its own chip, because "324 verified" and "321
                                // verified with 3 animals missing this dose" are different medical
                                // facts that must not render as the same green block.
                                return (
                                  <td
                                    key={v}
                                    className={`cbm-cell cbm-cohort-cell ${pending > 0 ? "cbm-pending" : awaiting > 0 ? "cbm-awaiting" : "cbm-clear"}${
                                      exceptionCount > 0 ? " cbm-cell-exception" : ""
                                    }${selectedCell?.key === cellKey ? " cbm-cell-on" : ""}`}
                                    title={`${label} · ${v} · ${pending} ${pendingWord}, ${awaiting} ${submittedWord}, ${done} ${verifiedWord}${dateSuffix}${
                                      exceptionCount > 0 ? ` · ${exceptionCount} ${exceptionWord}` : ""
                                    }`}
                                    role="button"
                                    tabIndex={0}
                                    aria-pressed={selectedCell?.key === cellKey}
                                    onClick={() => {
                                      if (selectedCell?.key === cellKey) {
                                        setSelectedCell(null);
                                      } else {
                                        openCohortDrawer(selection);
                                      }
                                    }}
                                    onKeyDown={(e) => {
                                      if (e.key === "Enter" || e.key === " ") {
                                        e.preventDefault();
                                        if (selectedCell?.key === cellKey) {
                                          setSelectedCell(null);
                                        } else {
                                          openCohortDrawer(selection);
                                        }
                                      }
                                    }}
                                  >
                                    <span className="cbm-cell-head">{headline}</span>
                                    <small>{parts.join(" · ")}</small>
                                    {administeredDate ? (
                                      <small className="cbm-cell-date">{administeredDate}</small>
                                    ) : done > 0 ? (
                                      <small className="cbm-cell-date">{copy(pageContract, "command_board.cohort_matrix.date_unavailable")}</small>
                                    ) : null}
                                    {exceptionCount > 0 ? (
                                      <span className="cbm-cell-exception-chip">
                                        {exceptionCount} {exceptionWord}
                                      </span>
                                    ) : null}
                                  </td>
                                );
                              });

                            return (
                              <tr key={`${farm}-${row.cohort}`}>
                                <th className="cbm-rowh">
                                  {row.cohort}
                                  {rowQualifier(pageContract, row.cohort) ? (
                                    <span className="cbm-rowh-note">{rowQualifier(pageContract, row.cohort)}</span>
                                  ) : null}
                                </th>
                                {cells}
                                <td className="cbm-cell cbm-na">{row.animals > 0 ? row.animals : "—"}</td>
                              </tr>
                            );
                          })}
                        </tbody>
                      </table>
                    </div>
                  </div>
                ))
              )}
            </div>
          );
        })()}
        {futureCampaigns.length > 0 && (
          <div className="cbm-future-section">
            <div className="cbm-section-head">
              <h3>{copy(pageContract, "command_board.future_drives.title")}</h3>
              <span className="cbm-meta">
                {futureCampaigns.length} {copy(pageContract, "command_board.future_drives.count_suffix")} · {futureDrives.length} {copy(pageContract, "command_board.future_drives.lines_suffix")}
              </span>
            </div>
            <div className="cbm-future-table-wrap">
              <table className="cbm-future-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "command_board.future_drives.column.campaign")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.drive")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.dates")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.sheds")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.animals")}</th>
                    <th>{copy(pageContract, "command_board.future_drives.column.doses")}</th>
                  </tr>
                </thead>
                <tbody>
                  {futureCampaigns.flatMap((campaign) => campaign.treatments.map((drive, index) => (
                    <tr key={drive.key} className={driveBatchId && drive.batchIds.includes(driveBatchId) ? "is-selected" : undefined}>
                      {index === 0 && (
                        <td rowSpan={campaign.treatments.length} className="cbm-campaign-cell">
                          <strong>{campaign.name}</strong>
                          <small>
                            {campaign.targetCount} {copy(pageContract, "command_board.future_drives.campaign_animals")} · {campaign.doseCount} {copy(pageContract, "command_board.future_drives.campaign_doses")}
                          </small>
                        </td>
                      )}
                      <td><strong>{drive.driveName}</strong></td>
                      <td>{formatScheduledDriveDates(drive.dateKeys)}</td>
                      <td>{drive.shedNames.join(", ") || "—"}</td>
                      <td><strong>{drive.targetCount}</strong></td>
                      <td><strong>{drive.doseCount}</strong></td>
                    </tr>
                  )))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {/* The verification queue used to render here as its own table, but every count in it
            (shed, dose, awaiting) is already an amber cell in the shed matrix above. Only the
            queue age was unique, so it now rides along in that cell and the duplicate table is
            gone. */}
      </div>

      {/* Closed, No Dose record drawer. Opens from the KPI tile with the animals already in the
          payload -- no route re-run, no second fetch. Closes on X, scrim, and Escape. */}
      {/* The drawer is a CHILD of the scrim, not its sibling: `.drawer` is parked off-canvas by
          `transform: translateX(100%)` and the only rule that pulls it on screen is the DESCENDANT
          selector `.dscrim.on .drawer`. As a sibling it mounts, fills with data, and stays
          invisible -- the click looks dead. */}
      {/* Shed x Vaccine behind drawer. Same structure and the same reason as the Closed, No Dose
          drawer below: a red cell states the alarm, this names the animals behind it. Also a CHILD
          of the scrim -- `.drawer` is parked off-canvas and only `.dscrim.on .drawer` pulls it in,
          so mounting it as a sibling renders a dead click. */}
      {selectedShedVaccine && (
        <div className="dscrim on" onClick={() => setSelectedShedVaccine(null)}>
          <aside
            className="drawer on"
            role="dialog"
            aria-modal="true"
            aria-label={copy(pageContract, "command_board.shed_vaccine.title")}
            onClick={(e) => e.stopPropagation()}
          >
            <div className="dh">
              <div style={{ flex: 1 }}>
                <h3>
                  {(selectedShedVaccine.operational_location_display || operationalLocationLabel({
                    shedName: selectedShedVaccine.shedName,
                    partitionLabel: selectedShedVaccine.partition_label,
                  }))} ·{" "}
                  {view.shedVaccineColumns.find((c) => c.code === selectedShedVaccine.vaccineCode)?.label
                    || selectedShedVaccine.vaccineCode}
                </h3>
                <span>
                  {selectedShedVaccine.state === "verifying"
                    ? `${selectedShedVaccine.verifyingAnimals} ${copy(pageContract, "command_board.shed_vaccine.drawer.verifying_of")} ${selectedShedVaccine.totalAnimals}`
                    : `${selectedShedVaccine.behindAnimals} ${copy(pageContract, "command_board.shed_vaccine.drawer.behind_of")} ${selectedShedVaccine.totalAnimals}`}
                  {selectedShedVaccine.parkName ? ` · ${selectedShedVaccine.parkName}` : ""}
                </span>
              </div>
              <button
                className="cal-nav"
                onClick={() => setSelectedShedVaccine(null)}
                title={copy(pageContract, "command_board.cohort_matrix.detail.close")}
              >
                <X className="ic" aria-hidden="true" />
              </button>
            </div>
            {/* The videos are SHED-and-day proof covering every animal below, so they belong once in
                the header. Repeating a link on all 76 rows implied per-goat footage that does not
                exist. */}
            <div className="cbm-verify-videos">
              {(selectedShedVaccine.proofVideos ?? []).length > 0 ? (
                <>
                  <span>{copy(pageContract, "command_board.shed_vaccine.drawer.shed_videos")}</span>
                  {(selectedShedVaccine.proofVideos ?? []).map((video, index) => (
                    <a key={video.path} href={video.path} target="_blank" rel="noreferrer">
                      {copy(pageContract, "command_board.shed_vaccine.drawer.clip")} {index + 1}
                    </a>
                  ))}
                </>
              ) : (
                <span className="cbm-verify-novideo">
                  {copy(pageContract, "command_board.shed_vaccine.drawer.no_video")}
                </span>
              )}
            </div>
            <div className="db">
              {/* One row per animal as a two-line card, not five columns. Five columns overflowed
                  the drawer and pushed the animal identity off the left edge behind a horizontal
                  scrollbar, leaving rows whose visible text was identical and gave the reader no way
                  to tell which goat each belonged to. */}
              <table className="cbm-verify-table">
                <tbody>
                  {(selectedShedVaccine.flaggedAnimals ?? []).map((animal) => (
                    <tr key={animal.goatId}>
                      <td>
                        {/* EAR TAGS lead, both of them. Most of the herd carries two and an operator
                            may be reading either ear, so printing one tag makes the row unmatchable
                            at the animal. The internal id is not an identity on the farm and appears
                            only for an animal that has no active tag at all. */}
                        <div className="cbm-verify-who">
                          {animal.tag || animal.tag2 ? (
                            <>
                              {animal.tag ? <b>{animal.tag}</b> : null}
                              {animal.tag2 ? <b>{animal.tag2}</b> : null}
                            </>
                          ) : (
                            <b>{animal.displayId}</b>
                          )}
                        </div>
                        {/* One muted line. The state is identical on every row in a verifying cell,
                            so shouting it 76 times in amber added noise and no information -- the
                            drawer header already says what the whole list is waiting on. */}
                        {/* Location leads the meta line: it is the only thing that varies row to
                            row and the only thing that tells a person which pen to walk into. The
                            state is identical on every row of a verifying cell and the header
                            already says it, so it is not repeated here. */}
                        {/* The PEN and the date, nothing else. Park and shed are constant for every
                            row in this cell and already sit in the drawer header, so rendering the
                            full location display on each line repeated the partition twice over and
                            the shed once per animal. The pen is the only part that varies row to row
                            and the only part that sends a person to a physical place. */}
                        <div className="cbm-verify-meta">
                          {animal.partitionLabel ? (
                            <span className="cbm-verify-pen">{animal.partitionLabel}</span>
                          ) : null}
                          <span>
                            {copy(pageContract, "command_board.shed_vaccine.drawer.column.due")}{" "}
                            {animal.dueAt ? new Date(animal.dueAt).toLocaleDateString("en-GB") : "—"}
                          </span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {/* The COUNT is whole-scope truth and the list is capped, so a shorter list must say
                  so rather than read as the complete set. */}
              {(selectedShedVaccine.flaggedAnimals ?? []).length < selectedShedVaccine.behindAnimals && (
                <p className="cbm-meta">
                  {copy(pageContract, "command_board.shed_vaccine.drawer.truncated")}
                </p>
              )}
            </div>
          </aside>
        </div>
      )}

      {closedDrawerOpen && (
        <div className="dscrim on" onClick={() => setClosedDrawerOpen(false)}>
          <aside
            className="drawer on"
            role="dialog"
            aria-modal="true"
            aria-label={copy(pageContract, "command_board.kpi.closed_without_dose")}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (e.key === "Escape") setClosedDrawerOpen(false);
            }}
          >
            <div className="dh">
              <div style={{ flex: 1 }}>
                <h3>{copy(pageContract, "command_board.kpi.closed_without_dose")}</h3>
                <span>
                  {view.kpis.closedWithoutDose} {copy(pageContract, "command_board.closed_drawer.animals_word")}
                </span>
              </div>
              <button className="cal-nav" onClick={() => setClosedDrawerOpen(false)} title={copy(pageContract, "command_board.cohort_matrix.detail.close")}>
                <X className="ic" aria-hidden="true" />
              </button>
            </div>
            <div className="db">
              <table className="cbm-closed-table">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "command_board.closed_drawer.column.animal")}</th>
                    <th>{copy(pageContract, "command_board.closed_drawer.column.location")}</th>
                    <th>{copy(pageContract, "command_board.closed_drawer.column.vaccine")}</th>
                    <th>{copy(pageContract, "command_board.closed_drawer.column.reason")}</th>
                  </tr>
                </thead>
                <tbody>
                  {closedAnimals.map((animal) => (
                    <tr key={animal.goatId}>
                      {/* The tag on the animal's ear is what identifies it on the farm, so the
                          tags lead and the internal id sits under them. An animal may carry two;
                          both are shown so either ear matches. */}
                      <td>
                        {animal.tag1 || animal.tag2 ? (
                          <>
                            {animal.tag1 ? <b>{animal.tag1}</b> : null}
                            {animal.tag2 ? <b className="cbm-closed-tag2">{animal.tag2}</b> : null}
                            <span className="cbm-closed-tag">{animal.displayId}</span>
                          </>
                        ) : (
                          <b>{animal.displayId}</b>
                        )}
                      </td>
                      {/* Ground location, partition included -- the parent shed name alone would
                          send a park head to the wrong side of a partitioned shed. */}
                      <td>
                        {animal.operational_location_display}
                        <span className="cbm-closed-park">{animal.parkName}</span>
                      </td>
                      <td>{animal.vaccineLabel}</td>
                      <td>{animal.reason}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {(view.kpis.closedWithoutDose ?? 0) > closedAnimals.length ? (
                <div className="cbm-cohort-detail-muted" style={{ marginTop: 10 }}>
                  {copy(pageContract, "command_board.closed_drawer.capped")} {view.kpis.closedWithoutDose}
                </div>
              ) : null}
            </div>
          </aside>
        </div>
      )}

      {/* Cohort matrix cell detail drawer. Opens from clicking a cohort matrix cell with the data
          already in the rendered row. Closes on X, scrim, and Escape. Only one drawer open at a time. */}
      {selectedCell && (
        <div className="dscrim on" onClick={() => setSelectedCell(null)}>
          <aside
            className="drawer on"
            role="dialog"
            aria-modal="true"
            aria-label={`${selectedCell.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")} · ${selectedCell.cohort} × ${selectedCell.vaccine}`}
            onClick={(e) => e.stopPropagation()}
            onKeyDown={(e) => {
              if (e.key === "Escape") setSelectedCell(null);
            }}
          >
            <div className="dh">
              <div style={{ flex: 1 }}>
                <h3>
                  {selectedCell.farm || copy(pageContract, "command_board.cohort_matrix.no_farm")} · {selectedCell.cohort} × {selectedCell.vaccine}
                </h3>
              </div>
              <button className="cal-nav" onClick={() => setSelectedCell(null)} title={copy(pageContract, "command_board.cohort_matrix.detail.close")}>
                <X className="ic" aria-hidden="true" />
              </button>
            </div>
            <div className="db" style={{ display: "flex", flexDirection: "column", gap: "14px" }}>
              <div className="metagrid">
                <div>
                  <div className="k">{copy(pageContract, "command_board.cohort_matrix.detail.animals")}</div>
                  <div className="v">{selectedCell.animals}</div>
                </div>
                <div>
                  <div className="k">{copy(pageContract, "command_board.cohort_matrix.pending_word")}</div>
                  <div className="v">{selectedCell.pending}</div>
                </div>
                <div>
                  <div className="k">{copy(pageContract, "command_board.cohort_matrix.submitted_word")}</div>
                  <div className="v">{selectedCell.submitted}</div>
                </div>
                <div>
                  <div className="k">{copy(pageContract, "command_board.cohort_matrix.verified_word")}</div>
                  <div className="v">{selectedCell.verified}</div>
                </div>
                <div>
                  <div className="k">{copy(pageContract, "command_board.cohort_matrix.detail.dates")}</div>
                  <div className="v">
                    {selectedCell.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")}
                  </div>
                </div>
              </div>

              {/* The day story: which day the operator actually dosed how many animals. */}
              <div className="cbm-drawer-section">
                <b>{copy(pageContract, "command_board.cohort_matrix.detail.per_day")}</b>
                {selectedCell.days.length > 0 ? (
                  <ul className="cbm-daylist">
                    {selectedCell.days.map((day) => (
                      <li key={day.date}>
                        <span className="d">{formatDateSpan(day.date, day.date)}</span>
                        <span className="n">{day.animalCount}</span>
                        <span className="u">{copy(pageContract, "command_board.cohort_matrix.detail.animals_word")}</span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <span className="cbm-cohort-detail-muted">
                    {copy(pageContract, "command_board.cohort_matrix.date_unavailable")}
                  </span>
                )}
              </div>

              {/* The exception story: animals whose later dose is accepted while THIS dose is
                  not. Named, so the CEO can hand the list to a park head. */}
              <div className="cbm-drawer-section">
                <b>{copy(pageContract, "command_board.cohort_matrix.detail.exceptions")}</b>
                {selectedCell.exceptionCount > 0 ? (
                  <>
                    <span className="cbm-cohort-detail-exception-count">{selectedCell.exceptionCount}</span>
                    <ul className="cbm-goatlist">
                      {selectedCell.exceptionGoats.map((goat) => (
                        <li key={goat.goatId}>
                          {goat.displayId}
                          {goat.tag ? <span className="t">{goat.tag}</span> : null}
                        </li>
                      ))}
                    </ul>
                    {selectedCell.exceptionCount > selectedCell.exceptionGoats.length ? (
                      <span className="cbm-cohort-detail-muted">
                        {copy(pageContract, "command_board.cohort_matrix.detail.capped")} {selectedCell.exceptionCount}
                      </span>
                    ) : null}
                  </>
                ) : (
                  <span className="cbm-cohort-detail-muted">
                    {copy(pageContract, "command_board.cohort_matrix.detail.clean")}
                  </span>
                )}
              </div>

              {/* Sub-cohorts breakdown table */}
              {selectedCell.members.length > 0 ? (
                <table className="cbm-cohort-detail-table">
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "command_board.cohort_matrix.detail.breakdown")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.column.animals")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.pending_word")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.submitted_word")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.verified_word")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.exception_word")}</th>
                      <th>{copy(pageContract, "command_board.cohort_matrix.detail.dates")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {selectedCell.members.map((member) => (
                      <tr key={member.label}>
                        <td>{member.label}</td>
                        <td>{member.animals}</td>
                        <td>{member.pending}</td>
                        <td>{member.submitted}</td>
                        <td>{member.verified}</td>
                        <td>{member.exceptions}</td>
                        <td>{member.dateSpan || copy(pageContract, "command_board.cohort_matrix.date_unavailable")}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : null}
            </div>
          </aside>
        </div>
      )}
    </section>
  );

}
