"use client";

// Horizontal progress rows on the licensed MUI Minimal template.
//
// Template source (next-ts/src/sections/overview/e-commerce/ecommerce-sales-overview.tsx), the
// `Item` of EcommerceSalesOverview: a header row in subtitle2 (label grows on the left, the figure
// on the right, an optional secondary figure in body2 text.secondary), and under it an MUI
// `LinearProgress variant="determinate"` 8px high on a grey-500 16% track. The bar colour is the
// row's own Mesha token (a series colour, the danger tone for a loss) through
// `'& .MuiLinearProgress-bar'`, never a hard-coded palette key.
//
// Hover is the template's MUI `Tooltip` (arrow, placement top — banking-quick-transfer.tsx) on the
// whole row: portaled by MUI's Popper, so a card's overflow or a transformed ancestor never clips
// it. Rows are contiguous (their rhythm is padding, not a gap), so moving down a list hands the
// tooltip from one row to the next without closing in between — no flicker.
//
// Mesha additions on top of the template item, each noted where it happens:
// - `AxisTrack`: LinearProgress can only fill from the left, so a list that straddles zero (weighing
//   losses) or carries a reference value (the FCR break-even) draws a Box styled EXACTLY like the
//   LinearProgress track (same 8px height, 16px radius, grey-500 16% background) with an absolutely
//   placed fill, a zero rule and the dashed reference mark. Nearest template pattern: the same
//   LinearProgress root/bar anatomy (theme/core/components/progress.tsx), re-drawn by hand.
// - `inline`: the wide FCR list keeps label / track / value on ONE row at laptop widths (label
//   column clamp 240-420px, track >= 220px) and stacks into the template item at <= 900px.
// - `note`: a chip beside the label (weighing mode, head count), outside the clamped label text so
//   the clamp can never swallow it.
// - Stable hook class names (`<hook>`, `<hook>-label-text`, `<hook>-value`, `<hook>-track`,
//   `<hook>-fill` …) for the smoke / regression scripts and caller layout rules. They carry no
//   styling here.

import type { ReactNode } from "react";
import { useTheme, type SxProps, type Theme } from "@mui/material/styles";

import { varAlpha } from "minimal-shared/utils";

import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
import LinearProgress from "@mui/material/LinearProgress";

import { ChartLegends } from "@/components/minimal/chart/components/chart-legends";

// ----------------------------------------------------------------------

/** The LinearProgress track (template: height 8, grey-500 16%; theme override: radius 16). */
const trackSx = (theme: Theme) => ({
  height: "var(--sp-1)",
  borderRadius: "var(--r-xl)",
  bgcolor: varAlpha(theme.vars.palette.grey["500Channel"], 0.16),
});

export type ProgressItemTrack =
  /** Fill from the left edge to `percent` (0-100): the template LinearProgress. */
  | { kind: "linear"; percent: number }
  /**
   * Hand-drawn track for a signed or referenced scale; all positions are percentages of the track.
   * `zero` draws the zero rule, `reference` the dashed mark.
   */
  | {
      kind: "axis";
      left: number;
      width: number;
      zero?: number | null;
      reference?: { at: number; label?: string } | null;
      /** A loss grows leftwards from zero. */
      growsLeft?: boolean;
    };

export type ProgressItemProps = {
  label: ReactNode;
  /** Plain label text, used as the native title of the clamped label. */
  labelTitle?: string;
  /** Chip beside the label (mode tag, head count). */
  note?: ReactNode;
  /** The figure on the right of the header. */
  value: ReactNode;
  /** Template's secondary figure: body2, text.secondary, after the value. */
  secondary?: ReactNode;
  /** Muted line under the track. */
  hint?: ReactNode;
  /** Bar colour: any CSS colour or Mesha token. */
  color: string;
  /** The value text takes the danger tone (a loss). */
  negative?: boolean;
  /** Leading dot in the bar colour (a legend swatch). */
  dot?: boolean;
  track: ProgressItemTrack;
  /** Tooltip body; no tooltip when omitted. */
  tooltip?: ReactNode;
  /** Label / track / value on one row above 900px (wide FCR list). */
  inline?: boolean;
  /** Accessible name of the progress bar. */
  ariaLabel?: string;
  /** Hook class prefix: "kit-bar" (bar lists) or "kit-prow" (progress rows). */
  hook: string;
  className?: string;
};

// ----------------------------------------------------------------------

