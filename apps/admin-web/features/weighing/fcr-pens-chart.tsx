"use client";
// telemetry:exempt pure presentational chart; the FCR tab and its route error boundary own telemetry.

import useMediaQuery from "@mui/material/useMediaQuery";
import { useTheme } from "@mui/material/styles";
import type { ReactNode } from "react";

import { ConversionRatesCard, formatBarValue, wrapTooltipTitle } from "@/components/app/conversion-rates-card";

import { fcrAxisCeiling, isOffScale } from "./fcr-scale";

/**
 * FCR by pen as horizontal bars, with two fixes the shared card cannot make on its own:
 *
 * 1. ONE RUNAWAY PEN DOES NOT FLATTEN THE REST. The value axis is capped (fcrAxisCeiling); a pen past
 *    the cap is drawn to the edge, its label and tooltip still print its TRUE ratio, marked off scale.
 * 2. THE BREAK-EVEN LABEL SITS ABOVE THE PLOT, so it never paints over a bar's value label
 *    ("8.13" read ".13").
 *
 * Client-side because the axis and label formatters are functions, which cannot cross from the
 * server-rendered tab.
 */
export function FCRPensChart({
  ariaLabel,
  title,
  subheader,
  empty,
  categories,
  values,
  notes,
  seriesName,
  unit,
  breakEven,
  breakEvenLabel,
  offScaleLabel,
  children,
}: {
  ariaLabel: string;
  title: string;
  subheader?: string;
  empty: ReactNode;
  categories: string[];
  values: number[];
  notes: string[];
  seriesName: string;
  unit: string;
  breakEven: number | null;
  /** Fully composed ("Break-even: 7.5 kg/kg"). */
  breakEvenLabel: string;
  /** Backend word for a bar drawn cut at the ceiling ("off scale"). */
  offScaleLabel: string;
  children?: ReactNode;
}) {
  const theme = useTheme();
  const phone = useMediaQuery(theme.breakpoints.down("sm"), { noSsr: true });
  const ceiling = fcrAxisCeiling(values, breakEven);
  const drawn = values.map((value) => (ceiling !== undefined && value > ceiling ? ceiling : value));
  const trueText = (index: number) => {
    const value = values[index];
    const text = formatBarValue(value, unit, 2);
    return isOffScale(value, ceiling) ? `${text} · ${offScaleLabel}` : text;
  };
  return (
    <ConversionRatesCard
      aria-label={ariaLabel}
      title={title}
      subheader={subheader}
      empty={empty}
      chart={{
        categories,
        unit,
        digits: 2,
        series: [{ name: seriesName, data: drawn, notes }],
        options: {
          xaxis: {
            categories,
            // A ratio is never below zero, so the axis starts there (Apex padded it to "-5 kg/kg"). Four
            // ticks on a laptop; two on a phone, where four "25 kg/kg" labels ran into one another.
            min: 0,
            tickAmount: phone ? 2 : 4,
            ...(ceiling !== undefined ? { max: ceiling } : {}),
            labels: { formatter: (value: string) => formatBarValue(Number(value), unit, 0) },
          },
          dataLabels: {
            enabled: true,
            offsetX: -6,
            style: { fontSize: String(theme.typography.caption.fontSize), colors: [theme.vars.palette.common.white, theme.vars.palette.text.primary] },
            formatter: (_value: number, opts?: { dataPointIndex: number }) =>
              opts == null ? "" : formatBarValue(values[opts.dataPointIndex], "", 2),
          },
          tooltip: {
            shared: true,
            intersect: false,
            // The shared card's phone placement, restated because this tooltip replaces its own.
            ...(phone
              ? { fixed: { enabled: true, position: "topLeft", offsetX: 0, offsetY: 0 }, x: { formatter: (label: string | number) => wrapTooltipTitle(String(label)) } }
              : {}),
            y: {
              formatter: (_value: number, opts?: { dataPointIndex: number }) => {
                if (opts == null) return "";
                const note = notes[opts.dataPointIndex];
                return note ? `${trueText(opts.dataPointIndex)} · ${note}` : trueText(opts.dataPointIndex);
              },
              title: { formatter: (name: string) => `${name}: ` },
            },
          },
          ...(breakEven != null
            ? {
                annotations: {
                  // BEHIND the bars: a bar ending past break-even hides the dash under its own fill, so
                  // the dashed line never runs through the value label printed at the bar's end.
                  position: "back",
                  // The line's label sits ABOVE the plot (horizontal, offset up), clear of every bar's
                  // value label; drawn along the line it painted over "8.13" so it read ".13".
                  xaxis: [
                    {
                      x: breakEven,
                      strokeDashArray: 4,
                      label: { text: breakEvenLabel, orientation: "horizontal", position: "top", offsetY: -12 },
                    },
                  ],
                },
              }
            : {}),
        },
      }}
      sx={{ height: 1 }}
    >
      {children}
    </ConversionRatesCard>
  );
}
