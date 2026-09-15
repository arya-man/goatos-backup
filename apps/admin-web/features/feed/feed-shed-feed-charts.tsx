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

type PenRow = FeedAnalyticsShedFeedResponse["rows"][number];
type PenDay = PenRow["days"][number];

const num = (raw: string) => {
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

/** "Mon 08" -- a date, not copy; the weekday is what lets a reader spot a feed-day pattern. */
const DAY_LABEL = new Intl.DateTimeFormat("en-IN", { weekday: "short", day: "2-digit", timeZone: "UTC" });
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

  const parkOptions = dedupe(rows.map((row) => ({ value: row.park_id, label: row.park_label })));
  // Pen-name options follow the farm selection, keyed by shed_id never by name
  // (Castro, Gandhi, Yashoda exist in BOTH farms); a label shared across farms
  // carries its farm so the dropdown never prints the same word twice.
  const inFarm = rows.filter((row) => filters.park === "" || row.park_id === filters.park);
  const shedOptions = disambiguateByPark(
    inFarm.map((row) => ({ value: row.shed_id, label: row.shed_label, park: row.park_label })),
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

      {rows.length === 0 ? (
        <p className="muted small">{fc("shedfeed.empty")}</p>
      ) : pens.length === 0 ? (
        <p className="muted small">{fc("shedfeed.empty_filtered")}</p>
      ) : (
        <div className="qgrid penbars-grid">
          {pens.map((pen) => {
            const days = byPen.get(rowId(pen)) ?? new Map<string, PenDay>();
            return (
              <div className="penbars" key={rowId(pen)}>
                {/* Backend-composed location, rendered verbatim: "Castro 1". */}
                <div className="penbars-title">
                  <b>{pen.operational_location_display}</b>
                  <span className="small muted">{fc("unit.g_per_head")}</span>
                </div>
                <div className="penbars-bars" role="img" aria-label={`${pen.operational_location_display} · ${fc("shedfeed.chart.aria")}`}>
                  {slots.map((iso) => {
                    const day = days.get(iso);
                    const value = day ? num(day.per_head_grams) : null;
                    const total = day ? num(day.directed_kg) : null;
                    const tip =
                      day && value !== null
                        ? `${dayLabel(iso)} · ${grams(value)} ${fc("unit.g_per_head")} · ${day.head_count.toLocaleString("en-IN")} ${fc("shedfeed.day.animals")} · ${total === null ? "" : kg(total)} ${fc("shedfeed.day.total")}`
                        : `${dayLabel(iso)} · ${fc("shedfeed.day.gap")}`;
                    return (
                      <div className="penbars-slot" key={iso} title={tip}>
                        <span className="penbars-value small">{value === null ? "" : grams(value)}</span>
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
 */
function disambiguateByPark(
  options: { value: string; label: string; park: string }[],
): { value: string; label: string }[] {
  const byId = new Map<string, { label: string; park: string }>();
  for (const option of options) {
    if (option.value !== "" && !byId.has(option.value)) {
      byId.set(option.value, { label: option.label, park: option.park });
    }
  }
  const labelCounts = new Map<string, number>();
  for (const { label } of byId.values()) {
    labelCounts.set(label, (labelCounts.get(label) ?? 0) + 1);
  }
  return [...byId.entries()]
    .map(([value, { label, park }]) => ({
      value,
      label: (labelCounts.get(label) ?? 0) > 1 ? `${label} · ${park}` : label,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

/** First label wins per value, sorted for a stable dropdown. */
function dedupe(options: { value: string; label: string }[]): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const option of options) {
    if (option.value !== "" && !seen.has(option.value)) seen.set(option.value, option.label);
  }
  return [...seen.entries()]
    .map(([value, label]) => ({ value, label }))
    .sort((a, b) => a.label.localeCompare(b.label));
}
