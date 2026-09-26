import { byParkThen, parksInArrivalOrder } from "@/lib/park-order";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedAnalyticsShedFeedResponse } from "@/lib/api/server";
import { InfoHint } from "@/components/app/info-hint";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPenColumns, type PenColumnChart } from "./feed-pen-columns";

// Feed Analytics overview — "Feed by pen" (maintainer request 2026-09-14, replacing
// the feed-mix table): pick a pen NAME and every pen of that name -- Castro 1,
// Castro 2, Castro 3 -- gets its own chart of the LAST 7 DAYS, one bar per day,
// each bar the feed the sheet DIRECTED per animal that day. The pens of one name
// share ONE scale, because the point is to compare them side by side: on two
// scales a taller bar can mean less feed.
//
// Same rules as before: the backend serves the bounded pen set for its own
// 7-day window, and farm / pen narrowing runs over the SERVED rows. This file
// composes no business copy and derives no business number -- the per-animal
// figure is the backend's `per_head_grams` (kg over the pen's head count that
// day, heads counted once per day), the head count and the day's kg ride on the
// bar's tooltip as served, and the only local work is picking which pens to draw,
// laying seven day slots out from the window, and fixing the shared value axis.
//
// The columns themselves are the kit column chart (feed-pen-columns.tsx → TrendChart):
// figures on hover only, draw-in on first sight, pens of one name on ONE axis.
//
// A day the sheet directed nothing resolvable to the pen is ABSENT from the
// response and renders as a GAP with the backend's gap copy, never as a zero bar
// that would read as "the animals were given nothing".
//
// VERIFIED beside DIRECTED (maintainer request 2026-09-18): each day carries a
// second bar, the backend's `verified_per_head_grams` -- the packed kg the
// verifier typed when approving that day's packing videos, over the SAME head
// count -- so the two bars of one day are comparable and the pens still share
// one scale across both series. A day with no approved bag yet is a gap on the
// verified side only, with the backend's "not yet verified" copy; a day only
// partly approved shows what is verified so far and says "1 of 2 bags" in its
// tooltip (maintainer choice, same day), so a half-day is never read as a short
// measure. The coverage fraction is the backend's verified_bags / planned_bags,
// rendered, never derived here.

type PenRow = FeedAnalyticsShedFeedResponse["rows"][number];
type PenDay = PenRow["days"][number];

// A blank wire figure is ABSENT, never zero: `Number("")` is 0, and reading the
// backend's empty verified_per_head_grams as 0 drew a "0 g · 0 / 2 bags verified"
// bar on a day nobody had verified yet -- the exact "she measured nothing" the
// empty string exists to prevent.
const num = (raw: string) => {
  if (raw.trim() === "") return null;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : null;
};
const kg = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

/** Stable identity of a pen: the row's own location key, never the display string. */
function rowId(row: PenRow): string {
  return `${row.shed_id}|${row.partition_label}`;
}

/** The window's days, inclusive, as ISO dates -- the seven slots every chart draws. */
function daySlots(from: string, to: string): string[] {
  const out: string[] = [];
  const start = new Date(`${from}T00:00:00Z`);
  const end = new Date(`${to}T00:00:00Z`);
  for (let d = start; d <= end && out.length < 31; d = new Date(d.getTime() + 86_400_000)) {
    out.push(d.toISOString().slice(0, 10));
  }
  return out;
}

const DAY_LABEL = new Intl.DateTimeFormat("en-IN", { weekday: "short", day: "2-digit", timeZone: "UTC" });

/** "Mon 08" -- a date, not copy; the weekday is what lets a reader spot a feed-day pattern. */
function dayLabel(iso: string): string {
  return DAY_LABEL.format(new Date(`${iso}T00:00:00Z`));
}

