"use client";

// Horizontal bar card on the licensed MUI Minimal template's AnalyticsConversionRates
// (components/minimal/sections/overview/analytics/analytics-conversion-rates.tsx, verbatim; guard
// template-verbatim). The template card has no header action, empty state or footer, and formats
// every figure with fNumber, so this adapter adds only what our pages need, around the verbatim
// section and without touching it:
//  - one Card + CardHeader (title, subheader, the template chart-select `action`) with the verbatim
//    section inside as the chart body (its own header hidden, its card surface flat);
//  - `unit` / `digits` format the tooltip and value axis (the template's bar labels show the value
//    rounded to `digits`), a series may carry per-point
//    `notes` appended to the tooltip (a server page cannot pass formatter functions);
//  - the plot grows with its rows (never below the template's 360px); a caller may set its own
//    plot height through `sx` (`& .minimal__chart__root`), which wins over the default;
//  - `empty` renders instead of the chart when there are no categories; `children` render at the foot.

import type { ReactNode } from "react";
import type { CardProps } from "@mui/material/Card";
import type { SxProps, Theme } from "@mui/material/styles";

import Box from "@mui/material/Box";
import useMediaQuery from "@mui/material/useMediaQuery";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";

import type { ChartOptions } from "@/components/minimal/chart";
import { chartClasses } from "@/components/minimal/chart/classes";
import { AnalyticsConversionRates } from "@/components/minimal/sections/overview/analytics/analytics-conversion-rates";

/** Height of one category row, so a long list gets room instead of hairline bars. */
const ROW_HEIGHT = 36;

/** A unit ending in "·" (e.g. "₹·") is a PREFIX: "₹1,234"; any other unit follows the figure. */
export function formatBarValue(value: number | null | undefined, unit = "", digits = 0) {
  if (value == null || Number.isNaN(value)) return "–";
  const text = value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (unit.endsWith("·")) return `${value < 0 ? "−" : ""}${unit.slice(0, -1)}${text.replace("-", "")}`;
  return unit ? `${text} ${unit}` : text;
}

export type ConversionRatesCardProps = Omit<CardProps, "title"> & {
  title?: ReactNode;
  subheader?: ReactNode;
  action?: ReactNode;
  empty?: ReactNode;
  chart: {
    colors?: string[];
    categories?: string[];
    unit?: string;
    digits?: number;
    series: { name: string; data: (number | null)[]; notes?: (string | null)[] }[];
    options?: ChartOptions;
  };
};

/** The verbatim section as a flat chart body: no second surface, its own (empty) header hidden. */
const bodySx: SxProps<Theme> = {
  boxShadow: "none",
  bgcolor: "transparent",
  backgroundImage: "none",
  borderRadius: 0,
  overflow: "visible",
  "& > .MuiCardHeader-root": { display: "none" },
};

/** Breaks a tooltip title into lines of at most `width` characters (Apex sets the title as HTML). */
export function wrapTooltipTitle(label: string, width = 28): string {
  const escape = (text: string) => text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const lines: string[] = [];
  let line = "";
  for (const word of label.split(/\s+/).filter(Boolean)) {
    if (line && line.length + 1 + word.length > width) {
      lines.push(line);
      line = word;
    } else {
      line = line ? `${line} ${word}` : word;
    }
  }
  if (line) lines.push(line);
  return lines.map(escape).join("<br>");
}

export function ConversionRatesCard({ title, subheader, action, empty, chart, sx, children, ...other }: ConversionRatesCardProps) {
  const rows = chart.categories?.length ?? 0;
  const digits = chart.digits ?? 0;
  const plotHeight = Math.max(360, rows * ROW_HEIGHT * Math.max(1, chart.series.length) + 64);

  // `options` is spread shallowly over the template's, so each overridden key restates its template values.
  // Webview (TR1-#6): Apex places a horizontal bar's tooltip at the bar end and flips it left without
  // clamping, so a long pen label ran off a 390 viewport. On phones the tooltip is Apex's fixed one,
  // pinned to the plot's top-left corner (set at construction: noSsr, so the first render knows).
  const phone = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"), { noSsr: true });
  const options: ChartOptions = {
    tooltip: {
      shared: true,
      intersect: false,
      // A long label (pen · farm · arm) is also broken onto lines so the pinned tooltip fits the card.
      ...(phone ? { fixed: { enabled: true, position: "topLeft", offsetX: 0, offsetY: 0 }, x: { formatter: (label: string | number) => wrapTooltipTitle(String(label)) } } : {}),
      y: {
        formatter: (value: number, opts?: { seriesIndex: number; dataPointIndex: number }) => {
          const note = opts == null ? null : chart.series[opts.seriesIndex]?.notes?.[opts.dataPointIndex];
          const text = formatBarValue(value, chart.unit, digits);
          return note ? `${text} · ${note}` : text;
        },
        title: { formatter: (seriesName: string) => `${seriesName}: ` },
      },
    },
    xaxis: {
      categories: chart.categories,
      labels: { formatter: (value: string) => formatBarValue(Number(value), chart.unit, 0) },
      // Phones: four value ticks, so "200 g" / "250 g" never touch on a 390 plot (guard: axis-label-overlap).
      ...(phone ? { tickAmount: 4 } : {}),
    },
    ...chart.options,
  };

  return (
    <Card sx={[{ [`& .${chartClasses.root}`]: { height: plotHeight } }, ...(sx == null ? [] : Array.isArray(sx) ? sx : [sx])]} {...other}>
      <CardHeader title={title} subheader={subheader} action={action} />
      {rows > 0 ? (
        <AnalyticsConversionRates
          sx={bodySx}
          chart={{
            colors: chart.colors,
            categories: chart.categories,
            // The template's own bar labels print the raw value, so the data is rounded to `digits`;
            // Apex draws a null as a gap (the template types its data as number[]).
            series: chart.series.map((s) => ({ name: s.name, data: s.data.map((v) => (v == null ? v : Number(v.toFixed(digits)))) as number[] })),
            options,
          }}
        />
      ) : (
        <Box sx={{ p: 3 }}>{empty}</Box>
      )}
      {children}
    </Card>
  );
}
