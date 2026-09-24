// Client-side SSE reader for the leadership assistant answer stream.
//
// The admin-web proxy (/api/ceo-ai/ask) pipes the backend's text/event-stream
// straight through. Each SSE `data:` line is one JSON event:
//
//   { "type": "token",  "text": "Cas" }                       progressive token
//   { "type": "final",  "answer", "source", "mode",           terminal metadata
//                       "request_id", "conversation_id",
//                       "message_id", "citations": [...] }
//   { "type": "error",  "message": "..." }                    honest failure
//   { "type": "action_proposal", "proposal_id", "title",        CEO write action
//     "summary": [...], "risk", "requires_double_confirm",      awaiting Confirm /
//     "expires_at" }                                            Cancel in the panel
//   { "type": "watch",  "phase": "start"|"tick"|"end"|"error", live BLE tag watch
//                       "watch_id", "rows", "changes", ... }   (watch_tags tool)
//
// The reader never renders step traces / chain-of-thought — the backend only
// emits answer tokens + terminal metadata, and this reader forwards exactly
// those. If the backend answers with JSON instead of a stream (non-streaming
// fallback or an error envelope), `readCeoAiStream` detects it and emits a
// single final/error event so the caller has one code path.

export type CeoAiCitation = {
  surface: string;
  as_of?: string;
  tier?: "cube" | "api" | "toolbox" | "sql" | string;
  planned_by_model?: boolean;
};

// Optional, additive inline chart. Present only when the backend judged the
// answer plot-worthy and grounded it in a dimensioned series; points come
// verbatim from real Cube/tool rows. Absent for plain scalar answers.
export type CeoAiChartSeries = { name: string; data: number[] };
export type CeoAiChart = {
  type: "bar" | "line";
  title: string;
  x: string[];
  series: CeoAiChartSeries[];
};

export type CeoAiFinal = {
  answer: string;
  source?: string;
  mode?: string;
  request_id?: string;
  conversation_id?: string;
  message_id?: string;
  citations?: CeoAiCitation[];
  chart?: CeoAiChart;
};

// parseChart narrows an untrusted JSON value into a CeoAiChart, or undefined.
// Used on the non-streaming JSON fallback path (the SSE path spreads the parsed
// object, which already carries chart when present).
export function parseChart(raw: unknown): CeoAiChart | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const c = raw as Record<string, unknown>;
  if (c.type !== "bar" && c.type !== "line") return undefined;
  if (!Array.isArray(c.x) || !Array.isArray(c.series)) return undefined;
  const x = c.x.filter((v): v is string => typeof v === "string");
  const series = c.series
    .filter((s): s is Record<string, unknown> => !!s && typeof s === "object")
    .map((s) => ({
      name: typeof s.name === "string" ? s.name : "",
      data: Array.isArray(s.data) ? s.data.filter((n): n is number => typeof n === "number") : [],
    }));
  if (x.length < 2 || series.length === 0) return undefined;
  return {
    type: c.type,
    title: typeof c.title === "string" ? c.title : "",
    x,
    series,
  };
}

// Coarse pre-answer progress frame (planning / querying / synthesizing). It
// carries only a stable phase enum + a coarse route label — never step traces or
// chain-of-thought — so the UI can show progressive status while the grounded
// pipeline runs, instead of a frozen blank placeholder.
// request_id (coding-agent backend, first frame) identifies the run so a user
// Stop can be reported via sendCeoAiStopSignal.
// conversationId: the chat this run is saved in, sent up front so a stream cut short
// (server restart, network drop) still leaves the panel on that chat.
export type CeoAiProgress = { phase: string; label?: string; requestId?: string; conversationId?: string };

