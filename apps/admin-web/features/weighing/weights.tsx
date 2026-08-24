import { redirect } from "next/navigation";
import { Gauge, TrendingDown, Warehouse } from "lucide-react";

import { GrowthDirectorSection } from "./growth-director";
import { MetricChart, ShedMetricChart } from "./metric-chart";
import { SegmentedLinks } from "@/components/segmented-links";
import { GainThresholdBars, type GainThresholdRow } from "./gain-threshold-bars";
import { WeightsExportControl, type WeightsExportShed } from "./weights-export";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
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

// The window the page lands on: the 15 days before today, inclusive of both ends (maintainer,
// 2026-08-24; 30 before that day, briefly 7 the same day). The calendar still picks any span —
// this is only where the page starts. 15 days is wide enough that a shed weighed on a roughly
// fortnightly round has TWO weighs in it, which is what a daily gain needs to exist at all.
const DEFAULT_WINDOW_DAYS = 15;
// Which view the gain-mark card is showing. Absent means the chart, so a shared link
// that predates the toggle — or one copied from the default view — keeps meaning "chart".
const GAIN_VIEW_PARAM = "gain_view";
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

function modeBarTag(row: ShedWeightsRow, pageContract: AdminUiPageContract) {
  const mode = modeTag(row, pageContract);
  return { modeLabel: mode.label, modeTone: mode.tone };
}

