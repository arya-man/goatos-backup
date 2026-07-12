import { VerificationReviewPage } from "@/features/verification-review";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Admin / Data Ops -> Verification (AUTHORITY act screen). Same authority tier as /config and /sops
// per context/architecture/verification-module-design.md §2.1: "a top-level Admin / Data Ops
// command screen (/verification), category/vertical-filtered — same authority tier as Config and
// SOP Library." Head/Director/CEO review the standalone Verifier's approve/reject media queue and
// act on the linked SOP task (rework / re-assign / penalty note). The verifier's verdict itself is
// advisory input recorded by a separate app (context/architecture/verifier-app-and-flow.md); this
// screen never writes a verdict.
//
// Not yet in the sidebar: nav is backend-composed from department module grants
// (docs/decisions/role-module-nav-composition.md) and the Verification module has no nav-registry
// contribution yet (same current state as /operations/dlq). Reachable by direct route until that
// backend nav-registry entry lands.
//
// Telemetry (TELEMETRY GUARDRAIL, AGENTS.md): this route is covered globally by
// ObservabilityErrorBoundary (app/(admin)/layout.tsx), and its primary action fires a real Faro
// `pushEvent` from features/verification-review/verification-review-telemetry.tsx, rendered inside
// VerificationReviewPage's drawer — see that file for the pushEvent/faro wiring.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  return <VerificationReviewPage searchParams={await searchParams} />;
}
