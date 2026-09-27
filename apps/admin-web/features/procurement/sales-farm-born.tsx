import Table from "@mui/material/Table";
import TableContainer from "@mui/material/TableContainer";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { redirect } from "next/navigation";

import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { EmptyState } from "@/components/app/empty-state";
import { KpiWidget } from "@/components/app/kpi-widget";
import { Label } from "@/components/minimal/label";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { copy, optionGroup, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listAnimalStages } from "@/lib/api/server";
import { getFarmBornSales } from "@/lib/api/procurement-server";
import type { FarmBornBucket, FarmBornSales } from "@/lib/api/procurement";
import { todayIso } from "@/lib/format";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { stageNameMap, stageVocabularyLabel, type StageNameMap } from "@/lib/stage-display";
import { humanDate, inr, num } from "./sales-format";
import { SalesFarmToggle, SalesPageHeader, readSalesParkScope } from "./sales-chrome";
import { FarmBornSoldTable } from "./farm-born-sold-table";
import { ProcurementTableFooter } from "./table-footer-links";
import { ProgressBar } from "@/components/app/progress-bar";
import Alert from "@mui/material/Alert";
import { tableOrderFromParams, type TableOrder } from "./table-order";
import { salesErrorText } from "./sales-error";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Typography from "@mui/material/Typography";
import { cardTableScrollSx } from "./procurement-sx";

// Breakdown table (template analytics table anatomy): the label column keeps a readable floor so
// words never break per letter; on a laptop the table fits its half-width card, on a phone it
// scrolls inside the card from a 38.75rem floor.
// Six columns since main added "Tagged, sale not closed" (cc940b351): cells sit a little tighter
// and number headers may wrap onto a second line (figures never do), so the table still fits its
// half-width card on a laptop.
const FB_TABLE_SX = {
  width: "100%",
  tableLayout: "auto",
  minWidth: { xs: "38.75rem", sm: 0 },
  "& th, & td": { px: 1 },
  "& th": { lineHeight: 1.2, verticalAlign: "bottom" },
  "& th.num": { whiteSpace: "normal", maxWidth: "12ch" },
  "& td:first-of-type, & th:first-of-type": { minWidth: "8.75rem", whiteSpace: "normal", overflowWrap: "break-word" },
  "& td.num": { whiteSpace: "nowrap" },
} as const;
const FB_SHARE_CELL_SX = { minWidth: { xs: 0, sm: "7.5rem" } } as const;


const PAGE_PATH = "/sales/farm-born";
/** Only used when an older backend contract carries no sold table; the contract page size wins. */
const FALLBACK_LIMIT = 25;
const MAX_OFFSET = 10000;
/** The By pen card pages its rows: a farm has dozens of pens and the card sat 1,600px tall. */
const PEN_PAGE_SIZE = 10;
const PEN_OFFSET_PARAM = "pen_offset";

/**
 * One calendar month before an ISO day, the way the backend's DefaultFarmBornWindow counts it
 * (Go's AddDate(0, -1, 0) and Date.UTC both normalise "31 Feb" forward to 3 Mar), so the picker
 * recognises the backend's own default window and clears the parameters for it.
 */
