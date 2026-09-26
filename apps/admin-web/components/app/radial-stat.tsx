"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { useTheme } from "@mui/material/styles";
import { Chart, useChart } from "@/components/minimal/chart";
import { cx, type KitTone } from "@/lib/tone";

export type RadialStatProps = {
  /** 0-100. */
  value: number;
  size?: number;
  tone?: KitTone;
  color?: string;
  centerLabel?: string;
  caption?: ReactNode;
  digits?: number;
  className?: string;
};

const PALETTE_KEY = { primary: "primary", info: "info", success: "success", warning: "warning", error: "error", violet: "secondary", neutral: null } as const;

export function RadialStat({ value, size = 112, tone = "primary", color, centerLabel, caption, digits = 0, className }: RadialStatProps) {
  const theme = useTheme();
  const pct = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0));
  const key = PALETTE_KEY[tone];
  const stops = color ? [color, color] : key ? [theme.vars.palette[key].light, theme.vars.palette[key].main] : [theme.vars.palette.text.secondary, theme.vars.palette.text.primary];
  const shown = Number(pct.toFixed(digits));
  const label = centerLabel ?? `${shown.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits })}%`;
  const chartOptions = useChart({
    chart: { sparkline: { enabled: true } },
    stroke: { width: 0 },
    fill: {
      type: "gradient",
      gradient: {
        colorStops: [
          { offset: 0, color: stops[0], opacity: 1 },
          { offset: 100, color: stops[1], opacity: 1 },
        ],
      },
    },
    plotOptions: {
      radialBar: {
        dataLabels: {
          name: { show: false },
          value: { offsetY: 6, fontSize: theme.typography.subtitle2.fontSize as string, fontWeight: theme.typography.subtitle2.fontWeight, formatter: () => label },
          total: { show: false },
        },
      },
    },
  });
  return (
    <Box className={cx("kit-radial", className)} role="img" aria-label={label} sx={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 1.5 }}>
      <Chart type="radialBar" series={[shown]} options={chartOptions} deps={[label]} sx={{ width: size, height: size }} />
      {caption ? (
        <Typography variant="caption" sx={{ color: "text.secondary", textAlign: "center", lineHeight: 1.4 }}>
          {caption}
        </Typography>
      ) : null}
    </Box>
  );
}
