"use client";

import { useState } from "react";
import { Scale } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { WeightBars, type WeightBar } from "./weight-bars";

type Metric = "adg" | "weight";

type MetricLabels = {
  adg: string;
  weight: string;
};

type MetricSeries = {
  data: readonly WeightBar[];
  emptyLabel: string;
  unit: string;
  chartLabel: string;
};

function MetricToggle({
  current,
  labels,
  onChange,
}: {
  current: Metric;
  labels: MetricLabels;
  onChange: (next: Metric) => void;
}) {
  return (
    <span className="chips" aria-label="Chart metric">
      {(["adg", "weight"] as const).map((option) => (
        <button
          className={`chip${option === current ? " on" : ""}`}
          key={option}
          onClick={() => onChange(option)}
          type="button"
        >
          {labels[option]}
        </button>
      ))}
    </span>
  );
}

export function MetricChart({
  initialMetric,
  labels,
  title,
  caption,
  series,
  size = "short",
  wide = false,
  className = "card wchart",
}: {
  initialMetric: Metric;
  labels: MetricLabels;
  title: MetricLabels;
  caption?: string;
  series: Record<Metric, MetricSeries>;
  size?: "tall" | "short";
  wide?: boolean;
  className?: string;
}) {
  const [metric, setMetric] = useState<Metric>(initialMetric);
  const active = series[metric];
  return (
    <section className={className} aria-label={active.chartLabel}>
      <h2 className="h">
        {title[metric]}
        <MetricToggle current={metric} labels={labels} onChange={setMetric} />
      </h2>
      {caption ? <p className="muted small">{caption}</p> : null}
      <WeightBars
        data={active.data}
        emptyLabel={active.emptyLabel}
        unit={active.unit}
        chartLabel={active.chartLabel}
        size={size}
        wide={wide}
      />
    </section>
  );
}

type ShedChartColumn = {
  heading: string;
  rows: readonly WeightBar[];
};

export type ShedView = "chart" | "table";

/** Column headings for the table view, already resolved from the page contract. */
export type ShedTableColumns = {
  park: string;
  shed: string;
  breed: string;
  sex: string;
  count: string;
  basis: string;
  value: Record<Metric, string>;
};

/** Pager copy for the table view, already resolved from the page contract. */
export type ShedTablePagerLabels = {
  previous: string;
  next: string;
  page: string;
  of: string;
  /** Singular row noun; pluralised with an "s" the way the shared worklist pager does. */
  noun: string;
};

/**
 * 20 rows a page (maintainer request 2026-09-01). The farm has ~45 weighed pens across the two
 * parks, so the unpaged table ran three screens deep and buried the cards under it.
 */
const SHED_TABLE_PAGE_SIZE = 20;

type ShedSeries = MetricSeries & {
  columns: readonly ShedChartColumn[];
  domain: { lo: number; hi: number };
  size: "tall" | "short";
  caption: string;
};

/**
 * The Gender column's lines for one pen: ONE when every cohort shares a sex, otherwise one per
 * cohort so the lines still pair by index with breed and count.
 *
 * All-or-nothing on purpose. Collapsing only the runs of equal neighbours would put a single
 * "male" beside some rows and two beside others with no rule a reader could infer, and a
 * collapsed cell would no longer be a statement about the whole pen.
 */
function sexLines(cohorts: WeightBar["cohorts"]): readonly string[] {
  const lines = (cohorts ?? []).map((cohort) => cohort.sex);
  if (lines.length > 1 && lines.every((sex) => sex === lines[0])) return [lines[0]];
  return lines;
}

/**
 * The same figures as the chart, read as exact numbers (maintainer request 2026-09-01).
 *
 * The rows come from the CHART's own park columns rather than a second data path, so the
 * two views can never disagree about which sheds are in scope or what order they are in.
 * A single park is split into two chart columns for layout; flattening them back restores
 * one A-Z list, and the park cell is the column's own heading — the same string the chart
 * puts above the bars.
 *
 * Values reuse the bar list's formatting exactly, unit included, so a reader switching
 * views sees the same string move from the end of a bar into a cell.
 */