// Live BLE ear-tag watch (Ask Mesha watch_tags tool). The agent server polls the
// Herd Signals live table and streams one frame per poll: the full table and only
// the NEW change lines (the panel accumulates them). Labels are the Live Monitor's.
export type CeoAiWatchRow = {
  tag: string;
  animal: string | null;
  pen: string | null;
  park: string | null;
  state: string;
  state_label: string;
  live_state?: string | null;
  motion_count: number | null;
  motion_delta_15m: number | null;
  delta_since_start: number | null;
  still_min: number;
  last_seen_s: number | null;
  rssi: number | null;
  battery_mv: number | null;
  status: string;
  vs_own_pct?: number;
  vs_pen_pct?: number;
  flags: string[];
};
export type CeoAiWatchChange = { tag?: string; tone?: string; text: string; at_min?: number };
export type CeoAiWatchFrame = {
  watchId: string;
  phase: "start" | "tick" | "end" | "error";
  label?: string;
  startedAt?: string;
  endsAt?: string;
  intervalS?: number;
  compare?: string;
  stopWhen?: string | null;
  polls?: number;
  reason?: string;
  message?: string;
  unmatched?: string[];
  rows?: CeoAiWatchRow[];
  changes?: CeoAiWatchChange[];
};

function parseWatchRow(raw: unknown): CeoAiWatchRow | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  if (typeof r.tag !== "string") return null;
  const s = (v: unknown) => (typeof v === "string" ? v : null);
  const n = (v: unknown) => (typeof v === "number" && Number.isFinite(v) ? v : null);
  const state = s(r.state) ?? "not_moving";
  return {
    tag: r.tag,
    animal: s(r.animal),
    pen: s(r.pen),
    park: s(r.park),
    state,
    state_label: s(r.state_label) ?? state,
    live_state: s(r.live_state),
    motion_count: n(r.motion_count),
    motion_delta_15m: n(r.motion_delta_15m),
    delta_since_start: n(r.delta_since_start),
    still_min: n(r.still_min) ?? 0,
    last_seen_s: n(r.last_seen_s),
    rssi: n(r.rssi),
    battery_mv: n(r.battery_mv),
    status: s(r.status) ?? "",
    vs_own_pct: n(r.vs_own_pct) ?? undefined,
    vs_pen_pct: n(r.vs_pen_pct) ?? undefined,
    flags: Array.isArray(r.flags) ? r.flags.filter((f): f is string => typeof f === "string") : [],
  };
}

function parseWatch(obj: Record<string, unknown>): CeoAiWatchFrame | null {
  const phase = obj.phase;
  if (typeof obj.watch_id !== "string") return null;
  if (phase !== "start" && phase !== "tick" && phase !== "end" && phase !== "error") return null;
  const str = (v: unknown) => (typeof v === "string" ? v : undefined);
  return {
    watchId: obj.watch_id,
    phase,
    label: str(obj.label),
    startedAt: str(obj.started_at),
    endsAt: str(obj.ends_at),
    intervalS: typeof obj.interval_s === "number" ? obj.interval_s : undefined,
    compare: str(obj.compare),
    stopWhen: typeof obj.stop_when === "string" ? obj.stop_when : null,
    polls: typeof obj.polls === "number" ? obj.polls : undefined,
    reason: str(obj.reason),
    message: str(obj.message),
    unmatched: Array.isArray(obj.unmatched) ? obj.unmatched.filter((u): u is string => typeof u === "string") : undefined,
    // Rows/changes are rendered as React text: coerce every field so a malformed frame
    // (object where a string is expected) can't crash the panel. Capped like the server.
    rows: Array.isArray(obj.rows) ? obj.rows.slice(0, 60).map(parseWatchRow).filter((r): r is CeoAiWatchRow => r !== null) : undefined,
    changes: Array.isArray(obj.changes)
      ? obj.changes
          .slice(0, 200)
          .filter((c): c is Record<string, unknown> => !!c && typeof c === "object" && typeof (c as { text?: unknown }).text === "string")
          .map((c) => ({
            text: String(c.text).slice(0, 300),
            tag: typeof c.tag === "string" ? c.tag : undefined,
            tone: typeof c.tone === "string" ? c.tone : undefined,
            at_min: typeof c.at_min === "number" ? c.at_min : undefined,
          }))
      : undefined,
  };
}

