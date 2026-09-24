import { NextResponse } from "next/server";
import { forwardJson } from "../_forward";

// Shared plumbing for the Ask Mesha write-action routes (confirm / cancel).
// admin-web stays a thin proxy: the coding-agent service (CEO_AI_AGENT_URL) owns
// the proposal, its expiry, the permission check, execution and audit. The user's
// bearer + tenant are forwarded by forwardJson like every other /api/ceo-ai route.
// The legacy backend has no actions endpoint, so without the flag nothing is sent.

const ID_RE = /^[A-Za-z0-9_.:-]{1,100}$/;
const NO_STORE = { "Cache-Control": "no-store" } as const;

function fail(status: number, error: string, message: string): Response {
  return NextResponse.json({ ok: false, status: error, error, message }, { status, headers: NO_STORE });
}

export async function forwardAction(
  request: Request,
  id: string,
  verb: "confirm" | "cancel",
): Promise<Response> {
  if (!ID_RE.test(id)) return fail(400, "invalid_proposal", "This action link is not valid.");
  if (!process.env.CEO_AI_AGENT_URL) {
    return fail(404, "actions_unavailable", "Actions are not available on this assistant yet.");
  }
  let body: Record<string, unknown> = {};
  if (verb === "confirm") {
    try {
      const raw = (await request.json()) as unknown;
      if (raw && typeof raw === "object") body = raw as Record<string, unknown>;
    } catch {
      body = {};
    }
  }
  // Only the typed confirmation is forwarded; nothing else from the client body.
  const forwarded = verb === "confirm" && body.confirm_text === "CONFIRM" ? { confirm_text: "CONFIRM" } : {};
  return forwardJson(`/ceo-ai/actions/${encodeURIComponent(id)}/${verb}`, {
    method: "POST",
    body: JSON.stringify(forwarded),
    // No request.signal: a confirm already sent must run to its result even if
    // the tab closes; the outcome is also posted into the chat.
  });
}
