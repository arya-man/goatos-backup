import { type NextRequest } from "next/server";
import { forwardAction } from "../../_action";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type RouteContext = { params: Promise<{ id: string }> };

// POST /api/ceo-ai/actions/{proposal_id}/cancel — the CEO's cancel on an Ask Mesha
// write-action card, proxied to the assistant service (see ../../_action.ts).
export async function POST(request: NextRequest, ctx: RouteContext): Promise<Response> {
  const { id } = await ctx.params;
  return forwardAction(request, id, "cancel");
}
