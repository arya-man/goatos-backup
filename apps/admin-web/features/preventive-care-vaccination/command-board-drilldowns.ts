"use client";

import { useEffect, useMemo, useRef, useState } from "react";

// Command-board drilldowns, fetched when a drawer opens.
//
// These lists used to travel INSIDE the /vaccination/command payload, computed tenant-wide for
// every cell on every render. On staging-scale data that was ~62% of an endpoint which took ~8.6s
// of SQL and returned 500 when one of its statements exhausted the 15s pool timeout -- the board
// showed "Unable to load command board". The counts stayed on the board; only the evidence lists
// moved behind per-cell, keyset-paginated endpoints.
//
// This is the repo's local-overlay rule, not a departure from it: the drawer opens IMMEDIATELY from
// the summary data the board already carries (the count, the cell identity, the state), and fetches
// only the detail that is missing. Opening a drawer never re-runs the route.

export type CohortDayRow = { date: string; animalCount: number };
export type CohortAnimalRow = { goatId: string; displayId: string; tag?: string | null };

// Field names mirror the wire contract exactly (VaccinationCommandBoardClosedWithoutDoseAnimal).
export type ClosedWithoutDoseRow = {
  goatId: string;
  displayId: string;
  tag1?: string | null;
  tag2?: string | null;
  locationDisplay?: string | null;
  parkName?: string | null;
  shedName?: string | null;
  partitionLabel?: string | null;
  reason?: string | null;
  vaccineLabel?: string | null;
};

export type ShedVaccineAnimalRow = {
  goatId: string;
  displayId: string;
  tag?: string | null;
  tag2?: string | null;
  partitionLabel?: string | null;
  dueAt?: string | null;
};

// CohortCellRef addresses ONE backend cohort cell. A drawer row is a display bucket that can roll
// several backend cells together (several stage/sex combinations, and several parks), so the drawer
// fetches one page per contributing cell and merges them exactly as the board's own row builder
// merges the summary numbers. Fetching "the drawer" as a single call would need a cell key the
// backend does not have.
export type CohortCellRef = {
  cohortParkId: string;
  managementStage: string;
  sex: string;
  doseCodes: string[];
};

// DrilldownScope is the filter the board itself was rendered under. It is carried into every
// drilldown so the drawer explains the number the reader actually clicked: as_of in particular,
// because the overdue/behind predicates are IST business-DATE comparisons against it.
export type DrilldownScope = {
  driveBatchId?: string;
  parkId?: string;
  asOf?: string;
};

export type DrilldownState<T> = {
  data: T;
  loading: boolean;
  error: string | null;
};

function scopeParams(scope: DrilldownScope): URLSearchParams {
  const params = new URLSearchParams();
  if (scope.driveBatchId) params.set("drive_batch_id", scope.driveBatchId);
  if (scope.parkId) params.set("park_id", scope.parkId);
  if (scope.asOf) params.set("as_of", scope.asOf);
  return params;
}

async function fetchJson<T>(url: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(url, { signal, cache: "no-store" });
  if (!response.ok) {
    throw new Error(String(response.status));
  }
  return (await response.json()) as T;
}

// fetchAllPages walks a keyset cursor to exhaustion, bounded.
//
// The bound is not decoration. A drawer is a reading surface, and an unbounded walk would put the
// tenant-wide fetch this split removed back on the client -- one page at a time. maxPages caps the
// work; the caller shows the count from the board, which stays whole-scope truth, so a truncated
// list is still honest about how many animals exist.
async function fetchAllPages<TItem, TPage extends { nextCursor?: string }>(
  baseUrl: string,
  params: URLSearchParams,
  select: (page: TPage) => TItem[],
  signal: AbortSignal,
  maxPages = 4,
): Promise<{ items: TItem[]; truncated: boolean }> {
  const items: TItem[] = [];
  let cursor: string | undefined;
  let truncated = false;
  for (let page = 0; page < maxPages; page += 1) {
    const query = new URLSearchParams(params);
    if (cursor) query.set("cursor", cursor);
    const result = await fetchJson<TPage>(`${baseUrl}?${query.toString()}`, signal);
    items.push(...select(result));
    if (!result.nextCursor) break;
    cursor = result.nextCursor;
    // A cursor still standing after the last allowed page means rows were left behind. Reported,
    // never inferred by the caller from a count comparison: the lists are de-duplicated while the
    // board's counts are not, so length-vs-count labels a complete list as truncated.
    if (page === maxPages - 1) truncated = true;
  }
  return { items, truncated };
}

