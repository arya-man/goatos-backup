"use client";

// Feed mix on the template AnalyticsConversionRates card (overview/analytics). A client leaf only
// because the en-IN figure formatters are functions and cannot cross from the server page.
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme, type Theme } from "@mui/material/styles";
import { AnalyticsConversionRates } from "@/components/minimal/sections/overview/analytics/analytics-conversion-rates";
import { chartClasses } from "@/components/minimal/chart/classes";
import { FEED_MIX_LABEL_WIDTH, feedMixChartHeight, feedMixLabelLines } from "./feed-mix-layout";

const inr = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });

export function FeedMixCard({ title, unit, rows }: { title: string; unit: string; rows: { label: string; value: number }[] }) {
  const theme = useTheme();
  // Phones: the kg axis gets three ticks (0 / half / top), Apex hides any that would still touch, and
  // the feed names give up some width to the plot, so "60,000" and "80,000" never print into each
  // other on a 390 card (guard: axis-label-overlap in r2 text-fit + feed-mix-phone-axis test).
  const phone = useMediaQuery((t: Theme) => t.breakpoints.down("sm"), { noSsr: true });
  // A name takes at most two lines and the chart is as tall as two lines per row needs, so wrapped
  // names never print over each other (PR #294 O1); the tooltip carries the full name.
  const labels = rows.map((row) => feedMixLabelLines(row.label, phone ? FEED_MIX_LABEL_WIDTH.phone : FEED_MIX_LABEL_WIDTH.laptop));
  const height = feedMixChartHeight(labels);
  return (
    <AnalyticsConversionRates
      component="section"
      aria-label={title}
      title={title}
      chart={{
        categories: rows.map((row) => row.label),
        series: [{ name: unit, data: rows.map((row) => row.value) }],
        options: {
          // The section spreads `options` shallowly, so each overridden key carries its template values.
          // The figure sits just PAST the bar's end in the text colour, never inside it: inside, a short
          // bar pushed its figure back across the feed names (D2).
          dataLabels: {
            enabled: true,
            textAnchor: "start",
            offsetX: 8,
            formatter: (value: number) => inr(value),
            style: { fontSize: String(theme.typography.caption.fontSize), colors: [theme.vars.palette.text.primary] },
          },
          xaxis: {
            categories: labels,
            labels: { formatter: (value: string) => inr(Number(value)), ...(phone ? { hideOverlappingLabels: true } : {}) },
            ...(phone ? { tickAmount: 2 } : {}),
          },
          yaxis: { labels: { maxWidth: phone ? 112 : 200 } },
          // Room past the longest bar for its figure (template grid values carried, the spread is shallow).
          grid: { strokeDashArray: 3, borderColor: theme.vars.palette.divider, padding: { top: 0, right: 48, bottom: 0 }, xaxis: { lines: { show: false } } },
          tooltip: {
            shared: true,
            intersect: false,
            x: { formatter: (_value: unknown, opts?: { dataPointIndex?: number }) => rows[opts?.dataPointIndex ?? -1]?.label ?? "" },
            y: { formatter: (value: number) => `${inr(value)} ${unit}`, title: { formatter: () => "" } },
          },
        },
      }}
      // The template section fixes its chart at 360px; the rows decide here (the wrapper root is the
      // template Chart's own class, not an .apexcharts-* part).
      sx={{ height: 1, [`& .${chartClasses.root}`]: { height } }}
    />
  );
}
