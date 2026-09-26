import Box from "@mui/material/Box";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { redirect } from "next/navigation";
import { AlertTriangle, Fence, Package } from "lucide-react";

import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { InfoHint } from "@/components/app/info-hint";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedPackingWorklist,
  type FeedPackingWorklistPage,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDate, fmtKg } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedLifecycleBanner, isLifecycleEmpty } from "./feed-lifecycle";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import { FeedQuantityCell, FeedWorkflowTag, isBlockedItem } from "./feed-quantity";
import { isNothingToFeed, visibleOperationalFeedItems } from "./feed-quantity-state";
import { feedHref, feedLimit, feedOffset, resolveFeedPackingScope } from "./feed-scope";
import Alert from "@mui/material/Alert";
import fp from "./feed-packing.module.css";

// Feed -> Feed Packing. The same generated day as Feed Direction, collapsed to the line a packer
// actually works from: one group per pen per session, with the pen's ration grains already summed,
// because a packer fills one bag per feed item per pen rather than one per grain.
//
// ONE ROW PER PEN PER SESSION, which is what the backend serves again (maintainer decision
// 2026-08-11, reverting the 2026-08-10 pen-day row). Between those dates the backend nested the
// sessions inside a pen-day row and this table flattened them straight back out; the flattening is
// gone because a row IS a session line once more.
//
// READ-ONLY, and deliberately so. Nothing on this screen is recorded: there is no proof capture, no
// video, and no stored packing state. `status` is DERIVED from the generation result, not stored.
//
// The blocked-vs-zero contract carries over unchanged from the preview and is what makes `status`
// meaningful:
//   ready   — every item resolved.
//   blocked — at least one item has no authored ration. The line must NOT be packed from the
//             resolved remainder: doing that sends the shed out short while the sheet looks complete.
//   empty   — the shed holds no projected animals. That is not a configuration gap and must never be
//             shown as one.
//
// Configured-zero lines are HIDDEN here (`visibleOperationalFeedItems`): nobody needs a line telling
// them to weigh out 0.000 kg of RGS Concentrate. Blocked lines are never hidden — the filter keys off
// the `configured_zero` class rather than off "no number to show", which is what would sweep blocked
// up with it and send a packer out believing a shed with no authored ration was complete. The
// omission is disclosed under the table.
//
// KPI cards and the store draw come from `summary`, which is WHOLE-SCOPE (`scope === "filtered"`)
// and invariant to limit/offset. They are never computed from the visible rows: a page subtotal
// presented as the day's truth is what sends a packer out with a fraction of the load.

const PAGE_PATH = "/feed/packing";
const DEFAULT_PAGE_SIZE = 10;

/** Spans are counted from the VISIBLE items — see the twin note in feed-direction.tsx. */
function itemLineCount(visibleItems: readonly unknown[]): number {
  return Math.max(1, visibleItems.length);
}

function statusTone(status: string): string {
  if (status === "blocked") return "tag t-dng";
  if (status === "empty") return "tag t-mut";
  return "tag t-ok";
}