export function FeedShedFeedCharts({
  data,
  basePath,
  pageContract,
  filters,
  parkScopeLocked,
}: {
  data: FeedAnalyticsShedFeedResponse;
  basePath: string;
  pageContract: AdminUiPageContract;
  filters: { park: string; shed: string };
  /** True when the top bar already fixes the park, which disables this section's own farm select. */
  parkScopeLocked: boolean;
}) {
  const fc = (key: string) => copy(pageContract, key);
  const rows = data.rows ?? [];

  // Farms in the order the backend served the rows — park CODE order, CBE then CPT
  // (maintainer decision 2026-09-16) — never sorted by label, which put Channapatna first.
  const parkOrder = parksInArrivalOrder(rows, (row) => row.park_label);
  const parkOptions = dedupe(rows.map((row) => ({ value: row.park_id, label: row.park_label })), parkOrder);
  // Pen-name options follow the farm selection, keyed by shed_id never by name
  // (Castro, Gandhi, Yashoda exist in BOTH farms); every pen carries its farm, so
  // the dropdown never prints the same word twice and never leaves one unnamed. Under All farms
  // the list is two clusters, CBE's pens then CPT's, each A→Z.
  const inFarm = rows.filter((row) => filters.park === "" || row.park_id === filters.park);
  const shedOptions = disambiguateByPark(
    inFarm.map((row) => ({ value: row.shed_id, label: row.shed_label, park: row.park_label })),
    parkOrder,
  );
  // A pen name is always chosen: the section draws ONE name's pens. With none
  // picked (or a pick the farm change invalidated) it opens on the first name.
  const shed =
    shedOptions.some((option) => option.value === filters.shed) ? filters.shed : (shedOptions[0]?.value ?? "");
  const pens = inFarm.filter((row) => row.shed_id === shed);

  const slots = daySlots(data.date_from, data.date_to);
  const byPen = new Map<string, Map<string, PenDay>>();
  let max = 0;
  for (const pen of pens) {
    const days = new Map<string, PenDay>();
    for (const day of pen.days) {
      days.set(day.feed_day, day);
      const value = num(day.per_head_grams);
      if (value !== null && value > max) max = value;
      const verified = num(day.verified_per_head_grams);
      if (verified !== null && verified > max) max = verified;
    }
    byPen.set(rowId(pen), days);
  }
  // ONE scale for every pen of the chosen name: the top of the shared axis is the tallest figure
  // across every drawn pen and both series, so on two charts a taller bar never means less feed.
  const scale = Math.max(1, max);

  // The charts' data, composed here from the served figures and backend copy: a day the sheet
  // directed nothing is null (a gap, never a zero bar), and so is a day nobody has verified yet.
  const charts: PenColumnChart[] = pens.map((pen) => {
    const days = byPen.get(rowId(pen)) ?? new Map<string, PenDay>();
    return {
      key: rowId(pen),
      title: pen.operational_location_display,
      park: pen.park_label || undefined,
      ariaLabel: `${pen.operational_location_display} · ${fc("shedfeed.chart.aria")}`,
      days: slots.map((iso) => {
        const day = days.get(iso);
        const value = day ? num(day.per_head_grams) : null;
        const total = day ? num(day.directed_kg) : null;
        const verified = day ? num(day.verified_per_head_grams) : null;
        const verifiedTotal = day ? num(day.verified_kg) : null;
        const directedDetail =
          day && value !== null
            ? `${day.head_count.toLocaleString("en-IN")} ${fc("shedfeed.day.animals")} · ${total === null ? "" : kg(total)} ${fc("shedfeed.day.total")}`
            : fc("shedfeed.day.gap");
        const verifiedDetail =
          day && verified !== null
            ? `${verifiedTotal === null ? "" : kg(verifiedTotal)} ${fc("shedfeed.day.total")} · ${day.verified_bags} / ${day.planned_bags} ${fc("shedfeed.day.bags")}`
            : fc("shedfeed.day.verified_gap");
        return {
          day: iso,
          label: dayLabel(iso),
          directed: value,
          verified,
          // A 0 on either bar is left out of the hover, as on every feed chart.
          detail: [
            value === 0 ? null : `${fc("shedfeed.legend.directed")} · ${directedDetail}`,
            verified === 0 ? null : `${fc("shedfeed.legend.verified")} · ${verifiedDetail}`,
          ].filter((line): line is string => line !== null).join("\n"),
        };
      }),
    };
  });

  const fields: FeedFilterField[] = [
    {
      kind: "select",
      param: "fsf_park",
      label: fc("filter.park_label"),
      value: filters.park,
      allowAll: true,
      // Changing farm invalidates the pen-name pick: a name of the other farm
      // matches nothing, and the section would silently fall back to another.
      clears: ["fsf_shed"],
      disabledReason: parkScopeLocked ? fc("filter.scope_readonly") : undefined,
      options: parkOptions,
    },
    {
      kind: "select",
      param: "fsf_shed",
      label: fc("shedfeed.filter.shed"),
      value: shed,
      allowAll: false,
      options: shedOptions,
    },
  ];

  return (
    <section className="card" aria-label={fc("shedfeed.title")}>
      <div className="hd">
        <h3>{fc("shedfeed.title")}</h3>
        {/* The section's meaning lives behind the title's hint, not in a paragraph under it. */}
        <InfoHint text={fc("shedfeed.hint")} />
      </div>

      <FeedFilters basePath={basePath} pageParam="fsf_offset" fields={fields} pageContract={pageContract} />

      {rows.length > 0 && pens.length > 0 ? (
        // One legend for every pen of the name (they share the series and the axis), top-right.
        <div className="penbars-legend" aria-hidden="true">
          <span>
            <i />
            {fc("shedfeed.legend.directed")}
          </span>
          <span>
            <i className="verified" />
            {fc("shedfeed.legend.verified")}
          </span>
        </div>
      ) : null}

      {rows.length === 0 ? (
        <p className="muted small">{fc("shedfeed.empty")}</p>
      ) : pens.length === 0 ? (
        <p className="muted small">{fc("shedfeed.empty_filtered")}</p>
      ) : (
        <FeedPenColumns
          pens={charts}
          yMax={scale}
          directedLabel={fc("shedfeed.legend.directed")}
          verifiedLabel={fc("shedfeed.legend.verified")}
          unitLabel={fc("unit.g_per_head")}
          missingLabel="—"
        />
      )}
    </section>
  );
}

