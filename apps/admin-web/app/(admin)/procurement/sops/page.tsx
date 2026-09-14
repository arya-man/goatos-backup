import { renderSopModulePage } from "@/features/sops";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Procurement › Procurement SOP — the animal purchase inspection document (maintainer decision
// 2026-09-14): pages of questions, proof and compulsory flags, served to the phone from the
// published version.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return renderSopModulePage("procurement-sops", "procurement", "/procurement/sops", searchParams);
}
