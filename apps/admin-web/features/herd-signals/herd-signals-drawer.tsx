"use client";

import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  BATTERY_LABEL,
  BATTERY_TONE,
  MAPPING_LABEL,
  MAPPING_TONE,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  PATTERN_LABEL,
  PATTERN_TONE,
  SENSOR_LABEL,
  SENSOR_TONE,
  SIGNAL_LABEL,
  SIGNAL_TONE,
  fmtAgo,
  fmtBatteryMv,
  fmtDelta,
  fmtDelta1h,
  fmtRssi,
  fmtTagTemp,
} from "./format";
import { HistoryChart } from "./herd-signals-history-chart";
import { useNowMs } from "./herd-signals-poller";

type RangeKey = "1h" | "6h" | "24h";
const RANGE_SECONDS: Record<RangeKey, number> = { "1h": 3600, "6h": 6 * 3600, "24h": 24 * 3600 };
const RANGE_BUCKET_SECONDS: Record<RangeKey, number> = { "1h": 300, "6h": 300, "24h": 3600 };

function batteryLifeTone(estimate: string | null): "ok" | "warn" | "dng" | "mut" {
  if (!estimate) return "mut";
  const lower = estimate.toLowerCase();
  if (lower.includes("week")) return "dng";
  if (lower.includes("1 month") || lower.includes("2 month")) return "warn";
  return "ok";
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
  const [range, setRange] = useState<RangeKey>("1h");
  const nowMs = useNowMs();
  // Chart state is keyed to (tag, range) and reset by comparing that key DURING RENDER (React's
  // sanctioned alternative to an Effect that resets state — see "You Might Not Need an Effect").
  // The Effect below only ever calls setState from inside the async .then(), never synchronously at
  // its top, which is what react-hooks/set-state-in-effect requires.
  const chartKey = displayedItem ? `${displayedItem.tag_id}|${range}` : "";
  const [chart, setChart] = useState<{ key: string; buckets: HerdSignalTimelineBucket[] | null; error: string | null }>({
    key: chartKey,
    buckets: null,
    error: null,
  });
  if (chartKey !== chart.key) {
    setChart({ key: chartKey, buckets: null, error: null });
  }
  const requestId = useRef(0);

  useEffect(() => {
    if (!displayedItem) return;
    const id = ++requestId.current;
    const key = `${displayedItem.tag_id}|${range}`;
    void readTimeline(displayedItem.tag_id, range).then((result) => {
      if (requestId.current !== id) return;
      if (result.ok) setChart((prev) => (prev.key === key ? { ...prev, buckets: result.buckets } : prev));
      else setChart((prev) => (prev.key === key ? { ...prev, error: result.error } : prev));
    });
  }, [displayedItem, range]);

  if (!displayedItem) return null;
  const item = displayedItem;
  const { buckets, error: chartError } = chart;
  const location = item.operational_location_display
    ? item.operational_location_display
    : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const expandHref = `${closeHref}${closeHref.includes("?") ? "&" : "?"}hs_history=${encodeURIComponent(item.tag_id)}#hs-history-${encodeURIComponent(item.tag_id)}`;

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
          <div>
            <div className="mt mono">{item.tag_id}</div>
            <h2>{item.display_id || item.goat_id || "Unmapped tag"}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label="Close tag detail" onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          {/* Movement-history chart FIRST, above the readings, so it needs no scrolling. */}
          <div className="card">
            <div className="hd">
              <h3>Movement history</h3>
              <div className="sp" style={{ flex: 1 }} />
              <div className="rangepick" role="group" aria-label="History range">
                {(["1h", "6h", "24h"] as RangeKey[]).map((key) => (
                  <button key={key} type="button" className={range === key ? "on" : undefined} onClick={() => setRange(key)}>
                    {key}
                  </button>
                ))}
              </div>
            </div>
            <div className="bd flush">
              {chartError ? (
                <div className="empty">
                  <p className="muted small" style={{ margin: 0 }}>
                    Could not load history: {chartError}.
                  </p>
                </div>
              ) : buckets === null ? (
                <div style={{ padding: 20 }}>
                  <div className="skelrow" style={{ width: "100%", height: 90 }} />
                </div>
              ) : (
                <HistoryChart buckets={buckets} baseline={item.baseline_delta} />
              )}
              <div className="patrow">
                <LocalOverlayLink href={expandHref} scroll={false} className="btn sm">
                  Expand
                </LocalOverlayLink>
              </div>
            </div>
          </div>

          <div className="metagrid">
            <div>
              <div className="k">Shed</div>
              <div className="v">{location || "—"}</div>
            </div>
            <div>
              <div className="k">Gateway</div>
              <div className="v mono">{item.gateway_id || "—"}</div>
            </div>
            <div>
              <div className="k">BLE MAC</div>
              <div className="v mono">{item.tag_mac || "—"}</div>
            </div>
            <div>
              <div className="k">Mapping</div>
              <div className="v">
                <Tag tone={MAPPING_TONE[item.mapping_state]}>{MAPPING_LABEL[item.mapping_state]}</Tag>
              </div>
            </div>
          </div>

          <div className="muted small" style={{ fontWeight: 700, marginTop: 4 }}>
            Readings
          </div>
          <dl className="kv">
            <dt>Motion count <span className="srcl direct">Direct</span></dt>
            <dd>{fmtDelta(item.motion_count)}</dd>
            <dt>15m motion delta <span className="srcl direct">Direct</span></dt>
            <dd>{fmtDelta(item.motion_delta)}</dd>
            <dt>1h motion delta <span className="srcl direct">Direct</span></dt>
            <dd title="Backend currently aliases this to the 15m window; shown as — until it is a real 1h read">
              {fmtDelta1h(item.motion_delta_1h, item.motion_delta)}
            </dd>
            <dt>RSSI <span className="srcl direct">Direct</span></dt>
            <dd>{fmtRssi(item.rssi_dbm)}</dd>
            <dt>Signal <span className="srcl derived">Derived</span></dt>
            <dd>{item.signal_state ? <Tag tone={SIGNAL_TONE[item.signal_state]}>{SIGNAL_LABEL[item.signal_state]}</Tag> : "—"}</dd>
            <dt>Battery voltage <span className="srcl direct">Direct</span></dt>
            <dd>{fmtBatteryMv(item.battery_mv)}</dd>
            <dt>Battery state <span className="srcl derived">Derived</span></dt>
            <dd>{item.battery_state ? <Tag tone={BATTERY_TONE[item.battery_state]}>{BATTERY_LABEL[item.battery_state]}</Tag> : "—"}</dd>
            <dt>Estimated battery life <span className="srcl inferred">Inferred</span></dt>
            <dd>
              {item.battery_life_estimate ? (
                <Tag tone={batteryLifeTone(item.battery_life_estimate)}>{item.battery_life_estimate}</Tag>
              ) : (
                "—"
              )}
            </dd>
            <dt>Tag temperature <span className="srcl direct">Direct</span></dt>
            <dd>{fmtTagTemp(item.tag_temperature_c)}</dd>
            <dt>Movement trend <span className="srcl derived">Derived</span></dt>
            <dd>{item.movement_state ? <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag> : "—"}</dd>
            <dt>Pattern <span className="srcl derived">Derived</span></dt>
            <dd>{item.pattern_state ? <Tag tone={PATTERN_TONE[item.pattern_state]}>{PATTERN_LABEL[item.pattern_state]}</Tag> : "—"}</dd>
            <dt>Sensor status <span className="srcl direct">Direct</span></dt>
            <dd>{item.sensor_state ? <Tag tone={SENSOR_TONE[item.sensor_state]}>{SENSOR_LABEL[item.sensor_state]}</Tag> : "—"}</dd>
            <dt>Last seen <span className="srcl direct">Direct</span></dt>
            <dd>{fmtAgo(item.last_seen_at, nowMs)}</dd>
          </dl>
          <p className="faint small" style={{ marginTop: 4 }}>
            Tag temperature is the temperature measured at the tag&apos;s own sensor housing — not the
            animal&apos;s body temperature.
          </p>
        </div>
        <div className="df">
          <button type="button" className="btn" onClick={closeDrawer}>
            Close
          </button>
        </div>
      </aside>
    </>
  );
}
