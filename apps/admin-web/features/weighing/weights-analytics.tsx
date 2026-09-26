import { redirect } from "next/navigation";
import { CalendarRange, LayoutGrid, PackageOpen, Scale, Sprout, Warehouse } from "lucide-react";

import { GroupedBars, type BarGroup, type GroupedBar } from "./grouped-bars";
import { LoadComparisonTab } from "./load-comparison-tab";
import { WeightBars } from "./weight-bars";
import { WeightsExportControl, type WeightsExportShed } from "./weights-export";
import { SegmentedLinks } from "@/components/segmented-links";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FCRTab } from "./fcr-tab";
import { assumptionValue, bandEdgesParam, DEFAULT_SALE_READY_LOWER_KG, fillKg } from "./assumption-copy";
import { PensTable, type PensTableRow } from "./pens-table";
import { FeedWeightBandCard } from "./feed-weight-band-card";
import { PenWeekGainTable, type PenWeekGainPoint } from "./pen-week-gain-table";
import { LoadWeekGainTable, type LoadWeekGainPoint } from "./load-week-gain-table";
import { fmtDate, todayIso } from "@/lib/format";
import {
  firstAuthRequiredError,
  getGrowthAssumptions,
  getGrowthSalePrices,
  getWeighingFCR,
  getFeedWeightBand,
  getShedWeights,
  getWeighingGrowth,
  getWeightDemographics,
  type ShedWeightsResponse,
  type ShedWeightsRow,
  type ShedWeightsSummary,
  type FeedWeightBandResponse,
  type WeighingGrowthResponse,
  type WeightDemographicsResponse,
} from "@/lib/api/server";
import { getLoadwiseSales, getLoadwiseWeights } from "@/lib/api/procurement-server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
// The landing window is SHARED with /weighing/weights: both screens open on the latest whole-shed
// weigh against the one before it, so a reader moving between them is looking at one period rather
// than two that merely look alike. See landing-window.ts.
import {
  defaultWindow,
  landingWindow,
  weightsWindowSettings,
} from "./landing-window";
import { WINDOW_FROM_PARAM, WINDOW_TO_PARAM } from "./landing-window-constants";
import { SEX_ALL, resolveSexFilter, sexControlValue } from "./sex-filter";
import { weightsSexChoices } from "./sex-filter-contract";
import { WeightsAnalyticsTabLoading } from "./weights-analytics-tab-loading";

const PAGE_PATH = "/weighing/analytics";
const SEX_PARAM = "sex";
const ORIGIN_PARAM = "origin";
const TAB_PARAM = "tab";
// The pens table's own average-weight filter: an operator and a typed value, applied together.
// The Time-wise tab's own two controls (maintainer request 2026-09-21). They are TAB-LOCAL, like
// the pens table's weight filter and unlike Sex or Origin: the bucket decides how this tab's own
// columns are cut, and the pen narrows this tab to one pen. Neither changes what another tab counts.
const GAIN_BUCKET_PARAM = "tw_bucket";
const TIME_PEN_PARAM = "tw_pen";
const WEIGHT_OP_PARAM = "w_op";
const WEIGHT_VALUE_PARAM = "w_kg";
// The Weight-wise tab's feed table carries four filters of its own, scoped to that table: the
// sheet's feed type, which weight evidence backs the row, the pen and the group. They narrow the
// rows the server already served -- one day's sheet is small -- and travel in the URL like every
// other filter on this page.
const FEED_BAND_VIEW_PARAM = "fb_view";
const FEED_BAND_TYPE_PARAM = "fb_type";
const FEED_BAND_SOURCE_PARAM = "fb_src";
const FEED_BAND_BAND_PARAM = "fb_band";
const FEED_BAND_PEN_PARAM = "fb_pen";
const FEED_BAND_GROUP_PARAM = "fb_group";
// Animals is a SERVER filter, unlike the six above: it changes what the backend counts, so it
// travels to the read rather than narrowing served rows.
const FEED_BAND_ANIMALS_PARAM = "fb_animals";
const FEED_BAND_SEARCH_PARAM = "fb_q";
const FEED_BAND_LIMIT_PARAM = "fb_limit";
const FEED_BAND_OFFSET_PARAM = "fb_offset";
const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;

/**
 * Splits the pen picker's `location::partition` value into the two query parameters the reads take.
 *
 * A pen is (location, partition): a pen NAME repeats across parks, so the key carries the location
 * id. An empty or malformed value is NO scope -- every pen -- rather than a scope that matches
 * nothing, because a stale link must land on the whole tab, not on an empty one.
 */
function penScopeFrom(penKey: string): { pen_location_id?: string; pen_partition_label?: string } {
  const separator = penKey.indexOf("::");
  if (separator <= 0) return {};
  return {
    pen_location_id: penKey.slice(0, separator),
    pen_partition_label: penKey.slice(separator + 2),
  };
}


/** The six operators of the `weight_kg_compare` option group, evaluated on a pen's average. */
function compareKg(actual: number, op: string, wanted: number): boolean {
  switch (op) {
    case "gt":
      return actual > wanted;
    case "gte":
      return actual >= wanted;
    case "eq":
      return Math.abs(actual - wanted) < 0.05;
    case "lte":
      return actual <= wanted;
    case "lt":
      return actual < wanted;
    case "neq":
      return Math.abs(actual - wanted) >= 0.05;
    default:
      return true;
  }
}
const DEFAULT_LIMIT = 25;

const TABS = ["general", "breed", "birth", "shed", "weight", "time", "load", "fcr"] as const;

type Tab = (typeof TABS)[number];

function weighingModeFilter(raw: string | undefined): string {
  return raw === "individual_animal" || raw === "per_shed_partition" ? raw : "all";
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

function boundedLimit(raw: string | undefined): number {
  const parsed = Number(raw);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number]) ? parsed : DEFAULT_LIMIT;
}

function boundedOffset(raw: string | undefined): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 5000 ? parsed : 0;
}

