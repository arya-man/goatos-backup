import { redirect } from "next/navigation";
import { CalendarRange, Gauge, Scale, Sprout, Warehouse } from "lucide-react";

import { GroupedBars, type BarGroup, type GroupedBar } from "./grouped-bars";
import { WeightBars } from "./weight-bars";
import { SegmentedLinks } from "@/components/segmented-links";
import { Tag } from "@/components/ui-primitives";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { istDayPlus, todayIso } from "@/lib/format";
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

const PAGE_PATH = "/weighing/analytics";
const SEX_PARAM = "sex";
const TAB_PARAM = "tab";
const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;
const DEFAULT_LIMIT = 25;

/** Time-wise is fixed at twelve weeks — a quarter — whatever the period filter says. */
const TREND_WEEKS = 12;

const TABS = ["general", "breed", "birth", "shed", "time"] as const;
type Tab = (typeof TABS)[number];

/**
 * The tabs whose figures the Weighing filter can actually narrow: the two built from per-shed
 * ROWS. The other three read aggregates that arrive with both capture modes already blended into
 * one mean, which no client-side filter can unpick — so the control is not offered there rather
 * than offered and ignored.
 */
const WEIGHING_FILTER_TABS = new Set<Tab>(["general", "shed"]);

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

/**
 * The twelve-week window, aligned to ISO Mondays so the first bucket is a whole week rather than
 * a stub. The backend buckets on Monday too, so a window starting mid-week would draw a short
 * first bar that reads as a slow week instead of a partial one.
 */
