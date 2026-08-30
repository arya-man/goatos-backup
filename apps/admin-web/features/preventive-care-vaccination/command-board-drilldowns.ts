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

export type ClosedWithoutDoseRow = {
  goatId: string;
  displayId: string;
  tag?: string | null;
  tag2?: string | null;
  operational_location_display?: string | null;
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
): Promise<TItem[]> {
  const items: TItem[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < maxPages; page += 1) {
    const query = new URLSearchParams(params);
    if (cursor) query.set("cursor", cursor);
    const result = await fetchJson<TPage>(`${baseUrl}?${query.toString()}`, signal);
    items.push(...select(result));
    if (!result.nextCursor) break;
    cursor = result.nextCursor;
  }
  return items;
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
  const [state, setState] = useState<DrilldownState<T>>({ data: empty, loading: false, error: null });
  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    if (!key) {
      setState({ data: empty, loading: false, error: null });
      return;
    }
    const controller = new AbortController();
    setState({ data: empty, loading: true, error: null });
    loadRef
      .current(controller.signal)
      .then((data) => {
        if (controller.signal.aborted) return;
        setState({ data, loading: false, error: null });
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setState({ data: empty, loading: false, error: err instanceof Error ? err.message : "error" });
      });
    return () => controller.abort();
    // `empty` is a stable module-level constant at every call site; `load` is read through a ref so
    // an inline closure does not retrigger the fetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  return state;
}

const NO_CLOSED: ClosedWithoutDoseRow[] = [];
const NO_SHED_VACCINE: { animals: ShedVaccineAnimalRow[]; proofVideos: Array<{ path: string }> } = {
  animals: [],
  proofVideos: [],
};
const NO_COHORT: { days: CohortDayRow[]; exceptionGoats: CohortAnimalRow[] } = { days: [], exceptionGoats: [] };

export function useClosedWithoutDoseAnimals(open: boolean, scope: DrilldownScope): DrilldownState<ClosedWithoutDoseRow[]> {
  const params = scopeParams(scope);
  params.set("limit", "200");
  const query = params.toString();
  return useDrilldown<ClosedWithoutDoseRow[]>(
    open ? query : null,
    (signal) =>
      fetchAllPages<ClosedWithoutDoseRow, { animals: ClosedWithoutDoseRow[]; nextCursor?: string }>(
        "/api/vaccination/command/closed-without-dose",
        params,
        (page) => page.animals ?? [],
        signal,
      ),
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
): DrilldownState<{ days: CohortDayRow[]; exceptionGoats: CohortAnimalRow[] }> {
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
          return { days: days.days ?? [], exceptions };
        }),
      );

      const byDate = new Map<string, number>();
      const exceptionGoats: CohortAnimalRow[] = [];
      const seenGoat = new Set<string>();
      for (const result of results) {
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
      return { days, exceptionGoats };
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
  const [extra, setExtra] = useState<T[]>([]);
  const [loading, setLoading] = useState(false);
  const firstPageKey = firstPage.map((option) => `${option.driveBatchId}|${option.parkId ?? ""}`).join(",");

  useEffect(() => {
    if (!truncated) {
      setExtra([]);
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    const params = new URLSearchParams();
    if (parkId) params.set("park_id", parkId);
    params.set("limit", "100");
    fetchAllPages<T, { options: T[]; nextCursor?: string }>(
      "/api/vaccination/command/drives",
      params,
      (page) => page.options ?? [],
      controller.signal,
      // The catalogue is bounded in the product too: 400 drives is far past what a picker can be
      // read at, and DriveOptionsTruncated keeps the UI honest if a tenant ever exceeds it.
      4,
    )
      .then((options) => {
        if (controller.signal.aborted) return;
        setExtra(options);
        setLoading(false);
      })
      .catch(() => {
        if (controller.signal.aborted) return;
        // The board already rendered from its first page. A failed background completion leaves the
        // picker short, which DriveOptionsTruncated already declares; it must not blank the board.
        setLoading(false);
      });
    return () => controller.abort();
  }, [truncated, parkId, firstPageKey]);

  const options = useMemo(() => {
    if (extra.length === 0) return firstPage;
    // The catalogue endpoint returns the SAME ordering the board's first page uses and starts from
    // the beginning, so it is a superset. De-duplicate on the row key -- (batch, park), because a
    // drive spanning two parks is two rows -- and prefer the catalogue copy.
    const merged = new Map<string, T>();
    for (const option of [...firstPage, ...extra]) {
      merged.set(`${option.driveBatchId}|${option.parkId ?? ""}`, option);
    }
    return [...merged.values()];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [firstPageKey, extra]);

  return { options, loading };
}
