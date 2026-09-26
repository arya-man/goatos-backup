import { ProcurementPageSkeleton } from "@/components/procurement-page-skeleton";

// Shimmer placeholder shaped like this route's real layout, so the page settles in instead of
// flashing raw. Presentation only -- it renders no copy.
export default function Loading() {
  return <ProcurementPageSkeleton kpis={4} charts={3} table={8} spark tableWidths={["1.4fr", "1.2fr", "1fr", "0.9fr", "0.9fr", "0.7fr"]} />;
}