/**
 * Pen-name options, with the farm appended ONLY to labels that would otherwise appear twice.
 *
 * Dedupe is by shed_id (two farms really do own a shed called Castro); the label pass is what
 * stops the dropdown from showing the same word twice with no way to choose between them.
 * Clustered by farm in `parkOrder`, A→Z inside each farm.
 */
function disambiguateByPark(
  options: { value: string; label: string; park: string }[],
  parkOrder: readonly string[],
): { value: string; label: string }[] {
  const byId = new Map<string, { label: string; park: string }>();
  for (const option of options) {
    if (option.value !== "" && !byId.has(option.value)) {
      byId.set(option.value, { label: option.label, park: option.park });
    }
  }
  return [...byId.entries()]
    .map(([value, { label, park }]) => ({
      value,
      park,
      // EVERY pen names its farm (maintainer request 2026-09-24): naming it only on the
      // names both farms share left "Castro · CBE" beside a bare "Godel 1 - Part 1".
      label: park ? `${label} · ${park}` : label,
    }))
    .sort(byParkThen(parkOrder, (option) => option.park, (a, b) => a.label.localeCompare(b.label, undefined, { numeric: true })))
    .map(({ value, label }) => ({ value, label }));
}

/** First label wins per value, in `parkOrder` (the served farm order) for a stable dropdown. */
function dedupe(
  options: { value: string; label: string }[],
  parkOrder: readonly string[],
): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const option of options) {
    if (option.value !== "" && !seen.has(option.value)) seen.set(option.value, option.label);
  }
  return [...seen.entries()]
    .map(([value, label]) => ({ value, label }))
    .sort(byParkThen(parkOrder, (option) => option.label));
}