function trendWindow(today: string): { from: string; to: string } {
  const parsed = new Date(`${today}T00:00:00Z`);
  // getUTCDay is 0=Sunday; shift so 0=Monday, matching date_trunc('week').
  const mondayOffset = (parsed.getUTCDay() + 6) % 7;
  return { from: istDayPlus(today, -(mondayOffset + (TREND_WEEKS - 1) * 7)), to: today };
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
function shedBreed(
  composition: { chips: readonly { breed?: string; animals: number }[] } | undefined,
  pageContract: AdminUiPageContract,
): { key: string; heading: string } {
  const breeds = [...new Set((composition?.chips ?? []).map((chip) => chip.breed?.trim()).filter(Boolean))];
  if (breeds.length === 1) return { key: breeds[0] as string, heading: breeds[0] as string };
  if (breeds.length > 1) return { key: "__mixed", heading: copy(pageContract, "value.shed.mixed") };
  return { key: "__unknown", heading: copy(pageContract, "value.shed.unknown") };
}

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
  const modeFilter = one(params, "weighing") ?? "all";
  // MALE is the default, matching /weighing/weights (maintainer request 2026-09-01): the farm's
  // growth question is about the males it is fattening, so an unfiltered landing would show a
  // number nobody asked for. Every kid stays one click away as an explicit `sex=all`; anything
  // else falls back to the default rather than emptying the page.
  const rawSex = one(params, SEX_PARAM);
  const sexFilter = rawSex === "female" ? "female" : rawSex === "all" ? "" : "male";
  // What the CONTROL shows. The reads take "" for every kid; the control cannot, or its All
  // option would be the blank one and would read back as the male default on the next request.
  const sexChoice = sexFilter === "" ? "all" : sexFilter;

  const limit = boundedLimit(one(params, "limit"));
  const offset = boundedOffset(one(params, "offset"));

  const today = todayIso();
  const window = await landingWindow(params, today, parkFilter, sexFilter);
  // Time-wise is the ONE tab the period filter does not move, so it fetches its own window.
  const readWindow = tab === "time" ? trendWindow(today) : window;

  const scope = {
    park_id: parkFilter || undefined,
    sex: sexFilter || undefined,
  };

  // Every tab needs the shed read: it carries the park vocabulary the filter bar renders, plus
  // the whole-shed gain figures the Shed-wise tab reads. Only the tab's own extra reads are
  // fetched beside it, so opening Breed-wise does not pay for the Growth queries.
  const wantsGrowth = tab === "general" || tab === "shed" || tab === "time";
  const wantsDemographics = tab === "breed" || tab === "shed" || tab === "birth";

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
      markerFetchPath: `/api/weighing/lump-markers${parkFilter ? `?park_id=${encodeURIComponent(parkFilter)}` : ""}`,
    },
    // WEIGHING IS OFFERED ONLY WHERE IT ACTUALLY NARROWS SOMETHING (maintainer decision, review of
    // PR 162). It selects a capture MODE -- kids scanned one at a time, or a whole pen on the scale
    // once -- and the page filters on that client-side, over shed rows. General and Shed-wise have
    // those rows; Breed-wise, Birth-wise and Time-wise are served by aggregates that arrive with
    // both modes already blended, and no client-side filter can take one back out of a mean.
    //
    // It was rendered on all five tabs and did nothing on three of them: a reader could select
    // "Lump sum" and the breed chart would carry on counting scanned kids, with the control sitting
    // there claiming otherwise. A filter that silently does nothing is worse than an absent one --
    // it is a wrong answer the reader has no reason to doubt.
    //
    // The PARAMETER is deliberately left alone when the control is hidden, so a choice made on
    // General survives a trip through Breed-wise and is still set on the way back. Those tabs'
    // captions say in farm words that both ways of weighing are counted.
    ...(WEIGHING_FILTER_TABS.has(tab)
      ? [
          {
            // allowAll:false — this vocabulary already carries its own All, and the bar's generic
            // blank option on top would make two Alls that do not agree.
            kind: "select" as const,
            param: "weighing",
            label: copy(pageContract, "filter.weighing.label"),
            value: modeFilter,
            allowAll: false,
            options: modeOptions.map((option) => ({ value: option.key, label: option.label })),
          },
        ]
      : []),
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
  ];

  return (
    <div className="weights-page">
      <WorklistFilters basePath={PAGE_PATH} pageParam="offset" fields={filterFields} pageContract={pageContract} />

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

      <p className="muted small" style={{ margin: "0 0 -4px" }}>
        {copy(pageContract, "kpi.sheds.label")}: {summary.sheds_weighed} / {summary.sheds_in_scope}
        {" · "}
        {periodStart} – {periodEnd}
      </p>

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

      {tab === "shed" ? (
        <ShedTab
          pageContract={pageContract}
          rows={rows}
          modeFilter={modeFilter}
          demo={demo}
          growth={growth?.ok ? growth.data : null}
        />
      ) : null}

      {tab === "time" ? (
        <TimeTab pageContract={pageContract} growth={growth?.ok ? growth.data : null} />
      ) : null}
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
      <p className="muted small">{copy(pageContract, "section.general.caption")}</p>

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
 * SHED-WISE — every shed's daily gain, grouped by the breed it holds.
 *
 * Grouped rather than one long list because the question is "which of MY sheep pens is lagging",
 * and a flat list of ~40 pens across six breeds answers it only after the reader mentally
 * re-sorts it. Within a group the sheds are ALPHABETICAL, so a named pen is where it was last
 * time; the bar lengths carry the comparison.
 *
 * TWO MEASUREMENTS SHARE THIS CHART and each bar says which it is. A per-animal shed reports the
 * median of its kids' own gains; a whole-shed pen reports how fast its AVERAGE is moving, which
 * population change also moves. Merging them silently would be the defect; showing only the first
 * would drop most of this farm's kids, who are weighed by the whole shed.
 */
function ShedTab({
  pageContract,
  rows,
  modeFilter,
  demo,
  growth,
}: {
  pageContract: AdminUiPageContract;
  rows: readonly ShedWeightsRow[];
  modeFilter: string;
  demo: WeightDemographicsResponse | null;
  growth: WeighingGrowthResponse | null;
}) {
  const compositionByShed = new Map(
    (demo?.shed_composition ?? []).map((item) => [shedKey(item.location_id, item.partition_label), item]),
  );
  const weighedRows = rows.filter((row) => row.animals_weighed > 0);
  const visibleRows =
    modeFilter === "all" ? weighedRows : weighedRows.filter((row) => row.weighing_category === modeFilter);
  const visibleKeys = new Set(visibleRows.map((row) => shedKey(row.location_id, row.partition_label)));

  type ShedGain = { key: string; label: string; value: number; breed: { key: string; heading: string }; note: string; tone: "info" | "mut" };

  const perAnimal: ShedGain[] = (growth?.shed_leaderboard ?? [])
    .filter((shed) => shed.adg_pair_count > 0 && visibleKeys.has(shedKey(shed.location_id, shed.partition_label)))
    .map((shed) => {
      const key = shedKey(shed.location_id, shed.partition_label);
      return {
        key,
        // The park travels IN the label here, not as a separate column: 39 shed names exist in
        // BOTH parks, so a bare pen name would silently merge two different sheds.
        label: `${shed.park_name} · ${shed.operational_location_display || shed.display_name}`,
        value: Math.round(shed.median_adg_g_per_day),
        breed: shedBreed(compositionByShed.get(key), pageContract),
        note: copy(pageContract, "value.weighing.individual"),
        tone: "info" as const,
      };
    });

  const wholeShed: ShedGain[] = visibleRows
    .filter((row) => row.shed_average_gain_g_per_day != null)
    .map((row) => {
      const key = shedKey(row.location_id, row.partition_label);
      return {
        key: `${key}-shed`,
        label: `${row.park_name} · ${row.operational_location_display || row.shed_display_name}`,
        value: Math.round(row.shed_average_gain_g_per_day as number),
        breed: shedBreed(compositionByShed.get(key), pageContract),
        note: copy(pageContract, "value.weighing.lump"),
        tone: "mut" as const,
      };
    });

  const byBreed = new Map<string, { heading: string; bars: GroupedBar[] }>();
  for (const shed of [...perAnimal, ...wholeShed]) {
    const group = byBreed.get(shed.breed.key) ?? { heading: shed.breed.heading, bars: [] };
    group.bars.push({
      key: shed.key,
      label: shed.label,
      value: shed.value,
      seriesKey: "gain",
      noteLabel: shed.note,
      noteTone: shed.tone,
    });
    byBreed.set(shed.breed.key, group);
  }

  // Named breeds first, alphabetically; the mixed and unrecorded groups last, because they are
  // the residue rather than a breed anyone is comparing.
  const groups: BarGroup[] = [...byBreed.entries()]
    .sort(([a], [b]) => {
      const residue = (key: string) => (key.startsWith("__") ? 1 : 0);
      return residue(a) - residue(b) || a.localeCompare(b, undefined, { numeric: true });
    })
    .map(([key, group]) => ({
      key,
      heading: group.heading,
      // ALPHABETICAL within the breed (maintainer request), not ranked by gain. A rank tells a
      // reader nothing about where a specific pen will be, so finding "Castro 2" meant scanning
      // the whole group; a stable A-Z order means the same pen sits in the same place on every
      // load, and the bar lengths still show at a glance which pens are behind. `numeric` keeps
      // "Castro 2" ahead of "Castro 10". Same order the Weights page's shed chart uses.
      bars: group.bars.slice().sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true })),
    }));

  return (
    <section className="card wchart" aria-label={copy(pageContract, "section.shed.aria")}>
      <h2 className="h">
        <Warehouse className="ic" size={15} aria-hidden /> {copy(pageContract, "section.shed.title")}
      </h2>
      <p className="muted small">{copy(pageContract, "section.shed.caption")}</p>
      <GroupedBars
        groups={groups}
        series={[{ key: "gain", label: copy(pageContract, "series.gain"), unit: "g", fractionDigits: 0 }]}
        emptyLabel={copy(pageContract, "empty.shed.body")}
        chartLabel={copy(pageContract, "section.shed.aria")}
      />
    </section>
  );
}

/**
 * TIME-WISE — the last twelve weeks of daily gain.
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
function TimeTab({ pageContract, growth }: { pageContract: AdminUiPageContract; growth: WeighingGrowthResponse | null }) {
  const bars = (growth?.weekly_gain ?? []).map((point) => ({
    key: point.week_start,
    label: point.week_start,
    value: Math.round(point.average_adg_g_per_day),
    valueLabel: `${Math.round(point.average_adg_g_per_day).toLocaleString("en-IN")} g`,
    modeLabel: `${point.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.time.animals")}`,
    modeTone: "mut" as const,
  }));

  return (
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
  );
}
