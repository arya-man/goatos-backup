import { redirect } from "next/navigation";
import { Filter, Users } from "lucide-react";

import { SvgBars, type SvgBarDatum } from "@/components/svg-bars";
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
import { CountsBreakdownPensTable } from "./counts-breakdown-pens-table";
import { buildShedFilterOptions } from "./counts-breakdown-sheds";
import type { StageOption } from "./shed-stage-actions";
import type { InlineChoice } from "./inline-cell-editor";

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
    label: point.label || fallbackLabel,
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

  // The top bar owns park scope; page-body filters own the rest.
  const scope = parseScope(sp);
  const { parkId } = backendScope(scope);

  // Stage, breed and shed are MULTI-VALUED (repeated URL params, OR within the dimension);
  // farm and gender stay single-valued (maintainer instruction, 2026-09-03).
  const all = (key: string): string[] => {
    const value = sp[key];
    return (Array.isArray(value) ? value : value ? [value] : []).filter(Boolean);
  };
  const farmParkId = one(sp, "bd_farm");
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
      park_id: parkId || farmParkId,
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
  const hasFilter = Boolean(farmParkId || shedParams.length || stages.length || breeds.length || sex);

  const noParkLabel = copy(pageContract, "label.unassigned_farm");
  const noShedLabel = copy(pageContract, "label.unassigned_shed");
  const noStageLabel = copy(pageContract, "label.unassigned_stage");
  const noBreedLabel = copy(pageContract, "label.unassigned_breed");
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
  const stageUnrecorded = stageFacets.length > 0 && stageFacets.every((point) => point.key === "");

  // The shed dropdown cascades to the currently selected park: the top-bar park scope wins,
  // otherwise the in-body Farm filter. When a park is selected only that park's sheds show,
  // mirroring the park facet (and the Android CountsViewModel, which narrows sheds by parkId).
  const selectedParkId = parkId || farmParkId || "";

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
  const filterFields: BreakdownFilterField[] = [
    {
      param: "bd_farm",
      label: copy(pageContract, "filter.farm_label"),
      values: farmParkId ? [farmParkId] : [],
      // Disabled (not hidden) when the top bar already scopes a park: the mock's rule is
      // disable-with-reason, and hiding it would make the control appear to come and go.
      disabledReason: parkId ? copy(pageContract, "filter.scope_readonly") : undefined,
      options: (breakdown?.facets.parks ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.label })),
    },
    {
      param: "bd_stage",
      label: copy(pageContract, "filter.stage_label"),
      multi: true,
      values: stages,
      options: (breakdown?.facets.stages ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.key })),
    },
    {
      param: "bd_breed",
      label: copy(pageContract, "filter.breed_label"),
      multi: true,
      values: breeds,
      options: (breakdown?.facets.breeds ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.key })),
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
      options: buildShedFilterOptions(breakdown?.facets.sheds, selectedParkId, parkLabelsById),
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

  // Breed and shed only. The gender split is already stated exactly by the Gender column and the
  // filter, and the stage chart would be a single bar in a herd with no recorded stage — both were
  // dropped rather than kept as decoration. Each remaining chart gets the full page width, so they
  // render one per row with a wide viewBox instead of side-by-side masonry columns.
  const charts = [
    {
      id: "breed",
      title: copy(pageContract, "chart.breed.title"),
      caption: copy(pageContract, "chart.breed.caption"),
      data: toBarData(breakdown?.charts.breed ?? [], noBreedLabel),
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
  const pct = (part: number) => (totalCount > 0 ? Math.round((part / totalCount) * 100) : 0);

  // The tenant's active stage vocabulary, business-managed in Postgres. `name` is the human label
  // and `stage_code` is what the write sends.
  // Clinical tags (ICU, Quarantine) are dropped because the write rejects them: offering one and
  // failing on apply is worse than not offering it. The backend decides which those are.
  const stageOptions: StageOption[] = (stageResult.ok ? stageResult.data.items : [])
    .filter((item: AnimalStageOptionItem) => item.assignable_as_cohort !== false)
    .map((item: AnimalStageOptionItem) => ({
      code: item.stage_code,
      // The CODE is the tag: it is what the table cell shows, what the farm's own sheet uses, and
      // what the write stores. The lookup's descriptive name rides along as context.
      label: item.stage_code,
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

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.breakdown.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* An API failure surfaces as a visible error band, never as an empty table that reads
          to an operator as "this tenant has no animals". */}
      {!breakdownResult.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{breakdownResult.error.code ?? breakdownResult.error.kind}</b>&nbsp;{breakdownResult.error.message}
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.breakdown.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.breakdown.caption")}</span>
          {/* Double-click is invisible as an affordance, so the page says it out loud -- and only
              to a principal who may actually use it, since the backend decides that. */}
          {stageChangeEnabled ? (
            <span className="small muted" style={{ marginLeft: "auto" }}>
              {copy(pageContract, "action.retag.hint")}
            </span>
          ) : null}
        </div>
        <CountsBreakdownFilters fields={filterFields} pageContract={pageContract} />

        {/* Headline totals for the CURRENT filter selection, read from the response's
            whole-result window totals — never recomputed from the visible page, which would
            report a page subtotal as business truth. An unavailable read shows a dash. */}
        <div className="grid g2 counts-breakdown-kpis">
          <div className="kpi">
            <span className="acc" style={{ background: "var(--brand)" }} />
            <div className="lab">{copy(pageContract, "kpi.matching.label")}</div>
            <div className="val">{breakdown ? totalCount : dash(null)}</div>
            <div className="dl">
              <span className="muted">
                {breakdown ? copy(pageContract, "kpi.matching.sub") : copy(pageContract, "kpi.matching.unavailable")}
              </span>
            </div>
            <Filter className="ic kpiic" aria-hidden="true" />
          </div>
          <div className="kpi" aria-label={copy(pageContract, "kpi.age.aria")}>
            <span className="acc" style={{ background: "var(--teal)" }} />
            <div className="lab">{copy(pageContract, "kpi.age.label")}</div>
            <div className="val">{breakdown ? `${totalKids} · ${totalAdults}` : dash(null)}</div>
            <div className="dl">
              <span className="muted">
                {breakdown
                  ? `${pct(totalKids)}% ${copy(pageContract, "label.kids")} · ${pct(totalAdults)}% ${copy(pageContract, "label.adults")}`
                  : copy(pageContract, "kpi.matching.unavailable")}
              </span>
            </div>
            <Users className="ic kpiic" aria-hidden="true" />
          </div>
        </div>

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
            noShedLabel={noShedLabel}
            empty={
              <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                {breakdownResult.ok
                  ? hasFilter
                    ? copy(pageContract, "empty.breakdown_filtered")
                    : copy(pageContract, "empty.breakdown")
                  : copy(pageContract, "state.breakdown_unavailable")}
              </div>
            }
            footer={
              breakdown ? (
                <tr>
                  <th colSpan={cols.length - 1}>{copy(pageContract, "table.pens.total_row")}</th>
                  {/* Read from the response: this is the sum across ALL matching rows, not the
                      page. Recomputing it from `rows` would silently report the page subtotal —
                      and reordering the page cannot touch it, because it is not derived from
                      the rows at all. */}
                  <th style={{ textAlign: "right", color: "var(--brand-d)" }}>{breakdown.total_count}</th>
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
      </section>

      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.breakdown.note")}</div>

      {/* A dimension where every animal has a blank value is a source-data gap, not a bug. Say so
          plainly instead of leaving the operator staring at a uniformly-empty column and chart
          and concluding the screen is broken. Never fabricate values to fill it. */}
      {stageUnrecorded ? (
        <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "state.stage_unrecorded")}</div>
      ) : null}

      {/* Full-width charts: one per row. Deliberately NOT the mock's `.charts` masonry wrapper,
          which is a 340px multi-column layout — that would put these back side by side. */}
      <section aria-label={copy(pageContract, "section.charts.aria")} className="counts-breakdown-charts">
        {charts.map((chart) => (
          <div className="chartcard" key={chart.id} style={{ cursor: "default" }}>
            <h4>{chart.title}</h4>
            <div className="cap">{chart.caption}</div>
            {/* No maxBars: the series must PARTITION the herd, so the chart sums to the same total
                the KPI above it reports. Truncating here would reintroduce the gap the backend cap
                just lost (12 of 130 pens showed 560 of 1,670 animals). The scroll window bounds
                what a reader SEES — ten bars stand, the rest scroll — which is a different job from
                bounding what the number MEANS. */}
            <SvgBars
              data={chart.data}
              emptyLabel={emptyChartLabel}
              valueNoun={animalsNoun}
              chartLabel={chart.title}
              maxBars={chart.data.length}
            />
          </div>
        ))}
      </section>
    </div>
  );
}