// Write-action proposal (Ask Mesha actions). The agent never writes on its own: it
// proposes, the CEO confirms or cancels in the panel. Everything here is rendered
// as plain React text, so parsing coerces and caps every field.
export type CeoAiActionProposal = {
  proposalId: string;
  title: string;
  summary: string[];
  risk: "normal" | "high";
  requiresDoubleConfirm: boolean;
  expiresAt?: string;
};

const MAX_ACTION_SUMMARY_LINES = 12;

export function parseActionProposal(raw: unknown): CeoAiActionProposal | null {
  if (!raw || typeof raw !== "object") return null;
  const o = raw as Record<string, unknown>;
  const id = typeof o.proposal_id === "string" ? o.proposal_id.trim() : "";
  // Used in a URL path: keep it to a safe id alphabet.
  if (!id || id.length > 100 || !/^[A-Za-z0-9_.:-]+$/.test(id)) return null;
  const title = typeof o.title === "string" ? o.title.trim().slice(0, 200) : "";
  if (!title) return null;
  const summary = Array.isArray(o.summary)
    ? o.summary
        .filter((l): l is string => typeof l === "string" && l.trim() !== "")
        .slice(0, MAX_ACTION_SUMMARY_LINES)
        .map((l) => l.trim().slice(0, 300))
    : [];
  const risk = o.risk === "high" ? "high" : "normal";
  const expires = typeof o.expires_at === "string" && !Number.isNaN(Date.parse(o.expires_at)) ? o.expires_at : undefined;
  return {
    proposalId: id,
    title,
    summary,
    risk,
    // High risk always needs the typed CONFIRM, even if the flag is missing.
    requiresDoubleConfirm: o.requires_double_confirm === true || risk === "high",
    expiresAt: expires,
  };
}

export type CeoAiStreamEvent =
  | { type: "token"; text: string }
  | ({ type: "final" } & CeoAiFinal)
  | { type: "progress"; phase: string; label?: string; requestId?: string; conversationId?: string }
  | { type: "reset" }
  | { type: "watch"; frame: CeoAiWatchFrame }
  | { type: "action_proposal"; proposal: CeoAiActionProposal }
  | { type: "error"; message: string; status?: number };

export type CeoAiStreamHandlers = {
  onToken?: (text: string) => void;
  onFinal?: (final: CeoAiFinal) => void;
  onProgress?: (progress: CeoAiProgress) => void;
  // Coding-agent backend: discard text streamed so far (it was narration before a tool call).
  onReset?: () => void;
  // Live tag watch frames (watch_tags). Absent handler = frames are ignored.
  onWatch?: (frame: CeoAiWatchFrame) => void;
  // Write-action proposal awaiting the CEO's Confirm / Cancel.
  onActionProposal?: (proposal: CeoAiActionProposal) => void;
  onError?: (message: string, status?: number) => void;
};

type AskPageScope = { park_id?: string; shed_id?: string };

export type CeoAiAttachment = { name: string; type: string; data: string /* base64 */ };

type AskArgs = {
  question: string;
  attachments?: CeoAiAttachment[];
  conversationId?: string;
  locale?: string;
  pageScope?: AskPageScope;
  signal?: AbortSignal;
  // No-progress window (ms) after which a slow/unavailable Vertex is degraded
  // instead of hanging forever. The timer is armed before connect and re-armed
  // on every chunk, so a healthy progressive stream never trips it; only a real
  // stall (no headers, no token, no final for this long) does. Default 40s.
  idleTimeoutMs?: number;
};

// Default no-progress window. Full answers take ~20s and tokens then arrive
// continuously, so 40s of total silence is an unambiguous stall, not slowness.
const DEFAULT_IDLE_TIMEOUT_MS = 40_000;
const TIMEOUT_ERROR = "assistant_timeout";

