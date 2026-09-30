import { listOrEmpty } from "@/lib/list-or-empty";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { FilterCardSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { HerdSignalsLivePanelSkeleton, HerdSignalsLiveTabSkeleton } from "./herd-signals-skeletons";
import type { ReactNode } from "react";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import { DividedStack } from "@/components/app/divided-stack";
import { Label } from "@/components/minimal/label";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import Link from "@/components/no-prefetch-link";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { TemplateTabs } from "@/components/app/template-tabs";
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
import type { ApiResult } from "@/lib/api/server";
import { HerdSignalsStreamBridge } from "./herd-signals-stream-bridge";
import { HerdSignalsNavProvider } from "./herd-signals-nav-context";
import { HerdSignalsKpis } from "./herd-signals-kpis";
import { HerdSignalsFilters, type ShedOption } from "./herd-signals-filters";
import { HerdSignalsTable } from "./herd-signals-table";
import { HerdSignalsMappingTable } from "./herd-signals-mapping-table";
import { HerdSignalsGateways } from "./herd-signals-gateways";
import { HerdSignalsInsights } from "./herd-signals-insights";
import { Tag, type Tone } from "@/components/ui-primitives";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import { RISK_LABEL, RISK_TONE } from "./format";
import { HERD_SIGNALS_PAGE_SX } from "./herd-signals-layout";
import { HERD_SIGNALS_TABS, LIMIT_DEFAULT, herdSignalsHref, kpiToLiveState, kpiToMovementState, parseHerdSignalsParams, type HerdSignalsParams, type HerdSignalsTab } from "./params";

const TAB_LABEL: Record<HerdSignalsTab, string> = {
  live: "Live Monitor",
  animals: "Animals",
  gateways: "Gateways",
  alerts: "Alerts",
  mapping: "Tag Mapping",
  insights: "Insights",
};

// One glyph per tab (template Iconify solar set). Labels alone made the six tabs a wall of
// same-weight text; the icon is what lets the eye find "Gateways" without reading the row.
const TAB_ICON: Record<HerdSignalsTab, IconifyName> = {
  live: "ic:baseline-bluetooth",
  animals: "mingcute:location-fill",
  gateways: "ic:baseline-wifi",
  alerts: "solar:danger-triangle-bold",
  mapping: "eva:link-2-fill",
  insights: "solar:chart-square-outline",
};

// What the Alerts tab lists: the practical Watchlist shortlist. That is the cross-signal score from
// the backend: own baseline, same-pen comparison, tag-temperature deviation, persistent pattern and
// sensor health. A resting tag with zero 15-minute movement is not automatically an alert.

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
    sort: params.sort,
    sortDir: params.sortDir,
    riskState: params.risk,
  };
  if (params.tab === "animals") {
    return getHerdSignalsLive({ ...common, mappingState: "mapped", movementState: params.movementState, pattern: params.pattern });
  }
  if (params.tab === "mapping") {
    return getHerdSignalsLive({ ...common, mappingState: params.mappingState });
  }
  if (params.tab === "alerts") {
    return getHerdSignalsLive({ ...common, riskState: "attention" });
  }
  // live tab
  const liveState = kpiToLiveState(params.kpi);
  return getHerdSignalsLive({
    ...common,
    movementState: liveState ? undefined : (kpiToMovementState(params.kpi) ?? params.movementState),
    liveState,
    mappingState: params.mappingState,
    pattern: params.pattern,
  });
}

