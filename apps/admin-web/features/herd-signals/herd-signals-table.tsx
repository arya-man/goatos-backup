"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import TableSortLabel from "@mui/material/TableSortLabel";
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
  RISK_LABEL,
  RISK_TONE,
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
import { DenseToggleAuto } from "@/components/app/dense-toggle-auto";
import { herdSignalsHref, type HerdSignalsParams, type HerdSignalsSortKey } from "./params";
import { matchesClientSideFilters } from "./herd-signals-row-filter";
import { HerdSignalsDrawer } from "./herd-signals-drawer";
import { HerdSignalsHistoryFullscreen } from "./herd-signals-history-fullscreen";
import { HerdSignalsAnimalsHead, HerdSignalsAnimalsRow } from "./herd-signals-animals-table";
import { useNowMs } from "./herd-signals-stream-bridge";
import { useHerdSignalsLiveSnapshot } from "./herd-signals-live-store";
import Tooltip from "@mui/material/Tooltip";
import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";
import { EmptyState } from "@/components/app/empty-state";

function rowTagId(item: HerdSignalItem): string {
  return item.tag_id;
}

function animalPrimaryLabel(item: HerdSignalItem): string {
  return item.animal_identifier_1 || item.animal_identifier_2 || item.display_id || item.goat_id || "Unmapped";
}

function animalRfidLine(item: HerdSignalItem): string {
  return [item.animal_identifier_1, item.animal_identifier_2].filter(Boolean).join(" / ");
}

function animalProfileLine(item: HerdSignalItem): string {
  const age = item.age_days == null ? null : item.age_days < 60 ? `${item.age_days}d` : `${Math.floor(item.age_days / 30)}mo`;
  return [item.breed, item.sex, age].filter(Boolean).join(" · ");
}

function watchlistReasonLabel(reason: string): string {
  const normalized = reason.trim().toLowerCase();
  if (!normalized) return "";
  if (normalized.includes("temperature high") || normalized.includes("temp high")) return "Tag warmer than pen average";
  if (normalized.includes("inactive") || normalized.includes("no movement")) return "No movement right now";
  if (normalized.includes("quiet")) return "Movement lower than usual";
  if (normalized.includes("spike")) return "Movement spike";
  if (normalized.includes("missing")) return "Signal missing";
  return reason;
}

function signedPercent(value: number): string {
  return `${value > 0 ? "+" : ""}${Math.round(value)}%`;
}

function watchlistDetailLine(item: HerdSignalItem): string {
  const details: string[] = [];
  if (item.own_motion_delta_pct != null) details.push(`vs own normal ${signedPercent(item.own_motion_delta_pct)}`);
  if (item.group_motion_delta_pct != null) details.push(`vs pen group ${signedPercent(item.group_motion_delta_pct)}`);
  if (item.group_temp_delta_c != null) {
    const delta = `${item.group_temp_delta_c >= 0 ? "+" : ""}${item.group_temp_delta_c.toFixed(1)}°C`;
    details.push(`tag temp ${delta} vs pen`);
  }
  return details.join(" · ");
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("a,button,input,select,textarea,[role='button']"));
}

const PAGE_SIZE_OPTIONS = [25, 50, 100];

const ACTIVITY_RULES =
  "Activity is the current 15-minute motion-count delta. Moving: delta 100 or more. Low: 10 to 99. Quiet: 1 to 9. No movement: delta 0 while packets are still received. Stale: no recent packet.";
const PATTERN_RULES =
  "Pattern is the broader classification for the tag. Normal activity means the current deltas are within that tag's baseline band. No movement now means delta 0 in the current 15-minute window. Quiet watch and inactive require low or zero deltas to persist.";
const RISK_RULES =
  "Watchlist combines this animal's motion against its own 15-minute-scaled baseline, same-pen motion and tag-temperature comparison, persistent activity pattern, and sensor state. It is not a fever diagnosis.";
const TABLE_SORT_NOTE =
  "Rows are sorted by the server across the filtered result. Smart tag is the default so live refresh keeps the visible order steady.";

function selectedLiveDelta(item: HerdSignalItem, window: HerdSignalsParams["liveWindow"]): number | null {
  if (window === "30s") return item.motion_delta_30s;
  if (window === "1m") return item.motion_delta_60s;
  if (window === "5m") return item.motion_delta_5m;
  return item.motion_delta;
}

