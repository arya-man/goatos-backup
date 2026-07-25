import { type NextRequest } from "next/server";
import { forwardJson } from "../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// GET /api/ceo-ai/starters — backend-owned, tenant/role-aware suggested/starter
// questions (the backend picks starters truthful to the tools it can currently
// answer). This endpoint doubles as the leadership CAPABILITY probe the client
// uses to decide whether to render the assistant bubble: a 200 means the
// backend authorized this session as leadership (ceo_internal); a 403 means it
// did not, so the bubble stays hidden. That replaces the old spoofable client
// display-name regex — the backend permission is the security boundary.

export async function GET(request: NextRequest): Promise<Response> {
  return forwardJson("/ceo-ai/starters", { method: "GET", signal: request.signal });
}
