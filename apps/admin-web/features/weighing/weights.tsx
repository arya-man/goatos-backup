import { redirect } from "next/navigation";
import { Scale, Warehouse } from "lucide-react";

import { SvgBars } from "@/components/svg-bars";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getShedWeights, type ShedWeightsRow } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";

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

function kg(value: number, fractionDigits = 1): string {
  return value.toLocaleString("en-IN", {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  });
}

// A whole-shed weigh cannot say how many of its animals cleared a weight threshold, and can
// never produce a per-animal figure. The mode therefore has to be visible on every row, or a
// reader reasonably assumes both kinds of row answer the same questions.
function modeTag(row: ShedWeightsRow, pageContract: AdminUiPageContract) {
  return row.weighing_category === "individual_animal"
    ? { tone: "info" as const, label: copy(pageContract, "value.weighing.individual") }
    : { tone: "mut" as const, label: copy(pageContract, "value.weighing.lump") };
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

  const result = await getShedWeights({ park_id: parkFilter || undefined });

  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  if (!result.ok) {
    return (
      <section className="card">
        <h2 className="h">{copy(pageContract, "error.load.title")}</h2>
        <p className="muted small">{copy(pageContract, "error.load.body")}</p>
      </section>
    );
  }

  const { rows, summary, parks, period_start: periodStart, period_end: periodEnd } = result.data;

  // Mode narrowing happens here because the response already carries every shed in scope. It
  // changes the TABLE and the chart only — the KPI cards keep reporting the backend's
  // whole-filter truth, which is what they are for. Re-deriving a card from the visible slice
  // is the capped read-time rollup anti-pattern and would make the two disagree.
  const visibleRows =
    modeFilter === "all" ? rows : rows.filter((row) => row.weighing_category === modeFilter);
  const slice = visibleRows.slice(offset, offset + limit);

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
      kind: "select",
      param: "weighing",
      label: copy(pageContract, "filter.weighing.label"),
      value: modeFilter,
      options: modeOptions.map((option) => ({ value: option.key, label: option.label })),
    },
  ];

  const columns = tableLabels(pageContract, "shed-weights");

  // Chart reads the FILTERED rows so it and the table below always describe the same set.
  const chartData = visibleRows
    .filter((row) => row.animals_weighed > 0)
    .slice()
    .sort((a, b) => b.average_weight_kg - a.average_weight_kg)
    .map((row) => ({
      key: row.location_id,
      label: row.shed_display_name,
      value: Number(row.average_weight_kg.toFixed(1)),
    }));

  const hasAnyData = summary.animals_weighed > 0;

  return (
    <>
      <WorklistFilters
        basePath={PAGE_PATH}
        pageParam="offset"
        fields={filterFields}
        pageContract={pageContract}
      />

      <section className="kpis" aria-label={copy(pageContract, "section.sheds.aria")}>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.kids.label")}</div>
          <div className="kv">{summary.animals_weighed.toLocaleString("en-IN")}</div>
          <div className="muted small">{copy(pageContract, "kpi.kids.sub")}</div>
        </div>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.total.label")}</div>
          <div className="kv">{kg(summary.total_weight_kg, 0)} kg</div>
          <div className="muted small">{copy(pageContract, "note.total_weight")}</div>
        </div>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.average.label")}</div>
          {/* A null average means nothing was weighed. Rendering 0.0 kg would read as a herd
              that weighs nothing — a different, untrue statement. */}
          <div className="kv">
            {summary.average_weight_kg == null
              ? copy(pageContract, "empty.no_data.title")
              : `${kg(summary.average_weight_kg)} kg`}
          </div>
          <div className="muted small">{copy(pageContract, "kpi.average.sub")}</div>
        </div>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.over30.label")}</div>
          <div className="kv">{summary.at_or_above_30kg.toLocaleString("en-IN")}</div>
          {/* The threshold counts carry their OWN denominator: a whole-shed weigh contributes
              nothing to them, so showing them against animals_weighed would understate them. */}
          <div className="muted small">
            {summary.threshold_basis_animals.toLocaleString("en-IN")}{" "}
            {copy(pageContract, "kpi.threshold.basis")}
          </div>
        </div>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.over35.label")}</div>
          <div className="kv">{summary.at_or_above_35kg.toLocaleString("en-IN")}</div>
          <div className="muted small">
            {summary.threshold_basis_animals.toLocaleString("en-IN")}{" "}
            {copy(pageContract, "kpi.threshold.basis")}
          </div>
        </div>
        <div className="card kpi">
          <div className="kl">{copy(pageContract, "kpi.sheds.label")}</div>
          <div className="kv">
            {summary.sheds_weighed} / {summary.sheds_in_scope}
          </div>
          <div className="muted small">
            {periodStart} – {periodEnd}
          </div>
        </div>
      </section>

      <section className="card" aria-label={copy(pageContract, "chart.average.aria")}>
        <h2 className="h">
          <Scale size={15} aria-hidden /> {copy(pageContract, "chart.average.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "chart.average.caption")}</p>
        <SvgBars
          data={chartData}
          emptyLabel={copy(pageContract, "empty.no_data.body")}
          valueNoun="kg"
          chartLabel={copy(pageContract, "chart.average.aria")}
          maxBars={12}
        />
      </section>

      <section className="card" aria-label={copy(pageContract, "section.sheds.aria")}>
        <h2 className="h">
          <Warehouse size={15} aria-hidden /> {copy(pageContract, "section.sheds.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "note.no_cadence")}</p>

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
            <div className="tablewrap">
              <table className="tbl">
                <thead>
                  <tr>
                    {columns.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slice.map((row) => {
                    const mode = modeTag(row, pageContract);
                    return (
                      <tr key={row.location_id}>
                        <td>{row.park_name}</td>
                        <td>
                          <b>{row.shed_display_name}</b>
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
    </>
  );
}
