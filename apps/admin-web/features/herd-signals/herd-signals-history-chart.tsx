import type { HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { fmtClockIst } from "./format";

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
export function HistoryChart({
  buckets,
  baseline,
  height = 130,
  width = 900,
  onHover,
}: {
  buckets: HerdSignalTimelineBucket[];
  baseline?: number | null;
  height?: number;
  width?: number;
  onHover?: (bucket: HerdSignalTimelineBucket | null) => void;
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

  const maxDelta = Math.max(1, ...buckets.map((bucket) => bucket.motion_delta ?? 0));
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
  const tickEvery = Math.max(1, Math.ceil(buckets.length / 6));

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
              <text x={2} y={y + 3}>{Math.round(maxDelta - (maxDelta * k) / 3)}</text>
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
            return (
              <rect
                key={bucket.bucket_start}
                x={x}
                y={padT}
                width={barWidth}
                height={plotH}
                className="gap"
                onMouseEnter={() => onHover?.(bucket)}
              />
            );
          }
          const delta = bucket.motion_delta ?? 0;
          const barHeight = Math.max(delta > 0 ? 1.5 : 1, (delta / maxDelta) * plotH);
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