function AxisTrack({ track, color, hook }: { track: Extract<ProgressItemTrack, { kind: "axis" }>; color: string; hook: string }) {
  return (
    <Box
      component="span"
      className={`${hook}-track`}
      sx={[(theme) => ({ ...trackSx(theme), display: "block", position: "relative", minWidth: 0 })]}
    >
      {track.zero != null ? (
        <Box
          component="span"
          className={`${hook}-axis`}
          aria-hidden
          sx={{ position: "absolute", top: -2, bottom: -2, left: `${track.zero}%`, width: "1px", zIndex: 1, bgcolor: "text.disabled" }}
        />
      ) : null}
      {track.reference ? (
        <Box
          component="span"
          className={`${hook}-ref`}
          aria-hidden
          title={track.reference.label}
          sx={[
            (theme) => ({
              position: "absolute",
              top: -3,
              bottom: -3,
              left: `${track.reference?.at ?? 0}%`,
              width: 0,
              zIndex: 2,
              opacity: 0.85,
              borderLeft: `2px dashed ${theme.vars.palette.text.primary}`,
              transform: "translateX(-1px)",
              pointerEvents: "none",
            }),
          ]}
        />
      ) : null}
      <Box
        component="span"
        className={`${hook}-fill`}
        sx={{
          position: "absolute",
          top: 0,
          bottom: 0,
          left: `${track.left}%`,
          width: `${track.width}%`,
          borderRadius: "inherit",
          bgcolor: color,
          transformOrigin: track.growsLeft ? "right center" : "left center",
        }}
      />
    </Box>
  );
}

function LinearTrack({ percent, color, hook, ariaLabel }: { percent: number; color: string; hook: string; ariaLabel?: string }) {
  return (
    <LinearProgress
      color="primary"
      variant="determinate"
      value={Math.max(0, Math.min(100, percent))}
      aria-label={ariaLabel}
      className={`${hook}-track`}
      classes={{ bar: `${hook}-fill` }}
      sx={[(theme) => ({ ...trackSx(theme), minWidth: 0, "& .MuiLinearProgress-bar": { bgcolor: color } })]}
    />
  );
}

// ----------------------------------------------------------------------

/** One EcommerceSalesOverview item: header row (label · value · secondary) over the progress track. */
export function ProgressItem({
  label,
  labelTitle,
  note,
  value,
  secondary,
  hint,
  color,
  negative,
  dot,
  track,
  tooltip,
  inline,
  ariaLabel,
  hook,
  className,
}: ProgressItemProps) {
  // At laptop widths an inline row flattens the header (`display: contents`) so label, track and
  // value share one grid row; at <= 900px it is the template item again.
  const theme = useTheme();
  const WIDE = theme.breakpoints.up(901);
  const rowSx: SxProps<Theme> = inline
    ? {
        py: 0.75,
        minWidth: 0,
        [WIDE]: {
          display: "grid",
          gridTemplateColumns: "minmax(0, clamp(240px, 42%, 420px)) minmax(220px, 1fr) auto",
          alignItems: "center",
          columnGap: 2,
        },
      }
    : { py: 0.75, minWidth: 0 };

  const row = (
    <Box className={[hook, negative ? "neg" : "", className ?? ""].filter(Boolean).join(" ")} sx={rowSx}>
      <Box
        className={`${hook}-head`}
        sx={{
          mb: 1,
          gap: 0.5,
          display: "flex",
          alignItems: "center",
          typography: "subtitle2",
          minWidth: 0,
          ...(inline ? { [WIDE]: { display: "contents" } } : {}),
        }}
      >
        <Box
          component="span"
          className={`${hook}-label`}
          sx={{ flexGrow: 1, minWidth: 0, display: "flex", alignItems: "center", gap: 0.75, ...(inline ? { [WIDE]: { gridColumn: 1, gridRow: 1 } } : {}) }}
        >
          {dot ? (
            <Box component="span" aria-hidden sx={{ width: "calc(var(--sp-1) + 2px)", height: "calc(var(--sp-1) + 2px)", flexShrink: 0, borderRadius: "50%", bgcolor: color }} />
          ) : null}
          <Box
            component="span"
            className={`${hook}-label-text`}
            title={labelTitle || undefined}
            // Two-line clamp: a long pen label wraps instead of collapsing to "C..", and the
            // minWidth 0 keeps it inside its column so it never runs under the value.
            sx={{
              minWidth: 0,
              overflow: "hidden",
              display: "-webkit-box",
              WebkitLineClamp: 2,
              WebkitBoxOrient: "vertical",
              overflowWrap: "anywhere",
            }}
          >
            {label}
          </Box>
          {note ? (
            <Box component="span" className={`${hook}-note`} sx={{ flexShrink: 0, display: "inline-flex", alignItems: "center", gap: 0.5 }}>
              {note}
            </Box>
          ) : null}
        </Box>

        <Box
          component="span"
          className={`${hook}-value`}
          sx={{
            flexShrink: 0,
            whiteSpace: "nowrap",
            textAlign: "right",
            fontVariantNumeric: "tabular-nums",
            color: negative ? "var(--danger)" : undefined,
            ...(inline ? { [WIDE]: { gridColumn: 3, gridRow: 1 } } : {}),
          }}
        >
          {value}
        </Box>

        {secondary ? (
          <Box component="span" sx={{ typography: "body2", color: "text.secondary", flexShrink: 0 }}>
            {secondary}
          </Box>
        ) : null}
      </Box>

      <Box sx={inline ? { [WIDE]: { gridColumn: 2, gridRow: 1, minWidth: 0 } } : undefined}>
        {track.kind === "linear" ? (
          <LinearTrack percent={track.percent} color={color} hook={hook} ariaLabel={ariaLabel} />
        ) : (
          <AxisTrack track={track} color={color} hook={hook} />
        )}
      </Box>

      {hint ? (
        <Box className={`${hook}-hint`} sx={{ mt: 1, typography: "caption", color: "text.secondary" }}>
          {hint}
        </Box>
      ) : null}
    </Box>
  );

  if (!tooltip) return row;
  return (
    <Tooltip title={tooltip} arrow placement="top" describeChild disableInteractive>
      {row}
    </Tooltip>
  );
}

