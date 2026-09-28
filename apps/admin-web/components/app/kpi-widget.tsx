import type { CSSProperties, ElementType } from "react";
import type { SxProps, Theme } from "@mui/material/styles";
import type { PaletteColorKey } from "@/theme/core";

import Box from "@mui/material/Box";
import MuiLink from "@mui/material/Link";

import Link from "@/components/no-prefetch-link";
export { completeMonthPercent, lastStepPercent, sevenDayPercent } from "@/lib/kpi-trend";
import { compactFigure } from "@/lib/kpi-figure";
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

// The visible sub-line (TR2-P1-2). The template card keeps its markup; the adapter prints the text
// IN FLOW as the card's own `::after`: a full-width row right after the template content (the card
// wraps, lines packed to the top), so a stretched tile never pins its sub-line to the bottom and a
// tile hugs its content like the template's (guard: kpi-subline-in-flow). The text rides in a CSS
// custom property (`--kpi-caption`, spread onto the template Card via its CardProps `style`);
// generated content is in the accessibility tree and wraps in full (never truncated). An empty
// figure keeps its line height.
const EMPTY_FIGURE = { "& .MuiBox-root:empty": { minHeight: "1.5em" } };
const SUBLINE_IN_FLOW = {
  flexWrap: "wrap",
  alignContent: "flex-start",
  "&::after": {
    content: "var(--kpi-caption)",
    display: "block",
    flexBasis: "100%",
    typography: "body2",
    color: "text.secondary",
    mt: 1,
    whiteSpace: "normal",
    overflowWrap: "anywhere",
  },
};

/** The caption as a CSS string literal for `content:` (quotes, backslashes and line breaks escaped). */
export function cssString(text: string): string {
  return `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\r?\n/g, " ")}"`;
}

export function KpiWidget({ title, total: rawTotal, caption: rawCaption, color = "primary", icon, trend, href, linkComponent, sx, "data-testid": testId }: KpiWidgetProps) {
  // A lakh or more is compacted for the template figure (never under the corner icon / sparkline);
  // the scale word and the exact value lead the visible sub-line (guard: kpi-long-figure).
  const { total, caption } = compactFigure(rawTotal, rawCaption);
  const figure = total ?? Number.NaN;
  const t: KpiTrend | null = trend ?? null;
  const cardSx = [{ height: 1 }, EMPTY_FIGURE, ...(caption ? [SUBLINE_IN_FLOW] : []), ...(Array.isArray(sx) ? sx : [sx])] as SxProps<Theme>;
  const reserve = caption ? { "data-kpi-caption": caption, style: { "--kpi-caption": cssString(caption) } as CSSProperties } : {};
  let card;
  if (t && t.period === "week" && (t.series?.length ?? 0) > 1) {
    card = <EcommerceWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={cardSx} {...reserve} />;
  } else if (t && t.period === "7d" && (t.series?.length ?? 0) > 1) {
    card = <AppWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={cardSx} {...reserve} />;
  } else if (t && t.period === "month") {
    card = <BookingWidgetSummary title={title} total={figure} percent={t.percent} icon={<Iconify icon="solar:chart-square-outline" width={48} sx={{ color: `${color}.main`, opacity: 0.48 }} />} sx={cardSx} {...reserve} />;
  } else {
    card = <CourseWidgetSummary title={title} total={figure} color={color} icon={ICONS[icon ?? defaultIcon(color)]} sx={cardSx} {...reserve} />;
  }
  const body = (
    <Box data-testid={testId} data-kpi-widget="" sx={{ position: "relative", height: 1 }}>
      {card}
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
