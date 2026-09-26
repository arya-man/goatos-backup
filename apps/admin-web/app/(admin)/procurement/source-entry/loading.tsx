import { ProcurementPageSkeleton } from "@/components/procurement-page-skeleton";

// Shimmer placeholder shaped like the Source Entry Board's real layout (board columns read as one
// wide table here). Presentation only -- it renders no copy.
export default function Loading() {
  return <ProcurementPageSkeleton kpis={4} charts={0} table={10} tabs={5} tableWidths={["1.2fr", "1.6fr", "1fr", "0.9fr", "0.9fr", "0.8fr"]} />;
}
