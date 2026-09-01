import { copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedAnalyticsShedFeedResponse } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import { seriesColorVar } from "./feed-analytics-charts";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";

// Feed Analytics overview — "Feed by shed" (maintainer ask 2026-08-31): below the
// charts, every operational location across the farms with the feed items and kg
// the sheet DIRECTED there over the table's own LAST-7-DAYS window, pen-perfect
// (partitions carried, backend-composed display rendered verbatim).
//
// Same rules as the completion table one section over: the backend serves the
// bounded pen set for the window; farm / shed / feed-item narrowing plus the
// ten-row-default pager run over the SERVED rows. This file composes no business
// copy and derives no business number — every kg is a backend field, and the
// only local work is formatting, filtering, and the colour assignment (each feed
// item keeps ONE colour across every row, ranked by its window kg across the
// served pens — the same rank-by-window-kg rule the charts above apply).

/** The backend table contract this table renders — it owns the page-size vocabulary. */
const TABLE_ID = "shed-feed-mix";

type PenRow = FeedAnalyticsShedFeedResponse["rows"][number];

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });
const num = (raw: string) => {
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : 0;
};

/** Stable identity of a pen: the row's own location key, never the display string. */
function rowId(row: PenRow): string {
  return `${row.shed_id}|${row.partition_label}`;
}

