// (The svg- file name is historical: nothing here draws SVG by hand any more; the importers keep
// the path so no page changes.)
//
// Compact day-by-day column chart ("did we keep up?") on the licensed MUI Minimal template's
// ApexCharts `Chart`: AnalyticsWebsiteVisits (grouped columns). The drawing lives in the client
// component components/minimal/bar-charts/bar-charts.tsx.
//
// This export stays a SERVER-SAFE wrapper (no "use client") so its server caller does not change:
// every visible string -- the figure above each column, the tooltip values, the day labels -- is
// composed HERE and handed to the client chart as plain data. No hex literals, and NO copy of its
// own: the accessible chart name, the series nouns and the empty state are passed in already
// resolved from the backend page contract by the caller.
//
// Two series per day on purpose: one bar alone answers "how fast are we going" and cannot answer
// "are we keeping up". Both share ONE value axis, so a day with 3 verdicts against 40 arrivals can
// never draw as two equal-height marks -- the exact comparison this chart exists to make.

import "./charts-premium.css";

import { ColumnBars, type ColumnBarSeries } from "./minimal/bar-charts/bar-charts";

export type SvgColumnDatum = {
  key: string;
  /** Human label for this column (axis + tooltip), resolved by the caller. */
  label: string;
  value: number;
  /** Optional comparison value drawn as a second column on the same day. */
  compareValue?: number;
};

export function SvgColumnBars({
  data,
  chartLabel,
  valueNoun,
  compareNoun,
  emptyLabel,
}: {
  data: SvgColumnDatum[];
  /** Resolved from the page contract by the caller; the chart's accessible name. */
  chartLabel: string;
  /** Resolved from the page contract by the caller; names the primary series. */
  valueNoun: string;
  /** Resolved from the page contract by the caller; names the comparison series. */
  compareNoun?: string;
  /** Resolved from the page contract by the caller. */
  emptyLabel: string;
}) {
  const hasAnyValue = data.some((d) => d.value > 0 || (d.compareValue ?? 0) > 0);
  if (data.length === 0 || !hasAnyValue) {
    return (
      <div className="cx-empty muted small">
        {emptyLabel}
      </div>
    );
  }

  const figure = (value: number) => value.toLocaleString("en-IN");
  // A zero is passed through as 0: it draws no column (no minimum visible height) but still
  // prints its "0" at the baseline, because a zero day is exactly the one a reader interrogates.
  const series: ColumnBarSeries[] = [
    {
      key: "value",
      name: valueNoun,
      color: "var(--brand)",
      values: data.map((d) => d.value),
      labels: data.map((d) => figure(d.value)),
    },
  ];
  if (data.some((d) => d.compareValue != null)) {
    series.push({
      key: "compare",
      name: compareNoun ?? "",
      color: "var(--amber)",
      values: data.map((d) => (d.compareValue == null ? null : d.compareValue)),
      labels: data.map((d) => (d.compareValue == null ? "" : figure(d.compareValue))),
    });
  }

  return <ColumnBars categories={data.map((d) => d.label)} series={series} chartLabel={chartLabel} />;
}
