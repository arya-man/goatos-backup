import { HerdRegisterPage } from "@/features/counts";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Counts -> Herd Register. Operational Counts module surface and the real business entry point for the
// vaccination cascade (register/import a goat -> goat.created -> obligation generation).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <HerdRegisterPage searchParams={await searchParams} pageContract={await requireAdminWebPageContract("herd-register")} />;
}
