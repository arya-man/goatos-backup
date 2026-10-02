"use client";

// Feed mix on the template AnalyticsConversionRates card (overview/analytics). A client leaf only
// because the en-IN figure formatters are functions and cannot cross from the server page.
import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme, type Theme } from "@mui/material/styles";
import { AnalyticsConversionRates } from "@/components/minimal/sections/overview/analytics/analytics-conversion-rates";

const inr = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 0 });

/**
 * A feed name broken at word boundaries into lines of at most `width` characters (Apex draws an
 * array category as one line each). Truncating instead cut "Mesha Adult Concentrate Goat" /
 * "... Sheep" / "..." to three identical "Mesha Adult Concentrat…" rows (D2).
 */
export function wrapFeedLabel(label: string, width: number): string[] {
  const lines: string[] = [];
  for (const word of label.split(/\s+/).filter(Boolean)) {
    const last = lines[lines.length - 1];
    if (last !== undefined && last.length + 1 + word.length <= width) lines[lines.length - 1] = `${last} ${word}`;
    else lines.push(word);
  }
  return lines.length > 0 ? lines : [label];
}

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
            categories: rows.map((row) => wrapFeedLabel(row.label, phone ? 14 : 22)),
            labels: { formatter: (value: string) => inr(Number(value)), ...(phone ? { hideOverlappingLabels: true } : {}) },
            ...(phone ? { tickAmount: 2 } : {}),
          },
          yaxis: { labels: { maxWidth: phone ? 112 : 200 } },
          // Room past the longest bar for its figure (template grid values carried, the spread is shallow).
          grid: { strokeDashArray: 3, borderColor: theme.vars.palette.divider, padding: { top: 0, right: 48, bottom: 0 }, xaxis: { lines: { show: false } } },
          tooltip: { shared: true, intersect: false, y: { formatter: (value: number) => `${inr(value)} ${unit}`, title: { formatter: () => "" } } },
        },
      }}
      sx={{ height: 1 }}
    />
  );
}
