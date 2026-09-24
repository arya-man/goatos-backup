import type { CeoAiActionProposal, CeoAiChart, CeoAiCitation, CeoAiFinal, CeoAiWatchChange, CeoAiWatchRow } from "@/lib/ceo-ai-stream";

export type { CeoAiChart, CeoAiCitation, CeoAiFinal };

// Accumulated live tag watch (watch_tags frames merged in order).
export type WatchState = {
  watchId: string;
  label?: string;
  startedAt?: string;
  endsAt?: string;
  intervalS?: number;
  compare?: string;
  polls: number;
  rows: CeoAiWatchRow[];
  changes: CeoAiWatchChange[];
  unmatched?: string[];
  ended: boolean;
  reason?: string;
  error?: string;
};

// Ask Mesha write-action card. `pending` = awaiting Confirm/Cancel; `executing`
// while the confirm/cancel request is in flight; the rest are terminal.
export type ActionStatus = "pending" | "executing" | "succeeded" | "failed" | "cancelled" | "expired";
export type ActionLink = { text: string; href?: string };
export type ActionState = CeoAiActionProposal & {
  status: ActionStatus;
  // Plain-words outcome or backend error for the card footer.
  message?: string;
  link?: ActionLink;
};

// One rendered chat turn. Assistant turns accumulate streamed tokens into
// `text` and gain terminal metadata (source/mode/citations/message_id) on
// completion. `state` drives the per-message UI (streaming skeleton, error).
export type ChatMessageState = "streaming" | "complete" | "error";

export type ChatMessage = {
  // Files attached to a user turn (object URLs; previews only for this session).
  files?: { name: string; type: string; url: string }[];
  id: string;
  role: "user" | "assistant";
  text: string;
  state: ChatMessageState;
  source?: string;
  mode?: string;
  requestId?: string;
  messageId?: string;
  citations?: CeoAiCitation[];
  chart?: CeoAiChart;
  // Coarse live progress status shown under the streaming placeholder before the
  // first answer token (planning / querying / synthesizing). Cleared once answer
  // text arrives. Never carries chain-of-thought — only a coarse route label.
  progress?: string;
  // Coding-agent activity: step labels as they happen, and how long the run took.
  steps?: string[];
  startedAt?: number;
  workedMs?: number;
  // Live BLE tag watch card (watch_tags tool), updated every poll.
  watch?: WatchState;
  // Write-action proposal card (Confirm / Cancel).
  action?: ActionState;
};

// A conversation thread summary in the sidebar (backend-owned list).
export type ConversationSummary = {
  id: string;
  title: string;
  updated_at?: string;
};

// Backend messages payload row (for resuming a thread).
export type StoredMessage = {
  id?: string;
  // Coding-agent backend only: attachment refs on user turns, chart on answers.
  files?: { id: string; name: string; type?: string }[];
  chart?: CeoAiChart;
  message_id?: string;
  role?: string;
  content?: string;
  source?: string;
  mode?: string;
  request_id?: string;
  citations?: CeoAiCitation[];
  // Coding-agent backend: the write-action proposal this answer carried, with its
  // current status, so the card survives a reload. Same wire shape as the SSE event
  // plus optional status / message / result.
  action_proposal?: unknown;
  action?: unknown;
};

// All user-visible copy for the assistant surface. The subset shared with the
// backend admin-web contract (title/subtitle/starters/etc.) is passed in from
// `mesha-shell`; the thread and state strings are local literals for this
// leadership-only surface, documented as an exception in
// context/frontend/admin-web-backend-ui-contract.md (Verification-style: no
// backend page contract for the assistant chrome exists yet).
export type AssistantCopy = {
  title: string;
  subtitle: string;
  hello: string;
  helloMeta: string;
  checking: string;
  noAnswer: string;
  unavailable: string;
  unavailableMeta: string;
  sourceFallback: string;
  modeFallback: string;
  placeholder: string;
  open: string;
  close: string;
  send: string;
  starters: string[];
};
