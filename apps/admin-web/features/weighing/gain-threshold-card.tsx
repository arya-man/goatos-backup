"use client";

// The breed gain card's body: the caption, the bars and the figures, at whichever grain the Sex
// control in the filter bar is on.
//
// All three grains arrive prepared from the server and are held here together, so switching is a
// re-render rather than another page load. The grains OVERLAP by construction (a per-sex row's
// kids are also in its breed's combined row), so exactly ONE is rendered and they are never added.
// Nothing is filtered or recomputed here either: each grain arrives already banded and already
// apportioned, and this only chooses which prepared set to show.
//
// The caption lives here rather than in the page because it NAMES the kids the selected grain
// counted — left on the server it would say one thing while the bars below it showed another.
//
// It renders NO copy of its own: every string — the three captions, the three head-count nouns,
// the three empty lines, the column headers — arrives already resolved from the page contract.
import { GainThresholdBars, type GainThresholdRow } from "./gain-threshold-bars";
import { useGainSex, type GainSex } from "./gain-sex-scope";

/** One grain's prepared rows and the copy that names the kids it counted. */
export type GainThresholdGrain = {
  rows: readonly GainThresholdRow[];
  /** Caption naming this grain's denominator. */
  caption: string;
  /** Farm noun for the head count under a breed, e.g. "male kids". */
  kidsLabel: string;
  /** Empty line for this grain specifically — "no male kid…" is not "no kid…". */
  emptyLabel: string;
};

export function GainThresholdCard({
  grains,
  view,
  columns,
  chartLabel,
  ofLabel,
}: {
  grains: Record<GainSex, GainThresholdGrain>;
  /** The Chart/Table choice, which stays URL-driven and governs whichever grain is showing. */
  view: "chart" | "table";
  /** Table headers from the table contract, in contract order. */
  columns: readonly string[];
  chartLabel: string;
  ofLabel: string;
}) {
  const { sex } = useGainSex();
  const grain = grains[sex];

  return (
    <>
      <p className="muted small">{grain.caption}</p>
      {view === "chart" ? (
        <GainThresholdBars
          rows={grain.rows}
          chartLabel={chartLabel}
          emptyLabel={grain.emptyLabel}
          kidsLabel={grain.kidsLabel}
          ofLabel={ofLabel}
        />
      ) : grain.rows.length === 0 ? (
        <div className="empty">
          <span className="muted small">{grain.emptyLabel}</span>
        </div>
      ) : (
        <div className="tablewrap">
          <table className="tbl">
            <thead>
              <tr>
                {columns.map((label, index) => (
                  <th key={label} className={index >= 1 ? "num" : undefined}>
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {grain.rows.map((row) => (
                <tr key={row.key}>
                  <td>
                    <b>{row.breed}</b>
                  </td>
                  <td className="num">{row.animals.toLocaleString("en-IN")}</td>
                  {row.marks.map((mark) => (
                    <td key={mark.step} className="num">
                      {mark.count.toLocaleString("en-IN")}{" "}
                      <span className="muted">({mark.pct.toFixed(1)}%)</span>
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
