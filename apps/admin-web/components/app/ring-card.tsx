"use client";

import type { CardProps } from "@mui/material/Card";

import { varAlpha } from "minimal-shared/utils";
import { chartColor, useChartTheme } from "@/components/app/chart-colors";

import { fNumber } from "@/components/minimal/_shared/format-number";
import { EcommerceSaleByGender } from "@/components/minimal/sections/overview/e-commerce/ecommerce-sale-by-gender";

// Ring card adapter: our shares on the VERBATIM template EcommerceSaleByGender (radial bars + legend).
// Everything goes in through the template's own props:
//  - colours: the locked Mesha pairs (primary, info, secondary) via `chart.colors`, never the
//    template's warning/error defaults (an ordinary share is not "at risk");
//  - the centre total's caption (backend copy) via `chart.options` (the template spreads it over its
//    options, so the radialBar block is restated whole with only the label added);
//  - each legend entry carries its own figure in its label ("Over 30 kg · 42").

// ApexCharts radialBar gaps are unit-less px (the template value), not a CSS length.
const RING_GAP = 10;

export type RingCardSeries = { label: string; value: number; display?: string };

export type RingCardProps = Omit<CardProps, "title"> & {
  title?: string;
  subheader?: string;
  /** The centre figure (a count). */
  total: number;
  /** Caption under the centre figure (the template prints "Total"). */
  totalLabel?: string;
  series: RingCardSeries[];
};

export function RingCard({ title, subheader, total, totalLabel, series, ...other }: RingCardProps) {
  // Through the chart theme: the legend dots follow the active scheme from the first paint (N4).
  const theme = useChartTheme();
  const colors = [
    [chartColor(theme, "primary.light"), chartColor(theme, "primary")],
    [chartColor(theme, "info.light"), chartColor(theme, "info")],
    [chartColor(theme, "secondary.light"), chartColor(theme, "secondary")],
  ];
  return (
    <EcommerceSaleByGender
      title={title}
      subheader={subheader}
      total={total}
      chart={{
        colors,
        series: series.map((item) => ({ label: item.display ? `${item.label} · ${item.display}` : item.label, value: item.value })),
        options: {
          plotOptions: {
            radialBar: {
              hollow: { margin: RING_GAP, size: "32%" },
              track: { margin: RING_GAP, background: varAlpha(theme.vars.palette.grey["500Channel"], 0.08) },
              dataLabels: {
                total: { ...(totalLabel ? { label: totalLabel } : {}), formatter: () => fNumber(total) },
                value: { offsetY: 2, fontSize: theme.typography.h5.fontSize as string },
                name: { offsetY: -10 },
              },
            },
          },
        },
      }}
      {...other}
    />
  );
}
