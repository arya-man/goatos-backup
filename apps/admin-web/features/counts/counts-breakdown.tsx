import { splitParts } from "@/components/minimal/widgets";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { TableSkeleton } from "@/components/app/skeletons";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import type { SvgBarDatum, SvgStackedDatum } from "@/components/svg-bars";
import Card from "@mui/material/Card";
import Box from "@mui/material/Box";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import { EmptyContent } from "@/components/minimal/empty-content";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";
import {
  EcommerceSalesOverview,
  type EcommerceSalesOverviewItem,
} from "@/components/minimal/sections/overview/e-commerce/ecommerce-sales-overview";
import { AnalyticsWebsiteVisits } from "@/components/minimal/sections/overview/analytics/analytics-website-visits";
import { CountsShedChart } from "./counts-shed-chart";
import { PageHeader } from "@/components/app/page-header";
import { KpiGrid } from "@/components/minimal/widgets";
import { dash } from "@/lib/format";
import { control, controlEnabled, copy, optionGroup, table, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getCountsBreakdown,
  listAnimalStages,
  type CountsBreakdownResponse,
  type CountsBreakdownSeriesPoint,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import {
  paginationFromParams,
  VaccinationTablePager,
  type VaccinationPageSize,
} from "@/features/preventive-care-vaccination";
import { CountsBreakdownFilters, type BreakdownFilterField } from "./counts-breakdown-filters";
import "./counts-breakdown.css";
import { LEGACY_FARM_PARAM, PARK_PARAM, withSelectedOptions } from "./counts-breakdown-query";
import { CountsBreakdownLoads } from "./counts-breakdown-loads";
import { CountsBreakdownPensTable } from "./counts-breakdown-pens-table";
import { buildShedFilterOptions } from "./counts-breakdown-sheds";
import { buildCountsSummaryCards, countsSexDetail } from "./counts-summary-cards";
import type { StageOption } from "./shed-stage-actions";
import type { InlineChoice } from "./inline-cell-editor";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";

// Counts -> Counts Breakdown. The census view: how many live animals stand in each PEN, with the
// breed, gender and stage mix on the same line, and the exact farm x stage x breed x gender x pen
// combinations one click away under each pen (maintainer decision 2026-09-04) — plus the same
// numbers as distributions.
//
// Population is live animals only (lifecycle_status=alive), matching Counts -> Herd Register,
// so the two Counts tabs never disagree about the denominator.
//
// Stage is goats.management_stage read RAW. That column has no CHECK constraint and the importer
// writes the source sheet cell verbatim, so near-duplicate labels ("ICU-Kid" vs "ICU-Kids") show
// up as separate rows. That is deliberate: collapsing them here would hide a real source data
// problem. count_dimension_aliases exists to fix it at the source when Data Ops chooses to.
//
// Pagination note: this table shows a real total and numbered pages, unlike Herd Register's
// cursor pager. That is NOT a regression against the million-goat rule in AGENTS.md failure mode
// 6c — that rule bans COUNT(*) over PER-GOAT rows. These rows are pre-aggregated grain
// combinations (tens, not millions), and the totals come from SQL window functions over the full
// grouped set, so they cost nothing extra and are exact.

const PAGE_PATH = "/counts/breakdown";

/** Legacy summary-card tone names -> kit tones (presentation only). */
const NO_SPARK = { categories: [], series: [] };
const ROW_COLORS = ["primary", "info", "warning", "success", "secondary", "error"] as const;

/** Breed rows as template EcommerceSalesOverview progress rows: count + share of the matching herd. */
function shareRows(bars: SvgBarDatum[]): EcommerceSalesOverviewItem[] {
  const total = bars.reduce((sum, bar) => sum + Math.max(bar.value, 0), 0);
  return bars.map((bar, i) => ({
    key: bar.key,
    label: bar.label,
    value: total > 0 ? Math.round((Math.max(bar.value, 0) / total) * 1000) / 10 : 0,
    display: bar.value.toLocaleString("en-IN"),
    color: ROW_COLORS[i % ROW_COLORS.length],
  }));
}
const DEFAULT_PAGE_SIZE = 10;

type AnimalStageOptionItem = {
  stage_code: string;
  name?: string | null;
  age_band?: string | null;
  assignable_as_cohort?: boolean;
};

function toBarData(points: CountsBreakdownSeriesPoint[], fallbackLabel: string): SvgBarDatum[] {
  return points.map((point) => ({
    key: point.key || fallbackLabel,
    label: stageLabel(point.label) || fallbackLabel,
    value: point.count,
  }));
}

export async function CountsBreakdownPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};

  // The park is ONE value, the shared `park` parameter every other page reads (maintainer
  // decision 2026-08-18: a page that hides the top-bar park control owns the park "on the same
  // `park` parameter"). The top-bar chip is hidden here, so the Farm dropdown below IS the park
  // control, and it writes `park` — never a page-private key. A private key is what made a park
  // picked on Herd Analytics render here as a greyed-out "All" over CPT-only numbers, and a farm
  // picked here vanish on the next Counts page.
  //
  // `bd_farm` is still READ, only so a link saved before this change keeps opening on its park;
  // the filter bar drops it on the next apply.
  const scope = parseScope(sp);
  const legacyFarmParkId = one(sp, LEGACY_FARM_PARAM);
  const parkId = backendScope(scope).parkId || legacyFarmParkId || undefined;

  // Stage, breed and shed are MULTI-VALUED (repeated URL params, OR within the dimension);
  // farm and gender stay single-valued (maintainer instruction, 2026-09-03).
  const all = (key: string): string[] => {
    const value = sp[key];
    return (Array.isArray(value) ? value : value ? [value] : []).filter(Boolean);
  };
  const shedParams = all("bd_shed");
  const stages = all("bd_stage");
  const breeds = all("bd_breed");
  const sex = one(sp, "bd_sex");

  // Each shed filter value is "shed_id" (whole shed) or "shed_id|partition_label" (one pen).
  // The API's `pen` parameter carries the same pair with "#" as the separator (the facet-key
  // convention), so the translation is only the separator swap.
  const pens = shedParams.map((value) => {
    const idx = value.indexOf("|");
    return idx < 0 ? value : `${value.slice(0, idx)}#${value.slice(idx + 1)}`;
  });

  const pageSizeOptions = tablePageSizes(pageContract, "pen-breakdown");
  const requestedLimit = Number(one(sp, "bd_limit"));
  const pageSize: VaccinationPageSize = pageSizeOptions.includes(requestedLimit)
    ? requestedLimit
    : DEFAULT_PAGE_SIZE;
  const requestedPage = Math.max(1, Number(one(sp, "bd_page")) || 1);

  // One round trip for the whole screen: rows, totals, all four chart series and the filter
  // facets (including the park-scoped shed vocabulary) come back together, so there is no
  // per-chart fan-out and no serial await.
  //
  // The stage-change picker's two vocabularies ride along in the SAME fan-out rather than a serial
  // await: neither depends on the breakdown, and both are small tenant reference sets.
  //
  const [breakdownResult, stageResult] = await Promise.all([
    getCountsBreakdown({
      park_id: parkId,
      pen: pens,
      management_stage: stages,
      breed: breeds,
      sex,
      // PEN grain: the page walks pens, and each pen carries its own combination rows nested, so
      // the drill-down needs no second read.
      group_by: "pen",
      limit: pageSize,
      offset: (requestedPage - 1) * pageSize,
    }),
    listAnimalStages(),
  ]);

  const authError = firstAuthRequiredError(breakdownResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const breakdown: CountsBreakdownResponse | null = breakdownResult.ok ? breakdownResult.data : null;
  const penRows = breakdown?.pens ?? [];
  const hasFilter = Boolean(parkId || shedParams.length || stages.length || breeds.length || sex);

  const noParkLabel = copy(pageContract, "label.unassigned_farm");
  const noShedLabel = copy(pageContract, "label.unassigned_shed");
  const noStageLabel = copy(pageContract, "label.unassigned_stage");
  const noBreedLabel = copy(pageContract, "label.unassigned_breed");
  const noSexLabel = copy(pageContract, "label.unassigned_sex");
  const emptyChartLabel = copy(pageContract, "chart.empty");
  const animalsNoun = copy(pageContract, "label.animals_noun");

  // The compiled table contract drives the table: column keys, labels, visibility and which
  // headers are sortable. An opened pen's rows render on the same columns. `cols` remains only
  // for the footer's colSpan.
  const pensTable = table(pageContract, "pen-breakdown");
  const cols = tableLabels(pageContract, "pen-breakdown");

  // True when the herd carries no recorded stage at all (every animal blank), which makes both
  // the Stage column and the stage chart uniformly empty.
  const stageFacets = breakdown?.facets.stages ?? [];
  // Stage code -> the label the BACKEND decided for it, taken from the response's own facet.
  // Reading it here rather than re-deriving one means the table, the chart and the Stage filter
  // are three renderings of one answer and cannot spell a stage three ways.
  // The tenant's stage NAME wins where the vocabulary has one (F2-Male -> "Fattening male",
  // K2 -> "Milk drinking"); the facet's own label is the fallback for a code it lacks.
  const stageNameByCode = new Map(
    (stageResult.ok ? listOrEmpty(stageResult.data.items) : []).filter((s) => s.name).map((s) => [s.stage_code, s.name as string] as const),
  );
  // Vocabulary entries are spread LAST so they win, and so a sexed code the facet does not carry
  // (the row's "F2-Male" under the facet's "F2") still resolves to its name.
  const stageLabels = new Map<string, string>([
    ...stageFacets.filter((point) => point.key).map((point) => [point.key, stageLabel(point.label || point.key)] as const),
    ...stageNameByCode,
  ]);
  const stageUnrecorded = stageFacets.length > 0 && stageFacets.every((point) => point.key === "");

  // The shed dropdown cascades to the selected park: only that park's sheds show, mirroring the
  // park facet (and the Android CountsViewModel, which narrows sheds by parkId).
  const selectedParkId = parkId || "";

  // park_id -> park code, from this response's own park facet.
  const parkLabelsById = new Map(
    (breakdown?.facets.parks ?? [])
      .filter((point) => point.key && point.label)
      .map((point) => [point.key, point.label] as const),
  );

  // Filter vocabularies come from the response's own facets so an option can never match zero
  // rows. Sheds specifically use `facets.sheds` (the backend's live-herd, park-scoped shed
  // vocabulary), NOT the locations master: shed NAMES repeat across parks (two thirds of them in
  // real data), so each option is KEYED BY park_id + shed_id to keep same-named sheds in
  // different parks distinct, while the option `value` stays the bare shed UUID that round-trips
  // to the backend as `shed_id`. Sourcing from the locations registry instead would hide
  // live-herd sheds missing from the master and show master sheds that hold zero live animals.
  //
  // Blank-valued facets are dropped from the DROPDOWNS (not from the table or charts, where they
  // stay visible as "No stage"/"No breed"). A blank key would render as <option value="">, which
  // collides with the "All" sentinel — two options sharing one value makes the browser select the
  // last, so an unfiltered page renders looking pre-filtered to "No stage". Filtering *to* the
  // blank bucket would need a real sentinel value round-tripped through the API; it is not worth
  // that until someone asks for it.
  // Every park's pens (the pen facet is not narrowed by park on the server), for two jobs: the
  // pen -> park map the filter bar uses to keep a new farm's pens across a farm change, and the
  // label of a selected pen the current farm's list no longer carries.
  const allPenOptions = buildShedFilterOptions(breakdown?.facets.sheds, "", parkLabelsById);
  const penParks: Record<string, string> = Object.fromEntries(
    allPenOptions.map((option) => [option.value, (option.key ?? "").split("|")[0] ?? ""] as const),
  );

  const filterFields: BreakdownFilterField[] = [
    {
      // The shared park parameter, so the choice carries to every other page and back.
      param: PARK_PARAM,
      label: copy(pageContract, "filter.farm_label"),
      values: parkId ? [parkId] : [],
      options: (breakdown?.facets.parks ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.label })),
    },
    {
      param: "bd_stage",
      label: copy(pageContract, "filter.stage_label"),
      multi: true,
      values: stages,
      // A stage picked under another farm stays in the list so it can be unticked.
      options: withSelectedOptions(
        (breakdown?.facets.stages ?? [])
          .filter((point) => point.key !== "")
          .map((point) => ({ value: point.key, label: stageLabels.get(point.key) ?? (point.label || point.key) })),
        stages,
        (value) => stageLabels.get(value),
      ),
    },
    {
      param: "bd_breed",
      label: copy(pageContract, "filter.breed_label"),
      multi: true,
      values: breeds,
      options: withSelectedOptions(
        (breakdown?.facets.breeds ?? [])
          .filter((point) => point.key !== "")
          .map((point) => ({ value: point.key, label: point.key })),
        breeds,
      ),
    },
    {
      param: "bd_shed",
      label: copy(pageContract, "filter.shed_label"),
      multi: true,
      // The COMPOSITE "<shed_id>|<partition_label>" is the option value, so the control must be
      // set to the composite too. Using the bare shedId meant no option matched when a partition
      // was chosen and the control silently claimed nothing was selected. The translation to the
      // API's `pen` values happens separately above.
      values: shedParams,
      // The park vocabulary is handed over so same-named sheds can be told apart. `park_label` on
      // the shed facet is a field nothing has ever filled — the Go struct and the OpenAPI schema
      // both lack it — so without this the disambiguation was dead code and the dropdown listed
      // "Castro" twice, "Mandela 1 - Part 3" twice, and so on. `facets.parks` is keyed by park id
      // and labelled with the park code, in the SAME response, so no extra read is involved.
      options: withSelectedOptions(
        buildShedFilterOptions(breakdown?.facets.sheds, selectedParkId, parkLabelsById),
        shedParams,
        (value) => allPenOptions.find((option) => option.value === value)?.label,
      ),
    },
    {
      param: "bd_sex",
      label: copy(pageContract, "filter.gender_label"),
      values: sex ? [sex] : [],
      // Gender comes from the backend contract's own vocabulary, NOT from charts.sex. The chart
      // series is computed over the FILTERED set, so sourcing the dropdown from it collapses the
      // options to whatever is already selected — pick female and male vanishes, leaving no way
      // back. Stage and breed avoid this by using `facets`, which the backend computes without
      // the dimension filters; sex is a closed CHECK (female|male) so its vocabulary is declared
      // in the page contract instead.
      options: optionGroup(pageContract, "counts_gender").map((option) => ({
        value: option.key,
        label: option.label,
      })),
    },
  ]
    // A dropdown whose only entry is "All" filters nothing — drop it rather than show dead
    // chrome. When the whole dimension is unrecorded, the data-gap note below says so explicitly.
    .filter((field) => field.options.length > 0);

  const pagination = paginationFromParams(sp, "bd", breakdown?.total_rows ?? 0, DEFAULT_PAGE_SIZE, pageSizeOptions);

  function hrefWithParam(key: string, value: string): string {
    const next = new URLSearchParams();
    for (const [paramKey, paramValue] of Object.entries(sp)) {
      if (paramKey === key) continue;
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

  // Stage crossed with sex (maintainer decision 2026-09-10). One bar per management stage, divided
  // female / male, which is one chart rather than two because the sexes are NOT spread evenly
  // across stages: Mother and Pregnant are female by definition and Buck is male by definition, so
  // a herd-wide sex ratio beside a stage total answers nothing about any single stage.
  //
  // This supersedes the note that stood here — "the gender split is already stated exactly by the
  // Gender column, and the stage chart would be a single bar in a herd with no recorded stage".
  // The Gender column states the split for ONE grain row, not for a stage; and the herd this reads
  // now carries a real stage vocabulary, so the single-bar case is the empty state rather than the
  // normal one.
  //
  // The backend owns every number here, including the segment values: `count` is the bar and is
  // read from the response rather than summed from the segments, so a stage whose parts failed to
  // reconcile shows a gap on screen instead of quietly redefining its own total.
  const sexSegments = [
    { key: "female", label: copy(pageContract, "label.sex_female"), colorVar: "var(--info)" },
    { key: "male", label: copy(pageContract, "label.sex_male"), colorVar: "var(--amber)" },
    // Third segment, drawn only where it carries animals. It is the honest home for a sex the
    // register does not hold as female or male — including unrecorded — so a bar still reports the
    // stage's true head count instead of shrinking to the two known buckets.
    { key: "other", label: copy(pageContract, "label.sex_other"), colorVar: "var(--muted)" },
  ] as const;

  const stageSexData: SvgStackedDatum[] = (breakdown?.charts.stage_sex ?? []).map((point) => ({
    key: point.key || noStageLabel,
    label: (point.key && stageNameByCode.get(point.key)) || stageLabel(point.label) || noStageLabel,
    total: point.count,
    segments: [
      { ...sexSegments[0], value: point.female },
      { ...sexSegments[1], value: point.male },
      { ...sexSegments[2], value: point.other },
    ],
  }));

  // Breed, then stage x sex, then pens. Each chart gets the full page width, so they render one per
  // row with a wide viewBox instead of side-by-side masonry columns.
  const charts: {
    id: string;
    title: string;
    caption: string;
    data?: SvgBarDatum[];
    stacked?: SvgStackedDatum[];
  }[] = [
    {
      id: "breed",
      title: copy(pageContract, "chart.breed.title"),
      caption: copy(pageContract, "chart.breed.caption"),
      data: toBarData(breakdown?.charts.breed ?? [], noBreedLabel),
    },
    {
      id: "stage_sex",
      title: copy(pageContract, "chart.stage_sex.title"),
      caption: copy(pageContract, "chart.stage_sex.caption"),
      stacked: stageSexData,
    },
    {
      id: "shed",
      title: copy(pageContract, "chart.shed.title"),
      caption: copy(pageContract, "chart.shed.caption"),
      data: toBarData(breakdown?.charts.shed ?? [], noShedLabel),
    },
  ];

  // Kids + adults always partition the total exactly, so the percentages are computed from the
  // response's own totals and never drift from the headline count. A zero total shows a dash
  // rather than a fabricated 0%.
  const totalCount = breakdown?.total_count ?? 0;
  const totalKids = breakdown?.total_kids ?? 0;
  const totalAdults = breakdown?.total_adults ?? 0;
  const totalFemale = (breakdown?.charts.stage_sex ?? []).reduce((sum, point) => sum + (point.female ?? 0), 0);
  const totalMale = (breakdown?.charts.stage_sex ?? []).reduce((sum, point) => sum + (point.male ?? 0), 0);
  const totalOther = (breakdown?.charts.stage_sex ?? []).reduce((sum, point) => sum + (point.other ?? 0), 0);

  // The kid-stage cards are titled with the tenant's stage NAME (K1 -> "Milk training"), read from
  // the same vocabulary the stage picker below uses; the contract's code label is the fallback.
  const kidStageLabel = (code: "K0" | "K1" | "K2" | "K3" | "K4") =>
    stageNameByCode.get(code) ?? copy(pageContract, `summary_card.${code.toLowerCase()}.label`);
  const stageSummaryCards = buildCountsSummaryCards(breakdown?.charts.stage_sex ?? [], {
    female: copy(pageContract, "label.sex_female"),
    male: copy(pageContract, "label.sex_male"),
    other: copy(pageContract, "label.sex_other"),
  }, {
    fattening: copy(pageContract, "summary_card.fattening.label"),
    bucks: copy(pageContract, "summary_card.bucks.label"),
    breeding: copy(pageContract, "summary_card.breeding.label"),
    icu: copy(pageContract, "summary_card.icu.label"),
    k0: kidStageLabel("K0"),
    k1: kidStageLabel("K1"),
    k2: kidStageLabel("K2"),
    k3: kidStageLabel("K3"),
    k4: kidStageLabel("K4"),
    other: copy(pageContract, "summary_card.other_stages.label"),
  });
  const summaryCards = [
    {
      key: "total-animals",
      label: copy(pageContract, "summary_card.total_animals.label"),
      count: totalCount,
      detail: countsSexDetail({
        female: totalFemale,
        male: totalMale,
        other: totalOther,
      }, {
        female: copy(pageContract, "label.female_short"),
        male: copy(pageContract, "label.male_short"),
        other: copy(pageContract, "label.sex_other_short"),
      }),
      tone: "brand" as const,
    },
    // Stage tiles with nothing in them (K4 = 0) are dropped from the deck: a zero tile is a slot
    // that says nothing, and the stage table below still lists every stage.
    ...stageSummaryCards.filter((card) => card.count > 0),
  ];

  // The tenant's active stage vocabulary, business-managed in Postgres. `name` is the human label
  // and `stage_code` is what the write sends.
  // Clinical tags (ICU, Quarantine) are dropped because the write rejects them: offering one and
  // failing on apply is worse than not offering it. The backend decides which those are.
  const stageOptions: StageOption[] = (stageResult.ok ? listOrEmpty(stageResult.data.items) : [])
    .filter((item: AnimalStageOptionItem) => item.assignable_as_cohort !== false)
    .map((item: AnimalStageOptionItem) => ({
      code: item.stage_code,
      // The VALUE is still the code, but the option text must match the backend-decided display
      // label used by the filter, chart, table cell and current retag chip.
      label: stageLabels.get(item.stage_code) ?? item.stage_code,
      // Case-insensitive: "Non-Pregnant" and "Non-pregnant" are the same word, and repeating it
      // under the tag is noise rather than help.
      description:
        (item.name ?? "").toLowerCase() === item.stage_code.toLowerCase() ? "" : (item.name ?? ""),
      band: item.age_band ?? "",
      assignable: true,
    }));

  // Authority is the backend's answer, read off the compiled control. A principal without
  // goat.reclassify_shed_stage gets a DISABLED button carrying the backend's reason, not a missing
  // one -- and the routes require the same permission, so the button is the honest label, not the
  // lock.
  // Vocabularies for the inline Breed and Gender corrections, both backend-owned. Breed is the
  // CATALOG (compiled from the tenant's breeds reference family), deliberately not the response's
  // `facets.breeds`: a facet reports the breeds already on the herd, and a correction frequently
  // needs one that is not -- that is the point of correcting a wrongly recorded breed.
  const breedChoices: InlineChoice[] = optionGroup(pageContract, "counts_breed").map((option) => ({
    value: option.key,
    label: option.label,
  }));
  const genderChoices: InlineChoice[] = optionGroup(pageContract, "counts_gender").map((option) => ({
    value: option.key,
    label: option.label,
  }));

  // Authority for the inline Stage editor, read off the compiled control -- the same control id and
  // permission the write itself is gated on.
  const stageChangeEnabled = controlEnabled(pageContract, "change_shed_stage", false);
  const stageChangeReason = control(pageContract, "change_shed_stage").disabled_reason ?? "";

  const breedChart = charts.find((chart) => chart.id === "breed")!;
  const stageChart = charts.find((chart) => chart.id === "stage_sex")!;
  const shedChart = charts.find((chart) => chart.id === "shed")!;
  // A stacked segment is drawn only where it carries animals (a herd with every sex recorded never
  // advertises a third key).
  const stackedSegments = sexSegments.filter(
    (segment) => segment.key !== "other" || stageSexData.some((point) => (point.segments.find((part) => part.key === "other")?.value ?? 0) > 0),
  );

  return (
    <Stack spacing={3} useFlexGap className="counts-breakdown-page" sx={{ minWidth: 0 }}>
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "section.breakdown.title") }]}
        />
      </div>

      {/* An API failure surfaces as a visible error band, never as an empty table that reads
          to an operator as "this tenant has no animals". */}
      {!breakdownResult.ok ? (
        <Alert severity="error" variant="outlined">
          <b>{breakdownResult.error.code ?? breakdownResult.error.kind}</b>&nbsp;{breakdownResult.error.message}
        </Alert>
      ) : null}

      {/* KPI row: template EcommerceWidgetSummary. Headline totals for the CURRENT filter
          selection, read from the response's whole-result window totals - never recomputed from
          the visible page, which would report a page subtotal as business truth. An unavailable
          read shows a dash. */}
      {/* KPI deck (guard: url-keyed-panel): a filter change swaps it to its skeleton at once; a page
          change of the pen table leaves it on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={BD_PAGER_PARAMS} fallback={<PanelSkeleton kpis={2 + summaryCards.length} />}>
      <Box component="section" aria-label={copy(pageContract, "kpi.matching.label")}>
        <KpiGrid>
          <EcommerceWidgetSummary
            title={copy(pageContract, "kpi.matching.label")}
            total={breakdown ? totalCount : dash(null)}
            caption={breakdown ? undefined : copy(pageContract, "kpi.matching.unavailable")}
            chart={NO_SPARK}
            sx={{ height: 1 }}
          />
          <EcommerceWidgetSummary
            title={copy(pageContract, "kpi.age.label")}
            total={breakdown ? totalKids + totalAdults : dash(null)}
            caption={
              breakdown
                ? splitParts(copy(pageContract, "kpi.age.label"), [totalKids, totalAdults])
                    .map((part) => `${Number(part.value).toLocaleString("en-IN")} ${part.label}`)
                    .join(" \u00b7 ")
                : copy(pageContract, "kpi.matching.unavailable")
            }
            chart={NO_SPARK}
            sx={{ height: 1 }}
          />
          {summaryCards.map((card) => (
            <EcommerceWidgetSummary
              key={card.key}
              title={card.label}
              total={breakdown ? card.count : dash(null)}
              caption={breakdown ? card.detail || copy(pageContract, "chart.empty") : copy(pageContract, "kpi.matching.unavailable")}
              chart={NO_SPARK}
              sx={{ height: 1 }}
            />
          ))}
        </KpiGrid>
      </Box>
      </UrlSuspense>

      <div>
      <Card className="counts-breakdown-card">
        <CountsBreakdownFilters fields={filterFields} penParks={penParks} pageContract={pageContract} />

        {/* The pen table + pager: every filter / page change shows its skeleton at once; the filter
            bar above stays mounted. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<TableSkeleton bare header={false} columns={cols.length || 6} rows={pagination.pageSize} />}>
        <div
          className="bd"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.breakdown.aria")}
        >
          {/* Headless table: TanStack owns the column model and the page-local sort; the markup
              stays the mock's plain table. Column keys, labels and which headers carry a sort
              affordance all come from the compiled contract, so this page declares no local
              column list. */}
          <CountsBreakdownPensTable
            contract={pensTable}
            pageContract={pageContract}
            pens={penRows}
            stages={stageOptions}
            breeds={breedChoices}
            genders={genderChoices}
            retagEnabled={stageChangeEnabled}
            retagDisabledReason={stageChangeReason}
            ariaLabel={copy(pageContract, "table.pens.aria")}
            noParkLabel={noParkLabel}
            noStageLabel={noStageLabel}
            noBreedLabel={noBreedLabel}
            noSexLabel={noSexLabel}
            noShedLabel={noShedLabel}
            stageLabels={stageLabels}
            empty={
              <EmptyContent
                filled
                sx={{ py: 8 }}
                title={
                  breakdownResult.ok
                    ? hasFilter
                      ? copy(pageContract, "empty.breakdown_filtered")
                      : copy(pageContract, "empty.breakdown")
                    : copy(pageContract, "state.breakdown_unavailable")
                }
              />
            }
            footer={
              breakdown ? (
                <tr>
                  <th colSpan={cols.length - 1}>{copy(pageContract, "table.pens.total_row")}</th>
                  {/* Read from the response: this is the sum across ALL matching rows, not the
                      page. Recomputing it from `rows` would silently report the page subtotal —
                      and reordering the page cannot touch it, because it is not derived from
                      the rows at all. */}
                  <th style={{ textAlign: "right" }}>{breakdown.total_count}</th>
                </tr>
              ) : undefined
            }
          />
        </div>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={pagination.page}
          pageSize={pagination.pageSize}
          total={pagination.total}
          start={pagination.start}
          end={pagination.end}
          noun={copy(pageContract, "table.pens.noun")}
          hrefForPage={(nextPage) => hrefWithParam("bd_page", String(nextPage))}
          hrefForPageSize={(nextSize) => hrefWithParam("bd_limit", String(nextSize))}
        />
        </UrlSuspense>
      </Card>
      </div>

      {/* A dimension where every animal has a blank value is a source-data gap, not a bug. Say so
          plainly instead of leaving the operator staring at a uniformly-empty column and chart
          and concluding the screen is broken. Never fabricate values to fill it. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={BD_PAGER_PARAMS} fallback={<PanelSkeleton charts={2} />}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      {stageUnrecorded ? (
        <Alert severity="info" variant="outlined">{copy(pageContract, "state.stage_unrecorded")}</Alert>
      ) : null}

      {/* Charts on template cards: breed share → EcommerceSalesOverview, stage × sex → stacked
          AnalyticsWebsiteVisits, sheds → AnalyticsConversionRates (horizontal, paged ten bars at a time).
          No truncation: the series PARTITION the herd, so each chart sums to the KPI above it. */}
      <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.charts.aria")}>
        <Grid size={{ xs: 12, md: 5 }}>
          <EcommerceSalesOverview
            title={breedChart.title}
            aria-label={breedChart.title}
            data={shareRows(breedChart.data ?? [])}
            sx={{ height: 1 }}
          >
            {(breedChart.data ?? []).length === 0 ? <EmptyContent title={emptyChartLabel} sx={{ py: 3 }} /> : null}
          </EcommerceSalesOverview>
        </Grid>
        <Grid size={{ xs: 12, md: 7 }}>
          <AnalyticsWebsiteVisits
            title={stageChart.title}
            aria-label={stageChart.title}
            valueNoun={animalsNoun}
            empty={<EmptyContent title={emptyChartLabel} />}
            chart={{
              categories: stageSexData.map((point) => point.label),
              colors: stackedSegments.map((segment) => segment.colorVar),
              series: stackedSegments.map((segment) => ({
                name: segment.label,
                data: stageSexData.map((point) => point.segments.find((part) => part.key === segment.key)?.value ?? 0),
              })),
              options: { chart: { stacked: true }, plotOptions: { bar: { columnWidth: "40%" } } },
            }}
            sx={{ height: 1 }}
          />
        </Grid>
        <Grid size={12}>
          <CountsShedChart title={shedChart.title} bars={shedChart.data ?? []} unit={animalsNoun} emptyLabel={emptyChartLabel} />
        </Grid>
      </Grid>
      </Stack>
      </UrlSuspense>
    </Stack>
  );
}

/** The pen table's pager params: the KPI deck and the charts do not change with them. */
const BD_PAGER_PARAMS = ["bd_page", "bd_limit"] as const;
