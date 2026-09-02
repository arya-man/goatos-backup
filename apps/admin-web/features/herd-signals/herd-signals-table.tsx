"use client";

import { useSyncExternalStore, type MouseEvent } from "react";
import { LocalOverlayLink, pushLocalOverlayUrl } from "@/components/local-overlay-link";
import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { operationalLocationLabel } from "@/lib/operational-location";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import {
  BATTERY_LABEL,
  BATTERY_TONE,
  MAPPING_LABEL,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  PATTERN_LABEL,
  PATTERN_TONE,
  SIGNAL_LABEL,
  fmtBleMac,
  SIGNAL_TONE,
  fmtAgo,
  fmtBatteryMv,
  fmtDelta,
  fmtDelta1h,
  fmtRssi,
  fmtSignedDelta,
  fmtTagTemp,
  herdSignalStatus,
} from "./format";
import { one } from "@/lib/search-params";
import { herdSignalsHref, type HerdSignalsParams } from "./params";
import { matchesResidualKpi } from "./herd-signals-row-filter";
import { HerdSignalsDrawer } from "./herd-signals-drawer";
import { HerdSignalsHistoryFullscreen } from "./herd-signals-history-fullscreen";
import { HerdSignalsAnimalsHead, HerdSignalsAnimalsRow } from "./herd-signals-animals-table";

function rowTagId(item: HerdSignalItem): string {
  return item.tag_id;
}

function animalPrimaryLabel(item: HerdSignalItem): string {
  return item.animal_identifier_1 || item.animal_identifier_2 || item.display_id || item.goat_id || "Unmapped";
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("a,button,input,select,textarea,[role='button']"));
}

const PAGE_SIZE_OPTIONS = [25, 50, 100];

// Keyset pagination carries no server-side page index, so the position readout is derived from the
// cursors this page has actually walked through -- never guessed from a cursor string. `stack` holds
// the cursor of every page BEFORE the current one (the first entry is "" for the uncursored first
// page), so stack.length is the current zero-based page index and popping it is a real "Previous".
//
// This lives in a module store rather than component state because every navigation on this screen
// re-renders the server component behind a Suspense boundary, which unmounts the table -- component
// state would be wiped on the very click that needs to record it. Read through
// useSyncExternalStore so the server render and the hydrating client render agree (both see the
// empty walk), instead of reading a mutable module value straight out of render.
type PagerWalk = { signature: string; stack: string[] };
const EMPTY_WALK: PagerWalk = { signature: "", stack: [] };
let pagerWalk: PagerWalk = EMPTY_WALK;
const pagerWalkListeners = new Set<() => void>();

function subscribePagerWalk(onChange: () => void): () => void {
  pagerWalkListeners.add(onChange);
  return () => {
    pagerWalkListeners.delete(onChange);
  };
}

function readPagerWalk(): PagerWalk {
  return pagerWalk;
}

function readServerPagerWalk(): PagerWalk {
  return EMPTY_WALK;
}

function writePagerWalk(next: PagerWalk): void {
  pagerWalk = next;
  for (const listener of pagerWalkListeners) listener();
}

