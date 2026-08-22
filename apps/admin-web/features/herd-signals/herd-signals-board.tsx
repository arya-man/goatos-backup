import type { ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { RouteSearchParams } from "@/lib/search-params";
import {
  getHerdSignalsGateways,
  getHerdSignalsInsights,
  getHerdSignalsLive,
  type HerdGatewaysResponse,
  type HerdInsightsResponse,
  type HerdSignalsLiveResponse,
} from "@/lib/api/herd-signals";
import { listLocations, type ApiResult } from "@/lib/api/server";
import { HerdSignalsPoller } from "./herd-signals-poller";
import { HerdSignalsNavProvider } from "./herd-signals-nav-context";
import { HerdSignalsKpis } from "./herd-signals-kpis";
import { HerdSignalsFilters, type ParkOption, type ShedOption } from "./herd-signals-filters";
import { HerdSignalsTable } from "./herd-signals-table";
import { HerdSignalsMappingTable } from "./herd-signals-mapping-table";
import { HerdSignalsGateways } from "./herd-signals-gateways";
import { HerdSignalsInsights } from "./herd-signals-insights";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import { PATTERN_LABEL, PATTERN_TONE, PATTERN_WHY, fmtAgo } from "./format";
import { HERD_SIGNALS_TABS, herdSignalsHref, kpiToMovementState, parseHerdSignalsParams, type HerdSignalsParams, type HerdSignalsTab } from "./params";

const TAB_LABEL: Record<HerdSignalsTab, string> = {
  live: "Live Monitor",
  animals: "Animals",
  gateways: "Gateways",
  alerts: "Alerts",
  mapping: "Tag Mapping",
  insights: "Insights",
};

// One glyph per tab, using exactly the paths the reference `.segs` buttons carry. Labels alone made
// the six tabs a wall of same-weight text; the icon is what lets the eye find "Gateways" without
// reading the row. Rendered at `ic sm` (14px) as the reference does, not the 18px default `ic`.
const TAB_ICON: Record<HerdSignalsTab, ReactNode> = {
  live: (
    <>
      <path d="M4.9 19.1a10 10 0 0 1 0-14.2" />
      <path d="M7.8 16.2a6 6 0 0 1 0-8.4" />
      <circle cx="12" cy="12" r="2" />
      <path d="M16.2 7.8a6 6 0 0 1 0 8.4" />
      <path d="M19.1 4.9a10 10 0 0 1 0 14.2" />
    </>
  ),
  animals: (
    <>
      <path d="M21 10c0 7-9 12-9 12s-9-5-9-12a9 9 0 0 1 18 0Z" />
      <circle cx="12" cy="10" r="3" />
    </>
  ),
  gateways: (
    <>
      <path d="M5 12.5a7 7 0 0 1 14 0" />
      <path d="M2 9a11 11 0 0 1 20 0" />
      <circle cx="12" cy="17" r="2" />
    </>
  ),
  alerts: (
    <>
      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
      <path d="M12 9v4" />
      <path d="M12 17h.01" />
    </>
  ),
  mapping: (
    <>
      <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.7 1.7" />
      <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7L12.2 19" />
    </>
  ),
  insights: <path d="M3 12h4l3 8 4-16 3 8h4" />,
};

// What the Alerts tab lists: every tag whose pattern classification is anything other than
// "normal". pattern_state is a single, always-populated classification (the backend computes it on
// every read), so "not normal" is a partition -- selected SERVER-SIDE via `pattern: "not_normal"`
// in fetchForTab (herdSignalsLiveFilter, pattern_state <> 'normal'), not by filtering whatever page
// of rows happens to come back. Client-side selection from one fetched page is wrong at scale: a
// page can be entirely non-alerting rows while thousands of alerting tags exist elsewhere in the
// fleet. The tab badge stays a real server-side aggregate (fleet total minus the tenant-wide
// `normal` count) either way.

export function loadHerdSignalsLive(searchParams: RouteSearchParams | undefined): Promise<ApiResult<HerdSignalsLiveResponse>> {
  const params = parseHerdSignalsParams(searchParams);
  return fetchForTab(params);
}

function fetchForTab(params: HerdSignalsParams): Promise<ApiResult<HerdSignalsLiveResponse>> {
  const common = {
    parkId: params.parkId,
    shedId: params.shedId,
    q: params.q,
    cursor: params.cursor,
    limit: params.limit,
  };
  if (params.tab === "animals") {
    return getHerdSignalsLive({ ...common, mappingState: "mapped", movementState: params.movementState, pattern: params.pattern });
  }
  if (params.tab === "mapping") {
    return getHerdSignalsLive({ ...common, mappingState: params.mappingState });
  }
  if (params.tab === "alerts") {
    // The mock's Alerts tab is ONE unified list of every alerting tag, with no type-filter row at
    // all -- so the query asks the server for the whole "not normal" partition (pattern_state <>
    // 'normal') rather than fetching one page unfiltered and picking the alerting rows out of it
    // client-side. At fleet scale a page can be entirely non-alerting rows; a server-side filter is
    // the only way the Alerts tab stays correct past the first page.
    return getHerdSignalsLive({ ...common, pattern: "not_normal" });
  }
  // live tab
  return getHerdSignalsLive({
    ...common,
    movementState: kpiToMovementState(params.kpi) ?? params.movementState,
    mappingState: params.mappingState,
    pattern: params.pattern,
  });
}

export function HerdSignalsSkeleton() {
  return (
    <div className="herd-signals-page" aria-busy="true">
      <div className="phead">
        <div>
          <div className="crumb">Herd Signals / <b>Live Monitor</b></div>
          <h1>Herd Signals</h1>
        </div>
      </div>
      <div className="kpis">
        {Array.from({ length: 6 }, (_, index) => (
          <div key={index} className="kpi">
            <div className="skelrow" style={{ width: 92, height: 12 }} />
            <div className="skelrow" style={{ width: 64, height: 26, marginTop: 8 }} />
          </div>
        ))}
      </div>
      <div className="card">
        <div className="bd">
          {Array.from({ length: 8 }, (_, index) => (
            <div key={index} className="skelrow" style={{ width: "100%", height: 13, marginBottom: 10 }} />
          ))}
        </div>
      </div>
    </div>
  );
}

export async function HerdSignalsBoard({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const params = parseHerdSignalsParams(searchParams);
  // This IS a server-rendered "as of now" read (matches the sibling live-tracker board's
  // router.refresh()-driven model): the wall-clock instant is the point of the request, not
  // incidental impurity to memoize away.
  // eslint-disable-next-line react-hooks/purity -- see comment above.
  const nowMs = Date.now();

  // The tab badges are WHOLE-FLEET counts on every tab, exactly as the mock keeps them. The
  // summary on the active tab's own /live response cannot supply them: the backend narrows that
  // summary by the same mapping_state / pattern the tab asked for (verified against the running
  // API -- `?pattern=inactive` returns a summary of all zeroes, `?mapping_state=mapped` returns
  // tags_seen 18 instead of 20). Reading badges off it is what made every badge render 0 on
  // ?hs_tab=alerts. So the badges come from their own scope-only aggregate reads -- park / shed /
  // search only, never a tab's narrowing filter and never a count of the fetched rows
  // (AGENTS.md operational read model contract).
  const scopeOnly = { parkId: params.parkId, shedId: params.shedId, q: params.q, limit: 1 };
  // Skipped when the active tab already asked for exactly the scope-only summary, so the common
  // case costs no extra read.
  const tabNarrowsSummary = params.tab === "animals" || Boolean(params.mappingState) || Boolean(params.pattern);

  const [liveResult, fleetOwnResult, normalResult, gatewaysResult, insightsResult, locationsResult] = await Promise.all([
    fetchForTab(params),
    tabNarrowsSummary ? getHerdSignalsLive(scopeOnly) : Promise.resolve(null),
    // The tenant-wide "normal pattern" count. Alerting = fleet total - normal, both server-side
    // aggregates over the same scope, which is the only honest source for the Alerts badge.
    getHerdSignalsLive({ ...scopeOnly, pattern: "normal" }),
    getHerdSignalsGateways(),
    params.tab === "insights" ? getHerdSignalsInsights() : Promise.resolve(null),
    // Park options for the filter bar come from the canonical locations master (BUG-019: never a
    // client-derived list built from whatever parks happen to appear on the current page of rows).
    listLocations({ type: "park", status: "active" }),
  ]);
  const parks: ParkOption[] = locationsResult.ok
    ? locationsResult.data.items.map((location) => ({ id: location.location_id, label: location.name }))
    : [];

  const fleetResult = fleetOwnResult ?? liveResult;
  const tabCounts: Partial<Record<HerdSignalsTab, number>> = {};
  if (fleetResult.ok) {
    const fleet = fleetResult.data.summary;
    tabCounts.live = fleet.tags_seen;
    tabCounts.animals = fleet.mapped_animals;
    // Tag Mapping lists every tag (mapped, unmapped and conflict), so its count is the same
    // tenant-wide tag total as Live Monitor's, not the mapped-only or unmapped-only subset.
    tabCounts.mapping = fleet.tags_seen;
    if (normalResult.ok) {
      tabCounts.alerts = Math.max(0, fleet.tags_seen - normalResult.data.summary.tags_seen);
    }
  }
  if (gatewaysResult.ok) tabCounts.gateways = gatewaysResult.data.gateways.length;

  return (
    <div className="herd-signals-page">
      {/* One shared pending-transition flag for the poller, the KPI cards, the filter bar and every
          pagination control on this tab — see herd-signals-nav-context.tsx for why a plain <Link>
          per control was the "clicking a filter reloads the whole page" defect. */}
      <HerdSignalsNavProvider>
        <div className="phead">
          <div>
            <div className="crumb">
              Herd Signals / <b>{TAB_LABEL[params.tab]}</b>
            </div>
            <h1>{copy(pageContract, "page.title", "Herd Signals")}</h1>
            <div className="sub">
              {copy(
                pageContract,
                "page.subtitle",
                "BLE ear-tag signals, movement counters, and gateway coverage for mapped animals. Values are read from the tag broadcast — the tag reports a cumulative motion counter, not behaviour.",
              )}
            </div>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          {liveResult.ok ? <HerdSignalsPoller generatedAt={new Date(nowMs).toISOString()} /> : null}
        </div>

        <div className="segs">
          {HERD_SIGNALS_TABS.map((tab) => (
            <Link key={tab} href={herdSignalsHref(params, { hs_tab: tab === "live" ? undefined : tab })} className={params.tab === tab ? "on" : undefined}>
              <svg className="ic sm" viewBox="0 0 24 24" aria-hidden="true">
                {TAB_ICON[tab]}
              </svg>
              {TAB_LABEL[tab]}
              {tabCounts[tab] !== undefined ? <span className="cnt">{tabCounts[tab]}</span> : null}
            </Link>
          ))}
        </div>

        {params.tab === "live" ? (
          <LiveMonitorTab params={params} result={liveResult} nowMs={nowMs} parks={parks} />
        ) : params.tab === "animals" ? (
          <FilteredTableTab params={params} result={liveResult} nowMs={nowMs} title="Mapped animals" note="One row per animal carrying an active smart-tag-capable identifier" />
        ) : params.tab === "mapping" ? (
          <MappingTab params={params} result={liveResult} />
        ) : params.tab === "alerts" ? (
          <AlertsTab params={params} result={liveResult} nowMs={nowMs} alertingTotal={tabCounts.alerts} />
        ) : params.tab === "gateways" ? (
          <GatewaysTab result={gatewaysResult} nowMs={nowMs} />
        ) : (
          <InsightsTab result={insightsResult} />
        )}
      </HerdSignalsNavProvider>
    </div>
  );
}

function ReadFailed({ message, retryHref }: { message: string; retryHref: string }) {
  return (
    <div className="empty dngstate">
      <div className="eicon">
        <svg className="ic" viewBox="0 0 24 24">
          <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
          <path d="M12 9v4" />
          <path d="M12 17h.01" />
        </svg>
      </div>
      <h4>The read failed</h4>
      <p>{message}</p>
      <div className="eact">
        <Link href={retryHref} className="btn sm">
          Retry
        </Link>
      </div>
    </div>
  );
}

function LiveMonitorTab({
  params,
  result,
  nowMs,
  parks,
}: {
  params: HerdSignalsParams;
  result: ApiResult<HerdSignalsLiveResponse>;
  nowMs: number;
  parks: ParkOption[];
}) {
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref={herdSignalsHref(params, {})} />;
  const { summary, items, next_cursor } = result.data;
  const sheds: ShedOption[] = Array.from(
    new Map(items.filter((item) => item.shed_id && item.shed_name).map((item) => [item.shed_id as string, item.shed_name as string])).entries(),
  ).map(([id, label]) => ({ id, label }));

  return (
    <>
      <HerdSignalsFilters params={params} sheds={sheds} parks={parks} />
      <HerdSignalsKpis summary={summary} params={params} />
      <div className="small faint" style={{ margin: "-6px 0 14px" }}>
        Counts are whole-filter aggregates computed by the backend from the same tenant-scoped query
        as the table — never summed from the rows on the fetched page.
      </div>
      <div className="card">
        <div className="hd">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M4.9 19.1a10 10 0 0 1 0-14.2" />
            <path d="M7.8 16.2a6 6 0 0 1 0-8.4" />
            <circle cx="12" cy="12" r="2" />
            <path d="M16.2 7.8a6 6 0 0 1 0 8.4" />
            <path d="M19.1 4.9a10 10 0 0 1 0 14.2" />
          </svg>
          <h3>Live tag signals</h3>
          <span className="tag t-mut">{summary.tags_seen} tags</span>
          <div className="sp" style={{ flex: 1 }} />
          <span className="small faint">Click a row for tag detail</span>
        </div>
        <div className="bd flush">
          <HerdSignalsTable items={items} nextCursor={next_cursor} params={params} nowMs={nowMs} tagsSeen={summary.tags_seen} />
        </div>
      </div>
    </>
  );
}

function FilteredTableTab({
  params,
  result,
  nowMs,
  title,
  note,
}: {
  params: HerdSignalsParams;
  result: ApiResult<HerdSignalsLiveResponse>;
  nowMs: number;
  title: string;
  note: string;
}) {
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref={herdSignalsHref(params, {})} />;
  const { items, next_cursor, summary } = result.data;
  // "No mapped animals yet" is an honest, expected state (docs/modules/herd-signals.md "Unmapped
  // tags are the NORMAL state") -- on staging today NO tag is mapped, so this branch is the common
  // case, not an error and not the generic "no gateway packets" empty (that would be a false claim
  // when the gateway IS posting and simply nothing is mapped yet).
  if (items.length === 0 && !params.hasFilter && summary.mapped_animals === 0) {
    return (
      <div className="card">
        <div className="hd">
          <h3>{title}</h3>
        </div>
        <div className="bd flush">
          <div className="empty">
            <div className="eicon">
              <svg className="ic" viewBox="0 0 24 24">
                <path d="M21 10c0 7-9 12-9 12s-9-5-9-12a9 9 0 0 1 18 0Z" />
                <circle cx="12" cy="10" r="3" />
              </svg>
            </div>
            <h4>No mapped animals yet</h4>
            <p>
              {summary.tags_seen > 0
                ? `${summary.tags_seen.toLocaleString("en-IN")} smart tag(s) are broadcasting, but none carry an active smart-tag-capable identifier yet.`
                : "No BLE gateway has posted for this tenant yet."}{" "}
              Map a tag to an animal identifier in Tag Mapping to see it here.
            </p>
            <div className="eact">
              <Link href={herdSignalsHref(params, { hs_tab: "mapping" })} className="btn sm">
                Go to Tag Mapping
              </Link>
            </div>
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="card">
      <div className="hd">
        <h3>{title}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small faint">{note}</span>
      </div>
      <div className="bd flush">
        <HerdSignalsTable items={items} nextCursor={next_cursor} params={params} nowMs={nowMs} tagsSeen={summary.tags_seen} variant="animals" />
      </div>
    </div>
  );
}

function MappingTab({
  params,
  result,
}: {
  params: HerdSignalsParams;
  result: ApiResult<HerdSignalsLiveResponse>;
}) {
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref={herdSignalsHref(params, {})} />;
  const { items, next_cursor, summary } = result.data;
  // The mapping-state chips, the three write actions (MAP / REPLACE / UNMAP), the table and its
  // pagination all live in ONE client component: they share a selected row, and splitting the
  // toolbar off into this server component left the buttons unable to see what was selected.
  return <HerdSignalsMappingTable items={items} nextCursor={next_cursor} params={params} tagsSeen={summary.tags_seen} />;
}

function AlertsTab({
  params,
  result,
  nowMs,
  alertingTotal,
}: {
  params: HerdSignalsParams;
  result: ApiResult<HerdSignalsLiveResponse>;
  nowMs: number;
  // Tenant-wide count of alerting tags (server aggregate, computed in the board above). Used for
  // the "N of M" readout only -- never recomputed from the rows on this page.
  alertingTotal: number | undefined;
}) {
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref={herdSignalsHref(params, {})} />;
  // The mock has NO alert-type filter row: `renderAlerts` builds ONE `.rowlist` holding every
  // alerting tag, whatever its type. `fetchForTab` already asked the server for the whole
  // "not_normal" partition (pattern="not_normal", see herdSignalsLiveFilter in the Postgres
  // repository) -- every row on this page is already alerting, at any fleet scale, because the
  // filter runs in the WHERE clause, not after the page lands. Re-filtering here with the old
  // client-side `isAlerting` predicate would just be a no-op on top of an already-server-filtered
  // page, so `items` IS the alerting set.
  const { items, next_cursor } = result.data;
  const alerting = items;
  return (
    <div className="card">
      <div className="hd">
        <h3>Signal alerts</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small faint">Signal conditions only — none of these are clinical findings</span>
      </div>
      <div className="bd flush">
        {alerting.length === 0 ? (
          <div className="empty">
            <div className="eicon">
              <svg className="ic" viewBox="0 0 24 24">
                <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                <path d="M12 9v4" />
                <path d="M12 17h.01" />
              </svg>
            </div>
            <h4>No tags in an alert state</h4>
            <p>Every tag in scope is reading within thresholds — a healthy outcome, not an error.</p>
          </div>
        ) : (
          <>
            {/* The mock renders Alerts as a `.rowlist`, not as the shared live table: one row per
                signal condition, severity chip first, the explanation in prose, shed right-aligned.
                A table here re-states fourteen telemetry columns the reader did not ask for. */}
            <div className="rowlist">
              {alerting.map((item) => (
                <AlertRow key={item.tag_id} item={item} nowMs={nowMs} />
              ))}
            </div>
            {next_cursor ? (
              <div className="pager">
                <Link href={herdSignalsHref(params, { hs_cursor: next_cursor })} className="pgbtn">
                  Next &rarr;
                </Link>
                <span>
                  Showing <b>{alerting.length.toLocaleString("en-IN")}</b>
                  {alertingTotal !== undefined ? (
                    <>
                      {" of "}
                      <b>{alertingTotal.toLocaleString("en-IN")}</b>
                    </>
                  ) : null}{" "}
                  alerting tags in scope
                </span>
              </div>
            ) : null}
          </>
        )}
      </div>
    </div>
  );
}

// One alert row. The severity chip is the pattern's own approved tone/label (features/herd-signals/
// format.ts) — this row never invents a synonym or a severity of its own.
function AlertRow({ item, nowMs }: { item: HerdSignalItem; nowMs: number }) {
  const pattern = item.pattern_state ?? "normal";
  // Missing signal names the GATEWAY before the animal, always: the reader must check the radio
  // path first, and the row must not read as "this animal is missing".
  const missing = pattern === "missing" || item.movement_state === "stale";
  const explanation = missing
    ? `Gateway ${item.gateway_id ?? "coverage for this tag"} last delivered a packet for this tag ${fmtAgo(item.last_seen_at, nowMs)}. Missing signal — never a missing animal. Check the gateway before checking the animal.`
    : `${PATTERN_WHY[pattern].charAt(0).toUpperCase()}${PATTERN_WHY[pattern].slice(1)}.`;
  const location = item.operational_location_display ?? item.shed_name ?? item.park_name ?? "—";
  return (
    <div className="rowitem">
      <Tag tone={missing ? PATTERN_TONE.missing : PATTERN_TONE[pattern]}>
        {missing ? PATTERN_LABEL.missing : PATTERN_LABEL[pattern]}
      </Tag>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div className="rt">
          {item.display_id ?? "No animal mapped to this tag"} <span className="mono faint">{item.tag_id}</span>
        </div>
        <div className="rs">{explanation}</div>
      </div>
      <span className="faint small">{location}</span>
    </div>
  );
}

function GatewaysTab({ result, nowMs }: { result: ApiResult<HerdGatewaysResponse> | null; nowMs: number }) {
  if (!result) return null;
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref="/herd-signals?hs_tab=gateways" />;
  return <HerdSignalsGateways gateways={result.data.gateways} nowMs={nowMs} />;
}

function InsightsTab({ result }: { result: ApiResult<HerdInsightsResponse> | null }) {
  if (!result) return null;
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref="/herd-signals?hs_tab=insights" />;
  return <HerdSignalsInsights cards={result.data.cards} />;
}
