"use client";
import { chartClasses } from "@/components/minimal/chart/classes";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useState } from "react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";

import { EmptyState } from "@/components/app/empty-state";
import { ChartSelect } from "@/components/minimal/chart";
import { TablePaginationCustom } from "@/components/minimal/table";
import { ConversionRatesCard } from "@/components/app/conversion-rates-card";
import { Tag } from "@/components/ui-primitives";
import type { WeightBar } from "./weight-bars";
import { stageLabel } from "@/lib/stage-labels";

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

/** The template chart-card select (CardHeader action): daily gain or weight, client state only. */
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
    <ChartSelect
      options={[labels.adg, labels.weight]}
      value={labels[current]}
      onChange={(next) => onChange(next === labels.weight ? "weight" : "adg")}
    />
  );
}

/** The bars as the template conversion-rates chart: one category per bar, its chip in the tooltip. */
function barChart(active: MetricSeries, name: string) {
  return {
    categories: active.data.map((bar) => stageLabel(bar.label)),
    unit: active.unit,
    digits: active.unit === "kg" ? 1 : 0,
    series: [
      {
        name,
        data: active.data.map((bar) => bar.value),
        notes: active.data.map((bar) => bar.modeLabel ?? null),
      },
    ],
  };
}

/** Plot height for a one-to-three bar chart (sex, stage). */
const SHORT_PLOT = 220;

export function MetricChart({
  initialMetric,
  labels,
  title,
  caption,
  series,
  size = "short",
  wide = false,
}: {
  initialMetric: Metric;
  labels: MetricLabels;
  title: MetricLabels;
  caption?: string;
  series: Record<Metric, MetricSeries>;
  size?: "tall" | "short";
  wide?: boolean;
}) {
  const [metric, setMetric] = useState<Metric>(initialMetric);
  const active = series[metric];
  void size;
  void wide;
  return (
    <ConversionRatesCard
      key={metric}
      aria-label={active.chartLabel}
      title={title[metric]}
      subheader={caption}
      action={<MetricToggle current={metric} labels={labels} onChange={setMetric} />}
      empty={<EmptyState title={active.emptyLabel} />}
      chart={barChart(active, labels[metric])}
      // Sex / stage usually carry one to three bars: the template's 360px plot would leave them
      // floating, so a short list gets a 220px plot (set here, the section stays verbatim).
      sx={{ height: 1, ...(active.data.length <= 3 ? { [`& .${chartClasses.root}`]: { height: SHORT_PLOT } } : {}) }}
    />
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
  const [page, setPage] = useState(0);
  const rows = active.columns.flatMap((col) => col.rows.map((row) => ({ park: col.heading, row })));
  const pageCount = Math.max(1, Math.ceil(rows.length / SHED_TABLE_PAGE_SIZE));
  const current = Math.min(page, pageCount - 1);
  const start = current * SHED_TABLE_PAGE_SIZE;
  const visible = rows.slice(start, start + SHED_TABLE_PAGE_SIZE);
  if (rows.length === 0) {
    return <EmptyState title={active.emptyLabel} />;
  }
  // A pen holding more than one cohort lists each on its own line, aligned across the breed, sex
  // and count cells, so a reader can pair a breed with its sex and head count. The GAIN stays on
  // the row and is never repeated per cohort: a whole-shed average cannot be split across breed or
  // sex. Sex collapses to ONE line only when the whole pen is one sex (maintainer 2026-09-01).
  const lines = (items: readonly string[], key: string) =>
    items.map((item, index) => (
      <Box component="span" key={`${key}|${index}`} sx={{ display: "block" }}>
        {item}
      </Box>
    ));
  return (
    <>
      <Box sx={{ overflowX: "auto" }}>
        <Table aria-label={active.chartLabel}>
          <TableHead>
            <TableRow>
              <TableCell component="th">{columns.park}</TableCell>
              <TableCell component="th">{columns.shed}</TableCell>
              <TableCell component="th">{columns.breed}</TableCell>
              <TableCell component="th">{columns.sex}</TableCell>
              <TableCell component="th" align="right">{columns.count}</TableCell>
              <TableCell component="th">{columns.basis}</TableCell>
              <TableCell component="th" align="right">{columns.value[metric]}</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {visible.map(({ park, row }) => (
              <TableRow key={row.key} hover>
                <TableCell>{park}</TableCell>
                <TableCell sx={{ typography: "subtitle2" }}>{row.shedName ?? row.label}</TableCell>
                <TableCell>{lines((row.cohorts ?? []).map((cohort) => cohort.breed), `${row.key}|breed`)}</TableCell>
                <TableCell>{lines(sexLines(row.cohorts), `${row.key}|sex`)}</TableCell>
                <TableCell align="right">
                  {lines((row.cohorts ?? []).map((cohort) => cohort.animals.toLocaleString("en-IN")), `${row.key}|count`)}
                </TableCell>
                <TableCell>{row.modeLabel ? <Tag tone={row.modeTone ?? "mut"}>{row.modeLabel}</Tag> : null}</TableCell>
                <TableCell align="right" sx={{ typography: "subtitle2", ...(row.value < 0 ? { color: "error.main" } : {}) }}>
                  {row.valueLabel ?? `${row.value.toLocaleString("en-IN", { maximumFractionDigits: 1 })} ${active.unit}`}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </Box>
      {/* The template table pagination, client-side over the rows already served (nothing
          navigates): range and arrows, ends disabled rather than hidden. */}
      <TablePaginationCustom
        count={rows.length}
        page={current}
        rowsPerPage={SHED_TABLE_PAGE_SIZE}
        rowsPerPageOptions={[SHED_TABLE_PAGE_SIZE]}
        onPageChange={(_event, next) => setPage(next)}
        labelDisplayedRows={({ from, to, count }) =>
          `${from}-${to} ${count === 1 ? pager.noun : `${pager.noun}s`} · ${pager.page} ${current + 1} ${pager.of} ${pageCount}`
        }
        getItemAriaLabel={(type) => (type === "previous" ? pager.previous : pager.next)}
      />
    </>
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
  if (view === "chart") {
    // One chart over every park column, each bar named with its park (the column heading).
    const data = active.columns.flatMap((col) => col.rows.map((row) => ({ ...row, label: `${col.heading} · ${row.label}` })));
    return (
      <ConversionRatesCard
        key={metric}
        aria-label={active.chartLabel}
        title={title[metric]}
        subheader={active.caption}
        action={<MetricToggle current={metric} labels={labels} onChange={setMetric} />}
        empty={<EmptyState title={active.emptyLabel} />}
        chart={barChart({ ...active, data }, labels[metric])}
      />
    );
  }
  return (
    <Card aria-label={active.chartLabel}>
      <CardHeader
        title={title[metric]}
        subheader={active.caption}
        action={<MetricToggle current={metric} labels={labels} onChange={setMetric} />}
        sx={{ mb: 3 }}
      />
      <ShedMetricTable key={metric} active={active} columns={tableColumns} metric={metric} pager={tablePager} />
    </Card>
  );
}