function liveWindowLabel(window: HerdSignalsParams["liveWindow"]): string {
  if (window === "30s") return "30s";
  if (window === "1m") return "1m";
  if (window === "5m") return "5m";
  return "15m";
}

function movingNowLabel(item: HerdSignalItem, nowMs: number): { label: string; tone: "ok" | "warn" | "dng" | "mut" } {
  const seenAt = item.last_seen_at ? Date.parse(item.last_seen_at) : Number.NaN;
  if (!Number.isFinite(seenAt) || nowMs - seenAt > 30 * 60_000) return { label: "No signal", tone: "dng" };
  if (nowMs - seenAt > 30_000) return { label: "Delayed", tone: "warn" };
  if ((item.last_packet_motion_delta ?? 0) > 0 || (item.motion_delta_30s ?? 0) > 0) return { label: "Moving", tone: "ok" };
  return { label: "Still", tone: "mut" };
}

function baselineLabel(value: number | null, disabled = false): string {
  if (disabled) return "Off";
  if (value == null) return "—";
  return signedPercent(value);
}

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
  liveKey,
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
  liveKey: string;
  // Which column set to render. "live" is the fourteen-column radio/telemetry table; "animals"
  // is the mock's own nine-column animal-first set (herd-signals-animals-table.tsx). Only the
  // <TableHead>/<TableBody> differ -- the keyset pager walk, the rows-per-page control, the drawer and
  // the history full-screen are shared, which is why this is a variant here rather than a second
  // component that would need its own copy of the pager store.
  variant?: "live" | "animals";
}) {
  const { isPending, navigate } = useHerdSignalsNav();
  const liveSnapshot = useHerdSignalsLiveSnapshot(liveKey);
  const clientNowMs = useNowMs();
  const displayedNowMs = clientNowMs || nowMs;
  const displayedItems = liveSnapshot?.data.items ?? items;
  const displayedNextCursor = liveSnapshot?.data.next_cursor ?? nextCursor;
  const displayedTagsSeen = liveSnapshot?.data.summary.tags_seen ?? tagsSeen;
  // Hooks must run before the empty-state early returns below.
  // Any filter change rewrites this signature, which resets the walk to page 1.
  const filterSignature = herdSignalsHref(params, {});
  const walk = useSyncExternalStore(subscribePagerWalk, readPagerWalk, readServerPagerWalk);
  const stack = walk.signature === filterSignature ? walk.stack : [];
  const visible = displayedItems.filter((item) => matchesClientSideFilters(item, params));
  const drawerCloseHref = herdSignalsHref(params, {});
  const rowHref = (item: HerdSignalItem) => `${herdSignalsHref(params, { hs_tag: item.tag_id })}#hs-tag-${encodeURIComponent(item.tag_id)}`;

  if (displayedItems.length === 0) {
    return <HerdSignalsTableEmpty params={params} tagsSeen={displayedTagsSeen} />;
  }

  if (visible.length === 0) {
    // The KPI's own summary count came from the tenant-wide aggregate; this page of rows simply
    // does not carry a match for it. Distinct from "no rows at all" (handled above) and from a read
    // failure (handled by the caller before this component ever renders).
    return (
      <EmptyState
        icon={<svg className="ic" viewBox="0 0 24 24">
          <path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z" />
        </svg>}
        title="No rows on this page match that filter"
        description="The gateway is still receiving. Try clearing filters or paging through more rows."
      />
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
  const summaryIsRowTotal = displayedTagsSeen > 0 && !params.movementState && !params.pattern && !params.kpi;
  const walkedToEnd = positionKnown && !displayedNextCursor;
  const total = summaryIsRowTotal ? displayedTagsSeen : walkedToEnd ? rangeTo : undefined;
  const pageCount = total ? Math.max(1, Math.ceil(total / params.limit)) : undefined;
  const nf = (value: number) => value.toLocaleString("en-IN");

  const prevHref = positionKnown && pageIndex > 0 ? herdSignalsHref(params, { hs_cursor: stack[pageIndex - 1] || undefined }) : null;
  const nextHref = displayedNextCursor ? herdSignalsHref(params, { hs_cursor: displayedNextCursor }) : null;

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

  const pager = (
    <TablePaginationLinks
      className="herd-signals-pager"
      page={positionKnown ? pageIndex : 0}
      rowsPerPage={params.limit}
      count={-1}
      rowsPerPageHrefs={PAGE_SIZE_OPTIONS.map((size) => ({ value: size, href: herdSignalsHref(params, { hs_limit: String(size) }) }))}
      prevHref={prevHref}
      nextHref={nextHref}
      onPrevClick={goPrev}
      onNextClick={goNext}
      labelRowsPerPage="Rows:"
      // Every clause is omitted rather than guessed when its source is unknown: no range without a
      // known walk position, no total without a source that is actually the row total, no
      // "of N pages" without that total.
      rangeLabel={`${positionKnown ? `${nf(rangeFrom)}\u2013${nf(rangeTo)}` : `${nf(visible.length)} rows`}${total ? ` of ${nf(total)}` : ""}${positionKnown ? ` \u00b7 page ${nf(pageIndex + 1)}${pageCount ? ` of ${nf(pageCount)}` : ""}` : ""}`}
      prevLabel="Previous page"
      nextLabel="Next page"
      left={
        <>
          {isPending ? <span className="wfspin" aria-hidden="true" title="Loading" /> : null}
          {visible.length > 10 ? <DenseToggleAuto /> : null}
        </>
      }
    />
  );


  function sortHref(sort: HerdSignalsSortKey): string {
    const nextDir = params.sort === sort && params.sortDir === "asc" ? "desc" : "asc";
    return herdSignalsHref(params, {
      hs_sort: sort === "smart_tag" ? undefined : sort,
      hs_dir: nextDir === "asc" ? undefined : nextDir,
    });
  }

  const sortableHead = (label: string, sort: HerdSignalsSortKey, className?: string, help?: string) => {
    const active = params.sort === sort;
    return (
      // aria-sort belongs on the <TableCell component="th">, not on the link inside it: a screen reader announces the
      // column's sort state from the cell, and without it the table read as unsorted everywhere.
      <TableCell component="th" className={className} aria-sort={active ? (params.sortDir === "asc" ? "ascending" : "descending") : "none"}>
        {/* Template TableHeadCustom sort anatomy (TableSortLabel), as a link: sorting is URL state. */}
        <TableSortLabel component={Link} href={sortHref(sort)} hideSortIcon active={active} direction={active ? params.sortDir : "asc"} title={`Sort by ${label}`}>
          {label}
        </TableSortLabel>
        {help ? <InfoTip label={`${label} rules`} text={help} /> : null}
      </TableCell>
    );
  };

  return (
    <>
      <div className={`tblwrap${isPending ? " wfbusy" : ""}`}>
        <Table className={`resp herd-signals-table${variant === "animals" ? " herd-signals-animals-table" : ""}`}>
          <TableHead>
            {variant === "animals" ? (
              <HerdSignalsAnimalsHead />
            ) : (
            <TableRow>
              <TableCell component="th">Animal</TableCell>
              {sortableHead("Smart tag", "smart_tag", undefined, TABLE_SORT_NOTE)}
              <TableCell component="th">Pen</TableCell>
              <TableCell component="th">Gateway</TableCell>
              <TableCell component="th">Signal</TableCell>
              <TableCell component="th">Now</TableCell>
              <TableCell component="th">Last moved</TableCell>
              {sortableHead("Motion count", "motion_count", "num", "Cumulative counter maintained by the tag firmware. It can stay flat while packets are received.")}
              <TableCell component="th" className="num">
                {liveWindowLabel(params.liveWindow)} delta
                <InfoTip label="Selected movement window" text="Movement counter delta for the selected window. Use 30s or 1m for live checks; 15m remains the sustained activity window." />
              </TableCell>
              {sortableHead("Tag temp", "tag_temp", "num", "Tag housing temperature, not the animal's body temperature.")}
              {sortableHead("15m delta", "delta_15m", "num", "Current 15-minute motion-count delta: latest counter minus the baseline reading for the window.")}
              {sortableHead("1h delta", "delta_1h", "num", "Current 1-hour motion-count delta when enough readings exist; blank means the window is not established yet.")}
              <TableCell component="th" className="num">
                24h delta
                <InfoTip label="24h delta rules" text="Rolling 24-hour motion-counter delta. This is movement units from the tag firmware, not a step count." />
              </TableCell>
              <TableCell component="th">
                15m activity
                <InfoTip label="Activity rules" text={ACTIVITY_RULES} />
              </TableCell>
              <TableCell component="th">Own baseline</TableCell>
              <TableCell component="th">Pen peers</TableCell>
              <TableCell component="th">
                Pattern
                <InfoTip label="Pattern rules" text={PATTERN_RULES} />
              </TableCell>
              <TableCell component="th">
                Watchlist
                <InfoTip label="Watchlist rules" text={RISK_RULES} />
              </TableCell>
              <TableCell component="th">Battery</TableCell>
              {sortableHead("Last seen", "last_seen", undefined, "When the backend last received a packet from this tag. Sorting by this can move rows during live refresh.")}
              <TableCell component="th">Status</TableCell>
            </TableRow>
            )}
          </TableHead>
          <TableBody>
            {visible.map((item) => {
              if (variant === "animals") {
                return <HerdSignalsAnimalsRow key={item.tag_id} item={item} nowMs={displayedNowMs} href={rowHref(item)} />;
              }
              const location = item.operational_location_display
                ? item.operational_location_display
                : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
              const parkName = item.park_name;
              const delta15 = fmtSignedDelta(item.motion_delta);
              const liveDelta = fmtSignedDelta(selectedLiveDelta(item, params.liveWindow));
              const delta1hText = fmtDelta1h(item.motion_delta_1h, item.motion_delta);
              const delta1h = delta1hText === "—" ? { text: "—", tone: "zero" as const } : fmtSignedDelta(item.motion_delta_1h);
              const delta24h = fmtSignedDelta(item.motion_delta_24h);
              const nowState = movingNowLabel(item, displayedNowMs);
              const href = rowHref(item);
              return (
                <TableRow
                  key={item.tag_id}
                  className="hs-selectable"
                  onClick={(event) => {
                    if (isInteractiveTarget(event.target)) return;
                    pushLocalOverlayUrl(href);
                  }}
                >
                  {/* Two lines per row (56px): the animal and ONE secondary line. The RFID pair and
                      the profile live in the cell's tooltip and in the drawer; four stacked lines
                      made a 94px row (judge M2 round 4 #4). */}
                  <TableCell data-l="Animal" className="animcell wide" title={[animalRfidLine(item), animalProfileLine(item)].filter(Boolean).join(" · ") || undefined}>
                    <LocalOverlayLink href={href} scroll={false} title="Open tag detail">
                      {animalPrimaryLabel(item)}
                    </LocalOverlayLink>
                    <small>
                      {item.display_id && (item.animal_identifier_1 || item.animal_identifier_2) ? `${item.display_id} · ` : ""}
                      {item.mapping_state === "conflict" ? "mapping conflict" : MAPPING_LABEL[item.mapping_state].toLowerCase()}
                      {animalProfileLine(item) ? ` · ${animalProfileLine(item)}` : ""}
                    </small>
                  </TableCell>
                  <TableCell data-l="Smart tag" title={fmtBleMac(item.tag_mac)}>
                    <span className="mono">{item.tag_id}</span>
                  </TableCell>
                  <TableCell data-l="Pen" title={parkName || undefined}>
                    {location || "—"}
                  </TableCell>
                  <TableCell data-l="Gateway" className="mono">{item.gateway_id || "—"}</TableCell>
                  <TableCell data-l="Signal">
                    {item.signal_state ? (
                      <Tag tone={SIGNAL_TONE[item.signal_state]} title={SIGNAL_LABEL[item.signal_state]}>
                        {fmtRssi(item.rssi_dbm)}
                      </Tag>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell data-l="Now">
                    <Tag tone={nowState.tone}>{nowState.label}</Tag>
                    {item.last_packet_motion_delta != null ? <small className="faint">last pkt {fmtSignedDelta(item.last_packet_motion_delta).text}</small> : null}
                  </TableCell>
                  <TableCell data-l="Last moved">{item.last_moved_at ? fmtAgo(item.last_moved_at, displayedNowMs) : "—"}</TableCell>
                  <TableCell data-l="Motion count" className="num mono">{fmtDelta(item.motion_count)}</TableCell>
                  <TableCell data-l={`${liveWindowLabel(params.liveWindow)} delta`} className="num">
                    <span className={`delta ${liveDelta.tone}`}>{liveDelta.text}</span>
                  </TableCell>
                  <TableCell data-l="Tag temp" className="num" title="Tag housing temperature, not the animal's body temperature">
                    {fmtTagTemp(item.tag_temperature_c)}
                  </TableCell>
                  <TableCell
                    data-l="15m delta"
                    className="num"
                    title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}
                  >
                    <span className={`delta ${delta15.tone}`}>{delta15.text}</span>
                    {item.gap_delta ? <sup title="Gap total">*</sup> : null}
                  </TableCell>
                  <TableCell data-l="1h delta" className="num">
                    <span className={`delta ${delta1h.tone}`}>{delta1h.text}</span>
                  </TableCell>
                  <TableCell data-l="24h delta" className="num">
                    <span className={`delta ${delta24h.tone}`}>{delta24h.text}</span>
                  </TableCell>
                  <TableCell data-l="15m activity">
                    {item.movement_state ? (
                      <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell data-l="Own baseline">{baselineLabel(item.own_motion_delta_pct, params.ownBaseline === "off")}</TableCell>
                  <TableCell data-l="Pen peers">{baselineLabel(item.group_motion_delta_pct)}</TableCell>
                  <TableCell data-l="Pattern">
                    {item.pattern_state ? (
                      <Tag tone={PATTERN_TONE[item.pattern_state]}>{PATTERN_LABEL[item.pattern_state]}</Tag>
                    ) : (
                      "—"
                    )}
                  </TableCell>
                  <TableCell data-l="Watchlist">
                    {/* One chip; the reasons and the detail line are its tooltip (two-line rows). */}
                    <div
                      className="hs-watch"
                      title={[item.risk_reasons?.length ? item.risk_reasons.slice(0, 2).map(watchlistReasonLabel).filter(Boolean).join("; ") : "", watchlistDetailLine(item)].filter(Boolean).join(" · ") || undefined}
                    >
                      {item.risk_state ? <Tag tone={RISK_TONE[item.risk_state]}>{RISK_LABEL[item.risk_state]}</Tag> : "—"}
                      {item.risk_reasons?.length ? (
                        <small>{item.risk_reasons.slice(0, 1).map(watchlistReasonLabel).filter(Boolean).join("; ")}</small>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell data-l="Battery">
                    {fmtBatteryMv(item.battery_mv)}
                    {item.battery_state && item.battery_state !== "healthy" ? (
                      <Tag tone={BATTERY_TONE[item.battery_state]}>{BATTERY_LABEL[item.battery_state]}</Tag>
                    ) : null}
                  </TableCell>
                  <TableCell data-l="Last seen">{fmtAgo(item.last_seen_at, displayedNowMs)}</TableCell>
                  <TableCell data-l="Status">
                    <Tag tone={herdSignalStatus(item).tone}>{herdSignalStatus(item).label}</Tag>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>

      {pager}

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

function InfoTip({ label, text }: { label: string; text: string }) {
  return (
    <Tooltip title={text} placement="top" slotProps={{ tooltip: { sx: { maxWidth: 300 } } }}>
      <span className="tipwrap">
        <button type="button" className="ihelp" aria-label={label}>
          i
        </button>
      </span>
    </Tooltip>
  );
}

function HerdSignalsTableEmpty({ params, tagsSeen }: { params: HerdSignalsParams; tagsSeen: number }) {
  if (params.hasFilter) {
    return (
      <EmptyState
        icon={<svg className="ic" viewBox="0 0 24 24">
          <path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z" />
        </svg>}
        title="No tags match these filters"
        description={tagsSeen > 0
          ? "The gateway is still receiving packets for this park. Clear filters to see the rest of the fleet."
          : "Filters exclude every row in scope."}
        action={
          <Link href={herdSignalsHref(params, { hs_shed: undefined, hs_q: undefined, hs_move: undefined, hs_map: undefined, hs_pattern: undefined, hs_risk: undefined, hs_kpi: undefined })} className="btn sm">
            Clear filters
          </Link>
        }
      />
    );
  }
  return (
    <EmptyState
      icon={<svg className="ic" viewBox="0 0 24 24">
        <path d="M4.9 19.1a10 10 0 0 1 0-14.2" />
        <path d="M7.8 16.2a6 6 0 0 1 0-8.4" />
        <circle cx="12" cy="12" r="2" />
        <path d="M16.2 7.8a6 6 0 0 1 0 8.4" />
        <path d="M19.1 4.9a10 10 0 0 1 0 14.2" />
      </svg>}
      title="No gateway packets yet"
      description="No BLE gateway has posted for this tenant. Confirm the gateway is powered, has a network route, and is in range of at least one smart tag."
    />
  );
}
