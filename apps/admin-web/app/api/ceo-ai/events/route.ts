import { type NextRequest } from "next/server";
import { forwardJson } from "../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// POST /api/ceo-ai/events — the panel's fire-and-forget "user pressed Stop"
// signal, sent just before it aborts the answer stream, so the assistant can log
// ask_stopped with reason stop_pressed instead of client_closed. Only the
// coding-agent service (CEO_AI_AGENT_URL) has this endpoint; the legacy backend
// does not, so without the flag this answers 204 and forwards nothing.

// watch_stop: "Stop watching" on a live tag watch card (ends the watch, not the answer).
const KINDS = new Set(["stop_pressed", "watch_stop"]);

export async function POST(request: NextRequest): Promise<Response> {
  let body: Record<string, unknown>;
  try {
    body = (await request.json()) as Record<string, unknown>;
  } catch {
    return Response.json({ error: "invalid_json" }, { status: 400 });
  }
  const requestId = typeof body.request_id === "string" ? body.request_id.slice(0, 100) : "";
  const kind = typeof body.kind === "string" ? body.kind : "";
  if (!requestId || !KINDS.has(kind)) return Response.json({ error: "invalid_event" }, { status: 400 });
  if (!process.env.CEO_AI_AGENT_URL) return new Response(null, { status: 204 });
  const upstream = await forwardJson("/ceo-ai/events", {
    method: "POST",
    body: JSON.stringify({ request_id: requestId, kind }),
  });
  return upstream.status === 204 ? new Response(null, { status: 204 }) : upstream;
}