function ShedMetricTable({
  active,
  columns,
  metric,
  pager,
}: {
  active: ShedSeries;
  columns: ShedTableColumns;
  metric: Metric;
  pager: ShedTablePagerLabels;
}) {
  // Page state is LOCAL, not a URL param: every row is already in the browser, so paging is a
  // slice rather than a fetch, and a server round trip here would cost a page render and throw the
  // reader back up the page for a purely visual step. The metric toggle remounts this component
  // (`key={metric}`), which is what resets the reader to page 1 when the row set changes under
  // them -- the gain view drops every shed without a second weigh, so page 3 of one metric is not
  // page 3 of the other.
  const [page, setPage] = useState(0);
  const rows = active.columns.flatMap((col) => col.rows.map((row) => ({ park: col.heading, row })));
  const pageCount = Math.max(1, Math.ceil(rows.length / SHED_TABLE_PAGE_SIZE));
  const current = Math.min(page, pageCount - 1);
  const start = current * SHED_TABLE_PAGE_SIZE;
  const visible = rows.slice(start, start + SHED_TABLE_PAGE_SIZE);
  if (rows.length === 0) {
    return (
      <div className="empty">
        <span className="muted small">{active.emptyLabel}</span>
      </div>
    );
  }
  return (
    <div className="tablewrap">
      {/* Fixed layout, and the shed cell is the only one allowed to wrap. A pen label carries its
          whole breed/sex composition, which runs past a hundred characters on a mixed pen, so an
          auto-layout table sized itself to that one cell and pushed the basis and the VALUE — the
          column the card exists for — off the card's right edge behind a scrollbar. */}
      <table className="tbl wsgtable" aria-label={active.chartLabel}>
        <thead>
          <tr>
            <th className="wsg-park">{columns.park}</th>
            <th>{columns.shed}</th>
            <th className="wsg-breed">{columns.breed}</th>
            <th className="wsg-sex">{columns.sex}</th>
            <th className="num wsg-count">{columns.count}</th>
            <th className="wsg-basis">{columns.basis}</th>
            <th className="num wsg-val">{columns.value[metric]}</th>
          </tr>
        </thead>
        <tbody>
          {visible.map(({ park, row }) => (
            <tr key={row.key}>
              <td className="wsg-park">{park}</td>
              <td className="wsg-shed">
                <b>{row.shedName ?? row.label}</b>
              </td>
              {/* A pen holding more than one cohort lists each on its own line, aligned across
                  the three cells, so a reader can pair a breed with its sex and head count.
                  The GAIN stays on the row and is never repeated per cohort: a whole-shed
                  average cannot be split across breed or sex, and a per-animal shed's figure
                  is the pen's, not any one breed's. */}
              <td className="wsg-breed">
                {(row.cohorts ?? []).map((cohort, index) => (
                  <span className="wsg-line" key={`${row.key}|breed|${index}`}>
                    {cohort.breed}
                  </span>
                ))}
              </td>
              {/* One line when the whole pen is one sex, every line the moment ONE cohort
                  differs (maintainer request 2026-09-01). Repeating "male" five times down a
                  pen that is entirely male is noise, and it buried the mixed pens -- which are
                  the ones a reader has to look at -- among identical columns. Collapsing is
                  therefore all-or-nothing: a pen with a single female in it lists every line,
                  so the collapsed cell can only ever mean "this pen is all of this sex".
                  Breed and count never collapse: two cohorts really can share a breed (Godel
                  1 - Part 7 carries Osmanabadi three times), and each carries its own count. */}
              <td className="wsg-sex">
                {sexLines(row.cohorts).map((sex, index) => (
                  <span className="wsg-line" key={`${row.key}|sex|${index}`}>
                    {sex}
                  </span>
                ))}
              </td>
              <td className="num wsg-count">
                {(row.cohorts ?? []).map((cohort, index) => (
                  <span className="wsg-line" key={`${row.key}|count|${index}`}>
                    {cohort.animals.toLocaleString("en-IN")}
                  </span>
                ))}
              </td>
              <td className="wsg-basis">
                {row.modeLabel ? <Tag tone={row.modeTone ?? "mut"}>{row.modeLabel}</Tag> : null}
              </td>
              <td className={`num wsg-val${row.value < 0 ? " neg" : ""}`}>
                {row.valueLabel ??
                  `${row.value.toLocaleString("en-IN", { maximumFractionDigits: 1 })} ${active.unit}`}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {/* The mock's pager footer: range and page on the left, the two steps on the right. Buttons
          rather than links, because nothing navigates -- and the ends are disabled rather than
          hidden, so the control does not change shape as the reader walks the pages. */}
      {rows.length > 0 ? (
        <div className="pager2">
          <span className="small muted" style={{ marginRight: "auto" }}>
            {`${start + 1}-${start + visible.length} ${rows.length === 1 ? pager.noun : `${pager.noun}s`} · ${pager.page} ${current + 1} ${pager.of} ${pageCount}`}
          </span>
          <button
            className="btn sm"
            type="button"
            disabled={current === 0}
            onClick={() => setPage(current - 1)}
          >
            {pager.previous}
          </button>
          <button
            className="btn sm"
            type="button"
            disabled={current >= pageCount - 1}
            onClick={() => setPage(current + 1)}
          >
            {pager.next}
          </button>
        </div>
      ) : null}
    </div>
  );
}

export function ShedMetricChart({
  initialMetric,
  labels,
  title,
  series,
  view,
  tableColumns,
  tablePager,
}: {
  initialMetric: Metric;
  labels: MetricLabels;
  title: MetricLabels;
  series: Record<Metric, ShedSeries>;
  /**
   * Which view to render. The Table/Chart control was RETIRED from this card (maintainer request
   * 2026-09-01) -- the figures are the card now -- so nothing renders a switch any more. The prop
   * stays because `?shed_view=chart` is still honoured for links shared before the control went.
   */
  view: ShedView;
  tableColumns: ShedTableColumns;
  tablePager: ShedTablePagerLabels;
}) {
  const [metric, setMetric] = useState<Metric>(initialMetric);
  const active = series[metric];
  return (
    <section className="card wchart" aria-label={active.chartLabel}>
      <h2 className="h">
        <Scale className="ic" size={15} aria-hidden /> {title[metric]}
        <MetricToggle current={metric} labels={labels} onChange={setMetric} />
      </h2>
      <p className="muted small">{active.caption}</p>
      {view === "table" ? (
        <ShedMetricTable key={metric} active={active} columns={tableColumns} metric={metric} pager={tablePager} />
      ) : active.columns.length === 0 ? (
        <WeightBars
          data={[]}
          emptyLabel={active.emptyLabel}
          unit={active.unit}
          chartLabel={active.chartLabel}
          size={active.size}
        />
      ) : (
        <div className="wcols">
          {active.columns.map((col, index) => (
            <div key={`${col.heading}|${index}`}>
              <h3 className="wcol-h">{col.heading}</h3>
              <WeightBars
                data={col.rows}
                domain={active.domain}
                emptyLabel={active.emptyLabel}
                unit={active.unit}
                chartLabel={active.chartLabel}
                size={active.size}
              />
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
