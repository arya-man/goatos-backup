"use client";

import type { ReactNode } from "react";

import Box from "@mui/material/Box";

import {
  ProgressGroupHeading,
  ProgressItem,
  ProgressLegend,
  ProgressTooltip,
  progressGroupSx,
  progressListSx,
  type ProgressItemTrack,
} from "@/components/minimal/progress-list/progress-item";

import { EmptyState } from "@/components/app/empty-state";
// Shared chart sheet (KPI sparkline placement, chart empty frames): loaded wherever the old hover
// layer used to pull it in, so no page loses a rule it relied on.
import "@/components/charts-premium.css";
import { cx } from "@/lib/tone";

export type BarListRow = {
  key: string;
  label: ReactNode;
  /** Plain-text label for the tooltip and title (defaults to `label` when it is a string). */
  labelText?: string;
  value: number;
  /** Pre-formatted value shown at the right; falls back to en-IN formatting of `value`. */
  display?: string;
  /** Small chip after the label (head count, mode). Already resolved copy. */
  note?: ReactNode;
  /** CSS colour token for this bar (a series colour in a multi-series list); defaults to brand. */
  color?: string;
  /** Series name for the tooltip (multi-series lists). */
  series?: string;
};

export type BarListGroup = {
  key: string;
  heading: ReactNode;
  /** Plain-text heading for the group's tooltip (defaults to `heading` when it is a string). */
  headingText?: string;
  rows: BarListRow[];
};

export type BarListProps = {
  rows?: BarListRow[];
  groups?: BarListGroup[];
  /** Accessible name of the chart. */
  ariaLabel: string;
  /** Word before the value in the tooltip row, e.g. "animals". */
  valueNoun?: string;
  /** Legend chips above the list: label + colour. */
  legend?: { label: string; color: string }[];
  /** Shared scale; unioned with the data so nothing overflows. */
  domain?: { lo: number; hi: number };
  /**
   * An optional reference value (e.g. the FCR break-even) drawn as a dashed line across every
   * track at the same position. Included in the scale so the line is never pushed off the track.
   */
  reference?: { value: number; label: string };
  /** Fixed-height scroll box: "auto" (grow), "tall" (300px), "short" (150px). */
  size?: "auto" | "tall" | "short";
  /**
   * Wide list (full-width card, two-fact labels): label / track / value on ONE row above 900px
   * with a 240-420px label column and a >= 220px track; the template's stacked item below.
   */
  wide?: boolean;
  emptyLabel?: string;
  className?: string;
};

function fmt(value: number): string {
  return value.toLocaleString("en-IN", { maximumFractionDigits: 1 });
}

const textOf = (node: ReactNode, fallback?: string) => fallback ?? (typeof node === "string" ? node : "");

/**
 * The horizontal bar list, on the template's EcommerceSalesOverview item
 * (components/minimal/progress-list): label and figure over an 8px LinearProgress, inside whatever
 * card the caller owns, with the template Tooltip on each row. ONE measure, ONE colour: a ranked
 * list is brand-toned throughout with the danger tone as the only accent (a loss); `row.color`
 * carries a series colour in a multi-series list with a legend. A series that straddles zero, or
 * carries a reference value, draws on the hand-built axis track (zero rule, losses growing left in
 * the danger tone, the dashed reference mark). Theme tokens only, no copy of its own.
 */
export function BarList({ rows, groups, ariaLabel, valueNoun, legend, domain, reference, size = "auto", wide, emptyLabel, className }: BarListProps) {
  const sections: BarListGroup[] = groups ?? [{ key: "_", heading: null, rows: rows ?? [] }];
  const all = sections.flatMap((section) => section.rows).filter((row) => Number.isFinite(row.value));
  if (all.length === 0) {
    return <EmptyState title={emptyLabel ?? ""} />;
  }
  const refValue = reference && Number.isFinite(reference.value) ? reference.value : null;
  const lo = Math.min(0, domain?.lo ?? 0, refValue ?? 0, ...all.map((row) => row.value));
  const hi = Math.max(0, domain?.hi ?? 0, refValue ?? 0, ...all.map((row) => row.value));
  const span = hi - lo || 1;
  const zero = ((0 - lo) / span) * 100;
  const straddles = lo < 0 && hi > 0;
  // LinearProgress fills from the left edge only; a signed or referenced scale needs the axis track.
  const axis = lo < 0 || refValue != null;
  const refAt = refValue != null ? ((refValue - lo) / span) * 100 : null;

  const colorOf = (row: BarListRow) => row.color ?? (row.value < 0 ? "var(--danger)" : "var(--brand)");

  const groupTip = (section: BarListGroup) => {
    const head = textOf(section.heading, section.headingText);
    if (!head) return undefined;
    return (
      <ProgressTooltip
        heading={head}
        rows={section.rows
          .filter((r) => Number.isFinite(r.value))
          .map((r) => {
            const full = textOf(r.label, r.labelText);
            return { name: full.startsWith(`${head} · `) ? full.slice(head.length + 3) : full, value: r.display ?? fmt(r.value), color: colorOf(r) };
          })}
      />
    );
  };

  return (
    <Box
      className={cx("kit-barlist", `kit-barlist-${size}`, straddles && "kit-barlist-axis", className)}
      role="img"
      aria-label={ariaLabel}
      sx={progressListSx(size)}
    >
      {legend && legend.length > 0 ? <ProgressLegend items={legend} /> : null}
      {sections.map((section) => (
        <Box className="kit-barlist-group" key={section.key} sx={progressGroupSx}>
          {section.heading ? <ProgressGroupHeading tooltip={groupTip(section)}>{section.heading}</ProgressGroupHeading> : null}
          {section.rows
            .filter((row) => Number.isFinite(row.value))
            .map((row) => {
              const negative = row.value < 0;
              const width = Math.max((Math.abs(row.value) / span) * 100, 0.8);
              const color = colorOf(row);
              const text = textOf(row.label, row.labelText);
              const display = row.display ?? fmt(row.value);
              const track: ProgressItemTrack = axis
                ? { kind: "axis", left: negative ? zero - width : zero, width, zero: straddles ? zero : null, reference: refAt != null ? { at: refAt, label: reference?.label } : null, growsLeft: negative }
                : { kind: "linear", percent: row.value > 0 ? width : 0 };
              return (
                <ProgressItem
                  key={row.key}
                  hook="kit-bar"
                  label={row.label}
                  labelTitle={text}
                  note={row.note}
                  value={display}
                  color={color}
                  negative={negative}
                  track={track}
                  inline={wide}
                  ariaLabel={text ? `${text} ${display}` : display}
                  tooltip={
                    <ProgressTooltip
                      heading={`${text}${row.series ? ` · ${row.series}` : ""}`}
                      rows={[{ name: valueNoun, value: display, color }]}
                    />
                  }
                />
              );
            })}
        </Box>
      ))}
    </Box>
  );
}
