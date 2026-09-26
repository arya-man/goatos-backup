import { ProcurementPageSkeleton } from "@/components/procurement-page-skeleton";

// Shimmer placeholder shaped like this route's real layout, so the page settles in instead of
// flashing raw. Presentation only -- it renders no copy.
export default function Loading() {
  return <ProcurementPageSkeleton kpis={0} charts={0} table={10} tableWidths={["1.4fr", "1.6fr", "0.9fr", "0.9fr", "0.9fr", "0.8fr"]} />;
}
