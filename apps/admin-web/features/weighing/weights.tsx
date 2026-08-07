import { redirect } from "next/navigation";
import { Scale, TrendingDown, TrendingUp, Warehouse } from "lucide-react";

import { WeightBars } from "./weight-bars";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getShedWeights,
  getWeighingGrowth,
  getWeightDemographics,
  type ShedWeightsRow,
} from "@/lib/api/server";
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

// Inclusive Asia/Kolkata business dates, which is what the API's from/to expect.
function businessDayWindow(days: number): { from: string; to: string } {
  const now = new Date();
  const istToday = new Date(now.getTime() + (5.5 * 60 - now.getTimezoneOffset()) * 60_000);
  const to = istToday.toISOString().slice(0, 10);
  const fromDate = new Date(istToday.getTime() - (days - 1) * 86_400_000);
  return { from: fromDate.toISOString().slice(0, 10), to };
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

  // Period is a business-day window, not a clock offset: a weigh belongs to the
  // Asia/Kolkata day it happened on.
  const periodDays = one(params, "period") === "84" ? 84 : 28;
  const window = businessDayWindow(periodDays);

  const [weights, growth, demographics] = await Promise.all([
    getShedWeights({ park_id: parkFilter || undefined, ...window }),
    getWeighingGrowth({ park_id: parkFilter || undefined, ...window }),
    getWeightDemographics({ park_id: parkFilter || undefined, ...window }),
  ]);

  if (firstAuthRequiredError(weights, growth, demographics)) redirect(INTERNAL_LOGIN_PATH);

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
  const visibleRows =
    modeFilter === "all" ? rows : rows.filter((row) => row.weighing_category === modeFilter);
  const slice = visibleRows.slice(offset, offset + limit);

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
            return {
              name: park.name,
              median: result.ok ? result.data.headline.median_adg_g_per_day : null,
            };
          }),
        )
      : [];

  const losingAll = growth.ok ? growth.data.losing_animals : [];
  const losingSlice = losingAll.slice(losingOffset, losingOffset + DEFAULT_LIMIT);

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
      param: "period",
      label: copy(pageContract, "filter.period.label"),
      value: String(periodDays),
      options: optionGroup(pageContract, "weighing_period").map((option) => ({
        value: option.key,
        label: option.label,
      })),
    },
    {
      kind: "select",
      param: "weighing",
      label: copy(pageContract, "filter.weighing.label"),
      value: modeFilter,
      options: modeOptions.map((option) => ({ value: option.key, label: option.label })),
    },
  ];

  const shedColumns = tableLabels(pageContract, "shed-weights");
  const losingColumns = tableLabels(pageContract, "losing-kids");

  const chartData = visibleRows
    .filter((row) => row.animals_weighed > 0)
    .slice()
    .sort((a, b) => b.average_weight_kg - a.average_weight_kg)
    .map((row) => ({
      key: row.location_id,
      label: row.shed_display_name,
      value: Number(row.average_weight_kg.toFixed(1)),
    }));

  const demo = demographics.ok ? demographics.data : null;
  const headlineGain = growth.ok ? growth.data.headline.median_adg_g_per_day : null;

  // Daily gain per shed comes from the growth read's own shed leaderboard, which is already
  // restricted to per-animal sheds — a whole-shed total can never produce a per-kid gain.
  const gainChartData = (growth.ok ? growth.data.shed_leaderboard : [])
    .filter((shed) => shed.adg_pair_count > 0)
    .slice()
    .sort((a, b) => b.median_adg_g_per_day - a.median_adg_g_per_day)
    .map((shed) => ({
      key: shed.location_id,
      label: shed.display_name,
      value: Math.round(shed.median_adg_g_per_day),
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

      <p className="muted small" style={{ margin: "0 0 -4px" }}>
        {copy(pageContract, "kpi.sheds.label")}: {summary.sheds_weighed} / {summary.sheds_in_scope}
        {" · "}
        {periodStart} – {periodEnd}
      </p>

      <section className="grid g6" aria-label={copy(pageContract, "section.sheds.aria")}>
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
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.gain.label")}</div>
          {/* insufficient_data is a real state: a park where nothing was weighed twice has NO
              gain, and printing 0 g/day would read as a herd that stopped growing. */}
          <div className="val">
            {headlineGain == null ? copy(pageContract, "empty.no_data.title") : `${Math.round(headlineGain)} g`}
          </div>
          <div className="dl">
            {headlineGain == null
              ? copy(pageContract, "kpi.gain.none")
              : copy(pageContract, "kpi.gain.sub")}
          </div>
        </div>
      </section>

      {perParkGain.length > 0 ? (
        <section className="grid g3" aria-label={copy(pageContract, "section.park_gain.aria")}>
          <div className="kpi">
            <div className="lab">
              {copy(pageContract, "kpi.park_gain.all")} {copy(pageContract, "kpi.park_gain.suffix")}
            </div>
            <div className="val">
              {headlineGain == null
                ? copy(pageContract, "empty.no_data.title")
                : `${Math.round(headlineGain)} g`}
            </div>
            <div className="dl">{copy(pageContract, "kpi.gain.sub")}</div>
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
                  : copy(pageContract, "kpi.gain.sub")}
              </div>
            </div>
          ))}
        </section>
      ) : null}

      {/* Row 1 — shed and breed side by side, equal width, fixed height with the
          list scrolling inside so neither card grows with its row count. */}
      <div className="grid g2">
        <section className="card" aria-label={copy(pageContract, "chart.average.aria")}>
          <h2 className="h">
            <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "chart.average.title")}
          </h2>
          <p className="muted small">{copy(pageContract, "chart.average.caption")}</p>
          <WeightBars
            data={chartData}
            emptyLabel={copy(pageContract, "empty.no_data.body")}
            unit="kg"
            chartLabel={copy(pageContract, "chart.average.aria")}
            size="tall"
          />
        </section>
        <section className="card" aria-label={copy(pageContract, "chart.breed.aria")}>
          <h2 className="h">{copy(pageContract, "chart.breed.title")}</h2>
          <p className="muted small">{copy(pageContract, "section.demographics.caption")}</p>
          <WeightBars
            data={(demo?.by_breed ?? []).map((bucket) => ({
              key: bucket.label,
              label: bucket.label,
              value: Number(bucket.average_weight_kg.toFixed(1)),
            }))}
            emptyLabel={copy(pageContract, "empty.demographics.body")}
            unit="kg"
            chartLabel={copy(pageContract, "chart.breed.aria")}
            size="tall"
          />
        </section>
      </div>

      {/* Row 2 — sex and stage. Few rows each, so a shorter box. */}
      <div className="grid g2">
        <section className="card" aria-label={copy(pageContract, "chart.sex.aria")}>
          <h2 className="h">{copy(pageContract, "chart.sex.title")}</h2>
          <WeightBars
            data={(demo?.by_sex ?? []).map((bucket) => ({
              key: bucket.label,
              label: bucket.label,
              value: Number(bucket.average_weight_kg.toFixed(1)),
            }))}
            emptyLabel={copy(pageContract, "empty.demographics.body")}
            unit="kg"
            chartLabel={copy(pageContract, "chart.sex.aria")}
            size="short"
          />
        </section>
        <section className="card" aria-label={copy(pageContract, "chart.stage.aria")}>
          <h2 className="h">{copy(pageContract, "chart.stage.title")}</h2>
          <WeightBars
            data={(demo?.by_stage ?? []).map((bucket) => ({
              key: bucket.label,
              label: bucket.label,
              value: Number(bucket.average_weight_kg.toFixed(1)),
            }))}
            emptyLabel={copy(pageContract, "empty.demographics.body")}
            unit="kg"
            chartLabel={copy(pageContract, "chart.stage.aria")}
            size="short"
          />
        </section>
      </div>

      {demo ? (
        <p className="muted small">
          {copy(pageContract, "note.demographics.coverage")}
          {demo.unresolved_animals > 0
            ? ` ${demo.unresolved_animals} weighed kid(s) are not in the herd register.`
            : ""}
        </p>
      ) : null}

      <section className="card" aria-label={copy(pageContract, "chart.gain.aria")}>
        <h2 className="h">
          <TrendingUp className="ic" size={15} aria-hidden /> {copy(pageContract, "chart.gain.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "chart.gain.caption")}</p>
        <WeightBars
          data={gainChartData}
          emptyLabel={
            gainChartData.length > 0 && gainChartData.every((shed) => shed.value <= 0)
              ? copy(pageContract, "empty.gain.all_losing")
              : copy(pageContract, "empty.gain.body")
          }
          unit="g"
          chartLabel={copy(pageContract, "chart.gain.aria")}
          size="short"
        />
      </section>

      <section className="card" aria-label={copy(pageContract, "section.sheds.aria")}>
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
            <div className="tablewrap">
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

      <section className="card" aria-label={copy(pageContract, "section.losing.aria")}>
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
            <div className="tablewrap">
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
    </>
  );
}
