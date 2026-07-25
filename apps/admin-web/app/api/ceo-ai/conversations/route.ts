import { type NextRequest } from "next/server";
import { forwardJson } from "../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// GET  /api/ceo-ai/conversations?cursor=&limit= — keyset-paginated thread list.
// POST /api/ceo-ai/conversations — create a new (empty) thread.
// Backend owns tenant/user scope, keyset pagination, soft-delete, and titles.

export async function GET(request: NextRequest): Promise<Response> {
  const url = new URL(request.url);
  const query = new URLSearchParams();
  const cursor = url.searchParams.get("cursor");
  const limit = url.searchParams.get("limit");
  if (cursor) query.set("cursor", cursor);
  if (limit) query.set("limit", limit);
  const suffix = query.toString() ? `?${query.toString()}` : "";
  return forwardJson(`/ceo-ai/conversations${suffix}`, { method: "GET", signal: request.signal });
}

export async function POST(request: NextRequest): Promise<Response> {
  let raw = "";
  try {
    raw = await request.text();
  } catch {
    raw = "";
  }
  return forwardJson("/ceo-ai/conversations", {
    method: "POST",
    body: raw || "{}",
    signal: request.signal,
  });
}
