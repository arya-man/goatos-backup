// Feed Analytics chart marks.
//
// The implementation moved to components/svg-series.tsx when Herd Analytics
// started drawing the same marks over calendar months: two hand-rolled copies of
// one chart is how two screens begin disagreeing about how a fact looks. This
// file keeps the feed-named exports the page already imports, so nothing about
// the Feed Analytics page changes.

export {
  SERIES_VARS as FEED_SERIES_VARS,
  StackedColumns as FeedStackedColumns,
  SeriesLines as FeedLines,
  SeriesLegend as FeedChartLegend,
  SeriesPie as FeedSpendPie,
  seriesColorVar,
} from "@/components/svg-series";
export type { LineSeries, PieSlice, StackedDay } from "@/components/svg-series";
