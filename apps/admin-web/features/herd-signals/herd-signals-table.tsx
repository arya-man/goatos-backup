"use client";

import { useState } from "react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import { operationalLocationLabel } from "@/lib/operational-location";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import {
  BATTERY_LABEL,
  BATTERY_TONE,
  MAPPING_LABEL,
  MAPPING_TONE,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  PATTERN_LABEL,
  PATTERN_TONE,
  SIGNAL_LABEL,
  SIGNAL_TONE,
  fmtAgo,
  fmtBatteryMv,
  fmtDelta,
  fmtRssi,
  fmtTagTemp,
} from "./format";
import { one } from "@/lib/search-params";
import { herdSignalsHref, type HerdSignalsParams } from "./params";
import { matchesResidualKpi } from "./herd-signals-row-filter";
import { HerdSignalsDrawer } from "./herd-signals-drawer";
import { HerdSignalsHistoryFullscreen } from "./herd-signals-history-fullscreen";

function rowTagId(item: HerdSignalItem): string {
  return item.tag_id;
}

const PAGE_SIZE_OPTIONS = [25, 50, 100];

export function HerdSignalsTable({
  items,
  nextCursor,
  params,
  nowMs,
  columns = "full",
}: {
  items: HerdSignalItem[];
  nextCursor: string | null;
  params: HerdSignalsParams;
  nowMs: number;
  columns?: "full" | "compact";
}) {
  const [sizeChanging, setSizeChanging] = useState(false);
  const visible = items.filter((item) => matchesResidualKpi(item, params.kpi));
  const drawerCloseHref = herdSignalsHref(params, {});
  const rowHref = (item: HerdSignalItem) => `${herdSignalsHref(params, { hs_tag: item.tag_id })}#hs-tag-${encodeURIComponent(item.tag_id)}`;

  if (items.length === 0) {
    return <HerdSignalsTableEmpty params={params} />;
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

  return (
    <>
      <div className="tblwrap">
        <table className="resp herd-signals-table">
          <thead>
            <tr>
              <th>Animal</th>
              <th>Smart tag</th>
              <th>Shed</th>
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
          </thead>
          <tbody>
            {visible.map((item) => {
              const location = item.operational_location_display
                ? item.operational_location_display
                : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
              return (
                <tr key={item.tag_id}>
                  <td data-l="Animal" className="animcell">
                    <LocalOverlayLink href={rowHref(item)} scroll={false} title="Open tag detail">
                      {item.display_id || item.goat_id || "Unmapped"}
                    </LocalOverlayLink>
                    <small>{item.mapping_state === "mapped" ? item.tag_id : "no animal identifier"}</small>
                  </td>
                  <td data-l="Smart tag" className="mono">{item.tag_id}</td>
                  <td data-l="Shed">{location || "—"}</td>
                  <td data-l="Gateway" className="mono">{item.gateway_id || "—"}</td>
                  <td data-l="Signal">
                    {item.signal_state ? (
                      <Tag tone={SIGNAL_TONE[item.signal_state]} title={fmtRssi(item.rssi_dbm)}>
                        {SIGNAL_LABEL[item.signal_state]}
                      </Tag>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td data-l="Motion count" className="num">{fmtDelta(item.motion_count)}</td>
                  <td data-l="15m delta" className="num delta">{fmtDelta(item.motion_delta)}</td>
                  <td data-l="1h delta" className="num delta">{fmtDelta(item.motion_delta_1h)}</td>
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
                    {item.battery_state ? (
                      <Tag tone={BATTERY_TONE[item.battery_state]} title={fmtBatteryMv(item.battery_mv)}>
                        {BATTERY_LABEL[item.battery_state]}
                      </Tag>
                    ) : (
                      "—"
                    )}
                  </td>
                  <td data-l="Tag temp" className="num" title="Tag housing temperature, not the animal's body temperature">
                    {fmtTagTemp(item.tag_temperature_c)}
                  </td>
                  <td data-l="Last seen">{fmtAgo(item.last_seen_at, nowMs)}</td>
                  <td data-l="Status">
                    <Tag tone={MAPPING_TONE[item.mapping_state]}>{MAPPING_LABEL[item.mapping_state]}</Tag>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div className="pager herd-signals-pager">
        <span className="muted">{visible.length} rows on this page</span>
        <span className="sp" style={{ flex: 1 }} />
        <span className="fsel">
          Rows
          <select
            value={params.limit}
            disabled={sizeChanging}
            onChange={(event) => {
              setSizeChanging(true);
              window.location.href = herdSignalsHref(params, { hs_limit: event.target.value });
            }}
          >
            {PAGE_SIZE_OPTIONS.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </span>
        {nextCursor ? (
          <Link href={herdSignalsHref(params, { hs_cursor: nextCursor })} className="pgbtn">
            Next page
          </Link>
        ) : (
          <button type="button" className="pgbtn" disabled>
            Next page
          </button>
        )}
      </div>

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

function HerdSignalsTableEmpty({ params }: { params: HerdSignalsParams }) {
  if (params.hasFilter) {
    return (
      <div className="empty">
        <div className="eicon">
          <svg className="ic" viewBox="0 0 24 24">
            <path d="M22 3H2l8 9.5V19l4 2v-8.5L22 3Z" />
          </svg>
        </div>
        <h4>No tags match these filters</h4>
        <p>The gateway is still receiving packets for this park. Clear filters to see the rest of the fleet.</p>
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
