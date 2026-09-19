import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// telemetry:exempt server-rendered mount of the shared SOP module page; route errors are caught by the parent (admin)/sales/error.tsx boundary and the editor's own actions carry the SOP telemetry

// Sales › Sales SOP — what happens after a sale is recorded (tagging, loading, the money) and WHO
// does each step, authored with the same List | Flow editor the herd operations use and run by
// the tasks engine as one workflow per sale (maintainer instruction 2026-09-19,
// docs/decisions/sales-sop.md). Same library + operator-steps editor shape as /counts/sops.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("sales-sops", "sales", "/sales/sops", searchParams);
}
