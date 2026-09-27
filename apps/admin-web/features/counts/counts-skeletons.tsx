import {
  ChartCardSkeleton,
  ControlRowSkeleton,
  FieldSkeleton,
  GridSkeleton,
  KpiCardSkeleton,
  KpiRowSkeleton,
  StackSkeleton,
  TableSkeleton,
  type KpiShape,
} from "@/components/app/skeletons";
import {
  BD_GRID,
  BD_KPI_CAPTIONS,
  BD_PLOT,
  COUNTS_WINDOW_HEIGHT,
  COUNTS_WINDOW_WIDTH,
  HA_GRID,
  HA_KPI_CAPTIONS,
  HA_PLOT,
  HERD_KPI_COUNT,
  HERD_KPI_SIZE,
  MORTALITY_KPI_CAPTIONS,
  MORTALITY_PLOT,
  MORTALITY_RATE_SIZE,
  MORTALITY_RATE_TABLE,
} from "./counts-layout";

// Loading twins for the Counts pages, composed ONLY from the shared skeleton blocks and counts-layout.ts
// (which the pages read too). Each route loading.tsx and the page's UrlSuspense fallback render the same
// component, so a hard load, a sidebar click and a window / filter change paint one shape.

/** A KpiWidget course card per entry: no caption line (0), or the caption's lines (per breakpoint). */
const kpiShapes = (captions: (number | { xs: number; sm: number })[]): KpiShape[] => captions.map((lines) => (lines === 0 ? {} : { hint: true, hintLines: lines }));

/** The DateRangePicker window control (/counts/analytics, /counts/mortality). */
export function CountsWindowSkeleton() {
  return (
    <ControlRowSkeleton>
      <FieldSkeleton width={COUNTS_WINDOW_WIDTH} height={COUNTS_WINDOW_HEIGHT} />
    </ControlRowSkeleton>
  );
}

/** /counts/analytics panel: the KPI cards, flow beside sex, breed beside the stacked stage / age mixes. */
export function HerdAnalyticsPanelSkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <KpiRowSkeleton count={HA_KPI_CAPTIONS.length} shapes={kpiShapes(HA_KPI_CAPTIONS)} />
      <GridSkeleton
        items={[
          { size: HA_GRID.flow, node: <ChartCardSkeleton height={HA_PLOT.flow} /> },
          { size: HA_GRID.sex, node: <ChartCardSkeleton height={HA_PLOT.sex} /> },
        ]}
      />
      <GridSkeleton
        items={[
          { size: HA_GRID.mix, node: <ChartCardSkeleton height={HA_PLOT.breed} /> },
          {
            size: HA_GRID.mix,
            node: (
              <StackSkeleton>
                <ChartCardSkeleton height={HA_PLOT.stage} />
                <ChartCardSkeleton height={HA_PLOT.age} />
              </StackSkeleton>
            ),
          },
        ]}
      />
    </StackSkeleton>
  );
}

/** /counts/breakdown KPI deck (the first card, the live head count, has no caption line). */
export function BreakdownKpiSkeleton() {
  return <KpiRowSkeleton count={BD_KPI_CAPTIONS.length} shapes={kpiShapes(BD_KPI_CAPTIONS)} />;
}

/** /counts/breakdown charts: breed share beside stage x sex, then the pen bars. */
export function BreakdownChartsSkeleton() {
  return (
    <GridSkeleton
      items={[
        { size: BD_GRID.breed, node: <ChartCardSkeleton height={BD_PLOT.breed} /> },
        { size: BD_GRID.stageSex, node: <ChartCardSkeleton height={BD_PLOT.stageSex} /> },
        { size: BD_GRID.pens, node: <ChartCardSkeleton height={BD_PLOT.pens} /> },
      ]}
    />
  );
}

/** /counts/herd make-up KPI cards (each with its caption line). */
export function HerdKpiSkeleton() {
  return <GridSkeleton items={Array.from({ length: HERD_KPI_COUNT }, () => ({ size: HERD_KPI_SIZE, node: <KpiCardSkeleton hint /> }))} />;
}

/** /counts/mortality panel: the KPI cards, the monthly deaths chart, the rate cards two to a row. */
export function MortalityPanelSkeleton() {
  return (
    <StackSkeleton spacing={3}>
      <KpiRowSkeleton count={MORTALITY_KPI_CAPTIONS.length} shapes={kpiShapes(MORTALITY_KPI_CAPTIONS)} />
      <ChartCardSkeleton height={MORTALITY_PLOT.monthly} />
      <GridSkeleton
        items={Array.from({ length: MORTALITY_RATE_TABLE.cards }, () => ({
          size: MORTALITY_RATE_SIZE,
          node: <TableSkeleton columns={MORTALITY_RATE_TABLE.columns} rows={MORTALITY_RATE_TABLE.rows} pager={false} />,
        }))}
      />
    </StackSkeleton>
  );
}