// useDrilldown runs `load` whenever `key` changes to a non-null value, and cancels in flight when
// the drawer closes or the reader opens a different cell.
//
// Keyed on a STRING rather than on the object so a re-render with an equal-but-new object does not
// refetch -- a drawer that refetched on every parent render would turn one read into many.
function useDrilldown<T>(
  key: string | null,
  load: (signal: AbortSignal) => Promise<T>,
  empty: T,
): DrilldownState<T> {
  // State is KEYED. Deriving "is this result for the cell I am looking at" from a stored key,
  // rather than resetting state when the key changes, keeps every setState inside an async callback
  // — a synchronous setState in an effect cascades an extra render pass on every open, and React's
  // own lint rule rejects it.
  const [state, setState] = useState<{ key: string | null; data: T; error: string | null }>({
    key: null,
    data: empty,
    error: null,
  });
  const loadRef = useRef(load);

  // The loader is read through a ref so an inline closure at the call site does not retrigger the
  // fetch on every parent render. Assigned in an effect, not during render: a ref written while
  // rendering is not safe under concurrent rendering.
  useEffect(() => {
    loadRef.current = load;
  });

  useEffect(() => {
    if (!key) return;
    const controller = new AbortController();
    loadRef
      .current(controller.signal)
      .then((data) => {
        if (controller.signal.aborted) return;
        setState({ key, data, error: null });
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setState({ key, data: empty, error: err instanceof Error ? err.message : "error" });
      });
    return () => controller.abort();
    // `empty` is a stable module-level constant at every call site.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  const settled = key !== null && state.key === key;
  return {
    data: settled ? state.data : empty,
    loading: key !== null && !settled,
    error: settled ? state.error : null,
  };
}

// CohortMatrixCell mirrors the wire contract's cell. It is a SECTION, not a drilldown: the whole
// matrix arrives in one read, keyed only by the board's own filter.
export type CohortMatrixCell = Record<string, unknown>;

const NO_COHORT_MATRIX: CohortMatrixCell[] = [];

// The interned wire shape of the shed x dose grid, and its expanded form.
export type ShedDoseMatrixWire = {
  sheds: Array<{ shedId: string; shedName: string; partitionLabel?: string; locationDisplay?: string }>;
  doseRules: string[];
  cells: Array<{
    shed: number;
    dose: number;
    state: string;
    count: number;
    adminFrom?: string;
    adminTo?: string;
    dueFrom?: string;
    dueTo?: string;
  }>;
};

export type ShedDoseCellRow = {
  // doseKey is the matrix's dose INDEX, stringified. It is the only safe grid key: two dose codes
  // can share a display label (DoseQualifiedDisplayLabel leaves et_tt_kid_4w and et_tt_kid_7w both
  // as "ET+TT"), so keying a grid on doseRule silently drops one of their animal counts.
  doseKey: string;
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
};

const NO_SHED_DOSE: ShedDoseCellRow[] = [];

// expandShedDoseMatrix mirrors domain.ShedDoseMatrix.Flatten() on the backend, INCLUDING its
// safety rule: a cell whose shed or dose index does not resolve is dropped, never rendered against
// a different shed. Showing one shed's animal count under another shed's name is the kind of error
// a CEO board must not make quietly.
export function expandShedDoseMatrix(matrix?: ShedDoseMatrixWire | null): ShedDoseCellRow[] {
  if (!matrix) return NO_SHED_DOSE;
  const sheds = matrix.sheds ?? [];
  const doseRules = matrix.doseRules ?? [];
  const out: ShedDoseCellRow[] = [];
  for (const cell of matrix.cells ?? []) {
    const shed = sheds[cell.shed];
    const doseRule = doseRules[cell.dose];
    if (!shed || doseRule === undefined) continue;
    out.push({
      doseKey: String(cell.dose),
      shedId: shed.shedId,
      shedName: shed.shedName,
      partition_label: shed.partitionLabel ?? null,
      operational_location_display: shed.locationDisplay ?? null,
      doseRule,
      state: cell.state,
      animalCount: cell.count,
      minAdministeredDate: cell.adminFrom ?? null,
      maxAdministeredDate: cell.adminTo ?? null,
      minDueDate: cell.dueFrom ?? null,
      maxDueDate: cell.dueTo ?? null,
    });
  }
  return out;
}

// useShedDoseMatrix loads the shed x dose grid after first paint.
//
// It used to ship inside /vaccination/command. Interning it cut 408KB to 155KB and the board's p90
// barely moved -- the cost was the round trip, not the payload -- so the section itself moved:
// p90 343 with it inline, 251-281 without. Numbers are unchanged and whole-scope; only their
// arrival moved, and the caller renders a loading state rather than an empty grid that would read
// as "no sheds".
export function useShedDoseMatrix(scope: DrilldownScope): DrilldownState<ShedDoseCellRow[]> {
  const params = scopeParams(scope);
  const query = params.toString();
  return useDrilldown<ShedDoseCellRow[]>(
    query || "all",
    async (signal) => {
      const page = await fetchJson<{ matrix: ShedDoseMatrixWire }>(
        `/api/vaccination/command/shed-dose-matrix${query ? `?${query}` : ""}`,
        signal,
      );
      return expandShedDoseMatrix(page.matrix);
    },
    NO_SHED_DOSE,
  );
}
const NO_CLOSED: ClosedWithoutDoseRow[] = [];
const NO_SHED_VACCINE: { animals: ShedVaccineAnimalRow[]; proofVideos: Array<{ path: string }> } = {
  animals: [],
  proofVideos: [],
};
const NO_COHORT: { days: CohortDayRow[]; exceptionGoats: CohortAnimalRow[]; truncated: boolean } = {
  days: [],
  exceptionGoats: [],
  truncated: false,
};

// useCohortMatrix loads the cohort grid after first paint.
//
// It used to ship inside /vaccination/command. Its three statements -- the cell aggregate, the true
// herd head count and the dose-sequence exception count -- were ~420ms of that endpoint's ~850ms of
// SQL, enough on their own to hold it over a p90 300ms budget the latency policy hard-caps. The
// numbers are unchanged and whole-scope; only their arrival moved, and the caller renders an
// explicit loading state for the gap rather than an empty grid that reads as "no cohorts".
export function useCohortMatrix<T = CohortMatrixCell>(scope: DrilldownScope): DrilldownState<T[]> {
  const params = scopeParams(scope);
  const query = params.toString();
  return useDrilldown<T[]>(
    // Keyed on the scope alone and never null: this section always loads, unlike a drawer that
    // waits for a click.
    query || "all",
    async (signal) => {
      const page = await fetchJson<{ cells: T[] }>(
        `/api/vaccination/command/cohort-matrix${query ? `?${query}` : ""}`,
        signal,
      );
      return page.cells ?? [];
    },
    NO_COHORT_MATRIX as unknown as T[],
  );
}

export function useClosedWithoutDoseAnimals(open: boolean, scope: DrilldownScope): DrilldownState<ClosedWithoutDoseRow[]> {
  const params = scopeParams(scope);
  params.set("limit", "200");
  const query = params.toString();
  return useDrilldown<ClosedWithoutDoseRow[]>(
    open ? query : null,
    async (signal) => {
      const result = await fetchAllPages<ClosedWithoutDoseRow, { animals: ClosedWithoutDoseRow[]; nextCursor?: string }>(
        "/api/vaccination/command/closed-without-dose",
        params,
        (page) => page.animals ?? [],
        signal,
      );
      return result.items;
    },
    NO_CLOSED,
  );
}

export function useShedVaccineAnimals(
  cell: { shedId: string; vaccineCode: string; partitionLabel?: string | null } | null,
  scope: DrilldownScope,
): DrilldownState<{ animals: ShedVaccineAnimalRow[]; proofVideos: Array<{ path: string }> }> {
  const params = scopeParams(scope);
  if (cell) {
    params.set("shed_id", cell.shedId);
    params.set("vaccine_code", cell.vaccineCode);
    // Sent even when empty: an unpartitioned shed's cell key IS the empty label, so omitting it
    // would ask for a different cell than the one the reader clicked.
    params.set("partition_label", cell.partitionLabel ?? "");
    params.set("limit", "200");
  }
  const query = cell ? params.toString() : null;
  return useDrilldown(
    query,
    async (signal) => {
      // The first page carries the videos: they belong to the SHED and the days this page's animals
      // were recorded, so paging further animals cannot add footage the first page did not name.
      const first = await fetchJson<{
        animals: ShedVaccineAnimalRow[];
        proofVideos: Array<{ path: string }>;
        nextCursor?: string;
      }>(`/api/vaccination/command/shed-vaccine-animals?${params.toString()}`, signal);
      const animals = [...(first.animals ?? [])];
      let cursor = first.nextCursor;
      for (let page = 1; page < 4 && cursor; page += 1) {
        const query2 = new URLSearchParams(params);
        query2.set("cursor", cursor);
        const next = await fetchJson<{ animals: ShedVaccineAnimalRow[]; nextCursor?: string }>(
          `/api/vaccination/command/shed-vaccine-animals?${query2.toString()}`,
          signal,
        );
        animals.push(...(next.animals ?? []));
        cursor = next.nextCursor;
      }
      return { animals, proofVideos: first.proofVideos ?? [] };
    },
    NO_SHED_VACCINE,
  );
}

// useCohortCellDetail fetches the administered-day split and the dose-sequence exceptions for every
// backend cell that rolls into the open drawer, then merges them the way the board's row builder
// merges the summary numbers: days with the same date add, exception animals concatenate.
export function useCohortCellDetail(
  refs: CohortCellRef[] | null,
  scope: DrilldownScope,
): DrilldownState<{ days: CohortDayRow[]; exceptionGoats: CohortAnimalRow[]; truncated: boolean }> {
  const key = refs && refs.length > 0
    ? JSON.stringify([refs, scope])
    : null;

  return useDrilldown(
    key,
    async (signal) => {
      const cells = refs ?? [];
      const results = await Promise.all(
        cells.map(async (ref) => {
          const params = scopeParams(scope);
          params.set("cohort_park_id", ref.cohortParkId ?? "");
          params.set("management_stage", ref.managementStage);
          params.set("sex", ref.sex);
          params.set("dose_codes", ref.doseCodes.join(","));
          params.set("limit", "200");
          const [days, exceptions] = await Promise.all([
            fetchJson<{ days: CohortDayRow[] }>(
              `/api/vaccination/command/cohort-days?${params.toString()}`,
              signal,
            ),
            fetchAllPages<CohortAnimalRow, { animals: CohortAnimalRow[]; nextCursor?: string }>(
              "/api/vaccination/command/cohort-exceptions",
              params,
              (page) => page.animals ?? [],
              signal,
            ),
          ]);
          return { days: days.days ?? [], exceptions: exceptions.items, truncated: exceptions.truncated };
        }),
      );

      const byDate = new Map<string, number>();
      const exceptionGoats: CohortAnimalRow[] = [];
      const seenGoat = new Set<string>();
      let truncated = false;
      for (const result of results) {
        truncated = truncated || result.truncated;
        for (const day of result.days) {
          byDate.set(day.date, (byDate.get(day.date) ?? 0) + day.animalCount);
        }
        for (const goat of result.exceptions) {
          // One animal can hold the missing dose in more than one contributing cell; the drawer
          // names ANIMALS, so it must not list the same one twice.
          if (seenGoat.has(goat.goatId)) continue;
          seenGoat.add(goat.goatId);
          exceptionGoats.push(goat);
        }
      }
      const days = [...byDate.entries()]
        .map(([date, animalCount]) => ({ date, animalCount }))
        .sort((a, b) => a.date.localeCompare(b.date));
      return { days, exceptionGoats, truncated };
    },
    NO_COHORT,
  );
}

// useDriveCatalogue completes the drive picker after first paint.
//
// /vaccination/command carries only the first page of drives (20). The catalogue used to ship whole
// and was 448ms and 753 KB -- more than the endpoint's entire 512 KB budget -- for a dropdown, and
// it was the board's critical path once the drilldowns had moved off it. The board therefore paints
// from its first page and this hook pages the rest in the background, so the picker and the
// future-drive rows stay complete without the reader waiting for them.
export function useDriveCatalogue<T extends { driveBatchId?: string; parkId?: string | null }>(
  firstPage: T[],
  truncated: boolean,
  parkId?: string,
): { options: T[]; loading: boolean } {
  const firstPageKey = firstPage.map((option) => `${option.driveBatchId}|${option.parkId ?? ""}`).join(",");
  const requestKey = truncated ? `${parkId ?? ""}|${firstPageKey}` : null;
  // Keyed for the same reason useDrilldown is: no synchronous setState in an effect.
  const [state, setState] = useState<{ key: string | null; options: T[] }>({ key: null, options: [] });

  useEffect(() => {
    if (!requestKey) return;
    const controller = new AbortController();
    const params = new URLSearchParams();
    if (parkId) params.set("park_id", parkId);
    params.set("limit", "100");
    fetchAllPages<T, { options: T[]; nextCursor?: string }>(
      "/api/vaccination/command/drives",
      params,
      (page) => page.options ?? [],
      controller.signal,
      // The catalogue is bounded in the product too: 400 drives is far past what a picker can be
      // read at, and driveOptionsTruncated keeps the UI honest if a tenant ever exceeds it.
      4,
    )
      .then((result) => {
        if (controller.signal.aborted) return;
        setState({ key: requestKey, options: result.items });
      })
      .catch(() => {
        if (controller.signal.aborted) return;
        // The board already rendered from its first page. A failed background completion leaves the
        // picker short, which driveOptionsTruncated already declares; it must not blank the board.
        setState({ key: requestKey, options: [] });
      });
    return () => controller.abort();
  }, [requestKey, parkId]);

  const settled = requestKey !== null && state.key === requestKey;
  const extra = settled ? state.options : [];

  const options = useMemo(() => {
    if (extra.length === 0) return firstPage;
    // The catalogue endpoint returns the SAME ordering the board's first page uses and starts from
    // the beginning, so it is a superset. De-duplicate on the row key — (batch, park), because a
    // drive spanning two parks is two rows — and prefer the catalogue copy.
    const merged = new Map<string, T>();
    for (const option of [...firstPage, ...extra]) {
      merged.set(`${option.driveBatchId}|${option.parkId ?? ""}`, option);
    }
    return [...merged.values()];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [firstPageKey, extra]);

  return { options, loading: requestKey !== null && !settled };
}