export function FeedShedFeedTable({
  data,
  searchParams,
  basePath,
  pageContract,
  filters,
  parkScopeLocked,
}: {
  data: FeedAnalyticsShedFeedResponse;
  searchParams: RouteSearchParams;
  basePath: string;
  pageContract: AdminUiPageContract;
  filters: { park: string; shed: string; item: string; offset: number; limit: number };
  /** True when the top bar already fixes the park, which disables this table's own farm select. */
  parkScopeLocked: boolean;
}) {
  const fc = (key: string) => copy(pageContract, key);
  const rows = data.rows ?? [];

  // Narrowing runs over the SERVED rows. Farm removes other-farm pens, shed
  // keeps that shed's pens, and the feed-item select keeps pens whose window
  // mix contains that item (only that item's line is shown for them).
  const inScope = rows.filter(
    (row) =>
      (filters.park === "" || row.park_id === filters.park) &&
      (filters.shed === "" || row.shed_id === filters.shed),
  );
  const visible = inScope.filter(
    (row) => filters.item === "" || row.items.some((item) => item.feed_item_key === filters.item),
  );

  // ONE PAGE OF TEN by default, from the table contract; the offset is CLAMPED
  // to the last page so a pasted link or a narrowing filter never lands on a
  // blank table that reads as "no pens".
  const pageSizes = tablePageSizes(pageContract, TABLE_ID);
  const limit = pageSizes.includes(filters.limit) ? filters.limit : (pageSizes[0] ?? 10);
  const lastPageOffset = Math.max(0, Math.floor(Math.max(visible.length - 1, 0) / limit) * limit);
  const offset = Math.min(Math.max(filters.offset, 0), lastPageOffset);
  const paged = visible.slice(offset, offset + limit);

  const parkOptions = dedupe(rows.map((row) => ({ value: row.park_id, label: row.park_label })));
  // Shed options follow the farm selection, keyed by shed_id never by name
  // (Castro, Gandhi, Yashoda exist in BOTH farms); a label shared across farms
  // carries its farm so the dropdown never prints the same word twice.
  const shedOptions = disambiguateByPark(
    rows
      .filter((row) => filters.park === "" || row.park_id === filters.park)
      .map((row) => ({ value: row.shed_id, label: row.shed_label, park: row.park_label })),
  );
  // Feed-item options span the served window's mix; keyed by feed_item_key, the
  // backend's own vocabulary, with first label winning per key.
  const itemOptions = dedupe(
    rows.flatMap((row) =>
      row.items.map((item) => ({ value: item.feed_item_key, label: item.feed_item_label })),
    ),
  );

  // ONE colour per feed item across every row, ranked by the item's kg across
  // the served pens (largest first) — the charts' rank-by-window-kg rule, so a
  // reader scanning down a column sees the same item in the same colour.
  const itemColor = new Map<string, string>();
  {
    const totals = new Map<string, number>();
    for (const row of rows) {
      for (const item of row.items) {
        totals.set(item.feed_item_key, (totals.get(item.feed_item_key) ?? 0) + num(item.directed_kg));
      }
    }
    [...totals.entries()]
      .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
      .forEach(([key], index) => itemColor.set(key, seriesColorVar(index)));
  }

  const fields: FeedFilterField[] = [
    {
      kind: "select",
      param: "fsf_park",
      label: fc("filter.park_label"),
      value: filters.park,
      allowAll: true,
      // Changing farm invalidates the shed pick: a shed of the other farm
      // matches nothing and the reader cannot tell why the table went empty.
      clears: ["fsf_shed"],
      disabledReason: parkScopeLocked ? fc("filter.scope_readonly") : undefined,
      options: parkOptions,
    },
    {
      kind: "select",
      param: "fsf_shed",
      label: fc("shedfeed.filter.shed"),
      value: filters.shed,
      allowAll: true,
      options: shedOptions,
    },
    {
      kind: "select",
      param: "fsf_item",
      label: fc("shedfeed.filter.item"),
      value: filters.item,
      allowAll: true,
      options: itemOptions,
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
      ) : visible.length === 0 ? (
        <p className="muted small">{fc("shedfeed.empty_filtered")}</p>
      ) : (
        <>
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fc("shedfeed.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fc("col.shedfeed.park")}</th>
                  <th>{fc("col.shedfeed.pen")}</th>
                  <th>{fc("col.shedfeed.items")}</th>
                </tr>
              </thead>
              <tbody>
                {paged.map((row) => {
                  // With a feed-item filter active only that item's line shows,
                  // so the reader compares one feed across pens undistracted.
                  const shown =
                    filters.item === ""
                      ? row.items
                      : row.items.filter((item) => item.feed_item_key === filters.item);
                  return (
                    <tr key={rowId(row)}>
                      <td>{row.park_label}</td>
                      {/* Backend-composed location, rendered verbatim: "Godel 1 - Part 3". */}
                      <td>{row.operational_location_display}</td>
                      <td>
                        <div className="sfmix">
                          {shown.map((item) => (
                            <div className="sfmix-row" key={item.feed_item_key}>
                              <span
                                className="sfmix-dot"
                                style={{ background: itemColor.get(item.feed_item_key) }}
                                aria-hidden="true"
                              />
                              <span className="sfmix-label">{item.feed_item_label}</span>
                              <span className="sfmix-kg">
                                {nf(num(item.directed_kg))} {fc("unit.kg")}
                              </span>
                            </div>
                          ))}
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <FeedPager
            pageContract={pageContract}
            offset={offset}
            limit={limit}
            rowCount={paged.length}
            hasMore={offset + limit < visible.length}
            noun={fc("shedfeed.pager.noun")}
            pageSizeOptions={pageSizes}
            hrefForOffset={(next) => hrefWith(basePath, searchParams, { fsf_offset: String(next) })}
            // A page-size change returns to the FIRST page, same as every table here.
            hrefForLimit={(next) =>
              hrefWith(basePath, searchParams, { fsf_limit: String(next), fsf_offset: undefined })
            }
          />
        </>
      )}
    </section>
  );
}

/** Preserves every other param, so paging never resets the page's tab, range or filters. */
function hrefWith(
  basePath: string,
  sp: RouteSearchParams | undefined,
  next: Record<string, string | undefined>,
): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(sp ?? {})) {
    if (key in next) continue;
    if (Array.isArray(value)) value.forEach((item) => params.append(key, item));
    else if (value) params.set(key, value);
  }
  for (const [key, value] of Object.entries(next)) {
    if (value) params.set(key, value);
  }
  const query = params.toString();
  return query ? `${basePath}?${query}` : basePath;
}

/**
 * Shed options, with the farm appended ONLY to labels that would otherwise appear twice.
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
