import { SopRouteSkeleton } from "@/components/sop-route-skeleton";

/**
 * Route-shaped shimmer. The same shared shape the other module SOP libraries use, so the PC Care
 * library cannot drift from them; it branches to the editor shape on `?compose=1`.
 */
export default function Loading() {
  return <SopRouteSkeleton />;
}
