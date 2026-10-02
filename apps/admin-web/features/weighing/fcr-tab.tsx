import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";

import { EmptyState } from "@/components/app/empty-state";
import { WorklistPager } from "@/components/worklist-pager";
import { ColumnChartCard } from "@/components/app/column-chart-card";
import { BalanceStatisticsCard } from "@/components/app/balance-statistics-card";
import { KpiWidget } from "@/components/app/kpi-widget";
import { FCRPensChart } from "./fcr-pens-chart";
import { FCRPensTable } from "./fcr-pens-table";
import { cohortWord } from "./fcr-labels";
import { copy, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { GrowthFCRGroup, GrowthFCRResponse, GrowthFCRPen } from "@/lib/api/server";
import { weightsSexChoices } from "./sex-filter-contract";

/**
 * ADG Analytics › FCR (maintainer request 2026-09-07) — feed conversion ratio: kilograms of feed
 * directed to a pen per kilogram the pen gained over the same days. Lower is better.
 *
 * ONE backend read (`/growth-director/fcr`) carries every figure on this tab: the KPI strip, each
 * pen, every cut and the weekly series are computed by the backend over the whole filter, and this
 * component divides nothing. It does not even compute a percentage: weighing has no expected
 * denominator, and a ratio this tab might invent from two served figures would disagree with the
 * one the backend already published.
 *
 * Absence is load-bearing. A pen weighed once, a pen whose sheet has no rows, and a pen that did
 * not gain each have NO honest ratio; they stay in the table with their status and out of every
 * chart, and the caption says so.
 */

const num = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

/** These figures have no series behind them, so the widget draws no sparkline. */

/** "1 pen", "12 pens": the count and the right word, both from the page contract. */
function penCount(pageContract: AdminUiPageContract, n: number): string {
  return `${n.toLocaleString("en-IN")} ${copy(pageContract, n === 1 ? "value.fcr.pen" : "value.fcr.pens")}`;
}

function groupLabel(pageContract: AdminUiPageContract, group: GrowthFCRGroup, id: string): string {
  // "Mixed" names a different fact on each card: several breeds, both sexes, or bought and born.
  if (group.key === "mixed" && id === "breed") return copy(pageContract, "label.fcr.mixed_breed");
  if (group.key === "mixed" && id === "sex") return copy(pageContract, "label.fcr.mixed_sex");
  switch (group.key) {
    case "male":
      return copy(pageContract, "view.sex.male");
    case "female":
      return copy(pageContract, "view.sex.female");
    case "mixed":
      return copy(pageContract, "label.fcr.mixed");
    case "unknown":
      return copy(pageContract, "label.fcr.unknown");
    case "farm_born":
      return copy(pageContract, "label.fcr.farm_born");
    case "procured_no_load":
      return copy(pageContract, "label.fcr.procured_no_load");
    case "procured_load":
      return copy(pageContract, "label.fcr.procured_load");
    default:
      // A gender the farm added on Configuration is named from its list, never shown as a code.
      if (id === "sex") {
        const configured = weightsSexChoices(pageContract).find((choice) => choice.value === group.key);
        if (configured) return configured.label;
      }
      return group.label;
  }
}

/**
 * One FCR cut (breed, sex, band, park, origin) on the template balance-statistics card: the ratio
 * and the money made over feed are two selectable series, never one axis (kg/kg and rupees are not
 * comparable lengths). A group with no honest ratio is left out; a group whose gain or feed is
 * unpriced has no money bar (null), never a zero one. The pen count rides in each bar's tooltip.
 */
function GroupCard({
  pageContract,
  id,
  groups,
  rupee,
}: {
  pageContract: AdminUiPageContract;
  id: string;
  groups: GrowthFCRGroup[];
  rupee: string;
}) {
  const measured = (groups ?? []).filter((group): group is GrowthFCRGroup & { fcr: number } => group.fcr != null);
  const categories = measured.map((group) => groupLabel(pageContract, group, id));
  const notes = measured.map((group) => penCount(pageContract, group.pens));
  const caption = copy(pageContract, `section.fcr.${id}.caption`, "");
  return (
    <BalanceStatisticsCard
      aria-label={copy(pageContract, `section.fcr.${id}.aria`)}
      title={copy(pageContract, `section.fcr.${id}.title`)}
      subheader={caption || undefined}
      empty={<EmptyState title={copy(pageContract, "empty.fcr.body")} />}
      chart={{
        series: [
          {
            name: copy(pageContract, "series.fcr"),
            categories,
            unit: copy(pageContract, "unit.fcr"),
            digits: 2,
            data: [{ name: copy(pageContract, "series.fcr"), data: measured.map((group) => Number(group.fcr.toFixed(2))), notes }],
          },
          ...(measured.some((group) => group.margin_inr != null)
            ? [
                {
                  name: copy(pageContract, "legend.fcr.margin"),
                  categories,
                  unit: `${rupee}·`,
                  data: [
                    {
                      name: copy(pageContract, "legend.fcr.margin"),
                      data: measured.map((group) => (group.margin_inr == null ? null : Math.round(group.margin_inr))),
                      notes,
                    },
                  ],
                },
              ]
            : []),
        ],
      }}
      sx={{ height: 1 }}
    />
  );
}

export function FCRTab({
  pageContract,
  fcr,
  pager,
}: {
  pageContract: AdminUiPageContract;
  fcr: GrowthFCRResponse | null;
  /** The page's shared offset/limit query params, so the pens table pages like the shed table does. */
  pager: { offset: number; limit: number; pageSizeOptions: readonly number[]; hrefForOffset: (offset: number) => string; hrefForLimit: (limit: number) => string };
}) {
  if (fcr === null) {
    return (
      <Alert severity="error" variant="outlined">
        {copy(pageContract, "error.fcr.body")}
      </Alert>
    );
  }
  const none = copy(pageContract, "kpi.fcr.no_value");
  const rupee = copy(pageContract, "unit.fcr.rupees");
  const priceDefaults = fcr.sale_prices.filter((price) => price.management_stage === "");
  const priceOverrides = fcr.sale_prices.length - priceDefaults.length;
  const s = fcr.summary;
  const money = (value: number | null | undefined) => (value == null ? none : `${rupee}${num(value, 0)}`);
  const visiblePens = fcr.pens.slice(pager.offset, pager.offset + pager.limit);

  const tableLabels = {
    ariaLabel: copy(pageContract, "table.fcr.title"),
    noValue: none,
    mixedBreed: copy(pageContract, "label.fcr.mixed_breed"),
    mixedSex: copy(pageContract, "label.fcr.mixed_sex"),
    unknown: copy(pageContract, "label.fcr.unknown"),
    male: copy(pageContract, "view.sex.male"),
    female: copy(pageContract, "view.sex.female"),
    sexes: Object.fromEntries(weightsSexChoices(pageContract).map((choice) => [choice.value, choice.label])),
    wholePen: copy(pageContract, "label.fcr.whole_pen"),
    scanned: copy(pageContract, "label.fcr.scanned"),
    status: {
      ok: copy(pageContract, "table.fcr.status.ok"),
      blocked: copy(pageContract, "table.fcr.status.blocked"),
      weighed_once: copy(pageContract, "table.fcr.status.weighed_once"),
      no_feed: copy(pageContract, "table.fcr.status.no_feed"),
      no_gain: copy(pageContract, "table.fcr.status.no_gain"),
    },
    unpriced: copy(pageContract, "table.fcr.unpriced"),
    wasted: copy(pageContract, "table.fcr.wasted"),
    rupee,
    empty: copy(pageContract, "empty.fcr.body"),
  };

  // Pens with a ratio as horizontal bars on one FCR scale, in the contract's own order: park
  // clusters (CBE, then CPT) and pens A→Z inside each, the backend's order for every All-parks
  // surface. The cohort words ride in the tooltip -- the same words the table's cohort column uses.
  const ratedPens = fcr.pens.filter((pen): pen is GrowthFCRPen & { fcr: number } => pen.fcr != null);

  // Money by pen: gain value, feed cost and money made on ONE rupee scale, so a loss draws below
  // the baseline. Pens whose gain or feed bill is unpriced show only the half they have (null).
  const moneyPens = ratedPens.filter((pen) => pen.gain_value_inr != null || pen.feed_cost_inr != null);
  const roundOrNull = (value: number | null | undefined) => (value == null ? null : Math.round(value));

  const weeks = fcr.weekly.filter((week): week is typeof week & { fcr: number } => week.fcr != null);

  const fcrTable = table(pageContract, "fcr-pens");

  const kpis = [
    {
      key: "farm",
      title: copy(pageContract, "kpi.fcr.farm.label"),
      total: s.fcr == null ? null : Number(s.fcr.toFixed(2)),
      caption: `${s.fcr == null ? none : copy(pageContract, "kpi.fcr.farm.unit")} · ${penCount(pageContract, s.pens_with_fcr)} · ${s.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.fcr.kids")}`,
    },
    {
      key: "gain",
      title: copy(pageContract, "kpi.fcr.gain_value.label"),
      total: s.gain_value_inr == null ? null : Math.round(s.gain_value_inr),
      caption: `${s.gain_value_inr == null ? none : rupee} · ${num(s.gain_kg, 0)} kg · ${copy(pageContract, "kpi.fcr.gain_value.sub")}`,
    },
    {
      key: "feed",
      title: copy(pageContract, "kpi.fcr.feed_cost.label"),
      total: s.feed_cost_inr == null ? null : Math.round(s.feed_cost_inr),
      caption: `${s.feed_cost_inr == null ? none : rupee} · ${num(s.feed_kg, 0)} kg${s.wastage_kg > 0 ? ` (${num(s.wastage_kg, 0)} ${copy(pageContract, "table.fcr.wasted")})` : ""} · ${money(s.feed_cost_per_kg_gain_inr)} ${copy(pageContract, "kpi.fcr.cost_gain.label").toLowerCase()}`,
    },
    {
      key: "margin",
      title: copy(pageContract, "kpi.fcr.margin.label"),
      total: s.margin_inr == null ? null : Math.round(s.margin_inr),
      caption: `${s.margin_inr == null ? none : rupee} · ${s.margin_inr != null && s.margin_inr < 0 ? copy(pageContract, "kpi.fcr.margin.loss") : copy(pageContract, "kpi.fcr.margin.sub")}`,
    },
    {
      key: "break_even",
      title: copy(pageContract, "kpi.fcr.break_even.label"),
      total: s.break_even_fcr == null ? null : Number(s.break_even_fcr.toFixed(1)),
      caption: s.break_even_fcr == null ? `${none} · ${copy(pageContract, "kpi.fcr.break_even.sub")}` : copy(pageContract, "kpi.fcr.break_even.sub"),
    },
  ];

  return (
    <Grid container spacing={3}>
      {kpis.map((kpi, index) => (
        <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: index < 3 ? 4 : 6 }}>
          <KpiWidget title={kpi.title} total={kpi.total} caption={kpi.caption} sx={{ height: 1 }} />
        </Grid>
      ))}

      {/* The price the gain is valued at: maintainer-edited DATA, printed beside the figures it
          prices, with who set it and when, because a figure priced on an assumption must show it. */}
      <Grid size={12}>
        <Alert severity="info" variant="outlined">
          {priceDefaults.length === 0 ? (
            copy(pageContract, "fcr.price.missing")
          ) : (
            <>
              {/* One line per species, then one line for the stage x sex prices and where else the
                  prices are used: printed as one run-on sentence it read as a paragraph. */}
              {priceDefaults.map((price) => (
                <Box component="span" key={price.species} sx={{ display: "block" }}>
                  {copy(pageContract, "fcr.price.prefix")}{" "}
                  <b>
                    {rupee}
                    {num(price.price_per_kg_inr, 0)}
                  </b>{" "}
                  {copy(pageContract, "fcr.price.per_kg")} ({price.species}) · {copy(pageContract, "fcr.price.set")} {fmtDate(price.effective_from)}
                  {price.set_by ? ` ${copy(pageContract, "fcr.price.by")} ${price.set_by}` : ""}
                </Box>
              ))}
              {/* Stage x sex prices (maintainer decision 2026-09-24) sit on top of these defaults; each
                  animal in a pen is valued at its own, so the caption says how many are in force. */}
              <Box component="span" sx={{ display: "block", color: "text.secondary" }}>
                {priceOverrides > 0 ? `${priceOverrides} ${copy(pageContract, "fcr.price.overrides")} · ` : ""}
                {copy(pageContract, "fcr.price.shared")}
              </Box>
            </>
          )}
        </Alert>
      </Grid>

      <Grid size={{ xs: 12, lg: 7 }}>
        <FCRPensChart
          ariaLabel={copy(pageContract, "section.fcr.pens.aria")}
          title={copy(pageContract, "section.fcr.pens.title")}
          subheader={copy(pageContract, "section.fcr.pens.caption")}
          empty={<EmptyState title={copy(pageContract, "empty.fcr.body")} />}
          categories={ratedPens.map((pen) => pen.operational_location_display)}
          values={ratedPens.map((pen) => Number(pen.fcr.toFixed(2)))}
          notes={ratedPens.map((pen) => `${cohortWord(pen.breed, tableLabels, "breed")} · ${cohortWord(pen.sex, tableLabels, "sex")}`)}
          seriesName={copy(pageContract, "series.fcr")}
          unit={copy(pageContract, "unit.fcr")}
          // The caption promises a dashed break-even line across every pen's bar, so a pen past it
          // reads as losing money without comparing two numbers.
          breakEven={s.break_even_fcr}
          breakEvenLabel={
            s.break_even_fcr == null
              ? ""
              : `${copy(pageContract, "label.fcr.break_even")}: ${num(s.break_even_fcr, 1)} ${copy(pageContract, "unit.fcr")}`
          }
          offScaleLabel={copy(pageContract, "label.fcr.off_scale")}
        >
          <Typography variant="body2" sx={{ px: 3, pb: 3, color: "text.secondary" }}>
            {copy(pageContract, "note.fcr.excluded")}
          </Typography>
        </FCRPensChart>
      </Grid>
      <Grid size={{ xs: 12, lg: 5 }}>
        <ColumnChartCard
          aria-label={copy(pageContract, "section.fcr.weekly.aria")}
          title={copy(pageContract, "section.fcr.weekly.title")}
          subheader={copy(pageContract, "section.fcr.weekly.caption")}
          empty={<EmptyState title={copy(pageContract, "empty.fcr.body")} />}
          chart={{
            categories: weeks.map((week) => fmtDate(week.week_start)),
            unit: copy(pageContract, "unit.fcr"),
            digits: 2,
            series: [
              {
                name: copy(pageContract, "series.fcr"),
                data: weeks.map((week) => Number(week.fcr.toFixed(2))),
                notes: weeks.map((week) => penCount(pageContract, week.pens)),
              },
            ],
          }}
          sx={{ height: 1 }}
        />
      </Grid>

      <Grid size={12}>
        <BalanceStatisticsCard
          aria-label={copy(pageContract, "section.fcr.money.aria")}
          title={copy(pageContract, "section.fcr.money.title")}
          subheader={copy(pageContract, "section.fcr.money.caption")}
          empty={<EmptyState title={copy(pageContract, "empty.fcr.body")} />}
          chart={{
            series: [
              {
                name: copy(pageContract, "section.fcr.money.title"),
                categories: moneyPens.map((pen) => pen.operational_location_display),
                unit: `${rupee}·`,
                data: [
                  { name: copy(pageContract, "legend.fcr.gain_value"), data: moneyPens.map((pen) => roundOrNull(pen.gain_value_inr)) },
                  { name: copy(pageContract, "legend.fcr.feed_cost"), data: moneyPens.map((pen) => roundOrNull(pen.feed_cost_inr)) },
                  { name: copy(pageContract, "legend.fcr.margin"), data: moneyPens.map((pen) => roundOrNull(pen.margin_inr)) },
                ],
              },
            ],
          }}
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <GroupCard pageContract={pageContract} id="breed" groups={fcr.estimated_by_breed ?? fcr.by_breed} rupee={rupee} />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <GroupCard pageContract={pageContract} id="sex" groups={fcr.by_sex} rupee={rupee} />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <GroupCard pageContract={pageContract} id="band" groups={fcr.by_weight_band} rupee={rupee} />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <GroupCard pageContract={pageContract} id="park" groups={fcr.by_park} rupee={rupee} />
      </Grid>
      <Grid size={12}>
        <GroupCard pageContract={pageContract} id="origin" groups={fcr.by_origin} rupee={rupee} />
      </Grid>

      <Grid size={12}>
        <Card aria-label={copy(pageContract, "table.fcr.aria")}>
          <CardHeader title={copy(pageContract, "table.fcr.title")} subheader={copy(pageContract, "table.fcr.caption")} sx={{ mb: 3 }} />
          {/* Paged on the page's shared offset/limit, exactly like the pens table on the General tab:
              the rows are one read, the window is a query param, and the pager is the shared one. */}
          <FCRPensTable contract={fcrTable} rows={visiblePens} labels={tableLabels} />
          <WorklistPager
            pageContract={pageContract}
            offset={pager.offset}
            limit={pager.limit}
            rowCount={visiblePens.length}
            hasMore={pager.offset + visiblePens.length < fcr.pens.length}
            noun={copy(pageContract, "pager.noun")}
            pageSizeOptions={pager.pageSizeOptions}
            hrefForOffset={pager.hrefForOffset}
            hrefForLimit={pager.hrefForLimit}
          />
          <Box sx={{ px: 3, pb: 3, display: "flex", flexDirection: "column", gap: 1 }}>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "note.fcr.basis")}
            </Typography>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "note.fcr.filters")}
            </Typography>
          </Box>
        </Card>
      </Grid>
    </Grid>
  );
}
