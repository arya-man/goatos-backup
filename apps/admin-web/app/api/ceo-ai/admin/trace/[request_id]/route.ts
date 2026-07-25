import { type NextRequest } from "next/server";
import { forwardJson } from "../../../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// GET /api/ceo-ai/admin/trace/{request_id} — thin authenticated proxy to the
// backend ADMIN-ONLY step-trace debug endpoint. This is the only surface that
// exposes the internal execution trace (sub-questions, tools, params, row
// counts, latency, review verdict), and only to the ceo_internal/superadmin
// cohort — the BACKEND enforces the role gate (a non-admin session gets 403
// verbatim). admin-web adds no gating of its own and reads no business data;
// the trace is INTERNAL and never surfaces in the leadership chat answer.
export async function GET(
  _request: NextRequest,
  { params }: { params: Promise<{ request_id: string }> },
): Promise<Response> {
  const { request_id } = await params;
  const requestId = (request_id ?? "").trim().slice(0, 200);
  if (!requestId) {
    return Response.json({ error: "request_id_required" }, { status: 400 });
  }
  return forwardJson(`/ceo-ai/admin/trace/${encodeURIComponent(requestId)}`, {
    method: "GET",
  });
}
