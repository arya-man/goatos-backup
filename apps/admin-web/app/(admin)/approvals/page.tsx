import { redirect } from "next/navigation";
import { ApprovalsPage } from "@/features/approvals";
import { adminWebLandingHref, adminWebRouteOffered } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Approvals — top-level decision surface (maintainer decision 2026-07-21). The pending
// birth/death/shifting approval queue, moved off mobile onto admin-web and gated server-side to the
// four org tiers (director/head/manager/am) + admin + ceo_internal via counts.approve_access. The
// backend nav item (backend/internal/adminui/app/service.go) is RBAC-disabled for anyone lacking
// that permission (compiler.go permissionsForNav), and the /admin-web/counts/approvals endpoints
// enforce it independently, so an out-of-authority caller sees no data and cannot decide.
//
// No backend admin-ui PAGE contract exists for "approvals" yet, so this renders from local literal
// copy (features/approvals/copy.ts) — the same documented exception /verification uses.
//
// Telemetry (TELEMETRY GUARDRAIL, AGENTS.md): route-change/error covered globally by
// ObservabilityErrorBoundary (app/(admin)/layout.tsx); the primary approve/reject action fires a
// Faro pushEvent from features/approvals/approvals-telemetry.tsx inside the drawer.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, routeOffered] = await Promise.all([searchParams, adminWebRouteOffered("/approvals")]);
  // Local literal copy means there is no page contract to withhold from an out-of-authority
  // principal, so this route must check reachability itself. Without it, the verifier-only
  // workspace (which drops every other page contract) would still render the full Approvals
  // chrome — Birth/Death/Shifting tabs and queue — to anyone who typed the URL, even though the
  // decision endpoints independently 403 on counts.approve_access.
  if (!routeOffered) {
    redirect((await adminWebLandingHref()) ?? "/verify");
  }
  return <ApprovalsPage searchParams={params} />;
}
