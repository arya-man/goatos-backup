import { byParkThen, parksInArrivalOrder } from "@/lib/park-order";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedAnalyticsShedFeedResponse } from "@/lib/api/server";
import { FeedFilters, type FeedFilterField } from "./feed-filters";

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
// laying seven day slots out from the window, and scaling bar heights.
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
const grams = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });
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
  const scale = Math.max(1, max);

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
        <span className="small muted">{fc("shedfeed.hint")}</span>
      </div>

      <FeedFilters basePath={basePath} pageParam="fsf_offset" fields={fields} pageContract={pageContract} />

      {rows.length > 0 && pens.length > 0 ? (
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
        <div className="qgrid penbars-grid">
          {pens.map((pen) => {
            const days = byPen.get(rowId(pen)) ?? new Map<string, PenDay>();
            return (
              <div
                className="penbars"
                key={rowId(pen)}
                role="group"
                tabIndex={0}
                aria-label={`${pen.operational_location_display} · ${fc("shedfeed.chart.aria")}`}
              >
                {/* Backend-composed location, rendered verbatim: "Castro 1". */}
                <div className="penbars-title">
                  <b>{pen.operational_location_display}</b>
                  {pen.park_label ? <span className="small muted"> · {pen.park_label}</span> : null}
                  <span className="small muted">{fc("unit.g_per_head")}</span>
                </div>
                <div className="penbars-bars" role="img" aria-label={`${pen.operational_location_display} · ${fc("shedfeed.chart.aria")}`}>
                  {slots.map((iso) => {
                    const day = days.get(iso);
                    const value = day ? num(day.per_head_grams) : null;
                    const total = day ? num(day.directed_kg) : null;
                    const verified = day ? num(day.verified_per_head_grams) : null;
                    const verifiedTotal = day ? num(day.verified_kg) : null;
                    const directedTip =
                      day && value !== null
                        ? `${fc("shedfeed.legend.directed")} · ${grams(value)} ${fc("unit.g_per_head")} · ${day.head_count.toLocaleString("en-IN")} ${fc("shedfeed.day.animals")} · ${total === null ? "" : kg(total)} ${fc("shedfeed.day.total")}`
                        : `${fc("shedfeed.legend.directed")} · ${fc("shedfeed.day.gap")}`;
                    const verifiedTip =
                      day && verified !== null
                        ? `${fc("shedfeed.legend.verified")} · ${grams(verified)} ${fc("unit.g_per_head")} · ${verifiedTotal === null ? "" : kg(verifiedTotal)} ${fc("shedfeed.day.total")} · ${day.verified_bags} / ${day.planned_bags} ${fc("shedfeed.day.bags")}`
                        : `${fc("shedfeed.legend.verified")} · ${fc("shedfeed.day.verified_gap")}`;
                    // A 0 on either bar is left out of the hover, as on every feed chart.
                    const tip = [
                      dayLabel(iso),
                      value === 0 ? null : directedTip,
                      verified === 0 ? null : verifiedTip,
                    ].filter((line): line is string => line !== null).join("\n");
                    return (
                      <div className="penbars-slot" key={iso} title={tip}>
                        <div className="penbars-pair">
                          <div className="penbars-col">
                            <span className="penbars-value">{value === null ? "" : grams(value)}</span>
                            <div className="penbars-track">
                              {value === null ? (
                                <div className="penbars-gap" aria-hidden="true" />
                              ) : (
                                <div
                                  className="penbars-bar"
                                  style={{ height: `${Math.max(2, Math.round((value / scale) * 100))}%` }}
                                />
                              )}
                            </div>
                          </div>
                          <div className="penbars-col">
                            <span className="penbars-value">{verified === null ? "" : grams(verified)}</span>
                            <div className="penbars-track">
                              {verified === null ? (
                                <div className="penbars-gap" aria-hidden="true" />
                              ) : (
                                <div
                                  className="penbars-bar verified"
                                  style={{ height: `${Math.max(2, Math.round((verified / scale) * 100))}%` }}
                                />
                              )}
                            </div>
                          </div>
                        </div>
                        <span className="penbars-day small muted">{dayLabel(iso)}</span>
                      </div>
                    );
                  })}
                </div>
              </div>
            );
          })}
        </div>
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
