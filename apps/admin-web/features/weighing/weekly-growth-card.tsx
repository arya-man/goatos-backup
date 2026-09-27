"use client";

// ADG Analytics "Weekly growth" on the template AnalyticsWebsiteVisits card. A client leaf only
// because the tooltip formatters are functions and cannot cross from the server page.
//
// The week's animal count rides in the tooltip TITLE ("24/08/2026 · 48 animals") and the row keeps
// only the gain ("Daily gain: 206 g"). With both in the row the tooltip grew to ~220px, and at 390px
// Apex flipped it left of a middle bar and out past the card edge, where the card clipped it
// (r2 audit chart-hover|tooltip-clipped, which now hovers seven points across the plot).
import type { SxProps, Theme } from "@mui/material/styles";

import { EmptyState } from "@/components/app/empty-state";
import { ColumnChartCard } from "@/components/app/column-chart-card";

export type WeeklyGrowthPoint = { label: string; gain: number | null; animalsLabel: string };

const grams = (value: number | null | undefined) =>
  value == null || Number.isNaN(value) ? "–" : `${value.toLocaleString("en-IN", { maximumFractionDigits: 0 })} g`;

export function WeeklyGrowthCard({
  ariaLabel,
  title,
  subheader,
  emptyLabel,
  seriesName,
  points,
  sx,
}: {
  ariaLabel: string;
  title: string;
  subheader: string;
  emptyLabel: string;
  seriesName: string;
  points: readonly WeeklyGrowthPoint[];
  sx?: SxProps<Theme>;
}) {
  return (
    <ColumnChartCard
      aria-label={ariaLabel}
      title={title}
      subheader={subheader}
      empty={<EmptyState title={emptyLabel} />}
      chart={{
        categories: points.map((point) => point.label),
        unit: "g",
        series: [{ name: seriesName, data: points.map((point) => point.gain) }],
        options: {
          // `options` replaces the section's tooltip key whole, so the gain formatter is restated here.
          tooltip: {
            x: {
              formatter: (_value: number, opts?: { dataPointIndex: number }) => {
                const point = opts == null ? undefined : points[opts.dataPointIndex];
                return point ? `${point.label} · ${point.animalsLabel}` : "";
              },
            },
            y: { formatter: (value: number) => grams(value) },
          },
        },
      }}
      sx={sx}
    />
  );
}
