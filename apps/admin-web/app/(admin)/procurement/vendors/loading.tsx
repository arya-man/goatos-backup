import { ProcurementPageSkeleton } from "@/components/procurement-page-skeleton";

// Shimmer placeholder shaped like this route's real layout, so the page settles in instead of
// flashing raw. Presentation only -- it renders no copy.
export default function Loading() {
  return <ProcurementPageSkeleton kpis={0} charts={0} table={12} tableWidths={["2fr", "1.2fr", "1fr", "0.8fr", "0.8fr", "44px"]} />;
}
