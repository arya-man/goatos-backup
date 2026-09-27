// Readable horizontal bar list for full/half-width report cards (sales board).
//
// A ranked list of ONE measure (price per kg by breed): the kit BarList, which draws each row as
// the MUI Minimal template's EcommerceSalesOverview item (label and figure over an 8px
// LinearProgress, template Tooltip on hover — components/app/progress-item). The labels are
// real text at the theme's subtitle2 size, so they stay readable at every card width.
//
// NO copy of its own: every visible string arrives already resolved from the backend page
// contract by the caller. Theme tokens only.

import { BarList } from "@/components/bar-list";

export type HBarDatum = {
  key: string;
  label: string;
  value: number;
  /** Optional pre-formatted value label (e.g. "₹467 per kg"); falls back to the raw value. */
  display?: string;
};

export function HBarList({
  data,
  emptyLabel,
  chartLabel,
  valueNoun,
  maxBars = 10,
}: {
  data: HBarDatum[];
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; used in per-bar tooltips. */
  valueNoun: string;
  maxBars?: number;
}) {
  const bars = data.filter((d) => d.value > 0).slice(0, maxBars);
  // One measure, one colour: a ranked list of the same figure is brand-toned throughout (the
  // BarList default). Categorical hues are for multi-series charts with a legend, never for rank.
  // `hbarlist` is the layout hook for the card's side insets (mesha-theme.css).
  return (
    <BarList
      className="hbarlist"
      ariaLabel={chartLabel}
      valueNoun={valueNoun}
      emptyLabel={emptyLabel}
      rows={bars.map((datum) => ({
        key: datum.key,
        label: datum.label,
        labelText: datum.label,
        value: datum.value,
        display: datum.display ?? datum.value.toLocaleString("en-IN"),
      }))}
    />
  );
}