function kg(value: number, fractionDigits = 1): string {
  return value.toLocaleString("en-IN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  });
}

function shedKey(locationID: string, partitionLabel?: string | null): string {
  return `${locationID}|${partitionLabel ?? ""}`;
}


/**
 * Which breed a shed belongs to, from the backend's own composition chips.
 *
 * AGREE-OR-NEITHER, the same rule the Sex and Origin filters use one grain up. A shed whose
 * residents are all one breed is grouped under it; a shed holding two is grouped as mixed rather
 * than filed under whichever breed happens to have more animals. Splitting one shed average
 * across two breeds would invent a distribution nobody measured, and picking the majority breed
 * would make a pen's group flip as animals move.
 */
export async function WeighingWeightsAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const params = searchParams ?? {};
  const rawTab = one(params, TAB_PARAM);
  const tab: Tab = (TABS as readonly string[]).includes(rawTab ?? "") ? (rawTab as Tab) : "general";

  const parkFilter = one(params, "park") ?? "";
  const modeFilter = weighingModeFilter(one(params, "weighing"));
  // MALE is the default, matching /weighing/weights (maintainer request 2026-09-01): the farm's
  // growth question is about the males it is fattening, so an unfiltered landing would show a
  // number nobody asked for. Every kid stays one click away as an explicit `sex=all`; anything
  // else falls back to the default rather than emptying the page.
  const sexChoices = weightsSexChoices(pageContract);
  const sexFilter = resolveSexFilter(one(params, SEX_PARAM), sexChoices);
  // What the CONTROL shows. The reads take "" for every kid; the control cannot, or its All
  // option would be the blank one and would read back as the male default on the next request.
  const sexChoice = sexControlValue(sexFilter);
  const rawOrigin = one(params, ORIGIN_PARAM);
  const originFilter = rawOrigin === "farm_born" || rawOrigin === "purchased" ? rawOrigin : "";

  // WEEK is the default and anything unrecognised falls back to it, because a bucket the backend
  // would refuse must not take the whole tab down over a hand-typed URL. `month` is a rolling
  // 30-day block counted back from the window's last day, never a calendar month.
  const gainBucket = one(params, GAIN_BUCKET_PARAM) === "month" ? "month" : "week";
  // The selected pen, as the picker encodes it: `location::partition`. It is sent to the reads, not
  // applied to their answers -- three of the Time-wise sections are server-side aggregates (one
  // number per bucket, one row per breed, one row per load) and carry no pen to filter on once they
  // are grouped. A malformed value narrows nothing rather than emptying the tab.
  const penScope = penScopeFrom(one(params, TIME_PEN_PARAM) ?? "");
  // The key the CONTROL shows is the key the READS were narrowed by, always. A malformed value
  // narrows nothing, so it reads back as "All"; anything that did narrow the reads must show as a
  // selection, or a reader sees an unscoped-looking control above a scoped (often empty) page.
  const penKey = penScope.pen_location_id ? `${penScope.pen_location_id}::${penScope.pen_partition_label ?? ""}` : "";

  const limit = boundedLimit(one(params, "limit"));
  const offset = boundedOffset(one(params, "offset"));

  const today = todayIso();
  // The window the page opens on and the earliest calendar day: the Weighing SOP's weights_pages
  // block, served on this page's contract copy (backend-owned; the constants are the fallback).
  const windowSettings = weightsWindowSettings(pageContract.copy, today);
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";
  const landingWindowPromise = landingWindow(
    params,
    today,
    parkFilter,
    sexFilter,
    originFilter,
    weighingCategoryFilter,
    windowSettings,
  );
  // The assumptions (maintainer decision 2026-09-19): the shed-weights read takes both sale lines
  // and the demographics read its band edges from them -- weighing is isolated and does not read
  // the assumptions table itself, so the caller names them, exactly as it names the tolerance.
  // The landing-window date lookup is independent, so start both before awaiting either one.
  const [assumptions, window] = await Promise.all([getGrowthAssumptions(), landingWindowPromise]);
  if (firstAuthRequiredError(assumptions)) redirect(INTERNAL_LOGIN_PATH);
  // Any other failure is the page's failure (PR #320 review): rendering "Over 35 kg" from the
  // constants while the tenant's line may be something else would show a wrong count as if it
  // were valid. The figures the page is valued against are part of the page.
  if (!assumptions.ok) {
    return <WeightsAnalyticsLoadError pageContract={pageContract} />;
  }
  const assumptionRows = assumptions.data.values;
  const saleThresholdKg = assumptionRows ? assumptionValue(assumptionRows, "sale_ready_threshold_kg") : null;
  const saleLowerKg = assumptionRows ? assumptionValue(assumptionRows, "sale_ready_lower_kg") : null;
  // Every tab reads the same selected/default period, including Time-wise. That keeps the tab strip
  // as slices of one population instead of silently changing the date range under the reader.
  const readWindow = window;

  const scope = {
    park_id: parkFilter || undefined,
    sex: sexFilter || undefined,
    origin: originFilter || undefined,
    weighing_category: weighingCategoryFilter || undefined,
  };

  // Every tab needs the shed read: it carries the park vocabulary the filter bar renders, plus
  // the whole-shed gain figures the Shed-wise tab reads. Only the tab's own extra reads are
  // fetched beside it, so opening Breed-wise does not pay for the Growth queries.
  const growthSections =
    tab === "general" ? "headline,shed_leaderboard,by_park" : tab === "time" ? "weekly_gain" : "";
  const wantsGrowth = growthSections !== "";
  // General reads demographics too, for the pens table's Breed column ONLY (maintainer request
  // 2026-09-21): the pen's resident cohort lives on the same `shed_composition` chips the Weights
  // table renders, so the two screens can never name a different breed for one pen. It asks for
  // the `composition` section alone -- the landing tab pays for that one read, not for the
  // dimensions, bands and weekly gain it draws nothing from.
  const wantsDemographics =
    tab === "general" ||
    tab === "breed" ||
    tab === "shed" ||
    tab === "birth" ||
    tab === "weight" ||
    tab === "time";
  const demographicsSections =
    tab === "general"
      ? "composition"
      : tab === "breed"
      ? "dimensions"
      : tab === "birth"
        ? "origin"
        : tab === "shed"
          ? "shed_type"
          : tab === "weight"
            ? "weight_bands"
            : tab === "time"
              ? "weekly_gain"
              : "";

  // The Load-wise tab reads the purchase ledger beside the ONE shed-weights request every tab
  // makes. On that tab the shed read carries park + the selected period only: a load is bought
  // whole and followed through every pen move by its own animals, so sex/origin/mode cannot slice
  // it (and are hidden there). One request either way, never two overlapping shed reads.
  const wantsLoads = tab === "load";
  // The Weight-wise tab's feed table reads the Growth Director's feed-by-weight-band endpoint
  // beside the demographics read, under every page filter: the backend resolves sex, origin and
  // weighing mode through the weighing module's own scope resolvers, so the table narrows exactly
  // as the General tab does.
  const wantsFeedBand = tab === "weight";
  const shedParams = {
    // The Comparison tab reads the SELECTED period (maintainer request 2026-09-24): "latest weighing"
    // is each load animal's newest weigh inside it. A load is bought whole and followed through
    // every pen move, so only park narrows it -- the sex, origin and mode filters are hidden there.
    ...(wantsLoads ? { park_id: parkFilter || undefined, ...readWindow } : { ...scope, ...readWindow }),
    ...(saleThresholdKg != null ? { sale_threshold_kg: saleThresholdKg } : {}),
    ...(saleLowerKg != null ? { sale_lower_kg: saleLowerKg } : {}),
    include_loads: wantsLoads,
    include_dates: false,
  };

  // ONE demographics read serves all three tabs that need it, Birth-wise included: the backend
  // carries `gain_by_breed_origin` on this same response. Asking the read twice under the two
  // origins would recompute by_sex, by_stage, the bands and the shed composition only to discard
  // both copies -- and would let the two halves be resolved a request apart.
  // The purchase side of the Comparison tab is the UNPRICED load read, which WeighingMonitor may
  // hold. The PRICED read behind the value chart is fetched only when the contract enabled that
  // chart for this principal (load_value_chart, SalesRead): fetch = render, and a principal the
  // priced endpoint would refuse is never asked to call it.
  const valueChart = pageContract.controls.find((item) => item.id === "load_value_chart");
  const wantsValue = wantsLoads && (valueChart?.enabled ?? false);
  // The FCR tab (maintainer request 2026-09-07) is ONE read carrying every figure it shows; the
  // Comparison tab reads the same sale-price vocabulary that read values gain at, so the two tabs
  // price the herd at one number.
  const wantsFCR = tab === "fcr";
  const [weights, growth, demographics, loadwise, loadValues, feedBand, fcr, salePrices] = await Promise.all([
    getShedWeights(shedParams),
    // The bucket and the pen ride BOTH Time-wise reads, and only on that tab: the tab's four
    // sections must sit on one set of columns and answer for one pen, while the General and
    // Shed-wise tabs read the same growth response and have no such control of their own.
    wantsGrowth
      ? getWeighingGrowth({
          ...scope,
          ...readWindow,
          sections: growthSections,
          ...(tab === "time" ? { bucket: gainBucket, ...penScope } : {}),
        })
      : null,
    wantsDemographics
      ? getWeightDemographics({
          ...scope,
          ...readWindow,
          sections: demographicsSections,
          band_edges_kg: bandEdgesParam(assumptionRows),
          ...(tab === "time" ? { bucket: gainBucket, ...penScope } : {}),
        })
      : null,
    wantsLoads ? getLoadwiseWeights({ park_id: parkFilter || undefined }) : null,
    wantsValue ? getLoadwiseSales({ park_id: parkFilter || undefined }) : null,
    wantsFeedBand
      ? getFeedWeightBand({
          park_id: parkFilter || undefined,
          sex: sexFilter || undefined,
          origin: originFilter || undefined,
          weighing_category: weighingCategoryFilter || undefined,
          ...readWindow,
        })
      : null,
    wantsFCR ? getWeighingFCR({ ...scope, ...readWindow }) : null,
    wantsLoads ? getGrowthSalePrices() : null,
  ]);

  if (firstAuthRequiredError(weights, growth, demographics, loadwise, loadValues, feedBand, fcr, salePrices)) redirect(INTERNAL_LOGIN_PATH);

  if (!weights.ok) {
    return <WeightsAnalyticsLoadError pageContract={pageContract} />;
  }
  if (growth && !growth.ok) {
    return <WeightsAnalyticsLoadError pageContract={pageContract} />;
  }
  if (demographics && !demographics.ok) {
    return <WeightsAnalyticsLoadError pageContract={pageContract} />;
  }

  const { rows, summary, parks, period_start: periodStart, period_end: periodEnd } = weights.data;
  const demo = demographics?.ok ? demographics.data : null;

  // Per-park gain cards beside the all-parks one, so CBE and CPT can be read against each other
  // and against the herd. They come off the growth read the page ALREADY made -- `by_park` is the
  // identical statistic as the headline, cut per park in the same query -- so the cards cost no
  // extra request.
  //
  // They were once built by calling that endpoint again once per park, which is one extra round
  // trip per park on a screen with a sub-500ms budget; removing those calls for page speed left
  // the card markup below rendering an empty list, and the cards silently vanished. The backend
  // now carries the cut, so speed and the cards are no longer a trade.
  //
  // Empty whenever the reader has narrowed to one park: the card above is then that park's, and
  // restating it beside itself says nothing. The backend applies the SAME filters to this cut as
  // to the headline, so the cards can never describe a different population than the row above.
  const perParkGain =
    tab === "general" && parkFilter === ""
      ? (growth?.ok ? (growth.data.by_park ?? []) : []).map((park) => ({
          name: park.park_name,
          gain: park.average_adg_g_per_day ?? null,
          animals: park.headline_animals,
        }))
      : [];

  const modeOptions = optionGroup(pageContract, "weighing_mode");
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
      kind: "select",
      param: "weighing",
      label: copy(pageContract, "filter.weighing.label"),
      value: modeFilter,
      allowAll: false,
      options: modeOptions.map((option) => ({ value: option.key, label: option.label })),
    },
    {
      // Sex governs the WHOLE page, every tab alike: a screen whose tabs disagree about which
      // kids they counted has no true number on it. Same reason it carries its own explicit All
      // as a real value — an absent parameter here means MALE.
      kind: "select",
      param: SEX_PARAM,
      label: copy(pageContract, "filter.sex.label"),
      value: sexChoice,
      allowAll: false,
      options: [{ value: SEX_ALL, label: copy(pageContract, "filter.all_option") }, ...sexChoices],
    },
    {
      // Same as /weighing/weights: absent means every origin, while the explicit values narrow
      // every analytics read to farm-born or purchased kids.
      kind: "select",
      param: ORIGIN_PARAM,
      label: copy(pageContract, "filter.origin.label"),
      allowAll: true,
      value: originFilter,
      options: [
        { value: "farm_born", label: copy(pageContract, "view.origin.farm_born") },
        { value: "purchased", label: copy(pageContract, "view.origin.purchased") },
      ],
    },
  ];
  // Filters the Comparison tab does not apply are HIDDEN there, not left showing a choice that
  // changes nothing (maintainer request 2026-09-24). Park and Period stay.
  const visibleFilterFields = wantsLoads
    ? filterFields.filter((field) => field.param === "park" || field.param === WINDOW_FROM_PARAM)
    : filterFields;

  return (
    <div className="weights-page">
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={visibleFilterFields}
        pageContract={pageContract}
        trailing={
          <WeightsExportControl
            pageContract={pageContract}
            parks={parks.map((park) => ({ park_id: park.park_id, name: park.name }))}
            sheds={exportSheds}
            initialParkId={parkFilter}
            initialFrom={window.from}
            initialTo={window.to}
            origin={originFilter}
            weighingCategory={modeFilter !== "all" ? modeFilter : undefined}
            today={today}
            openHref={hrefWith(params, { wt_export: "1" })}
            closeHref={hrefWith(params, { wt_export: null })}
          />
        }
      />

      {/* Centred, not left-flush: this strip is the page's primary navigation across five views of
          one dataset, and hard against the left edge it read as another filter belonging to the bar
          above it rather than as the control that changes the whole screen. */}
      <div className="feed-tabbar wt-tabbar">
        <SegmentedLinks
          ariaLabel={copy(pageContract, "tab.aria")}
          current={tab}
          options={TABS.map((name) => ({
            value: name,
            label: copy(pageContract, `tab.${name}`),
            // The default tab clears the parameter, so a shared link keeps meaning "the tab this
            // page opens on" rather than freezing on the one it was copied from.
            href: hrefWith(params, {
              [TAB_PARAM]: name === "general" ? null : name,
              [WINDOW_FROM_PARAM]: window.from,
              [WINDOW_TO_PARAM]: window.to,
              offset: null,
            }),
          }))}
        />
      </div>
      <WeightsAnalyticsTabLoading
        currentTab={tab}
        tabLabels={Object.fromEntries(TABS.map((name) => [name, copy(pageContract, `tab.${name}`)]))}
      />

      <p className="muted small" style={{ margin: "0 0 -4px" }}>
        {copy(pageContract, "kpi.sheds.label")}: {summary.sheds_weighed} / {summary.sheds_in_scope}
        {" · "}
        {fmtDate(periodStart)} – {fmtDate(periodEnd)}
      </p>

      <div className="wt-tab-live">
        {tab === "general" ? (
          <GeneralTab
            pageContract={pageContract}
            summary={summary}
            rows={rows}
            parks={parks}
            parkFilter={parkFilter}
            modeFilter={modeFilter}
            growth={growth?.ok ? growth.data : null}
            demo={demo}
            perParkGain={perParkGain}
            limit={limit}
            offset={offset}
            params={params}
            saleThresholdKg={saleThresholdKg}
            saleLowerKg={saleLowerKg}
          />
        ) : null}

        {tab === "breed" ? <BreedTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "birth" ? <BirthTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "shed" ? <ShedTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "weight" ? (
          <WeightTab
            pageContract={pageContract}
            demo={demo}
            feedBand={feedBand?.ok ? feedBand.data : null}
            params={params}
            periodLabel={`${fmtDate(periodStart)} – ${fmtDate(periodEnd)}`}
          />
        ) : null}

        {tab === "time" ? (
          <TimeTab
            pageContract={pageContract}
            growth={growth?.ok ? growth.data : null}
            demo={demo}
            gainBucket={gainBucket}
            penKey={penKey}
            /* The pen vocabulary comes from the SHED read, which every tab already makes and which
               the pen scope never narrows. Building it from the narrowed response instead would
               leave the picker holding one option -- the pen already chosen -- with no way back. */
            pens={rows.map((row) => ({
              key: `${row.location_id}::${row.partition_label ?? ""}`,
              park: row.park_name,
              label: row.operational_location_display || row.shed_display_name,
            }))}
          />
        ) : null}

        {/* Load-wise degrades by half, not whole-page: a dead purchase ledger empties the tab
            with its own message, a dead weighing side keeps the purchase figures with a band. */}
        {tab === "load" ? (
          <LoadComparisonTab
            pageContract={pageContract}
            loads={loadwise?.ok ? (loadwise.data.loads ?? []) : null}
            weights={weights.data}
            valueLoads={loadValues?.ok ? (loadValues.data.loads ?? []) : null}
            valueChartReason={valueChart?.enabled ? "" : (valueChart?.disabled_reason ?? "")}
            salePrices={salePrices?.ok ? salePrices.data.prices : null}
          />
        ) : null}

        {/* FCR degrades whole: every figure on the tab comes from the one read, so a failed read
            shows the tab's own error rather than half a strip. */}
        {tab === "fcr" ? (
          <FCRTab
            pageContract={pageContract}
            fcr={fcr?.ok ? fcr.data : null}
            pager={{
              offset,
              limit,
              pageSizeOptions: PAGE_SIZE_OPTIONS,
              hrefForOffset: (next) => hrefWith(params, { offset: String(next) }),
              hrefForLimit: (next) => hrefWith(params, { limit: String(next), offset: null }),
            }}
          />
        ) : null}
      </div>
    </div>
  );
}