function monthBefore(day: string): string {
  const [year, month, date] = day.split("-").map(Number);
  const shifted = new Date(Date.UTC(year, month - 2, date));
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

function hrefWith(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
}

/** A requested page size is honoured only when the contract offers it. */
function resolveLimit(raw: string | undefined, offered: readonly number[], fallback: number): number {
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;
  if (Number.isFinite(parsed) && offered.includes(parsed)) return parsed;
  return fallback;
}

/**
 * One breakdown card: the same animals split one way, on farm today beside sold in the period.
 * Every figure is the backend's; the share is the row's sold over the whole-filter sold, which
 * the payload guarantees the rows sum to.
 */
function BreakdownCard({
  title,
  rows,
  totalSold,
  pageContract,
  pager,
}: {
  title: string;
  rows: FarmBornBucket[];
  totalSold: number;
  pageContract: AdminUiPageContract;
  /**
   * Optional page window over `rows`. The rows are the WHOLE breakdown (so the share column is
   * still over the whole-filter sold count); only the slice shown moves. Server-rendered links,
   * so the page survives a reload and a shared URL.
   */
  pager?: { offset: number; limit: number; noun: string; href: (offset: number) => string };
}) {
  // A filter change can leave a pen offset past the end of a now-shorter list (the bar resets the
  // ledger offset, not this one); an out-of-range page falls back to the first rather than an
  // empty card.
  if (pager && pager.offset >= rows.length) pager = { ...pager, offset: 0 };
  const shown = pager ? rows.slice(pager.offset, pager.offset + pager.limit) : rows;
  const pageCount = pager ? Math.max(1, Math.ceil(rows.length / pager.limit)) : 1;
  const pageNumber = pager ? Math.floor(pager.offset / pager.limit) + 1 : 1;
  const maxSold = Math.max(0, ...rows.map((row) => row.sold));
  const tableId = `farm-born-${title.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`;
  return (
    <div>
    <Card component="section" aria-label={title} sx={{ minWidth: 0 }}>
      <CardHeader
        title={title}
        action={pager && pageCount > 1 ? <Label variant="soft" color="default">{num(rows.length)} {pager.noun}</Label> : null}
        sx={{ mb: 2, [`& .${cardHeaderClasses.action}`]: { alignSelf: "center" } }}
      />
      <TableContainer id={tableId} tabIndex={0} role="region" aria-label={title} sx={cardTableScrollSx}>
        <Table sx={FB_TABLE_SX}>
          <TableHead>
            <TableRow>
              <TableCell component="th" />
              <TableCell component="th" className="num">{copy(pageContract, "column.on_farm")}</TableCell>
              <TableCell component="th" className="num" title={copy(pageContract, "value.tagged_not_closed.hint")}>
                {copy(pageContract, "column.tagged_not_closed")}
              </TableCell>
              <TableCell component="th" className="num">{copy(pageContract, "column.sold")}</TableCell>
              <TableCell component="th" className="num" sx={FB_SHARE_CELL_SX}>{copy(pageContract, "column.share_pct")}</TableCell>
              <TableCell component="th" className="num">{copy(pageContract, "column.revenue")}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6}>
                  <EmptyState title={copy(pageContract, "empty.breakdown")} />
                </TableCell>
              </TableRow>
            ) : (
              shown.map((row) => {
                const share = totalSold > 0 ? (row.sold / totalSold) * 100 : null;
                return (
                <TableRow key={row.key}>
                  <TableCell>
                    <Typography variant="subtitle2" component="div">{row.label}</Typography>
                    {row.detail ? <Typography variant="caption" component="div" color="text.secondary">{row.detail}</Typography> : null}
                  </TableCell>
                  <TableCell className="num">{num(row.on_farm)}</TableCell>
                  <TableCell className="num">{num(row.tagged_not_closed)}</TableCell>
                  <TableCell className="num">
                    <b>{num(row.sold)}</b>
                  </TableCell>
                  <TableCell className="num" sx={FB_SHARE_CELL_SX}>
                    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1.25, width: "100%", justifyContent: "flex-end" }}>
                      {/* Inline mini-bar: the row's sold against the largest row, so a breed that
                          sold ten times another reads at a glance; the share text stays the number. */}
                      <Box component="span" sx={{ display: { xs: "none", sm: "block" }, flex: "1 1 3rem", maxWidth: "4.5rem" }}>
                        <ProgressBar value={maxSold > 0 ? (row.sold / maxSold) * 100 : 0} />
                      </Box>
                      <Box component="span" sx={{ minWidth: "2.25rem", textAlign: "right", fontVariantNumeric: "tabular-nums" }}>
                        {share == null ? "—" : `${num(share, 0)}%`}
                      </Box>
                    </Box>
                  </TableCell>
                  <TableCell className="num">{row.sold_priced > 0 ? inr(row.revenue) : "—"}</TableCell>
                </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </TableContainer>
      {pager && pageCount > 1 ? (
        <ProcurementTableFooter
          denseLabel={copy(pageContract, "action.dense", "Dense")}
          rowsLabel={copy(pageContract, "pager.rows", "Rows")}
          page={pageNumber}
          pageCount={pageCount}
          rangeLabel={`${pager.offset + 1}\u2013${Math.min(pager.offset + pager.limit, rows.length)} ${copy(pageContract, "pager.of")} ${num(rows.length)} ${pager.noun}`}
          prevHref={pager.href(Math.max(0, pager.offset - pager.limit))}
          nextHref={pager.href(pager.offset + pager.limit)}
          prevLabel={copy(pageContract, "action.previous")}
          nextLabel={copy(pageContract, "action.next")}
          denseTargetId={tableId}
        />
      ) : null}
    </Card>
    </div>
  );
}

function FarmBornSections({
  data,
  pageContract,
  pageHref,
  penOffset,
  penHref,
  stageNames,
  order,
}: {
  data: FarmBornSales;
  pageContract: AdminUiPageContract;
  pageHref: (offset: number) => string;
  penOffset: number;
  penHref: (offset: number) => string;
  stageNames: StageNameMap;
  order: TableOrder;
}) {
  const s = data.summary;
  const unpriced = s.sold - s.sold_priced;
  const pageNumber = Math.floor(data.offset / data.limit) + 1;
  const pageCount = Math.max(1, Math.ceil(data.total_sold / data.limit));
  const sexLabel = (key: string) =>
    optionGroup(pageContract, "farm_born_sexes").find((option) => option.key === key)?.label ?? key;

  return (
    <>
      {/* Headline figures: template CourseWidgetSummary (KpiWidget) -- figure, title, tone icon;
          never the pastel AnalyticsWidgetSummary in dark. */}
      <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.headline.aria")}>
        <Grid size={{ xs: 12, sm: 6, md: 3 }}>
          <KpiWidget color="primary" title={copy(pageContract, "kpi.on_farm")} total={s.on_farm} sx={{ height: 1 }} />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, md: 3 }}>
          {/* Tagged to a sale that has not closed: out of the herd, not yet sold (main 054918241). */}
          <KpiWidget
            color="info"
            icon="certificates"
            title={copy(pageContract, "kpi.sold")}
            total={s.sold}
            caption={`${humanDate(s.from)} ${copy(pageContract, "filter.period.range_separator")} ${humanDate(s.to)}${
              s.tagged_not_closed > 0 ? ` · ${num(s.tagged_not_closed)} ${copy(pageContract, "kpi.tagged_not_closed")} (${copy(pageContract, "value.tagged_not_closed.hint")})` : ""
            }`}
            sx={{ height: 1 }}
          />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, md: 3 }}>
          <KpiWidget
            color="success"
            title={copy(pageContract, "kpi.revenue")}
            total={s.revenue}
            caption={[`₹`, unpriced > 0 ? `${num(s.sold_priced)} / ${num(s.sold)}` : undefined].filter(Boolean).join(" · ")}
            sx={{ height: 1 }}
          />
        </Grid>
        <Grid size={{ xs: 12, sm: 6, md: 3 }}>
          <KpiWidget color="secondary" title={copy(pageContract, "kpi.avg_price")}
 caption={`${(s.sold_priced > 0 ? s.avg_price : null) == null ? "—" : `₹`}`} total={s.sold_priced > 0 ? s.avg_price : null} sx={{ height: 1 }} />
        </Grid>
      </Grid>

      {/* Breed on the left; Sex and Stage stacked on the right (both short). Pen gets its own
          full-width card below: a farm has dozens of pens, and beside a two-row sex table it left
          the right column mostly blank. */}
      <Grid container spacing={3} sx={{ mt: 3 }}>
        <Grid size={{ xs: 12, md: 6 }}>
          <BreakdownCard title={copy(pageContract, "section.by_breed.title")} rows={data.by_breed} totalSold={s.sold} pageContract={pageContract} />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <Stack spacing={3}>
            <BreakdownCard
              title={copy(pageContract, "section.by_sex.title")}
              rows={data.by_sex.map((row) => ({ ...row, label: sexLabel(row.key) === row.key ? row.label : sexLabel(row.key) }))}
              totalSold={s.sold}
              pageContract={pageContract}
            />
            <BreakdownCard
              title={copy(pageContract, "section.by_stage.title")}
              rows={data.by_stage.map((row) => ({ ...row, label: stageVocabularyLabel(row.label, stageNames) }))}
              totalSold={s.sold}
              pageContract={pageContract}
            />
          </Stack>
        </Grid>
        <Grid size={12}>
          <BreakdownCard
            title={copy(pageContract, "section.by_pen.title")}
            rows={data.by_pen}
            totalSold={s.sold}
            pageContract={pageContract}
            pager={{ offset: penOffset, limit: PEN_PAGE_SIZE, noun: copy(pageContract, "pager.pens"), href: penHref }}
          />
        </Grid>
      </Grid>

      <div>
      <Card component="section" aria-label={copy(pageContract, "section.sold.aria")} sx={{ minWidth: 0, mt: 3 }}>
        <CardHeader
          title={copy(pageContract, "section.sold.title")}
          action={
            <Label variant="soft" color={data.total_sold ? "info" : "default"}>
              {num(data.total_sold)} {copy(pageContract, "pager.noun")}
            </Label>
          }
          sx={{ mb: 2, [`& .${cardHeaderClasses.action}`]: { alignSelf: "center" } }}
        />
        <TableContainer id="farm-born-sold" tabIndex={0} role="region" aria-label={copy(pageContract, "section.sold.aria")} sx={cardTableScrollSx}>
          <FarmBornSoldTable
            contract={table(pageContract, "sales-farm-born-sold")}
            rows={data.sold.map((row) => ({ ...row, stage: stageVocabularyLabel(row.stage, stageNames) }))}
            order={order}
            labels={{
              sortAll: copy(pageContract, "table.sort_all"),
              ariaLabel: copy(pageContract, "section.sold.aria"),
              notRecorded: copy(pageContract, "value.not_recorded"),
              noDeal: copy(pageContract, "value.no_deal"),
              sexLabels: Object.fromEntries(
                optionGroup(pageContract, "farm_born_sexes").map((option) => [option.key, option.label]),
              ),
              empty: <EmptyState title={copy(pageContract, "empty.sold")} />,
            }}
          />
        </TableContainer>
        {pageCount > 1 ? (
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows", "Rows")}
            page={pageNumber}
            pageCount={pageCount}
            rangeLabel={`${data.offset + 1}\u2013${Math.min(data.offset + data.limit, data.total_sold)} ${copy(pageContract, "pager.of")} ${num(data.total_sold)} ${copy(pageContract, "pager.noun")}`}
            prevHref={pageHref(Math.max(0, data.offset - data.limit))}
            nextHref={pageHref(data.offset + data.limit)}
            prevLabel={copy(pageContract, "action.previous")}
            nextLabel={copy(pageContract, "action.next")}
            denseTargetId="farm-born-sold"
          />
        ) : null}
      </Card>
      </div>
    </>
  );
}

