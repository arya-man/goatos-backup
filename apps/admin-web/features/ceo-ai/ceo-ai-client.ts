import type { ConversationSummary, StoredMessage } from "./types";

// Browser-side fetchers for the assistant proxy routes. Every call goes to
// /api/ceo-ai/* (the thin authenticated proxy); the browser never talks to the
// backend, Cube, Toolbox, Vertex, or Postgres directly. Answer streaming lives
// in lib/ceo-ai-stream.ts; this module covers the non-streaming REST surface:
// leadership capability probe, thread CRUD, and feedback.

export type CapabilityResult = {
  allowed: boolean;
  starters: string[];
};

// probeCapability hits the starters endpoint, which doubles as the leadership
// authorization probe. 200 => leadership (show bubble) with backend-owned
// starters; 403 => not leadership (hide bubble); anything else => treat as not
// available yet (hide) without throwing.
export async function probeCapability(signal?: AbortSignal): Promise<CapabilityResult> {
  try {
    const res = await fetch("/api/ceo-ai/starters", { signal, cache: "no-store" });
    if (!res.ok) return { allowed: false, starters: [] };
    const body = (await res.json()) as { starters?: unknown };
    const starters = Array.isArray(body.starters)
      ? body.starters.filter((s): s is string => typeof s === "string")
      : [];
    return { allowed: true, starters };
  } catch {
    return { allowed: false, starters: [] };
  }
}

export async function listConversations(
  cursor?: string,
  signal?: AbortSignal,
): Promise<{ conversations: ConversationSummary[]; next_cursor?: string }> {
  const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
  const res = await fetch(`/api/ceo-ai/conversations${query}`, { signal, cache: "no-store" });
  if (!res.ok) return { conversations: [] };
  const body = (await res.json()) as {
    conversations?: ConversationSummary[];
    items?: ConversationSummary[];
    next_cursor?: string;
  };
  return {
    conversations: body.conversations ?? body.items ?? [],
    next_cursor: body.next_cursor,
  };
}

export async function createConversation(signal?: AbortSignal): Promise<ConversationSummary | null> {
  const res = await fetch("/api/ceo-ai/conversations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}",
    signal,
  });
  if (!res.ok) return null;
  return (await res.json()) as ConversationSummary;
}

export async function loadConversationMessages(
  conversationId: string,
  signal?: AbortSignal,
): Promise<StoredMessage[]> {
  const res = await fetch(`/api/ceo-ai/conversations/${encodeURIComponent(conversationId)}`, {
    signal,
    cache: "no-store",
  });
  if (!res.ok) return [];
  const body = (await res.json()) as { messages?: StoredMessage[]; items?: StoredMessage[] };
  return body.messages ?? body.items ?? [];
}

export async function renameConversation(
  conversationId: string,
  title: string,
  signal?: AbortSignal,
): Promise<boolean> {
  const res = await fetch(`/api/ceo-ai/conversations/${encodeURIComponent(conversationId)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title: title.trim().slice(0, 120) }),
    signal,
  });
  return res.ok;
}

export async function deleteConversation(
  conversationId: string,
  signal?: AbortSignal,
): Promise<boolean> {
  const res = await fetch(`/api/ceo-ai/conversations/${encodeURIComponent(conversationId)}`, {
    method: "DELETE",
    signal,
  });
  return res.ok;
}

export async function sendFeedback(
  messageId: string,
  rating: "up" | "down",
  reason?: string,
): Promise<boolean> {
  const res = await fetch("/api/ceo-ai/feedback", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message_id: messageId, rating, reason }),
  });
  return res.ok;
}
