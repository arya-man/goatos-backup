"use client";

import { useEffect, useMemo, useState } from "react";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import { fmtBleMac } from "./format";
import { ChartReadout, HistoryChart, historyChartLegend } from "./herd-signals-history-chart";

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
// 5-minute / hourly / 6-hourly buckets by range, matching the mock's own roll-up thresholds:
// up to 24h stays on the 300s tier (288 readable bars), 24h-72h rolls to hourly, beyond that to
// 6-hourly. Rolling 24h up to hourly would leave 24 fat bars and break the per-5-minute baseline.
function bucketSecondsFor(range: RangeKey, seconds: number): number {
  if (range === "custom") return seconds <= 24 * 3600 ? 300 : seconds <= 72 * 3600 ? 3600 : 21600;
  if (range === "3d") return 3600;
  if (range === "7d" || range === "30d") return 21600;
  return 300;
}
const BUCKET_LABEL: Record<number, string> = { 300: "5-minute", 3600: "hourly", 21600: "6-hourly" };

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
      const fromDate = new Date(customFrom);
      const toDate = new Date(customTo);
      return {
        from: fromDate.toISOString(),
        to: toDate.toISOString(),
        seconds: Math.max(300, Math.round((toDate.getTime() - fromDate.getTime()) / 1000)),
      };
    }
    const seconds = RANGE_SECONDS[range] ?? 3600;
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    return { from: from.toISOString(), to: to.toISOString(), seconds };
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
    void readTimeline(displayedItem.tag_id, bounds.from, bounds.to, bucketSecondsFor(range, bounds.seconds)).then((result) => {
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
  const location = item.operational_location_display
    ? item.operational_location_display
    : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const bucketSeconds = bucketSecondsFor(range, bounds?.seconds ?? 3600);
  const rangeSummary =
    range === "custom"
      ? bounds
        ? `${new Date(bounds.from).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" })} → ${new Date(bounds.to).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" })}`
        : "Pick a from and a to"
      : `last ${RANGE_LABEL[range]}`;
  const titleLine = `${item.display_id ? `${item.display_id} · ` : "Unmapped tag "}${item.tag_id} — movement history`;
  const subtitleLine = [fmtBleMac(item.tag_mac), location || null, item.gateway_id || null].filter(Boolean).join(" · ");

  return (
    <div className={`fs${drawerOpen ? " on" : ""}`} role="dialog" aria-label="Full movement history" aria-hidden={!drawerOpen}>
      <div className="fshd">
        <svg className="ic" viewBox="0 0 24 24">
          <path d="M3 12h4l3 8 4-16 3 8h4" />
        </svg>
        <div style={{ minWidth: 0 }}>
          <b>{titleLine}</b>
          <div className="faint small mono">{subtitleLine || "—"}</div>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" onClick={closeDrawer}>
          ✕ Close
        </button>
      </div>
      <div className="fsbd">
        <div className="daterow">
          <span className="small faint rowlabel">Range</span>
          <div className="rangepick" role="group" aria-label="History range">
            {(Object.keys(RANGE_LABEL) as RangeKey[])
              .filter((key) => key !== "custom")
              .map((key) => (
                <button key={key} type="button" className={range === key ? "on" : undefined} onClick={() => setRange(key)}>
                  {RANGE_LABEL[key]}
                </button>
              ))}
          </div>
          <span className="muted small">or</span>
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
          <span className="sp" style={{ flex: 1 }} />
          <span className="small faint">{rangeSummary} · Asia/Kolkata</span>
        </div>

        <div className="daterow" style={{ gap: 7 }}>
          <span className="small faint rowlabel">Overlay activity</span>
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
          <span className="sp" style={{ flex: 1 }} />
          <span className="small faint">Overlays are joins onto existing Goat OS records — they are context, not cause.</span>
        </div>

        <div className="card">
          <div className="hd">
            <h3>Movement trend</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="small faint">{BUCKET_LABEL[bucketSeconds] ?? `${bucketSeconds / 60}-minute`} buckets</span>
          </div>
          <div className="bd flush">
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
              <div style={{ padding: "12px 12px 0" }}>
                <HistoryChart buckets={buckets} baseline={item.baseline_delta} height={240} onHover={setHovered} />
              </div>
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
              <ChartReadout buckets={buckets} hovered={hovered} />
            </div>
            <p className="chartnote">
              Activity uses motion-count deltas from historical packets. Quiet periods are normal; alerts use sustained
              patterns. Overlaid markers are other recorded farm activity for the same animal or its shed — read them as
              correlation, never as behaviour, cause, or a clinical finding.
            </p>
          </div>
        </div>

        <div className="grid2" style={{ marginTop: 14 }}>
          <div className="card">
            <div className="hd">
              <h3>Activity around recorded farm activity</h3>
              <div className="sp" style={{ flex: 1 }} />
              <span className="tag t-mut">Correlated</span>
            </div>
            <div className="bd flush">
              <div className="tblwrap">
                <table className="resp">
                  <thead>
                    <tr>
                      <th>Activity</th>
                      <th>When (IST)</th>
                      <th className="num">2h before</th>
                      <th className="num">2h after</th>
                      <th className="num">Change</th>
                      <th>Read</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr>
                      <td colSpan={6}>
                        <div className="empty">
                          <h4>No recorded farm activity available</h4>
                          <p>
                            The overlay read endpoints (vaccination, feed given, weighing, treatment, hoof trimming, shed
                            move) are not built yet, so this table has nothing to join against the motion history above.
                          </p>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <p className="chartnote">
                Change compares the summed motion-count delta in the two hours before and after the recorded activity. A
                feed correlation is a shed-level response to feeding — it is not eating detection. A post-vaccination or
                post-treatment change is a watch signal, never a diagnosis or an adverse-event finding.
              </p>
            </div>
          </div>

          <div className="card">
            <div className="hd">
              <h3>Animal &amp; tag detail</h3>
            </div>
            <div className="bd">
              <dl className="kv">
                <dt>Animal</dt>
                <dd>
                  {item.display_id || <span className="muted">Unmapped</span>}
                  <span className="srcl derived">Derived</span>
                </dd>
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
                <dt>Location</dt>
                <dd>
                  {location || "—"}
                  <span className="srcl derived">Derived</span>
                </dd>
                <dt>Gateway</dt>
                <dd className="mono">
                  {item.gateway_id || "—"}
                  <span className="srcl direct">Direct</span>
                </dd>
                <dt>Buckets in range</dt>
                <dd>
                  {buckets === null ? "—" : buckets.length.toLocaleString("en-IN")}
                  <span className="srcl derived">Derived</span>
                </dd>
                <dt>Buckets with packets</dt>
                <dd>
                  {buckets === null ? "—" : buckets.filter((bucket) => !bucket.is_gap).length.toLocaleString("en-IN")}
                  <span className="srcl derived">Derived</span>
                </dd>
                <dt>Signal-gap buckets</dt>
                <dd>
                  {buckets === null ? "—" : buckets.filter((bucket) => bucket.is_gap).length.toLocaleString("en-IN")}
                  <span className="srcl derived">Derived</span>
                </dd>
              </dl>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
