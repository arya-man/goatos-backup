import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { MORTALITY_RATE_SIZE } from "./counts-layout";
import { MortalityPanelSkeleton } from "./counts-skeletons";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableFooter from "@mui/material/TableFooter";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { redirect } from "next/navigation";


import type { DateRangePickerLabels } from "@/components/date-range-picker";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Grid from "@mui/material/Grid";
import LinearProgress from "@mui/material/LinearProgress";
import Stack from "@mui/material/Stack";
import { varAlpha } from "minimal-shared/utils";
import { EmptyContent } from "@/components/minimal/empty-content";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { KpiWidget, completeMonthPercent, type KpiTrend } from "@/components/app/kpi-widget";
import { ColumnChartCard } from "@/components/app/column-chart-card";
import { PageHeader } from "@/components/app/page-header";
import type { PaletteColorKey } from "@/theme/core";
import { KpiGrid } from "@/components/app/kpi-grid";
import type { StackedDay } from "@/components/svg-series";
import { copy, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getCountsMortality,
  listAnimalStages,
  type MortalityBucket,
  type MortalityCrossCell,
  type MortalityResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, humanizeEnum, istDayPlus, todayIso } from "@/lib/format";
import { backendScope, parseScope } from "@/lib/scope";
import type { LoadPen } from "@/lib/load-pens";
import { loadPensIndex, mortalityLoadLabel } from "./mortality-load-label";
import { stageVocabularyLabel, stageNameMap, type StageNameMap } from "@/lib/stage-display";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { VaccinationTablePager } from "@/features/preventive-care-vaccination";
import { HerdAnalyticsDateFilter } from "./herd-analytics-date-filter";
import { RecentDeathsTable } from "./mortality-tables";
import { MortalityTelemetry } from "./mortality-telemetry";

// Counts -> Mortality. One question — which animals died, and what did they have in common —
// asked from every angle the farm can ask it.
//
// Two kinds of series, and the page keeps them visibly apart:
//
//   RATE series (kids/adults, stage, breed, sex, species, farm, pen, load, vendor) are drawn as a
//   rate table: deaths, every animal that was in the section during the window, and the rate with a proportional bar, because a breed with
//   3 deaths out of 40 and one with 3 out of 400 must not look alike.
//
//   COUNT series (age at death, season, cause, days since arrival, days since vaccination)
//   are drawn as share tables: they are facts about the death alone and carry no rate.
//
// The page derives NO business number of its own. Every figure below is a backend field; the
// only arithmetic is re-grouping backend rows into the shape a mark is drawn from (a cross
// tab's row totals are sums of the backend's own cells, rendered as such). The KPI tiles read
// `totals`, which the backend rolled up over the WHOLE window — re-summing a series or counting
// the capped recent list would be the banned read-time rollup.
//
// Park scope belongs to the top bar (Scope Chrome Rule); the page body owns only the window,
// as a URL param so a view survives reload and pastes as a link.

const PAGE_PATH = "/counts/mortality";
/** Mirrors counts/domain.HerdAnalyticsMaxDays — the widest window the read serves. */
const MAX_WINDOW_DAYS = 1150;
/** Mirrors counts/domain.HerdAnalyticsDefaultMonths. */
const DEFAULT_MONTHS = 12;
/** Mirrors counts/domain.HerdAnalyticsFloorDate. */
const FLOOR_DATE = "2026-08-01";
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/;

const nf = (value: number) => value.toLocaleString("en-IN");
const pct = (value: number | null | undefined) =>
  value == null ? null : `${value.toLocaleString("en-IN", { maximumFractionDigits: 1 })}%`;

function mc(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, key);
}

/** Same default-window rule as Herd Analytics, so the two Counts read screens agree. */
function defaultWindow(): { from: string; to: string } {
  const today = todayIso();
  let cursor = `${today.slice(0, 7)}-01`;
  for (let i = 0; i < DEFAULT_MONTHS - 1; i += 1) {
    cursor = `${istDayPlus(cursor, -1).slice(0, 7)}-01`;
  }
  if (cursor < FLOOR_DATE) cursor = FLOOR_DATE;
  return { from: cursor, to: today };
}

function readWindow(sp: RouteSearchParams): { from?: string; to?: string } {
  const from = one(sp, "from");
  const to = one(sp, "to");
  if (!from || !to || !DATE_PATTERN.test(from) || !DATE_PATTERN.test(to)) return {};
  if (to < from) return {};
  const days = Math.round((Date.parse(`${to}T00:00:00Z`) - Date.parse(`${from}T00:00:00Z`)) / 86_400_000) + 1;
  if (!Number.isFinite(days) || days < 1 || days > MAX_WINDOW_DAYS) return {};
  return { from, to };
}

