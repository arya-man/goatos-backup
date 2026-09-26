"use client";

// Composed from the licensed MUI Minimal template widget summaries (sections/overview: e-commerce,
// banking, analytics, booking check-in); see KpiCard below for which card maps to which source.

import type { ReactNode } from "react";
import { varAlpha } from "minimal-shared/utils";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardActionArea from "@mui/material/CardActionArea";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import type { Theme } from "@mui/material/styles";
import Link from "@/components/no-prefetch-link";
import { WidgetSparkChart } from "@/components/minimal/widgets/widget-spark-chart";
import { Iconify } from "@/components/minimal/iconify";
import { SvgColor } from "@/components/minimal/svg-color";
import { MINIMAL_ASSETS } from "@/components/minimal/_shared/config";
import type { PaletteColorKey } from "@/theme/core";
import { CountUp } from "@/components/app/count-up";
import { cx, toneVars, type KitTone } from "@/lib/tone";

type TrendReading = {
  value: number | null | undefined;
  suffix?: string;
  display?: ReactNode;
  invert?: boolean;
  caption?: ReactNode;
  digits?: number;
};

import type { KpiPart } from "./kpi-parts";

export type { KpiPart };

export type KpiCardProps = {
  label: ReactNode;
  /** Number animates with count-up; a string/node renders as-is. */
  value: number | string | ReactNode;
  format?: (n: number) => string;
  digits?: number;
  /** Small muted unit after the number, e.g. "kg". */
  unit?: ReactNode;
  icon?: ReactNode;
  tone?: KitTone;
  trend?: TrendReading;
  sparkline?: number[];
  /** Muted helper line under the value. */
  hint?: ReactNode;
  /**
   * Two readings in one card, each with its own label (template BookingCheckInWidgets split).
   * Replaces `value`; never write "54 · 7" into one figure.
   */
  parts?: KpiPart[];
  /**
   * - "plain" (default) — template EcommerceWidgetSummary (chart right) / BankingWidgetSummary badge.
   * - "tint" / "gradient" — template AnalyticsWidgetSummary (tone gradient, icon on top).
   */
  variant?: "plain" | "tint" | "gradient";
  /** Kept for call-site compatibility; the AnalyticsWidgetSummary shape art replaces it. */
  watermark?: ReactNode;
  /** Mini chart shape. Bars are the default: a KPI is a count per period, not a sampled curve. */
  sparkVariant?: "bar" | "line";
  onClick?: () => void;
  href?: string;
  className?: string;
  footer?: ReactNode;
};

const PALETTE: Record<Exclude<KitTone, "neutral">, PaletteColorKey> = {
  primary: "primary",
  info: "info",
  success: "success",
  warning: "warning",
  error: "error",
  violet: "secondary",
};

function paletteOf(tone: KitTone): PaletteColorKey | null {
  return tone === "neutral" ? null : PALETTE[tone];
}

/** Template trending row (EcommerceWidgetSummary): round tinted arrow, subtitle2 figure, body2 caption. */
function Trending({ trend }: { trend: NonNullable<KpiCardProps["trend"]> }) {
  const { value, suffix = "%", display, invert, caption, digits = 1 } = trend;
  if (value == null || Number.isNaN(value)) return null;
  const good = value === 0 ? null : (value > 0) !== Boolean(invert);
  const key: PaletteColorKey | null = good === null ? null : good ? "success" : "error";
  const text = display ?? `${value > 0 ? "+" : ""}${value.toFixed(digits)}${suffix}`;
  return (
    <Box sx={{ gap: 0.5, display: "flex", alignItems: "center", flexWrap: "wrap" }}>
      <Box
        component="span"
        sx={(theme: Theme) => ({
          width: 24,
          height: 24,
          display: "flex",
          borderRadius: "50%",
          alignItems: "center",
          justifyContent: "center",
          bgcolor: key ? varAlpha(theme.vars.palette[key].mainChannel, 0.16) : varAlpha(theme.vars.palette.grey["500Channel"], 0.16),
          color: key ? `${key}.dark` : "text.secondary",
          ...(key ? theme.applyStyles("dark", { color: `${key}.light` }) : {}),
        })}
      >
        <Iconify width={16} icon={value < 0 ? "eva:trending-down-fill" : "eva:trending-up-fill"} />
      </Box>
      <Box component="span" sx={{ typography: "subtitle2" }}>
        {text}
      </Box>
      {caption ? (
        <Box component="span" sx={{ color: "text.secondary", typography: "body2" }}>
          {caption}
        </Box>
      ) : null}
    </Box>
  );
}

