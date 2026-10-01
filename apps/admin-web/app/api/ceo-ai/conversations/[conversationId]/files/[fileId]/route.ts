import { type NextRequest } from "next/server";
import { forwardBinary } from "../../../../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = { params: Promise<{ conversationId: string; fileId: string }> };

// GET /api/ceo-ai/conversations/{id}/files/{fileId} — an attachment the user
// sent in that thread (owner-checked by the assistant backend).
export async function GET(request: NextRequest, ctx: RouteContext): Promise<Response> {
  const { conversationId, fileId } = await ctx.params;
  return forwardBinary(
    `/ceo-ai/conversations/${encodeURIComponent(conversationId)}/files/${encodeURIComponent(fileId)}`,
    { method: "GET", signal: request.signal },
  );
}
