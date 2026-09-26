// A readable horizontal bar list for the Weights page.
//
// Not the shared SvgBars chart: a Weights row carries a mode chip beside its label, a
// shared scale across side-by-side columns and an optional dashed reference line, none
// of which that chart draws. This is the kit BarList instead: the labels are real text
// at a real font size, so they stay legible at any card width, and the row list scrolls
// inside a FIXED height so a chart with 40 breeds occupies exactly as much page as one with 4.
//
// Each row is the MUI Minimal template's EcommerceSalesOverview item (via the kit BarList);
// this wrapper holds no client state of its own. Colours are theme tokens only, so it stays
// correct in both themes and passes the banned-hex scan by construction. It renders
// no copy of its own: every string is passed in already resolved from the page
// contract by the caller.
import { BarList } from "@/components/bar-list";
import { BarChart3 } from "lucide-react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { stageLabel } from "@/lib/stage-labels";
import { EmptyState } from "@/components/app/empty-state";

export type WeightBar = {
  key: string;
  label: string;
  value: number;
  valueLabel?: string;
  modeLabel?: string;
  modeTone?: Tone;
  /**
   * Table-only companions to `label`, ignored by every bar renderer here.
   *
   * The chart has one text column, so a pen's breed/sex/head count can only ride inside
   * `label`. The shed table has real columns for them, and a mixed pen's composition ran
   * past a hundred characters when it was crammed into the name. Both views therefore read
   * the SAME row: the chart draws `label`, the table draws `shedName` plus `cohorts`, and
   * neither can disagree with the other about which pen it is showing.
   */
  shedName?: string;
  cohorts?: readonly { breed: string; sex: string; animals: number }[];
};

export function WeightBars({
  data,
  emptyLabel,
  unit,
  chartLabel,
  size = "tall",
  wide = false,
  domain,
  reference,
}: {
  data: readonly WeightBar[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  unit: string;
  /** Resolved from the page contract by the caller; the list's accessible name. */
  chartLabel: string;
  /**
   * "tall" for the shed/breed row, "short" for the sex/stage row, "bands" for a list whose row
   * count is FIXED and known — the six weight bands. The first two are fixed-height boxes so a
   * chart with a weight/gain toggle never makes the row jump as the series changes length; a band
   * list has no toggle and always renders exactly six rows, so a fixed box only ever leaves dead
   * space under the last bar.
   */
  size?: "tall" | "short" | "bands";
  /**
   * Widen the label column. For a full-width card whose labels carry two facts —
   * the load number AND its supplier — the half-width column clips the supplier off,
   * which is the one thing that label exists to show.
   */
  wide?: boolean;
  /**
   * Shared scale for lists rendered side by side. Two columns of the same chart must
   * draw the same value at the same length, or the eye reads the shorter column's
   * best pen as slower than the other column's mid-pack. Unioned with this list's own
   * values so a value outside the caller's span can never overflow the track.
   */
  domain?: { lo: number; hi: number };
  /**
   * An optional reference value (the FCR tab's break-even) drawn as a dashed line across every
   * track at the same position, so each bar can be read against it. Included in the scale so the
   * line is never pushed off the track.
   */
  reference?: { value: number; label: string };
}) {
  // A bar can only be drawn with a positive length. Non-positive values are real
  // data, so they are not silently dropped — the caller's empty copy has to explain
  // them, which is why this returns the empty state rather than an empty box.
  // EVERY value renders, gain or loss. Filtering to value > 0 silently dropped every
  // losing shed and every losing kid — which is precisely the row a reader is looking
  // for. Only a genuinely empty series shows the empty state.
  const bars = data.filter((bar) => Number.isFinite(bar.value));
  if (bars.length === 0) {
    // Same fixed box as the populated list, so toggling a chart between weight and
    // gain never makes the row jump.
    return (
      <EmptyState className={`wbars-empty wbars-${size}`} icon={<BarChart3 className="ic" />} title={emptyLabel} />
    );
  }

  // Bars are drawn from a ZERO baseline by the kit list, not from the smallest value: a loss has
  // to read as crossing zero, not as a short positive bar. The caller's shared domain is unioned
  // with this list's own values so two side-by-side columns draw the same value at the same length.
  return (
    <BarList
      ariaLabel={chartLabel}
      valueNoun={unit}
      domain={domain}
      reference={reference}
      size={size === "bands" ? "auto" : size}
      wide={wide}
      emptyLabel={emptyLabel}
      className={`wbars-kit wbars-${size}${wide ? " wbars-wide" : ""}`}
      rows={bars.map((bar) => ({
        key: bar.key,
        label: stageLabel(bar.label),
        labelText: stageLabel(bar.label),
        value: bar.value,
        display: bar.valueLabel ?? `${bar.value.toLocaleString("en-IN", { maximumFractionDigits: 1 })} ${unit}`,
        note: bar.modeLabel ? <Tag tone={bar.modeTone ?? "mut"}>{bar.modeLabel}</Tag> : undefined,
      }))}
    />
  );
}
