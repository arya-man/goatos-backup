"use client";

import { useEffect, useRef, useState } from "react";
import { AlertTriangle, Maximize2, Radio, X } from "lucide-react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  MAPPING_LABEL,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  PATTERN_LABEL,
  PATTERN_TONE,
  PATTERN_WHY,
  fmtAgo,
  fmtBatteryMv,
  fmtBleMac,
  fmtDelta,
  fmtRssi,
} from "./format";
import { ChartReadout, HistoryChart, historyChartLegend } from "./herd-signals-history-chart";
import { useNowMs } from "./herd-signals-poller";

type RangeKey = "1h" | "6h" | "24h";
const RANGE_SECONDS: Record<RangeKey, number> = { "1h": 3600, "6h": 6 * 3600, "24h": 24 * 3600 };
// Every drawer range stays on the 300s (5-minute) tier, exactly as the mock draws it: rolling 24h
// up to hourly leaves 24 fat bars instead of 288 readable ones, and the baseline chip is quoted per
// 5 minutes, so a 3600s bar cannot be compared against it without a unit mismatch.
const RANGE_BUCKET_SECONDS: Record<RangeKey, number> = { "1h": 300, "6h": 300, "24h": 300 };

function drawerBatteryVoltage(mv: number | null | undefined): string {
  if (mv === null || mv === undefined) return "—";
  return `${fmtBatteryMv(mv)} (${mv} mV)`;
}

function drawerGateway(gatewayId: string | null | undefined): string {
  if (!gatewayId) return "—";
  if (/^\d+$/.test(gatewayId)) return `GW-${gatewayId}`;
  return gatewayId;
}

function drawerTagTemp(celsius: number | null | undefined): string {
  if (celsius === null || celsius === undefined) return "—";
  return `${celsius.toFixed(1)} C`;
}