function withStageNames(buckets: MortalityBucket[], names: StageNameMap): MortalityBucket[] {
  return buckets.map((bucket) => ({ ...bucket, label: stageVocabularyLabel(bucket.label, names) }));
}

/** A breakdown block: template Card + CardHeader (title, optional subheader) around its table. */
function ChartCard({ title, hint, children, wide }: { title: string; hint?: string; children: React.ReactNode; wide?: boolean }) {
  // Two to a row from lg; the cross tabs take the full width because their column count is data-driven.
  return (
    <Grid size={wide ? 12 : MORTALITY_RATE_SIZE} sx={{ minWidth: 0 }}>
      <Card sx={{ height: 1 }}>
        <CardHeader title={title} subheader={hint} sx={{ mb: 2 }} />
        {children}
      </Card>
    </Grid>
  );
}

/** A bucket label that is a bare backend code ("male", "goat") reads as a word ("Male", "Goat"); names stay as sent. */
function bucketLabel(label: string | null | undefined): string {
  if (!label) return "";
  return /^[a-z][a-z0-9_]*$/.test(label) ? humanizeEnum(label) : label;
}

/** Template list table head: `background.neutral` band, secondary text (TableHeadCustom look). */
const HEAD_SX = { "& th": { color: "text.secondary", bgcolor: "background.neutral", fontWeight: 600, whiteSpace: "nowrap" } } as const;

/**
 * The rate / share / cross tables are a label plus three short figures; they fit a phone outright.
 * The baseline's 540px phone floor (theme/app-baseline) stretched them past the card so every
 * figure sat behind the horizontal scroll, away from its label (PR #294 O3/O15). "&&" lifts this
 * table's own min width over that floor, which the baseline lets a table do.
 */
const COMPACT_TABLE_SX = { "&&": { minWidth: 0 } } as const;
/**
 * A row label holds ONE line and ends in an ellipsis rather than pushing its figures away; the
 * full text is the cell's title. Narrower on a phone, where the figures need the room.
 */
