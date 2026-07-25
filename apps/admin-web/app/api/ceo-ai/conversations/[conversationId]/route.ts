import { type NextRequest } from "next/server";
import { forwardJson } from "../../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = { params: Promise<{ conversationId: string }> };

function encodeId(id: string): string {
  return encodeURIComponent(id);
}

// GET    /api/ceo-ai/conversations/{id} — thread + keyset message history.
// PATCH  /api/ceo-ai/conversations/{id} — rename thread ({ title }).
// DELETE /api/ceo-ai/conversations/{id} — soft-delete thread.

export async function GET(request: NextRequest, ctx: RouteContext): Promise<Response> {
  const { conversationId } = await ctx.params;
  const url = new URL(request.url);
  const cursor = url.searchParams.get("cursor");
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  return forwardJson(`/ceo-ai/conversations/${encodeId(conversationId)}/messages${query}`, {
    method: "GET",
    signal: request.signal,
  });
}

export async function PATCH(request: NextRequest, ctx: RouteContext): Promise<Response> {
  const { conversationId } = await ctx.params;
  let raw = "";
  try {
    raw = await request.text();
  } catch {
    raw = "";
  }
  return forwardJson(`/ceo-ai/conversations/${encodeId(conversationId)}`, {
    method: "PATCH",
    body: raw || "{}",
    signal: request.signal,
  });
}

export async function DELETE(request: NextRequest, ctx: RouteContext): Promise<Response> {
  const { conversationId } = await ctx.params;
  return forwardJson(`/ceo-ai/conversations/${encodeId(conversationId)}`, {
    method: "DELETE",
    signal: request.signal,
  });
}
