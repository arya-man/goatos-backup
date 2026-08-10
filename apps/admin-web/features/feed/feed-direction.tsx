import { redirect } from "next/navigation";
import { AlertTriangle, Warehouse } from "lucide-react";

import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedDirectionPreview,
  listFeedConfigSessionTemplates,
  type FeedDirectionPreviewPage,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedLifecycleBanner, isLifecycleEmpty } from "./feed-lifecycle";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import {
  FeedItemStatusTag,
  FeedOverdueShiftingChip,
  FeedQuantityCell,
  FeedWorkflowTag,
} from "./feed-quantity";
import { isNothingToFeed, visibleOperationalFeedItems } from "./feed-quantity-state";
import { feedHref, feedLimit, feedOffset, resolveFeedScope } from "./feed-scope";

// Feed -> Feed Direction. The generated feed sheet for ONE park and ONE Asia/Kolkata business day:
// projected head count x authored grams per head x shed factor, split across the park's sessions.
//
// Three things on this screen are easy to get wrong and expensive when wrong:
//
//  1. BLOCKED IS NOT ZERO. Handled once, in feed-quantity.tsx — read the header there before
//     touching any quantity rendering. A blocked cell has no number and must never acquire one.
//     This screen also HIDES configured-zero item lines (`visibleOperationalFeedItems`): a line
//     reading "0.000 kg RGS Concentrate" is an instruction to do nothing, printed among the
//     instructions to do something. Blocked lines are never hidden — the filter keys off the
//     `configured_zero` class, not off "there is no number to show", which is the mistake that would
//     take blocked with it. The omission is disclosed under the table so an absent line can only be
//     read as a deliberate zero.
//
//  2. THE HEAD COUNT IS A PROJECTION, NOT A CENSUS. It is the live herd PLUS approved movements that
//     are already feed-effective for the selected day. It is not how many animals are standing in the
//     shed right now. The column is therefore labelled as projected, and where the projection has
//     drifted from reality (an approved movement whose feed-effective date passed with the animals
//     still not moved) the row carries the overdue-movement chip instead of quietly absorbing it.
//
//  3. EXPERIMENT SHEDS DO NOT MULTIPLY BY HEAD COUNT. An experiment row's quantity is hand-entered
//     absolute kg for the whole shed, and `head_count_informational` says so. Presenting that head
//     count the same way as a normal row's invites someone to multiply it and overfeed the shed by a
//     factor of its whole population.
//
// Pagination is by SHED, so a shed's sessions never straddle a page boundary and every
// `session_total_kg` on screen is complete.

const PAGE_PATH = "/feed/direction";
const DEFAULT_PAGE_SIZE = 10;

/**
 * One table line per VISIBLE (row, feed item). Shed-level cells span the shed's visible item lines.
 *
 * Counted from the visible items, not `row.items` — spanning the unfiltered count would leave the
 * shed/breed/session cells stretching over rows that are no longer rendered, pulling the table apart.
 */
function itemLineCount(visibleItems: readonly unknown[]): number {
  return Math.max(1, visibleItems.length);
}

