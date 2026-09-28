import { SopRouteSkeleton } from "@/features/sops/sop-route-skeleton";
import { SOP_HEADER_ACTIONS } from "@/features/sops/sop-library-layout";

/** Module SOP library (+ the Assumptions header button this route mounts): the one shared SOP library shape (builder shape on ?compose=1 / ?edit=). */
export default function Loading() {
  return <SopRouteSkeleton actionWidths={SOP_HEADER_ACTIONS.withAssumptions} />;
}