const LABEL_CELL_SX = { typography: "subtitle2", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis", maxWidth: { xs: 120, sm: 240 } } as const;
/** Figures keep their full width; on a phone the gutters tighten so three fit beside the label. */
const FIGURE_CELL_SX = { whiteSpace: "nowrap", px: { xs: 1, sm: 2 } } as const;

/** A row's label and its full title: a LOAD row names its pens compactly (see mortality-load-label). */
function rowLabel(text: string, pens: readonly LoadPen[] | null | undefined): { text: string; full: string } {
  return pens && pens.length > 0 ? mortalityLoadLabel(text, pens) : { text, full: text };
}

function EmptyBlock({ label }: { label: string }) {
  return <EmptyContent title={label} sx={{ py: 4 }} />;
}

/** The template progress bar (EcommerceSalesOverview row bar): 8px LinearProgress on a grey track. */
function RateBar({ value, muted }: { value: number; muted?: boolean }) {
  return (
    <LinearProgress
      variant="determinate"
      value={Math.max(0, Math.min(100, value))}
      color={muted ? "inherit" : "error"}
      aria-hidden="true"
      // Static sx: this helper renders in a Server Component, so no (theme) => function may cross.
      sx={{ height: 8, bgcolor: varAlpha("var(--palette-grey-500Channel)", 0.16), ...(muted ? { color: "text.disabled" } : {}) }}
    />
  );
}

/**
 * A RATE series: one row per bucket with deaths, the animals that were in the section during the window and the rate, plus a bar whose length
 * is the rate against the series' highest rate. Buckets with no deaths still list (a breed with
 * zero deaths out of 400 is a finding), sorted by the backend — most deaths first.
 */
function RateTable({
  buckets,
  unassignedLabel,
  deathsLabel,
  animalsLabel,
  rateLabel,
  noRateLabel,
  emptyLabel,
  ariaLabel,
}: {
  buckets: MortalityBucket[];
  unassignedLabel: string;
  deathsLabel: string;
  animalsLabel: string;
  rateLabel: string;
  noRateLabel: string;
  emptyLabel: string;
  ariaLabel: string;
}) {
  if (buckets.length === 0) return <EmptyBlock label={emptyLabel} />;
  const maxRate = Math.max(0, ...buckets.map((b) => b.rate_pct ?? 0));
  return (
    <Scrollbar tabIndex={0} role="group" aria-label={ariaLabel}>
    <Table aria-label={ariaLabel} sx={COMPACT_TABLE_SX}>
      <TableHead sx={HEAD_SX}>
        <TableRow>
          <TableCell component="th" scope="col" />
          <TableCell component="th" scope="col" align="right">
            {deathsLabel}
          </TableCell>
          <TableCell component="th" scope="col" align="right">
            {/* The copy word is the lowercase noun ("animals") used mid-sentence; a column head is capitalised like its neighbours. */}
            {animalsLabel.charAt(0).toUpperCase() + animalsLabel.slice(1)}
          </TableCell>
          <TableCell component="th" scope="col" align="right">
            {rateLabel}
          </TableCell>
          <TableCell component="th" scope="col" sx={{ width: "32%", display: { xs: "none", sm: "table-cell" } }} />
        </TableRow>
      </TableHead>
      <TableBody>
        {buckets.map((bucket) => {
          const rate = bucket.rate_pct ?? null;
          const width = rate == null || maxRate <= 0 ? 0 : Math.max(rate > 0 ? 2 : 0, (rate / maxRate) * 100);
          // A LOAD row names its pens (load-charts-name-their-pens) compactly, "Load 128 (CBE
          // Castro 3 +2)", with the whole bracket in the title; every other series carries no pens.
          const label = bucketLabel(bucket.label) ? rowLabel(bucketLabel(bucket.label), bucket.pens) : { text: unassignedLabel, full: unassignedLabel };
          return (
            <TableRow hover key={bucket.key || "__unassigned"}>
              <TableCell component="th" scope="row" sx={LABEL_CELL_SX} title={label.full}>{label.text}</TableCell>
              <TableCell align="right" sx={{ ...FIGURE_CELL_SX, typography: bucket.deaths > 0 ? "subtitle2" : "body2" }}>{nf(bucket.deaths)}</TableCell>
              <TableCell align="right" sx={{ ...FIGURE_CELL_SX, color: "text.secondary" }}>{nf(bucket.animals)}</TableCell>
              <TableCell align="right" sx={FIGURE_CELL_SX}>{rate == null ? <Box component="span" sx={{ color: "text.secondary" }} title={noRateLabel}>—</Box> : pct(rate)}</TableCell>
              <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
                <RateBar value={width} />
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
    </Scrollbar>
  );
}

/**
 * A cross tab. Rows and columns are the backend's own labels in first-seen order (the backend
 * already sorts rows by deaths); each cell is a backend count and the margins are sums of those
 * same cells, shown so a reader can check the table against the KPI above it.
 */
function CrossTable({
  cells,
  totalLabel,
  emptyLabel,
  ariaLabel,
  rowPens,
}: {
  cells: MortalityCrossCell[];
  totalLabel: string;
  emptyLabel: string;
  ariaLabel: string;
  /** Load × cause: a row's pens by its load key, so it reads as the load series does. */
  rowPens?: (key: string) => readonly LoadPen[] | undefined;
}) {
  if (cells.length === 0) return <EmptyBlock label={emptyLabel} />;
  const rows: { key: string; label: string }[] = [];
  const cols: { key: string; label: string }[] = [];
  const seenRow = new Set<string>();
  const seenCol = new Set<string>();
  const grid = new Map<string, number>();
  const rowTotals = new Map<string, number>();
  const colTotals = new Map<string, number>();
  for (const cell of cells) {
    if (!seenRow.has(cell.row_key)) {
      seenRow.add(cell.row_key);
      rows.push({ key: cell.row_key, label: cell.row_label });
    }
    if (!seenCol.has(cell.col_key)) {
      seenCol.add(cell.col_key);
      cols.push({ key: cell.col_key, label: cell.col_label });
    }
    grid.set(`${cell.row_key}\t${cell.col_key}`, (grid.get(`${cell.row_key}\t${cell.col_key}`) ?? 0) + cell.deaths);
    rowTotals.set(cell.row_key, (rowTotals.get(cell.row_key) ?? 0) + cell.deaths);
    colTotals.set(cell.col_key, (colTotals.get(cell.col_key) ?? 0) + cell.deaths);
  }
  // Rows by their total, columns by theirs, so the heaviest corner is top-left.
  rows.sort((a, b) => (rowTotals.get(b.key) ?? 0) - (rowTotals.get(a.key) ?? 0));
  cols.sort((a, b) => (colTotals.get(b.key) ?? 0) - (colTotals.get(a.key) ?? 0));
  const max = Math.max(1, ...grid.values());
  const grand = [...rowTotals.values()].reduce((a, b) => a + b, 0);
  return (
    <Scrollbar tabIndex={0} role="region" aria-label={ariaLabel}>
      <Table aria-label={ariaLabel} sx={COMPACT_TABLE_SX}>
        <TableHead sx={HEAD_SX}>
          <TableRow>
            <TableCell component="th" scope="col" />
            {cols.map((col) => (
              <TableCell component="th" key={col.key || "__none"} scope="col" align="right">
                {col.label}
              </TableCell>
            ))}
            <TableCell component="th" scope="col" align="right">
              {totalLabel}
            </TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => {
            const label = rowLabel(row.label, rowPens?.(row.key));
            return (
            <TableRow hover key={row.key || "__none"}>
              <TableCell component="th" scope="row" sx={LABEL_CELL_SX} title={label.full}>{label.text}</TableCell>
              {cols.map((col) => {
                const value = grid.get(`${row.key}\t${col.key}`) ?? 0;
                // Heat: the cell's share of the largest cell, as an alpha on the brand colour.
                const alpha = value === 0 ? 0 : 0.12 + 0.55 * (value / max);
                return (
                  <TableCell
                    key={col.key || "__none"}
                    align="right"
                    sx={value === 0 ? { color: "text.disabled" } : { bgcolor: `color-mix(in srgb, var(--palette-error-main) ${Math.round(alpha * 100)}%, transparent)` }}
                  >
                    {value === 0 ? "·" : nf(value)}
                  </TableCell>
                );
              })}
              <TableCell align="right" sx={{ ...FIGURE_CELL_SX, typography: "subtitle2" }}>{nf(rowTotals.get(row.key) ?? 0)}</TableCell>
            </TableRow>
            );
          })}
        </TableBody>
        <TableFooter sx={{ "& td, & th": { typography: "subtitle2", color: "text.primary", bgcolor: "background.neutral", borderBottom: 0 } }}>
          <TableRow>
            <TableCell component="th" scope="row">{totalLabel}</TableCell>
            {cols.map((col) => (
              <TableCell key={col.key || "__none"} align="right">
                {nf(colTotals.get(col.key) ?? 0)}
              </TableCell>
            ))}
            <TableCell align="right">{nf(grand)}</TableCell>
          </TableRow>
        </TableFooter>
      </Table>
    </Scrollbar>
  );
}

/**
 * A COUNT series as a table: label, deaths, share of all deaths and a bar against the largest
 * bucket. Every band is listed, zeros included, so "none in summer" is a visible zero rather
 * than a missing row. The cause series adds a basis chip per row, which a bar chart cannot carry.
 */
function ShareTable({
  buckets,
  totalDeaths,
  basisLabels,
  deathsLabel,
  shareLabel,
  emptyLabel,
  ariaLabel,
  unassignedLabel,
}: {
  buckets: MortalityBucket[];
  totalDeaths: number;
  basisLabels?: Record<"recorded" | "inferred" | "none", string>;
  deathsLabel: string;
  shareLabel: string;
  emptyLabel: string;
  ariaLabel: string;
  unassignedLabel?: string;
}) {
  if (buckets.length === 0 || totalDeaths === 0) return <EmptyBlock label={emptyLabel} />;
  const max = Math.max(1, ...buckets.map((b) => b.deaths));
  return (
    <Scrollbar tabIndex={0} role="group" aria-label={ariaLabel}>
    <Table aria-label={ariaLabel} sx={COMPACT_TABLE_SX}>
      <TableHead sx={HEAD_SX}>
        <TableRow>
          <TableCell component="th" scope="col" />
          {basisLabels ? <TableCell component="th" scope="col" /> : null}
          <TableCell component="th" scope="col" align="right">
            {deathsLabel}
          </TableCell>
          <TableCell component="th" scope="col" align="right">
            {shareLabel}
          </TableCell>
          <TableCell component="th" scope="col" sx={{ width: "32%", display: { xs: "none", sm: "table-cell" } }} />
        </TableRow>
      </TableHead>
      <TableBody>
        {buckets.map((bucket) => {
          const basis = (bucket.basis ?? "none") as "recorded" | "inferred" | "none";
          const share = (bucket.deaths / totalDeaths) * 100;
          const muted = basisLabels ? basis === "none" : false;
          return (
            <TableRow hover key={`${bucket.basis ?? ""}:${bucket.key}`}>
              <TableCell component="th" scope="row" sx={LABEL_CELL_SX} title={bucketLabel(bucket.label) || unassignedLabel || bucket.key}>{bucketLabel(bucket.label) || unassignedLabel || bucket.key}</TableCell>
              {basisLabels ? (
                <TableCell>
                  {/* The no-cause row is LABELLED by its basis already; a chip repeating the row's
                      own words beside it says nothing twice (D5). */}
                  {basisLabels[basis] === bucketLabel(bucket.label) ? null : (
                    <Label variant="soft" color={basis === "recorded" ? "success" : basis === "inferred" ? "info" : "default"}>{basisLabels[basis]}</Label>
                  )}
                </TableCell>
              ) : null}
              <TableCell align="right" sx={{ ...FIGURE_CELL_SX, ...(bucket.deaths > 0 ? { typography: "subtitle2" } : { color: "text.secondary" }) }}>{nf(bucket.deaths)}</TableCell>
              <TableCell align="right" sx={{ ...FIGURE_CELL_SX, color: "text.secondary" }}>{bucket.deaths === 0 ? "—" : share < 1 ? "<1%" : pct(Math.round(share * 10) / 10)}</TableCell>
              <TableCell sx={{ display: { xs: "none", sm: "table-cell" } }}>
                <RateBar value={(bucket.deaths / max) * 100} muted={muted} />
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
    </Scrollbar>
  );
}

export async function MortalityPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const requested = readWindow(sp);
  const { parkId } = backendScope(parseScope(sp));

  // The deaths list pages on the SERVER: the window can hold more animals than one screen, and
  // slicing a capped list on the client would show a pager that stops before the tile above it
  // does. Both parameters are sent as asked and RESOLVED there -- an unknown size or a bad offset
  // lands on the first page rather than failing a screen whose every other figure is whole-window.
  const pageSizeOptions = tablePageSizes(pageContract, "recent-deaths");
  const requestedSize = Number(one(sp, "md_limit"));
  const recentLimit = pageSizeOptions.includes(requestedSize) ? requestedSize : (pageSizeOptions[1] ?? pageSizeOptions[0] ?? 25);
  const requestedPage = Math.max(1, Number(one(sp, "md_page")) || 1);

  // ONE round trip for the whole screen.
  const [result, stageResult] = await Promise.all([
    getCountsMortality({
      park_id: parkId,
      from: requested.from,
      to: requested.to,
      recent_limit: recentLimit,
      recent_offset: (requestedPage - 1) * recentLimit,
    }),
    // Tenant stage vocabulary — the same read Counts Breakdown and Herd Register make — so the
    // fattening family can show its configured name instead of its code (see stageDisplayLabel).
    listAnimalStages(),
  ]);
  if (firstAuthRequiredError(result, stageResult)) redirect(INTERNAL_LOGIN_PATH);
  const data: MortalityResponse | null = result.ok ? result.data : null;
  const stageNames = stageNameMap(stageResult.ok ? stageResult.data.items : undefined);

  const pickerLabels: DateRangePickerLabels = {
    field: mc(pageContract, "filter.date"),
    today: mc(pageContract, "filter.date.today"),
    single: mc(pageContract, "filter.date.single"),
    range: mc(pageContract, "filter.date.range"),
    aria: mc(pageContract, "filter.date.aria"),
    previousMonth: mc(pageContract, "filter.date.previous_month"),
    nextMonth: mc(pageContract, "filter.date.next_month"),
    rangeStartHint: mc(pageContract, "filter.date.range_start_hint"),
    rangeEndHint: mc(pageContract, "filter.date.range_end_hint"),
    rangeSeparator: mc(pageContract, "filter.date.range_separator"),
  };
  const fallback = defaultWindow();

  if (!data) {
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        <MortalityTelemetry routeId={pageContract.route_id} parkId={parkId} months={0} deaths={0} />
        <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb", "Counts"), href: "/counts/herd" }, { label: pageContract.title }]} />
        <Alert severity="error" variant="outlined">
          <AlertTitle>{mc(pageContract, "error.title")}</AlertTitle>
          {mc(pageContract, "error.body")}
        </Alert>
      </Stack>
    );
  }

  const totals = data.totals;
  // One load's pens, found the same way from the load series for every surface that names a load.
  const loadPens = loadPensIndex(data.load);

  // Every pager link carries the rest of the URL forward -- the window and the park scope live
  // there too, and a page link that dropped them would silently re-scope the screen it is paging.
  function hrefWithParam(key: string, value: string, options?: { drop?: string[] }): string {
    const dropped = new Set([key, ...(options?.drop ?? [])]);
    const next = new URLSearchParams();
    for (const [paramKey, paramValue] of Object.entries(sp)) {
      if (dropped.has(paramKey)) continue;
      if (Array.isArray(paramValue)) {
        for (const item of paramValue) if (item) next.append(paramKey, item);
      } else if (paramValue) {
        next.set(paramKey, paramValue);
      }
    }
    if (value) next.set(key, value);
    const qs = next.toString();
    return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
  }

  const deathsNoun = mc(pageContract, "label.deaths_noun");
  const emptyChart = mc(pageContract, "chart.empty");
  const animalsWord = mc(pageContract, "kpi.animals");
  const noRate = mc(pageContract, "kpi.no_rate");
  const rateLabels = {
    deathsLabel: mc(pageContract, "series.deaths"),
    animalsLabel: mc(pageContract, "kpi.animals"),
    rateLabel: mc(pageContract, "series.rate"),
    noRateLabel: noRate,
    emptyLabel: emptyChart,
  };
  const basisLabels = {
    recorded: mc(pageContract, "basis.recorded"),
    inferred: mc(pageContract, "basis.inferred"),
    none: mc(pageContract, "basis.none"),
  } as const;

  const monthDays: StackedDay[] = data.months.map((m) => ({ key: m.month, label: m.label, segments: [m.kids, m.adults] }));
  // Kids over adults take the template AnalyticsWebsiteVisits default pair (primary.dark /
  // warning.main); a palette KEY such as "primary" is not a CSS colour and paints black in Apex.
  const monthSeries = [{ label: mc(pageContract, "series.kids") }, { label: mc(pageContract, "series.adults") }];
  const causeEstablished = totals.cause_recorded + totals.cause_inferred;
  const rateWithAnimals = (rate: number | null | undefined, animals: number) =>
    rate == null ? noRate : `${pct(rate)} · ${nf(animals)} ${animalsWord}`;
  const showParks = data.park.length > 1;
  const showSpecies = data.species.length > 1;
  // Seven rate cards always, plus species and park when they discriminate: two to a row, so an odd
  // count leaves the last one alone at half width unless it fills its row.
  const rateCardsOdd = (7 + (showSpecies ? 1 : 0) + (showParks ? 1 : 0)) % 2 === 1;
  const stageBuckets = withStageNames(data.stage, stageNames);
  const seasonByStage = data.season_by_stage.map((cell) => ({ ...cell, col_label: stageVocabularyLabel(cell.col_label, stageNames) }));
  // Template CourseWidgetSummary takes a number: units go in the title, detail in the caption.
  // Template widgets print a number: the unit / remainder leads the visible sub-line. Deaths, kids
  // and adults carry the change between the last two COMPLETE months (the change leads the Course card sub-line, J2 P1-8); the
  // monthly series itself is the kids / adults chart below.
  const monthTrend = (series: number[]) => {
    const percent = completeMonthPercent(series, data.window_to);
    return percent == null ? null : { percent, period: "month" as const };
  };
  // The unit rides in the tile's title ("Mortality rate (%)"), never as a stray "%" leading the sub-line.
  const pctSub = (rate: number | null | undefined, rest: string) => (rate == null ? noRate : rest);
  const kpis: { key: string; tone: PaletteColorKey; label: string; value: number | null; hint: string; trend?: KpiTrend | null }[] = [
    { key: "deaths", tone: "error", label: mc(pageContract, "kpi.deaths.label"), value: totals.deaths, hint: "", trend: monthTrend(data.months.map((m) => m.deaths)) },
    {
      key: "rate",
      tone: "primary",
      label: mc(pageContract, "kpi.rate.label"),
      value: totals.rate_pct == null ? null : Math.round(totals.rate_pct * 10) / 10,
      hint: pctSub(totals.rate_pct, `${nf(totals.animals)} ${animalsWord} · ${mc(pageContract, "kpi.rate.sub")}`),
    },
    { key: "kids", tone: "warning", label: mc(pageContract, "kpi.kids.label"), value: totals.kid_deaths, hint: rateWithAnimals(totals.kid_rate_pct, totals.kid_animals), trend: monthTrend(data.months.map((m) => m.kids)) },
    { key: "adults", tone: "secondary", label: mc(pageContract, "kpi.adults.label"), value: totals.adult_deaths, hint: rateWithAnimals(totals.adult_rate_pct, totals.adult_animals), trend: monthTrend(data.months.map((m) => m.adults)) },
    { key: "first_week", tone: "info", label: mc(pageContract, "kpi.first_week.label"), value: totals.first_week_deaths, hint: "" },
    {
      // "12 / 30": the figure is 12, the sub-line reads "of 30".
      key: "cause",
      tone: "success",
      label: mc(pageContract, "kpi.cause.label"),
      value: totals.deaths === 0 ? null : causeEstablished,
      hint: totals.deaths === 0 ? "—" : `of ${nf(totals.deaths)}`,
    },
  ];

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <MortalityTelemetry routeId={pageContract.route_id} parkId={parkId} months={data.months.length} deaths={totals.deaths} />

      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb", "Counts"), href: "/counts/herd" }, { label: pageContract.title }]}
      />

      <Box>
        <HerdAnalyticsDateFilter
          labels={pickerLabels}
          basePath={PAGE_PATH}
          from={data.window_from}
          to={data.window_to}
          today={todayIso()}
          defaultFrom={fallback.from}
          defaultTo={fallback.to}
        />
      </Box>

      {/* Everything the window reads (guard: url-keyed-panel): a date / park change swaps it to its
          skeleton at once; header and date filter stay on screen. The recent-deaths pager does not. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PAGER_PARAMS} fallback={<MortalityPanelSkeleton />}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      {/* KPI row: template CourseWidgetSummary (KpiWidget); backend window totals. */}
      <Box component="section" aria-label={mc(pageContract, "section.kpi.aria")}>
        <KpiGrid>
          {kpis.map((kpi) => (
            <KpiWidget
              key={kpi.key}
              title={kpi.label}
              total={kpi.value}
              color={kpi.tone}
              caption={kpi.hint || undefined}
              trend={kpi.trend}
              sx={{ height: 1 }}
            />
          ))}
        </KpiGrid>
      </Box>

      {/* Deaths by month, kids over adults: template AnalyticsWebsiteVisits stacked. */}
      <ColumnChartCard
        aria-label={mc(pageContract, "chart.months.title")}
        title={mc(pageContract, "chart.months.title")}
        valueNoun={deathsNoun}
        empty={<EmptyContent title={emptyChart} />}
        chart={{
          categories: monthDays.map((d) => d.label),
          series: monthSeries.map((series, i) => ({ name: series.label, data: monthDays.map((d) => d.segments[i] ?? 0) })),
          options: { chart: { stacked: true }, plotOptions: { bar: { columnWidth: "36%" } } },
        }}
      />

      {/* RATE series. Each card names what it divides by. */}
      <Grid container spacing={3} component="section" aria-label={mc(pageContract, "section.rates.aria")}>
          <ChartCard title={mc(pageContract, "chart.stage.title")}>
            <RateTable buckets={stageBuckets} unassignedLabel={mc(pageContract, "label.unassigned_stage")} ariaLabel={mc(pageContract, "chart.stage.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.breed.title")}>
            <RateTable buckets={data.breed} unassignedLabel={mc(pageContract, "label.unassigned_breed")} ariaLabel={mc(pageContract, "chart.breed.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.load.title")}>
            <RateTable buckets={data.load} unassignedLabel={mc(pageContract, "label.no_load")} ariaLabel={mc(pageContract, "chart.load.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.vendor.title")} hint={mc(pageContract, "chart.vendor.hint")}>
            <RateTable buckets={data.vendor} unassignedLabel={mc(pageContract, "label.no_vendor")} ariaLabel={mc(pageContract, "chart.vendor.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.pen.title")} hint={mc(pageContract, "chart.pen.hint")}>
            <RateTable buckets={data.pen} unassignedLabel={mc(pageContract, "label.unassigned_pen")} ariaLabel={mc(pageContract, "chart.pen.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.kid_adult.title")}>
            <RateTable buckets={data.kid_adult} unassignedLabel={mc(pageContract, "label.unassigned_stage")} ariaLabel={mc(pageContract, "chart.kid_adult.title")} {...rateLabels} />
          </ChartCard>
          <ChartCard wide={!showSpecies && !showParks && rateCardsOdd} title={mc(pageContract, "chart.sex.title")}>
            <RateTable buckets={data.sex} unassignedLabel={mc(pageContract, "label.unassigned_sex")} ariaLabel={mc(pageContract, "chart.sex.title")} {...rateLabels} />
          </ChartCard>
          {showSpecies ? (
            <ChartCard wide={!showParks && rateCardsOdd} title={mc(pageContract, "chart.species.title")}>
              <RateTable buckets={data.species} unassignedLabel={mc(pageContract, "label.unassigned_species")} ariaLabel={mc(pageContract, "chart.species.title")} {...rateLabels} />
            </ChartCard>
          ) : null}
          {showParks ? (
            <ChartCard wide={rateCardsOdd} title={mc(pageContract, "chart.park.title")}>
              <RateTable buckets={data.park} unassignedLabel={mc(pageContract, "label.unassigned_park")} ariaLabel={mc(pageContract, "chart.park.title")} {...rateLabels} />
            </ChartCard>
          ) : null}
      </Grid>

      {/* COUNT series: facts about the death alone. */}
      <Grid container spacing={3} component="section" aria-label={mc(pageContract, "section.counts.aria")}>
          <ChartCard title={mc(pageContract, "chart.cause.title")}>
            <ShareTable
              buckets={data.cause}
              totalDeaths={totals.deaths}
              basisLabels={basisLabels}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.cause.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.age.title")}>
            <ShareTable
              buckets={data.age_at_death}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.age.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.season.title")}>
            <ShareTable
              buckets={data.season}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.season.title")}
            />
          </ChartCard>
          <ChartCard title={mc(pageContract, "chart.arrival.title")}>
            <ShareTable
              buckets={data.days_since_arrival}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.arrival.title")}
            />
          </ChartCard>
          {/* Five count cards: the fifth fills its row rather than sitting half-width alone. */}
          <ChartCard wide title={mc(pageContract, "chart.vaccine.title")}>
            <ShareTable
              buckets={data.days_since_vaccination}
              totalDeaths={totals.deaths}
              deathsLabel={mc(pageContract, "series.deaths")}
              shareLabel={mc(pageContract, "label.share")}
              emptyLabel={emptyChart}
              ariaLabel={mc(pageContract, "chart.vaccine.title")}
            />
          </ChartCard>
      </Grid>

      {/* Cross tabs. */}
      <Grid container spacing={3} component="section" aria-label={mc(pageContract, "section.cross.aria")}>
          <ChartCard wide title={mc(pageContract, "cross.season_stage.title")}>
            <CrossTable cells={seasonByStage} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.season_stage.title")} />
          </ChartCard>
          <ChartCard wide title={mc(pageContract, "cross.load_cause.title")}>
            <CrossTable cells={data.load_by_cause} rowPens={loadPens.byKey} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.load_cause.title")} />
          </ChartCard>
          <ChartCard wide title={mc(pageContract, "cross.vendor_cause.title")} hint={mc(pageContract, "cross.vendor_cause.hint")}>
            <CrossTable cells={data.vendor_by_cause} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.vendor_cause.title")} />
          </ChartCard>
          <ChartCard wide title={mc(pageContract, "cross.breed_cause.title")} hint={mc(pageContract, "cross.breed_cause.hint")}>
            <CrossTable cells={data.breed_by_cause} totalLabel={mc(pageContract, "cross.total")} emptyLabel={emptyChart} ariaLabel={mc(pageContract, "cross.breed_cause.title")} />
          </ChartCard>
      </Grid>

      </Stack>
      </UrlSuspense>

      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<PanelSkeleton table={data.recent_limit} tableWidths={Array.from({ length: 9 }, () => "1fr")} />}>
      <div>
        <Card aria-label={mc(pageContract, "table.recent.title")}>
          <CardHeader
            title={mc(pageContract, "table.recent.title")}
            subheader={mc(pageContract, "table.recent.hint")}
            action={<Label variant="soft" color="default">{nf(totals.deaths)}</Label>}
            sx={{ mb: 2 }}
          />
          <RecentDeathsTable
            contract={table(pageContract, "recent-deaths")}
            rows={data.deaths.map((d) => ({
              goatId: d.goat_id,
              diedOn: fmtDate(d.died_on),
              sortDate: d.died_on,
              tag: d.tag,
              displayId: d.display_id,
              breed: d.breed,
              sex: bucketLabel(d.sex),
              stage: stageVocabularyLabel(d.stage, stageNames),
              ageDays: d.age_days ?? null,
              ageBandLabel: d.age_band_label,
              park: d.park,
              pen: d.pen,
              loadRef: d.load_ref,
              loadPens: d.load_ref ? loadPens.byLabel(d.load_ref) : undefined,
              causeLabel: d.cause_label,
              causeBasis: d.cause_basis,
              basisLabel: basisLabels[d.cause_basis],
            }))}
            ariaLabel={mc(pageContract, "table.recent.title")}
            empty={emptyChart}
            noDataLabel={mc(pageContract, "label.no_load")}
            daysSuffix={mc(pageContract, "label.days_suffix")}
          />
          {/* The pager reads the page the SERVER actually served (`recent_offset` / `recent_limit`),
              never the one the URL asked for, so a resolved parameter cannot leave the footer
              describing a page the table is not showing. Its total is `totals.deaths`: the list and
              that tile count the same window deaths, which is why paging moves no figure above. */}
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={pageSizeOptions}
            page={Math.floor(data.recent_offset / data.recent_limit) + 1}
            pageSize={data.recent_limit}
            total={totals.deaths}
            start={data.deaths.length === 0 ? 0 : data.recent_offset + 1}
            end={data.recent_offset + data.deaths.length}
            noun={mc(pageContract, "table.recent.noun")}
            hrefForPage={(nextPage) => hrefWithParam("md_page", nextPage === 1 ? "" : String(nextPage))}
            /* A size change returns to the first page: page 4 of 10-row pages is a different set of
               animals from page 4 of 50-row pages, and keeping the number would scroll the reader
               somewhere they did not ask to go. */
            hrefForPageSize={(nextSize) => hrefWithParam("md_limit", String(nextSize), { drop: ["md_page"] })}
          />
        </Card>
      </div>
      </UrlSuspense>
    </Stack>
  );
}

/** The recent-deaths pager: paging the list moves no figure above it. */
const PAGER_PARAMS = ["md_page", "md_limit"] as const;