export async function FeedPackingPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};

  const locations = await getCensusLocations();
  // Feed Packing browses by the PACKING day (defaults to today, back up to 30 days). The backend is
  // asked for feed day = packing day + 1; the caption states that feed day so the axis relabel is clear.
  const scope = resolveFeedPackingScope(sp, "fp_park", "fp_date", locations.parks);

  const pageSizeOptions = tablePageSizes(pageContract, "packing-worklist");
  const limit = feedLimit(sp, "fp_limit", pageSizeOptions, DEFAULT_PAGE_SIZE);
  const offset = feedOffset(sp, "fp_offset");

  const worklistResult = scope.parkId
    ? await getFeedPackingWorklist({
        park_id: scope.parkId,
        target_date: scope.targetDate,
        limit,
        offset,
      })
    : null;

  const authError = firstAuthRequiredError(worklistResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const worklist: FeedPackingWorklistPage | null =
    worklistResult && worklistResult.ok ? worklistResult.data : null;
  const rows = worklist?.items ?? [];
  const summary = worklist?.summary;
  const lifecycle = worklist?.lifecycle;
  // This page sends only park and day, so an empty result is never a filter exclusion — an empty
  // page here is always the lifecycle's own "nothing was issued" state.
  const lifecycleEmpty = lifecycle ? isLifecycleEmpty(lifecycle, rows.length) : false;

  const cols = tableLabels(pageContract, "packing-worklist");

  // Only park and day are rendered. The endpoint has no shed parameter at all, and while it does
  // accept `session`, this sheet deliberately does not offer it: the web packing worklist is printed
  // and read down in one pass, and hiding half the day's bags from it would understate what the crew
  // must carry out. The phone, which captures one bag at a time, is where the session matters.
  const filterFields: FeedFilterField[] = [
    {
      kind: "date",
      param: "fp_date",
      label: copy(pageContract, "filter.date_label"),
      // The picker value/bounds are the PACKING day: default today, capped at today, back 30 days.
      value: scope.packingDay,
      min: scope.minDate,
      max: scope.maxDate,
    },
    {
      kind: "select",
      param: "fp_park",
      label: copy(pageContract, "filter.park_label"),
      value: scope.parkId,
      allowAll: false,
      disabledReason: scope.parkLockedByTopBar ? copy(pageContract, "filter.scope_readonly") : undefined,
      options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
    },
  ];

  // A row is ONE pen-session, so its own items are the cells. A pen's morning and evening arrive as
  // two rows and are both counted here, which is what a packer's cell count means.
  const blockedCellsOnPage = rows.reduce(
    (total, row) => total + row.items.filter((item) => isBlockedItem(item)).length,
    0,
  );

  return (
    <div className="kit-enter screen on feed-packing-page">
      <FeedFaroView
        routeId={pageContract.route_id}
        parkId={scope.parkId}
        targetDate={scope.targetDate}
        blockedCells={blockedCellsOnPage}
      />

      <div>
        <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "section.packing.title") }]} />
      </div>

      {worklistResult && !worklistResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 16 }}><div>
            <b>{copy(pageContract, "state.packing_unavailable")}</b>
            <div className="small muted">
              {worklistResult.error.code ?? worklistResult.error.kind}&nbsp;{worklistResult.error.message}
            </div>
          </div>
        </Alert>
      ) : null}

      <div>
      {/* The page CSS zeroes this shell's padding; on phones it flattens into the page so the
          filter/status cluster does not read as a card inside a card. */}
      <Card className="feed-packing-shell" sx={{ overflow: "visible", "@media (max-width:640px)": { mb: 1.75, p: 0, borderRadius: 0, bgcolor: "transparent", boxShadow: "none" } }}>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fp_offset"
          fields={filterFields}
          pageContract={pageContract}
        />

        {/* The packing day is the picker's axis; this states the FEED day it is for (packing day + 1),
            so the operator reads "packed today, for tomorrow" without doing the arithmetic. The template
            is backend-owned copy; only the date is client-formatted. */}
        <div className="note feed-packing-for">
          {copy(pageContract, "caption.feed_for").replace("{date}", fmtDate(scope.targetDate))}
        </div>

        {/* Issue -> amend -> lock status of the served park-day. For a not-yet-issued day the banner
            IS the content — the KPIs/worklist below are suppressed rather than showing an empty bar. */}
        {lifecycle ? (
          <div className={fp.lifecycle}>
            <FeedLifecycleBanner lifecycle={lifecycle} feedDay={scope.targetDate} pageContract={pageContract} />
          </div>
        ) : null}
      </Card>
      </div>

      <div>
        {summary && !lifecycleEmpty ? (
          <KpiGrid min={220} className="feed-packing-kpis">
            <KpiCard tone="primary" icon={<Fence size={22} />} label={copy(pageContract, "kpi.sheds.label")} value={summary.shed_count} hint={copy(pageContract, "kpi.sheds.sub")} />
            {/* The label is "Blocked sheds", so this is the SHED count, not the cell count. */}
            <KpiCard
              tone={summary.blocked_shed_count > 0 ? "error" : "info"}
              icon={<AlertTriangle size={22} />}
              label={copy(pageContract, "kpi.blocked.label")}
              value={summary.blocked_shed_count}
              hint={summary.blocked_shed_count > 0 ? copy(pageContract, "kpi.blocked.sub") : copy(pageContract, "empty.blocked")}
            />
          </KpiGrid>
        ) : null}

      </div>

      <div>
      <Card className="feed-packing-worklist">
        {/* Worklist header: 24px inset over a hairline on desktop; on phones a two-column
            grid of title block + info glyph with a tighter inset and no hairline. */}
        <CardHeader
          title={copy(pageContract, "section.packing.title")}
          subheader={copy(pageContract, "section.packing.caption")}
          action={summary && !lifecycleEmpty ? <InfoHint text={copy(pageContract, "section.summary.note")} size={28} /> : null}
          sx={{
            alignItems: "flex-start",
            columnGap: 1.5,
            pt: { xs: 2.5, sm: 3 },
            px: { xs: 2.25, sm: 3 },
            pb: { xs: 1.5, sm: 2 },
            borderBottom: { xs: 0, sm: 1 },
            borderColor: { sm: "divider" },
            [`& .${cardHeaderClasses.content}`]: { minWidth: 0 },
            [`& .${cardHeaderClasses.action}`]: { m: 0, pt: { xs: 0.25, sm: 0 } },
          }}
        />
        {/* The store draw for the WHOLE filtered worklist. Each item carries its own blocked-cell
            count, so a column is never read as complete when part of it could not be resolved. */}
        {summary && !lifecycleEmpty && summary.total_kg_by_feed_item.length > 0 ? (
          <div className="bd" style={{ display: "flex", flexWrap: "wrap", gap: 8, paddingBottom: 16 }}>
            {summary.total_kg_by_feed_item.map((total) => (
              <span
                key={total.feed_item}
                className={total.blocked_cells > 0 ? "tag t-warn" : "tag t-mut"}
                title={total.blocked_cells > 0 ? copy(pageContract, "label.blocked_note") : undefined}
              >
                {total.feed_item} · {fmtKg(total.quantity_kg)} {copy(pageContract, "label.kg_noun")}
              </span>
            ))}
          </div>
        ) : null}

        {lifecycleEmpty ? (
          <div className="bd">
            {/* One glyph + one line: the card's own caption already names the grain above. */}
            <EmptyState title={copy(pageContract, "empty.packing")} icon={<Package className="ic" />} />
          </div>
        ) : null}

        {!lifecycleEmpty ? (
        <>
        <div
          className="bd tablewrap feed-stock-tablewrap feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.packing.aria")}
        >
          <Table className="feed-table" aria-label={copy(pageContract, "table.packing.aria")}>
            <TableHead>
              <TableRow>
                {cols.map((col) => (
                  <TableCell component="th" key={col}>{col}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!worklistResult || worklistResult.ok
                        ? copy(pageContract, "empty.packing")
                        : copy(pageContract, "state.packing_unavailable")}
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                rows.flatMap((row) => {
                  // Configured zeros drop out here. Blocked items are NOT touched by this filter.
                  const visibleItems = visibleOperationalFeedItems(row.items);
                  const nothingToFeed = isNothingToFeed(row.items);
                  const span = itemLineCount(visibleItems);
                  // Full line identity: the pen AND its session. Keying on the pen alone would give
                  // a pen's morning and evening the same React key.
                  const rowKey = `${row.shed_id}|${row.partition_label ?? ""}|${row.session_no}`;
                  const items = visibleItems.length > 0 ? visibleItems : [null];

                  return items.map((item, index) => (
                    <TableRow key={`${rowKey}|${item ? item.feed_item : "none"}`}>
                      {index === 0 ? (
                        <>
                          {/* No park sub-line under the shed. The worklist endpoint requires
                              park_id, so it was the same park name repeated under every shed —
                              a whole extra line per shed group for zero information. The pinned
                              park is named by the Park filter above. */}
                          <TableCell rowSpan={span}>
                            <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                              <span style={{ fontWeight: 650 }}>
                                {row.operational_location_display || operationalLocationLabel({ shedName: row.shed_label, partitionLabel: row.partition_label })}
                              </span>
                              <FeedWorkflowTag workflow={row.workflow} pageContract={pageContract} />
                              {/* No experiment arm here. A packer's unit of work is the bag: the
                                  Experiment tag already says this shed's quantity is hand-authored
                                  rather than per-head, which is the only part that changes how they
                                  pack. The arm names the trial the shed is enrolled in — authoring
                                  context, shown where it is authored, on /feed/config. */}
                            </div>
                          </TableCell>
                          <TableCell className="muted" rowSpan={span}>
                            {row.session_label}
                          </TableCell>
                        </>
                      ) : null}

                      {item ? (
                        <>
                          <TableCell>{item.feed_item}</TableCell>
                          {/* Expected kg is derived from the day's direction, never entered here.
                              Blocked renders as the gap, not as a packable zero. */}
                          <TableCell title={copy(pageContract, "label.expected_kg_note")}>
                            <FeedQuantityCell item={item} pageContract={pageContract} />
                          </TableCell>
                        </>
                      ) : nothingToFeed ? (
                        /* Every item authored at 0 — stated, not left as two blank dashes that would
                           read as missing data. */
                        <TableCell className="muted" colSpan={2}>
                          {copy(pageContract, "empty.nothing_to_feed")}
                        </TableCell>
                      ) : (
                        <>
                          <TableCell className="muted">{copy(pageContract, "label.placeholder")}</TableCell>
                          <TableCell className="muted">{copy(pageContract, "label.placeholder")}</TableCell>
                        </>
                      )}

                      {index === 0 ? (
                        <TableCell rowSpan={span}>
                          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                            {/* This line's own status and total. A row IS one pen-session, so a
                                perfectly packable morning stays OK even when the same pen's evening
                                is short, and each row prints its own bag's weight rather than the
                                day's. */}
                            <span
                              className={statusTone(row.status)}
                              title={copy(
                                pageContract,
                                row.status === "blocked" ? "label.blocked_note" : "label.ok_note",
                              )}
                            >
                              {row.status === "blocked"
                                ? copy(pageContract, "label.blocked")
                                : copy(pageContract, "label.ok")}
                            </span>
                            <span
                              className="muted"
                              style={{ fontSize: 11, fontVariantNumeric: "tabular-nums" }}
                              title={copy(pageContract, "label.expected_kg_note")}
                            >
                              {fmtKg(row.total_kg)} {copy(pageContract, "label.kg_noun")}
                            </span>
                          </div>
                        </TableCell>
                      ) : null}
                    </TableRow>
                  ));
                })
              )}
            </TableBody>
          </Table>
        </div>

        {/* Disclosed once, under the table it applies to. */}
        <Box
          sx={{
            display: "flex",
            justifyContent: "flex-end",
            m: 0,
            px: 3,
            pb: 2,
            "@media (max-width: 640px)": { px: 2.5, pb: 1.75 },
          }}
        >
          <InfoHint text={copy(pageContract, "label.zero_items_omitted")} />
        </Box>

        <FeedPager
          pageContract={pageContract}
          offset={offset}
          limit={limit}
          rowCount={rows.length}
          hasMore={worklist?.has_more ?? false}
          noun={copy(pageContract, "table.packing.noun")}
          pageSizeOptions={pageSizeOptions}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fp_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fp_limit", String(next))}
        />
        </>
        ) : null}
      </Card>
      </div>

    </div>
  );
}
