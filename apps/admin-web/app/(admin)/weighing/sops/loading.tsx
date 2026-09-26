import { SopRouteSkeleton } from "@/components/sop-route-skeleton";

/**
 * Route-shaped shimmer. One shared shape for all five module SOP libraries so it cannot drift, and
 * it branches to the editor shape on `?compose=1` (the builder shares this route).
 */
export default function Loading() {
  return <SopRouteSkeleton />;
}
