import type { CSSProperties, ElementType } from "react";
import type { SxProps, Theme } from "@mui/material/styles";
import type { PaletteColorKey } from "@/theme/core";

import Box from "@mui/material/Box";
import MuiLink from "@mui/material/Link";

import Link from "@/components/no-prefetch-link";
export { completeMonthPercent, lastStepPercent, sevenDayPercent } from "@/lib/kpi-trend";
import { compactFigure, liftFigureUnit } from "@/lib/kpi-figure";
import { AppWidgetSummary } from "@/components/minimal/sections/overview/app/app-widget-summary";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";

// KPI adapter: maps one Goat OS figure onto a VERBATIM template widget summary (guard
// template-verbatim). It picks the template card by the comparison the figure really has, so the
// template's own period text is true:
//  - trend.period "week"  -> EcommerceWidgetSummary (prints "<±x%> last week" + line sparkline);
//  - trend.period "7d"    -> AppWidgetSummary (prints "<±x%> last 7 days" + bar sparkline);
//  - trend.period "month" -> CourseWidgetSummary with the change leading the visible sub-line
//                            ("+400% · …", no period text, no sparkline; the monthly series is the
//                            page's chart card). It used to be BookingWidgetSummary, which put a
//                            second card anatomy (outlined icon, big circle, trend row) into rows of
//                            Course tiles (J2 P1-8; guard: kpi-row-one-kind);
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
  /** Visible sub-line: unit / remainder first ("kg", "of 30 deaths", "₹ lakh"), then detail. A
   * leading unit is lifted onto the figure (lib/kpi-figure liftFigureUnit). */
  caption?: string;
  /** The figure's unit, printed WITH it: "%" -> "80%", "₹" -> "₹75,341", "kg" -> "412 kg". Prefer
   * this over leading the caption with the unit. */
  unit?: string;
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

// The figure's unit and the missing-figure dash (lib/kpi-figure liftFigureUnit) are painted ON the
// template's figure box, as its own ::before / ::after, so the verbatim template keeps printing a
// number: "80" + "%" reads "80%", a null figure reads "—" instead of an empty slot (PR #294 C2/D1).
// The figure box is the template's h3 Box: the first child of the content column on
// CourseWidgetSummary, the second (after the title) on App/EcommerceWidgetSummary.
function figureSx(kind: "ecommerce" | "app" | "course", unit: { prefix: string; suffix: string; missing: boolean }) {
  const box = kind === "course" ? "& > .MuiBox-root:first-of-type > .MuiBox-root:first-of-type" : "& > .MuiBox-root:first-of-type > .MuiBox-root:nth-of-type(2)";
  if (unit.missing) return { [`${box}::before`]: { content: '"—"' } };
  const word = /^ /.test(unit.suffix);
  return {
    ...(unit.prefix ? { [`${box}::before`]: { content: cssString(unit.prefix) } } : null),
    ...(unit.suffix ? { [`${box}::after`]: { content: cssString(unit.suffix), whiteSpace: "pre", ...(word ? { typography: "subtitle1" } : null) } } : null),
  };
}

/** The caption as a CSS string literal for `content:` (quotes, backslashes and line breaks escaped). */
export function cssString(text: string): string {
  return `"${text.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\r?\n/g, " ")}"`;
}

export function KpiWidget({ title, total: rawTotal, caption: givenCaption, unit: unitProp, color = "primary", icon, trend, href, linkComponent, sx, "data-testid": testId }: KpiWidgetProps) {
  // A lakh or more is compacted for the template figure (never under the corner icon / sparkline);
  // the scale word and the exact value lead the visible sub-line (guard: kpi-long-figure).
  const t: KpiTrend | null = trend ?? null;
  const monthLead = t && t.period === "month" ? monthChange(t.percent) : "";
  // An explicit unit rides the caption convention, so compaction and lifting treat it the same way.
  const rawCaption = unitProp ? [unitProp, givenCaption].filter(Boolean).join(" · ") : givenCaption;
  const compact = compactFigure(rawTotal, rawCaption);
  const total = compact.total;
  // The unit (or the missing-figure dash) leading the caption moves onto the figure.
  const unit = liftFigureUnit(total, compact.caption);
  const caption = monthLead ? [monthLead, unit.caption].filter(Boolean).join(" · ") : unit.caption;
  const figure = total ?? Number.NaN;
  const sxFor = (k: "ecommerce" | "app" | "course") =>
    [{ height: 1 }, EMPTY_FIGURE, figureSx(k, unit), ...(caption ? [SUBLINE_IN_FLOW] : []), ...(Array.isArray(sx) ? sx : [sx])] as SxProps<Theme>;
  const reserve = caption ? { "data-kpi-caption": caption, style: { "--kpi-caption": cssString(caption) } as CSSProperties } : {};
  let card;
  let kind: "ecommerce" | "app" | "course" = "course";
  if (t && t.period === "week" && (t.series?.length ?? 0) > 1) {
    kind = "ecommerce";
    card = <EcommerceWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={sxFor(kind)} {...reserve} />;
  } else if (t && t.period === "7d" && (t.series?.length ?? 0) > 1) {
    kind = "app";
    card = <AppWidgetSummary title={title} total={figure} percent={t.percent} chart={{ series: t.series ?? [], categories: t.categories ?? [] }} sx={sxFor(kind)} {...reserve} />;
  } else {
    card = <CourseWidgetSummary title={title} total={figure} color={color} icon={ICONS[icon ?? defaultIcon(color)]} sx={sxFor(kind)} {...reserve} />;
  }
  const body = (
    <Box data-testid={testId} data-kpi-widget="" data-kpi-kind={kind} sx={{ position: "relative", height: 1 }}>
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

/** A month-over-month change as the sub-line lead: "+400%", "−12.5%", "0%" (no period words). */
export function monthChange(percent: number): string {
  const rounded = Math.round(percent * 10) / 10;
  const text = `${Math.abs(rounded).toLocaleString("en-IN", { maximumFractionDigits: 1 })}%`;
  return rounded > 0 ? `+${text}` : rounded < 0 ? `\u2212${text}` : text;
}

/** One half of a two-part reading ("Individual 54 · Lump-sum 7"). */
export type KpiPart = { label: string; value: number };

/** Pairs a contract label naming both halves in order ("Kids · Adults") with its two counts. */
export function splitParts(label: string, values: [number, number]): KpiPart[] {
  const names = label.split(" · ");
  return values.map((value, index) => ({ label: names[index] ?? label, value }));
}
