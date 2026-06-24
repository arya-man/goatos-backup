import { ProtocolAdherencePage } from "@/features/process-integrity";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Protocol Adherence is a top-level command screen. Vaccination-only is the data scope, not the UI hierarchy.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <ProtocolAdherencePage searchParams={await searchParams} />;
}
