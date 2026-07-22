import { CeoAiAdminTraceViewer } from "@/features/ceo-ai-admin";
import { one, type RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / engineering ONLY — the leadership-assistant step-trace debug surface.
// This is the sanctioned place to inspect one request's internal execution
// trace (sub-questions, tool/tier, redacted params, row counts, latency, review
// verdict) WITHOUT leaking it into the leadership chat answer (Internal Tracking
// rule). The real security gate is server-side: the backend
// GET /ceo-ai/admin/trace/{request_id} endpoint enforces the ceo_internal
// (superadmin) role and tenant scope; a non-admin session gets 403, surfaced
// honestly by the viewer. This page holds no business data and no gating logic.
//
// Not in the sidebar: nav is backend-composed from department module grants
// (same current state as /verification and /operations/dlq). Reachable by direct
// route; optionally deep-linked with ?request_id=... from an audit row.
//
// Telemetry (TELEMETRY GUARDRAIL, AGENTS.md): this route is covered globally by
// ObservabilityErrorBoundary (app/(admin)/layout.tsx), and the viewer fires real
// Faro pushEvents (open / lookup / loaded / error) from
// features/ceo-ai-admin/telemetry.ts.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const params = await searchParams;
  const initialRequestId = one(params, "request_id") ?? "";
  return (
    <div className="p-6">
      <CeoAiAdminTraceViewer initialRequestId={initialRequestId} />
    </div>
  );
}
