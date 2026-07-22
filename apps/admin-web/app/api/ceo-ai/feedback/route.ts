import { type NextRequest } from "next/server";
import { forwardJson } from "../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// POST /api/ceo-ai/feedback — thumbs up/down + optional reason on one answer.
// Body: { message_id, rating: "up"|"down", reason? }. Proxied to the backend
// answer-feedback capture that mines the eval set. actor identity stays
// server-side/sensitive; the browser only sends the message id + rating.

type FeedbackBody = {
  message_id?: unknown;
  rating?: unknown;
  reason?: unknown;
};

export async function POST(request: NextRequest): Promise<Response> {
  let body: FeedbackBody;
  try {
    body = (await request.json()) as FeedbackBody;
  } catch {
    return Response.json({ error: "invalid_json" }, { status: 400 });
  }

  const messageId = typeof body.message_id === "string" ? body.message_id : "";
  const rating = body.rating === "up" || body.rating === "down" ? body.rating : "";
  if (!messageId || !rating) {
    return Response.json({ error: "message_id_and_rating_required" }, { status: 400 });
  }

  const payload: Record<string, unknown> = { rating };
  if (typeof body.reason === "string" && body.reason.trim()) {
    payload.reason = body.reason.trim().slice(0, 1000);
  }

  return forwardJson(`/ceo-ai/messages/${encodeURIComponent(messageId)}/feedback`, {
    method: "POST",
    body: JSON.stringify(payload),
    signal: request.signal,
  });
}
