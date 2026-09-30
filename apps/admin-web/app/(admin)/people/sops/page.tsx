import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// HRMS › HRMS SOP (maintainer instruction 2026-09-30): the violation types and their fines, and the
// enquiries farm events open -- every list authored here, by HR and the CEO. HR reaches this SOP and
// no other (hrms.sop.author; the SOP adapter narrows it to hrms.* codes).
// telemetry:exempt shared SOP library + editor, instrumented in features/sops
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("people-sops", "hrms", "/people/sops", searchParams);
}
