"use client";

import { useEffect, useMemo, useState } from "react";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { fmtDateTime } from "@/lib/format";
import { fmtClockIst, fmtDelta, fmtRssi } from "./format";
import { HistoryChart, historyChartLegend, gapWindowForReconnect } from "./herd-signals-history-chart";

type RangeKey = "1h" | "6h" | "12h" | "24h" | "3d" | "7d" | "30d" | "custom";
const RANGE_LABEL: Record<RangeKey, string> = {
  "1h": "1h",
  "6h": "6h",
  "12h": "12h",
  "24h": "24h",
  "3d": "3d",
  "7d": "7d",
  "30d": "30d",
  custom: "Custom",
};
const RANGE_SECONDS: Partial<Record<RangeKey, number>> = {
  "1h": 3600,
  "6h": 6 * 3600,
  "12h": 12 * 3600,
  "24h": 24 * 3600,
  "3d": 3 * 24 * 3600,
  "7d": 7 * 24 * 3600,
  "30d": 30 * 24 * 3600,
};
// 5-minute / hourly / 6-hourly buckets by range (per herd-signals module spec).
function bucketSecondsFor(range: RangeKey): number {
  if (range === "1h" || range === "6h") return 300;
  if (range === "12h" || range === "24h") return 3600;
  return 21600;
}

// The six farm-activity overlays. None of these has a data endpoint in the fixed contract this
// page was built against (GET /herd-signals/live|tags/{id}/timeline|gateways|insights only) — so
// every toggle here is disabled-with-a-reason rather than faking a data source. Wiring them up is a
// follow-up once the corresponding read model exists.
const OVERLAYS = ["Vaccination", "Feed given", "Weighing", "Treatment", "Hoof trimming", "Shed move"];