export function HerdSignalsTable({
  items,
  nextCursor,
  params,
  nowMs,
  tagsSeen,
  variant = "live",
}: {
  items: HerdSignalItem[];
  nextCursor: string | null;
  params: HerdSignalsParams;
  nowMs: number;
  // The tenant-wide summary.tags_seen, NOT items.length — needed so the filtered-to-nothing empty
  // state can honestly say whether the gateway is receiving anything at all in scope, rather than
  // asserting reception the page has not actually evidenced.
  tagsSeen: number;
  // Which column set to render. "live" is the fourteen-column radio/telemetry table; "animals"
  // is the mock's own nine-column animal-first set (herd-signals-animals-table.tsx). Only the
  // <thead>/<tbody> differ -- the keyset pager walk, the rows-per-page control, the drawer and
  // the history full-screen are shared, which is why this is a variant here rather than a second
  // component that would need its own copy of the pager store.
  variant?: "live" | "animals";
}) {
  const { isPending, navigate } = useHerdSignalsNav();
  // Hooks must run before the empty-state early returns below.
  // Any filter change rewrites this signature, which resets the walk to page 1.
  const filterSignature = herdSignalsHref(params, {});
  const walk = useSyncExternalStore(subscribePagerWalk, readPagerWalk, readServerPagerWalk);
  const stack = walk.signature === filterSignature ? walk.stack : [];
  const visible = items.filter((item) => matchesResidualKpi(item, params.kpi));
  const drawerCloseHref = herdSignalsHref(params, {});
  const rowHref = (item: HerdSignalItem) => `${herdSignalsHref(params, { hs_tag: item.tag_id })}#hs-tag-${encodeURIComponent(item.tag_id)}`;

  if (items.length === 0) {
    return <HerdSignalsTableEmpty params={params} tagsSeen={tagsSeen} />;
  }

  if (visible.length === 0) {
    // The KPI's own summary count came from the tenant-wide aggregate; this page of rows simply
    // does not carry a match for it. Distinct from "no rows at all" (handled above) and from a read
    // failure (handled by the caller before this component ever renders).
    return (
      <div className="empty">
        <div className="eicon">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z" />
          </svg>
        </div>
        <h4>No rows on this page match that filter</h4>
        <p>The gateway is still receiving. Try clearing filters or paging through more rows.</p>
      </div>
    );
  }

  // A cold load on a ?hs_cursor= URL has no walk behind it, so the page index is genuinely unknown
  // and is left out rather than invented.
  const positionKnown = Boolean(params.cursor) === (stack.length > 0);
  const pageIndex = stack.length;
  const rangeFrom = pageIndex * params.limit + 1;
  const rangeTo = rangeFrom + visible.length - 1;
  // Total row count for the readout. Keyset pagination gives none, so there are exactly two sources
  // that are TRUE, and no total is shown when neither applies:
  //
  //  1. summary.tags_seen -- the tenant-scoped aggregate the KPI strip already reports, never a
  //     count of the fetched page. The backend narrows it by shed/mapping/search but NOT by
  //     movement state, pattern, or the client-side KPI filter (verified against the running API:
  //     hs_move=moving returns 1 row while tags_seen stays 20), so it is only the row total when
  //     none of those three are active.
  //  2. The end of a walk -- once we are on the last page (no next cursor) with a known position,
  //     rangeTo IS the exact total, whatever the filters were.
  const summaryIsRowTotal = tagsSeen > 0 && !params.movementState && !params.pattern && !params.kpi;
  const walkedToEnd = positionKnown && !nextCursor;
  const total = summaryIsRowTotal ? tagsSeen : walkedToEnd ? rangeTo : undefined;
  const pageCount = total ? Math.max(1, Math.ceil(total / params.limit)) : undefined;
  const nf = (value: number) => value.toLocaleString("en-IN");

  const prevHref = positionKnown && pageIndex > 0 ? herdSignalsHref(params, { hs_cursor: stack[pageIndex - 1] || undefined }) : null;
  const nextHref = nextCursor ? herdSignalsHref(params, { hs_cursor: nextCursor }) : null;

  function plainClick(event: MouseEvent): boolean {
    return !(event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey);
  }

  function goPrev(event: MouseEvent) {
    if (!prevHref || !plainClick(event)) return;
    event.preventDefault();
    writePagerWalk({ signature: filterSignature, stack: stack.slice(0, -1) });
    navigate(prevHref);
  }

  function goNext(event: MouseEvent) {
    if (!nextHref || !plainClick(event)) return;
    event.preventDefault();
    writePagerWalk({ signature: filterSignature, stack: [...stack, params.cursor ?? ""] });
    navigate(nextHref);
  }

  const pager = (variant: "top" | "bottom") => (
    <div className={`pager herd-signals-pager${variant === "top" ? " pager-top" : ""}`} aria-busy={isPending}>
      {isPending ? <span className="wfspin" aria-hidden="true" title="Loading" /> : null}
      {prevHref ? (
        <Link href={prevHref} className="pgbtn" onClick={goPrev}>
          &larr; Previous
        </Link>
      ) : (
        <button type="button" className="pgbtn" disabled>
          &larr; Previous
        </button>
      )}
      {nextHref ? (
        <Link href={nextHref} className="pgbtn" onClick={goNext}>
          Next &rarr;
        </Link>
      ) : (
        <button type="button" className="pgbtn" disabled>
          Next &rarr;
        </button>
      )}
      {/* Every clause here is omitted rather than guessed when its source is unknown: no range
          without a known walk position, no total without a source that is actually the row total,
          no "of N pages" without that total. */}
      <span>
        Showing <b>{positionKnown ? `${nf(rangeFrom)}\u2013${nf(rangeTo)}` : nf(visible.length)}</b>
        {positionKnown ? null : " rows"}
        {total ? (
          <>
            {" of "}
            <b>{nf(total)}</b>
          </>
        ) : null}
        {positionKnown ? (
          <>
            {" \u00b7 page "}
            <b>{nf(pageIndex + 1)}</b>
            {pageCount ? (
              <>
                {" of "}
                <b>{nf(pageCount)}</b>
              </>
            ) : null}
          </>
        ) : null}
      </span>
      <span className="sp" style={{ flex: 1 }} />
      <span className="fsel">
        Rows
        <select
          value={params.limit}
          onChange={(event) => navigate(herdSignalsHref(params, { hs_limit: event.target.value }))}
        >
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </select>
      </span>
    </div>
  );

  return (
    <>
      {pager("top")}
      <div className={`tblwrap${isPending ? " wfbusy" : ""}`}>
        <table className={`resp herd-signals-table${variant === "animals" ? " herd-signals-animals-table" : ""}`}>
          <thead>
            {variant === "animals" ? (
              <HerdSignalsAnimalsHead />
            ) : (
            <tr>
              <th>Animal</th>
              <th>Smart tag</th>
              <th>Pen</th>
              <th>Gateway</th>
              <th>Signal</th>
              <th className="num">Motion count</th>
              <th className="num">15m delta</th>
              <th className="num">1h delta</th>
              <th>Activity</th>
              <th>Pattern</th>
              <th>Battery</th>
              <th className="num">Tag temp</th>
              <th>Last seen</th>
              <th>Status</th>
            </tr>
            )}
          </thead>
          <tbody>
            {visible.map((item) => {
              if (variant === "animals") {
                return <HerdSignalsAnimalsRow key={item.tag_id} item={item} nowMs={nowMs} href={rowHref(item)} />;
              }
              const location = item.operational_location_display
                ? item.operational_location_display
                : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
              const parkName = item.park_name;
              const delta15 = fmtSignedDelta(item.motion_delta);
              const delta1hText = fmtDelta1h(item.motion_delta_1h, item.motion_delta);
              const delta1h = delta1hText === "—" ? { text: "—", tone: "zero" as const } : fmtSignedDelta(item.motion_delta_1h);
              const href = rowHref(item);
              return (
                <tr
                  key={item.tag_id}
                  className="hs-selectable"
                  tabIndex={0}
                  role="button"
                  aria-label={`Open tag detail for ${animalPrimaryLabel(item)}`}
                  onClick={(event) => {
                    if (isInteractiveTarget(event.target)) return;
                    pushLocalOverlayUrl(href);
                  }}
                  onKeyDown={(event) => {
                    if (event.key !== "Enter" && event.key !== " ") return;
                    if (isInteractiveTarget(event.target)) return;
                    event.preventDefault();
                    pushLocalOverlayUrl(href);
                  }}
                >
                  <td data-l="Animal" className="animcell wide">
                    <LocalOverlayLink
                      href={href}
                      scroll={false}
                      className="hs-row-hit"
                      title="Open tag detail"
                      aria-label={`Open tag detail for ${animalPrimaryLabel(item)}`}
                    />
                    <LocalOverlayLink href={href} scroll={false} title="Open tag detail">
                      {animalPrimaryLabel(item)}
                    </LocalOverlayLink>
                    <small>
                      {item.display_id && (item.animal_identifier_1 || item.animal_identifier_2) ? `${item.display_id} · ` : ""}
                      {item.mapping_state === "conflict" ? "mapping conflict" : MAPPING_LABEL[item.mapping_state].toLowerCase()}
                    </small>
                  </td>
                  <td data-l="Smart tag">
                    <span className="mono">{item.tag_id}</span>
                    <br />
                    <span className="mono faint">{fmtBleMac(item.tag_mac)}</span>
                  </td>
                  <td data-l="Pen">
                    {location || "—"}
                    {parkName ? (
                      <>
                        <br />
                        <span className="faint small">{parkName}</span>
                      </>
                    ) : null}
                  </td>
                  <td data-l="Gateway" className="mono">{item.gateway_id || "—"}</td>
                  <td data-l="Signal">
                    {item.signal_state ? (
                      <Tag tone={SIGNAL_TONE[item.signal_state]} title={SIGNAL_LABEL[item.signal_state]}>
                        {fmtRssi(item.rssi_dbm)}
                      </Tag>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td data-l="Motion count" className="num mono">{fmtDelta(item.motion_count)}</td>
                  <td
                    data-l="15m delta"
                    className="num"
                    title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}
                  >
                    <span className={`delta ${delta15.tone}`}>{delta15.text}</span>
                    {item.gap_delta ? <sup title="Gap total">*</sup> : null}
                  </td>
                  <td data-l="1h delta" className="num">
                    <span className={`delta ${delta1h.tone}`}>{delta1h.text}</span>
                  </td>
                  <td data-l="Activity">
                    {item.movement_state ? (
                      <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td data-l="Pattern">
                    {item.pattern_state ? (
                      <Tag tone={PATTERN_TONE[item.pattern_state]}>{PATTERN_LABEL[item.pattern_state]}</Tag>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td data-l="Battery">
                    {fmtBatteryMv(item.battery_mv)}
                    {item.battery_state && item.battery_state !== "healthy" ? (
                      <Tag tone={BATTERY_TONE[item.battery_state]}>{BATTERY_LABEL[item.battery_state]}</Tag>
                    ) : null}
                  </td>
                  <td data-l="Tag temp" className="num" title="Tag housing temperature, not the animal's body temperature">
                    {fmtTagTemp(item.tag_temperature_c)}
                  </td>
                  <td data-l="Last seen">{fmtAgo(item.last_seen_at, nowMs)}</td>
                  <td data-l="Status">
                    <Tag tone={herdSignalStatus(item).tone}>{herdSignalStatus(item).label}</Tag>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {pager("bottom")}

      <HerdSignalsDrawer
        rows={visible}
        rowId={rowTagId}
        initialSelectedId={one(params.sp, "hs_tag")}
        closeHref={drawerCloseHref}
      />
      <HerdSignalsHistoryFullscreen rows={visible} closeHref={drawerCloseHref} />
    </>
  );
}

function HerdSignalsTableEmpty({ params, tagsSeen }: { params: HerdSignalsParams; tagsSeen: number }) {
  if (params.hasFilter) {
    return (
      <div className="empty">
        <div className="eicon">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z" />
          </svg>
        </div>
        <h4>No tags match these filters</h4>
        {/* Conditioned on the tenant-wide summary, the one thing this page has actually measured —
            never asserted as a fact this branch (items.length === 0) has no evidence for. */}
        <p>
          {tagsSeen > 0
            ? "The gateway is still receiving packets for this park. Clear filters to see the rest of the fleet."
            : "Filters exclude every row in scope."}
        </p>
        <div className="eact">
          <Link href={herdSignalsHref(params, { hs_shed: undefined, hs_q: undefined, hs_move: undefined, hs_map: undefined, hs_pattern: undefined, hs_kpi: undefined })} className="btn sm">
            Clear filters
          </Link>
        </div>
      </div>
    );
  }
  return (
    <div className="empty">
      <div className="eicon">
        <svg className="ic" viewBox="0 0 24 24">
          <path d="M4.9 19.1a10 10 0 0 1 0-14.2" />
          <path d="M7.8 16.2a6 6 0 0 1 0-8.4" />
          <circle cx="12" cy="12" r="2" />
          <path d="M16.2 7.8a6 6 0 0 1 0 8.4" />
          <path d="M19.1 4.9a10 10 0 0 1 0 14.2" />
        </svg>
      </div>
      <h4>No gateway packets yet</h4>
      <p>No BLE gateway has posted for this tenant. Confirm the gateway is powered, has a network route, and is in range of at least one smart tag.</p>
    </div>
  );
}
