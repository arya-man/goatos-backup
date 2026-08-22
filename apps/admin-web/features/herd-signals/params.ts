import { parseScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import type { HerdSignalMappingState, HerdSignalMovementState, HerdSignalPatternState } from "@/lib/api/herd-signals";

// URL keys owned by this page, mirroring the vaccination-live-tracker convention: park scope comes
// from the shared top-bar scope (parseScope), everything else is a page-local `hs_*` key so this
// page's filters never collide with another surface reusing the same search params.
export const HERD_SIGNALS_PATH = "/herd-signals";

const MOVEMENT_VALUES: HerdSignalMovementState[] = ["moving", "low", "quiet", "not_moving", "stale"];
const MAPPING_VALUES: HerdSignalMappingState[] = ["mapped", "unmapped", "conflict"];
const PATTERN_VALUES: HerdSignalPatternState[] = ["no_movement", "quiet_watch", "inactive", "missing", "spike", "recovered", "normal"];
export const HERD_SIGNALS_TABS = ["live", "animals", "gateways", "alerts", "mapping", "insights"] as const;
export type HerdSignalsTab = (typeof HERD_SIGNALS_TABS)[number];

// KPI-card click-to-filter targets (Section: KPI cards). Each maps a card key to the query params
// it applies; clicking the SAME card again clears it (see herd-signals-kpis.tsx).
export const KPI_FILTER_KEYS = ["moving", "quiet", "weak_signal", "missing_signal", "low_battery"] as const;
export type KpiFilterKey = (typeof KPI_FILTER_KEYS)[number];

export type HerdSignalsParams = {
  sp: RouteSearchParams;
  scope: Scope;
  tab: HerdSignalsTab;
  parkId?: string;
  shedId?: string;
  q?: string;
  movementState?: HerdSignalMovementState;
  mappingState?: HerdSignalMappingState;
  pattern?: HerdSignalPatternState;
  kpi?: KpiFilterKey;
  cursor?: string;
  limit: number;
  hasFilter: boolean;
};

const LIMIT_DEFAULT = 25;
const LIMIT_MAX = 100;

function boundedText(raw: string | undefined, max: number): string | undefined {
  const trimmed = raw?.trim();
  if (!trimmed || trimmed.length > max) return undefined;
  return trimmed;
}

// Maps a KPI card to the concrete filter it applies. "moving" and "quiet" filter by movement_state;
// the three signal-health cards filter fields the /live endpoint does not expose as their own query
// key today, so they fall back to the closest supported filter (pattern for missing/weak reads as a
// movement/mapping proxy is wrong — instead these three are applied CLIENT-SIDE on the fetched page
// via the shared summary-vs-rows contract: see herd-signals-board.tsx `kpiRowFilter`). This keeps
// every KPI number itself a `summary` field (server aggregate), never a recount of rows on screen.
export function kpiToMovementState(kpi: KpiFilterKey | undefined): HerdSignalMovementState | undefined {
  if (kpi === "moving") return "moving";
  if (kpi === "quiet") return "quiet";
  return undefined;
}

export function parseHerdSignalsParams(searchParams: RouteSearchParams | undefined): HerdSignalsParams {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const tab = HERD_SIGNALS_TABS.find((value) => value === one(sp, "hs_tab")) ?? "live";
  const parkId = scope.parkId;
  const shedId = boundedText(one(sp, "hs_shed"), 64);
  const q = boundedText(one(sp, "hs_q"), 120);
  const movementState = MOVEMENT_VALUES.find((value) => value === one(sp, "hs_move"));
  const mappingState = MAPPING_VALUES.find((value) => value === one(sp, "hs_map"));
  const pattern = PATTERN_VALUES.find((value) => value === one(sp, "hs_pattern"));
  const kpi = KPI_FILTER_KEYS.find((value) => value === one(sp, "hs_kpi"));
  const cursor = boundedText(one(sp, "hs_cursor"), 200);
  const limit = boundedInt(one(sp, "hs_limit"), LIMIT_DEFAULT, 10, LIMIT_MAX);

  return {
    sp,
    scope,
    tab,
    parkId,
    shedId,
    q,
    movementState,
    mappingState,
    pattern,
    kpi,
    cursor,
    limit,
    hasFilter: Boolean(shedId || q || movementState || mappingState || pattern || kpi),
  };
}

// Every link on this page routes through here so the top-bar park scope and this page's own filter
// state survive each other, exactly like liveTrackerHref. Overrides that omit `hs_cursor` reset
// pagination — every filter change starts back at the first page of its own cursor sequence.
export function herdSignalsHref(params: HerdSignalsParams, overrides: Record<string, string | undefined> = {}): string {
  const { park, ...rest } = overrides as Record<string, string | undefined> & { park?: string };
  const scopeOverride = "park" in overrides ? { park: park ?? null, mode: (park ? "park" : "company") as "park" | "company" } : {};
  return scopeHref(HERD_SIGNALS_PATH, params.scope, scopeOverride, {
    hs_tab: params.tab === "live" ? undefined : params.tab,
    hs_shed: params.shedId,
    hs_q: params.q,
    hs_move: params.movementState,
    hs_map: params.mappingState,
    hs_pattern: params.pattern,
    hs_kpi: params.kpi,
    hs_limit: params.limit === LIMIT_DEFAULT ? undefined : String(params.limit),
    hs_cursor: undefined,
    ...rest,
  });
}

export function herdSignalsResetHref(params: HerdSignalsParams): string {
  return scopeHref(HERD_SIGNALS_PATH, params.scope, {}, { hs_tab: params.tab === "live" ? undefined : params.tab });
}
