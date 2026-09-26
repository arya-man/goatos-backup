import Table from "@mui/material/Table";
import Typography from "@mui/material/Typography";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { Gauge, TrendingDown, Warehouse } from "lucide-react";

import { GrowthDirectorSection } from "./growth-director";
import { WeightsKpiDeck } from "./weights-kpi-deck";
import { splitParts } from "@/components/minimal/widgets";
import { MetricChart, ShedMetricChart } from "./metric-chart";
import { SegmentedLinks } from "@/components/segmented-links";
import { GainThresholdBars, type GainThresholdRow } from "./gain-threshold-bars";
import { WeightsExportControl, type WeightsExportShed } from "./weights-export";
import { Tag } from "@/components/ui-primitives";
import { Caption } from "@/components/app/caption";
import { InfoHint } from "@/components/app/info-hint";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { ORIGIN_KEYS, canonicalOriginRedirect, originFromParam } from "@/lib/animal-origin";
import { fmtDate, humanizeEnum, todayIso } from "@/lib/format";
import { sharesOfWhole } from "@/lib/shares";
import {
  firstAuthRequiredError,
  getGrowthAssumptions,
  getGrowthDirector,
  getShedWeights,
  getWeighingGrowth,
  getWeightDemographics,
  type ShedWeightsRow,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { pensFromPlacements, withLoadPens } from "@/lib/load-pens";
import { byParkThen, parkRank } from "@/lib/park-order";
import { one, type RouteSearchParams } from "@/lib/search-params";
// The landing window is SHARED with /weighing/analytics so the two screens can never disagree
// about which weighing period they are describing. See landing-window.ts for why it is one module.
import {
  defaultWindow,
  landingWindow,
  weightsWindowSettings,
} from "./landing-window";
import { WINDOW_FROM_PARAM, WINDOW_TO_PARAM } from "./landing-window-constants";
import { SEX_ALL, resolveSexFilter, sexControlValue, sexLabel } from "./sex-filter";
import { sexSentence, weightsSexChoices } from "./sex-filter-contract";
import { assumptionValue, bandEdgesParam, DEFAULT_SALE_READY_LOWER_KG, fillKg } from "./assumption-copy";

const PAGE_PATH = "/weighing/weights";
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

const GAIN_VIEW_PARAM = "gain_view";
// Which view the shed daily-gain card is showing (maintainer request 2026-09-01). Absent
// means the TABLE: the card was asked for as exact figures, and the bars stay one click away.
const SHED_VIEW_PARAM = "shed_view";
// Which kids the WHOLE PAGE counts (maintainer, 2026-08-26). Absent means all of them, so a link
// that predates the Sex filter keeps meaning what it showed when it was written.
const SEX_PARAM = "sex";
// Farm born, procured (no load) or procured (load) (maintainer, 2026-09-01; three-way since 2026-09-26). Absent means both, so a link that predates this
// filter keeps meaning what it showed when it was written -- deliberately UNLIKE the Sex filter
// beside it, which defaults to male: there is no "the number the farm cares about" side here, and
// defaulting to one would hide half the herd from a reader who never chose.
const ORIGIN_PARAM = "origin";

function weighingModeFilter(raw: string | undefined): string {
  return raw === "individual_animal" || raw === "per_shed_partition" ? raw : "all";
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
  const sex = chip.sex?.trim() ? humanizeEnum(chip.sex.trim()) : copy(pageContract, "composition.unknown_sex");
  return `${breed} · ${sex}`;
}

type ShedComposition =
  | { source?: string; chips: readonly { breed?: string; sex?: string; animals: number }[] }
  | undefined;

/**
 * The pen's cohorts as TABLE CELLS rather than a label suffix (maintainer request 2026-09-01).
 *
 * Same chips, same backend `animals` figure and the same unknown-breed/unknown-sex copy the
 * label composer uses, so the chart's parenthesised suffix and the table's three columns can
 * never name a different herd. The order is the backend's, and every cell renders it, which is
 * what keeps breed, gender and count aligned line for line on a mixed pen.
 */
function shedCohorts(
  composition: ShedComposition,
  pageContract: AdminUiPageContract,
): readonly { breed: string; sex: string; animals: number }[] {
  return (composition?.chips ?? []).map((chip) => ({
    breed: chip.breed?.trim() || copy(pageContract, "composition.unknown_breed"),
    sex: chip.sex?.trim() ? humanizeEnum(chip.sex.trim()) : copy(pageContract, "composition.unknown_sex"),
    animals: chip.animals,
  }));
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
  // Each cohort carries its resident COUNT (maintainer ask 2026-08-31), matching
  // the sheds table's chips, which have always shown it. "×" and not a bare
  // number: "Beetal - female - 12" would read as a pen suffix in this dashed
  // label, while "× 12" can only be a head count. The chip's own backend
  // `animals` figure, never a client-derived sum.
  const suffix = chips
    .map(
      (chip) =>
        `${compositionLabel(chip, pageContract).replaceAll(" · ", " - ")} × ${chip.animals.toLocaleString("en-IN")}`,
    )
    .join(", ");
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
 *
 * Columns run in `parkOrder` — the page's backend-served park vocabulary, CBE then
 * CPT — never alphabetically: the labels here are park codes, which happen to sort
 * that way, but the order is a decision, not a spelling.
 */
function shedChartColumns(
  rows: readonly ShedChartBar[],
  parkOrder: readonly string[],
): { heading: string; rows: ShedChartBar[] }[] {
  const byPark = new Map<string, ShedChartBar[]>();
  for (const row of rows) {
    const list = byPark.get(row.park_name);
    if (list) list.push(row);
    else byPark.set(row.park_name, [row]);
  }
  // Ranked so the column order is stable across reloads and metric toggles — Map
  // order would follow whichever park happens to hold the fastest pen today.
  const rank = parkRank(parkOrder);
  const parkNames = [...byPark.keys()].sort((a, b) => rank(a) - rank(b));
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
  const modeFilter = weighingModeFilter(one(params, "weighing"));
  // MALE is the default (maintainer request 2026-09-01): the farm's growth question is about the
  // males it is fattening, so an unfiltered landing showed a number nobody had asked for. Every
  // kid is still one click away as an EXPLICIT `sex=all`, the same shape the Weighing filter
  // already uses for its own All -- an absent parameter can only mean one thing, and here it
  // means male. Anything else falls back to the default rather than emptying the page, because a
  // hand-edited URL must not take the screen down.
  // The choices are the farm's genders from Configuration (see sex-filter.ts).
  const sexChoices = weightsSexChoices(pageContract);
  const sexFilter = resolveSexFilter(one(params, SEX_PARAM), sexChoices);
  // What the CONTROL shows. The reads take "" for every kid; the control cannot, or its All
  // option would be the blank one and would read back as the male default on the next request.
  const sexChoice = sexControlValue(sexFilter);
  // Origin takes the opposite default: absent means EVERY kid, so "all" is the blank value and no
  // separate control spelling is needed. An unrecognised value falls back to every kid rather than
  // emptying the page, for the same reason the sex fallback does -- a hand-edited URL must not take
  // the screen down.
  const rawOrigin = one(params, ORIGIN_PARAM);
  // A bookmark from the two-way filter (`origin=purchased`) is rewritten to its three-way key so
  // the control shows the cohort the figures are actually filtered to.
  const legacyOrigin = canonicalOriginRedirect("/weighing/weights", params);
  if (legacyOrigin) redirect(legacyOrigin);
  const originFilter = originFromParam(rawOrigin);
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
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";

  // The window is business DAYS, not a clock offset: a weigh belongs to the Asia/Kolkata day it
  // happened on.
  const today = todayIso();
  // The window the page opens on and the earliest calendar day: the Weighing SOP's weights_pages
  // block, served on this page's contract copy (backend-owned; the constants are the fallback).
  const windowSettings = weightsWindowSettings(pageContract.copy, today);
  const window = await landingWindow(
    params,
    today,
    parkFilter,
    sexFilter,
    originFilter,
    weighingCategoryFilter,
    windowSettings,
  );

  // EVERY read carries the same page scope. A page where the shed table counts one population
  // while the KPI cards count another has no true number on it.
  const scope = {
    park_id: parkFilter || undefined,
    sex: sexFilter || undefined,
    origin: originFilter || undefined,
    weighing_category: weighingCategoryFilter || undefined,
  };
  // The assumptions (maintainer decision 2026-09-19): the same sale lines and band edges ADG
  // Analytics passes -- weighing is isolated and does not read the assumptions table, so this
  // page names them too, or the two Weights pages would count "over N kg" at different lines.
  const assumptions = await getGrowthAssumptions();
  if (firstAuthRequiredError(assumptions)) redirect(INTERNAL_LOGIN_PATH);
  // Any other failure is the page's failure (PR #320 review): a count at the constants' lines
  // while the tenant's may differ is a wrong number shown as valid, not a graceful fallback.
  if (!assumptions.ok) {
    return (
      <section className="card">
        <h2 className="h">{copy(pageContract, "error.load.title")}</h2>
        <p className="muted small">{copy(pageContract, "error.load.body")}</p>
      </section>
    );
  }
  const assumptionRows = assumptions.data.values;
  const saleThresholdKg = assumptionRows ? assumptionValue(assumptionRows, "sale_ready_threshold_kg") : null;
  const saleLowerKg = assumptionRows ? assumptionValue(assumptionRows, "sale_ready_lower_kg") : null;
  const [weights, growth, demographics, growthDirector] = await Promise.all([
    getShedWeights({
      ...scope,
      ...window,
      ...(saleThresholdKg != null ? { sale_threshold_kg: saleThresholdKg } : {}),
      ...(saleLowerKg != null ? { sale_lower_kg: saleLowerKg } : {}),
    }),
    getWeighingGrowth({ ...scope, ...window, sections: "headline,shed_leaderboard,losing_animals" }),
    getWeightDemographics({
      ...scope,
      ...window,
      sections: "composition,dimensions,gain_thresholds",
      band_edges_kg: bandEdgesParam(assumptionRows),
    }),
    getGrowthDirector({ ...scope, ...window, sections: "road_to_sale,fair_fight" }),
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

  const {
    rows,
    summary,
    parks,
    period_start: periodStart,
    period_end: periodEnd,
  } = weights.data;

  // The park order every park-grouped chart and table on this page uses: the backend serves
  // the vocabulary CBE-first, then CPT (maintainer decision 2026-09-16), labelled with the same
  // code the rows carry in `park_name`. Two clusters, never interleaved.
  const parkOrder = parks.map((park) => park.name);

  // The backend owns the mode filter for every aggregate. This defensive row narrowing keeps the
  // bounded table aligned if an older backend ever returns a broader row set.
  const weighedRows = rows.filter((row) => row.animals_weighed > 0);
  const visibleRows =
    modeFilter === "all" ? weighedRows : weighedRows.filter((row) => row.weighing_category === modeFilter);
  const slice = visibleRows.slice(offset, offset + limit);
  const visibleRowKeys = new Set(visibleRows.map((row) => shedKey(row.location_id, row.partition_label)));

  // Biggest loss first: the kid that dropped most is the one to go and look at.
  // Sorted on the CHANGE, not the daily rate, because that is what the table shows
  // and a reader ordering by an unshown column has no way to check the order.
  const losingAll = (growth.ok ? listOrEmpty(growth.data.losing_animals) : [])
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
      minDate: windowSettings.earliestDate,
      // This is only the fallback window the control shows when the URL carries no picked period. A
      // picked span is still written explicitly, even when it equals this pair. Named fields, never a spread of
      // defaultWindow(): `{...{from,to}}` would silently overwrite the SELECTED window above with
      // the default and pin the page to 30 days whatever the reader picked.
      defaultFrom: defaultWindow(today, windowSettings).from,
      defaultTo: defaultWindow(today, windowSettings).to,
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
        markerHint: copy(pageContract, "filter.period.lump_marker_hint"),
      },
      markerFetchPath: `/api/weighing/lump-markers?sex=${encodeURIComponent(sexChoice)}&origin=${encodeURIComponent(originFilter || "all")}&weighing=${encodeURIComponent(modeFilter)}${parkFilter ? `&park_id=${encodeURIComponent(parkFilter)}` : ""}`,
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
    {
      // The Sex filter governs the WHOLE page (maintainer, 2026-08-26): every KPI, the shed table,
      // both leaderboards, the load chart and the breed gain card follow it, because a page whose
      // cards disagree about which kids they counted has no true number on it.
      //
      // A whole-shed weigh carries no tag and is claimed only when its shed's cohort is entirely
      // one sex, which is the farm's own rule for how those sheds are stocked; a shed the register
      // shows as mixed is claimed by neither side rather than split.
      kind: "select",
      param: SEX_PARAM,
      label: copy(pageContract, "filter.sex.label"),
      value: sexChoice,
      // allowAll:false, and this vocabulary carries its OWN All — the same reason the Weighing
      // filter above does. With the bar's generic blank option on top, "every kid" would have two
      // spellings: the explicit `sex=all` and an absent parameter, which now means MALE. The
      // blank one would silently switch the page back to the default while claiming to show
      // everything, so there is exactly one All and it is a real value.
      allowAll: false,
      options: [{ value: SEX_ALL, label: copy(pageContract, "filter.all_option") }, ...sexChoices],
    },
    {
      // Origin governs the WHOLE page for the same reason Sex does: the farm breeds its own kids
      // and buys them in loads, and a page where the shed table counts both while the gain card
      // counts one has no true number on it.
      //
      // The backend decides which pens were bought, from the purchase-load tag the pen carries --
      // the same mapping the load chart further down this page reads. A pen's whole-shed weighs
      // and its scanned weighs therefore always land on the same side of this filter.
      kind: "select",
      param: ORIGIN_PARAM,
      label: copy(pageContract, "filter.origin.label"),
      // allowAll:true, and this is the ONE place this page's two cohort filters differ. Sex carries
      // its own "all" as a real value because an absent `sex` means MALE; origin has no default
      // side, so absent already means every kid and the bar's own blank option IS the All. Giving
      // it a second, explicit spelling would put two Alls in one list again -- the defect the
      // Weighing and Sex filters both carry a comment about.
      allowAll: true,
      value: originFilter,
      options: [
        ...ORIGIN_KEYS.map((key) => ({ value: key, label: copy(pageContract, `view.origin.${key}`) })),
      ],
    },
  ];

  const shedColumns = tableLabels(pageContract, "shed-weights");
  const losingColumns = tableLabels(pageContract, "losing-kids");
  const placementColumns = tableLabels(pageContract, "load-placements");

  const demo = demographics.ok ? demographics.data : null;
  const compositionByShed = new Map(
    (demo?.shed_composition ?? []).map((item) => [shedKey(item.location_id, item.partition_label), item]),
  );

  // Alphabetical by shed/pen, the SAME order the gain view uses (maintainer decision
  // 2026-08-22, extended to Weight 2026-08-24). Both metrics are the same sheds, and the
  // metric toggle is a reading preference: ranking one view heaviest-first and the other
  // A→Z reshuffled every row under the reader's eyes on a toggle they expected to change
  // only the bars. A stable order is also how an operator finds one specific pen at all —
  // a rank tells them nothing about where "Castro 2" will be. Sorted AFTER the map,
  // because the label the reader scans is composed there.
  const chartData = visibleRows
    .filter((row) => row.animals_weighed > 0)
    .slice()
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
        // The table reads these two instead of `label`: the pen name on its own, and the
        // composition as data for the breed/gender/count columns.
        shedName,
        cohorts: shedCohorts(compositionByShed.get(key), pageContract),
        value: Number(row.average_weight_kg.toFixed(1)),
        ...modeBarTag(row, pageContract),
      };
    })
    // Clustered by park first (CBE, then CPT), then sorted on the PEN NAME, not the composed
    // label: the two agree today because the composition trails the name, but a label-keyed
    // sort makes the table's A→Z order depend on a string the table no longer shows. `numeric`
    // keeps "Castro 2" ahead of "Castro 10".
    .sort(
      byParkThen(parkOrder, (row) => row.park_name, (a, b) =>
        a.shedName.localeCompare(b.shedName, undefined, { numeric: true }),
      ),
    );
  // The headline is the backend's park-level same-animal median. Do not average
  // shed medians here: the median of medians is not the herd median and produced
  // a visible 38 g card while the API/SQL truth was 120.8 g.
  // The backend's ONE daily-gain number: the animal-weighted mean over kids weighed twice PLUS the
  // whole-shed pens, which is the identical statistic the breed/sex/stage gain charts below report
  // (maintainer decision 2026-08-26). It was the median of scanned pairs only, so this card and
  // those charts described different herds -- filtered to Male the page showed 133 g here and 200 g
  // there. `headline_animals`, not `pair_count`: the gain speaks for every kid behind it, and most
  // of this farm's kids are weighed by the whole shed rather than one at a time.
  const headlineGain = growth.ok ? (growth.data.headline.average_adg_g_per_day ?? null) : null;
  const headlineWeight = growth.ok ? growth.data.headline.headline_animals : 0;
  // Presentation only: weekly gain drives the headline card's sparkline and trend pill.
  const weeklyGainSpark = (growth.ok ? (growth.data.weekly_gain ?? []) : []).map((point) =>
    Math.round(point.average_adg_g_per_day),
  );
  const weeklyGainDelta =
    weeklyGainSpark.length >= 2
      ? weeklyGainSpark[weeklyGainSpark.length - 1] - weeklyGainSpark[weeklyGainSpark.length - 2]
      : null;

  // Daily gain per shed comes from the growth read's own shed leaderboard, which is already
  // restricted to per-animal sheds — a whole-shed total can never produce a per-kid gain.
  // Two different measurements share this chart, and the caption says so. A
  // per-animal shed reports the mean of its kids' own gains (the headline's statistic,
  // maintainer decision 2026-09-24 -- not the median of leg rates). A whole-shed weigh
  // has no per-animal gain at all, so it reports how fast its AVERAGE is moving —
  // which population change also moves. Merging them silently would be the defect;
  // showing only the first would drop every whole-shed shed from a gain view they
  // now have real history for.
  const perAnimalGainRows = (growth.ok ? listOrEmpty(growth.data.shed_leaderboard) : [])
      .filter(
        (shed): shed is typeof shed & { average_adg_g_per_day: number } =>
          shed.average_adg_g_per_day != null && shed.adg_animals > 0 && visibleRowKeys.has(shedKey(shed.location_id, shed.partition_label)),
      )
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
        shedName: shed.operational_location_display || shed.display_name,
        cohorts: shedCohorts(
          compositionByShed.get(shedKey(shed.location_id, shed.partition_label)),
          pageContract,
        ),
        value: Math.round(shed.average_adg_g_per_day),
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
        shedName: row.operational_location_display || row.shed_display_name,
        cohorts: shedCohorts(
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
  // Clustered by park (CBE, then CPT), then alphabetical by shed/pen, not ranked by gain
  // (maintainer decision 2026-08-22): every park column keeps the same stable A→Z order so an
  // operator can find a specific pen by name. `numeric` keeps "Castro 2" ahead of "Castro 10".
  const gainChartData = [...perAnimalGainRows, ...shedAverageGainRows].sort(
    byParkThen(parkOrder, (row) => row.park_name, (a, b) =>
      a.shedName.localeCompare(b.shedName, undefined, { numeric: true }),
    ),
  );

  const hasAnyData = summary.animals_weighed > 0;

  // The full-width shed chart's columns for both metrics, on ONE shared scale per metric:
  // computed across every column's rows before the split, so a bar's length means the
  // same thing whichever column it lands in.
  const shedSeries = {
    adg: {
      data: gainChartData,
      columns: shedChartColumns(gainChartData, parkOrder),
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
      columns: shedChartColumns(chartData, parkOrder),
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
  // `name` turns a bucket's key into the farm's word. The sex buckets arrive as the stored gender
  // CODE ("male", "castrated") -- weighing reads no gender list of its own -- so the sex chart
  // names them from the farm's genders on this page's contract (audit 2026-09-26).
  const dimensionBars = (
    kind: "weight" | "adg",
    weightBuckets: readonly { label: string; average_weight_kg: number }[],
    gainBuckets: readonly { label: string; median_gain_g_per_day: number }[],
    name: (key: string) => string = (key) => key,
  ) =>
    kind === "weight"
      ? weightBuckets.map((b) => ({ key: b.label, label: name(b.label), value: Number(b.average_weight_kg.toFixed(1)) }))
      : gainBuckets.map((b) => ({ key: b.label, label: name(b.label), value: Math.round(b.median_gain_g_per_day) }));
  const sexName = (code: string) => sexLabel(code, sexChoices);
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
  const byLoad = weights.ok ? listOrEmpty(weights.data.by_load) : [];
  const loadUnattributed = weights.ok ? weights.data.load_unattributed_sheds : 0;
  // A load's park cluster, for the same CBE-then-CPT clustering the shed chart uses. A load
  // is placed into pens, and the pens name the park; a load split across both parks (a real
  // shape) files under the FIRST park it sits in, and a load with no placement at all goes
  // last — it cannot say where it is, so it must not sit inside either cluster.
  const loadPark = (load: (typeof byLoad)[number]): string => {
    const rank = parkRank(parkOrder);
    let best = "";
    for (const placement of load.placements ?? []) {
      if (placement.park_name && (best === "" || rank(placement.park_name) < rank(best))) {
        best = placement.park_name;
      }
    }
    return best;
  };
  const loadOrder = [...parkOrder, ""];
  // The pens each load sits in, in a bracket beside its name (maintainer request 2026-09-22): a
  // load number says which invoice the animals arrived on, not where to walk. The SAME
  // `placements` the load-placements table below reads, so a bar and its row can never name
  // different pens, and a load with no placement gets no bracket rather than a guess.
  // The pens are the TRAILING bracket, as they are on every other load chart. On the gain view
  // that puts them AFTER the span -- "126 · Ramesh Reddy (49d) (CBE Castro 1)" -- rather than
  // between the name and the span, which read as two competing parentheses.
  const loadName = (load: (typeof byLoad)[number], suffix = ""): string =>
    withLoadPens(
      `${load.owner_name ? `${load.load_ref} · ${load.owner_name}` : load.load_ref}${suffix}`,
      pensFromPlacements(load.placements),
    );
  const loadWeightBars = byLoad
    .slice()
    .sort(byParkThen(loadOrder, loadPark, (a, b) => b.average_weight_kg - a.average_weight_kg))
    .map((load) => ({
      key: load.load_ref,
      label: loadName(load),
      value: Number(load.average_weight_kg.toFixed(1)),
    }));
  const loadGainBars = byLoad
    .filter((load) => load.gain_g_per_day != null)
    .sort(byParkThen(loadOrder, loadPark))
    .map((load) => ({
      key: load.load_ref,
      label: loadName(load, ` (${load.gain_span_days ?? 0}d)`),
      value: Math.round(load.gain_g_per_day as number),
    }));

  // WHERE each load sits. Clustered by park like the chart above (CBE, then CPT), then
  // ordered by head count so the biggest placement reads first — the load chart's own order
  // changes with the metric toggle, and a table that reshuffled underneath it would be
  // harder to read, not easier.
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
        park: loadPark(load),
      };
    })
    .sort(byParkThen(loadOrder, (row) => row.park, (a, b) => b.animals - a.animals));

  // Row 2b — how many kids of each breed fall into each daily gain band.
  //
  // The four counts are DISJOINT (maintainer, 2026-08-24): a kid at 260 g/day is counted in
  // the top band only, so the four add up to the row's own denominator. They are still
  // rendered as four independent bars — each band's share is the fact, and no arithmetic
  // ACROSS bands happens here: every count arrives from the backend already banded.
  //
  // The share is taken against the row's OWN backend-supplied denominator (kids of this breed
  // with a second weigh), which is the exact key set the backend filtered — never against the
  // page's animal total, which covers kids weighed once and would understate every breed.
  const gainThresholdColumns = tableLabels(pageContract, "gain-thresholds");
  // The four band columns, in contract order, paired with their colour step. The labels are
  // the table contract's own, so the chart legend and the table header cannot drift into two
  // spellings of one band. `under` is the slowest band and the only red one: it is not a step
  // on the green growth ramp, it is the kids that are not growing.
  const gainThresholdSteps = ["hi", "mid", "lo", "under"] as const;
  // The page is ALREADY filtered by the time these rows arrive, so the card renders exactly what
  // it was sent. Re-selecting by sex here would be a second implementation of one rule and the two
  // would drift — and the rows carry BOTH kinds of weigh: the individually scanned kids plus every
  // kid of a single-breed whole-shed weigh, banded by that shed's own average change.
  const gainThresholdRows: GainThresholdRow[] = (demo?.gain_thresholds_by_breed ?? [])
    .filter((row) => row.animals > 0)
    .map((row) => {
      const counts = [
        row.above_250_g_per_day,
        row.band_200_to_250_g_per_day,
        row.band_180_to_200_g_per_day,
        row.at_or_below_180_g_per_day,
      ];
      return {
        key: row.label,
        breed: row.label,
        animals: row.animals,
        marks: sharesOfWhole(counts, row.animals).map((pct, index) => ({
          step: gainThresholdSteps[index],
          // Column 0 is the breed and column 1 the head count, so the bands start at 2.
          label: gainThresholdColumns[index + 2] ?? "",
          count: counts[index],
          pct,
        })),
      };
    });
  // Caption, head-count noun and empty line name the kids the page counted, so they follow the
  // filter — a combined caption under Male would say the bands add up to the kids weighed twice
  // when they add up to the male kids weighed twice. All backend-contract copy.
  const gainCaption = sexSentence(
    pageContract,
    sexFilter,
    { all: "section.gain_thresholds.caption", perSex: (sex) => `section.gain_thresholds.caption_${sex}` },
    sexChoices,
  );
  const gainKidsLabel = sexSentence(
    pageContract,
    sexFilter,
    { all: "value.gain_thresholds.kids", perSex: (sex) => `value.gain_thresholds.kids_${sex}` },
    sexChoices,
  );
  const gainEmptyLabel = sexSentence(
    pageContract,
    sexFilter,
    { all: "empty.gain_thresholds.body", perSex: (sex) => `empty.gain_thresholds.${sex}` },
    sexChoices,
  );
  // Chart first: the card exists to answer "is this breed growing", and six rows of
  // figures answer that more slowly than six rows of bars. The exact counts are one
  // click away and the chart carries them on hover, so nothing is hidden by the default.
  const gainThresholdView = one(params, GAIN_VIEW_PARAM) === "table" ? "table" : "chart";
  // Table first here, unlike the card above: the reader asked for the exact per-shed numbers,
  // and with ~40 pens the bars answer "which pen" more slowly than a sorted list of figures.
  // The card is TABLE-ONLY (maintainer request 2026-09-01): the Table/Chart control is no longer
  // offered. `?shed_view=chart` is still honoured so an already-shared link keeps working, but
  // nothing on the page produces one any more, and the default is the table.
  const shedGainView = one(params, SHED_VIEW_PARAM) === "chart" ? "chart" : "table";

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
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb", "Weighing") }, { label: pageContract.title }]}
        actions={
          <WeightsExportControl
            pageContract={pageContract}
            parks={parks.map((park) => ({ park_id: park.park_id, name: park.name }))}
            sheds={exportSheds}
            initialParkId={parkFilter}
            initialFrom={window.from}
            initialTo={window.to}
            origin={originFilter}
            weighingCategory={modeFilter}
            today={today}
            openHref={hrefWith(params, { wt_export: "1" })}
            closeHref={hrefWith(params, { wt_export: null })}
          />
        }
      />
      {/* The Sex control sits IN LINE with the filters, beside Weighing, and is shaped like the
          fields next to it — but it is held in client state, not the URL: all three grains arrive
          in one response, so it changes nothing the server has to fetch and must not cost a page
          render. See gain-sex-scope. */}
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={filterFields}
        pageContract={pageContract}
      />

      <p className="muted small" style={{ margin: "0 0 -4px" }}>
        {copy(pageContract, "kpi.sheds.label")}: {summary.sheds_weighed} / {summary.sheds_in_scope}
        {" · "}
        {fmtDate(periodStart)} – {fmtDate(periodEnd)}
      </p>

      {/* Five cards, not six (maintainer, 2026-08-12). The sixth was Median daily gain, which
          printed the SAME number, the same denominator and the same sub-line as the "All parks —
          daily gain" card in the row below it — one figure stated twice, costing a sixth of the
          headline row. The gain row below is now unconditional so removing it here loses nothing in
          any scope. */}
      <WeightsKpiDeck
        ariaLabel={copy(pageContract, "section.sheds.aria")}
        items={[
          {
            key: "kids",
            // Two readings, two labels (template split card): the contract label names both halves in
            // order ("Individual · Lump-sum"); the card title is the whole count.
            label: `${summary.animals_weighed.toLocaleString("en-IN")} ${copy(pageContract, "kpi.kids.split.total_sub")}`,
            value: summary.animals_weighed,
            parts: splitParts(copy(pageContract, "kpi.kids.split.label"), [summary.individual_animals_weighed, summary.lump_sum_animals_weighed]),
            noDataText: copy(pageContract, "empty.no_data.title"),
            icon: "kids",
            tone: "info",
          },
          {
            key: "total",
            label: copy(pageContract, "kpi.total.label"),
            value: summary.total_weight_kg,
            noDataText: copy(pageContract, "empty.no_data.title"),
            unit: "kg",
            icon: "total",
            hint: copy(pageContract, "kpi.total.sub"),
          },
          {
            // Null average means nothing was weighed: never render 0.0 kg.
            key: "average",
            label: copy(pageContract, "kpi.average.label"),
            value: summary.average_weight_kg ?? null,
            noDataText: copy(pageContract, "empty.no_data.title"),
            digits: 1,
            unit: "kg",
            icon: "average",
            tone: "violet",
            hint: copy(pageContract, "kpi.average.sub"),
          },
          {
            // Threshold counts carry their OWN denominator (whole-shed weighs contribute nothing).
            key: "over30",
            label: fillKg(copy(pageContract, "kpi.over30.label"), saleLowerKg ?? DEFAULT_SALE_READY_LOWER_KG),
            value: summary.at_or_above_30kg,
            noDataText: copy(pageContract, "empty.no_data.title"),
            icon: "over30",
            tone: "success",
            hint: `${summary.threshold_basis_animals.toLocaleString("en-IN")} ${copy(pageContract, "kpi.threshold.basis")}`,
          },
          {
            key: "over35",
            label: fillKg(copy(pageContract, "kpi.over35.label"), saleThresholdKg),
            value: summary.at_or_above_35kg,
            noDataText: copy(pageContract, "empty.no_data.title"),
            icon: "over35",
            tone: "warning",
            hint: `${summary.threshold_basis_animals.toLocaleString("en-IN")} ${copy(pageContract, "kpi.threshold.basis")}`,
          },
        ]}
      />

      {/* Daily gain, ALWAYS rendered, naming the CURRENT scope. insufficient_data is a real state:
          no gain renders the no-data text, never 0 g/day. */}
      <WeightsKpiDeck
        ariaLabel={copy(pageContract, "section.park_gain.aria")}
        min={260}
        items={[
          {
            key: "headline",
            label: `${selectedParkName || copy(pageContract, "kpi.park_gain.all")} ${copy(pageContract, "kpi.park_gain.suffix")}`,
            value: headlineGain == null ? null : Math.round(headlineGain),
            noDataText: copy(pageContract, "empty.no_data.title"),
            unit: "g",
            icon: "gain",
            sparkline: weeklyGainSpark,
            trend: headlineGain == null ? null : weeklyGainDelta,
            trendSuffix: " g",
            hint:
              headlineGain == null
                ? copy(pageContract, "kpi.gain.none")
                : `${copy(pageContract, "kpi.gain.blended")} · ${headlineWeight.toLocaleString("en-IN")}`,
          },
        ]}
      />

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
              data: dimensionBars("adg", demo?.by_sex ?? [], demo?.gain_by_sex ?? [], sexName),
              emptyLabel: copy(pageContract, "empty.metric.no_gain"),
              unit: "g",
              chartLabel: copy(pageContract, "chart.sex.aria"),
            },
            weight: {
              data: dimensionBars("weight", demo?.by_sex ?? [], demo?.gain_by_sex ?? [], sexName),
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
        view={shedGainView}
        tablePager={{
          previous: copy(pageContract, "action.previous"),
          next: copy(pageContract, "action.next"),
          page: copy(pageContract, "pager.page"),
          of: copy(pageContract, "pager.of"),
          noun: copy(pageContract, "pager.noun"),
        }}
        tableColumns={{
          park: copy(pageContract, "table.shed_gain.park"),
          shed: copy(pageContract, "table.shed_gain.shed"),
          breed: copy(pageContract, "table.shed_gain.breed"),
          sex: copy(pageContract, "table.shed_gain.sex"),
          count: copy(pageContract, "table.shed_gain.count"),
          basis: copy(pageContract, "table.shed_gain.basis"),
          value: {
            adg: copy(pageContract, "table.shed_gain.gain"),
            weight: copy(pageContract, "table.shed_gain.weight"),
          },
        }}
      />

      {/* Row 2b — kids clearing each daily gain mark, by breed. A breed median says where the
          middle kid sits; it cannot say how many of the breed are actually growing well, which
          is the question this answers. Sits directly above the load chart because both read as
          "who is growing", one by breed and one by supplier.

          Every column header is the backend table contract's, and the caption says each kid is
          counted once — the four bands are a real distribution that adds to the denominator. */}
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
        <Caption>{gainCaption}</Caption>
        {gainThresholdView === "chart" ? (
          <GainThresholdBars
            rows={gainThresholdRows}
            chartLabel={copy(pageContract, "chart.gain_thresholds.aria")}
            emptyLabel={gainEmptyLabel}
            kidsLabel={gainKidsLabel}
            ofLabel={copy(pageContract, "value.gain_thresholds.of")}
          />
        ) : gainThresholdRows.length === 0 ? (
          <EmptyState title={gainEmptyLabel} />
        ) : (
          <div className="tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "section.gain_thresholds.aria")}>
            <Table className="tbl" aria-label={copy(pageContract, "section.gain_thresholds.aria")}>
              <TableHead>
                <TableRow>
                  {gainThresholdColumns.map((label, index) => (
                    <TableCell component="th" key={label} className={index >= 1 ? "num" : undefined}>
                      {label}
                    </TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {gainThresholdRows.map((row) => (
                  <TableRow key={row.key}>
                    <TableCell>
                      <b>{row.breed}</b>
                    </TableCell>
                    <TableCell className="num">{row.animals.toLocaleString("en-IN")}</TableCell>
                    {row.marks.map((mark) => (
                      <TableCell key={mark.step} className="num">
                        {mark.count.toLocaleString("en-IN")}{" "}
                        <span className="muted">({mark.pct.toFixed(1)}%)</span>
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
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
          <Typography
            component="p"
            variant="caption"
            color="text.secondary"
            sx={{ display: "flex", justifyContent: "flex-end", m: 0 }}
          >
            <InfoHint text={`${loadUnattributed.toLocaleString("en-IN")} ${copy(pageContract, "note.load.unmapped")}`} />
          </Typography>
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
        <Caption>{copy(pageContract, "section.load_placements.caption")}</Caption>
        {loadPlacementRows.length === 0 ? (
          <EmptyState title={copy(pageContract, "empty.load_placements.body")} />
        ) : (
          <div className="tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "section.load_placements.aria")}>
            <Table className="tbl" aria-label={copy(pageContract, "section.load_placements.aria")}>
              <TableHead>
                <TableRow>
                  {placementColumns.map((label) => (
                    <TableCell component="th" key={label}>{label}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {loadPlacementRows.map((row) => (
                  <TableRow key={row.key}>
                    <TableCell>
                      <b>{row.loadRef}</b>
                      {row.ownerName ? <div className="muted small">{row.ownerName}</div> : null}
                    </TableCell>
                    <TableCell>{row.parks.join(", ")}</TableCell>
                    <TableCell>
                      {row.sheds.map((shed) => (
                        <Tag key={shed.key} tone="mut">
                          {shed.label}
                        </Tag>
                      ))}
                    </TableCell>
                    <TableCell>{row.animals.toLocaleString("en-IN")}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
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
        <Caption>{copy(pageContract, "note.total_weight")}</Caption>

        {slice.length === 0 ? (
          <EmptyState
            title={hasAnyData ? copy(pageContract, "empty.filtered.title") : copy(pageContract, "empty.no_data.title")}
            description={hasAnyData ? copy(pageContract, "empty.filtered.body") : copy(pageContract, "empty.no_data.body")}
          />
        ) : (
          <>
            <div
              className="tablewrap"
              tabIndex={0}
              role="group"
              aria-label={copy(pageContract, "section.sheds.aria")}
            >
              <Table className="tbl">
                <TableHead>
                  <TableRow>
                    {shedColumns.map((label) => (
                      <TableCell component="th" key={label}>{label}</TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {slice.map((row) => {
                    const mode = modeTag(row, pageContract);
                    const composition = compositionByShed.get(shedKey(row.location_id, row.partition_label));
                    return (
                      <TableRow key={shedKey(row.location_id, row.partition_label)}>
                        <TableCell>{row.park_name}</TableCell>
                        <TableCell>
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
                        </TableCell>
                        <TableCell>
                          <Tag tone={mode.tone}>{mode.label}</Tag>
                        </TableCell>
                        <TableCell className="num">{row.animals_weighed.toLocaleString("en-IN")}</TableCell>
                        <TableCell className="num">{kg(row.average_weight_kg)} kg</TableCell>
                        <TableCell className="num">{kg(row.total_weight_kg, 0)} kg</TableCell>
                        <TableCell className="num">
                          {row.last_weighed_date ? fmtDate(row.last_weighed_date) : (
                            <span className="muted">
                              {copy(pageContract, "value.never_weighed")}
                            </span>
                          )}
                        </TableCell>
                        <TableCell>{workflowLabel(row.bucket_status, pageContract)}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
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

        <Caption>{copy(pageContract, "note.threshold_basis")}</Caption>
      </section>

      <section className="card wtable" aria-label={copy(pageContract, "section.losing.aria")}>
        <h2 className="h">
          <TrendingDown className="ic" size={15} aria-hidden />{" "}
          {copy(pageContract, "section.losing.title")}
        </h2>
        <Caption>{copy(pageContract, "section.losing.caption")}</Caption>

        {losingSlice.length === 0 ? (
          <EmptyState
            title={copy(pageContract, "empty.losing.title")}
            description={copy(pageContract, "empty.losing.body")}
          />
        ) : (
          <>
            <div
              className="tablewrap"
              tabIndex={0}
              role="group"
              aria-label={copy(pageContract, "section.losing.aria")}
            >
              <Table className="tbl">
                <TableHead>
                  <TableRow>
                    {losingColumns.map((label) => (
                      <TableCell component="th" key={label}>{label}</TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {losingSlice.map((animal) => (
                    <TableRow key={`${animal.scanned_identifier}-${animal.latest_weigh_date}`}>
                      <TableCell>
                        <b>{animal.scanned_identifier}</b>
                      </TableCell>
                      <TableCell>{animal.operational_location_display || animal.shed_display_name}</TableCell>
                      <TableCell className="num">{kg(animal.previous_weight_kg)} kg</TableCell>
                      <TableCell className="num">{kg(animal.latest_weight_kg)} kg</TableCell>
                      <TableCell className="num">
                        <Tag tone="dng">
                          {kg(animal.latest_weight_kg - animal.previous_weight_kg)} kg
                        </Tag>
                      </TableCell>
                      <TableCell className="num">{Math.round(animal.days_between)}</TableCell>
                      <TableCell className="num">{fmtDate(animal.latest_weigh_date)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
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
