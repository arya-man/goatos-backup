import type { ReactNode } from "react";

import { ProgressItem, ProgressTooltip } from "@/components/app/progress-item";

import { cx, toneVars, type KitTone } from "@/lib/tone";

export type ProgressRowProps = {
  label: ReactNode;
  value?: ReactNode;
  total?: number;
  percent?: number;
  tone?: KitTone;
  color?: string;
  hint?: ReactNode;
  dot?: boolean;
  className?: string;
};

/**
 * Label + right-aligned value over a slim rounded track — the MUI Minimal
 * EcommerceSalesOverview item (via components/app/progress-item). Product
 * behaviour here: percent/total/value normalisation and tone→colour mapping;
 * visual rendering is delegated to the template ProgressItem.
 */
export function ProgressRow({ label, value, total, percent, tone = "primary", color, hint, dot, className }: ProgressRowProps) {
  const raw = percent != null
    ? percent
    : typeof value === "number" && total ? (value / total) * 100 : 0;
  const pct = Math.max(0, Math.min(100, Number.isFinite(raw) ? raw : 0));
  const fill = color ?? toneVars(tone).solid;
  const text = typeof label === "string" || typeof label === "number" ? String(label) : null;
  const share = `${Math.round(pct)}%`;
  const figure = typeof value === "string" || typeof value === "number" ? String(value) : share;
  return (
    <ProgressItem
      hook="kit-prow"
      className={className}
      label={label}
      labelTitle={text ?? undefined}
      value={value ?? share}
      hint={hint}
      color={fill}
      dot={dot}
      track={{ kind: "linear", percent: pct }}
      ariaLabel={`${text ?? ""} ${figure}`.trim()}
      tooltip={text ? <ProgressTooltip heading={text} rows={[{ name: share, value: figure, color: fill }]} /> : undefined}
    />
  );
}

/**
 * Class for a stack of ProgressRows. Callers own the grid layout (sx on their
 * wrapper, or `.wrap .card>.kit-prows`); this exports the className hook.
 */
export const progressRowsClass = cx("kit-prows");
