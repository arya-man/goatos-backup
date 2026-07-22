import { type NextRequest } from "next/server";
import { forwardStream } from "../_forward";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// POST /api/ceo-ai/ask — thin authenticated proxy to the Mesha backend
// leadership assistant. The browser sends { question, conversation_id?,
// stream?, locale? }; the backend returns either an SSE token stream (default,
// ChatGPT-class progressive render) whose terminal event carries
// { answer, source, mode, request_id, conversation_id, citations }, or a JSON
// answer when streaming is disabled or on an honest error. This handler adds no
// routing, no tool selection, and no business data — that is all backend-owned.

type AskBody = {
  question?: unknown;
  conversation_id?: unknown;
  stream?: unknown;
  locale?: unknown;
};

export async function POST(request: NextRequest): Promise<Response> {
  let body: AskBody;
  try {
    body = (await request.json()) as AskBody;
  } catch {
    return Response.json({ error: "invalid_json" }, { status: 400 });
  }

  const question = typeof body.question === "string" ? body.question.trim().slice(0, 1200) : "";
  if (!question) {
    return Response.json({ error: "question_required" }, { status: 400 });
  }

  const payload: Record<string, unknown> = {
    question,
    stream: body.stream === false ? false : true,
  };
  if (typeof body.conversation_id === "string" && body.conversation_id) {
    payload.conversation_id = body.conversation_id;
  }
  if (typeof body.locale === "string" && body.locale) {
    payload.locale = body.locale;
  }

  return forwardStream("/ceo-ai/ask", {
    method: "POST",
    body: JSON.stringify(payload),
    signal: request.signal,
  });
}
