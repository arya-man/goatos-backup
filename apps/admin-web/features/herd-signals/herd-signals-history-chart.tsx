import type { HerdSignalTimelineBucket } from "@/lib/api/herd-signals";

// Shared bucket-chart renderer for the drawer's mini chart and the full-screen history view.
// The one rule this exists to enforce: a GAP (no packets received) must never look like ZERO
// MOVEMENT (packets received, delta 0) — docs/modules/herd-signals.md "Required UI states". A gap
// renders as a hatched danger-toned band across the full row height; a real zero-delta bucket
// renders as a flush-to-baseline muted bar the same width as every other bar.
export function HistoryChart({
  buckets,
  baseline,
  height = 120,
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

  const maxDelta = Math.max(1, ...buckets.map((bucket) => bucket.motion_delta ?? 0));
  const barGap = 1;
  const barWidth = Math.max(1, width / buckets.length - barGap);
  const baselineY = baseline ? height - (Math.min(baseline, maxDelta) / maxDelta) * (height - 14) : null;

  return (
    <svg
      className="hchart"
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      role="img"
      aria-label="Motion-count delta history"
      onMouseLeave={() => onHover?.(null)}
    >
      <line x1={0} y1={height - 1} x2={width} y2={height - 1} className="gl" />
      {baselineY !== null ? <line x1={0} y1={baselineY} x2={width} y2={baselineY} className="base" /> : null}
      {buckets.map((bucket, index) => {
        const x = index * (barWidth + barGap);
        if (bucket.is_gap) {
          return (
            <rect
              key={bucket.bucket_start}
              x={x}
              y={0}
              width={barWidth}
              height={height}
              className="gap"
              onMouseEnter={() => onHover?.(bucket)}
            />
          );
        }
        const delta = bucket.motion_delta ?? 0;
        const barHeight = Math.max(delta > 0 ? 1.5 : 1, (delta / maxDelta) * (height - 14));
        const spike = delta > maxDelta * 0.85 && delta > (baseline ?? 0) * 3;
        const cls = delta === 0 ? "b-zero" : delta < (baseline ?? 999999) ? "b-low" : spike ? "b-spike" : "b-move";
        return (
          <rect
            key={bucket.bucket_start}
            x={x}
            y={height - barHeight}
            width={barWidth}
            height={barHeight}
            className={cls}
            onMouseEnter={() => onHover?.(bucket)}
          />
        );
      })}
    </svg>
  );
}

export function historyChartLegend(): { label: string; className: string }[] {
  return [
    { label: "Moving", className: "b-move" },
    { label: "Low", className: "b-low" },
    { label: "No movement (0, packets received)", className: "b-zero" },
    { label: "Spike", className: "b-spike" },
    { label: "Gap — no packets received", className: "gap" },
  ];
}
