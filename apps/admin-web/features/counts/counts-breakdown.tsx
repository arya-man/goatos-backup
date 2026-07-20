import { redirect } from "next/navigation";
import { Filter, Users } from "lucide-react";

import { SvgBars, type SvgBarDatum } from "@/components/svg-bars";
import { dash } from "@/lib/format";
import { copy, optionGroup, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getCountsBreakdown,
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
import { buildShedFilterOptions } from "./counts-breakdown-sheds";

// Counts -> Counts Breakdown. The census view: how many live animals exist at each
// farm x stage x breed x gender x shed combination, plus the same numbers as distributions.
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

  const farmParkId = one(sp, "bd_farm");
  const shedId = one(sp, "bd_shed");
  const stage = one(sp, "bd_stage");
  const breed = one(sp, "bd_breed");
  const sex = one(sp, "bd_sex");

  const pageSizeOptions = tablePageSizes(pageContract, "detail-breakdown");
  const requestedLimit = Number(one(sp, "bd_limit"));
  const pageSize: VaccinationPageSize = pageSizeOptions.includes(requestedLimit)
    ? requestedLimit
    : DEFAULT_PAGE_SIZE;
  const requestedPage = Math.max(1, Number(one(sp, "bd_page")) || 1);

  // One round trip for the whole screen: rows, totals, all four chart series and the filter
  // facets (including the park-scoped shed vocabulary) come back together, so there is no
  // per-chart fan-out and no serial await.
  const breakdownResult = await getCountsBreakdown({
    park_id: parkId || farmParkId,
    shed_id: shedId,
    management_stage: stage,
    breed,
    sex,
    limit: pageSize,
    offset: (requestedPage - 1) * pageSize,
  });

  const authError = firstAuthRequiredError(breakdownResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const breakdown: CountsBreakdownResponse | null = breakdownResult.ok ? breakdownResult.data : null;
  const rows = breakdown?.items ?? [];
  const hasFilter = Boolean(farmParkId || shedId || stage || breed || sex);

  const noParkLabel = copy(pageContract, "label.unassigned_farm");
  const noShedLabel = copy(pageContract, "label.unassigned_shed");
  const noStageLabel = copy(pageContract, "label.unassigned_stage");
  const noBreedLabel = copy(pageContract, "label.unassigned_breed");
  const emptyChartLabel = copy(pageContract, "chart.empty");
  const animalsNoun = copy(pageContract, "label.animals_noun");

  const cols = tableLabels(pageContract, "detail-breakdown");

  // True when the herd carries no recorded stage at all (every animal blank), which makes both
  // the Stage column and the stage chart uniformly empty.
  const stageFacets = breakdown?.facets.stages ?? [];
  const stageUnrecorded = stageFacets.length > 0 && stageFacets.every((point) => point.key === "");

  // The shed dropdown cascades to the currently selected park: the top-bar park scope wins,
  // otherwise the in-body Farm filter. When a park is selected only that park's sheds show,
  // mirroring the park facet (and the Android CountsViewModel, which narrows sheds by parkId).
  const selectedParkId = parkId || farmParkId || "";

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
      value: farmParkId ?? "",
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
      value: stage ?? "",
      options: (breakdown?.facets.stages ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.key })),
    },
    {
      param: "bd_breed",
      label: copy(pageContract, "filter.breed_label"),
      value: breed ?? "",
      options: (breakdown?.facets.breeds ?? [])
        .filter((point) => point.key !== "")
        .map((point) => ({ value: point.key, label: point.key })),
    },
    {
      param: "bd_shed",
      label: copy(pageContract, "filter.shed_label"),
      value: shedId ?? "",
      options: buildShedFilterOptions(breakdown?.facets.sheds, selectedParkId),
    },
    {
      param: "bd_sex",
      label: copy(pageContract, "filter.gender_label"),
      value: sex ?? "",
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
          <table className="counts-breakdown-table" aria-label={copy(pageContract, "table.breakdown.aria")}>
            <thead>
              <tr>
                {cols.map((col, index) => (
                  <th key={col} style={index === cols.length - 1 ? { textAlign: "right" } : undefined}>
                    {col}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {breakdownResult.ok
                        ? hasFilter
                          ? copy(pageContract, "empty.breakdown_filtered")
                          : copy(pageContract, "empty.breakdown")
                        : copy(pageContract, "state.breakdown_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((row) => (
                  <tr
                    key={`${row.park_id ?? ""}|${row.shed_id ?? ""}|${row.management_stage}|${row.breed}|${row.sex}`}
                  >
                    <td className="muted">{row.park_label || noParkLabel}</td>
                    <td>{row.management_stage || noStageLabel}</td>
                    <td>{row.breed || noBreedLabel}</td>
                    <td>{row.sex}</td>
                    <td className="muted">{row.shed_label || noShedLabel}</td>
                    <td style={{ textAlign: "right", fontWeight: 700, color: "var(--brand-d)" }}>{row.count}</td>
                  </tr>
                ))
              )}
            </tbody>
            {breakdown ? (
              <tfoot>
                <tr>
                  <th colSpan={cols.length - 1}>{copy(pageContract, "table.breakdown.total_row")}</th>
                  {/* Read from the response: this is the sum across ALL matching rows, not the
                      page. Recomputing it from `rows` would silently report the page subtotal. */}
                  <th style={{ textAlign: "right", color: "var(--brand-d)" }}>{breakdown.total_count}</th>
                </tr>
              </tfoot>
            ) : null}
          </table>
        </div>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={pagination.page}
          pageSize={pagination.pageSize}
          total={pagination.total}
          start={pagination.start}
          end={pagination.end}
          noun={copy(pageContract, "table.breakdown.noun")}
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
            <SvgBars
              data={chart.data}
              emptyLabel={emptyChartLabel}
              valueNoun={animalsNoun}
              chartLabel={chart.title}
              maxBars={12}
            />
          </div>
        ))}
      </section>
    </div>
  );
}