function Figure({ value, format, digits, unit, variant }: Pick<KpiCardProps, "value" | "format" | "digits" | "unit"> & { variant: "h3" | "h4" }) {
  return (
    // Phone two-up decks (390px) step the figure down one size so a lakh-grouped number fits.
    <Box className="kit-kpi-value" sx={{ typography: { xs: "h4", sm: variant }, display: "flex", alignItems: "baseline", flexWrap: "wrap", gap: 0.5, minWidth: 0, "& > span": { minWidth: 0, overflowWrap: "anywhere" } }}>
      {typeof value === "number" ? <CountUp value={value} format={format} digits={digits} /> : value}
      {unit ? (
        <Box component="span" sx={{ typography: "subtitle2", color: "text.secondary" }}>
          {unit}
        </Box>
      ) : null}
    </Box>
  );
}

function Hint({ hint }: { hint: ReactNode }) {
  return (
    <Box sx={{ typography: "body2", color: "text.secondary", mt: 0.5, overflowWrap: "anywhere" }} title={typeof hint === "string" ? hint : undefined}>
      {hint}
    </Box>
  );
}

/**
 * THE KPI card, built from the template widget summaries (never a local design):
 * - plain + series: EcommerceWidgetSummary (title, h3 figure, trending row, mini chart right);
 * - plain + icon: the same body with BankingWidgetSummary's 48px round badge;
 * - tint / gradient: AnalyticsWidgetSummary (icon on top, tone gradient + shape art, h4 figure);
 * - two readings (`parts`): BookingCheckInWidgets split (h5 figure + body2 label, dashed divider).
 */