function herdSignalsLiveStreamKey(params: HerdSignalsParams): string {
  const out = new URLSearchParams();
  if (params.parkId) out.set("park_id", params.parkId);
  if (params.shedId) out.set("shed_id", params.shedId);
  if (params.q) out.set("q", params.q);
  if (params.cursor) out.set("cursor", params.cursor);
  if (params.sort) out.set("sort", params.sort);
  if (params.sortDir) out.set("dir", params.sortDir);
  if (params.limit) out.set("limit", String(params.limit));
  if (params.tab === "live") {
    const liveState = kpiToLiveState(params.kpi);
    const movementState = kpiToMovementState(params.kpi) ?? params.movementState;
    if (liveState) {
      out.set("live_state", liveState);
    } else if (movementState) {
      out.set("movement_state", movementState);
    }
    if (params.mappingState) out.set("mapping_state", params.mappingState);
    if (params.pattern) out.set("pattern", params.pattern);
    if (params.risk) out.set("risk_state", params.risk);
  } else if (params.tab === "animals") {
    out.set("mapping_state", "mapped");
    if (params.movementState) out.set("movement_state", params.movementState);
    if (params.pattern) out.set("pattern", params.pattern);
    if (params.risk) out.set("risk_state", params.risk);
  } else if (params.tab === "mapping") {
    if (params.mappingState) out.set("mapping_state", params.mappingState);
    if (params.risk) out.set("risk_state", params.risk);
  } else if (params.tab === "alerts") {
    out.set("risk_state", "attention");
  }
  return out.toString();
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

  // Pulled out of the Promise.all array below (rather than inlined as a second literal
  // getHerdSignalsLive(...) call in that array) so the two scope-summary reads stay textually
  // distinct: this one is the conditional "does the active tab already narrow its own summary"
  // read, the other is the always-on tenant-wide "normal pattern" read below. Same two bounded,
  // O(1) aggregate calls as before -- this is a request-plan-fanout false positive on same-name
  // literal matching, not an actual overlapping/duplicate request, so it is restructured rather
  // than suppressed with an ignore comment.
  const scopeSummaryPromise = tabNarrowsSummary ? getHerdSignalsLive(scopeOnly) : Promise.resolve(null);

  const [liveResult, fleetOwnResult, alertsResult, gatewaysResult, insightsResult] = await Promise.all([
    fetchForTab(params),
    scopeSummaryPromise,
    getHerdSignalsLive({ ...scopeOnly, riskState: "attention" }),
    getHerdSignalsGateways(),
    params.tab === "insights" ? getHerdSignalsInsights() : Promise.resolve(null),
  ]);

  const fleetResult = fleetOwnResult ?? liveResult;
  const tabCounts: Partial<Record<HerdSignalsTab, number>> = {};
  if (fleetResult.ok) {
    const fleet = fleetResult.data.summary;
    tabCounts.live = fleet.tags_seen;
    tabCounts.animals = fleet.mapped_animals;
    // Tag Mapping lists every tag (mapped, unmapped and conflict), so its count is the same
    // tenant-wide tag total as Live Monitor's, not the mapped-only or unmapped-only subset.
    tabCounts.mapping = fleet.tags_seen;
    if (alertsResult.ok) tabCounts.alerts = alertsResult.data.summary.tags_seen;
  }
  if (gatewaysResult.ok) tabCounts.gateways = listOrEmpty(gatewaysResult.data.gateways).length;

  return (
    // Page column: header, tab strip, tab body on the page's 24px rhythm (gap 3).
    <Box sx={HERD_SIGNALS_PAGE_SX}>
      {/* One shared pending-transition flag for the stream bridge, the KPI cards, the filter bar and every
          pagination control on this tab — see herd-signals-nav-context.tsx for why a plain <Link>
          per control was the "clicking a filter reloads the whole page" defect. */}
      <HerdSignalsNavProvider>
        <PageHeader
          title={copy(pageContract, "page.title", "Herd Signals")}
          crumbs={[{ label: copy(pageContract, "crumb", "Herd Signals") }, { label: TAB_LABEL[params.tab] }]}
          actions={
            <>
              {liveResult.ok ? <HerdSignalsStreamBridge generatedAt={new Date(nowMs).toISOString()} /> : null}
            </>
          }
          tabs={
            <>
            <TemplateTabs
              ariaLabel="Herd Signals views"
              countTone="brand"
              value={params.tab}
              items={HERD_SIGNALS_TABS.map((tab) => ({
                value: tab,
                label: TAB_LABEL[tab],
                icon: <Iconify icon={TAB_ICON[tab]} width={18} aria-hidden="true" />,
                count: tabCounts[tab] !== undefined ? tabCounts[tab] : undefined,
                href: herdSignalsHref(params, { hs_tab: tab === "live" ? undefined : tab }),
              }))}
            />
            </>
          }
        />

        {/* The tab body (guard: url-keyed-panel): a tab click shows the clicked tab's skeleton in the
            same frame; header and strip stay on screen. */}
        <UrlSuspense searchParams={searchParams ?? {}} watch={["hs_tab"]} fallback={TAB_SKELETON[params.tab] ?? TAB_SKELETON.live} fallbackBy={{ param: "hs_tab", shapes: { ...TAB_SKELETON, "": TAB_SKELETON.live } }}>
        {params.tab === "live" ? (
          <LiveMonitorTab params={params} result={liveResult} nowMs={nowMs} searchParams={searchParams ?? {}} />
        ) : (
          <UrlSuspense searchParams={searchParams ?? {}} watch={[ALL_PARAMS]} fallback={TAB_SKELETON[params.tab] ?? TAB_SKELETON.live}>
        {params.tab === "animals" ? (
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
          </UrlSuspense>
        )}
        </UrlSuspense>
      </HerdSignalsNavProvider>
    </Box>
  );
}

function ReadFailed({ message, retryHref }: { message: string; retryHref: string }) {
  return (
    <Alert
      severity="error"
      action={
        <Button component={Link} href={retryHref} color="inherit" size="small">
          Retry
        </Button>
      }
    >
      <AlertTitle>The read failed</AlertTitle>
      {message}
    </Alert>
  );
}

function LiveMonitorTab({
  params,
  result,
  nowMs,
  searchParams,
}: {
  params: HerdSignalsParams;
  result: ApiResult<HerdSignalsLiveResponse>;
  nowMs: number;
  searchParams: RouteSearchParams;
}) {
  if (!result.ok) return <ReadFailed message={result.error.message} retryHref={herdSignalsHref(params, {})} />;
  const { summary, items, next_cursor } = result.data;
  const liveKey = herdSignalsLiveStreamKey(params);
  const sheds: ShedOption[] = Array.from(
    new Map(items.filter((item) => item.shed_id && item.shed_name).map((item) => [item.shed_id as string, item.shed_name as string])).entries(),
  ).map(([id, label]) => ({ id, label }));

  return (
    <>
      <HerdSignalsFilters params={params} sheds={sheds} />
      {/* KPIs + table (guard: url-keyed-panel): a filter / KPI / sort / page click swaps them to their
          skeleton at once; the filter bar stays on screen. */}
      <UrlSuspense searchParams={searchParams} watch={[ALL_PARAMS]} fallback={<HerdSignalsLivePanelSkeleton />}>
      <HerdSignalsKpis summary={summary} params={params} liveKey={liveKey} />
      {/* Template table card: CardHeader (title + count Label). No developer note as subheader
          (TR2-P2-9; guard: herd-signals-no-dev-note): the whole-filter aggregate rule is code
          behaviour, not something the reader acts on. */}
      <Card sx={{ mt: 3 }}>
        <CardHeader
          title={
            <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
              Live tag signals
              <Label variant="soft">{summary.tags_seen} tags</Label>
            </Box>
          }
          sx={{ mb: 2 }}
        />
        <HerdSignalsTable items={items} nextCursor={next_cursor} params={params} nowMs={nowMs} tagsSeen={summary.tags_seen} liveKey={liveKey} />
      </Card>
      </UrlSuspense>
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
  const liveKey = herdSignalsLiveStreamKey(params);
  // "No mapped animals yet" is an honest, expected state (docs/modules/herd-signals.md "Unmapped
  // tags are the NORMAL state") -- on staging today NO tag is mapped, so this branch is the common
  // case, not an error and not the generic "no gateway packets" empty (that would be a false claim
  // when the gateway IS posting and simply nothing is mapped yet).
  if (items.length === 0 && !params.hasFilter && summary.mapped_animals === 0) {
    return (
      <Card>
        <CardHeader title={title} sx={{ mb: 2 }} />
        <Box>
          <EmptyState
            title="No mapped animals yet"
            description={<>{summary.tags_seen > 0
              ? `${summary.tags_seen.toLocaleString("en-IN")} smart tag(s) are broadcasting, but none carry an active smart-tag-capable identifier yet.`
              : "No BLE gateway has posted for this tenant yet."}{" "}
              Map a tag to an animal identifier in Tag Mapping to see it here.</>}
            action={
              <Button component={Link} href={herdSignalsHref(params, { hs_tab: "mapping" })} variant="outlined" color="inherit" size="small">
                Go to Tag Mapping
              </Button>
            }
          />
        </Box>
      </Card>
    );
  }
  return (
    <Card>
      <CardHeader title={title} subheader={note} sx={{ mb: 2 }} />
      <HerdSignalsTable items={items} nextCursor={next_cursor} params={params} nowMs={nowMs} tagsSeen={summary.tags_seen} liveKey={liveKey} variant="animals" />
    </Card>
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
  // fetchForTab already asked the backend for risk_state=attention, so every row here is a scored
  // Watchlist animal. Do not derive this from the currently fetched Live Monitor page.
  const { items, next_cursor } = result.data;
  return (
    <Card>
      <CardHeader title="Tags needing attention" subheader="Shortlist only — confirm with clinical checks before action" sx={{ mb: 1 }} />
      <Box>
        {items.length === 0 ? (
          <EmptyState
            title="No tags need attention right now"
            description="No smart tag is unusual against its own baseline or its pen group in this scope."
          />
        ) : (
          <>
            <DividedStack sx={{ px: 3, py: 1 }}>
              {items.map((item) => (
                <AlertRow key={item.tag_id} item={item} />
              ))}
            </DividedStack>
            {next_cursor ? (
              <Box sx={{ px: 3, py: 2, display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap", borderTop: 1, borderColor: "divider", borderTopStyle: "dashed", typography: "body2", color: "text.secondary" }}>
                <Button component={Link} href={herdSignalsHref(params, { hs_cursor: next_cursor })} variant="outlined" color="inherit" size="small">
                  Next &rarr;
                </Button>
                <span>
                  Showing <b>{items.length.toLocaleString("en-IN")}</b>
                  {alertingTotal !== undefined ? (
                    <>
                      {" of "}
                      <b>{alertingTotal.toLocaleString("en-IN")}</b>
                    </>
                  ) : null}{" "}
                  tags needing attention
                </span>
              </Box>
            ) : null}
          </>
        )}
      </Box>
    </Card>
  );
}

function readableRiskReason(reason: string): string {
  const normalized = reason.trim().toLowerCase();
  if (normalized === "motion far below own baseline") return "Movement is far below this animal's normal baseline.";
  if (normalized === "motion spike vs own baseline") return "Movement is much higher than this animal's normal baseline.";
  if (normalized === "motion lower than pen group") return "Movement is lower than nearby animals in the same pen.";
  if (normalized === "tag temperature high vs pen group") return "Tag is warmer than the pen group average.";
  if (normalized === "persistent abnormal activity") return "Abnormal movement pattern has persisted.";
  if (normalized === "activity pattern needs watch") return "Movement pattern needs a watch check.";
  if (normalized === "sensor abnormal") return "Tag sensor health is abnormal.";
  return reason;
}

function AlertRow({ item }: { item: HerdSignalItem }) {
  const location = item.operational_location_display ?? item.shed_name ?? item.park_name ?? "—";
  const label = item.risk_state ? RISK_LABEL[item.risk_state] : "Needs review";
  const tone = item.risk_state ? RISK_TONE[item.risk_state] : "warn";
  const reasons = item.risk_reasons?.length ? item.risk_reasons.map(readableRiskReason).join("; ") : "Baseline or group comparison changed enough to review.";
  return (
    // Template list row: status Label, subtitle2 + body2 secondary, caption on the right.
    <Box sx={{ py: 2, display: "flex", alignItems: "flex-start", gap: 2, flexWrap: { xs: "wrap", sm: "nowrap" } }}>
      <Tag tone={tone}>{label}</Tag>
      <Box sx={{ flex: "1 1 240px", minWidth: 0 }}>
        <Typography variant="subtitle2">
          {item.display_id ?? "No animal mapped to this tag"}{" "}
          <Box component="span" sx={{ fontFamily: "monospace", color: "text.disabled", fontWeight: "fontWeightRegular" }}>
            {item.tag_id}
          </Box>
        </Typography>
        <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
          {reasons}
        </Typography>
      </Box>
      <Typography variant="caption" sx={{ color: "text.disabled", whiteSpace: "nowrap" }}>
        {location}
      </Typography>
    </Box>
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

/** Each tab's body skeleton, from the shared blocks. */
const TAB_SKELETON: Record<string, ReactNode> = {
  live: <HerdSignalsLiveTabSkeleton />,
  animals: <TableSkeleton columns={12} rows={LIMIT_DEFAULT} toolbar={<FilterCardSkeleton inCard fields={["search", 180, 180]} />} />,
  mapping: <TableSkeleton columns={10} rows={LIMIT_DEFAULT} toolbar={<FilterCardSkeleton inCard fields={["search", 180, 180]} />} />,
  alerts: <TableSkeleton columns={12} rows={LIMIT_DEFAULT} toolbar={<FilterCardSkeleton inCard fields={["search", 180]} />} />,
  gateways: <TableSkeleton columns={6} rows={8} />,
  insights: <PanelSkeleton kpis={4} charts={2} />,
};
