import { redirect } from "next/navigation";
import { CalendarRange, Scale, Sprout, Warehouse } from "lucide-react";

import { GroupedBars, type BarGroup, type GroupedBar } from "./grouped-bars";
import { WeightBars } from "./weight-bars";
import { WeightsExportControl, type WeightsExportShed } from "./weights-export";
import { SegmentedLinks } from "@/components/segmented-links";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { todayIso } from "@/lib/format";
import {
  firstAuthRequiredError,
  getShedWeights,
  getWeighingGrowth,
  getWeightDemographics,
  type ShedWeightsResponse,
  type ShedWeightsRow,
  type ShedWeightsSummary,
  type WeighingGrowthResponse,
  type WeightDemographicsResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
// The landing window is SHARED with /weighing/weights: both screens open on the latest whole-shed
// weigh against the one before it, so a reader moving between them is looking at one period rather
// than two that merely look alike. See landing-window.ts.
import {
  WINDOW_FROM_PARAM,
  WINDOW_TO_PARAM,
  defaultWindow,
  landingWindow,
} from "./landing-window";
import { WeightsAnalyticsTabLoading } from "./weights-analytics-tab-loading";

const PAGE_PATH = "/weighing/analytics";
const SEX_PARAM = "sex";
const ORIGIN_PARAM = "origin";
const TAB_PARAM = "tab";
const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;
const DEFAULT_LIMIT = 25;

const TABS = ["general", "breed", "birth", "shed", "weight", "time"] as const;
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

function workflowLabel(status: string, pageContract: AdminUiPageContract): string {
  const key = `value.workflow.${status}`;
  const resolved = copy(pageContract, key);
  return resolved === key ? status : resolved;
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
  const rawSex = one(params, SEX_PARAM);
  const sexFilter = rawSex === "female" ? "female" : rawSex === "all" ? "" : "male";
  // What the CONTROL shows. The reads take "" for every kid; the control cannot, or its All
  // option would be the blank one and would read back as the male default on the next request.
  const sexChoice = sexFilter === "" ? "all" : sexFilter;
  const rawOrigin = one(params, ORIGIN_PARAM);
  const originFilter = rawOrigin === "farm_born" || rawOrigin === "purchased" ? rawOrigin : "";

  const limit = boundedLimit(one(params, "limit"));
  const offset = boundedOffset(one(params, "offset"));

  const today = todayIso();
  const weighingCategoryFilter = modeFilter !== "all" ? modeFilter : "";
  const window = await landingWindow(
    params,
    today,
    parkFilter,
    sexFilter,
    originFilter,
    weighingCategoryFilter,
  );
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
  const wantsGrowth = tab === "general" || tab === "shed" || tab === "time";
  const wantsDemographics =
    tab === "breed" || tab === "shed" || tab === "birth" || tab === "weight" || tab === "time";

  // ONE demographics read serves all three tabs that need it, Birth-wise included: the backend
  // carries `gain_by_breed_origin` on this same response. Asking the read twice under the two
  // origins would recompute by_sex, by_stage, the bands and the shed composition only to discard
  // both copies -- and would let the two halves be resolved a request apart.
  const [weights, growth, demographics] = await Promise.all([
    getShedWeights({ ...scope, ...readWindow }),
    wantsGrowth ? getWeighingGrowth({ ...scope, ...readWindow }) : null,
    wantsDemographics ? getWeightDemographics({ ...scope, ...readWindow }) : null,
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
  const demo = demographics?.ok ? demographics.data : null;

  // Per-park gain cards beside the all-parks one, so CBE and CPT can be read against each other
  // and against the herd. One call per PARK -- a handful of rows (two today), never a paginated
  // entity list: the banned shape is draining a cursor, not asking a bounded vocabulary. Skipped
  // entirely when the reader has already narrowed to one park, because the card above is then
  // that park's, and skipped off the General tab because nothing else on the page renders them.
  //
  // Each card carries the SAME filters as the rest of the page. They were the one read on the
  // Weights page that once did not, and it showed a filtered headline above two unfiltered park
  // cards -- three numbers about three different populations, side by side, with nothing saying so.
  const perParkGain =
    tab === "general" && parkFilter === "" && parks.length > 1
      ? await Promise.all(
          parks.map(async (park) => {
            const result = await getWeighingGrowth({ ...scope, ...readWindow, park_id: park.park_id });
            const headline = result.ok ? result.data.headline : null;
            return {
              name: park.name,
              gain: headline?.average_adg_g_per_day ?? null,
              animals: headline?.headline_animals ?? 0,
            };
          }),
        )
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
      options: [
        { value: "all", label: copy(pageContract, "filter.all_option") },
        { value: "male", label: copy(pageContract, "view.sex.male") },
        { value: "female", label: copy(pageContract, "view.sex.female") },
      ],
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

  return (
    <div className="weights-page">
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
            sex={sexFilter}
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
            href: hrefWith(params, { [TAB_PARAM]: name === "general" ? null : name, offset: null }),
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
        {periodStart} – {periodEnd}
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
            perParkGain={perParkGain}
            limit={limit}
            offset={offset}
            params={params}
          />
        ) : null}

        {tab === "breed" ? <BreedTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "birth" ? <BirthTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "shed" ? <ShedTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "weight" ? <WeightTab pageContract={pageContract} demo={demo} /> : null}

        {tab === "time" ? (
          <TimeTab pageContract={pageContract} growth={growth?.ok ? growth.data : null} demo={demo} />
        ) : null}
      </div>
    </div>
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
  perParkGain,
  limit,
  offset,
  params,
}: {
  pageContract: AdminUiPageContract;
  summary: ShedWeightsSummary;
  rows: readonly ShedWeightsRow[];
  parks: ShedWeightsResponse["parks"];
  parkFilter: string;
  modeFilter: string;
  growth: WeighingGrowthResponse | null;
  perParkGain: readonly { name: string; gain: number | null; animals: number }[];
  limit: number;
  offset: number;
  params: RouteSearchParams;
}) {
  const shedColumns = tableLabels(pageContract, "shed-weights");
  const weighedRows = rows.filter((row) => row.animals_weighed > 0);
  const visibleRows =
    modeFilter === "all" ? weighedRows : weighedRows.filter((row) => row.weighing_category === modeFilter);
  const slice = visibleRows.slice(offset, offset + limit);
  const hasAnyData = summary.animals_weighed > 0;

  // The backend's ONE daily-gain number: the animal-weighted mean over kids weighed twice PLUS
  // whole-shed pens. Every gain figure on every other tab is this same statistic, cut a
  // different way, so this card and those charts can never describe different herds.
  // Per-shed daily gain, keyed by (location, partition) -- the SAME two figures the Shed-wise tab
  // draws, so the table and that chart can never disagree about a pen. A scanned shed reports the
  // median of its kids' own gains; a whole-shed pen reports how fast its average is moving. They
  // are different measurements, which is why the row's existing capture-mode chip has to stay
  // beside this column: without it the two would read as one number.
  const shedGainByKey = new Map<string, number>();
  for (const shed of growth?.shed_leaderboard ?? []) {
    if (shed.adg_pair_count > 0) {
      shedGainByKey.set(shedKey(shed.location_id, shed.partition_label), shed.median_adg_g_per_day);
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
            <div className="lab">{copy(pageContract, "kpi.over30.label")}</div>
            <div className="val">{summary.at_or_above_30kg.toLocaleString("en-IN")}</div>
            <div className="dl">
              {summary.threshold_basis_animals.toLocaleString("en-IN")} {copy(pageContract, "kpi.threshold.basis")}
            </div>
          </div>
          <div className="kpi">
            <div className="lab">{copy(pageContract, "kpi.over35.label")}</div>
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
              <table className="tbl">
                <thead>
                  <tr>
                    {shedColumns.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {slice.map((row) => (
                    <tr key={shedKey(row.location_id, row.partition_label)}>
                      <td>{row.park_name}</td>
                      <td>
                        <b>{row.operational_location_display || row.shed_display_name}</b>
                      </td>
                      <td>
                        <Tag tone={row.weighing_category === "individual_animal" ? "info" : "mut"}>
                          {row.weighing_category === "individual_animal"
                            ? copy(pageContract, "value.weighing.individual")
                            : copy(pageContract, "value.weighing.lump")}
                        </Tag>
                      </td>
                      <td className="num">{row.animals_weighed.toLocaleString("en-IN")}</td>
                      <td className="num">{kg(row.average_weight_kg)} kg</td>
                      <td className="num">
                        {(() => {
                          // A whole-shed pen carries its own average-weight movement on the row; a
                          // scanned shed's gain comes from the growth leaderboard. A shed weighed
                          // ONCE in the window has neither -- a gain needs two weighs -- and reads
                          // as no data rather than as 0 g/day, which would claim it stopped growing.
                          const gain =
                            row.shed_average_gain_g_per_day ??
                            shedGainByKey.get(shedKey(row.location_id, row.partition_label)) ??
                            null;
                          if (gain == null) {
                            return <span className="muted">{copy(pageContract, "empty.no_data.title")}</span>;
                          }
                          return (
                            <span className={gain < 0 ? "neg" : undefined}>
                              {Math.round(gain).toLocaleString("en-IN")} g
                            </span>
                          );
                        })()}
                      </td>
                      <td className="num">{kg(row.total_weight_kg, 0)} kg</td>
                      <td className="num">
                        {row.last_weighed_date ?? (
                          <span className="muted">{copy(pageContract, "value.never_weighed")}</span>
                        )}
                      </td>
                      <td>{workflowLabel(row.bucket_status, pageContract)}</td>
                    </tr>
                  ))}
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
 * SHED-WISE — elevated shed against ground shed, per breed.
 */
function ShedTab({
  pageContract,
  demo,
}: {
  pageContract: AdminUiPageContract;
  demo: WeightDemographicsResponse | null;
}) {
  const buckets = demo?.gain_by_breed_shed_type ?? [];
  const elevated = new Map(buckets.filter((b) => b.shed_type === "elevated").map((b) => [b.label, b]));
  const ground = new Map(buckets.filter((b) => b.shed_type === "ground").map((b) => [b.label, b]));
  const breeds = [...new Set([...elevated.keys(), ...ground.keys()])].sort((a, b) =>
    a.localeCompare(b, undefined, { numeric: true }),
  );
  const groups: BarGroup[] = breeds.map((breed) => {
    const bars: GroupedBar[] = [];
    const elevatedBucket = elevated.get(breed);
    const groundBucket = ground.get(breed);
    if (elevatedBucket) {
      bars.push({
        key: `${breed}-elevated`,
        label: copy(pageContract, "view.shed_type.elevated"),
        value: Math.round(elevatedBucket.average_gain_g_per_day),
        seriesKey: "elevated",
        noteLabel: `${elevatedBucket.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
      });
    }
    if (groundBucket) {
      bars.push({
        key: `${breed}-ground`,
        label: copy(pageContract, "view.shed_type.ground"),
        value: Math.round(groundBucket.average_gain_g_per_day),
        seriesKey: "ground",
        noteLabel: `${groundBucket.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
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
        series={[
          { key: "elevated", scaleKey: "gain", label: copy(pageContract, "view.shed_type.elevated"), unit: "g", fractionDigits: 0 },
          { key: "ground", scaleKey: "gain", label: copy(pageContract, "view.shed_type.ground"), unit: "g", fractionDigits: 0 },
        ]}
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
function WeightTab({ pageContract, demo }: { pageContract: AdminUiPageContract; demo: WeightDemographicsResponse | null }) {
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
        heading: copy(pageContract, `band.weight.${band.band}`),
        // A bracket with animals but no second weigh says so, rather than leaving the reader to
        // wonder whether the gain bar failed to render.
        subheading:
          band.average_gain_g_per_day == null ? copy(pageContract, "value.weight.no_gain") : undefined,
        bars,
      };
    });

  return (
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
}: {
  pageContract: AdminUiPageContract;
  growth: WeighingGrowthResponse | null;
  demo: WeightDemographicsResponse | null;
}) {
  const bars = (growth?.weekly_gain ?? []).map((point) => ({
    key: point.week_start,
    label: point.week_start,
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
      label: point.week_start,
      value: Math.round(point.average_gain_g_per_day),
      seriesKey: "gain",
      noteLabel: `${point.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
    });
  }

  return (
    <>
    <section className="card wchart" aria-label={copy(pageContract, "section.time.aria")}>
      <h2 className="h">
        <CalendarRange className="ic" size={15} aria-hidden /> {copy(pageContract, "section.time.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.time.caption")}</p>
      <WeightBars
        data={bars}
        emptyLabel={copy(pageContract, "empty.time.body")}
        unit="g"
        chartLabel={copy(pageContract, "section.time.aria")}
        size="bands"
        wide
      />
      <p className="muted small">{copy(pageContract, "note.time.gaps")}</p>
    </section>
    {/* The selected period's weeks, one row per breed. A second section rather than more series on
        the chart above: many weeks across six breeds is dense, and stacking them on one axis
        answers "which breed" more slowly than six short rows do.

        The breed rows need not add up to the overall trend, and the caption says so -- a shed
        holding more than one breed counts in the overall series and in no breed here, because one
        shed average cannot be divided between two cohorts. */}
    <section className="card wchart" aria-label={copy(pageContract, "section.time.breed.aria")}>
      <h2 className="h">
        <Sprout className="ic" size={15} aria-hidden /> {copy(pageContract, "section.time.breed.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.time.breed.caption")}</p>
      <GroupedBars
        groups={breedWeekGroups}
        series={[{ key: "gain", label: copy(pageContract, "series.gain"), unit: "g", fractionDigits: 0 }]}
        emptyLabel={copy(pageContract, "empty.time.breed.body")}
        chartLabel={copy(pageContract, "section.time.breed.aria")}
      />
    </section>
    </>
  );
}
