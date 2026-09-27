import type { ElementType } from "react";
import type { SxProps, Theme } from "@mui/material/styles";
import type { PaletteColorKey } from "@/theme/core";
import type { KitTone } from "@/lib/tone";

import MuiLink from "@mui/material/Link";
import Tooltip from "@mui/material/Tooltip";

import Link from "@/components/no-prefetch-link";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";

// KPI adapter: maps one Goat OS figure onto a VERBATIM template widget summary (guard
// template-verbatim). It only picks the template card and passes props; it draws nothing itself.
//  - a week-over-week change with a weekly series -> EcommerceWidgetSummary (the template card
//    prints "last week" next to the percent, so only pass `weekChange` for a real weekly comparison);
//  - anything else -> CourseWidgetSummary (h3 figure, title, template corner icon in `color`).
// The template cards have no sub-line slot: `caption` (backend copy) becomes the card's tooltip.

export type KpiIcon = "progress" | "completed" | "certificates";

export type KpiWidgetProps = {
  title: string;
  /** The figure. Units go in the title ("Total weight (kg)"); null renders an empty figure. */
  total: number | null | undefined;
  caption?: string;
  color?: PaletteColorKey;
  icon?: KpiIcon;
  /** Percent change vs last week, with the weekly series it came from. */
  weekChange?: { percent: number; series: number[]; categories: string[] } | null;
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
export function kpiColor(tone: KitTone | undefined): PaletteColorKey {
  if (!tone || tone === "neutral") return "primary";
  return tone === "violet" ? "secondary" : tone;
}

function defaultIcon(color: PaletteColorKey | undefined): KpiIcon {
  if (color === "success") return "completed";
  if (color === "info" || color === "secondary") return "certificates";
  return "progress";
}

export function KpiWidget({ title, total, caption, color = "primary", icon, weekChange, href, linkComponent, sx, "data-testid": testId }: KpiWidgetProps) {
  const figure = total ?? Number.NaN;
  const card =
    weekChange && weekChange.series.length > 1 ? (
      <EcommerceWidgetSummary
        title={title}
        total={figure}
        percent={weekChange.percent}
        chart={{ series: weekChange.series, categories: weekChange.categories }}
        sx={sx}
        data-testid={testId}
      />
    ) : (
      <CourseWidgetSummary title={title} total={figure} color={color} icon={ICONS[icon ?? defaultIcon(color)]} sx={sx} data-testid={testId} />
    );
  const withTip = caption ? (
    <Tooltip title={caption} placement="bottom-start">
      {card}
    </Tooltip>
  ) : (
    card
  );
  if (!href) return withTip;
  return (
    <MuiLink component={linkComponent ?? Link} href={href} underline="none" color="inherit" sx={{ display: "block", height: 1 }}>
      {withTip}
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
