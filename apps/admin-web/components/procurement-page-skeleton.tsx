import Box from "@mui/material/Box";
import { Skeleton, SkeletonChart, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import { PageHeaderSkeleton } from "@/components/app/page-header";
import { skeletonClasses } from "@/components/app/page-skeletons";

/**
 * The shimmer placeholder every Sales / Procurement / Routines route shows while its server data
 * is in flight. It reproduces the real page's shape — header, KPI deck, charts, table — so the
 * route settles into its layout instead of flashing a blank screen and then jumping.
 *
 * Presentation only: it renders no copy at all (a loader that guessed at headings would show
 * strings the page contract never issued) and is fully `aria-hidden` inside an `aria-busy` region.
 */
export function ProcurementPageSkeleton({
  kpis = 4,
  charts = 0,
  table = 8,
  filters = true,
  tabs = 0,
  spark = false,
  tableWidths,
  kpiPhonePairs = false,
}: {
  /** KPI tiles in the deck; 0 hides the deck. */
  kpis?: number;
  /** Chart cards below the deck. */
  charts?: number;
  /** Table rows; 0 hides the table card. */
  table?: number;
  /** A filter/chip row under the header. */
  filters?: boolean;
  /** Tab strip under the header: the number of tabs the route actually renders. 0 hides it. */
  tabs?: number;
  /** Mini spark block inside each KPI tile, for the decks that carry one. */
  spark?: boolean;
  /** Grid tracks for the table's columns, so the placeholder rows land on the real widths. */
  tableWidths?: string[];
  /** The route's real KPI deck is a 2x2 grid at phone width (KpiGrid), not a single column. */
  kpiPhonePairs?: boolean;
}) {
  // `.kit-sk-group` is a GRID, so the chip row below declares `display:flex` outright: a bare
  // `flexDirection:"row"` left the chips stacked in a column on every Sales/Procurement loader.
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      {filters ? (
        <div className={skeletonClasses.group} style={{ display: "flex", flexWrap: "wrap", gap: 8, marginBottom: 14 }}>
          {[96, 120, 88, 140].map((width) => (
            <Skeleton key={width} width={width} height={32} radius={999} />
          ))}
        </div>
      ) : null}
      {tabs > 0 ? (
        <div className={skeletonClasses.group} style={{ display: "flex", gap: 18, marginBottom: 16, alignItems: "center" }}>
          {Array.from({ length: tabs }, (_, index) => (
            <Skeleton key={index} width={72 + ((index * 23) % 44)} height={14} radius={6} />
          ))}
        </div>
      ) : null}
      {kpis > 0 ? (
        // Routes whose real deck is KpiGrid load two widgets per row on a phone, like the page.
        <Box sx={kpiPhonePairs ? { "@media (max-width:599.95px)": { "& .kit-sk-kpis": { gridTemplateColumns: "repeat(2,minmax(0,1fr))", gap: 1.5 } } } : undefined}>
          <SkeletonKpiRow count={kpis} spark={spark} />
        </Box>
      ) : null}
      {Array.from({ length: charts }, (_, index) => (
        <SkeletonChart key={index} bars={14} height={200} />
      ))}
      {table > 0 ? (
        <div className={skeletonClasses.card}>
          <Skeleton width="30%" height={18} />
          <SkeletonTable rows={table} widths={tableWidths} />
        </div>
      ) : null}
    </div>
  );
}
