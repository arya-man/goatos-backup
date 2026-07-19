import { redirect } from "next/navigation";
import { AlertTriangle, Warehouse } from "lucide-react";

import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedPackingWorklist,
  type FeedPackingRow,
  type FeedPackingWorklistPage,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import { FeedQuantityCell, FeedWorkflowTag, isBlockedItem } from "./feed-quantity";
import { feedHref, feedLimit, feedOffset, resolveFeedScope } from "./feed-scope";

// Feed -> Feed Packing. The same generated day as Feed Direction, collapsed to the line a packer
// actually works from: one row per shed per session, with the shed's ration grains already summed,
// because a packer fills one bag per feed item per shed rather than one per grain.
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
// KPI cards and the store draw come from `summary`, which is WHOLE-SCOPE (`scope === "filtered"`)
// and invariant to limit/offset. They are never computed from the visible rows: a page subtotal
// presented as the day's truth is what sends a packer out with a fraction of the load.

const PAGE_PATH = "/feed/packing";
const DEFAULT_PAGE_SIZE = 10;

function itemLineCount(row: FeedPackingRow): number {
  return Math.max(1, row.items.length);
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
  const scope = resolveFeedScope(sp, "fp_park", "fp_date", locations.parks);

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

  const cols = tableLabels(pageContract, "packing-worklist");

  // The worklist endpoint takes only park and day — it has no shed or session parameter — so those
  // filters are not rendered. A control that cannot narrow the backend result would be decoration.
  const filterFields: FeedFilterField[] = [
    {
      kind: "date",
      param: "fp_date",
      label: copy(pageContract, "filter.date_label"),
      value: scope.targetDate,
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

        {summary ? (
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

        {summary ? (
          <div className="note" style={{ marginBottom: 16 }}>
            {copy(pageContract, "section.summary.note")}
          </div>
        ) : null}

        {/* The store draw for the WHOLE filtered worklist. Each item carries its own blocked-cell
            count, so a column is never read as complete when part of it could not be resolved. */}
        {summary && summary.total_kg_by_feed_item.length > 0 ? (
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
                  const span = itemLineCount(row);
                  const rowKey = `${row.shed_id}|${row.session_no}`;
                  const items = row.items.length > 0 ? row.items : [null];

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
                              <span style={{ fontWeight: 650 }}>{row.shed_label}</span>
                              <FeedWorkflowTag workflow={row.workflow} pageContract={pageContract} />
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
                      ) : (
                        <>
                          <td className="muted">{copy(pageContract, "label.placeholder")}</td>
                          <td className="muted">{copy(pageContract, "label.placeholder")}</td>
                        </>
                      )}

                      {index === 0 ? (
                        <td rowSpan={span}>
                          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
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
      </section>

      <div className="note">{copy(pageContract, "section.packing.note")}</div>
    </div>
  );
}
