"use client";

import type { CardProps } from "@mui/material/Card";
import type { ChartOptions } from "@/components/minimal/chart";

import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";

import { AnalyticsWebsiteVisits } from "@/components/minimal/sections/overview/analytics/analytics-website-visits";

// Column chart card adapter: our series on the VERBATIM template AnalyticsWebsiteVisits, through its
// own props only. Server pages cannot pass formatters, so `unit` / `digits` / per-point `notes` become
// the y-axis + tooltip formatters here and go in via `chart.options` (the template spreads it over its
// options, replacing its demo "visits" tooltip). A missing point is `null` and draws no bar. With no
// categories the card shows `empty` in the same Card + CardHeader anatomy (MUI parts).

export type ColumnChartSeries = { name: string; data: (number | null)[]; notes?: (string | null)[] };

export type ColumnChartCardProps = Omit<CardProps, "title"> & {
  title?: string;
  subheader?: string;
  empty?: React.ReactNode;
  /** @deprecated same as chart.unit */
  valueNoun?: string;
  chart: {
    colors?: string[];
    categories?: string[];
    series: ColumnChartSeries[];
    /** A unit ending in "·" (e.g. "₹·") is a PREFIX: "₹1,234"; any other unit follows the figure. */
    unit?: string;
    digits?: number;
    options?: ChartOptions;
  };
};

function formatValue(value: number | null | undefined, unit = "", digits = 0) {
  if (value == null || Number.isNaN(value)) return "–";
  const text = value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (unit.endsWith("·")) return `${value < 0 ? "−" : ""}${unit.slice(0, -1)}${text.replace("-", "")}`;
  return unit ? `${text} ${unit}` : text;
}

export function ColumnChartCard({ title, subheader, empty, valueNoun, chart, sx, ...other }: ColumnChartCardProps) {
  const unit = chart.unit ?? valueNoun ?? "";
  if ((chart.categories?.length ?? 0) === 0) {
    return (
      <Card sx={sx} {...other}>
        <CardHeader title={title} subheader={subheader} />
        <Box sx={{ p: 3 }}>{empty}</Box>
      </Card>
    );
  }
  const options: ChartOptions = {
    yaxis: { labels: { formatter: (value: number) => formatValue(value, chart.unit ?? "", 0) } },
    tooltip: {
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : chart.series[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatValue(value, unit, chart.digits ?? 0);
          return note ? `${text} · ${note}` : text;
        },
      },
    },
    ...chart.options,
  };
  return (
    <AnalyticsWebsiteVisits
      title={title}
      subheader={subheader}
      chart={{
        colors: chart.colors,
        categories: chart.categories,
        series: chart.series.map((s) => ({ name: s.name, data: s.data as number[] })),
        options,
      }}
      sx={sx}
      {...other}
    />
  );
}
