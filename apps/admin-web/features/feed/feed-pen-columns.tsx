"use client";

// telemetry:exempt presentational chart wrapper — no user action, no data read

import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
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
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "minmax(0, 1fr)", md: "repeat(2, minmax(0, 1fr))", xl: "repeat(3, minmax(0, 1fr))" }, gap: 1.75, mx: { xs: 2.5, sm: 3 }, mb: { xs: 2.5, sm: 3 } }}>
      {/* `tabIndex={0}` on each pen is main's fix (56b3da919, "make vaccination schedule scroll on
          mobile"): `.penbars` can scroll at phone width, and a scroll container nothing can focus
          cannot be scrolled from a keyboard. It survives the move into this component. */}
      {pens.map((pen) => (
        <Box
          key={pen.key}
          role="group"
          tabIndex={0}
          aria-label={pen.ariaLabel}
          sx={{ border: 1, borderColor: "divider", borderRadius: 1, px: 1.5, pt: 1.25, pb: 1, bgcolor: "background.neutral", minWidth: 0 }}
        >
          <Box sx={{ display: "flex", alignItems: "baseline", justifyContent: "space-between", gap: 1, mb: 0.75 }}>
            <Typography variant="subtitle2" component="span">
              {pen.title}
              {pen.park ? (
                <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
                  {" "}· {pen.park}
                </Typography>
              ) : null}
            </Typography>
            <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
              {unitLabel}
            </Typography>
          </Box>
          <Box sx={{ mx: -0.5 }}>
            <TrendChart
              kind="bar"
              height={220}
              data={pen.days.map((day) => ({ day: day.day, label: day.label, directed: day.directed, verified: day.verified, detail: day.detail }))}
              xKey="label"
              series={[
                { key: "directed", label: directedLabel, color: "var(--palette-primary-main)" },
                { key: "verified", label: verifiedLabel, color: "var(--palette-info-main)" },
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
        </Box>
      ))}
    </Box>
  );
}

/** The smallest 1 / 2 / 2.5 / 5 × 10ⁿ at or above `value` (≥ 1). */
function niceCeiling(value: number): number {
  const v = Math.max(1, value);
  const step = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 2.5, 5, 10]) if (m * step >= v) return m * step;
  return 10 * step;
}
