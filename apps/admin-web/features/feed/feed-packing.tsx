import { redirect } from "next/navigation";
import { AlertTriangle, Warehouse } from "lucide-react";

import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedPackingWorklist,
  type FeedPackingWorklistPage,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import type { RouteSearchParams } from "@/lib/search-params";
import { fmtDate } from "@/lib/format";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedLifecycleBanner, isLifecycleEmpty } from "./feed-lifecycle";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import { FeedQuantityCell, FeedWorkflowTag, isBlockedItem } from "./feed-quantity";
import { isNothingToFeed, visibleOperationalFeedItems } from "./feed-quantity-state";
import { feedHref, feedLimit, feedOffset, resolveFeedPackingScope } from "./feed-scope";

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
    <div className="screen on">
      <FeedFaroView
        routeId={pageContract.route_id}
        parkId={scope.parkId}
        targetDate={scope.targetDate}
        blockedCells={blockedCellsOnPage}
      />

      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.packing.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {worklistResult && !worklistResult.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "state.packing_unavailable")}</b>
            <div className="small muted">
              {worklistResult.error.code ?? worklistResult.error.kind}&nbsp;{worklistResult.error.message}
            </div>
          </div>
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.packing.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.packing.caption")}</span>
        </div>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fp_offset"
          fields={filterFields}
          pageContract={pageContract}
        />

        {/* The packing day is the picker's axis; this states the FEED day it is for (packing day + 1),
            so the operator reads "packed today, for tomorrow" without doing the arithmetic. The template
            is backend-owned copy; only the date is client-formatted. */}
        <div className="note" style={{ marginBottom: 16, fontWeight: 600 }}>
          {copy(pageContract, "caption.feed_for").replace("{date}", fmtDate(scope.targetDate))}
        </div>

        {/* Issue -> amend -> lock status of the served park-day. For a not-yet-issued day the banner
            IS the content — the KPIs/worklist below are suppressed rather than showing an empty bar. */}
        {lifecycle ? (
          <FeedLifecycleBanner lifecycle={lifecycle} feedDay={scope.targetDate} pageContract={pageContract} />
        ) : null}

        {summary && !lifecycleEmpty ? (
          <div className="grid g2" aria-label={copy(pageContract, "section.summary.aria")}>
            <div className="kpi">
              <span className="acc" style={{ background: "var(--brand)" }} />
              <div className="lab">{copy(pageContract, "kpi.sheds.label")}</div>
              <div className="val">{summary.shed_count}</div>
              <div className="dl">
                <span className="muted">{copy(pageContract, "kpi.sheds.sub")}</span>
              </div>
              <Warehouse className="ic kpiic" aria-hidden="true" />
            </div>
            <div className="kpi">
              <span
                className="acc"
                style={{ background: summary.blocked_shed_count > 0 ? "var(--danger)" : "var(--teal)" }}
              />
              <div className="lab">{copy(pageContract, "kpi.blocked.label")}</div>
              <div className="val" style={summary.blocked_shed_count > 0 ? { color: "var(--danger)" } : undefined}>
                {/* The label is "Blocked sheds", so this is the SHED count, not the cell count. */}
                {summary.blocked_shed_count}
              </div>
              <div className="dl">
                <span className="muted">
                  {summary.blocked_shed_count > 0
                    ? copy(pageContract, "kpi.blocked.sub")
                    : copy(pageContract, "empty.blocked")}
                </span>
              </div>
              <AlertTriangle className="ic kpiic" aria-hidden="true" />
            </div>
          </div>
        ) : null}

        {summary && !lifecycleEmpty ? (
          <div className="note" style={{ marginBottom: 16 }}>
            {copy(pageContract, "section.summary.note")}
          </div>
        ) : null}

        {/* The store draw for the WHOLE filtered worklist. Each item carries its own blocked-cell
            count, so a column is never read as complete when part of it could not be resolved. */}
        {summary && !lifecycleEmpty && summary.total_kg_by_feed_item.length > 0 ? (
          <div className="bd" style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {summary.total_kg_by_feed_item.map((total) => (
              <span
                key={total.feed_item}
                className={total.blocked_cells > 0 ? "tag t-warn" : "tag t-mut"}
                title={total.blocked_cells > 0 ? copy(pageContract, "label.blocked_note") : undefined}
              >
                {total.feed_item} · {total.quantity_kg} {copy(pageContract, "label.kg_noun")}
              </span>
            ))}
          </div>
        ) : null}

        {!lifecycleEmpty ? (
        <>
        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.packing.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.packing.aria")}>
            <thead>
              <tr>
                {cols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!worklistResult || worklistResult.ok
                        ? copy(pageContract, "empty.packing")
                        : copy(pageContract, "state.packing_unavailable")}
                    </div>
                  </td>
                </tr>
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
                    <tr key={`${rowKey}|${item ? item.feed_item : "none"}`}>
                      {index === 0 ? (
                        <>
                          {/* No park sub-line under the shed. The worklist endpoint requires
                              park_id, so it was the same park name repeated under every shed —
                              a whole extra line per shed group for zero information. The pinned
                              park is named by the Park filter above. */}
                          <td rowSpan={span}>
                            <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                              <span style={{ fontWeight: 650 }}>{row.operational_location_display || row.shed_label}</span>
                              <FeedWorkflowTag workflow={row.workflow} pageContract={pageContract} />
                              {/* No experiment arm here. A packer's unit of work is the bag: the
                                  Experiment tag already says this shed's quantity is hand-authored
                                  rather than per-head, which is the only part that changes how they
                                  pack. The arm names the trial the shed is enrolled in — authoring
                                  context, shown where it is authored, on /feed/config. */}
                            </div>
                          </td>
                          <td className="muted" rowSpan={span}>
                            {row.session_label}
                          </td>
                        </>
                      ) : null}

                      {item ? (
                        <>
                          <td>{item.feed_item}</td>
                          {/* Expected kg is derived from the day's direction, never entered here.
                              Blocked renders as the gap, not as a packable zero. */}
                          <td title={copy(pageContract, "label.expected_kg_note")}>
                            <FeedQuantityCell item={item} pageContract={pageContract} />
                          </td>
                        </>
                      ) : nothingToFeed ? (
                        /* Every item authored at 0 — stated, not left as two blank dashes that would
                           read as missing data. */
                        <td className="muted" colSpan={2}>
                          {copy(pageContract, "empty.nothing_to_feed")}
                        </td>
                      ) : (
                        <>
                          <td className="muted">{copy(pageContract, "label.placeholder")}</td>
                          <td className="muted">{copy(pageContract, "label.placeholder")}</td>
                        </>
                      )}

                      {index === 0 ? (
                        <td rowSpan={span}>
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
                              {row.total_kg} {copy(pageContract, "label.kg_noun")}
                            </span>
                          </div>
                        </td>
                      ) : null}
                    </tr>
                  ));
                })
              )}
            </tbody>
          </table>
        </div>

        {/* Disclosed once, under the table it applies to. */}
        <div className="note" style={{ marginTop: 12 }}>
          {copy(pageContract, "label.zero_items_omitted")}
        </div>

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
      </section>

      <div className="note">{copy(pageContract, "section.packing.note")}</div>
    </div>
  );
}