function workflowLabel(status: string, pageContract: AdminUiPageContract): string {
  const key = `value.workflow.${status.toLowerCase().replace(/[^a-z0-9]+/g, "_")}`;
  const label = pageContract.copy[key];
  if (label) return label;
  return status
    .split(/[_\s-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
    .join(" ");
}

// One row of the full-width shed chart: a WeightBars bar plus the park it belongs to.
type ShedChartBar = {
  key: string;
  park_name: string;
  label: string;
  value: number;
  valueLabel?: string;
  modeLabel?: string;
  modeTone?: "info" | "mut";
};

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
  composition:
    | { source?: string; chips: readonly { breed?: string; sex?: string; animals: number }[] }
    | undefined,
  pageContract: AdminUiPageContract,
): string {
  const chips = composition?.chips ?? [];
  if (chips.length === 0) return shedName;
  const suffix = chips.map((chip) => compositionLabel(chip, pageContract).replaceAll(" · ", " - ")).join(", ");
  return `${shedName} (${suffix})`;
}

/**
 * The shed chart's two columns. With rows from more than one park, each park gets its
 * own column (heading = park), so the two farms stop interleaving in one long list.
 * With one park — the park filter, or a period only one park weighed in — the single
 * park's list is split in half across both columns instead of leaving the right half
 * of a full-width card empty; the list reads down the left column and continues down
 * the right. Rows arrive pre-sorted (the gain chart alphabetically by shed/pen, the
 * weight chart heaviest-first) and grouping preserves that order.
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
            const headline = result.ok ? result.data.headline : null;
            return {
              name: park.name,
              median: headline?.median_adg_g_per_day ?? null,
              animals: headline?.pair_count ?? 0,
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
      // Landing on this window clears both parameters, so a shared link keeps meaning "the last 15
      // days" rather than freezing on the fortnight it was copied in. Named fields, never a spread of
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
  const placementColumns = tableLabels(pageContract, "load-placements");

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
        ...modeBarTag(row, pageContract),
      };
    });
  // The headline is the backend's park-level same-animal median. Do not average
  // shed medians here: the median of medians is not the herd median and produced
  // a visible 38 g card while the API/SQL truth was 120.8 g.
  const headlineGain = growth.ok ? (growth.data.headline.median_adg_g_per_day ?? null) : null;
  const headlineWeight = growth.ok ? growth.data.headline.pair_count : 0;

  // Daily gain per shed comes from the growth read's own shed leaderboard, which is already
  // restricted to per-animal sheds — a whole-shed total can never produce a per-kid gain.
  // Two different measurements share this chart, and the caption says so. A
  // per-animal shed reports the median of its kids' own gains. A whole-shed weigh
  // has no per-animal gain at all, so it reports how fast its AVERAGE is moving —
  // which population change also moves. Merging them silently would be the defect;
  // showing only the first would drop every whole-shed shed from a gain view they
  // now have real history for.
  const perAnimalGainRows = (growth.ok ? growth.data.shed_leaderboard : [])
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
        // by contract precisely so they group identically here: the row's OWN park,
        // not the selected filter — a literal "All parks" here split the chart into a
        // pseudo-park column of per-animal rows beside real CBE/CPT columns holding
        // only the lump-sum sheds, so Castro never appeared under the All-parks view.
        // `operational_location_display` is the canonical shed+pen string;
        // `display_name` is the weighing bucket's free-text planning label, which has
        // held "M1P5" and "C1" for sheds whose real names are "Mandela 1 - Part 5"
        // and "Castro 1".
        park_name: shed.park_name,
        label: shedLabelWithComposition(
          shed.operational_location_display || shed.display_name,
          compositionByShed.get(shedKey(shed.location_id, shed.partition_label)),
          pageContract,
        ),
        value: Math.round(shed.median_adg_g_per_day),
        modeLabel: copy(pageContract, "value.weighing.individual"),
        modeTone: "info" as const,
      }));
  const shedAverageGainRows = visibleRows
      .filter((row) => row.shed_average_gain_g_per_day != null)
      .map((row) => ({
        key: `${shedKey(row.location_id, row.partition_label)}-shed`,
        park_name: row.park_name,
        label: shedLabelWithComposition(
          row.operational_location_display || row.shed_display_name,
          compositionByShed.get(shedKey(row.location_id, row.partition_label)),
          pageContract,
        ),
        value: Math.round(row.shed_average_gain_g_per_day as number),
        modeLabel: copy(pageContract, "value.weighing.lump"),
        modeTone: "mut" as const,
      }));
  // A shed with ONE weigh in the window has NO daily gain — a gain needs two weighs, and a
  // whole-shed weigh yields no per-animal gain at all. Such sheds used to appear here anyway,
  // with a zero-length bar labelled in KILOGRAMS (their average weight) beside real g/day bars:
  // two different measures on one axis, which is how "Castro 1 · 34.4 kg" came to sit in a
  // daily-gain chart. They are not plotted here at all now. Nothing is lost — the Weight view
  // shows every one of them, which is where a weight in kg belongs.
  // Alphabetical by shed/pen, not ranked by gain (maintainer decision 2026-08-22): every park
  // column keeps the same stable A→Z order so an operator can find a specific pen by name.
  // `numeric` keeps "Castro 2" ahead of "Castro 10".
  const gainChartData = [...perAnimalGainRows, ...shedAverageGainRows].sort(
    (a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }),
  );

  const hasAnyData = summary.animals_weighed > 0;

  // The full-width shed chart's columns for both metrics, on ONE shared scale per metric:
  // computed across every column's rows before the split, so a bar's length means the
  // same thing whichever column it lands in.
  const shedSeries = {
    adg: {
      data: gainChartData,
      columns: shedChartColumns(gainChartData),
      domain: {
        lo: Math.min(0, ...gainChartData.map((bar) => bar.value)),
        hi: Math.max(0, ...gainChartData.map((bar) => bar.value)),
      },
      emptyLabel: copy(pageContract, "empty.metric.no_gain"),
      unit: "g",
      chartLabel: copy(pageContract, "chart.gain.aria"),
      size: gainChartData.length <= 8 ? ("short" as const) : ("tall" as const),
      caption: copy(pageContract, "chart.gain.caption_shed"),
    },
    weight: {
      data: chartData,
      columns: shedChartColumns(chartData),
      domain: {
        lo: Math.min(0, ...chartData.map((bar) => bar.value)),
        hi: Math.max(0, ...chartData.map((bar) => bar.value)),
      },
      emptyLabel: copy(pageContract, "empty.no_data.body"),
      unit: "kg",
      chartLabel: copy(pageContract, "chart.average.aria"),
      size: chartData.length <= 8 ? ("short" as const) : ("tall" as const),
      caption: copy(pageContract, "chart.average.caption"),
    },
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
  const metricLabels = {
    adg: copy(pageContract, "metric.gain"),
    weight: copy(pageContract, "metric.weight"),
  };

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
  const loadWeightBars = byLoad
    .slice()
    .sort((a, b) => b.average_weight_kg - a.average_weight_kg)
    .map((load) => ({
      key: load.load_ref,
      label: load.owner_name ? `${load.load_ref} · ${load.owner_name}` : load.load_ref,
      value: Number(load.average_weight_kg.toFixed(1)),
    }));
  const loadGainBars = byLoad
    .filter((load) => load.gain_g_per_day != null)
    .map((load) => ({
      key: load.load_ref,
      label: `${load.owner_name ? `${load.load_ref} · ${load.owner_name}` : load.load_ref} (${load.gain_span_days ?? 0}d)`,
      value: Math.round(load.gain_g_per_day as number),
    }));

  // WHERE each load sits. Ordered by head count so the biggest placement reads first —
  // the load chart's own order changes with the metric toggle, and a table that
  // reshuffled underneath it would be harder to read, not easier.
  //
  // The park chips are the DISTINCT parks across the load's sheds: a load bought once
  // and split across two parks is a real shape, and naming only the first would be a
  // quiet lie. Shed labels are the backend-composed operational display, passed
  // through as data.
  const loadPlacementRows = byLoad
    .filter((load) => (load.placements ?? []).length > 0)
    .map((load) => {
      const placements = load.placements ?? [];
      return {
        key: load.load_ref,
        loadRef: load.load_ref,
        ownerName: load.owner_name ?? "",
        parks: [...new Set(placements.map((p) => p.park_name).filter(Boolean))],
        sheds: placements.map((p) => ({
          key: `${p.park_name}|${p.operational_location_display}`,
          label: `${p.operational_location_display} · ${p.animals.toLocaleString("en-IN")}`,
        })),
        animals: load.animals,
      };
    })
    .sort((a, b) => b.animals - a.animals);

  // Row 2b — how many kids of each breed clear each daily gain mark.
  //
  // The three counts are CUMULATIVE (maintainer, 2026-08-24): a kid at 260 g/day is counted
  // under all three. So they are rendered as three independent columns and are NEVER summed,
  // stacked, or subtracted from one another — "Above 180" already contains the other two.
  //
  // The share is taken against the row's OWN backend-supplied denominator (kids of this breed
  // with a second weigh), which is the exact key set the backend filtered — never against the
  // page's animal total, which covers kids weighed once and would understate every breed.
  const gainThresholdColumns = tableLabels(pageContract, "gain-thresholds");
  // The three mark columns, in contract order, paired with their ordered colour step. The
  // labels are the table contract's own, so the chart legend and the table header cannot
  // drift into two spellings of one mark.
  const gainThresholdSteps = ["hi", "mid", "lo"] as const;
  const gainThresholdRows: GainThresholdRow[] = (demo?.gain_thresholds_by_breed ?? [])
    .filter((row) => row.animals > 0)
    .map((row) => ({
      key: row.label,
      breed: row.label,
      animals: row.animals,
      marks: [row.above_250_g_per_day, row.above_200_g_per_day, row.above_180_g_per_day].map(
        (count, index) => ({
          step: gainThresholdSteps[index],
          // Column 0 is the breed and column 1 the head count, so the marks start at 2.
          label: gainThresholdColumns[index + 2] ?? "",
          count,
          pct: (count / row.animals) * 100,
        }),
      ),
    }));
  // Chart first: the card exists to answer "is this breed growing", and six rows of
  // figures answer that more slowly than six rows of bars. The exact counts are one
  // click away and the chart carries them on hover, so nothing is hidden by the default.
  const gainThresholdView = one(params, GAIN_VIEW_PARAM) === "table" ? "table" : "chart";

  // The download drawer's shed list: every shed the page knows about, at the same
  // location grain the backend filter takes. The park id travels with each shed so
  // the list follows the drawer's own park select; parks are unique by name within
  // a tenant, so the name→id hop cannot merge two parks.
  const parkIdByName = new Map(parks.map((park) => [park.name, park.park_id]));
  const exportShedsById = new Map<string, WeightsExportShed>();
  for (const row of rows) {
    if (exportShedsById.has(row.location_id)) continue;
    exportShedsById.set(row.location_id, {
      location_id: row.location_id,
      label: row.operational_location_display || row.shed_display_name,
      park_id: parkIdByName.get(row.park_name) ?? "",
    });
  }
  const exportSheds = [...exportShedsById.values()].sort((a, b) => a.label.localeCompare(b.label));

  return (
    <div className="weights-page">
      {/* The download opener rides at the far end of the filter bar, on the same line as the park
          and period controls, rather than on a line of its own above them (maintainer, 2026-08-24). */}
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={filterFields}
        pageContract={pageContract}
        trailing={
          <WeightsExportControl
            pageContract={pageContract}
            parks={parks.map((park) => ({ park_id: park.park_id, name: park.name }))}
            sheds={exportSheds}
            initialParkId={parkFilter}
            initialFrom={window.from}
            initialTo={window.to}
            today={today}
            openHref={hrefWith(params, { wt_export: "1" })}
            closeHref={hrefWith(params, { wt_export: null })}
          />
        }
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

      {/* Row 1 — the true growth charts. These sit before shed/scale movement because
          their daily gain is same-tag-twice ADG, not lump-sum average movement. */}
      <div className="grid g3">
        <MetricChart
          initialMetric={breedMetric}
          labels={metricLabels}
          title={{ adg: copy(pageContract, "chart.breed.title_gain"), weight: copy(pageContract, "chart.breed.title") }}
          caption={copy(pageContract, "section.demographics.caption")}
          series={{
            adg: {
              data: dimensionBars("adg", demo?.by_breed ?? [], demo?.gain_by_breed ?? []),
              emptyLabel: copy(pageContract, "empty.metric.no_gain"),
              unit: "g",
              chartLabel: copy(pageContract, "chart.breed.aria"),
            },
            weight: {
              data: dimensionBars("weight", demo?.by_breed ?? [], demo?.gain_by_breed ?? []),
              emptyLabel: copy(pageContract, "empty.demographics.body"),
              unit: "kg",
              chartLabel: copy(pageContract, "chart.breed.aria"),
            },
          }}
          size="short"
        />
        <MetricChart
          initialMetric={sexMetric}
          labels={metricLabels}
          title={{ adg: copy(pageContract, "chart.sex.title_gain"), weight: copy(pageContract, "chart.sex.title") }}
          series={{
            adg: {
              data: dimensionBars("adg", demo?.by_sex ?? [], demo?.gain_by_sex ?? []),
              emptyLabel: copy(pageContract, "empty.metric.no_gain"),
              unit: "g",
              chartLabel: copy(pageContract, "chart.sex.aria"),
            },
            weight: {
              data: dimensionBars("weight", demo?.by_sex ?? [], demo?.gain_by_sex ?? []),
              emptyLabel: copy(pageContract, "empty.demographics.body"),
              unit: "kg",
              chartLabel: copy(pageContract, "chart.sex.aria"),
            },
          }}
          size="short"
        />
        <MetricChart
          initialMetric={stageMetric}
          labels={metricLabels}
          title={{ adg: copy(pageContract, "chart.stage.title_gain"), weight: copy(pageContract, "chart.stage.title") }}
          series={{
            adg: {
              data: dimensionBars("adg", demo?.by_stage ?? [], demo?.gain_by_stage ?? []),
              emptyLabel: copy(pageContract, "empty.metric.no_gain"),
              unit: "g",
              chartLabel: copy(pageContract, "chart.stage.aria"),
            },
            weight: {
              data: dimensionBars("weight", demo?.by_stage ?? [], demo?.gain_by_stage ?? []),
              emptyLabel: copy(pageContract, "empty.demographics.body"),
              unit: "kg",
              chartLabel: copy(pageContract, "chart.stage.aria"),
            },
          }}
          size="short"
        />
      </div>

      {/* Row 2 — shed/partition chart. In gain mode this intentionally mixes two
          operational signals, so each row carries a Per animal/Lump sum chip. */}
      <ShedMetricChart
        initialMetric={shedMetric}
        labels={metricLabels}
        title={{ adg: copy(pageContract, "chart.gain.title"), weight: copy(pageContract, "chart.average.title") }}
        series={shedSeries}
      />

      {/* Row 2b — kids clearing each daily gain mark, by breed. A breed median says where the
          middle kid sits; it cannot say how many of the breed are actually growing well, which
          is the question this answers. Sits directly above the load chart because both read as
          "who is growing", one by breed and one by supplier.

          Every column header is the backend table contract's, and the caption states the
          overlap — the columns must not be read as a distribution that adds to the total. */}
      <section className="card wtable" aria-label={copy(pageContract, "section.gain_thresholds.aria")}>
        <h2 className="h">
          <Gauge className="ic" size={15} aria-hidden /> {copy(pageContract, "section.gain_thresholds.title")}
          {/* Top-right, and URL-driven like every other toggle on this page, so the choice
              survives a reload and travels in a shared link. SegmentedLinks keeps the reader
              beside the card instead of throwing them back to the top of a long page. */}
          <SegmentedLinks
            ariaLabel={copy(pageContract, "section.gain_thresholds.view_aria")}
            current={gainThresholdView}
            options={[
              { value: "chart", label: copy(pageContract, "view.chart"), href: hrefWith(params, { [GAIN_VIEW_PARAM]: null }) },
              { value: "table", label: copy(pageContract, "view.table"), href: hrefWith(params, { [GAIN_VIEW_PARAM]: "table" }) },
            ]}
          />
        </h2>
        <p className="muted small">{copy(pageContract, "section.gain_thresholds.caption")}</p>
        {gainThresholdView === "chart" ? (
          <GainThresholdBars
            rows={gainThresholdRows}
            chartLabel={copy(pageContract, "chart.gain_thresholds.aria")}
            emptyLabel={copy(pageContract, "empty.gain_thresholds.body")}
            kidsLabel={copy(pageContract, "value.gain_thresholds.kids")}
            ofLabel={copy(pageContract, "value.gain_thresholds.of")}
          />
        ) : gainThresholdRows.length === 0 ? (
          <div className="empty">
            <span className="muted small">{copy(pageContract, "empty.gain_thresholds.body")}</span>
          </div>
        ) : (
          <div className="tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  {gainThresholdColumns.map((label, index) => (
                    <th key={label} className={index >= 1 ? "num" : undefined}>
                      {label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {gainThresholdRows.map((row) => (
                  <tr key={row.key}>
                    <td>
                      <b>{row.breed}</b>
                    </td>
                    <td className="num">{row.animals.toLocaleString("en-IN")}</td>
                    {row.marks.map((mark) => (
                      <td key={mark.step} className="num">
                        {mark.count.toLocaleString("en-IN")}{" "}
                        <span className="muted">({mark.pct.toFixed(1)}%)</span>
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Row 3 — growth by purchase load, full width: the label carries both the load
          number and the supplier, which does not fit a half-width card. */}
      <div>
        <MetricChart
          initialMetric={loadMetric}
          labels={metricLabels}
          title={{ adg: copy(pageContract, "chart.load.title"), weight: copy(pageContract, "chart.load.title_weight") }}
          caption={copy(pageContract, "chart.load.caption")}
          series={{
            adg: {
              data: loadGainBars,
              emptyLabel: byLoad.length === 0 ? copy(pageContract, "empty.load.body") : copy(pageContract, "empty.metric.no_gain"),
              unit: "g",
              chartLabel: copy(pageContract, "chart.load.aria"),
            },
            weight: {
              data: loadWeightBars,
              emptyLabel: copy(pageContract, "empty.load.body"),
              unit: "kg",
              chartLabel: copy(pageContract, "chart.load.aria"),
            },
          }}
          size="short"
          wide
        />
        {loadUnattributed > 0 ? (
          <p className="muted small">
            {loadUnattributed.toLocaleString("en-IN")} {copy(pageContract, "note.load.unmapped")}
          </p>
        ) : null}
      </div>

      {/* Row 3b — WHERE each load sits. The chart above says a supplier's stock is
          growing; without this a reader cannot tell which park or shed grew it, and
          cannot walk from a load bar down to the shed table.

          Every row is rendered from the backend's own placement entries — the park
          name, the shed label and the head count all arrive composed, so this never
          re-derives an operational location client-side (AGENTS.md rule 5). The load
          list is bounded by the authored tag estate, so it is not paged. */}
      <section className="card wtable" aria-label={copy(pageContract, "section.load_placements.aria")}>
        <h2 className="h">
          <Warehouse className="ic" size={15} aria-hidden /> {copy(pageContract, "section.load_placements.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.load_placements.caption")}</p>
        {loadPlacementRows.length === 0 ? (
          <div className="empty">
            <span className="muted small">{copy(pageContract, "empty.load_placements.body")}</span>
          </div>
        ) : (
          <div className="tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  {placementColumns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {loadPlacementRows.map((row) => (
                  <tr key={row.key}>
                    <td>
                      <b>{row.loadRef}</b>
                      {row.ownerName ? <div className="muted small">{row.ownerName}</div> : null}
                    </td>
                    <td>{row.parks.join(", ")}</td>
                    <td>
                      {row.sheds.map((shed) => (
                        <Tag key={shed.key} tone="mut">
                          {shed.label}
                        </Tag>
                      ))}
                    </td>
                    <td>{row.animals.toLocaleString("en-IN")}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
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
                              {composition.chips.map((chip, chipIndex) => (
                                // breed|sex alone is NOT unique — the backend can emit two chips
                                // for the same breed+sex (one per resident cohort), and Godel 1
                                // - Part 7 really does carry "Osmanabadi - female" twice. The
                                // list is render-only (never reordered or edited in place), so
                                // the index disambiguates safely.
                                <span className="wcomp-chip" key={`${chip.breed ?? ""}|${chip.sex ?? ""}|${chipIndex}`}>
                                  {compositionLabel(chip, pageContract)}
                                  <span>{chip.animals.toLocaleString("en-IN")}</span>
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
                        <td>{workflowLabel(row.bucket_status, pageContract)}</td>
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
                      <td>{animal.operational_location_display || animal.shed_display_name}</td>
                      <td className="num">{kg(animal.previous_weight_kg)} kg</td>
                      <td className="num">{kg(animal.latest_weight_kg)} kg</td>
                      <td className="num">
                        <Tag tone="dng">
                          {kg(animal.latest_weight_kg - animal.previous_weight_kg)} kg
                        </Tag>
                      </td>
                      <td className="num">{Math.round(animal.days_between)}</td>
                      <td className="num">{fmtDate(animal.latest_weigh_date)}</td>
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