export async function FeedDirectionPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};

  const locations = await getCensusLocations();
  const scope = resolveFeedScope(sp, "fd_park", "fd_date", locations.parks);
  const shedId = (sp.fd_shed as string | undefined) || "";
  const sessionRaw = (sp.fd_session as string | undefined) || "";

  const pageSizeOptions = tablePageSizes(pageContract, "direction-rows");
  const limit = feedLimit(sp, "fd_limit", pageSizeOptions, DEFAULT_PAGE_SIZE);
  const offset = feedOffset(sp, "fd_offset");

  // The park's own session template supplies the session filter vocabulary. Sourcing it from the
  // returned rows instead would collapse the dropdown to whatever is already selected — pick Morning
  // and Evening disappears, leaving no way back.
  const [previewResult, sessionTemplates] = await Promise.all([
    scope.parkId
      ? getFeedDirectionPreview({
          park_id: scope.parkId,
          target_date: scope.targetDate,
          shed_id: shedId || undefined,
          session: sessionRaw ? Number(sessionRaw) : undefined,
          limit,
          offset,
        })
      : Promise.resolve(null),
    scope.parkId ? listFeedConfigSessionTemplates({ park_id: scope.parkId, limit: 50 }) : Promise.resolve(null),
  ]);

  const authError = firstAuthRequiredError(previewResult, sessionTemplates);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const preview: FeedDirectionPreviewPage | null =
    previewResult && previewResult.ok ? previewResult.data : null;
  const rows = preview?.items ?? [];
  const summary = preview?.summary;
  const lifecycle = preview?.lifecycle;
  const hasFilter = Boolean(shedId || sessionRaw);
  // A served day whose empty table is EXPLAINED by the lifecycle (nothing was issued) shows the
  // banner AS the content instead of an empty grid. A filter that merely excludes rows from a sheet
  // that DOES exist keeps the normal "no rows match these filters" state, so the filter guard stands.
  const lifecycleEmpty = lifecycle ? isLifecycleEmpty(lifecycle, rows.length) && !hasFilter : false;

  const cols = tableLabels(pageContract, "direction-rows");

  const filterFields: FeedFilterField[] = [
    {
      kind: "date",
      param: "fd_date",
      label: copy(pageContract, "filter.date_label"),
      value: scope.targetDate,
      // Bound to [today, tomorrow]: beyond that the projected counts are unknown, so the backend
      // refuses to generate a sheet. Preventing the pick is the honest, no-fabrication guard.
      min: scope.minDate,
      max: scope.maxDate,
    },
    {
      kind: "select",
      param: "fd_park",
      label: copy(pageContract, "filter.park_label"),
      value: scope.parkId,
      // A park is mandatory for this endpoint, so there is no "All" option. Disabled (not hidden)
      // when the top bar already owns park scope: the mock's rule is disable-with-reason, and hiding
      // it would make the control appear to come and go.
      allowAll: false,
      disabledReason: scope.parkLockedByTopBar ? copy(pageContract, "filter.scope_readonly") : undefined,
      options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
    },
    {
      kind: "select",
      param: "fd_shed",
      label: copy(pageContract, "filter.shed_label"),
      value: shedId,
      options: locations.sheds
        .filter((shed) => !scope.parkId || shed.parentId === scope.parkId)
        .map((shed) => ({ value: shed.id, label: shed.name })),
    },
    {
      kind: "select",
      param: "fd_session",
      label: copy(pageContract, "filter.session_label"),
      value: sessionRaw,
      options: (sessionTemplates && sessionTemplates.ok ? sessionTemplates.data.items : []).map((template) => ({
        value: String(template.session_no),
        label: template.session_label,
      })),
    },
  ];

  return (
    <div className="screen on">
      <FeedFaroView
        routeId={pageContract.route_id}
        parkId={scope.parkId}
        targetDate={scope.targetDate}
        blockedCells={summary?.blocked_count}
      />

      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.direction.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* An API failure surfaces as a visible error band. Swallowing it into an empty table would
          read to an operator as "nothing to feed today", which is the worst possible misreading. */}
      {previewResult && !previewResult.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "state.direction_unavailable")}</b>
            <div className="small muted">
              {previewResult.error.code ?? previewResult.error.kind}&nbsp;{previewResult.error.message}
            </div>
          </div>
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.direction.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.direction.caption")}</span>
        </div>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fd_offset"
          fields={filterFields}
          pageContract={pageContract}
        />

        {/* The issue -> amend -> lock status of the served park-day. For a not-yet-issued day this
            banner IS the content: the summary/table below are suppressed so the operator sees the
            explanation, not a blank grid that reads as "nothing to feed". */}
        {lifecycle ? (
          <FeedLifecycleBanner lifecycle={lifecycle} feedDay={scope.targetDate} pageContract={pageContract} />
        ) : null}

        {/* Always rendered. The API summary is WHOLE-SCOPE (`summary.scope === "filtered"`) and
            invariant to limit/offset, so these figures are the day's real totals on every page —
            they no longer need the first-page-only guard that used to hide a page subtotal. */}
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
              <span className="acc" style={{ background: summary.blocked_count > 0 ? "var(--danger)" : "var(--teal)" }} />
              <div className="lab">{copy(pageContract, "kpi.blocked.label")}</div>
              <div className="val" style={summary.blocked_count > 0 ? { color: "var(--danger)" } : undefined}>
                {summary.blocked_count}
              </div>
              <div className="dl">
                <span className="muted">
                  {summary.blocked_count > 0
                    ? copy(pageContract, "kpi.blocked.sub")
                    : copy(pageContract, "empty.blocked")}
                </span>
              </div>
              <AlertTriangle className="ic kpiic" aria-hidden="true" />
            </div>
          </div>
        ) : null}

        {/* Per-item day totals for the WHOLE filtered scope, exactly as the API reports them. Each
            carries its own blocked-cell count: a column total is never read as complete when part of
            it is missing, and a blocked cell is absent from the sum rather than added as zero. */}
        {/* The coverage claim the backend copy makes. It is rendered next to the figures rather than
            buried in a tooltip because the whole point of the whole-scope rewrite is that an
            operator can trust these numbers as the day's totals on any page. */}
        {summary && !lifecycleEmpty ? (
          <div className="note" style={{ marginBottom: 16 }}>
            {copy(pageContract, "section.summary.note")}
          </div>
        ) : null}

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
          aria-label={copy(pageContract, "section.direction.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.direction.aria")}>
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
                      {!previewResult || previewResult.ok
                        ? hasFilter
                          ? copy(pageContract, "empty.direction_filtered")
                          : copy(pageContract, "empty.direction")
                        : copy(pageContract, "state.direction_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.flatMap((row) => {
                  // Configured zeros drop out here. Blocked items are NOT touched by this filter.
                  const visibleItems = visibleOperationalFeedItems(row.items);
                  // Every item this shed has was authored at 0 — a real, explainable state that must
                  // render as a stated "nothing to feed", never as an empty hole in the table.
                  const nothingToFeed = isNothingToFeed(row.items);
                  const span = itemLineCount(visibleItems);
                  // The row's real identity: one operational location, one session, one workflow.
                  // Keyed on the descriptive columns instead (`ration_group|breed`), two partitions of
                  // one shed holding the same mix produced the SAME React key, and those columns are
                  // now backend-joined summaries of a pen rather than the thing that separates rows.
                  const rowKey = `${row.shed_id}|${row.partition_label ?? ""}|${row.workflow}|${row.session_no}`;
                  const items = visibleItems.length > 0 ? visibleItems : [null];

                  return items.map((item, index) => (
                    <tr key={`${rowKey}|${item ? item.feed_item : "none"}`}>
                      {index === 0 ? (
                        <>
                          {/* No park cell: this endpoint is single-park by contract (park_id is
                              required), so the value is constant for the whole page and is named
                              once by the Park filter above. See the contract comment in
                              adminui/app/service.go pages(). */}
                          <td rowSpan={span}>
                            <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                              <span style={{ fontWeight: 650 }}>{row.operational_location_display || row.shed_label}</span>
                              <span style={{ display: "inline-flex", gap: 5, flexWrap: "wrap" }}>
                                <FeedWorkflowTag workflow={row.workflow} pageContract={pageContract} />
                                {row.overdue_pending ? <FeedOverdueShiftingChip pageContract={pageContract} /> : null}
                              </span>
                            </div>
                          </td>
                          {/* Always the ANIMALS' management stage, on every workflow — the experiment
                              arm has its own column now. Multi-stage sheds arrive pre-joined. */}
                          <td className="muted" rowSpan={span}>
                            {row.shed_tag || copy(pageContract, "label.placeholder")}
                          </td>
                          {/* Breed only. The ration group used to print as a sub-line here, but on a
                              feed sheet it is derivable noise: the operator reads the breed at the
                              shed door, and the group is an internal lookup key that is empty on
                              every experiment row anyway. It stays visible on /feed/config, which is
                              where the breed → group merge is actually authored. A multi-breed shed
                              arrives already joined into one label by the backend; the frontend does
                              not decide how a mixed shed is named. */}
                          <td rowSpan={span}>{row.breed || copy(pageContract, "label.placeholder")}</td>
                          <td className="muted" rowSpan={span}>
                            {row.session_label}
                          </td>
                          {/* Projected, not census. `head_count_informational` marks the experiment
                              case, whose kg is already a shed total and must not be multiplied. */}
                          <td rowSpan={span}>
                            <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                              <span
                                style={{ fontWeight: 700, fontVariantNumeric: "tabular-nums" }}
                                title={copy(pageContract, "label.projected_count_note")}
                              >
                                {row.head_count}
                              </span>
                              <span className="muted" style={{ fontSize: 11 }}>
                                {row.head_count_informational
                                  ? copy(pageContract, "label.workflow_experiment")
                                  : copy(pageContract, "label.projected_count")}
                              </span>
                            </div>
                          </td>
                        </>
                      ) : null}

                      {item ? (
                        <>
                          {/* `feed-wrap` is layout only (frontend-owned): the feed item is a long
                              multi-word LABEL, and holding it on one line is what pushed the
                              session total off the right edge of the card. It word-wraps; it is
                              never broken mid-token. */}
                          <td className="feed-wrap">{item.feed_item}</td>
                          <td>
                            <FeedQuantityCell item={item} pageContract={pageContract} />
                          </td>
                        </>
                      ) : nothingToFeed ? (
                        /* Every item was a configured zero. Say so across the item + quantity columns
                           rather than leaving two placeholder dashes, which would read as missing
                           data — the one meaning this shed's state is NOT. */
                        <td className="muted feed-wrap" colSpan={2}>
                          {copy(pageContract, "empty.nothing_to_feed")}
                        </td>
                      ) : (
                        <>
                          <td className="muted feed-wrap">{copy(pageContract, "label.placeholder")}</td>
                          <td className="muted">{copy(pageContract, "label.placeholder")}</td>
                        </>
                      )}

                      {index === 0 ? (
                        <td className="feed-total" rowSpan={span} style={{ textAlign: "right" }}>
                          {/* Sum of the RESOLVED items only. When the row is blocked this total is
                              partial by construction, and saying so is the whole point — the number
                              is not what the shed needs, it is what we know how to give it. */}
                          <div style={{ display: "flex", flexDirection: "column", gap: 4, alignItems: "flex-end" }}>
                            <span
                              style={{ fontWeight: 700, fontVariantNumeric: "tabular-nums", color: "var(--brand-d)" }}
                              title={copy(pageContract, "label.session_split_note")}
                            >
                              {row.session_total_kg} {copy(pageContract, "label.kg_noun")}
                            </span>
                            {row.blocked ? (
                              // One row now covers a whole pen, so "blocked" alone no longer says WHICH
                              // part of it is unauthored — the backend names each gap and they are shown
                              // here. The operator feeds what IS configured; this is how they see what
                              // is missing and where to close it. Backend-owned wording, joined only.
                              <span
                                className="tag t-dng"
                                title={
                                  row.blocked_reasons?.length
                                    ? row.blocked_reasons.map((reason) => reason.detail).join("\n")
                                    : copy(pageContract, "label.blocked_note")
                                }
                              >
                                {copy(pageContract, "label.blocked_short")}
                              </span>
                            ) : null}
                          </div>
                        </td>
                      ) : null}

                      <td>
                        {item ? (
                          <FeedItemStatusTag item={item} pageContract={pageContract} />
                        ) : (
                          <span className="muted">{copy(pageContract, "label.placeholder")}</span>
                        )}
                      </td>
                    </tr>
                  ));
                })
              )}
            </tbody>
          </table>
        </div>

        {/* The omission is disclosed once, under the table it applies to — not per row. Without it a
            reader who expects an item and cannot find it has no way to tell "authored as 0" from
            "dropped", and those have opposite consequences. */}
        <div className="note" style={{ marginTop: 12 }}>
          {copy(pageContract, "label.zero_items_omitted")}
        </div>

        <FeedPager
          pageContract={pageContract}
          offset={offset}
          limit={limit}
          rowCount={rows.length}
          hasMore={preview?.has_more ?? false}
          noun={copy(pageContract, "table.direction.noun")}
          pageSizeOptions={pageSizeOptions}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fd_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fd_limit", String(next))}
        />
        </>
        ) : null}
      </section>

      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.direction.note")}</div>
      <div className="note">{copy(pageContract, "label.formula")}</div>
    </div>
  );
}
