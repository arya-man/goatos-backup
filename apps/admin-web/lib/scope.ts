// Single top-bar scope contract shared by every command/operations screen (Control Tower, Action Center,
// Protocol Adherence, Workflows, Vaccination, Execution). The URL carries backend-safe values; the UI shows
// human labels. One parser, one query-builder, one backend mapping — so scope can never disagree across
// screens.
//
// URL params:
//   scope_mode = company | park        (park without `park` = all parks as park/shed breakdown)
//   park       = <location uuid>        (omitted/`all` = company-wide)
//   range      = last_7_days | last_30_days | custom
//   as_of      = YYYY-MM-DD             (top-bar date scope; default today. Per-endpoint semantics:
//                                        process-integrity reads (CT/AC/PA/WF) reconstruct that
//                                        business date from versioned snapshots; the vaccination
//                                        execution/shed reads are current-view-only and reject a past
//                                        as_of with 400 historical_as_of_unsupported. Actions maps it
//                                        to verification business_date so historical proof stays available.)
//   date_from / date_to = YYYY-MM-DD    (custom range bounds)
//   domain     = vaccination | procurement | …  (command-lens data source; omitted = default vaccination)
import { one, type RouteSearchParams } from "@/lib/search-params";

export type Park = { id: string; code: string | null; name: string };
export type ScopeMode = "company" | "park";
export type RangeKey = "last_7_days" | "last_30_days" | "custom";

export interface Scope {
  mode: ScopeMode;
  parkId?: string;
  range: RangeKey;
  asOf?: string;
  dateFrom?: string;
  dateTo?: string;
  // Command-lens data source (Control Tower / Action Center / Protocol Adherence / Workflows). Parsed and
  // PRESERVED across links now so Phase 4 can switch the CT/AC/PA/WF data source by domain WITHOUT rewriting
  // every scope link. It is NOT sent to any backend yet (backendScope omits it); the lens data-source switch
  // lands in Phase 4. `undefined` means the default (vaccination) domain.
  domain?: string;
}

const RANGES: RangeKey[] = ["last_7_days", "last_30_days", "custom"];

export function parseScope(sp: RouteSearchParams | undefined): Scope {
  const params = sp ?? {};
  const parkRaw = one(params, "park");
  const parkId = parkRaw && parkRaw !== "all" ? parkRaw : undefined;
  const modeRaw = one(params, "scope_mode");
  const mode: ScopeMode = parkId || modeRaw === "park" ? "park" : "company";
  const range = (RANGES.find((r) => r === one(params, "range")) ?? "last_30_days") as RangeKey;
  const domainRaw = one(params, "domain");
  return {
    mode,
    parkId,
    range,
    asOf: one(params, "as_of"),
    dateFrom: one(params, "date_from"),
    dateTo: one(params, "date_to"),
    domain: domainRaw && domainRaw !== "all" && domainRaw !== "vaccination" ? domainRaw : undefined,
  };
}

// Number of days a range key covers (custom falls back to its date bounds, else 30).
export function rangeDays(range: RangeKey): number {
  return range === "last_7_days" ? 7 : range === "last_30_days" ? 30 : 30;
}

// What each backend actually HONORS today (do not overstate — agents copy comments as truth):
//   - park_id: honored by ALL scope-aware reads (Control Tower, Action Center, Protocol Adherence,
//     Workflows, /vaccination operations, execution, verification queue).
//   - as_of: honored as a top-bar DATE scope, with two different semantics. The process-integrity reads
//     (Control Tower, Action Center, Protocol Adherence, Workflows) reconstruct completion/dose state and
//     the obligation bucket as-of that instant from versioned snapshots — historical is supported. The
//     vaccination reads (GET /vaccination/operations, /vaccination/execution + shed summary/detail,
//     /vaccination/schedule, and /app/vaccination/coverage) are CURRENT-VIEW-ONLY: operations/execution
//     keep a single serving snapshot at ~now and reject a past as_of with 400 historical_as_of_unsupported
//     (future clamps to now); schedule reads only pre-materialized month windows. The verification
//     queue consumes the date through the Actions page's business_date mapping.
//   - range / date_from (lower bound): parsed + carried in the URL but NOT consumed by ANY query yet —
//     "Last 7 vs Last 30" does not change results until the range backend pass lands. Do not pretend it
//     filters. backendScope therefore returns only park_id + as_of (the genuinely-honored params).
export function backendScope(scope: Scope): { parkId?: string; asOf?: string } {
  return {
    parkId: scope.parkId,
    // as_of is a DATE in the URL; "compute state as of <date>" means inclusive end of that Goat OS
    // business day. Send the India business-calendar instant, not UTC end-of-day.
    asOf: scope.asOf ? `${scope.asOf}T23:59:59.999+05:30` : undefined,
  };
}

// Build a URL query string that preserves the current scope, applying overrides (e.g. switch park/range).
// Pass park:null to clear to company-wide.
export function scopeHref(
  basePath: string,
  scope: Scope,
  overrides: Partial<{ park: string | null; mode: ScopeMode; range: RangeKey; asOf: string | null; domain: string | null }> = {},
  // Page-specific filters (severity, state, bucket, …) layered ON TOP of the preserved top-bar scope. This
  // is how CT/AC/PA/WF/Execution build their filter links without dropping scope — never hand-roll
  // URLSearchParams for a scoped link.
  extra: Record<string, string | undefined> = {},
): string {
  const p = new URLSearchParams();
  const park = "park" in overrides ? overrides.park : scope.parkId ?? null;
  const mode = overrides.mode ?? (park ? "park" : scope.mode);
  const range = overrides.range ?? scope.range;
  const asOf = "asOf" in overrides ? overrides.asOf : scope.asOf;
  const domain = "domain" in overrides ? overrides.domain : scope.domain ?? null;
  if (mode === "park") {
    p.set("scope_mode", "park");
    if (park) p.set("park", park);
  } else {
    p.set("scope_mode", "company");
  }
  if (range !== "last_30_days") p.set("range", range);
  if (asOf) p.set("as_of", asOf);
  if (domain && domain !== "vaccination") p.set("domain", domain);
  if (range === "custom") {
    if (scope.dateFrom) p.set("date_from", scope.dateFrom);
    if (scope.dateTo) p.set("date_to", scope.dateTo);
  }
  for (const [k, v] of Object.entries(extra)) {
    if (v && v !== "all") p.set(k, v);
  }
  const qs = p.toString();
  return qs ? `${basePath}?${qs}` : basePath;
}

export function parkLabel(parks: Park[], parkId: string | undefined): string {
  const park = parks.find((p) => p.id === parkId);
  return park?.code ?? park?.name ?? "";
}