/**
 * Farm born (maintainer request 2026-09-18): every animal the register marks born on this farm
 * (goats.origin_type = 'birth'; maintainer decision 2026-09-22, superseding the 2026-09-19
 * not-on-a-load rule) -- the counterpart of Load wise. How many are on the farm today, how many
 * sold in the chosen period, which breed / sex / stage / pen the sold ones came from, and what
 * they earned, behind a filter bar (period, park, pen, species, breed, sex, stage) that governs
 * the whole page.
 *
 * The period binds the SOLD side only (maintainer decision, same day): the on-farm count is
 * today's whatever the period. Read-only by contract, the /sales/sold shape: the page declares
 * no write control, so nothing here opens a form. Every figure and every label is the backend's.
 */
export async function SalesFarmBornPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const today = todayIso();
  const defaultFrom = monthBefore(today);
  const defaultTo = today;

  // Every filter value is validated against the SERVED vocabulary before it is trusted: sex and
  // species against the contract's option groups here, the park / pen / breed / stage against the
  // payload's own options once it arrives (an unknown value simply matches nothing, which the
  // backend reports honestly as zero rows).
  const sexOptions = optionGroup(pageContract, "farm_born_sexes");
  const rawSex = one(sp, "sex") ?? "";
  const sex = sexOptions.some((option) => option.key === rawSex) ? rawSex : "";
  const speciesOptions = optionGroup(pageContract, "farm_born_species");
  const rawSpecies = one(sp, "species") ?? "";
  const species = speciesOptions.some((option) => option.key === rawSpecies) ? rawSpecies : "";
  // Park scope: the SHELL's `park` (one filter across every Sales page), chosen on the farm chips
  // above the filter bar rather than a park select inside it. Validated against the caller's
  // parks, so a hand-edited uuid reads as every farm rather than as a farm nobody chose.
  const { parkId: park, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);
  const pen = one(sp, "pen") ?? "";
  const breed = one(sp, "breed") ?? "";
  const stage = one(sp, "stage") ?? "";
  const from = one(sp, "from") ?? "";
  const to = one(sp, "to") ?? "";

  const pageSizes = tablePageSizes(pageContract, "sales-farm-born-sold");
  const defaultLimit = pageSizes[0] ?? FALLBACK_LIMIT;
  const limit = resolveLimit(one(sp, "limit"), pageSizes, defaultLimit);
  const offset = boundedInt(one(sp, "offset"), 0, 0, MAX_OFFSET);
  const penOffset = boundedInt(one(sp, PEN_OFFSET_PARAM), 0, 0, MAX_OFFSET);

  // The sold ledger's whole-result order, validated against the contract's sortable columns.
  const order = tableOrderFromParams(sp, table(pageContract, "sales-farm-born-sold"));
  // serial-await: allow farm-born read depends on readSalesParkScope validating the shell park.
  const [result, stageResult] = await Promise.all([
    getFarmBornSales({
      from: from || undefined,
      to: to || undefined,
      park_id: park || undefined,
      pen: pen || undefined,
      species: species || undefined,
      breed: breed || undefined,
      sex: sex || undefined,
      stage: stage || undefined,
      limit,
      offset,
      sort: order.sort || undefined,
      dir: order.sort ? order.dir : undefined,
    }),
    // Tenant stage vocabulary, so the fattening family shows its configured name rather than
    // its "F2" code (lib/stage-display). Values stay the code; only the words change.
    listAnimalStages(),
  ]);
  if (firstAuthRequiredError(result, stageResult)) redirect(INTERNAL_LOGIN_PATH);
  const stageNames = stageNameMap(stageResult.ok ? stageResult.data.items : undefined);

  const options = result.ok ? result.data.options : null;
  // The pen list follows the park select: a pen belongs to one park, so with a park chosen only
  // its pens are offered, and a park change clears a pen that would no longer be in the list.
  const penOptions = (options?.pens ?? []).filter((option) => !park || option.park_id === park);
  // Pen names repeat across parks (Gandhi 1, Castro 2 exist in both), so on the All-parks view
  // each pen option also names its park; with a park chosen the name alone is unambiguous.
  const parkLabel = new Map((options?.parks ?? []).map((option) => [option.key, option.label]));
  const penOptionLabel = (option: { label: string; park_id?: string }) =>
    park || !option.park_id ? option.label : `${option.label} · ${parkLabel.get(option.park_id) ?? ""}`;
  const served = result.ok ? result.data.summary : null;

  const filterFields: WorklistFilterField[] = [
    {
      kind: "daterange",
      param: "from",
      toParam: "to",
      label: copy(pageContract, "filter.period.label"),
      // The window the BACKEND applied, so the control shows what is on screen.
      from: served?.from ?? (from || defaultFrom),
      to: served?.to ?? (to || defaultTo),
      today,
      defaultFrom,
      defaultTo,
      labels: {
        // The bar's own label says what the dates bind ("Sold between"); the picker's inner field
        // name stays the short word so the control does not read the sentence twice.
        field: copy(pageContract, "filter.period.field"),
        today: copy(pageContract, "filter.period.today"),
        single: copy(pageContract, "filter.period.single"),
        range: copy(pageContract, "filter.period.range"),
        aria: copy(pageContract, "filter.period.aria"),
        previousMonth: copy(pageContract, "filter.period.previous_month"),
        nextMonth: copy(pageContract, "filter.period.next_month"),
        rangeStartHint: copy(pageContract, "filter.period.range_start_hint"),
        rangeEndHint: copy(pageContract, "filter.period.range_end_hint"),
        rangeSeparator: copy(pageContract, "filter.period.range_separator"),
      },
    },
    {
      kind: "select",
      param: "pen",
      label: copy(pageContract, "filter.pen.label"),
      value: pen,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: penOptions.map((option) => ({ value: option.key, label: penOptionLabel(option) })),
    },
    {
      kind: "select",
      param: "species",
      label: copy(pageContract, "filter.species.label"),
      value: species,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: speciesOptions.map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "breed",
      label: copy(pageContract, "filter.breed.label"),
      value: breed,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: (options?.breeds ?? []).map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "sex",
      label: copy(pageContract, "filter.sex.label"),
      value: sex,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: sexOptions.map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "stage",
      label: copy(pageContract, "filter.stage.label"),
      value: stage,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: (options?.stages ?? []).map((option) => ({ value: option.key, label: stageVocabularyLabel(option.label, stageNames) })),
    },
  ];

  const pageHref = (nextOffset: number) =>
    hrefWith(sp, {
      offset: nextOffset > 0 ? String(nextOffset) : null,
      limit: limit === defaultLimit ? null : String(limit),
    });
  const penHref = (nextOffset: number) => hrefWith(sp, { [PEN_OFFSET_PARAM]: nextOffset > 0 ? String(nextOffset) : null });

  return (
    <div className="kit-enter screen on sales-farm-born-page">
      <SalesPageHeader pageContract={pageContract} subtitle={false} />

      {/* A park change drops the pen (a pen belongs to one park) and every page offset. */}
      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={park}
        parks={parks}
        clears={["pen", "offset", PEN_OFFSET_PARAM]}
      />

      {/* The filters are STAGED and committed on one Apply (flicker fix, 2026-09-25): every pick used
          to run the whole page again and dim all of it. Only the bar says it is busy now. */}
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={filterFields}
        pageContract={pageContract}
        deferApply
        holdChildren={false}
      >
        {/* The sections (guard: url-keyed-panel): a farm / filter / sort / page change swaps them to
            their skeleton at once; header, farm chips and the filter bar stay on screen. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<PanelSkeleton kpis={4} charts={2} table={limit} />}>
        {!result.ok ? (
          <Alert severity="error" sx={{ mb: 1.75 }}>
            {salesErrorText(result.error, copy(pageContract, "error.load"))}
          </Alert>
        ) : (
          <FarmBornSections data={result.data} pageContract={pageContract} pageHref={pageHref} penOffset={penOffset} penHref={penHref} stageNames={stageNames} order={order} />
        )}
        </UrlSuspense>
      </WorklistFilters>
    </div>
  );
}