// ----------------------------------------------------------------------

/** Tooltip body: a heading line, then "noun  value" rows with the bar colour as a dot. */
export function ProgressTooltip({ heading, rows }: { heading?: ReactNode; rows: { name?: ReactNode; value: ReactNode; color?: string }[] }) {
  return (
    <Box sx={{ display: "grid", gap: 0.25 }}>
      {heading ? <Box sx={{ fontWeight: "fontWeightSemiBold" }}>{heading}</Box> : null}
      {rows.map((row, index) => (
        <Box key={index} sx={{ display: "flex", alignItems: "center", gap: 0.75, whiteSpace: "nowrap" }}>
          {row.color ? <Box component="span" aria-hidden sx={{ width: "var(--sp-1)", height: "var(--sp-1)", flexShrink: 0, borderRadius: "50%", bgcolor: row.color }} /> : null}
          {row.name ? <Box component="span" sx={{ flexGrow: 1, opacity: 0.8 }}>{row.name}</Box> : null}
          <Box component="span" sx={{ fontWeight: "fontWeightBold", fontVariantNumeric: "tabular-nums" }}>{row.value}</Box>
        </Box>
      ))}
    </Box>
  );
}

// ----------------------------------------------------------------------

export type ProgressListSize = "auto" | "tall" | "short";

/**
 * The list box the items sit in. Template: EcommerceSalesOverview's column flex box. Mesha: rows
 * are contiguous (padding, not a gap) so the tooltip hands over without closing, and a "tall"
 * (300px) or "short" (150px) list is a fixed box that scrolls inside itself from sm up, so a chart
 * with 40 pens occupies exactly as much page as one with 4; on phone it grows (no scroll trap).
 */
export function progressListSx(size: ProgressListSize): SxProps<Theme> {
  const box = size === "tall" ? 300 : size === "short" ? 150 : null;
  return {
    display: "flex",
    flexDirection: "column",
    minWidth: 0,
    // Phone: never a nested scroller (webview rule: no scroll traps) -- the list grows with the page.
    ...(box ? { height: { xs: "auto", sm: box }, overflowY: { xs: "visible", sm: "auto" }, overscrollBehavior: { sm: "contain" }, pr: { sm: 0.75 } } : {}),
  };
}

/** Legend above a multi-series list: the template ChartLegends (dot + label). */
export function ProgressLegend({ items }: { items: { label: string; color: string }[] }) {
  return (
    <ChartLegends
      aria-hidden
      labels={items.map((item) => item.label)}
      colors={items.map((item) => item.color)}
      values={[]}
      sx={{ m: 0, p: 0, pb: 2, listStyle: "none" }}
      slotProps={{ value: { sx: { display: "none" } } }}
    />
  );
}

/** Group heading inside a grouped list. */
export function ProgressGroupHeading({ children, tooltip }: { children: ReactNode; tooltip?: ReactNode }) {
  const heading = (
    <Box className="kit-barlist-heading" sx={{ typography: "subtitle2", pt: 0.5, pb: 0.25 }}>
      {children}
    </Box>
  );
  if (!tooltip) return heading;
  return (
    <Tooltip title={tooltip} arrow placement="top" describeChild disableInteractive>
      {heading}
    </Tooltip>
  );
}

/** Divider between groups (dashed, as the template's list separators). */
export const progressGroupSx: SxProps<Theme> = {
  display: "flex",
  flexDirection: "column",
  "& + &": { mt: 1, pt: 1, borderTop: "1px dashed", borderColor: "divider" },
};
