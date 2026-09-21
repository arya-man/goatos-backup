import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { FeedAnalyticsStockLoadRow, FeedAnalyticsStockLoadsResponse } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";

// Feed Analytics -> Purchased vs consumed (maintainer request 2026-09-19). One row per load in
// the purchase ledger: when it was bought, how many days the buyer SAID it would cover, and what
// the locked sheets actually did to it, load by load in arrival order. Every figure is a backend
// field -- the FIFO split, the day counts and the check column all come off the wire -- and this
// component only decides how to colour them: a check above zero is the load that ran out sooner
// than it was bought for, and it reads red because that is the one that leaves animals unfed.

const STATUS_TONE: Record<FeedAnalyticsStockLoadRow["status"], Tone> = {
  in_transit: "mut",
  not_started: "info",
  in_use: "ok",
  finished: "mut",
  overrun: "dng",
};

const kg = (raw: string) => {
  const value = Number(raw);
  return Number.isFinite(value) ? value.toLocaleString("en-IN", { maximumFractionDigits: 1 }) : raw;
};

export function FeedStockLoadsTable({
  data,
  pageContract,
  basePath,
  searchParams,
  filters,
}: {
  data: FeedAnalyticsStockLoadsResponse;
  pageContract: AdminUiPageContract;
  basePath: string;
  searchParams: RouteSearchParams;
  filters: {
    farm: string;
    item: string;
    limit: number;
    offset: number;
    pageSizes: readonly number[];
  };
}) {
  const fl = (key: string) => copy(pageContract, key);
  const rows = data.rows;
  const hasMore = data.offset + rows.length < data.total;

  const fields: FeedFilterField[] = [
    {
      kind: "select",
      param: "fl_farm",
      label: fl("loads.filter.farm"),
      value: filters.farm,
      allowAll: true,
      // Farm labels are the ledger's own, served by the backend; nothing here names a farm.
      options: data.farms.map((farm) => ({ value: farm, label: farm })),
    },
    {
      kind: "select",
      param: "fl_item",
      label: fl("loads.filter.feed"),
      value: filters.item,
      allowAll: true,
      options: data.feed_items.map((item) => ({ value: item.key, label: item.label })),
    },
  ];

  return (
    <section className="card feed-loads-table" aria-label={fl("loads.title")}>
      <div className="hd">
        <h3>{fl("loads.title")}</h3>
        <span className="small muted">{fl("loads.hint")}</span>
      </div>

      <FeedFilters basePath={basePath} pageParam="fl_offset" fields={fields} pageContract={pageContract} />

      {data.total === 0 ? (
        <p className="muted small">{fl("loads.empty")}</p>
      ) : (
        <>
          <div className="tablewrap" tabIndex={0} role="group" aria-label={fl("loads.title")}>
            <table className="tbl">
              <thead>
                <tr>
                  <th>{fl("loads.col.purchase_date")}</th>
                  <th>{fl("loads.col.feed_item")}</th>
                  <th>{fl("loads.col.status")}</th>
                  <th className="num">{fl("loads.col.purchased_kg")}</th>
                  <th className="num">{fl("loads.col.consumed_kg")}</th>
                  <th className="num">{fl("loads.col.left_kg")}</th>
                  <th className="num">{fl("loads.col.days_said")}</th>
                  <th className="num">{fl("loads.col.days_consumed")}</th>
                  <th className="num">{fl("loads.col.days_left")}</th>
                  <th>{fl("loads.col.gap_days")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.feed_purchase_id} data-testid="feed-stock-load-row" data-gap={row.gap_days ?? ""}>
                    <td>
                      {fmtDate(row.purchase_date)}
                      {/* The load's life in dates sits under the day it was bought, so the status
                          cell stays one chip wide and the check column stays on screen. */}
                      {row.consumption_from ? (
                        <div className="muted small">
                          {fl("loads.consumption_from").replace("{date}", fmtDate(row.consumption_from))}
                        </div>
                      ) : null}
                      {row.finished_on ? (
                        <div className="muted small">
                          {fl("loads.finished_on").replace("{date}", fmtDate(row.finished_on))}
                        </div>
                      ) : null}
                    </td>
                    <td>
                      {row.feed_item_label}
                      {/* Farm and load number ride under the feed name: two fewer columns, and the
                          check column stays on screen without a horizontal scroll. */}
                      <div className="muted small">
                        {fl("loads.load_line").replace("{farm}", row.farm_label).replace("{batch}", String(row.batch_no))}
                      </div>
                    </td>
                    <td>
                      <Tag tone={STATUS_TONE[row.status] ?? "mut"}>{fl(`loads.status.${row.status}`)}</Tag>
                    </td>
                    <td className="num">{kg(row.purchased_kg)}</td>
                    <td className="num">{kg(row.consumed_kg)}</td>
                    <td className="num" style={Number(row.left_kg) < 0 ? { color: "var(--danger)", fontWeight: 600 } : undefined}>
                      {kg(row.left_kg)}
                    </td>
                    <td className="num">
                      {row.days_said ?? (
                        <span className="muted" title={fl("loads.days_said.none")}>
                          {fl("loads.gap.none")}
                        </span>
                      )}
                    </td>
                    <td className="num">{row.days_consumed}</td>
                    <td className="num">
                      {row.days_left ?? (
                        <span className="muted" title={fl("loads.days_left.unknown")}>
                          {fl("loads.gap.none")}
                        </span>
                      )}
                    </td>
                    <td style={{ whiteSpace: "nowrap" }}>
                      <GapCell gap={row.gap_days} fl={fl} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <FeedPager
            pageContract={pageContract}
            offset={data.offset}
            limit={filters.limit}
            rowCount={rows.length}
            hasMore={hasMore}
            noun={fl("loads.pager.noun")}
            pageSizeOptions={filters.pageSizes}
            hrefForOffset={(next) => hrefWith(basePath, searchParams, { fl_offset: String(next) })}
            hrefForLimit={(next) => hrefWith(basePath, searchParams, { fl_limit: String(next), fl_offset: undefined })}
          />
        </>
      )}
    </section>
  );
}

// The check column: zero matches, above zero is short, below zero is spare.
// Absent means one side of the sum is unknown -- no figure stated, or nothing fed recently enough
// to project days left -- and reads as a dash, never as a pass.
function GapCell({ gap, fl }: { gap: number | null | undefined; fl: (key: string) => string }) {
  if (gap == null) return <span className="muted">{fl("loads.gap.none")}</span>;
  if (gap === 0) return <Tag tone="ok">{fl("loads.gap.zero")}</Tag>;
  if (gap > 0) return <Tag tone="dng">{fl("loads.gap.positive").replace("{days}", String(gap))}</Tag>;
  return <Tag tone="warn">{fl("loads.gap.negative").replace("{days}", String(-gap))}</Tag>;
}

function hrefWith(basePath: string, sp: RouteSearchParams | undefined, next: Record<string, string | undefined>): string {
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
