"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import type { HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { alpha } from "@mui/material/styles";
import { chartColor, useChartTheme } from "@/components/app/chart-colors";
import { Chart, useChart } from "@/components/minimal/chart";
import { niceCeiling } from "@/components/chart-scale";
import { fmtClockIst, fmtDelta, fmtRssi } from "./format";
import { EmptyState } from "@/components/app/empty-state";

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
  onHover,
  markers,
}: {
  buckets: HerdSignalTimelineBucket[];
  baseline?: number | null;
  height?: number;
  onHover?: (bucket: HerdSignalTimelineBucket | null) => void;
  // Other recorded farm activity (vaccination, feed, weighing, treatment, hoof trimming, shed
  // move), joined by tag/animal and time. Drawn as vertical lines at the event's REAL timestamp
  // (not snapped to a bucket) — correlation for a human to read, never cause. See the
  // correlation-not-cause copy in herd-signals-history-fullscreen.tsx, which this renders under.
  markers?: ChartMarker[];
}) {
  const theme = useChartTheme();
  const p = theme.palette;
  const rawMaxDelta = Math.max(1, ...buckets.map((bucket) => bucket.motion_delta ?? 0), baseline ?? 0);
  // Keep zero/gap-heavy tags from collapsing into a useless 1.0 / 0.7 / 0.3 / 0 axis. The mock's
  // mini chart always reads on a few-hundred-count scale, even when the selected tag is quiet.
  // A ROUND ceiling (1/1.2/1.6/2/2.4/3/4/5/6/8 x 10^n) so the four gridlines carry round figures:
  // 400 / 300 / 200 / 100, not 396 / 264 / 132.
  const maxDelta = niceCeiling(rawMaxDelta < 20 ? 400 : rawMaxDelta);
  // baseline_delta is the p75 of 300s (5-minute) buckets (Section 8), and the backend already
  // excludes every gap_delta reading from that p75 (a gap total is not an "ordinary active bucket").
  // Comparing it unscaled against a 3600s or 21600s bucket always reads "spike" (a bigger window
  // naturally accumulates more motion) and against a sub-300s bucket always reads "low" — neither is
  // a real signal, just a unit mismatch. Scale the baseline to each bucket's own width before
  // comparing or drawing it.
  const scaledBaseline = (bucketSeconds: number) => (baseline ? baseline * (bucketSeconds / 300) : null);
  const lineBaseline = scaledBaseline(buckets[0]?.bucket_seconds || 300);

  // One bar per bucket on a TIME axis (bucket start, received_at clock), so event markers sit at
  // their real timestamp instead of snapping to a bucket slot. A gap is a full-height danger band
  // (a hole in the data, never a zero); a zero delta is a flush-to-baseline muted bar; a reconnect
  // total is its own colour and never classified as a spike.
  const points = buckets.map((bucket) => {
    const x = new Date(bucket.bucket_start).getTime();
    if (bucket.is_gap) return { x, y: maxDelta, fillColor: alpha(p.error.main, 0.26) };
    const delta = bucket.motion_delta ?? 0;
    let fillColor: string;
    if (bucket.gap_delta) {
      fillColor = p.secondary.main;
    } else {
      const bucketBaseline = scaledBaseline(bucket.bucket_seconds);
      const spike = delta > maxDelta * 0.85 && delta > (bucketBaseline ?? 0) * 3;
      fillColor = delta === 0 ? p.grey[400] : delta < (bucketBaseline ?? 999999) ? p.grey[500] : spike ? p.warning.main : p.primary.main;
    }
    // A zero reading still draws a sliver on the baseline so "packets, no movement" stays visible.
    return { x, y: Math.max(delta, maxDelta * 0.015), fillColor };
  });
  const formatAxisTick = (value: number) => {
    if (maxDelta < 10) return value === 0 ? "0" : value.toFixed(1).replace(/\.0$/, "");
    return Math.round(value).toString();
  };

  // Bars = AnalyticsWebsiteVisits (useChart base options); no tooltip card — the readout under the
  // chart names the hovered bucket (ChartReadout), so the three facts are worded one way everywhere.
  const chartOptions = useChart({
    chart: {
      animations: { enabled: false },
      events: {
        dataPointMouseEnter: (_e, _c, opts) => onHover?.(buckets[opts?.dataPointIndex ?? -1] ?? null),
        mouseLeave: () => onHover?.(null),
      },
    },
    stroke: { width: 0 },
    plotOptions: { bar: { columnWidth: "92%", borderRadius: 0 } },
    tooltip: { enabled: false },
    grid: { padding: { left: 4, right: 4 } },
    xaxis: {
      type: "datetime",
      tickAmount: 6,
      labels: { rotate: 0, hideOverlappingLabels: true, formatter: (value: string | number) => fmtClockIst(new Date(Number(value)).toISOString()) },
      tooltip: { enabled: false },
    },
    // The drawer's mini chart is ~100px tall: zero, half and top only, or five ticks touch.
    yaxis: { min: 0, max: maxDelta, tickAmount: height < 160 ? 2 : 4, labels: { formatter: formatAxisTick } },
    annotations: {
      yaxis: lineBaseline
        ? [{ y: Math.min(lineBaseline, maxDelta), borderColor: p.grey[500], strokeDashArray: 4, label: { text: `baseline ${Math.round(lineBaseline)}`, borderWidth: 0, style: { background: "transparent", color: p.grey[500] } } }]
        : [],
      xaxis: (markers ?? []).map((marker) => ({ x: marker.atMs, borderColor: chartColor(theme, marker.color), strokeDashArray: 0 })),
    },
  });

  if (buckets.length === 0) {
    return <EmptyState title="No activity windows in this range yet. Buckets are written as packets arrive." />;
  }
  return (
    <Box sx={{ width: 1, height }} role="img" aria-label="Motion-count delta history" onMouseLeave={() => onHover?.(null)}>
      <Chart type="bar" series={[{ name: "delta", data: points }]} options={chartOptions} sx={{ height: 1 }} />
    </Box>
  );
}

