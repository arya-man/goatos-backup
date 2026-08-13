import { parseScope, scopeHref, type Scope } from "@/lib/scope";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import type { VaccinationLiveTrackerStatus } from "@/lib/api/server";

// URL keys owned by this page. Park / date scope is NOT among them: that lives in the top bar and is
// read through parseScope, exactly like every other scope-aware surface.
export const LIVE_TRACKER_PATH = "/vaccination/live-tracker";

const STATUS_VALUES: VaccinationLiveTrackerStatus[] = ["active", "done", "pending", "review"];

export type LiveTrackerParams = {
  sp: RouteSearchParams;
  scope: Scope;
  parkId?: string;
  shedId?: string;
  partitionLabel?: string;
  operatorId?: string;
  vaccineCode?: string;
  status?: VaccinationLiveTrackerStatus;
  businessDate?: string;
  activityLimit: number;
  // Both halves of the feed's keyset cursor. occurred_at alone is not a key.
  activityBefore?: string;
  activityBeforeId?: string;
  hasFilter: boolean;
  // hasNarrowing includes the top-bar PARK scope; hasFilter does not.
  //
  // They are different questions. "Is anything narrowing this board?" decides whether the empty
  // state may say "No vaccination drive work on this day" — which is a claim about the whole day and
  // is false when the other park is running. "Is there a page-local filter to clear?" decides whether
  // a clear-all control does anything, and clearing must not silently drop the shared top-bar scope.
  hasNarrowing: boolean;
};

const ACTIVITY_LIMIT_DEFAULT = 40;
const ACTIVITY_LIMIT_MAX = 100;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const BUSINESS_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function uuidOrUndefined(raw: string | undefined): string | undefined {
  return raw && UUID_RE.test(raw) ? raw : undefined;
}

function boundedText(raw: string | undefined, max: number): string | undefined {
  const trimmed = raw?.trim();
  if (!trimmed || trimmed.length > max) return undefined;
  return trimmed;
}

// Pure parser: no fetching, no side effects, so the board and its skeleton can agree on the exact
// same filter state without running the read twice.
export function parseLiveTrackerParams(searchParams: RouteSearchParams | undefined): LiveTrackerParams {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);

  // Park comes from the top-bar scope. The filter bar's park control writes the SAME scope key, so
  // there is one park truth on the page rather than a second inline scope.
  const parkId = scope.parkId;
  const shedId = uuidOrUndefined(one(sp, "lt_shed"));
  const partitionLabel = boundedText(one(sp, "lt_partition"), 64);
  const operatorId = uuidOrUndefined(one(sp, "lt_operator"));
  const vaccineCode = boundedText(one(sp, "lt_vaccine"), 64);
  const status = STATUS_VALUES.find((value) => value === one(sp, "lt_status"));
  const businessDateRaw = one(sp, "lt_date");
  const businessDate = businessDateRaw && BUSINESS_DATE_RE.test(businessDateRaw) ? businessDateRaw : undefined;
  const activityLimit = boundedInt(one(sp, "lt_activity_limit"), ACTIVITY_LIMIT_DEFAULT, 1, ACTIVITY_LIMIT_MAX);
  const activityBefore = boundedText(one(sp, "lt_activity_before"), 40);
  const activityBeforeId = boundedText(one(sp, "lt_activity_before_id"), 128);

  return {
    sp,
    scope,
    parkId,
    shedId,
    partitionLabel,
    operatorId,
    vaccineCode,
    status,
    businessDate,
    activityLimit,
    activityBefore,
    activityBeforeId,
    hasFilter: Boolean(shedId || partitionLabel || operatorId || vaccineCode || status),
    hasNarrowing: Boolean(parkId || shedId || partitionLabel || operatorId || vaccineCode || status),
  };
}

// Every link on this page routes through here, so the top-bar scope and the page's own filter state
// survive each other. Hand-rolling a URLSearchParams for one of them is how a filter link silently
// drops the park a user had selected.
export function liveTrackerHref(params: LiveTrackerParams, overrides: Record<string, string | undefined> = {}): string {
  const { park, ...rest } = overrides as Record<string, string | undefined> & { park?: string };
  const scopeOverride = "park" in overrides ? { park: park ?? null, mode: (park ? "park" : "company") as "park" | "company" } : {};
  return scopeHref(LIVE_TRACKER_PATH, params.scope, scopeOverride, {
    lt_shed: params.shedId,
    lt_partition: params.partitionLabel,
    lt_operator: params.operatorId,
    lt_vaccine: params.vaccineCode,
    lt_status: params.status,
    lt_date: params.businessDate,
    // The feed cursor is deliberately NOT carried across these links. It is a PAIR
    // (lt_activity_before + lt_activity_before_id) that keys one specific filter set's feed, so
    // re-emitting it onto a link that changes the filter set would page a different feed from a
    // boundary that does not belong to it. Every link here resets the feed to its newest page.
    ...rest,
  });
}

export function liveTrackerResetHref(params: LiveTrackerParams): string {
  return scopeHref(LIVE_TRACKER_PATH, params.scope, {}, { lt_date: params.businessDate });
}
