import Table from "@mui/material/Table";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import { TableHeadCustom } from "@/components/app/table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
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
//
// Finished loads and retired feeds are not served at all (maintainer instruction 2026-09-22) -- the
// table answers what is in the store now, for the feeds the farm buys today -- and a load fed
// beyond its own kg reads as in use with its negative kg left standing, because it is still the
// load the store is drawing on.

const STATUS_TONE: Record<FeedAnalyticsStockLoadRow["status"], Tone> = {
  in_transit: "mut",
  not_started: "info",
  in_use: "ok",
  finished: "mut",
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
    <Card component="section" aria-label={fl("loads.title")}>
      <CardHeader title={fl("loads.title")} subheader={fl("loads.hint")} />

      <FeedFilters basePath={basePath} pageParam="fl_offset" fields={fields} pageContract={pageContract} />

      {data.total === 0 ? (
        <Typography variant="body2" sx={{ color: "text.secondary", px: 3, pb: 3 }}>
          {fl("loads.empty")}
        </Typography>
      ) : (
        <>
          <Box sx={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={fl("loads.title")}>
            {/* Ten columns: a tighter cell pad and a wrapping header keep the check column on screen. */}
            <Table size="small" sx={{ "& th, & td": { px: 1 }, "& th": { whiteSpace: "normal", lineHeight: 1.2 }, "& td": { fontVariantNumeric: "tabular-nums" } }}>
              <TableHeadCustom
                headCells={[
                  { id: "purchase_date", label: fl("loads.col.purchase_date") },
                  { id: "feed_item", label: fl("loads.col.feed_item") },
                  { id: "status", label: fl("loads.col.status") },
                  { id: "purchased_kg", label: fl("loads.col.purchased_kg"), align: "right" },
                  { id: "consumed_kg", label: fl("loads.col.consumed_kg"), align: "right" },
                  { id: "left_kg", label: fl("loads.col.left_kg"), align: "right" },
                  { id: "days_said", label: fl("loads.col.days_said"), align: "right" },
                  { id: "days_consumed", label: fl("loads.col.days_consumed"), align: "right" },
                  { id: "days_left", label: fl("loads.col.days_left"), align: "right" },
                  { id: "gap_days", label: fl("loads.col.gap_days") },
                ]}
              />
              <TableBody>
                {rows.map((row) => (
                  <TableRow key={row.feed_purchase_id} data-testid="feed-stock-load-row" data-gap={row.gap_days ?? ""}>
                    <TableCell>
                      {fmtDate(row.purchase_date)}
                      {/* The load's life in dates sits under the day it was bought, so the status
                          cell stays one chip wide and the check column stays on screen. */}
                      {row.consumption_from ? (
                        <Muted>{fl("loads.consumption_from").replace("{date}", fmtDate(row.consumption_from))}</Muted>
                      ) : null}
                      {row.finished_on ? (
                        <Muted>{fl("loads.finished_on").replace("{date}", fmtDate(row.finished_on))}</Muted>
                      ) : null}
                    </TableCell>
                    <TableCell>
                      {row.feed_item_label}
                      {/* Farm and load number ride under the feed name: two fewer columns, and the
                          check column stays on screen without a horizontal scroll. */}
                      <Muted>{fl("loads.load_line").replace("{farm}", row.farm_label).replace("{batch}", String(row.batch_no))}</Muted>
                    </TableCell>
                    <TableCell>
                      <Tag tone={STATUS_TONE[row.status] ?? "mut"}>{fl(`loads.status.${row.status}`)}</Tag>
                    </TableCell>
                    <TableCell align="right">{kg(row.purchased_kg)}</TableCell>
                    <TableCell align="right">{kg(row.consumed_kg)}</TableCell>
                    <TableCell align="right" sx={Number(row.left_kg) < 0 ? { color: "error.main", fontWeight: "fontWeightSemiBold" } : undefined}>
                      {kg(row.left_kg)}
                    </TableCell>
                    <TableCell align="right">
                      {row.days_said ?? (
                        <Box component="span" sx={{ color: "text.secondary" }} title={fl("loads.days_said.none")}>
                          {fl("loads.gap.none")}
                        </Box>
                      )}
                    </TableCell>
                    <TableCell align="right">{row.days_consumed}</TableCell>
                    <TableCell align="right">
                      {row.days_left ?? (
                        <Box component="span" sx={{ color: "text.secondary" }} title={fl("loads.days_left.unknown")}>
                          {fl("loads.gap.none")}
                        </Box>
                      )}
                    </TableCell>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      <GapCell gap={row.gap_days} fl={fl} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>

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
    </Card>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

// The check column: zero matches, above zero is short, below zero is spare.
// Absent means one side of the sum is unknown -- no figure stated, or nothing fed recently enough
// to project days left -- and reads as a dash, never as a pass.
function GapCell({ gap, fl }: { gap: number | null | undefined; fl: (key: string) => string }) {
  if (gap == null) return <Box component="span" sx={{ color: "text.secondary" }}>{fl("loads.gap.none")}</Box>;
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