async function readTimeline(tagId: string, range: RangeKey): Promise<{ ok: true; buckets: HerdSignalTimelineBucket[] } | { ok: false; error: string }> {
  const to = new Date();
  const from = new Date(to.getTime() - RANGE_SECONDS[range] * 1000);
  try {
    const response = await fetch(
      `/api/herd-signals/tags/${encodeURIComponent(tagId)}/timeline?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}&bucket_seconds=${RANGE_BUCKET_SECONDS[range]}`,
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    const payload = (await response.json().catch(() => ({}))) as { buckets?: HerdSignalTimelineBucket[]; error?: string };
    if (!response.ok) return { ok: false, error: payload.error ?? `timeline_read_${response.status}` };
    return { ok: true, buckets: payload.buckets ?? [] };
  } catch {
    return { ok: false, error: "timeline_unreachable" };
  }
}

export function HerdSignalsDrawer({
  rows,
  rowId,
  initialSelectedId,
  closeHref,
}: {
  rows: HerdSignalItem[];
  rowId: (item: HerdSignalItem) => string;
  initialSelectedId?: string;
  closeHref: string;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "hs_tag",
    initialSelectedId,
    closeHref,
  });
  const [range, setRange] = useState<RangeKey>("6h");
  const nowMs = useNowMs();
  const [hovered, setHovered] = useState<HerdSignalTimelineBucket | null>(null);
  // Bumped by the Retry button so a failed read can be re-fetched without changing tag or range —
  // included in the effect's dependency array below so a retry actually re-runs the fetch, not just
  // clears the error text back to a loading skeleton that never resolves again.
  const [retryToken, setRetryToken] = useState(0);
  // Chart state is keyed to (tag, range, retryToken) and reset by comparing that key DURING RENDER
  // (React's sanctioned alternative to an Effect that resets state — see "You Might Not Need an
  // Effect"). The Effect below only ever calls setState from inside the async .then(), never
  // synchronously at its top, which is what react-hooks/set-state-in-effect requires.
  const chartKey = displayedItem ? `${displayedItem.tag_id}|${range}|${retryToken}` : "";
  const [chart, setChart] = useState<{ key: string; buckets: HerdSignalTimelineBucket[] | null; error: string | null }>({
    key: chartKey,
    buckets: null,
    error: null,
  });
  if (chartKey !== chart.key) {
    setChart({ key: chartKey, buckets: null, error: null });
  }
  const requestId = useRef(0);

  // Depend on the tag ID STRING, never the item OBJECT. The board re-renders every second (the
  // live clock ticks), which hands us a fresh `displayedItem` identity each time; with the object
  // in the dependency array this effect re-fired every second, each run cancelling the previous
  // request through requestId, so the fetch never committed and the chart sat on its loading
  // skeleton forever. Three in-flight timeline requests with different from/to was the tell.
  const displayedTagId = displayedItem?.tag_id ?? null;

  useEffect(() => {
    if (!displayedTagId) return;
    const id = ++requestId.current;
    const key = `${displayedTagId}|${range}|${retryToken}`;
    void readTimeline(displayedTagId, range).then((result) => {
      if (requestId.current !== id) return;
      if (result.ok) setChart((prev) => (prev.key === key ? { ...prev, buckets: result.buckets } : prev));
      else setChart((prev) => (prev.key === key ? { ...prev, error: result.error } : prev));
    });
  }, [displayedTagId, range, retryToken]);

  if (!displayedItem) return null;
  const item = displayedItem;
  const { buckets, error: chartError } = chart;
  const location = item.operational_location_display
    ? item.operational_location_display
    : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const expandHref = `${closeHref}${closeHref.includes("?") ? "&" : "?"}hs_history=${encodeURIComponent(item.tag_id)}#hs-history-${encodeURIComponent(item.tag_id)}`;
  const animalLabel = item.animal_identifier_1 || item.animal_identifier_2 || item.display_id;
  const titleLine = animalLabel ? `${animalLabel} · ${item.tag_id}` : `Unmapped tag ${item.tag_id}`;
  const subtitleLine = [fmtBleMac(item.tag_mac), location || null, item.gateway_id || null].filter(Boolean).join(" · ");

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label="Close tag detail"
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label="Tag detail" aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <Radio className="ic" />
          <div style={{ minWidth: 0 }}>
            <b>{titleLine}</b>
            <div className="faint small mono">{subtitleLine || "—"}</div>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label="Close tag detail" onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          {/* Control row directly under the header: pattern + baseline chips, range picker, Expand. */}
          <div className="patrow" style={{ paddingTop: 0 }}>
            {item.pattern_state ? <Tag tone={PATTERN_TONE[item.pattern_state]}>{PATTERN_LABEL[item.pattern_state]}</Tag> : null}
            {item.baseline_delta !== null && item.baseline_delta !== undefined ? (
              <Tag tone="mut">baseline {item.baseline_delta} / 5 min</Tag>
            ) : null}
            <span className="sp" style={{ flex: 1 }} />
            <div className="rangepick" role="group" aria-label="History range">
              {(["1h", "6h", "24h"] as RangeKey[]).map((key) => (
                <button key={key} type="button" className={range === key ? "on" : undefined} onClick={() => setRange(key)}>
                  {key}
                </button>
              ))}
            </div>
            <LocalOverlayLink href={expandHref} scroll={false} className="btn sm hs-btn" title="Full history, custom date range and farm-activity overlay">
              <Maximize2 className="ic sm" />
              Expand
            </LocalOverlayLink>
          </div>

          {/* Movement-history chart FIRST, above the readings, so it needs no scrolling. */}
          <div className="card">
            <div className="bd flush">
              {chartError ? (
                // A failed read is a distinct state from "this tag has no history" (empty buckets)
                // and must never render as a blank/grey panel indistinguishable from either —
                // docs/modules/herd-signals.md "Required UI states" is explicit about this exact
                // case. Same icon/heading/retry shape as every other read-failed state in this page.
                <div className="empty dngstate">
                  <div className="eicon">
                    <svg className="ic" viewBox="0 0 24 24">
                      <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                      <path d="M12 9v4" />
                      <path d="M12 17h.01" />
                    </svg>
                  </div>
                  <h4>History read failed</h4>
                  <p>{chartError}.</p>
                  <div className="eact">
                    <button type="button" className="btn sm hs-btn" onClick={() => setRetryToken((current) => current + 1)}>
                      Retry
                    </button>
                  </div>
                </div>
              ) : buckets === null ? (
                <div style={{ padding: 20 }}>
                  <div className="skelrow" style={{ width: "100%", height: 110 }} />
                </div>
              ) : (
                <HistoryChart buckets={buckets} baseline={item.baseline_delta} height={110} onHover={setHovered} />
              )}
              <div className="legend">
                {historyChartLegend().filter((entry) => entry.className !== "b-reconnect").map((entry) =>
                  entry.dashed ? (
                    <span key={entry.label}>
                      <i className={`${entry.className} dashed`} /> {entry.label}
                    </span>
                  ) : (
                    <span key={entry.label}>
                      <i className={entry.className} style={{ background: "currentColor" }} /> {entry.label}
                    </span>
                  ),
                )}
              </div>
              <div className="readout" aria-live="polite">
                <ChartReadout buckets={buckets} hovered={hovered} />
              </div>
              <div className="small faint hs-pattern-note">
                {item.pattern_state ? `${PATTERN_WHY[item.pattern_state].charAt(0).toUpperCase()}${PATTERN_WHY[item.pattern_state].slice(1)}. ` : ""}
                Activity uses motion-count deltas from historical packets. Quiet periods are normal; alerts use sustained patterns.
              </div>
            </div>
          </div>

          <dl className="kv">
            <dt>Tag ID</dt>
            <dd className="mono">
              {item.tag_id}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>BLE MAC</dt>
            <dd className="mono">
              {fmtBleMac(item.tag_mac)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Animal</dt>
            <dd>
              {animalLabel || <span className="muted">Unmapped</span>}
              <span className="srcl derived">Derived</span>
            </dd>
            <dt>Location</dt>
            <dd>
              {location || "—"}
              <span className="srcl correlated">Correlated</span>
            </dd>
            <dt>Gateway</dt>
            <dd className="mono">
              {drawerGateway(item.gateway_id)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>RSSI</dt>
            <dd>
              {fmtRssi(item.rssi_dbm)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Battery voltage</dt>
            <dd>
              {drawerBatteryVoltage(item.battery_mv)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Tag temp</dt>
            <dd>
              {drawerTagTemp(item.tag_temperature_c)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Motion count</dt>
            <dd>
              {fmtDelta(item.motion_count)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>15m motion delta</dt>
            <dd title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}>

              {fmtDelta(item.motion_delta)}
              {item.gap_delta ? <sup title="Gap total">*</sup> : null}
              <span className="srcl derived">Derived</span>
            </dd>
            <dt>Movement state</dt>
            <dd>
              {item.movement_state ? <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag> : "—"}
              <span className="srcl inferred">Inferred</span>
            </dd>
            <dt>Last seen</dt>
            <dd>
              {fmtAgo(item.last_seen_at, nowMs)}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Temp sensor</dt>
            <dd>
              {item.temperature_sensor_ok === null || item.temperature_sensor_ok === undefined ? "—" : item.temperature_sensor_ok ? "OK" : "Abnormal"}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Accelerometer</dt>
            <dd>
              {item.accelerometer_sensor_ok === null || item.accelerometer_sensor_ok === undefined ? "—" : item.accelerometer_sensor_ok ? "OK" : "Abnormal"}
              <span className="srcl direct">Direct</span>
            </dd>
            <dt>Mapping state</dt>
            <dd>
              {item.mapping_state === "conflict" ? "mapping conflict" : MAPPING_LABEL[item.mapping_state]}
              <span className="srcl derived">Derived</span>
            </dd>
          </dl>
          <div className="banner info hs-drawer-note">
            <AlertTriangle className="ic" />
            <div>Tag temperature is the temperature of the tag, not the animal. No behaviour, posture or clinical state is inferred from these values.</div>
          </div>
        </div>
      </aside>
    </>
  );
}