function WeightsAnalyticsLoadError({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <section className="card">
      <h2 className="h">{copy(pageContract, "error.load.title")}</h2>
      <p className="muted small">{copy(pageContract, "error.load.body")}</p>
    </section>
  );
}

/**
 * GENERAL — the Weights page's own summary, unchanged.
 *
 * Deliberately the same figures in the same order rather than an analytics-flavoured rewrite: a
 * reader arriving from Weights must recognise the numbers before the tabs beside this one
 * re-slice them, and two screens stating the same fact differently is how a page stops being
 * believed.
 */
function GeneralTab({
  pageContract,
  summary,
  rows,
  parks,
  parkFilter,
  modeFilter,
  growth,
  demo,
  perParkGain,
  limit,
  offset,
  params,
  saleThresholdKg,
  saleLowerKg,
}: {
  pageContract: AdminUiPageContract;
  summary: ShedWeightsSummary;
  rows: readonly ShedWeightsRow[];
  parks: ShedWeightsResponse["parks"];
  parkFilter: string;
  modeFilter: string;
  growth: WeighingGrowthResponse | null;
  /** The `composition` section only; a FAILED demographics read takes the page down above. */
  demo: WeightDemographicsResponse | null;
  perParkGain: readonly { name: string; gain: number | null; animals: number }[];
  limit: number;
  offset: number;
  params: RouteSearchParams;
  /** The sale lines the over-N counts were taken against; null when the assumptions read failed. */
  saleThresholdKg: number | null;
  saleLowerKg: number | null;
}) {
  const weighedRows = rows.filter((row) => row.animals_weighed > 0);
  const modeRows =
    modeFilter === "all" ? weighedRows : weighedRows.filter((row) => row.weighing_category === modeFilter);
  // The pens table's weight filter (maintainer request 2026-09-03): the feed config's
  // more-than / less-than control, applied with one button. It narrows THIS TABLE ONLY -- the
  // KPI cards above keep answering for the whole selection, or "pens over 30 kg" would silently
  // rewrite the herd's average. An unparseable value is no filter, never a filter on NaN.
  const weightOp = one(params, WEIGHT_OP_PARAM) ?? "";
  const weightValueRaw = one(params, WEIGHT_VALUE_PARAM) ?? "";
  const weightValue = Number(weightValueRaw);
  const weightFilterOn = weightOp !== "" && weightValueRaw.trim() !== "" && Number.isFinite(weightValue);
  const visibleRows = weightFilterOn
    ? modeRows.filter((row) => compareKg(row.average_weight_kg, weightOp, weightValue))
    : modeRows;
  const slice = visibleRows.slice(offset, offset + limit);
  const weightCompareOptions = optionGroup(pageContract, "weight_kg_compare").map((option) => ({
    value: option.key,
    label: option.label,
  }));
  const pensFilterFields: WorklistFilterField[] = [
    {
      kind: "compare",
      param: WEIGHT_OP_PARAM,
      valueParam: WEIGHT_VALUE_PARAM,
      label: copy(pageContract, "filter.weight.label"),
      op: weightOp,
      value: weightValueRaw,
      options: weightCompareOptions,
      valueAriaLabel: copy(pageContract, "filter.weight.value_aria"),
      note: copy(pageContract, "filter.weight.note"),
    },
  ];
  const hasAnyData = summary.animals_weighed > 0;

  // The backend's ONE daily-gain number: the animal-weighted mean over kids weighed twice PLUS
  // whole-shed pens. Every gain figure on every other tab is this same statistic, cut a
  // different way, so this card and those charts can never describe different herds.
  // Per-shed daily gain, keyed by (location, partition) -- the SAME two figures the Shed-wise tab
  // draws, so the table and that chart can never disagree about a pen. A scanned shed reports the
  // mean of its kids' own gains; a whole-shed pen reports how fast its average is moving. They
  // are different measurements, which is why the row's existing capture-mode chip has to stay
  // beside this column: without it the two would read as one number.
  // The pen's BREED, from the same `shed_composition` chips the Weights table renders as cohort
  // cells (maintainer request 2026-09-21). One breed among the pen's live residents is named;
  // SEVERAL read "Mixed breeds" rather than picking the largest, because one pen average cannot
  // be split across breeds and naming one of them would claim a herd nobody weighed. A pen the
  // register shows no cohort for reads as no data -- absence, never a breed.
  const breedByShedKey = new Map<string, string>();
  for (const item of demo?.shed_composition ?? []) {
    const chips = item.chips ?? [];
    if (chips.length === 0) continue;
    const named = new Set(chips.map((chip) => chip.breed?.trim() ?? "").filter((breed) => breed !== ""));
    const unnamed = chips.some((chip) => (chip.breed?.trim() ?? "") === "");
    breedByShedKey.set(
      shedKey(item.location_id, item.partition_label),
      named.size === 0
        ? copy(pageContract, "value.shed.unknown")
        : named.size === 1 && !unnamed
          ? [...named][0]
          : copy(pageContract, "value.shed.mixed"),
    );
  }

  // A scanned pen's gain is the MEAN of its kids' own gains -- the headline's statistic, so the pen
  // rows add up to the card above and the FCR tab reads the same figure (maintainer decision
  // 2026-09-24). The leaderboard's median is of per-leg rates, which one 1-day re-weigh can decide.
  const shedGainByKey = new Map<string, number>();
  for (const shed of growth?.shed_leaderboard ?? []) {
    if (shed.average_adg_g_per_day != null && shed.adg_animals > 0) {
      shedGainByKey.set(shedKey(shed.location_id, shed.partition_label), shed.average_adg_g_per_day);
    }
  }

  const headlineGain = growth ? (growth.headline.average_adg_g_per_day ?? null) : null;
  const headlineAnimals = growth ? growth.headline.headline_animals : 0;
  const selectedParkName = parks.find((park) => park.park_id === parkFilter)?.name ?? "";

  return (
    <>
      <div className="wt-general-metrics">
        <section className="grid g5 kpi-row" aria-label={copy(pageContract, "section.sheds.aria")}>
          <div className="kpi">
            <div className="lab">{copy(pageContract, "kpi.kids.split.label")}</div>
            <div className="val">
              {summary.individual_animals_weighed.toLocaleString("en-IN")} ·{" "}
              {summary.lump_sum_animals_weighed.toLocaleString("en-IN")}
            </div>
            <div className="dl">
              {summary.animals_weighed.toLocaleString("en-IN")} {copy(pageContract, "kpi.kids.split.total_sub")}
            </div>
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
            <div className="lab">{fillKg(copy(pageContract, "kpi.over30.label"), saleLowerKg ?? DEFAULT_SALE_READY_LOWER_KG)}</div>
            <div className="val">{summary.at_or_above_30kg.toLocaleString("en-IN")}</div>
            <div className="dl">
              {summary.threshold_basis_animals.toLocaleString("en-IN")} {copy(pageContract, "kpi.threshold.basis")}
            </div>
          </div>
          <div className="kpi">
            <div className="lab">{fillKg(copy(pageContract, "kpi.over35.label"), saleThresholdKg)}</div>
            <div className="val">{summary.at_or_above_35kg.toLocaleString("en-IN")}</div>
            <div className="dl">
              {summary.threshold_basis_animals.toLocaleString("en-IN")} {copy(pageContract, "kpi.threshold.basis")}
            </div>
          </div>
        </section>

        <section className="grid g3 kpi-row" aria-label={copy(pageContract, "section.park_gain.aria")}>
          <div className="kpi">
            <div className="lab">
              {selectedParkName || copy(pageContract, "kpi.park_gain.all")}{" "}
              {copy(pageContract, "kpi.park_gain.suffix")}
            </div>
            {/* No gain is a real state: a period where nothing was weighed twice HAS no gain, and
                printing 0 g/day would read as a herd that stopped growing. */}
            <div className="val">
              {headlineGain == null ? copy(pageContract, "empty.no_data.title") : `${Math.round(headlineGain)} g`}
            </div>
            <div className="dl">
              {headlineGain == null
                ? copy(pageContract, "kpi.gain.none")
                : `${copy(pageContract, "kpi.gain.blended")} · ${headlineAnimals.toLocaleString("en-IN")}`}
            </div>
          </div>
          {perParkGain.map((park) => (
            <div className="kpi" key={park.name}>
              <div className="lab">
                {park.name} {copy(pageContract, "kpi.park_gain.suffix")}
              </div>
              {/* A park where nothing was weighed twice HAS no gain. Printing 0 g/day would read as
                  a park whose kids stopped growing, which is a different and untrue statement. */}
              <div className="val">
                {park.gain == null ? copy(pageContract, "empty.no_data.title") : `${Math.round(park.gain)} g`}
              </div>
              <div className="dl">
                {park.gain == null
                  ? copy(pageContract, "kpi.gain.none")
                  : `${copy(pageContract, "kpi.gain.blended")} · ${park.animals.toLocaleString("en-IN")}`}
              </div>
            </div>
          ))}
        </section>
      </div>

      <section className="card wtable" aria-label={copy(pageContract, "section.sheds.aria")}>
        <h2 className="h">
          <Warehouse className="ic" size={15} aria-hidden /> {copy(pageContract, "section.sheds.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "note.total_weight")}</p>
        {/* STAGED, not applied per keystroke: the operator and the value are one question, so the
            bar collects both and a single Apply commits them (deferApply). Scoped to the pens
            table; the page's own bar above stays as it is. */}
        <WorklistFilters
          basePath={PAGE_PATH}
          pageParam="offset"
          fields={pensFilterFields}
          pageContract={pageContract}
          deferApply
          telemetry={{ eventPrefix: "weights_analytics_pens_filter_apply", surface: "pens_table", route: PAGE_PATH }}
        />
        {slice.length === 0 ? (
          <div className="empty">
            <b>{hasAnyData ? copy(pageContract, "empty.filtered.title") : copy(pageContract, "empty.no_data.title")}</b>
            <span className="muted small">
              {hasAnyData ? copy(pageContract, "empty.filtered.body") : copy(pageContract, "empty.no_data.body")}
            </span>
          </div>
        ) : (
          <>
            <div className="tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "section.sheds.aria")}>
              <PensTable
                contract={table(pageContract, "shed-weights")}
                rows={slice.map((row): PensTableRow => ({
                  key: shedKey(row.location_id, row.partition_label),
                  park: row.park_name,
                  pen: row.operational_location_display || row.shed_display_name,
                  breed: breedByShedKey.get(shedKey(row.location_id, row.partition_label)) ?? null,
                  weighingCategory: row.weighing_category,
                  animals: row.animals_weighed,
                  averageKg: row.average_weight_kg,
                  // A whole-shed pen carries its own average-weight movement on the row; a
                  // scanned shed's gain comes from the growth leaderboard. A shed weighed ONCE
                  // in the window has neither -- a gain needs two weighs -- and reads as no
                  // data rather than as 0 g/day, which would claim it stopped growing.
                  gainGPerDay:
                    row.shed_average_gain_g_per_day ??
                    shedGainByKey.get(shedKey(row.location_id, row.partition_label)) ??
                    null,
                  totalKg: row.total_weight_kg,
                  lastWeighed: row.last_weighed_date ?? null,
                }))}
                labels={{
                  ariaLabel: copy(pageContract, "section.sheds.aria"),
                  individual: copy(pageContract, "value.weighing.individual"),
                  lump: copy(pageContract, "value.weighing.lump"),
                  noData: copy(pageContract, "empty.no_data.title"),
                  neverWeighed: copy(pageContract, "value.never_weighed"),
                  empty: copy(pageContract, "empty.filtered.title"),
                }}
              />
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
    </>
  );
}

/**
 * BREED-WISE — one chart, two bars per breed: daily gain and average weight.
 *
 * The two bars are NOT the same population and the caption says so: weight covers every kid
 * weighed, gain only those weighed twice plus the whole-shed pens that moved. They share a
 * group because a reader comparing breeds wants both facts about a breed together; they do not
 * share a SCALE, because g/day and kg are not comparable lengths.
 */
function BreedTab({ pageContract, demo }: { pageContract: AdminUiPageContract; demo: WeightDemographicsResponse | null }) {
  const gainByBreed = new Map((demo?.gain_by_breed ?? []).map((bucket) => [bucket.label, bucket]));
  const weightByBreed = new Map((demo?.by_breed ?? []).map((bucket) => [bucket.label, bucket]));
  // Every breed that has EITHER fact, so a breed with weights but no second weigh still appears
  // with its weight bar rather than vanishing from the chart entirely.
  const breeds = [...new Set([...weightByBreed.keys(), ...gainByBreed.keys()])].sort((a, b) =>
    a.localeCompare(b, undefined, { numeric: true }),
  );

  const groups: BarGroup[] = breeds.map((breed) => {
    const gain = gainByBreed.get(breed);
    const weight = weightByBreed.get(breed);
    const bars: GroupedBar[] = [];
    if (gain) {
      bars.push({
        key: `${breed}-gain`,
        label: copy(pageContract, "series.gain"),
        value: Math.round(gain.median_gain_g_per_day),
        seriesKey: "gain",
        noteLabel: `${gain.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
      });
    }
    if (weight) {
      bars.push({
        key: `${breed}-weight`,
        label: copy(pageContract, "series.weight"),
        value: Number(weight.average_weight_kg.toFixed(1)),
        seriesKey: "weight",
        noteLabel: `${weight.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
      });
    }
    return { key: breed, heading: breed, bars };
  });

  return (
    <section className="card wchart" aria-label={copy(pageContract, "section.breed.aria")}>
      <h2 className="h">
        <Sprout className="ic" size={15} aria-hidden /> {copy(pageContract, "section.breed.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.breed.caption")}</p>
      <GroupedBars
        groups={groups}
        series={[
          { key: "gain", label: copy(pageContract, "series.gain"), unit: "g", fractionDigits: 0 },
          { key: "weight", label: copy(pageContract, "series.weight"), unit: "kg", fractionDigits: 1 },
        ]}
        emptyLabel={copy(pageContract, "empty.breed.body")}
        chartLabel={copy(pageContract, "section.breed.aria")}
      />
    </section>
  );
}

/**
 * BIRTH-WISE — farm born against purchased, per breed.
 *
 * A breed with only one kind shows ONE bar, which is the honest rendering: the farm buys sheep
 * and breeds goats, and drawing an empty bar for the side that does not exist would read as a
 * cohort growing at zero. The two sides also need not add up to the breed's own total — a kid
 * whose origin is not recorded is claimed by neither — and the caption states that rather than
 * leaving the gap to look like missing data.
 */
function BirthTab({ pageContract, demo }: { pageContract: AdminUiPageContract; demo: WeightDemographicsResponse | null }) {
  const buckets = demo?.gain_by_breed_origin ?? [];
  const born = new Map(buckets.filter((b) => b.origin === "farm_born").map((b) => [b.label, b]));
  const bought = new Map(buckets.filter((b) => b.origin === "purchased").map((b) => [b.label, b]));
  const breeds = [...new Set([...born.keys(), ...bought.keys()])].sort((a, b) =>
    a.localeCompare(b, undefined, { numeric: true }),
  );

  const groups: BarGroup[] = breeds.map((breed) => {
    const bars: GroupedBar[] = [];
    const bornBucket = born.get(breed);
    const boughtBucket = bought.get(breed);
    if (bornBucket) {
      bars.push({
        key: `${breed}-born`,
        label: copy(pageContract, "view.origin.farm_born"),
        value: Math.round(bornBucket.median_gain_g_per_day),
        seriesKey: "farm_born",
        noteLabel: `${bornBucket.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
      });
    }
    if (boughtBucket) {
      bars.push({
        key: `${breed}-bought`,
        label: copy(pageContract, "view.origin.purchased"),
        value: Math.round(boughtBucket.median_gain_g_per_day),
        seriesKey: "purchased",
        noteLabel: `${boughtBucket.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
      });
    }
    return { key: breed, heading: breed, bars };
  });

  return (
    <section className="card wchart" aria-label={copy(pageContract, "section.birth.aria")}>
      <h2 className="h">
        <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "section.birth.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.birth.caption")}</p>
      <GroupedBars
        groups={groups}
        // Both sides share the g/day scale, unlike Breed-wise: these two bars ARE the same
        // measure over two cohorts, which is the entire comparison.
        series={[
          { key: "farm_born", scaleKey: "gain", label: copy(pageContract, "view.origin.farm_born"), unit: "g", fractionDigits: 0 },
          { key: "purchased", scaleKey: "gain", label: copy(pageContract, "view.origin.purchased"), unit: "g", fractionDigits: 0 },
        ]}
        emptyLabel={copy(pageContract, "empty.birth.body")}
        chartLabel={copy(pageContract, "section.birth.aria")}
      />
    </section>
  );
}

/**
 * PEN-WISE — daily gain per breed, one bar per PEN TYPE.
 *
 * The pen types, their names and their order are the farm's own register (Configuration -> Items
 * and settings -> Pen types, migration 000437), served on the page contract as the `pen_types`
 * option group. Weighing returns only each bucket's type CODE, so nothing here names a pen type:
 * a type the farm adds tomorrow becomes a new series with no code change (maintainer instruction
 * 2026-09-25).
 */
function ShedTab({
  pageContract,
  demo,
}: {
  pageContract: AdminUiPageContract;
  demo: WeightDemographicsResponse | null;
}) {
  const buckets = demo?.gain_by_breed_shed_type ?? [];
  const penTypes = pageContract.option_groups.find((group) => group.id === "pen_types")?.options ?? [];
  const withData = new Set(buckets.map((b) => b.shed_type));
  // Legend order is the register's. An ACTIVE type is listed even with no bar, so a missing side
  // reads as absent rather than unmentioned; an archived type only while its pens still count.
  const series = penTypes
    .filter((type) => type.title !== "archived" || withData.has(type.key))
    .map((type) => ({ key: type.key, scaleKey: "gain", label: type.label, unit: "g", fractionDigits: 0 }));
  const known = new Set(series.map((entry) => entry.key));
  const byType = new Map<string, Map<string, (typeof buckets)[number]>>();
  for (const bucket of buckets) {
    if (!known.has(bucket.shed_type)) continue;
    const perBreed = byType.get(bucket.shed_type) ?? new Map();
    perBreed.set(bucket.label, bucket);
    byType.set(bucket.shed_type, perBreed);
  }
  const breeds = [...new Set(buckets.filter((b) => known.has(b.shed_type)).map((b) => b.label))].sort((a, b) =>
    a.localeCompare(b, undefined, { numeric: true }),
  );
  // WHICH PENS ARE BEHIND THIS BAR. A pen's type is configured per partition in Configuration --
  // a setting the chart cannot show, so the reader was being asked to accept each bar on trust.
  // The backend sends the pens each bar actually counted, already composed and already in natural
  // order, and each bar carries them behind a small `i`.
  //
  // PER BAR, not per series (maintainer, 2026-09-02): a pen holds one breed, so a list on the
  // legend would name mostly pens behind some OTHER breed's bar. These are also the CONTRIBUTING
  // pens only -- a classified pen weighed once is absent from the bar and absent here for the same
  // reason.
  //
  // GROUPED BY PARK when the list spans more than one. A shed name is NOT unique across parks --
  // this farm has a "Castro 1" in each -- so a flat list would show one name twice with nothing to
  // tell the two pens apart. With a park selected there is one group and no heading, so the
  // ordinary case stays a plain list. Park names, like the shed names, arrive from the backend.
  const members = demo?.shed_type_members ?? [];
  const hintFor = (breed: string, shedType: string) => {
    const mine = members.filter((m) => m.label === breed && m.shed_type === shedType);
    const parks: string[] = [];
    for (const member of mine) {
      const park = member.park_name ?? "";
      if (!parks.includes(park)) parks.push(park);
    }
    const headed = parks.length > 1;
    return {
      ariaLabel: copy(pageContract, "section.shed.members_hint"),
      title: copy(pageContract, "section.shed.members_title"),
      sections: parks.map((park) => ({
        // A row whose park did not resolve keeps its own unheaded section rather than being filed
        // under a park it may not belong to.
        heading: headed && park ? park : undefined,
        items: mine
          .filter((m) => (m.park_name ?? "") === park)
          .map((m) => m.operational_location_display),
      })),
      emptyLabel: copy(pageContract, "empty.shed.members"),
    };
  };

  const groups: BarGroup[] = breeds.map((breed) => {
    const bars: GroupedBar[] = [];
    for (const type of series) {
      const bucket = byType.get(type.key)?.get(breed);
      if (!bucket) continue;
      bars.push({
        key: `${breed}-${type.key}`,
        label: type.label,
        value: Math.round(bucket.average_gain_g_per_day),
        seriesKey: type.key,
        noteLabel: `${bucket.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
        hint: hintFor(breed, type.key),
      });
    }
    return { key: breed, heading: breed, bars };
  });

  return (
    <section className="card wchart" aria-label={copy(pageContract, "section.shed.aria")}>
      <h2 className="h">
        <Warehouse className="ic" size={15} aria-hidden /> {copy(pageContract, "section.shed.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.shed.caption")}</p>
      <GroupedBars
        groups={groups}
        series={series}
        emptyLabel={copy(pageContract, "empty.shed.body")}
        chartLabel={copy(pageContract, "section.shed.aria")}
      />
    </section>
  );
}

/**
 * WEIGHT-WISE — how many animals stand in each weight bracket, and how fast each is growing.
 *
 * BOTH WAYS OF WEIGHING COUNT, which on this farm is the whole point: a scanned animal is banded by
 * its own latest weight, and a whole-shed pen by the pen's average, contributing ALL the animals it
 * holds to that one bracket. Most of this farm's kids are weighed by the shed, so banding only the
 * scanned ones would describe the herd from a minority of it.
 *
 * The two numbers per bracket deliberately have DIFFERENT denominators, and the row says so: the
 * head count is every animal standing there, the gain comes only from those weighed twice. A
 * bracket where nothing was weighed twice shows no gain rather than 0 g/day, which would read as a
 * bracket that stopped growing.
 */
function WeightTab({
  pageContract,
  demo,
  feedBand,
  params,
  periodLabel,
}: {
  pageContract: AdminUiPageContract;
  demo: WeightDemographicsResponse | null;
  feedBand: FeedWeightBandResponse | null;
  params: RouteSearchParams;
  periodLabel: string;
}) {
  const bands = demo?.by_weight_band ?? [];
  // The backend orders these ascending and owns the band keys; the farm words come from the page
  // contract, so a bracket the contract cannot name is never drawn with its raw key.
  const groups: BarGroup[] = bands
    .filter((band) => band.animals > 0)
    .map((band) => {
      const bars: GroupedBar[] = [
        {
          key: `${band.band}-animals`,
          label: copy(pageContract, "series.animals"),
          value: band.animals,
          seriesKey: "animals",
        },
      ];
      if (band.average_gain_g_per_day != null) {
        bars.push({
          key: `${band.band}-gain`,
          label: copy(pageContract, "series.gain"),
          value: Math.round(band.average_gain_g_per_day),
          seriesKey: "gain",
          // The gain's own denominator rides on the bar, because it is not the head count beside it.
          noteLabel: `${band.gain_animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
        });
      }
      return {
        key: band.band,
        // The farm words come from the backend with the bracket: the edges are the tenant's
        // assumption, so a page contract cannot carry one label per key any more.
        heading: band.label,
        // A bracket with animals but no second weigh says so, rather than leaving the reader to
        // wonder whether the gain bar failed to render.
        subheading:
          band.average_gain_g_per_day == null ? copy(pageContract, "value.weight.no_gain") : undefined,
        bars,
      };
    });

  return (
    <>
    <section className="card wchart" aria-label={copy(pageContract, "section.weight.aria")}>
      <h2 className="h">
        <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "section.weight.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.weight.caption")}</p>
      <GroupedBars
        groups={groups}
        // Two scales, because a head count and a growth rate are not comparable lengths -- the same
        // reason Breed-wise keeps weight and gain apart.
        series={[
          { key: "animals", label: copy(pageContract, "series.animals"), unit: "", fractionDigits: 0 },
          { key: "gain", label: copy(pageContract, "series.gain"), unit: "g", fractionDigits: 0 },
        ]}
        emptyLabel={copy(pageContract, "empty.weight.body")}
        chartLabel={copy(pageContract, "section.weight.aria")}
      />
    </section>
    <FeedWeightBandSection pageContract={pageContract} feedBand={feedBand} params={params} periodLabel={periodLabel} />
    </>
  );
}

/**
 * FEED BY WEIGHT BAND -- directly under the bracket chart. The payload is fetched once per page
 * read (Park / Period / Weighing / Sex / Origin); every control on the card is client-side over
 * it in FeedWeightBandCard. This wrapper only parses the card's initial state from the URL.
 */
function FeedWeightBandSection({
  pageContract,
  feedBand,
  params,
  periodLabel,
}: {
  pageContract: AdminUiPageContract;
  feedBand: FeedWeightBandResponse | null;
  params: RouteSearchParams;
  /** The selected period as the page shows it (DD/MM/YYYY – DD/MM/YYYY), for the panel header. */
  periodLabel: string;
}) {
  return (
    <FeedWeightBandCard
      pageContract={pageContract}
      feedBand={feedBand}
      periodLabel={periodLabel}
      initial={{
        view: one(params, FEED_BAND_VIEW_PARAM) ?? "",
        type: one(params, FEED_BAND_TYPE_PARAM) ?? "",
        source: one(params, FEED_BAND_SOURCE_PARAM) ?? "",
        band: one(params, FEED_BAND_BAND_PARAM) ?? "",
        pen: one(params, FEED_BAND_PEN_PARAM) ?? "",
        group: one(params, FEED_BAND_GROUP_PARAM) ?? "",
        animals: one(params, FEED_BAND_ANIMALS_PARAM) ?? "",
        search: one(params, FEED_BAND_SEARCH_PARAM) ?? "",
        limit: Number(one(params, FEED_BAND_LIMIT_PARAM) ?? 25),
        offset: boundedOffset(one(params, FEED_BAND_OFFSET_PARAM)),
        exitScope: one(params, "fb_exit"),
      }}
    />
  );
}

/**
 * TIME-WISE — weekly daily gain inside the selected period.
 *
 * Reads `weekly_gain`, NOT `trend`. They look interchangeable and are not: `trend` is the median
 * over SCANNED PAIRS ONLY, while this page's own headline is the animal-weighted mean over those
 * pairs PLUS whole-shed pens. Most of this farm's kids are weighed by the whole shed, so a chart
 * built on `trend` would sit here disagreeing with the General tab about the same herd — the
 * exact defect the 2026-08-26 one-number decision exists to prevent.
 *
 * A week nobody weighed in is ABSENT rather than drawn at zero. Interpolating or zero-filling
 * would state that the kids stopped growing in a week when the truth is that nobody looked.
 */
function TimeTab({
  pageContract,
  growth,
  demo,
  gainBucket,
  penKey,
  pens,
}: {
  pageContract: AdminUiPageContract;
  growth: WeighingGrowthResponse | null;
  demo: WeightDemographicsResponse | null;
  /** `week` or `month` — which bucket the backend cut EVERY series on this tab into. */
  gainBucket: string;
  /** The selected pen key (`location::partition`), or "" for every pen. */
  penKey: string;
  /**
   * Every pen weighed in the selected period, from the shed read the pen scope does NOT narrow —
   * so the picker still offers the way back once a pen is chosen.
   */
  pens: readonly { key: string; park: string; label: string }[];
}) {
  // Every heading, caption and empty state on this tab has a WEEK wording and a 30-DAY wording,
  // both authored in the page contract. This picks the pair that matches the bucket the backend
  // actually cut the data into; it never edits a string, so a heading can never describe columns
  // the table is not showing.
  const bucketCopy = (key: string) => copy(pageContract, gainBucket === "month" ? `${key}.month` : key);

  // The pen picker's vocabulary, keyed on (location, partition) rather than on the name -- a pen
  // name repeats across parks, and keying on it would merge two real pens into one option. It is
  // built from the pens WEIGHED IN THE PERIOD (the shed read), not from the narrowed response: once
  // a pen is selected the response holds only that pen, and a picker built from it would offer no
  // way back.
  const penOptions: { value: string; label: string }[] = [];
  const seenPens = new Set<string>();
  for (const pen of pens) {
    if (seenPens.has(pen.key)) continue;
    seenPens.add(pen.key);
    penOptions.push({ value: pen.key, label: `${pen.park} · ${pen.label}` });
  }
  penOptions.sort((a, b) => a.label.localeCompare(b.label));
  // A selection the vocabulary does not contain -- a stale link, or a pen not weighed inside the
  // newly selected period -- STILL NARROWS THE READS, so it must still show as a selection. Showing
  // it as "All" would put an unscoped-looking control above a page narrowed to one pen, which
  // usually reads as an empty page rather than as a filter the reader can clear. It is offered as
  // its own option, named by backend copy, because nothing in this period can name that pen.
  const penListed = seenPens.has(penKey);
  if (penKey !== "" && !penListed) {
    penOptions.unshift({ value: penKey, label: copy(pageContract, "filter.time_pen.unlisted") });
  }
  const selectedPen = penKey;

  const timeFilterFields: WorklistFilterField[] = [
    {
      kind: "select",
      param: GAIN_BUCKET_PARAM,
      label: copy(pageContract, "filter.gain_bucket.label"),
      value: gainBucket,
      options: optionGroup(pageContract, "gain_bucket").map((option) => ({ value: option.key, label: option.label })),
      note: copy(pageContract, "filter.gain_bucket.note"),
    },
    {
      kind: "select",
      param: TIME_PEN_PARAM,
      label: copy(pageContract, "filter.time_pen.label"),
      value: selectedPen,
      allowAll: true,
      options: penOptions,
      note: copy(pageContract, "filter.time_pen.note"),
    },
  ];

  const bars = (growth?.weekly_gain ?? []).map((point) => ({
    key: point.week_start,
    label: fmtDate(point.week_start),
    value: Math.round(point.average_adg_g_per_day),
    valueLabel: `${Math.round(point.average_adg_g_per_day).toLocaleString("en-IN")} g`,
    modeLabel: `${point.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
    modeTone: "mut" as const,
  }));

  // One group per breed, its weeks in order. Ordered by breed so a reader finds the same row in the
  // same place each week; the backend already returns the rows sorted by (breed, week).
  const breedWeekGroups: BarGroup[] = [];
  for (const point of demo?.gain_by_breed_week ?? []) {
    let group = breedWeekGroups.find((entry) => entry.key === point.label);
    if (!group) {
      group = { key: point.label, heading: point.label, bars: [] };
      breedWeekGroups.push(group);
    }
    (group.bars as GroupedBar[]).push({
      key: `${point.label}-${point.week_start}`,
      label: fmtDate(point.week_start),
      value: Math.round(point.average_gain_g_per_day),
      seriesKey: "gain",
      noteLabel: `${point.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
    });
  }

  // The per-pen grid reads `gain_by_pen_week` as served -- ordered by park, pen, week -- and
  // composes no label of its own: the pen name is the backend's operational location display.
  const penWeekPoints: PenWeekGainPoint[] = (demo?.gain_by_pen_week ?? []).map((point) => ({
    locationId: point.location_id,
    partitionLabel: point.partition_label,
    park: point.park_name,
    pen: point.operational_location_display || point.shed_name,
    weekStart: point.week_start,
    animals: point.animals,
    gainGPerDay: point.average_gain_g_per_day,
  }));

  // The per-load grid reads `gain_by_load_week` as served -- ordered by load, week -- and renders
  // the farm's own load number and supplier verbatim.
  const loadWeekPoints: LoadWeekGainPoint[] = (demo?.gain_by_load_week ?? []).map((point) => ({
    loadRef: point.load_ref,
    source: point.owner_name,
    weekStart: point.week_start,
    animals: point.animals,
    gainGPerDay: point.average_gain_g_per_day,
  }));

  return (
    <>
    {/* One bar for the whole tab: both controls govern all four sections below, so a control
        sitting on any one of them would read as narrower than it is. */}
    <WorklistFilters
      basePath={PAGE_PATH}
      pageParam="offset"
      fields={timeFilterFields}
      pageContract={pageContract}
      telemetry={{ eventPrefix: "weights_analytics_time_filter_apply", surface: "time_wise", route: PAGE_PATH }}
    />
    <section className="card wchart" aria-label={bucketCopy("section.time.aria")}>
      <h2 className="h">
        <CalendarRange className="ic" size={15} aria-hidden /> {bucketCopy("section.time.title")}
      </h2>
      <p className="muted small">{bucketCopy("section.time.caption")}</p>
      <WeightBars
        data={bars}
        emptyLabel={bucketCopy("empty.time.body")}
        unit="g"
        chartLabel={bucketCopy("section.time.aria")}
        size="bands"
        wide
      />
      <p className="muted small">{bucketCopy("note.time.gaps")}</p>
    </section>
    {/* The selected period's weeks, one row per breed. A second section rather than more series on
        the chart above: many weeks across six breeds is dense, and stacking them on one axis
        answers "which breed" more slowly than six short rows do.

        The breed rows need not add up to the overall trend, and the caption says so -- a shed
        holding more than one breed counts in the overall series and in no breed here, because one
        shed average cannot be divided between two cohorts. */}
    <section className="card wchart" aria-label={bucketCopy("section.time.breed.aria")}>
      <h2 className="h">
        <Sprout className="ic" size={15} aria-hidden /> {bucketCopy("section.time.breed.title")}
      </h2>
      <p className="muted small">{bucketCopy("section.time.breed.caption")}</p>
      <GroupedBars
        groups={breedWeekGroups}
        series={[{ key: "gain", label: copy(pageContract, "series.gain"), unit: "g", fractionDigits: 0 }]}
        emptyLabel={bucketCopy("empty.time.breed.body")}
        chartLabel={bucketCopy("section.time.breed.aria")}
      />
    </section>
    {/* Every pen, every week (maintainer request 2026-09-08): the weekly line above cut one pen at
        a time. A TABLE rather than a third chart: the farm has dozens of pens and the question is
        "how did THIS pen do THIS week", which a grid answers on sight and a forest of bars does
        not. Unlike the breed rows a pen needs no single-cohort claim to be itself, so a mixed pen
        is listed here; only the page's own filters narrow it. */}
    <section className="card wchart" aria-label={bucketCopy("section.time.pen.aria")}>
      <h2 className="h">
        <LayoutGrid className="ic" size={15} aria-hidden /> {bucketCopy("section.time.pen.title")}
      </h2>
      <p className="muted small">{bucketCopy("section.time.pen.caption")}</p>
      {/* A long period is many week columns, so the grid scrolls inside its own box rather
          than pushing the page sideways. */}
      <div className="tablewrap" style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={bucketCopy("section.time.pen.aria")}>
        <PenWeekGainTable
          contract={table(pageContract, "pen-week-gain")}
          points={penWeekPoints}
          labels={{
            ariaLabel: bucketCopy("section.time.pen.aria"),
            blank: copy(pageContract, "value.time.pen.blank"),
            unit: copy(pageContract, "value.time.pen.unit"),
            animals: copy(pageContract, "value.time.animals"),
            empty: bucketCopy("empty.time.pen.body"),
          }}
        />
      </div>
    </section>
    {/* Every purchased load, every week (maintainer request 2026-09-14, "Time-wise ADG for each
        shed/load"): the pen grid above one grain up. A load is its tagged pens -- the SAME
        attribution the Load-wise tab uses -- so its weekly figure is those pens' gain weighted by
        animals, and a pen tagged to two loads counts toward neither. The same grid shape, because
        the question is the same: "how did THIS load do THIS week". */}
    <section className="card wchart" aria-label={bucketCopy("section.time.load.aria")}>
      <h2 className="h">
        <PackageOpen className="ic" size={15} aria-hidden /> {bucketCopy("section.time.load.title")}
      </h2>
      <p className="muted small">{bucketCopy("section.time.load.caption")}</p>
      <div className="tablewrap" style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={bucketCopy("section.time.load.aria")}>
        <LoadWeekGainTable
          contract={table(pageContract, "load-week-gain")}
          points={loadWeekPoints}
          labels={{
            ariaLabel: bucketCopy("section.time.load.aria"),
            blank: copy(pageContract, "value.time.pen.blank"),
            unit: copy(pageContract, "value.time.pen.unit"),
            animals: copy(pageContract, "value.time.animals"),
            empty: bucketCopy("empty.time.load.body"),
          }}
        />
      </div>
    </section>
    </>
  );
}
