"use client";

// Feed mix on the template AnalyticsConversionRates card (overview/analytics). A client leaf only
// because the en-IN figure formatters are functions and cannot cross from the server page.
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme, type Theme } from "@mui/material/styles";
import { AnalyticsConversionRates } from "@/components/minimal/sections/overview/analytics/analytics-conversion-rates";

const inr = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });

export function FeedMixCard({ title, unit, rows }: { title: string; unit: string; rows: { label: string; value: number }[] }) {
  const theme = useTheme();
  // Phones: the kg axis gets three ticks (0 / half / top), Apex hides any that would still touch, and
  // the feed names give up some width to the plot, so "60,000" and "80,000" never print into each
  // other on a 390 card (guard: axis-label-overlap in r2 text-fit + feed-mix-phone-axis test).
  const phone = useMediaQuery((t: Theme) => t.breakpoints.down("sm"), { noSsr: true });
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
          dataLabels: { enabled: true, offsetX: -6, formatter: (value: number) => inr(value), style: { fontSize: String(theme.typography.caption.fontSize) } },
          xaxis: {
            categories: rows.map((row) => row.label),
            labels: { formatter: (value: string) => inr(Number(value)), ...(phone ? { hideOverlappingLabels: true } : {}) },
            ...(phone ? { tickAmount: 2 } : {}),
          },
          ...(phone ? { yaxis: { labels: { maxWidth: 112 } } } : {}),
          tooltip: { shared: true, intersect: false, y: { formatter: (value: number) => `${inr(value)} ${unit}`, title: { formatter: () => "" } } },
        },
      }}
      sx={{ height: 1 }}
    />
  );
}