function parseEvent(raw: string): CeoAiStreamEvent | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const dataLines = trimmed
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trim());
  if (dataLines.length === 0) return null;
  const payload = dataLines.join("\n");
  if (payload === "[DONE]") return null;
  try {
    const obj = JSON.parse(payload) as Record<string, unknown>;
    const type = obj.type;
    if (type === "token" && typeof obj.text === "string") {
      return { type: "token", text: obj.text };
    }
    if (type === "reset") {
      return { type: "reset" };
    }
    if (type === "progress" && typeof obj.phase === "string") {
      return {
        type: "progress",
        phase: obj.phase,
        label: typeof obj.label === "string" ? obj.label : undefined,
        requestId: typeof obj.request_id === "string" ? obj.request_id : undefined,
        conversationId: typeof obj.conversation_id === "string" ? obj.conversation_id : undefined,
      };
    }
    if (type === "watch") {
      const frame = parseWatch(obj);
      return frame ? { type: "watch", frame } : null;
    }
    if (type === "action_proposal") {
      const proposal = parseActionProposal(obj);
      return proposal ? { type: "action_proposal", proposal } : null;
    }
    if (type === "error") {
      return {
        type: "error",
        message: typeof obj.message === "string" ? obj.message : "assistant_error",
        // In-stream status (e.g. 410: the chat was deleted mid-answer).
        status: typeof obj.status === "number" ? obj.status : undefined,
      };
    }
    if (typeof obj.answer === "string" || type === "final") {
      return { type: "final", ...(obj as unknown as CeoAiFinal) };
    }
    return null;
  } catch {
    return null;
  }
}

// sendCeoAiStopSignal tells the assistant the user pressed Stop (vs. closing the
// tab), so the run is logged as ask_stopped reason stop_pressed. Fire-and-forget:
// sent before the abort, keepalive so it survives the stream teardown.
export function sendCeoAiStopSignal(requestId: string | undefined): void {
  if (!requestId) return;
  void fetch("/api/ceo-ai/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ request_id: requestId, kind: "stop_pressed" }),
    keepalive: true,
  }).catch(() => {});
}

// sendCeoAiWatchStop ends only the running live tag watch ("Stop watching"): the
// stream stays open and the assistant still writes its short summary answer.
export function sendCeoAiWatchStop(requestId: string | undefined): void {
  if (!requestId) return;
  void fetch("/api/ceo-ai/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ request_id: requestId, kind: "watch_stop" }),
    keepalive: true,
  }).catch(() => {});
}

// readCeoAiStream POSTs the question through the admin-web proxy and drives the
// handlers as the answer streams in. Resolves to the terminal metadata. It is
// abortable via `signal` (stop-generating).
export async function readCeoAiStream(
  args: AskArgs,
  handlers: CeoAiStreamHandlers,
): Promise<CeoAiFinal | null> {
  // Compose the caller's stop signal with an internal no-progress timeout. Any
  // trip aborts the same fetch; `timedOut` tells the two apart so a stall
  // degrades honestly while a user Stop stays a user abort.
  const idleMs = args.idleTimeoutMs ?? DEFAULT_IDLE_TIMEOUT_MS;
  const internal = new AbortController();
  let timedOut = false;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;

  const armIdle = () => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = setTimeout(() => {
      timedOut = true;
      internal.abort();
    }, idleMs);
  };
  const clearIdle = () => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = undefined;
  };

  const onCallerAbort = () => internal.abort();
  if (args.signal) {
    if (args.signal.aborted) internal.abort();
    else args.signal.addEventListener("abort", onCallerAbort, { once: true });
  }

  try {
    armIdle();
    return await readComposed(args, handlers, internal.signal, armIdle, clearIdle);
  } catch (error: unknown) {
    // A no-progress timeout is an honest degraded state, not a thrown failure —
    // surface it through onError so the caller renders it like any backend
    // error. A genuine user Stop (caller signal) is rethrown for the caller.
    if (timedOut && !(args.signal?.aborted ?? false)) {
      handlers.onError?.(TIMEOUT_ERROR, 504);
      return null;
    }
    throw error;
  } finally {
    clearIdle();
    args.signal?.removeEventListener("abort", onCallerAbort);
  }
}

