import { ApprovalsPage } from "@/features/approvals";
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
  return <ApprovalsPage searchParams={await searchParams} />;
}
