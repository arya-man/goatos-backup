"use client";

// telemetry:exempt presentational chart wrapper — no user action, no data read

import Box from "@mui/material/Box";
import { TrendChart } from "@/components/app/trend-chart";

/**
 * One day of one pen, as the server composed it: the two figures as served (null = absent, never
 * 0) and the day's tooltip detail lines already written from backend copy.
 */
export type PenColumnDay = {
  day: string;
  label: string;
  directed: number | null;
  verified: number | null;
  detail: string;
};

export type PenColumnChart = {
  key: string;
  /** Backend-composed location, rendered verbatim: "Castro 1". */
  title: string;
  /** The pen's farm, printed beside its name so pens with a shared name stay apart. */
  park?: string;
  ariaLabel: string;
  days: PenColumnDay[];
};

/**
 * The "Feed by pen" charts as kit column charts: two series (directed / verified) per day, figures
 * on hover only, draw-in on first sight, and ONE value axis shared by every pen of the name
 * (`yMax`), so a taller bar always means more feed. A client component only because the kit chart
 * is one; every string and number arrives composed from the server.
 */
export function FeedPenColumns({
  pens,
  yMax,
  directedLabel,
  verifiedLabel,
  unitLabel,
  missingLabel,
}: {
  pens: PenColumnChart[];
  /** Top of the shared value axis (the tallest figure across every drawn pen, both series). */
  yMax: number;
  directedLabel: string;
  verifiedLabel: string;
  unitLabel: string;
  /** What the tooltip prints for a day with no verified figure. */
  missingLabel: string;
}) {
  const grams = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });
  // A round top for the shared axis (1,706 → 2,000) so the gridlines carry round figures; the
  // tallest bar still fits under it and every pen still shares it.
  const top = niceCeiling(yMax);
  return (
    <div className="qgrid penbars-grid">
      {/* `tabIndex={0}` on each pen is main's fix (56b3da919, "make vaccination schedule scroll on
          mobile"): `.penbars` can scroll at phone width, and a scroll container nothing can focus
          cannot be scrolled from a keyboard. It survives the move into this component. */}
      {pens.map((pen) => (
        <div className="penbars" key={pen.key} role="group" tabIndex={0} aria-label={pen.ariaLabel}>
          <div className="penbars-title">
            <b>{pen.title}</b>
            {pen.park ? <span className="small muted"> · {pen.park}</span> : null}
            <span className="small muted">{unitLabel}</span>
          </div>
          <Box sx={{ mx: -0.5 }}>
            <TrendChart
              kind="bar"
              height={220}
              data={pen.days.map((day) => ({ day: day.day, label: day.label, directed: day.directed, verified: day.verified, detail: day.detail }))}
              xKey="label"
              xLines
              series={[
                { key: "directed", label: directedLabel, color: "var(--primary)" },
                { key: "verified", label: verifiedLabel, color: "var(--info)" },
              ]}
              showLegend={false}
              yDomain={[0, top]}
              valueFormat={grams}
              integerY
              yWidth={40}
              missingLabel={missingLabel}
              detailKey="detail"
              hideZeroInTip
            />
          </Box>
        </div>
      ))}
    </div>
  );
}

/** The smallest 1 / 2 / 2.5 / 5 × 10ⁿ at or above `value` (≥ 1). */
function niceCeiling(value: number): number {
  const v = Math.max(1, value);
  const step = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * step >= v) return m * step;
  return 10 * step;
}