async function readComposed(
  args: AskArgs,
  handlers: CeoAiStreamHandlers,
  signal: AbortSignal,
  armIdle: () => void,
  clearIdle: () => void,
): Promise<CeoAiFinal | null> {
  const response = await fetch("/api/ceo-ai/ask", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      question: args.question,
      conversation_id: args.conversationId,
      locale: args.locale,
      page_scope: args.pageScope,
      attachments: args.attachments?.length ? args.attachments : undefined,
      stream: true,
    }),
    signal,
  });
  // Headers arrived: connect succeeded, re-arm for the first token/body.
  armIdle();

  const contentType = response.headers.get("Content-Type") ?? "";

  // JSON path: non-streaming fallback or an error envelope.
  if (!contentType.includes("text/event-stream")) {
    let body: Record<string, unknown> = {};
    try {
      body = (await response.json()) as Record<string, unknown>;
    } catch {
      body = {};
    }
    if (!response.ok) {
      const message =
        typeof body.message === "string"
          ? body.message
          : typeof body.error === "string"
            ? body.error
            : `assistant_http_${response.status}`;
      handlers.onError?.(message, response.status);
      return null;
    }
    const final: CeoAiFinal = {
      answer: typeof body.answer === "string" ? body.answer : "",
      source: typeof body.source === "string" ? body.source : undefined,
      mode: typeof body.mode === "string" ? body.mode : undefined,
      request_id: typeof body.request_id === "string" ? body.request_id : undefined,
      conversation_id: typeof body.conversation_id === "string" ? body.conversation_id : undefined,
      message_id: typeof body.message_id === "string" ? body.message_id : undefined,
      citations: Array.isArray(body.citations) ? (body.citations as CeoAiCitation[]) : undefined,
      chart: parseChart(body.chart),
    };
    if (final.answer) handlers.onToken?.(final.answer);
    handlers.onFinal?.(final);
    return final;
  }

  if (!response.body) {
    handlers.onError?.("assistant_empty_stream", response.status);
    return null;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let final: CeoAiFinal | null = null;

  const flush = (chunk: string) => {
    buffer += chunk;
    let idx = buffer.indexOf("\n\n");
    while (idx !== -1) {
      const rawEvent = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const event = parseEvent(rawEvent);
      if (event) {
        if (event.type === "token") {
          handlers.onToken?.(event.text);
        } else if (event.type === "reset") {
          handlers.onReset?.();
        } else if (event.type === "watch") {
          handlers.onWatch?.(event.frame);
        } else if (event.type === "action_proposal") {
          handlers.onActionProposal?.(event.proposal);
        } else if (event.type === "progress") {
          handlers.onProgress?.({ phase: event.phase, label: event.label, requestId: event.requestId, conversationId: event.conversationId });
        } else if (event.type === "final") {
          const { type: _t, ...rest } = event;
          void _t;
          final = rest;
          handlers.onFinal?.(rest);
        } else if (event.type === "error") {
          handlers.onError?.(event.message, event.status);
        }
      }
      idx = buffer.indexOf("\n\n");
    }
  };

  for (;;) {
    // serial-await: allow sequential SSE chunk reads must be consumed in order as they stream in; there is no batch to parallelize
    const { done, value } = await reader.read();
    if (done) break;
    // Progress: re-arm the no-progress timeout on every chunk (tokens AND
    // heartbeat comments), so only real silence trips the timeout.
    armIdle();
    flush(decoder.decode(value, { stream: true }));
  }
  if (buffer.trim()) flush("\n\n");
  clearIdle();

  return final;
}