async function readTimeline(
  tagId: string,
  from: string,
  to: string,
  bucketSeconds: number,
): Promise<{ ok: true; buckets: HerdSignalTimelineBucket[] } | { ok: false; error: string }> {
  try {
    const response = await fetch(
      `/api/herd-signals/tags/${encodeURIComponent(tagId)}/timeline?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&bucket_seconds=${bucketSeconds}`,
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    const payload = (await response.json().catch(() => ({}))) as { buckets?: HerdSignalTimelineBucket[]; error?: string };
    if (!response.ok) return { ok: false, error: payload.error ?? `timeline_read_${response.status}` };
    return { ok: true, buckets: payload.buckets ?? [] };
  } catch {
    return { ok: false, error: "timeline_unreachable" };
  }
}

function rowId(item: HerdSignalItem): string {
  return item.tag_id;
}

export function HerdSignalsHistoryFullscreen({ rows, closeHref }: { rows: HerdSignalItem[]; closeHref: string }) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "hs_history",
    closeHref,
  });
  const [range, setRange] = useState<RangeKey>("24h");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");
  const [hovered, setHovered] = useState<HerdSignalTimelineBucket | null>(null);
  const [overlaysOn, setOverlaysOn] = useState<Record<string, boolean>>({});

  const bounds = useMemo(() => {
    if (range === "custom") {
      if (!customFrom || !customTo) return null;
      return { from: new Date(customFrom).toISOString(), to: new Date(customTo).toISOString() };
    }
    const seconds = RANGE_SECONDS[range] ?? 3600;
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    return { from: from.toISOString(), to: to.toISOString() };
  }, [range, customFrom, customTo]);

  // Reset-on-key-change happens DURING RENDER (React's sanctioned alternative to an Effect that
  // resets state), not as a synchronous setState at the top of the Effect body — the Effect below
  // only calls setState from inside the async .then().
  const chartKey = displayedItem && bounds ? `${displayedItem.tag_id}|${bounds.from}|${bounds.to}|${range}` : "";
  const [chart, setChart] = useState<{ key: string; buckets: HerdSignalTimelineBucket[] | null; error: string | null }>({
    key: chartKey,
    buckets: null,
    error: null,
  });
  if (chartKey !== chart.key) {
    setChart({ key: chartKey, buckets: null, error: null });
  }

  useEffect(() => {
    if (!displayedItem || !bounds) return;
    let active = true;
    const key = `${displayedItem.tag_id}|${bounds.from}|${bounds.to}|${range}`;
    void readTimeline(displayedItem.tag_id, bounds.from, bounds.to, bucketSecondsFor(range)).then((result) => {
      if (!active) return;
      if (result.ok) setChart((prev) => (prev.key === key ? { ...prev, buckets: result.buckets } : prev));
      else setChart((prev) => (prev.key === key ? { ...prev, error: result.error } : prev));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, bounds, range]);
  const { buckets, error } = chart;

  if (!displayedItem) return null;
  const item = displayedItem;

  return (
    <div className={`fs${drawerOpen ? " on" : ""}`} role="dialog" aria-label="Full movement history" aria-hidden={!drawerOpen}>
      <div className="fshd">
        <svg className="ic" viewBox="0 0 24 24">
          <path d="M3 12h4l3 8 4-16 3 8h4" />
        </svg>
        <div style={{ minWidth: 0 }}>
          <b>{item.display_id || item.goat_id || "Unmapped tag"}</b>
          <div className="faint small mono">{item.tag_id}</div>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" onClick={closeDrawer}>
          ✕ Close
        </button>
      </div>
      <div className="fsbd">
        <div className="daterow">
          <div className="rangepick" role="group" aria-label="History range">
            {(Object.keys(RANGE_LABEL) as RangeKey[])
              .filter((key) => key !== "custom")
              .map((key) => (
                <button key={key} type="button" className={range === key ? "on" : undefined} onClick={() => setRange(key)}>
                  {RANGE_LABEL[key]}
                </button>
              ))}
          </div>
          <span className="muted small">or custom range:</span>
          <input
            type="datetime-local"
            value={customFrom}
            onChange={(event) => {
              setCustomFrom(event.target.value);
              setRange("custom");
            }}
            aria-label="From"
          />
          <span className="muted small">to</span>
          <input
            type="datetime-local"
            value={customTo}
            onChange={(event) => {
              setCustomTo(event.target.value);
              setRange("custom");
            }}
            aria-label="To"
          />
        </div>

        <div className="card">
          <div className="hd">
            <h3>Motion-count delta history</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="small faint">{RANGE_LABEL[range]} · {bucketSecondsFor(range) / 60}min buckets</span>
          </div>
          <div className="bd flush">
            <div style={{ padding: "12px 15px 0" }}>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                {OVERLAYS.map((overlay) => (
                  <button
                    key={overlay}
                    type="button"
                    className={`evchip${overlaysOn[overlay] ? " on" : ""}`}
                    disabled
                    title="No activity-overlay read endpoint is available yet — this toggle is wired but has nothing to fetch."
                    onClick={() => setOverlaysOn((current) => ({ ...current, [overlay]: !current[overlay] }))}
                  >
                    <i aria-hidden="true" /> {overlay}
                  </button>
                ))}
              </div>
            </div>
            {error ? (
              <div className="empty dngstate">
                <div className="eicon">
                  <svg className="ic" viewBox="0 0 24 24">
                    <path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" />
                    <path d="M12 9v4" />
                    <path d="M12 17h.01" />
                  </svg>
                </div>
                <h4>History read failed</h4>
                <p>{error}. Try selecting the range again.</p>
              </div>
            ) : buckets === null ? (
              <div style={{ padding: 20 }}>
                <div className="skelrow" style={{ width: "100%", height: 240 }} />
              </div>
            ) : (
              <HistoryChart buckets={buckets} baseline={item.baseline_delta} height={240} onHover={setHovered} />
            )}
            <div className="legend">
              {historyChartLegend().map((entry) =>
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
              {hovered ? (
                hovered.is_gap ? (
                  <>
                    <b>{fmtDateTime(hovered.bucket_start)}</b> — no packets received in this window (gap), not zero
                    movement.
                  </>
                ) : hovered.gap_delta ? (
                  (() => {
                    const index = buckets?.findIndex((bucket) => bucket.bucket_start === hovered.bucket_start) ?? -1;
                    const window = buckets && index >= 0 ? gapWindowForReconnect(buckets, index) : null;
                    return (
                      <>
                        <b>+{fmtDelta(hovered.motion_delta)}</b> accumulated while no packets were received
                        {window ? (
                          <>
                            {" "}
                            (<b>{fmtClockIst(window.startIso)}</b> - <b>{fmtClockIst(window.endIso)}</b> IST)
                          </>
                        ) : null}
                        ; when inside that window it happened is not known.
                      </>
                    );
                  })()
                ) : (
                  <>
                    <b>{fmtDateTime(hovered.bucket_start)}</b> · motion_count <b>{fmtDelta(hovered.first_motion_count)}</b>{" "}
                    → <b>{fmtDelta(hovered.last_motion_count)}</b> · delta <b>{fmtDelta(hovered.motion_delta)}</b> ·{" "}
                    {hovered.packet_count} packets · avg RSSI <b>{fmtRssi(hovered.avg_rssi_dbm)}</b>
                  </>
                )
              ) : (
                "Hover a bar for its window."
              )}
            </div>
          </div>
        </div>

        <div className="card" style={{ marginTop: 14 }}>
          <div className="hd">
            <h3>Activity around recorded farm activity</h3>
          </div>
          <div className="bd flush">
            <div className="empty">
              <div className="eicon">
                <svg className="ic" viewBox="0 0 24 24">
                  <rect x="3" y="4" width="18" height="18" rx="2" />
                  <path d="M16 2v4" />
                  <path d="M8 2v4" />
                  <path d="M3 10h18" />
                </svg>
              </div>
              <h4>No recorded farm activity available</h4>
              <p>
                The overlay read endpoints (vaccination, feed, weighing, treatment, hoof trimming,
                shed move) are not built yet, so this table has nothing to join against the motion
                history above.
              </p>
            </div>
          </div>
        </div>

        <p className="muted small" style={{ marginTop: 14 }}>
          Activity uses motion-count deltas from historical packets. Quiet periods are normal;
          alerts use sustained patterns.
        </p>
        <p className="faint small">
          Overlaid markers are other recorded farm activity for the same animal or its shed. Read
          them as correlation, never as behaviour, cause, or a clinical finding.
        </p>
      </div>
    </div>
  );
}
