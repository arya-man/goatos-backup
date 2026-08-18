import { redirect } from "next/navigation";
import { Scale, TrendingDown, Warehouse } from "lucide-react";

import { WeightBars } from "./weight-bars";
import { SegmentedLinks } from "./segmented-links";
import { GrowthDirectorSection } from "./growth-director";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { istDayPlus, todayIso } from "@/lib/format";
import {
  firstAuthRequiredError,
  getGrowthDirector,
  getShedWeights,
  getWeighingGrowth,
  getWeightDemographics,
  type ShedWeightsRow,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";

const PAGE_PATH = "/weighing/weights";
const WINDOW_FROM_PARAM = "wt_from";
const WINDOW_TO_PARAM = "wt_to";
const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;
const DEFAULT_LIMIT = 10;

function boundedLimit(raw: string | undefined): number {
  const parsed = Number(raw);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number])
    ? parsed
    : DEFAULT_LIMIT;
}

function boundedOffset(raw: string | undefined): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 5000 ? parsed : 0;
}

function hrefWith(searchParams: RouteSearchParams, updates: Record<string, string | null>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null) next.delete(key);
    else next.set(key, value);
  }
  const query = next.toString();
  return query ? `${PAGE_PATH}?${query}` : PAGE_PATH;
}

// The window the page lands on: the 30 days before today, inclusive of both ends (maintainer,
// 2026-08-12). It replaces the fixed "Last 4 weeks / Last 12 weeks" select, which could only answer
// the two questions someone thought of in advance — a reader comparing one drive week against
// another had no way to ask.
const DEFAULT_WINDOW_DAYS = 30;
const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

/**
 * The selected inclusive window, both ends "YYYY-MM-DD" Asia/Kolkata business dates, which is what
 * the API's from/to expect.
 *
 * Malformed, inverted or absent parameters fall back to the default window rather than throwing: a
 * hand-edited URL must not take the page down. A future end is clamped to today, because a weigh
 * cannot have happened tomorrow and the reads would return an empty span for it.
 */
function selectedWindow(params: RouteSearchParams, today: string): { from: string; to: string } {
  const rawFrom = one(params, WINDOW_FROM_PARAM)?.trim();
  const rawTo = one(params, WINDOW_TO_PARAM)?.trim();
  if (rawFrom && rawTo && BUSINESS_DAY.test(rawFrom) && BUSINESS_DAY.test(rawTo) && rawFrom <= rawTo) {
    return { from: rawFrom > today ? today : rawFrom, to: rawTo > today ? today : rawTo };
  }
  return defaultWindow(today);
}

function defaultWindow(today: string): { from: string; to: string } {
  return { from: istDayPlus(today, -(DEFAULT_WINDOW_DAYS - 1)), to: today };
}