export type HistoryLegendKey = "move" | "low" | "zero" | "spike" | "reconnect" | "gap" | "base";

// Legend swatches carry the same palette entries the chart bars draw with (points above).
export function historyChartLegend(): { label: string; key: HistoryLegendKey; color: string; opacity?: number; dashed?: boolean }[] {
  return [
    { label: "Moving", key: "move", color: "primary.main" },
    { label: "Low / quiet", key: "low", color: "grey.500" },
    { label: "No movement (packets seen, delta 0)", key: "zero", color: "grey.400" },
    { label: "Movement spike", key: "spike", color: "warning.main" },
    { label: "Reconnect — gap total, timing unknown", key: "reconnect", color: "secondary.main" },
    { label: "Missing signal (no packets)", key: "gap", color: "error.main", opacity: 0.35 },
    { label: "Animal baseline", key: "base", color: "grey.500", dashed: true },
  ];
}

/** The chart legend row (drawer mini chart and full-screen chart): swatch + label per bar kind. */
export function HistoryChartLegend({ omit = [], footer }: { omit?: HistoryLegendKey[]; footer?: ReactNode }) {
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", columnGap: 1.5, rowGap: 1, pt: 1.5 }}>
      {historyChartLegend()
        .filter((entry) => !omit.includes(entry.key))
        .map((entry) => (
          <Typography key={entry.key} variant="caption" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, color: "text.secondary" }}>
            <Box
              component="span"
              aria-hidden="true"
              sx={
                entry.dashed
                  ? { width: 10, borderTop: 2, borderTopStyle: "dashed", borderColor: entry.color }
                  : { width: 10, aspectRatio: "1", borderRadius: "var(--r-sm)", bgcolor: entry.color, opacity: entry.opacity ?? 1 }
              }
            />
            {entry.label}
          </Typography>
        ))}
      {footer ? (
        <Typography variant="caption" component="div" sx={{ flexBasis: "100%", color: "text.secondary" }}>
          {footer}
        </Typography>
      ) : null}
    </Box>
  );
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
    // No instruction line: the readout area is empty until a bucket is hovered (aria-live announces the reading).
    return null;
  }
  if (hovered.is_gap) {
    return (
      <>
        <b>{fmtClockIst(hovered.bucket_start)} IST</b> · <Box component="span" sx={{ color: "error.main" }}>Missing signal</Box> — no
        packets received in this window. Not the same as no movement.
      </>
    );
  }
  if (hovered.gap_delta) {
    const index = buckets ? buckets.findIndex((bucket) => bucket.bucket_start === hovered.bucket_start) : -1;
    const window = buckets && index >= 0 ? gapWindowForReconnect(buckets, index) : null;
    return (
      <>
        <b>{fmtClockIst(hovered.bucket_start)} IST</b> · <Box component="span" sx={{ color: "secondary.main" }}>Reconnect</Box> — delta{" "}
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
