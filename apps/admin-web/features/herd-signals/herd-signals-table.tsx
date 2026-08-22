import { LocalOverlayLink } from "@/components/local-overlay-link";
import Link from "@/components/no-prefetch-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtBatteryVoltage, fmtCount, fmtMotionDelta, fmtRssi, fmtStaleness, fmtTagTemp } from "./format";
import type { HerdSignalsItem } from "./types";
import { herdSignalsHref, type HerdSignalsParams } from "./params";

const COLUMNS = [
  "Animal",
  "Smart tag",
  "Shed",
  "Gateway",
  "Signal",
  "Motion count",
  "15m delta",
  "1h delta",
  "Activity",
  "Pattern",
  "Battery",
  "Tag temp",
  "Last seen",
  "Status",
];

function matchesLocalFilter(item: HerdSignalsItem, filter: string | undefined): boolean {
  if (!filter) return true;
  switch (filter) {
    case "weak_signal":
      return item.signal_state === "weak";
    case "missing_signal":
      return item.signal_state === "stale" || item.signal_state === "missing";
    case "low_battery":
      return item.battery_state === "low" || item.battery_state === "critical";
    case "sensor_abnormal":
      return item.sensor_state === "abnormal";
    default:
      return true;
  }
}

export function HerdSignalsTable({
  items,
  nextCursor,
  params,
  pageContract,
  nowMs,
}: {
  items: HerdSignalsItem[];
  nextCursor: string | null;
  params: HerdSignalsParams;
  pageContract: AdminUiPageContract;
  nowMs: number;
}) {
  const filteredItems = items.filter((item) => matchesLocalFilter(item, params.localFilter));

  if (items.length === 0) {
    return <HerdSignalsTableEmpty params={params} pageContract={pageContract} />;
  }
  if (params.localFilter && filteredItems.length === 0) {
    return (
      <div className="hs-empty">
        <p>{copy(pageContract, "table.filtered_to_nothing", "No rows on this page match this filter. Try Refresh or fetch another page.")}</p>
        <Link href={herdSignalsHref(params, { hs_local: undefined })}>{copy(pageContract, "filters.clear", "Clear filters")}</Link>
      </div>
    );
  }

  return (
    <div className="hs-tablewrap">
      <HerdSignalsPagination params={params} position="top" itemCount={filteredItems.length} nextCursor={nextCursor} />
      {params.localFilter ? (
        <p className="hs-local-caveat">
          {copy(
            pageContract,
            "table.local_filter_caveat",
            "This filter narrows only the rows already fetched on this page — the API does not yet support server-side filtering by this signal. KPI counts above still reflect the whole herd.",
          )}
        </p>
      ) : null}
      <table className="hs-table" role="table">
        <thead>
          <tr>
            {COLUMNS.map((label) => (
              <th key={label}>{label}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {filteredItems.map((item) => (
            <tr key={item.tag_id} data-row-card>
              <td data-label="Animal">
                <LocalOverlayLink href={herdSignalsHref(params, {}) + `#hs_tag=${encodeURIComponent(item.tag_id)}`}>
                  {item.display_id ?? item.goat_id ?? "Unmapped"}
                </LocalOverlayLink>
              </td>
              <td data-label="Smart tag" className="mono">
                {item.tag_mac}
              </td>
              <td data-label="Shed">{item.operational_location_display ?? item.shed_name ?? "—"}</td>
              <td data-label="Gateway">{item.gateway_id ?? "—"}</td>
              <td data-label="Signal">
                <span className={`hs-badge hs-signal-${item.signal_state}`}>{item.signal_state}</span>{" "}
                <span className="muted">{fmtRssi(item.rssi_dbm)}</span>
              </td>
              <td data-label="Motion count">{fmtCount(item.motion_count)}</td>
              <td data-label="15m delta">{fmtMotionDelta(item.motion_delta)}</td>
              <td data-label="1h delta">{fmtMotionDelta(item.motion_delta_1h)}</td>
              <td data-label="Activity">
                <span className={`hs-badge hs-move-${item.movement_state}`}>{movementLabel(item.movement_state)}</span>
              </td>
              <td data-label="Pattern">
                <span className={`hs-badge hs-pattern-${item.pattern_state}`}>{item.pattern_state}</span>
              </td>
              <td data-label="Battery">
                <span className={`hs-badge hs-batt-${item.battery_state}`}>{fmtBatteryVoltage(item.battery_mv)}</span>
              </td>
              <td data-label="Tag temp">{fmtTagTemp(item.tag_temperature_c)}</td>
              <td data-label="Last seen">{fmtStaleness(item.last_seen_at, nowMs)}</td>
              <td data-label="Status">
                <span className={`hs-badge hs-mapping-${item.mapping_state}`}>{item.mapping_state}</span>
                {item.sensor_state === "abnormal" ? <span className="hs-badge hs-sensor-abnormal">sensor abnormal</span> : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <HerdSignalsPagination params={params} position="bottom" itemCount={filteredItems.length} nextCursor={nextCursor} />
    </div>
  );
}

function movementLabel(state: HerdSignalsItem["movement_state"]): string {
  switch (state) {
    case "moving":
      return "active";
    case "quiet":
      return "quiet";
    case "not_moving":
      return "no movement";
    default:
      return "unknown";
  }
}

function HerdSignalsPagination({
  params,
  position,
  itemCount,
  nextCursor,
}: {
  params: HerdSignalsParams;
  position: "top" | "bottom";
  itemCount: number;
  nextCursor: string | null;
}) {
  return (
    <div className={`hs-pagination hs-pagination-${position}`}>
      <label>
        {"Rows per page"}
        <form method="get" style={{ display: "inline" }}>
          <input type="hidden" name="hs_tab" value={params.tab === "live" ? "" : params.tab} />
          <select name="hs_limit" defaultValue={String(params.limit)} onChange={(event) => event.currentTarget.form?.requestSubmit()}>
            {[10, 25, 50, 100].map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </form>
      </label>
      <span className="hs-page-count">{itemCount} shown</span>
      {params.cursor ? (
        <Link href={herdSignalsHref(params, { hs_cursor: undefined })} className="btn">
          Back to start
        </Link>
      ) : null}
      {nextCursor ? (
        <Link href={herdSignalsHref(params, { hs_cursor: nextCursor })} className="btn hs-next-page">
          Next page
        </Link>
      ) : null}
    </div>
  );
}

function HerdSignalsTableEmpty({ params, pageContract }: { params: HerdSignalsParams; pageContract: AdminUiPageContract }) {
  if (params.hasFilter) {
    return (
      <div className="hs-empty">
        <p>{copy(pageContract, "table.filtered_to_nothing", "No tags match this filter.")}</p>
        <Link href={herdSignalsHref(params, { hs_shed: undefined, hs_movement: undefined, hs_mapping: undefined, hs_pattern: undefined, hs_q: undefined, hs_local: undefined })}>
          {copy(pageContract, "filters.clear", "Clear filters")}
        </Link>
      </div>
    );
  }
  return (
    <div className="hs-empty">
      <p>{copy(pageContract, "table.empty_no_packets", "No tag packets have been received yet.")}</p>
    </div>
  );
}