function kg(value: number, fractionDigits = 1): string {
  return value.toLocaleString("en-IN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  });
}

// A whole-shed weigh cannot say how many of its kids cleared a weight threshold, and can never
// produce a per-animal figure. The mode has to be visible on every row, or a reader reasonably
// assumes both kinds of row answer the same questions.
function modeTag(row: ShedWeightsRow, pageContract: AdminUiPageContract) {
  return row.weighing_category === "individual_animal"
    ? { tone: "info" as const, label: copy(pageContract, "value.weighing.individual") }
    : { tone: "mut" as const, label: copy(pageContract, "value.weighing.lump") };
}

// One toggle per chart, rendered as links so the page stays a server component and
// each chart's choice survives a reload and a shared URL. Defined at module scope:
// declaring a component inside render recreates its type every pass.
function MetricToggle({
  param,
  current,
  params,
  pageContract,
}: {
  param: string;
  current: "weight" | "adg";
  params: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  return (
    <SegmentedLinks
      current={current}
      options={(["adg", "weight"] as const).map((option) => ({
        value: option,
        label: copy(pageContract, option === "adg" ? "metric.gain" : "metric.weight"),
        href: hrefWith(params, { [param]: option }),
      }))}
    />
  );
}

// One row of the full-width shed chart: a WeightBars bar plus the park it belongs to.
type ShedChartBar = { key: string; park_name: string; label: string; value: number };

function shedKey(locationID: string, partitionLabel?: string | null): string {
  return `${locationID}|${partitionLabel ?? ""}`;
}

function compositionLabel(
  chip: { breed?: string; sex?: string; animals: number },
  pageContract: AdminUiPageContract,
): string {
  const breed = chip.breed?.trim() || copy(pageContract, "composition.unknown_breed");
  const sex = chip.sex?.trim() || copy(pageContract, "composition.unknown_sex");
  return `${breed} · ${sex}`;
}

function shedLabelWithComposition(
  shedName: string,
  composition: { chips: readonly { breed?: string; sex?: string; animals: number }[] } | undefined,
  pageContract: AdminUiPageContract,
): string {
  const chips = composition?.chips ?? [];
  if (chips.length === 0) return shedName;
  const suffix = chips.map((chip) => compositionLabel(chip, pageContract).replace(" · ", " - ")).join(", ");
  return `${shedName} (${suffix})`;
}

/**
 * The shed chart's two columns. With rows from more than one park, each park gets its
 * own column (heading = park), so the two farms stop interleaving in one long list.
 * With one park — the park filter, or a period only one park weighed in — the single
 * park's ranked list is split in half across both columns instead of leaving the
 * right half of a full-width card empty; the ranking reads down the left column and
 * continues down the right. Rows arrive sorted best-first and grouping preserves
 * that order.
 *
 * Grouped by park NAME, not id, because the growth leaderboard rows carry no park id
 * — the contract requires `park_name` on both series specifically so they name a
 * park identically. Parks are unique by name within a tenant (unlike sheds, 39 of
 * which exist in both parks), so name-grouping cannot merge two parks here.
 */
function shedChartColumns(
  rows: readonly ShedChartBar[],
): { heading: string; rows: ShedChartBar[] }[] {
  const byPark = new Map<string, ShedChartBar[]>();
  for (const row of rows) {
    const list = byPark.get(row.park_name);
    if (list) list.push(row);
    else byPark.set(row.park_name, [row]);
  }
  // Sorted so the column order is stable across reloads and metric toggles — Map
  // order would follow whichever park happens to hold the fastest pen today.
  const parkNames = [...byPark.keys()].sort();
  if (parkNames.length >= 2) {
    return parkNames.map((name) => ({ heading: name, rows: byPark.get(name) as ShedChartBar[] }));
  }
  if (parkNames.length === 1) {
    const all = byPark.get(parkNames[0]) as ShedChartBar[];
    const half = Math.ceil(all.length / 2);
    const right = all.slice(half);
    // A one-row list gets one column: a second column holding an empty-state note
    // would read as "this park has a problem", which is not what an empty slice means.
    return right.length === 0
      ? [{ heading: parkNames[0], rows: all }]
      : [
          { heading: parkNames[0], rows: all.slice(0, half) },
          { heading: parkNames[0], rows: right },
        ];
  }
  return [];
}

export async function WeighingWeightsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const params = searchParams ?? {};
  const parkFilter = one(params, "park") ?? "";
  const modeFilter = one(params, "weighing") ?? "all";
  const limit = boundedLimit(one(params, "limit"));
  const offset = boundedOffset(one(params, "offset"));
  const losingOffset = boundedOffset(one(params, "losing_offset"));
  // Each chart toggles independently. Default is DAILY GAIN, not weight — the
  // question the screen exists to answer is whether the kids are growing.
  const metric = (name: string) => (one(params, name) === "weight" ? "weight" : "adg");
  const shedMetric = metric("shed_metric");
  const breedMetric = metric("breed_metric");
  const sexMetric = metric("sex_metric");
  const stageMetric = metric("stage_metric");
  const loadMetric = metric("load_metric");

  // The window is business DAYS, not a clock offset: a weigh belongs to the Asia/Kolkata day it
  // happened on.
  const today = todayIso();
  const window = selectedWindow(params, today);

  const [weights, growth, demographics, growthDirector] = await Promise.all([
    getShedWeights({ park_id: parkFilter || undefined, ...window }),
    getWeighingGrowth({ park_id: parkFilter || undefined, ...window }),
    getWeightDemographics({ park_id: parkFilter || undefined, ...window }),
    getGrowthDirector({ park_id: parkFilter || undefined, ...window }),
  ]);

  if (firstAuthRequiredError(weights, growth, demographics, growthDirector))
    redirect(INTERNAL_LOGIN_PATH);

  if (!weights.ok) {
    return (
      <section className="card">
        <h2 className="h">{copy(pageContract, "error.load.title")}</h2>
        <p className="muted small">{copy(pageContract, "error.load.body")}</p>
      </section>
    );
  }

  const { rows, summary, parks, period_start: periodStart, period_end: periodEnd } = weights.data;

  // Mode narrowing changes the TABLE and CHART only. The KPI cards keep reporting the backend's
  // whole-filter truth — recomputing a card from the visible slice is the capped read-time rollup
  // anti-pattern and would make the cards disagree with the table.
  const weighedRows = rows.filter((row) => row.animals_weighed > 0);
  const visibleRows =
    modeFilter === "all" ? weighedRows : weighedRows.filter((row) => row.weighing_category === modeFilter);
  const slice = visibleRows.slice(offset, offset + limit);
  const weighedRowKeys = new Set(weighedRows.map((row) => shedKey(row.location_id, row.partition_label)));
  const visibleRowKeys = new Set(visibleRows.map((row) => shedKey(row.location_id, row.partition_label)));

  // Losing kids come from the growth read, which already computes "latest pair went down".
  // A growth failure must not take the whole page down: the weights half is independent.
  // Per-park gain cards. This fans out one call per PARK, which is a handful of
  // rows (two today), never a paginated entity list — the banned shape is draining
  // a cursor, not asking a bounded vocabulary. Skipped entirely when the caller
  // already narrowed to one park, because the headline above is then that park's.
  const perParkGain =
    parkFilter === "" && parks.length > 1
      ? await Promise.all(
          parks.map(async (park) => {
            const result = await getWeighingGrowth({ park_id: park.park_id, ...window });
            // Both ways of weighing count. A park is not "no data" because its kids
            // were weighed as sheds rather than one by one — the weight moved either
            // way, and a card that ignores half the estate reports a park with 140
            // measured animals as unknown.
            //
            // Combined as a mean weighted by ANIMALS, so a 73-head shed moving 206
            // g/day counts for more than one kid moving -60. The two inputs are not
            // the same measurement: a per-kid figure is that animal growing, while a
            // shed figure also moves when animals join or leave. The card names both
            // in its subtitle rather than implying one.
            const parts: Array<{ value: number; weight: number }> = [];
            if (result.ok) {
              for (const shed of result.data.shed_leaderboard) {
                if (shed.adg_pair_count > 0 && weighedRowKeys.has(shedKey(shed.location_id, shed.partition_label))) {
                  parts.push({ value: shed.median_adg_g_per_day, weight: shed.adg_pair_count });
                }
              }
            }
            for (const row of rows) {
              if (row.park_id === park.park_id && row.shed_average_gain_g_per_day != null) {
                parts.push({
                  value: row.shed_average_gain_g_per_day,
                  weight: Math.max(row.animals_weighed, 1),
                });
              }
            }
            const totalWeight = parts.reduce((sum, part) => sum + part.weight, 0);
            return {
              name: park.name,
              median:
                totalWeight > 0
                  ? parts.reduce((sum, part) => sum + part.value * part.weight, 0) / totalWeight
                  : null,
              animals: totalWeight,
            };
          }),
        )
      : [];

  // Biggest loss first: the kid that dropped most is the one to go and look at.
  // Sorted on the CHANGE, not the daily rate, because that is what the table shows
  // and a reader ordering by an unshown column has no way to check the order.
  const losingAll = (growth.ok ? growth.data.losing_animals : [])
    .slice()
    .sort(
      (a, b) =>
        a.latest_weight_kg - a.previous_weight_kg - (b.latest_weight_kg - b.previous_weight_kg),
    );
  const losingSlice = losingAll.slice(losingOffset, losingOffset + DEFAULT_LIMIT);

  // The gain card's own scope label. Never the raw park id — that would put an internal identifier
  // in front of a CEO; an unresolvable filter simply falls back to the all-parks wording.
  const selectedParkName = parks.find((park) => park.park_id === parkFilter)?.name ?? "";

  const modeOptions = optionGroup(pageContract, "weighing_mode");
  const filterFields: WorklistFilterField[] = [
    {
      kind: "select",
      param: "park",
      label: copy(pageContract, "filter.park.label"),
      value: parkFilter,
      allowAll: true,
      options: parks.map((park) => ({ value: park.park_id, label: park.name })),
    },
    {
      kind: "daterange",
      param: WINDOW_FROM_PARAM,
      toParam: WINDOW_TO_PARAM,
      label: copy(pageContract, "filter.period.label"),
      from: window.from,
      to: window.to,
      today,
      // Landing on this window clears both parameters, so a shared link keeps meaning "the last 30
      // days" rather than freezing on the month it was copied in. Named fields, never a spread of
      // defaultWindow(): `{...{from,to}}` would silently overwrite the SELECTED window above with
      // the default and pin the page to 30 days whatever the reader picked.
      defaultFrom: defaultWindow(today).from,
      defaultTo: defaultWindow(today).to,
      labels: {
        field: copy(pageContract, "filter.period.label"),
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
      // allowAll:false because this vocabulary ALREADY carries its own "All" (`weighing_mode`
      // option `all`, which is also this filter's default value). With the bar's generic blank
      // option added on top, the control listed "All" TWICE and the two did not agree: the real
      // one shows every row, while the blank one set `weighing=` — a value no row's category
      // matches — and silently halved the table. One "All", and it is the backend's.
      kind: "select",
      param: "weighing",
      label: copy(pageContract, "filter.weighing.label"),
      value: modeFilter,
      allowAll: false,
      options: modeOptions.map((option) => ({ value: option.key, label: option.label })),
    },
  ];

  const shedColumns = tableLabels(pageContract, "shed-weights");
  const losingColumns = tableLabels(pageContract, "losing-kids");

  const demo = demographics.ok ? demographics.data : null;
  const compositionByShed = new Map(
    (demo?.shed_composition ?? []).map((item) => [shedKey(item.location_id, item.partition_label), item]),
  );

  const chartData = visibleRows
    .filter((row) => row.animals_weighed > 0)
    .slice()
    .sort((a, b) => b.average_weight_kg - a.average_weight_kg)
    .map((row) => {
      const key = shedKey(row.location_id, row.partition_label);
      const shedName = row.operational_location_display || row.shed_display_name;
      return {
        key,
        // The park is carried as its own field, not a label prefix: the shed chart
        // renders one column per park, and the column heading names the park once
        // instead of every row repeating it. 39 shed names exist in BOTH parks, so
        // the park must still travel with the row as data.
        park_name: row.park_name,
        label: shedLabelWithComposition(shedName, compositionByShed.get(key), pageContract),
        value: Number(row.average_weight_kg.toFixed(1)),
      };
    });
  // The headline blends the same two inputs the per-park cards do, weighted by
  // animals. Leaving it on per-animal pairs alone made it contradict its own park
  // cards on screen — "all parks -60 g" sitting above "CPT 118 g" and "CBE 187 g".
  const headlineParts: Array<{ value: number; weight: number }> = [];
  if (growth.ok) {
    for (const shed of growth.data.shed_leaderboard) {
      if (shed.adg_pair_count > 0 && weighedRowKeys.has(shedKey(shed.location_id, shed.partition_label))) {
        headlineParts.push({ value: shed.median_adg_g_per_day, weight: shed.adg_pair_count });
      }
    }
  }
  for (const row of rows) {
    if (row.shed_average_gain_g_per_day != null) {
      headlineParts.push({
        value: row.shed_average_gain_g_per_day,
        weight: Math.max(row.animals_weighed, 1),
      });
    }
  }
  const headlineWeight = headlineParts.reduce((sum, part) => sum + part.weight, 0);
  const headlineGain =
    headlineWeight > 0
      ? headlineParts.reduce((sum, part) => sum + part.value * part.weight, 0) / headlineWeight
      : null;

  // Daily gain per shed comes from the growth read's own shed leaderboard, which is already
  // restricted to per-animal sheds — a whole-shed total can never produce a per-kid gain.
  // Two different measurements share this chart, and the caption says so. A
  // per-animal shed reports the median of its kids' own gains. A whole-shed weigh
  // has no per-animal gain at all, so it reports how fast its AVERAGE is moving —
  // which population change also moves. Merging them silently would be the defect;
  // showing only the first would drop every whole-shed shed from a gain view they
  // now have real history for.
  const gainChartData = [
    ...(growth.ok ? growth.data.shed_leaderboard : [])
      .filter((shed) => shed.adg_pair_count > 0 && visibleRowKeys.has(shedKey(shed.location_id, shed.partition_label)))
      .map((shed) => ({
        // Keyed by location AND partition, because that is the grain the leaderboard is
        // grouped at (`GROUP BY location_id, partition_label` in growth.go). A partitioned
        // shed returns one row PER PEN under one shared location_id -- Mandela 1 returns ten
        // -- so keying on location_id alone gave nine React children the same key, which is
        // a duplicate-key crash, not a cosmetic warning. Matches the sibling series below and
        // the shed-weights chart above, both of which already key on the pair.
        key: shedKey(shed.location_id, shed.partition_label),
        // The park travels as a FIELD, never a label prefix (it used to be one): 39
        // shed names exist in BOTH parks, so "Mandela 1 - Part 5" alone names two
        // different pens — but the chart now renders one column per park and the
        // column heading names the park once. `park_name` is required on both series
        // by contract precisely so they group identically here.
        // `operational_location_display` is the canonical shed+pen string;
        // `display_name` is the weighing bucket's free-text planning label, which has
        // held "M1P5" and "C1" for sheds whose real names are "Mandela 1 - Part 5"
        // and "Castro 1".
        park_name: shed.park_name,
        label: shed.operational_location_display || shed.display_name,
        value: Math.round(shed.median_adg_g_per_day),
      })),
    ...visibleRows
      .filter((row) => row.shed_average_gain_g_per_day != null)
      .map((row) => ({
        key: `${shedKey(row.location_id, row.partition_label)}-shed`,
        park_name: row.park_name,
        // The label on the DAILY-GAIN view is the SHED NAME AND NOTHING ELSE (maintainer
        // instruction, 2026-08-18). It has carried two different suffixes: first the
        // measurement span, naming which of this chart's two measurements the row is and over
        // how many days, and then the breed/sex composition that replaced it. Both are real
        // context -- Channapatna's Castro 2 reads +1,532 g/day across a 2-day gap, which is
        // 1.5 kg per kid per day and impossible -- but this is the one chart whose rows are
        // already thirty-character park+shed+pen strings, and a suffix present on some rows
        // and absent on others reads as a difference between the SHEDS rather than between
        // the measurements. The caption still states that the chart mixes the two.
        //
        // The WEIGHT view of this same card KEEPS its composition suffix, so nothing landed on
        // main is deleted -- flip this one line to put it back on gain too.
        label: row.operational_location_display || row.shed_display_name,
        value: Math.round(row.shed_average_gain_g_per_day as number),
      })),
  ].sort((a, b) => b.value - a.value);

  const hasAnyData = summary.animals_weighed > 0;

  // The full-width shed chart's columns for the active metric, on ONE shared scale:
  // computed across every column's rows before the split, so a bar's length means the
  // same thing whichever column it lands in.
  const shedChartBars: readonly ShedChartBar[] = shedMetric === "adg" ? gainChartData : chartData;
  const shedChartCols = shedChartColumns(shedChartBars);
  const shedChartSize = shedChartBars.length <= 8 ? "short" : "tall";
  const shedChartDomain = {
    lo: Math.min(0, ...shedChartBars.map((bar) => bar.value)),
    hi: Math.max(0, ...shedChartBars.map((bar) => bar.value)),
  };

  // A dimension has a weight series and, separately, a gain series over the smaller
  // set of animals weighed twice. Selecting between them here keeps the two
  // populations from being conflated in one row.
  const dimensionBars = (
    kind: "weight" | "adg",
    weightBuckets: readonly { label: string; average_weight_kg: number }[],
    gainBuckets: readonly { label: string; median_gain_g_per_day: number }[],
  ) =>
    kind === "weight"
      ? weightBuckets.map((b) => ({ key: b.label, label: b.label, value: Number(b.average_weight_kg.toFixed(1)) }))
      : gainBuckets.map((b) => ({ key: b.label, label: b.label, value: Math.round(b.median_gain_g_per_day) }));

  // Growth per purchase load. The supplier is part of the label rather than a
  // separate chart: the load number alone means nothing to a reader, and the
  // question being asked is really about the supplier behind it.
  //
  // On the gain view a load with no second weigh is DROPPED rather than plotted at
  // zero, which would read as "this supplier's kids are flat" when the truth is
  // "nobody has weighed them twice yet". The span rides on the label for the same
  // reason it does on the shed chart — a figure drawn from two days deserves to be
  // discounted on sight.
  const byLoad = weights.ok ? weights.data.by_load : [];
  const loadUnattributed = weights.ok ? weights.data.load_unattributed_sheds : 0;
  const loadBars =
    loadMetric === "weight"
      ? byLoad
          .slice()
          .sort((a, b) => b.average_weight_kg - a.average_weight_kg)
          .map((load) => ({
            key: load.load_ref,
            label: load.owner_name ? `${load.load_ref} · ${load.owner_name}` : load.load_ref,
            value: Number(load.average_weight_kg.toFixed(1)),
          }))
      : byLoad
          .filter((load) => load.gain_g_per_day != null)
          .map((load) => ({
            key: load.load_ref,
            label: `${load.owner_name ? `${load.load_ref} · ${load.owner_name}` : load.load_ref} (${load.gain_span_days ?? 0}d)`,
            value: Math.round(load.gain_g_per_day as number),
          }));

  return (
    <div className="weights-page">
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={filterFields}
        pageContract={pageContract}
      />

      <p className="muted small" style={{ margin: "0 0 -4px" }}>
        {copy(pageContract, "kpi.sheds.label")}: {summary.sheds_weighed} / {summary.sheds_in_scope}
        {" · "}
        {periodStart} – {periodEnd}
      </p>

      {/* Five cards, not six (maintainer, 2026-08-12). The sixth was Median daily gain, which
          printed the SAME number, the same denominator and the same sub-line as the "All parks —
          daily gain" card in the row below it — one figure stated twice, costing a sixth of the
          headline row. The gain row below is now unconditional so removing it here loses nothing in
          any scope. */}
      <section className="grid g5 kpi-row" aria-label={copy(pageContract, "section.sheds.aria")}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.kids.label")}</div>
          <div className="val">{summary.animals_weighed.toLocaleString("en-IN")}</div>
          <div className="dl">{copy(pageContract, "kpi.kids.sub")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.total.label")}</div>
          <div className="val">{kg(summary.total_weight_kg, 0)} kg</div>
          <div className="dl">{copy(pageContract, "kpi.total.sub")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.average.label")}</div>
          {/* Null average means nothing was weighed. Rendering 0.0 kg would read as a herd that
              weighs nothing — a different, untrue statement. */}
          <div className="val">
            {summary.average_weight_kg == null
              ? copy(pageContract, "empty.no_data.title")
              : `${kg(summary.average_weight_kg)} kg`}
          </div>
          <div className="dl">{copy(pageContract, "kpi.average.sub")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.over30.label")}</div>
          <div className="val">{summary.at_or_above_30kg.toLocaleString("en-IN")}</div>
          {/* The threshold counts carry their OWN denominator: a whole-shed weigh contributes
              nothing to them, so showing them against animals_weighed would understate them. */}
          <div className="dl">
            {summary.threshold_basis_animals.toLocaleString("en-IN")}{" "}
            {copy(pageContract, "kpi.threshold.basis")}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.over35.label")}</div>
          <div className="val">{summary.at_or_above_35kg.toLocaleString("en-IN")}</div>
          <div className="dl">
            {summary.threshold_basis_animals.toLocaleString("en-IN")}{" "}
            {copy(pageContract, "kpi.threshold.basis")}
          </div>
        </div>
      </section>

      {/* Daily gain, ALWAYS rendered — it used to appear only when the page was showing more than
          one park, because the headline card above carried it in every other scope. With that card
          gone, keeping the condition would have deleted the growth figure entirely from a
          park-scoped page: the one number this screen exists to answer. Its first card names the
          CURRENT scope, so the all-parks wording appears only when it really is all of them. */}
      <section className="grid g3 kpi-row" aria-label={copy(pageContract, "section.park_gain.aria")}>
        <div className="kpi">
          <div className="lab">
            {selectedParkName || copy(pageContract, "kpi.park_gain.all")}{" "}
            {copy(pageContract, "kpi.park_gain.suffix")}
          </div>
          {/* insufficient_data is a real state: a park where nothing was weighed twice has NO
              gain, and printing 0 g/day would read as a herd that stopped growing. */}
          <div className="val">
            {headlineGain == null
              ? copy(pageContract, "empty.no_data.title")
              : `${Math.round(headlineGain)} g`}
          </div>
          <div className="dl">
            {headlineGain == null
              ? copy(pageContract, "kpi.gain.none")
              : `${copy(pageContract, "kpi.gain.blended")} · ${headlineWeight.toLocaleString("en-IN")}`}
          </div>
        </div>
        {perParkGain.map((park) => (
          <div className="kpi" key={park.name}>
            <div className="lab">
              {park.name} {copy(pageContract, "kpi.park_gain.suffix")}
            </div>
            <div className="val">
              {park.median == null
                ? copy(pageContract, "empty.no_data.title")
                : `${Math.round(park.median)} g`}
            </div>
            <div className="dl">
              {park.median == null
                ? copy(pageContract, "kpi.gain.none")
                : `${copy(pageContract, "kpi.gain.blended")} · ${park.animals.toLocaleString("en-IN")}`}
            </div>
          </div>
        ))}
      </section>

      {/* Row 1 — the shed chart alone at full width (maintainer, 2026-08-17): it is the
          page's most important chart and was unreadable at half width once both parks'
          pens were on it. Two ranked columns inside — one per park, or the single
          selected park's list split across both — sharing one scale so a bar's length
          means the same thing in either column. */}
      <section className="card wchart" aria-label={copy(pageContract, "chart.average.aria")}>
        <h2 className="h">
          <Scale className="ic" size={15} aria-hidden />{" "}
          {shedMetric === "adg" ? copy(pageContract, "chart.gain.title") : copy(pageContract, "chart.average.title")}
          <MetricToggle param="shed_metric" current={shedMetric} params={params} pageContract={pageContract} />
        </h2>
        <p className="muted small">
          {shedMetric === "adg"
            ? copy(pageContract, "chart.gain.caption_shed")
            : copy(pageContract, "chart.average.caption")}
        </p>
        {shedChartCols.length === 0 ? (
          <WeightBars
            data={[]}
            emptyLabel={
              shedMetric === "adg"
                ? copy(pageContract, "empty.metric.no_gain")
                : copy(pageContract, "empty.no_data.body")
            }
            unit={shedMetric === "adg" ? "g" : "kg"}
            chartLabel={copy(pageContract, shedMetric === "adg" ? "chart.gain.aria" : "chart.average.aria")}
            size={shedChartSize}
          />
        ) : (
          <div className="wcols">
            {shedChartCols.map((col, index) => (
              // Index in the key on purpose: a single-park split renders the same
              // heading twice, so the heading alone is not unique.
              <div key={`${col.heading}|${index}`}>
                <h3 className="wcol-h">{col.heading}</h3>
                <WeightBars
                  data={col.rows}
                  domain={shedChartDomain}
                  emptyLabel={
                    shedMetric === "adg"
                      ? copy(pageContract, "empty.metric.no_gain")
                      : copy(pageContract, "empty.no_data.body")
                  }
                  unit={shedMetric === "adg" ? "g" : "kg"}
                  chartLabel={copy(pageContract, shedMetric === "adg" ? "chart.gain.aria" : "chart.average.aria")}
                  size={shedChartSize}
                />
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Row 2 — the three herd-register dimensions in one row, moved below the shed
          chart when it went full width. Short boxes: sex and stage are a handful of
          rows, and a long breed list scrolls inside its box like the shed lists do. */}
      <div className="grid g3">
        <section className="card wchart" aria-label={copy(pageContract, "chart.breed.aria")}>
          <h2 className="h">
            {copy(pageContract, "chart.breed.title")}
            <MetricToggle param="breed_metric" current={breedMetric} params={params} pageContract={pageContract} />
          </h2>
          <p className="muted small">{copy(pageContract, "section.demographics.caption")}</p>
          <WeightBars
            data={dimensionBars(breedMetric, demo?.by_breed ?? [], demo?.gain_by_breed ?? [])}
            emptyLabel={copy(pageContract, breedMetric === "adg" ? "empty.metric.no_gain" : "empty.demographics.body")}
            unit={breedMetric === "adg" ? "g" : "kg"}
            chartLabel={copy(pageContract, "chart.breed.aria")}
            size="short"
          />
        </section>
        <section className="card wchart" aria-label={copy(pageContract, "chart.sex.aria")}>
          <h2 className="h">
            {copy(pageContract, "chart.sex.title")}
            <MetricToggle param="sex_metric" current={sexMetric} params={params} pageContract={pageContract} />
          </h2>
          <WeightBars
            data={dimensionBars(sexMetric, demo?.by_sex ?? [], demo?.gain_by_sex ?? [])}
            emptyLabel={copy(pageContract, sexMetric === "adg" ? "empty.metric.no_gain" : "empty.demographics.body")}
            unit={sexMetric === "adg" ? "g" : "kg"}
            chartLabel={copy(pageContract, "chart.sex.aria")}
            size="short"
          />
        </section>
        <section className="card wchart" aria-label={copy(pageContract, "chart.stage.aria")}>
          <h2 className="h">
            {copy(pageContract, "chart.stage.title")}
            <MetricToggle param="stage_metric" current={stageMetric} params={params} pageContract={pageContract} />
          </h2>
          <WeightBars
            data={dimensionBars(stageMetric, demo?.by_stage ?? [], demo?.gain_by_stage ?? [])}
            emptyLabel={copy(pageContract, stageMetric === "adg" ? "empty.metric.no_gain" : "empty.demographics.body")}
            unit={stageMetric === "adg" ? "g" : "kg"}
            chartLabel={copy(pageContract, "chart.stage.aria")}
            size="short"
          />
        </section>
      </div>

      {/* Row 3 — growth by purchase load, full width: the label carries both the load
          number and the supplier, which does not fit a half-width card. */}
      <section className="card wchart" aria-label={copy(pageContract, "chart.load.aria")}>
        <h2 className="h">
          {loadMetric === "adg"
            ? copy(pageContract, "chart.load.title")
            : copy(pageContract, "chart.load.title_weight")}
          <MetricToggle param="load_metric" current={loadMetric} params={params} pageContract={pageContract} />
        </h2>
        <p className="muted small">{copy(pageContract, "chart.load.caption")}</p>
        <WeightBars
          data={loadBars}
          emptyLabel={
            byLoad.length === 0
              ? copy(pageContract, "empty.load.body")
              : copy(pageContract, "empty.metric.no_gain")
          }
          unit={loadMetric === "adg" ? "g" : "kg"}
          chartLabel={copy(pageContract, "chart.load.aria")}
          size="short"
          wide
        />
        {loadUnattributed > 0 ? (
          <p className="muted small">
            {loadUnattributed.toLocaleString("en-IN")} {copy(pageContract, "note.load.unmapped")}
          </p>
        ) : null}
      </section>

      {demo ? (
        <p className="muted small">
          {copy(pageContract, "note.demographics.coverage")}
          {demo.unresolved_animals > 0
            ? ` ${demo.unresolved_animals} weighed kid(s) are not in the herd register.`
            : ""}
        </p>
      ) : null}

      <section className="card wtable" aria-label={copy(pageContract, "section.sheds.aria")}>
        <h2 className="h">
          <Warehouse className="ic" size={15} aria-hidden /> {copy(pageContract, "section.sheds.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "note.total_weight")}</p>

        {slice.length === 0 ? (
          <div className="empty">
            <b>
              {hasAnyData
                ? copy(pageContract, "empty.filtered.title")
                : copy(pageContract, "empty.no_data.title")}
            </b>
            <span className="muted small">
              {hasAnyData
                ? copy(pageContract, "empty.filtered.body")
                : copy(pageContract, "empty.no_data.body")}
            </span>
          </div>
        ) : (
          <>
            <div
              className="tablewrap"
              tabIndex={0}
              role="group"
              aria-label={copy(pageContract, "section.sheds.aria")}
            >
              <table className="tbl">
                <thead>
                  <tr>
                    {shedColumns.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slice.map((row) => {
                    const mode = modeTag(row, pageContract);
                    const composition = compositionByShed.get(shedKey(row.location_id, row.partition_label));
                    return (
                      <tr key={shedKey(row.location_id, row.partition_label)}>
                        <td>{row.park_name}</td>
                        <td>
                          <b>{row.operational_location_display || row.shed_display_name}</b>
                          {composition?.chips.length ? (
                            <span className="wcomp-chips" aria-label="Breed and sex composition">
                              {composition.chips.map((chip) => (
                                <span className="wcomp-chip" key={`${chip.breed ?? ""}|${chip.sex ?? ""}`}>
                                  {compositionLabel(chip, pageContract)}
                                  {composition.source === "scanned_tags" ? (
                                    <span>{chip.animals.toLocaleString("en-IN")}</span>
                                  ) : null}
                                </span>
                              ))}
                            </span>
                          ) : null}
                        </td>
                        <td>
                          <Tag tone={mode.tone}>{mode.label}</Tag>
                        </td>
                        <td className="num">{row.animals_weighed.toLocaleString("en-IN")}</td>
                        <td className="num">{kg(row.average_weight_kg)} kg</td>
                        <td className="num">{kg(row.total_weight_kg, 0)} kg</td>
                        <td className="num">
                          {row.last_weighed_date ?? (
                            <span className="muted">
                              {copy(pageContract, "value.never_weighed")}
                            </span>
                          )}
                        </td>
                        <td>{row.bucket_status}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <WorklistPager
              pageContract={pageContract}
              offset={offset}
              limit={limit}
              rowCount={slice.length}
              hasMore={offset + slice.length < visibleRows.length}
              noun={copy(pageContract, "pager.noun")}
              pageSizeOptions={PAGE_SIZE_OPTIONS}
              hrefForOffset={(next) => hrefWith(params, { offset: String(next) })}
              hrefForLimit={(next) => hrefWith(params, { limit: String(next), offset: null })}
            />
          </>
        )}

        <p className="muted small">{copy(pageContract, "note.threshold_basis")}</p>
      </section>

      <section className="card wtable" aria-label={copy(pageContract, "section.losing.aria")}>
        <h2 className="h">
          <TrendingDown className="ic" size={15} aria-hidden />{" "}
          {copy(pageContract, "section.losing.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.losing.caption")}</p>

        {losingSlice.length === 0 ? (
          <div className="empty">
            <b>{copy(pageContract, "empty.losing.title")}</b>
            <span className="muted small">{copy(pageContract, "empty.losing.body")}</span>
          </div>
        ) : (
          <>
            <div
              className="tablewrap"
              tabIndex={0}
              role="group"
              aria-label={copy(pageContract, "section.losing.aria")}
            >
              <table className="tbl">
                <thead>
                  <tr>
                    {losingColumns.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {losingSlice.map((animal) => (
                    <tr key={`${animal.scanned_identifier}-${animal.latest_weigh_date}`}>
                      <td>
                        <b>{animal.scanned_identifier}</b>
                      </td>
                      <td>{animal.shed_display_name}</td>
                      <td className="num">{kg(animal.previous_weight_kg)} kg</td>
                      <td className="num">{kg(animal.latest_weight_kg)} kg</td>
                      <td className="num">
                        <Tag tone="dng">
                          {kg(animal.latest_weight_kg - animal.previous_weight_kg)} kg
                        </Tag>
                      </td>
                      <td className="num">{Math.round(animal.days_between)}</td>
                      <td className="num">{animal.latest_weigh_date}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <WorklistPager
              pageContract={pageContract}
              offset={losingOffset}
              limit={DEFAULT_LIMIT}
              rowCount={losingSlice.length}
              hasMore={losingOffset + losingSlice.length < losingAll.length}
              noun={copy(pageContract, "pager.losing_noun")}
              pageSizeOptions={[DEFAULT_LIMIT]}
              hrefForOffset={(next) => hrefWith(params, { losing_offset: String(next) })}
              hrefForLimit={() => hrefWith(params, {})}
            />
          </>
        )}
      </section>
      <GrowthDirectorSection result={growthDirector} pageContract={pageContract} />
    </div>
  );
}
