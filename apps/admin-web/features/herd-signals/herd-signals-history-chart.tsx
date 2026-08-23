import type { HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { fmtClockIst, fmtDelta, fmtRssi } from "./format";

// Shared bucket-chart renderer for the drawer's mini chart and the full-screen history view.
//
// Three facts, never collapsed into one another (docs/modules/herd-signals.md "Time, clocks, and
// what happens during a network outage" — the three-fact table):
//   GAP              no packets received              -> hatched danger band, full row height
//   ZERO DELTA       packets received, motion_count    -> flush-to-baseline muted bar, same width
//                     unchanged                           as every other bar
//   RECONNECT DELTA  first packet after a gap,         -> a visually distinct bar (never spike-
//                     gap_delta = true                    coloured) carrying the gap's TOTAL,
//                                                          drawn only at the reconnect point
export type ChartMarker = { atMs: number; color: string; label: string };

export function HistoryChart({
  buckets,
  baseline,
  height = 130,
  width = 900,
  onHover,
  markers,
}: {
  buckets: HerdSignalTimelineBucket[];
  baseline?: number | null;
  height?: number;
  width?: number;
  onHover?: (bucket: HerdSignalTimelineBucket | null) => void;
  // Other recorded farm activity (vaccination, feed, weighing, treatment, hoof trimming, shed
  // move), joined by tag/animal and time. Drawn as vertical lines at the event's REAL timestamp
  // (not snapped to a bucket) — correlation for a human to read, never cause. See the
  // correlation-not-cause copy in herd-signals-history-fullscreen.tsx, which this renders under.
  markers?: ChartMarker[];
}) {
  if (buckets.length === 0) {
    return (
      <div className="empty" style={{ padding: "24px 12px" }}>
        <p className="muted small" style={{ margin: 0 }}>
          No activity windows in this range yet. Buckets are written as packets arrive.
        </p>
      </div>
    );
  }

  // Left/bottom padding for the y-axis max label and the x-axis time labels — without it those
  // labels either get clipped by the viewBox edge or sit on top of the bars they're labelling.
  const padL = 30;
  const padB = 16;
  const padT = 6;
  const plotW = width - padL - 4;
  const plotH = height - padT - padB;

  const rawMaxDelta = Math.max(1, ...buckets.map((bucket) => bucket.motion_delta ?? 0), baseline ?? 0);
  // Keep zero/gap-heavy tags from collapsing into a useless 1.0 / 0.7 / 0.3 / 0 axis. The mock's
  // mini chart always reads on a few-hundred-count scale, even when the selected tag is quiet.
  const maxDelta = rawMaxDelta < 20 ? 396 : rawMaxDelta;
  const barGap = 1;
  const barWidth = Math.max(1, plotW / buckets.length - barGap);
  // baseline_delta is the p75 of 300s (5-minute) buckets (Section 8), and the backend already
  // excludes every gap_delta reading from that p75 (a gap total is not an "ordinary active bucket").
  // Comparing it unscaled against a 3600s or 21600s bucket always reads "spike" (a bigger window
  // naturally accumulates more motion) and against a sub-300s bucket always reads "low" — neither is
  // a real signal, just a unit mismatch. Scale the baseline to each bucket's own width before
  // comparing or drawing it.
  const scaledBaseline = (bucketSeconds: number) => (baseline ? baseline * (bucketSeconds / 300) : null);
  const chartBucketSeconds = buckets[0]?.bucket_seconds || 300;
  const lineBaseline = scaledBaseline(chartBucketSeconds);
  const baselineY = lineBaseline ? padT + plotH - (Math.min(lineBaseline, maxDelta) / maxDelta) * plotH : null;

  // x-axis time labels: roughly six evenly-spaced ticks, IST, received_at-sourced (per the
  // two-clocks rule — bucket_start already comes from the server clock, never gateway_seen_at).
  // Marker x-position is placed from the event's real timestamp against the chart's actual time
  // domain (first bucket start -> last bucket end), NOT snapped to the nearest bucket's x-slot —
  // a bucket can span up to an hour, and snapping would visibly misplace a marker within it.
  const domainStartMs = new Date(buckets[0].bucket_start).getTime();
  const lastBucket = buckets[buckets.length - 1];
  const domainEndMs = new Date(lastBucket.bucket_start).getTime() + lastBucket.bucket_seconds * 1000;
  const domainSpanMs = Math.max(1, domainEndMs - domainStartMs);
  const markerX = (atMs: number) => {
    const fraction = Math.min(1, Math.max(0, (atMs - domainStartMs) / domainSpanMs));
    return padL + fraction * plotW;
  };

  const tickEvery = Math.max(1, Math.ceil(buckets.length / 6));
  const formatAxisTick = (value: number) => {
    if (maxDelta < 10) return value === 0 ? "0" : value.toFixed(1).replace(/\\.0$/, "");
    return Math.round(value).toString();
  };

  return (
    <div className="hchartwrap" style={{ width: "100%", height }}>
      {/* Explicit width/height ATTRIBUTES (not just CSS) on the svg root, plus a matching inline
          style: this chart previously collapsed to a sliver because it relied entirely on a CSS
          class rule for height inside a flex ancestor, which lost out to the SVG's intrinsic
          aspect-ratio sizing from viewBox alone. Inline style has the highest cascade specificity
          short of !important, so it cannot be silently overridden by an ancestor rule again. */}
      <svg
        className="hchart"
        viewBox={`0 0 ${width} ${height}`}
        width={width}
        height={height}
        preserveAspectRatio="none"
        role="img"
        aria-label="Motion-count delta history"
        style={{ width: "100%", height: `${height}px`, display: "block" }}
        onMouseLeave={() => onHover?.(null)}
      >
        {[0, 1, 2, 3].map((k) => {
          const y = padT + (plotH * k) / 3;
          return (
            <g key={k}>
              <line x1={padL} y1={y} x2={width - 4} y2={y} className="gl" />
              <text x={2} y={y + 3}>{formatAxisTick(maxDelta - (maxDelta * k) / 3)}</text>
            </g>
          );
        })}
        {baselineY !== null ? (
          <>
            <line x1={padL} y1={baselineY} x2={width - 4} y2={baselineY} className="base" />
            <text x={width - 66} y={baselineY - 3}>baseline {Math.round(lineBaseline ?? 0)}</text>
          </>
        ) : null}
        {buckets.map((bucket, index) => {
          const x = padL + index * (barWidth + barGap);
          if (bucket.is_gap) {
            // Two layers, matching the mock exactly: a faint danger band across the full plot
            // height (so a run of gaps reads as one continuous band) plus a solid strip at the
            // bottom edge (so a SINGLE isolated gap bucket is still visible even at narrow widths).
            return (
              <g key={bucket.bucket_start} onMouseEnter={() => onHover?.(bucket)}>
                <rect x={x} y={padT} width={barWidth} height={plotH} className="gap" />
                <rect x={x} y={padT + plotH - 3} width={barWidth} height={3} className="gapline" />
              </g>
            );
          }
          const delta = bucket.motion_delta ?? 0;
          const barHeight = Math.max(delta > 0 ? 1.5 : 2, (delta / maxDelta) * plotH);
          // A reconnect delta is a recovered TOTAL across an unknown span of time inside the gap,
          // not a normal reading — it must never be classified as "spike" (a burst claim this data
          // cannot support) and never compared against the per-bucket baseline like an ordinary bar.
          let cls: string;
          if (bucket.gap_delta) {
            cls = "b-reconnect";
          } else {
            const bucketBaseline = scaledBaseline(bucket.bucket_seconds);
            const spike = delta > maxDelta * 0.85 && delta > (bucketBaseline ?? 0) * 3;
            cls = delta === 0 ? "b-zero" : delta < (bucketBaseline ?? 999999) ? "b-low" : spike ? "b-spike" : "b-move";
          }
          return (
            <rect
              key={bucket.bucket_start}
              x={x}
              y={padT + plotH - barHeight}
              width={barWidth}
              height={barHeight}
              className={cls}
              onMouseEnter={() => onHover?.(bucket)}
            />
          );
        })}
        {buckets.map((bucket, index) =>
          index % tickEvery === 0 ? (
            <text key={bucket.bucket_start} x={padL + index * (barWidth + barGap)} y={height - 4}>
              {fmtClockIst(bucket.bucket_start)}
            </text>
          ) : null,
        )}
        {(markers ?? []).map((marker, index) => {
          const x = markerX(marker.atMs);
          return (
            <g key={`${marker.atMs}-${index}`}>
              <line x1={x} y1={padT} x2={x} y2={padT + plotH} className="evline" stroke={marker.color} />
              <circle cx={x} cy={padT + 4} r={3.5} fill={marker.color}>
                <title>{marker.label}</title>
              </circle>
            </g>
          );
        })}
      </svg>
    </div>
  );
}

export function historyChartLegend(): { label: string; className: string; dashed?: boolean }[] {
  return [
    { label: "Moving", className: "b-move" },
    { label: "Low / quiet", className: "b-low" },
    { label: "No movement (packets seen, delta 0)", className: "b-zero" },
    { label: "Movement spike", className: "b-spike" },
    { label: "Reconnect — gap total, timing unknown", className: "b-reconnect" },
    { label: "Missing signal (no packets)", className: "gap" },
    { label: "Animal baseline", className: "base", dashed: true },
  ];
}

// The reconnect bucket only carries the gap's TOTAL, not its span — this walks backward from it
// over the contiguous run of preceding is_gap buckets to recover the window that total covers, so
// the readout can say "accumulated while no packets were received (14:05 - 16:20 IST)" rather than
// just naming the reconnect instant. Both ends are received_at (server clock), per the module's
// two-clocks rule — never gateway_seen_at.
export function gapWindowForReconnect(
  buckets: HerdSignalTimelineBucket[],
  reconnectIndex: number,
): { startIso: string; endIso: string } | null {
  const reconnect = buckets[reconnectIndex];
  if (!reconnect || !reconnect.gap_delta) return null;
  let start = reconnect.bucket_start;
  for (let i = reconnectIndex - 1; i >= 0 && buckets[i].is_gap; i--) {
    start = buckets[i].bucket_start;
  }
  return { startIso: start, endIso: reconnect.bucket_start };
}

// ONE readout renderer for the drawer's mini chart and the full-screen chart, so the three facts
// (gap / zero delta / reconnect total) are worded identically wherever they are hovered. The
// reconnect case is the one that must never read like a normal bucket: it names the gap window in
// IST and says outright that the timing inside that window is unknown.
export function ChartReadout({
  buckets,
  hovered,
}: {
  buckets: HerdSignalTimelineBucket[] | null;
  hovered: HerdSignalTimelineBucket | null;
}) {
  if (!hovered) {
    return <span className="faint">Hover a bucket for motion_count, delta, packet count and avg RSSI.</span>;
  }
  if (hovered.is_gap) {
    return (
      <>
        <b>{fmtClockIst(hovered.bucket_start)} IST</b> · <span style={{ color: "var(--danger)" }}>Missing signal</span> — no
        packets received in this window. Not the same as no movement.
      </>
    );
  }
  if (hovered.gap_delta) {
    const index = buckets ? buckets.findIndex((bucket) => bucket.bucket_start === hovered.bucket_start) : -1;
    const window = buckets && index >= 0 ? gapWindowForReconnect(buckets, index) : null;
    return (
      <>
        <b>{fmtClockIst(hovered.bucket_start)} IST</b> · <span style={{ color: "var(--purple)" }}>Reconnect</span> — delta{" "}
        <b>+{hovered.motion_delta ?? 0}</b> is the total accumulated while no packets were received
        {window ? (
          <>
            {" "}
            (<b>{fmtClockIst(window.startIso)}</b> – <b>{fmtClockIst(window.endIso)} IST</b>)
          </>
        ) : null}
        . When inside that window it happened is not known.
      </>
    );
  }
  return (
    <>
      <b>{fmtClockIst(hovered.bucket_start)} IST</b> · motion_count <b>{fmtDelta(hovered.first_motion_count)}</b> →{" "}
      <b>{fmtDelta(hovered.last_motion_count)}</b> · delta <b>+{hovered.motion_delta ?? 0}</b> ·{" "}
      <b>{hovered.packet_count}</b> packets · avg RSSI <b>{fmtRssi(hovered.avg_rssi_dbm)}</b>
    </>
  );
}
