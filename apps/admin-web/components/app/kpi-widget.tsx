import type { ElementType } from "react";
import type { SxProps, Theme } from "@mui/material/styles";
import type { PaletteColorKey } from "@/theme/core";

import Box from "@mui/material/Box";
import MuiLink from "@mui/material/Link";
import Typography from "@mui/material/Typography";

import Link from "@/components/no-prefetch-link";
import { Iconify } from "@/components/minimal/iconify";
import { AppWidgetSummary } from "@/components/minimal/sections/overview/app/app-widget-summary";
import { BookingWidgetSummary } from "@/components/minimal/sections/overview/booking/booking-widget-summary";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";

// KPI adapter: maps one Goat OS figure onto a VERBATIM template widget summary (guard
// template-verbatim). It picks the template card by the comparison the figure really has, so the
// template's own period text is true:
//  - trend.period "week"  -> EcommerceWidgetSummary (prints "<±x%> last week" + line sparkline);
//  - trend.period "7d"    -> AppWidgetSummary (prints "<±x%> last 7 days" + bar sparkline);
//  - trend.period "month" -> BookingWidgetSummary (prints "<±x%>" with no period text, no sparkline;
//                            the monthly series itself is drawn by the page's chart card);
//  - no trend             -> CourseWidgetSummary (figure, title, template corner icon in `color`).
// The template figure is a number (fNumber / fShortenNumber). Anything else the contract says about
// it (unit, "of 30", the no-data text, detail) is the `caption`, which the adapter renders VISIBLY as
// a sub-line inside the card (template card keeps its markup; the adapter adds room via `sx`).

export type KpiIcon = "progress" | "completed" | "certificates";

export type KpiTrend = {
  /** Percent change over the period named by `period`. */
  percent: number;
  period: "week" | "7d" | "month";
  /** The series behind the figure (required for week / 7d; ignored for month). */
  series?: number[];
  categories?: string[];
};

export type KpiWidgetProps = {
  /** Contract label, unchanged. */
  title: string;
  /** The figure. null = unavailable: the template prints nothing and `caption` carries the no-data text. */
  total: number | null | undefined;
  /** Visible sub-line: unit / remainder first ("kg", "of 30 deaths", "₹ lakh"), then detail. */
  caption?: string;
  color?: PaletteColorKey;
  icon?: KpiIcon;
  trend?: KpiTrend | null;
  href?: string;
  /** Link component for `href` (e.g. LocalOverlayLink for a same-page overlay). Default: no-prefetch Link. */
  linkComponent?: ElementType;
  sx?: SxProps<Theme>;
  "data-testid"?: string;
};

const ICONS: Record<KpiIcon, string> = {
  progress: "/minimal/icons/courses/ic-courses-progress.svg",
  completed: "/minimal/icons/courses/ic-courses-completed.svg",
  certificates: "/minimal/icons/courses/ic-courses-certificates.svg",
};

/** Our semantic tone -> the template palette key the widget takes (neutral reads as primary). */
export function kpiColor(tone: string | undefined): PaletteColorKey {
  if (!tone || tone === "neutral") return "primary";
  if (tone === "violet") return "secondary";
  return tone as PaletteColorKey;
}

function defaultIcon(color: PaletteColorKey | undefined): KpiIcon {
  if (color === "success") return "completed";
  if (color === "info" || color === "secondary") return "certificates";
  return "progress";
}

/** Percent change of the last point against the one before it (null when not comparable). */
export function lastStepPercent(series: readonly (number | null | undefined)[]): number | null {
  const values = series.filter((v): v is number => typeof v === "number" && Number.isFinite(v));
  if (values.length < 2) return null;
  const prev = values[values.length - 2];
  const cur = values[values.length - 1];
  if (prev === 0) return null;
  return Math.round(((cur - prev) / Math.abs(prev)) * 1000) / 10;
}

/** Sum of the last 7 points against the 7 before them, as a percent (null without 14 points). */
export function sevenDayPercent(series: readonly (number | null | undefined)[]): number | null {
  const values = series.map((v) => (typeof v === "number" && Number.isFinite(v) ? v : 0));
  if (values.length < 14) return null;
  const sum = (xs: number[]) => xs.reduce((a, b) => a + b, 0);
  const cur = sum(values.slice(-7));
  const prev = sum(values.slice(-14, -7));
  if (prev === 0) return null;
  return Math.round(((cur - prev) / Math.abs(prev)) * 1000) / 10;
}

// Room for the visible sub-line under the template card's own content; an empty figure keeps its
// line height so an unavailable figure does not collapse the card.
const SUBLINE_ROOM = { pb: 5.5, "& .MuiBox-root:empty": { minHeight: "1.5em" } };

export function KpiWidget({ title, total, caption, color = "primary", icon, trend, href, linkComponent, sx, "data-testid": testId }: KpiWidgetProps) {
  const figure = total ?? Number.NaN;
  const t: KpiTrend | null = trend ?? null;
  const cardSx = [{ height: 1 }, ...(caption ? [SUBLINE_ROOM] : [{ "& .MuiBox-root:empty": { minHeight: "1.5em" } }]), ...(Array.isArray(sx) ? sx : [sx])] as SxProps<Theme>;
  let card;
  if (t && t.period === "week" && (t.series?.length ?? 0) > 1) {
    card = <EcommerceWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={cardSx} />;
  } else if (t && t.period === "7d" && (t.series?.length ?? 0) > 1) {
    card = <AppWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={cardSx} />;
  } else if (t && t.period === "month") {
    card = <BookingWidgetSummary title={title} total={figure} percent={t.percent} icon={<Iconify icon="solar:chart-square-outline" width={48} sx={{ color: `${color}.main`, opacity: 0.48 }} />} sx={cardSx} />;
  } else {
    card = <CourseWidgetSummary title={title} total={figure} color={color} icon={ICONS[icon ?? defaultIcon(color)]} sx={cardSx} />;
  }
  const body = (
    <Box data-testid={testId} data-kpi-widget="" sx={{ position: "relative", height: 1 }}>
      {card}
      {caption ? (
        <Typography
          variant="body2"
          noWrap
          title={caption}
          data-kpi-subline=""
          sx={{ position: "absolute", left: 24, right: 20, bottom: 16, color: "text.secondary" }}
        >
          {caption}
        </Typography>
      ) : null}
    </Box>
  );
  if (!href) return body;
  return (
    <MuiLink component={linkComponent ?? Link} href={href} underline="none" color="inherit" sx={{ display: "block", height: 1 }}>
      {body}
    </MuiLink>
  );
}

/** One half of a two-part reading ("Individual 54 · Lump-sum 7"). */
export type KpiPart = { label: string; value: number };

/** Pairs a contract label naming both halves in order ("Kids · Adults") with its two counts. */
export function splitParts(label: string, values: [number, number]): KpiPart[] {
  const names = label.split(" · ");
  return values.map((value, index) => ({ label: names[index] ?? label, value }));
}