export function KpiCard({ label, value, format, digits, unit, icon, tone = "primary", trend, sparkline, hint, parts, variant = "plain", sparkVariant = "bar", onClick, href, className, footer }: KpiCardProps) {
  const color = paletteOf(tone);
  const hasChart = Boolean(sparkline && sparkline.length > 1);
  const hero = variant !== "plain";

  const splitBody = parts && parts.length > 1 ? (
    // Side by side when the card has room; a phone two-up card wraps the halves under each other.
    <Stack
      direction="row"
      divider={<Divider flexItem orientation="vertical" sx={{ borderStyle: "dashed", display: { xs: "none", sm: "block" } }} />}
      sx={{ mt: 1.5, gap: { xs: 1, sm: 2 }, flexWrap: { xs: "wrap", sm: "nowrap" } }}
    >
      {parts.map((part, index) => (
        <Box key={index} sx={{ minWidth: { xs: 96, sm: 0 }, flex: "1 1 0" }}>
          <Box className="kit-kpi-value" sx={{ mb: 0.5, typography: "h5" }}>
            {typeof part.value === "number" ? <CountUp value={part.value} format={format} digits={digits} /> : part.value}
          </Box>
          <Box sx={{ typography: "body2", color: "text.secondary" }}>{part.label}</Box>
        </Box>
      ))}
    </Stack>
  ) : null;

  const body = hero ? (
    <>
      {icon ? <Box sx={{ width: 48, height: 48, mb: { xs: 1.5, sm: 3 }, display: "flex", alignItems: "center", "& svg": { width: 32, height: 32 } }}>{icon}</Box> : null}
      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "flex-end", justifyContent: "flex-end" }}>
        <Box sx={{ flexGrow: 1, minWidth: 112 }}>
          <Box sx={{ mb: 1, typography: "subtitle2" }}>{label}</Box>
          {splitBody ?? <Figure value={value} format={format} digits={digits} unit={unit} variant="h4" />}
          {trend ? <Box sx={{ mt: 1 }}><Trending trend={trend} /></Box> : null}
          {hint ? <Hint hint={hint} /> : null}
        </Box>
        {hasChart ? <WidgetSparkChart data={sparkline!} variant="line" color={toneVars(tone).solid} /> : null}
      </Box>
      {color ? (
        <SvgColor
          src={`${MINIMAL_ASSETS}/background/shape-square.svg`}
          sx={{ top: 0, left: -20, width: 240, zIndex: -1, height: 240, opacity: 0.24, position: "absolute", color: `${color}.main` }}
        />
      ) : null}
      {footer}
    </>
  ) : (
    <>
      {/* Mini chart sits right of the figure; in a narrow phone two-up card it wraps under it. */}
      <Box sx={{ display: "flex", flexWrap: hasChart ? "wrap" : "nowrap", alignItems: "center", gap: 2 }}>
        <Box sx={{ flex: hasChart ? "1 1 140px" : "1 1 auto", minWidth: 0 }}>
          <Box sx={{ typography: "subtitle2" }}>{label}</Box>
          {splitBody ?? (
            <Box sx={{ my: 1.5 }}>
              <Figure value={value} format={format} digits={digits} unit={unit} variant="h3" />
            </Box>
          )}
          {trend ? <Trending trend={trend} /> : null}
          {hint ? <Hint hint={hint} /> : null}
        </Box>
        {hasChart ? (
          <WidgetSparkChart data={sparkline!} variant={sparkVariant} color={toneVars(tone).solid} />
        ) : icon ? (
          <Box
            component="span"
            sx={(theme: Theme) => ({
              width: 48,
              height: 48,
              flexShrink: 0,
              alignSelf: "flex-start",
              borderRadius: "50%",
              // Hidden in a phone two-up deck: the badge would squeeze the label to a word per line.
              display: { xs: "none", sm: "inline-flex" },
              alignItems: "center",
              justifyContent: "center",
              color: color ? `${color}.main` : "text.secondary",
              bgcolor: varAlpha(color ? theme.vars.palette[color].mainChannel : theme.vars.palette.grey["500Channel"], 0.16),
              "& svg": { width: 24, height: 24 },
            })}
          >
            {icon}
          </Box>
        ) : null}
      </Box>
      {footer}
    </>
  );

  const cardSx = hero
    ? (theme: Theme) => ({
        p: { xs: 2, sm: 3 },
        height: 1,
        boxShadow: "none",
        position: "relative" as const,
        isolation: "isolate" as const,
        ...(color
          ? {
              color: `${color}.darker`,
              backgroundColor: "common.white",
              backgroundImage: `linear-gradient(135deg, ${varAlpha(theme.vars.palette[color].lighterChannel, 0.48)}, ${varAlpha(theme.vars.palette[color].lightChannel, 0.48)})`,
            }
          : {}),
      })
    : { p: { xs: 2, sm: 3 }, height: 1 };

  // The template's AnalyticsWidgetSummary stays pastel in dark mode: its contents (hint, footer
  // controls) read the light scheme so they keep contrast on the light card.
  const rootClass = cx("kit-kpi", hero && color && "kit-scheme-light", className);
  const scheme = hero && color ? { "data-theme": "light" } : {};
  if (href || onClick) {
    return (
      <Card className={rootClass} {...scheme} sx={hero ? [cardSx as never, { p: 0 }] : { height: 1 }}>
        <CardActionArea
          {...(href ? { component: Link, href } : { onClick })}
          // A button centres its content vertically; KPI content starts at the top like the static card.
          sx={{ p: { xs: 2, sm: 3 }, height: 1, display: "flex", flexDirection: "column", alignItems: "stretch", justifyContent: "flex-start", textAlign: "left", position: "relative", isolation: "isolate" }}
        >
          {body}
        </CardActionArea>
      </Card>
    );
  }
  return (
    <Card className={rootClass} {...scheme} sx={cardSx}>
      {body}
    </Card>
  );
}
