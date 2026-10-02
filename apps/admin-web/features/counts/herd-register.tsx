import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { HERD_HEADER_LAYOUT, HERD_KPI_SIZE } from "./counts-layout";
import { HerdKpiSkeleton } from "./counts-skeletons";
import { TableSkeleton } from "@/components/app/skeletons";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Box from "@mui/material/Box";
import { listOrEmpty } from "@/lib/list-or-empty";
import { randomUUID } from "node:crypto";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";

import Card from "@mui/material/Card";
import Avatar from "@mui/material/Avatar";
import { UrlTabs } from "@/components/app/url-tabs";
import type { KitTone } from "@/lib/tone";
import { PageHeader } from "@/components/app/page-header";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import { EmptyContent } from "@/components/minimal/empty-content";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { UrlSortHead } from "@/components/app/table";
import { KpiWidget, kpiColor } from "@/components/app/kpi-widget";
import { GoatGlyph } from "@/components/goat-glyph";
import { DenseTable } from "@/components/dense-table";
import { dash, humanizeEnum } from "@/lib/format";
import { actionFeedbackCopy, copy, optionalOption, readableOptionKey, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getHerdRegisterSummary,
  listAnimalStages,
  searchGoats,
  type GoatSearchResponse,
  type HerdRegisterSummaryResponse,
} from "@/lib/api/server";
import { getHerdRegisterLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import {
  boundedInt,
  hrefPreviousCursor,
  hrefWithCursor,
  hrefWithoutAction,
  one,
  type RouteSearchParams,
} from "@/lib/search-params";
import { HerdActions, type HerdAnimalStageOption, type HerdOperationalLocationOption } from "./herd-actions-ui";
import { HerdFiltersModalClient } from "./herd-filters-modal-client";
import { HerdPassportLocalDrawer, type HerdPassportDrawerItem } from "./herd-passport-local-drawer";
import { operationalLocationLabel } from "@/lib/operational-location";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";
import { TAP_MIN } from "@/theme/tap-target";

/** A status chip's words: the tenant's own label from the page's option group, never the stored key. */
function statusLabel(pageContract: AdminUiPageContract, groupId: string, value: string | null | undefined): string {
  if (!value) return dash(value);
  return optionalOption(pageContract, groupId, value)?.label ?? readableOptionKey(value);
}

/** A status cell: a soft Label for a real status; an unset one is the plain muted dash every other
 *  empty cell uses, not a grey Label box (TR2-P2-1; guard: herd-status-dash-plain). */
function StatusCell({ value, color, label }: { value: string | null | undefined; color: LabelColor; label: string }) {
  if (!value) return <Box component="span" sx={{ color: "text.disabled" }}>{label}</Box>;
  return <Label variant="soft" color={color}>{label}</Label>;
}

// Counts -> Herd Register. The vaccination cascade's real business entry point: register/import a goat,
// emit goat.created, generate vaccination obligations. This screen is the OPERATIONAL Counts module surface.
// KPI summary cards paginate through all /goats/search rows in scope (100 per page) for exact totals.
// Kid vs adult uses goat age_band and management_stage from the API (K1/K2/... stages), not shed names.
//
// Generated-client status: goat READ and WRITE operation IDs are present and wired. The herd table +
// filters read /goats/search; Register goat and Import sheet open real drawers that post createAdminGoat /
// bulk preview+commit (see herd-actions.ts / herd-actions-ui.tsx). No hand-rolled DTOs, no fake rows.
// New report has no API, so it is not rendered (no dead controls; TR2-P1-8: the header is one primary + ⋮).

const DEFAULT_PAGE_SIZE = 10;

type GoatRow = GoatSearchResponse["items"][number];
type HerdSummaryRow = HerdRegisterSummaryResponse["items"][number] & Record<string, unknown>;

function summaryNumber(row: HerdSummaryRow, camelKey: string, snakeKey: string): number {
  const value = row[camelKey] ?? row[snakeKey];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function hrefWithDrawerParam(pathname: string, params: RouteSearchParams, key: string, value: string | null): string {
  const next = new URLSearchParams();
  for (const [paramKey, paramValue] of Object.entries(params)) {
    if (paramKey === key) continue;
    if (Array.isArray(paramValue)) {
      for (const item of paramValue) if (item) next.append(paramKey, item);
    } else if (paramValue) {
      next.set(paramKey, paramValue);
    }
  }
  if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

const TONE_COLOR = { ok: "success", warn: "warning", dng: "error", info: "info", mut: "default" } as const;

function statusTone(value: string | null | undefined, kind: "lifecycle" | "health" | "breeding"): "ok" | "warn" | "dng" | "info" | "mut" {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (kind === "health") {
    if (["healthy", "normal", "ok"].includes(v)) return "ok";
    if (["sick", "critical", "dead"].includes(v)) return "dng";
    if (v.includes("treatment") || v.includes("watch") || v.includes("quarantine")) return "warn";
    return "info";
  }
  if (kind === "breeding") {
    if (v.includes("pregnant") || v.includes("lactating") || v.includes("ai")) return "info";
    if (v.includes("open") || v.includes("none")) return "mut";
    return "ok";
  }
  if (["alive", "active"].includes(v)) return "ok";
  if (["sold", "died", "culled", "lost", "inactive"].includes(v)) return "dng";
  return "mut";
}

function locationLabel(g: GoatRow, part: "park" | "shed"): string {
  const path = g.location_path;
  if (part === "park") return path.park_code ?? path.park_name ?? "—";
  const shedName = path.shed_name ?? path.shed_code ?? "";
  if (!shedName) return "—";
  return (
    path.operational_location_display ||
    operationalLocationLabel({
      shedName,
      partitionLabel: path.partition_label,
    })
  );
}

function weightLabel(weight: number | null | undefined): string {
  return typeof weight === "number" && Number.isFinite(weight) ? `${weight.toFixed(weight % 1 === 0 ? 0 : 1)}` : "—";
}

// KPIs come from canonical scoped goat counts. `summary === null` means the
// API read failed: show an honest dash, never fabricate a fallback.

function buildHerdSummary(pageContract: AdminUiPageContract, summary: HerdRegisterSummaryResponse | null) {
  const totals = summary
    ? summary.items.reduce(
        (acc, row) => ({
          total: acc.total + summaryNumber(row, "totalCount", "total_count"),
          active: acc.active + summaryNumber(row, "activeCount", "active_count"),
          adult: acc.adult + summaryNumber(row, "adultCount", "adult_count"),
          kid: acc.kid + summaryNumber(row, "kidCount", "kid_count"),
          untaggedKid: acc.untaggedKid + summaryNumber(row, "untaggedKidCount", "untagged_kid_count"),
          dead: acc.dead + summaryNumber(row, "deadCount", "dead_count"),
          sold: acc.sold + summaryNumber(row, "soldCount", "sold_count"),
          culled: acc.culled + summaryNumber(row, "culledCount", "culled_count"),
        }),
        { total: 0, active: 0, adult: 0, kid: 0, untaggedKid: 0, dead: 0, sold: 0, culled: 0 },
      )
    : null;
  // A real number renders as a count-up tile; an unavailable read stays a dash.
  const fmt = (value: number | undefined): number | null => (totals && typeof value === "number" ? value : null);
  const unavailable = copy(pageContract, "section.summary.unavailable");
  // The register's lifecycle counts are the template user list's status tabs (Label counts), not
  // tiles: each tab IS the `?status=` filter of the table below it (backend lifecycle_status).
  // An unavailable summary read leaves the tabs without a count rather than printing 0.
  const tabs: { value: HerdStatusTab; label: string; count: number | undefined; color: "default" | "success" | "error" | "secondary" | "warning" }[] = [
    { value: "all", label: copy(pageContract, "filter.status.all"), count: totals?.total, color: "default" },
    { value: "alive", label: copy(pageContract, "label.active"), count: totals?.active, color: "success" },
    { value: "dead", label: copy(pageContract, "label.dead"), count: totals?.dead, color: "error" },
    { value: "sold", label: copy(pageContract, "label.sold"), count: totals?.sold, color: "secondary" },
    { value: "culled", label: copy(pageContract, "label.culled"), count: totals?.culled, color: "warning" },
  ];
  // The live-herd make-up the status tabs cannot say: three template tiles (Ecommerce 3-up widths).
  // No weekly series exists for these register totals, so they stay CourseWidgetSummary (DECIDED KPI rule).
  const cards = [
    { label: copy(pageContract, "label.adults"), value: fmt(totals?.adult), sub: totals ? copy(pageContract, "label.live_scoped_register") : unavailable, tone: "info" as KitTone },
    { label: copy(pageContract, "label.kids"), value: fmt(totals?.kid), sub: totals ? copy(pageContract, "label.stage_shed_inferred") : unavailable, tone: "info" as KitTone },
    { label: copy(pageContract, "label.untagged_kids"), value: fmt(totals?.untaggedKid), sub: totals ? copy(pageContract, "label.identity") : unavailable, tone: "warning" as KitTone },
  ];
  return { tabs, cards };
}

/** The status tabs: `?status=` values the backend's lifecycle_status filter takes ("all" = no filter). */
const HERD_STATUS_TABS = ["all", "alive", "dead", "sold", "culled"] as const;
type HerdStatusTab = (typeof HERD_STATUS_TABS)[number];
const DEFAULT_STATUS_TAB: HerdStatusTab = "alive";


/** Template user list table: minWidth 960 in the card's Scrollbar, cells on one line; every cell is
 * the row's link to the passport drawer (full cell height, 44px tap box, reads as plain text). */
const HERD_TABLE_SX = {
  minWidth: 960,
  "& th, & td": { whiteSpace: "nowrap", overflowWrap: "normal", wordBreak: "normal" },
  "& td a": { color: "inherit", textDecoration: "none" },
  "& td > a": { display: "flex", alignItems: "center", minHeight: TAP_MIN },
} as const;

export async function HerdRegisterPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const pathname = "/counts/herd";

  // Top bar owns park/as-of scope. parseScope is the single sanctioned reader; backendScope maps it to the
  // params the API actually honors. searchGoats honors park_id; it has no as_of param, so as_of is preserved
  // in the URL for the top bar but not sent here.
  const scope = parseScope(sp);
  const { parkId } = backendScope(scope);

  const q = one(sp, "q");
  const breed = one(sp, "breed");
  const sex = one(sp, "sex");
  // The status tab (template user-list Tabs). The register opens on the live herd, as it always has.
  const statusParam = one(sp, "status");
  const statusTab: HerdStatusTab = (HERD_STATUS_TABS as readonly string[]).includes(statusParam ?? "") ? (statusParam as HerdStatusTab) : DEFAULT_STATUS_TAB;
  const status = statusTab === "all" ? undefined : statusTab;
  // The one server sort the register has: Display ID, the keyset the cursor pages on (`?order=desc`).
  const order: "asc" | "desc" = one(sp, "order") === "desc" ? "desc" : "asc";
  const pageSizeOptions = tablePageSizes(pageContract, "herd-register");
  const requestedLimit = Number(one(sp, "limit"));
  const pageSize = pageSizeOptions.includes(requestedLimit) ? requestedLimit : DEFAULT_PAGE_SIZE;
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const hasFilter = Boolean(q || breed || sex);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  // The backend's own reason for a refused save, shown beneath the banner (herd-actions.ts).
  const actionDetail = one(sp, "action_detail") ?? "";
  const returnTo = hrefWithoutAction(pathname, sp);
  const selectedGoatId = one(sp, "goat_passport");

  // Real goats + real location options for the write drawers, in parallel.
  const [result, summaryResult, locations, stagesResult] = await Promise.all([
    searchGoats({ limit: pageSize, cursor, q, breed, sex, park_id: parkId, status, ...(order === "desc" ? { order } : {}) }),
    getHerdRegisterSummary({ park_id: parkId, breed, sex }),
    getHerdRegisterLocations(),
    listAnimalStages(),
  ]);
  const authError = firstAuthRequiredError(result, summaryResult, stagesResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // A fresh idempotency key per render: a double-submit of the open Register drawer replays the same key
  // (backend returns the original goat); a reload mints a new key for a new logical create.
  const registerIdempotencyKey = randomUUID();
  // Dedicated key for the reproductive edit drawer so it never shares/replays the register create key.
  const reproductiveIdempotencyKey = randomUUID();
  const animalStages: HerdAnimalStageOption[] = stagesResult.ok
    ? listOrEmpty(stagesResult.data.items).map((stage) => ({
        code: stage.stage_code,
        label: stage.name ? `${stageLabel(stage.stage_code)} · ${stage.name}` : stageLabel(stage.stage_code),
      }))
    : [];
  const operationalLocations: HerdOperationalLocationOption[] = locations.operationalLocations;

  const goats: GoatRow[] = result.ok ? listOrEmpty(result.data.items) : [];
  // Honest state: an unavailable summary read shows a dash, not fabricated numbers.
  const { tabs: statusTabs, cards: summaryCards } = buildHerdSummary(pageContract, summaryResult.ok ? summaryResult.data : null);
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const { cursor: _cursor, page: _page, cursor_stack: _stack, ...firstPageParams } = sp;
  const prevHref = hrefPreviousCursor(pathname, sp);
  // A status tab restarts the pager and closes an open passport (its goat may not be on the new tab).
  const { goat_passport: _passport, ...tabParams } = firstPageParams;
  const cols = tableLabels(pageContract, "herd-register");
  const headCols = [cols[0] ?? "", ...cols.slice(3)];
  const closePassportHref = hrefWithDrawerParam(pathname, sp, "goat_passport", null);
  const drawerItems: HerdPassportDrawerItem[] = goats.map((goat) => ({
    goatId: goat.goat_id,
    displayId: goat.display_id,
    tag1: goat.animal_identifier_1,
    tag2: goat.animal_identifier_2,
    park: locationLabel(goat, "park"),
    shed: locationLabel(goat, "shed"),
    breed: goat.breed,
    sex: goat.sex,
    weightKg: goat.weight_kg,
    lifecycleStatus: goat.lifecycle_status,
    healthStatus: goat.health_status,
    reproductiveStatus: goat.reproductive_status,
  }));

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <div>
        <PageHeader
          layout={HERD_HEADER_LAYOUT}
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "section.herd.title") }]}
          actions={
            <>
            <HerdActions
              parks={locations.parks}
              sheds={locations.sheds}
              operationalLocations={operationalLocations}
              operationalLocationsAvailable={locations.available}
              farms={locations.farms}
              animalStages={animalStages}
              locationsAvailable={locations.available}
              stagesAvailable={stagesResult.ok}
              idempotencyKey={registerIdempotencyKey}
              returnTo={returnTo}
              pageContract={pageContract}
            />
            </>
          }
        />
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <Alert severity="success" variant="outlined">
            <b>{copy(pageContract, "action.success_tag")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        ) : (
          <Alert severity="error" variant="outlined">
	            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;
            <span>
              {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
              {actionDetail ? <Box component="span" sx={{ display: "block", mt: 0.75 }}>{actionDetail}</Box> : null}
            </span>
          </Alert>
        )
      ) : null}

      {/* KPI row: three template CourseWidgetSummary (KpiWidget) tiles, the live herd's make-up. */}
      {/* KPI deck (guard: url-keyed-panel): a filter / page change swaps it to its skeleton at once;
          opening a goat passport (goat_passport) never does. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={HERD_KPI_IGNORE} fallback={<HerdKpiSkeleton />}>
      <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.herd.title")}>
        {summaryCards.map((card) => (
          <Grid key={card.label} size={HERD_KPI_SIZE}>
            <KpiWidget title={card.label} total={card.value} caption={card.sub} color={kpiColor(card.tone)} sx={{ height: 1 }} />
          </Grid>
        ))}
      </Grid>
      </UrlSuspense>

      {!result.ok ? (
        <Alert severity="error" variant="outlined">
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </Alert>
      ) : null}

      <div>
      <Card>
        {/* Template user list: Card > status Tabs with Label counts > toolbar > table > pager. */}
        <UrlTabs
          ariaLabel={copy(pageContract, "filter.status.aria")}
          value={statusTab}
          items={statusTabs.map((tab) => ({
            value: tab.value,
            label: tab.label,
            href: hrefWithDrawerParam(pathname, tabParams, "status", tab.value === DEFAULT_STATUS_TAB ? null : tab.value),
            count: tab.count,
            color: tab.color,
          }))}
        />
        <HerdFiltersModalClient hasFilters={hasFilter} pageContract={pageContract} />
        {/* The footer's dense switch is the one piece of client state this server table needs, so
            the table rides into DenseTable as a server subtree rather than the page going client. */}
        {/* The herd rows + pager: the card header and filters stay mounted. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={HERD_PANEL_IGNORE} fallback={<TableSkeleton bare header={false} lead="avatar" columns={headCols.length || 9} rows={pageSize} />}>
        <DenseTable
          pagination={{
            page: Math.max(0, page - 1),
            rowsPerPage: pageSize,
            count: -1,
            // Cursor paging publishes no total, so the range ends at the page; the arrows are real
            // links and say whether more exists.
            rangeLabel: goats.length === 0 ? "0" : `${(page - 1) * pageSize + 1}–${(page - 1) * pageSize + goats.length}`,
            prevHref,
            nextHref,
            // Rows per page belongs to the pager (template TablePagination), a new size restarts at page 1.
            rowsPerPageHrefs: pageSizeOptions.map((size) => ({ value: size, href: hrefWithDrawerParam(pathname, firstPageParams, "limit", String(size)) })),
            labelRowsPerPage: copy(pageContract, "filter.rows_per_page_aria"),
            prevLabel: copy(pageContract, "action.previous"),
            nextLabel: copy(pageContract, "action.next"),
          }}
        >
          <Scrollbar tabIndex={0} role="group" aria-label={copy(pageContract, "section.herd.aria")}>
          {/* Template user list table: minWidth 960 scrolling inside the card's Scrollbar, cells on one line. */}
          <Table sx={HERD_TABLE_SX}>
            {/* Lead column (template user row): avatar + Display ID over the two tags, so the
                contract's first three heads become one. */}
            <UrlSortHead
              headCells={headCols.map((c, i) => ({ id: `${i}`, label: c }))}
              orderBy="0"
              order={order}
              // A new order restarts the cursor pager at page 1.
              sortHrefs={{ "0": hrefWithDrawerParam(pathname, tabParams, "order", order === "asc" ? "desc" : null) }}
            />
            <TableBody>
              {goats.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={headCols.length}>
                    <EmptyContent
                      filled
                      sx={{ py: 8 }}
                      title={
                        result.ok
                          ? hasFilter
                            ? copy(pageContract, "empty.herd_filtered")
                            : copy(pageContract, "empty.herd")
                          : copy(pageContract, "empty.unavailable")
                      }
                    />
                  </TableCell>
                </TableRow>
              ) : (
                goats.map((g) => {
                  const href = hrefWithDrawerParam(pathname, sp, "goat_passport", g.goat_id);
                  return (
                    <TableRow key={g.goat_id}>
                      <TableCell>
                        <Box sx={{ gap: 2, display: "flex", alignItems: "center" }}>
                          <Avatar aria-hidden="true" sx={{ bgcolor: "background.neutral", color: "text.secondary" }}>
                            <GoatGlyph size={22} />
                          </Avatar>
                          <Stack sx={{ typography: "body2", flex: "1 1 auto", alignItems: "flex-start", minWidth: 0 }}>
                            <LocalOverlayLink href={href} scroll={false}>
                              <Box component="span" sx={{ typography: "subtitle2" }}>{g.display_id}</Box>
                            </LocalOverlayLink>
                            {/* One tag per line, each kept whole: both on one line made this the widest
                                cell and pushed the last column (Breeding) past the card at 1440 (D9). */}
                            <Box component="span" sx={{ color: "text.disabled", whiteSpace: "nowrap" }}>
                              {`${cols[1] ?? ""} ${dash(g.animal_identifier_1)}`.trim()}
                            </Box>
                            {g.animal_identifier_2 ? (
                              <Box component="span" sx={{ color: "text.disabled", whiteSpace: "nowrap" }}>
                                {`${cols[2] ?? ""} ${g.animal_identifier_2}`.trim()}
                              </Box>
                            ) : null}
                          </Stack>
                        </Box>
                      </TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>
                        <LocalOverlayLink href={href} scroll={false}>{locationLabel(g, "park")}</LocalOverlayLink>
                      </TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>
                        <LocalOverlayLink href={href} scroll={false}>{locationLabel(g, "shed")}</LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={href} scroll={false}>{dash(g.breed)}</LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={href} scroll={false}>{humanizeEnum(g.sex)}</LocalOverlayLink>
                      </TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>
                        <LocalOverlayLink href={href} scroll={false}>
                          {weightLabel(g.weight_kg)}{g.weight_kg ? <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}> kg</Box> : null}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={href} scroll={false}>
                          <StatusCell value={g.lifecycle_status} color={TONE_COLOR[statusTone(g.lifecycle_status, "lifecycle")]} label={statusLabel(pageContract, "herd_lifecycle", g.lifecycle_status)} />
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={href} scroll={false}>
                          <StatusCell value={g.health_status} color={TONE_COLOR[statusTone(g.health_status, "health")]} label={statusLabel(pageContract, "herd_health", g.health_status)} />
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={href} scroll={false}>
                          <StatusCell value={g.reproductive_status} color={TONE_COLOR[statusTone(g.reproductive_status, "breeding")]} label={statusLabel(pageContract, "herd_reproductive", g.reproductive_status)} />
                        </LocalOverlayLink>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
          </Scrollbar>
        </DenseTable>
        </UrlSuspense>
      </Card>
      </div>
      <HerdPassportLocalDrawer
        items={drawerItems}
        initialSelectedId={selectedGoatId}
        closeHref={closePassportHref}
        reproductiveIdempotencyKey={reproductiveIdempotencyKey}
        returnTo={returnTo}
        pageContract={pageContract}
      />
    </Stack>
  );
}

/** The goat passport drawer param never changes the herd panels. */
const HERD_PANEL_IGNORE = ["goat_passport"] as const;
/** The KPI tiles read the summary, which neither the status tab, the sort nor the pager narrows. */
const HERD_KPI_IGNORE = ["goat_passport", "status", "order", "cursor", "cursor_stack", "page", "limit"] as const;
