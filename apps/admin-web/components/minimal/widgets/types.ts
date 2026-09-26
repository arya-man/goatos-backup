import type { CardProps } from '@mui/material/Card';
import type { ChartOptions } from '../chart';

export type WidgetValue = number | string;

export type WidgetSummaryBaseProps = Omit<CardProps, 'title'> & {
  title: React.ReactNode;
  /** A number is formatted (en-IN); a string is rendered as-is (pre-formatted by the page). */
  total: WidgetValue;
  /** Signed change; omit to hide the trend row. */
  percent?: number;
  /** Caption after the trend, e.g. "last 7 days". */
  caption?: React.ReactNode;
  /** Template mini chart (ApexCharts sparkline). Fewer than two points draws no chart. */
  chart?: WidgetChart;
};

/** The template widgets' `chart` prop: one series of numbers, optional x categories for the tooltip. */
export type WidgetChart = {
  colors?: string[];
  categories?: string[];
  series: number[];
  options?: ChartOptions;
};

export function hasWidgetChart(chart: WidgetChart | undefined): chart is WidgetChart {
  return Boolean(chart && chart.series.length > 1);
}
