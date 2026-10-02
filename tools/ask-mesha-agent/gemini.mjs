// Gemini agent for Ask Mesha on the Gemini Developer API (generativelanguage.googleapis.com) with an
// AI Studio API key, so usage bills the prepaid AI Studio credits. Vertex AI is never used (postpay):
// there is no Vertex, ADC or service-account path, and a missing key fails at startup.
// Key: env GEMINI_API_KEY (Cloud Run mounts secret goatos-stg-ask-mesha-gemini-api-key; locally your shell).
//
// runAgent() is a provider-neutral agent loop: stream a model turn, run every function call it
// asks for (in parallel), feed results back, repeat until a turn has no calls or maxSteps is hit.
// It reports through onEvent with a small event vocabulary the server maps onto the existing
// SSE stream (progress / reset / token / replace / final), so admin-web is unchanged:
//   {type:"step"}                         a new model turn starts
//   {type:"text", text}                   visible answer text (thought parts are never emitted)
//   {type:"tool_call", id, name, args}    the model called a tool (before it runs)
//   {type:"tool_result", id, name, args, text, isError}
//   {type:"turn_end", final}              final=true: the turn had no tool calls
import { GoogleGenAI } from "@google/genai";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

// Newest Gemini models (verified 2026-10-01): Pro = gemini-3.1-pro-preview, Flash = gemini-3.8-flash.
export const DEFAULT_MODEL = "gemini-3.1-pro-preview";
export const DEFAULT_FAST_MODEL = "gemini-3.8-flash";
export const GEMINI_API_HOST = "generativelanguage.googleapis.com";

export function geminiConfig(env = process.env) {
  return {
    apiKey: String(env.GEMINI_API_KEY || "").trim(),
    model: env.ASK_MESHA_MODEL || DEFAULT_MODEL,
    deepModel: env.ASK_MESHA_DEEP_MODEL || env.ASK_MESHA_MODEL || DEFAULT_MODEL,
    fastModel: env.ASK_MESHA_FAST_MODEL || DEFAULT_FAST_MODEL,
    checkModel: env.ASK_MESHA_CHECK_MODEL || env.ASK_MESHA_FAST_MODEL || DEFAULT_FAST_MODEL,
    maxSteps: Number(env.ASK_MESHA_MAX_STEPS) || 40,
  };
}

// Gemini Developer API client. vertexai is pinned false, so the SDK can never switch to Vertex
// from GOOGLE_GENAI_USE_VERTEXAI / project env; no key = hard error (no fallback of any kind).
export function createClient({ apiKey }) {
  if (!apiKey) throw new Error("GEMINI_API_KEY is not set: Ask Mesha calls the Gemini Developer API with an AI Studio key (secret goatos-stg-ask-mesha-gemini-api-key); it never falls back to Vertex");
  return new GoogleGenAI({ vertexai: false, apiKey });
}

// USD per 1M tokens (Gemini API paid-tier list prices; override with ASK_MESHA_PRICE_<IN|OUT>_PER_M when they change).
// Thinking tokens bill as output; cached prompt tokens at 10% of input.
const PRICES = [
  { re: /pro/i, in: 2.0, out: 12.0, inLong: 4.0, outLong: 18.0 },
  { re: /flash-lite/i, in: 0.1, out: 0.4 },
  { re: /flash/i, in: 0.5, out: 3.0 },
];
export function priceFor(model, env = process.env) {
  const p = PRICES.find((x) => x.re.test(model)) || PRICES[0];
  const inP = Number(env.ASK_MESHA_PRICE_IN_PER_M) || p.in;
  const outP = Number(env.ASK_MESHA_PRICE_OUT_PER_M) || p.out;
  return { ...p, in: inP, out: outP, inLong: Number(env.ASK_MESHA_PRICE_IN_PER_M) || p.inLong || inP, outLong: Number(env.ASK_MESHA_PRICE_OUT_PER_M) || p.outLong || outP };
}
// One call's usageMetadata -> USD. Long-context (>200k prompt) rates apply per call.
export function callCostUsd(model, u = {}, env = process.env) {
  const p = priceFor(model, env);
  const prompt = u.promptTokenCount || 0;
  const cached = Math.min(prompt, u.cachedContentTokenCount || 0);
  const out = (u.candidatesTokenCount || 0) + (u.thoughtsTokenCount || 0);
  const long = prompt > 200_000;
  const inRate = long ? p.inLong : p.in;
  const outRate = long ? p.outLong : p.out;
  return ((prompt - cached) * inRate + cached * inRate * 0.1 + out * outRate) / 1e6;
}

// MCP tool (from listTools) -> Gemini function declaration. JSON Schema passes through
// (parametersJsonSchema); "$schema" is dropped.
export function mcpToolToDeclaration(t) {
  const { $schema, ...schema } = t.inputSchema || { type: "object", properties: {} };
  return { name: t.name, description: t.description || "", parametersJsonSchema: schema };
}

// Connect an MCP client to an in-process McpServer (the same "mesha" server the tools live in)
// and expose its tools as {declarations, call(name,args)}.
// Per-call timeout: watch_tags may legitimately run up to its 30-min cap (+1 min slack); every
// other tool is a bounded read (SQL is killed at 75 s), so 2 min is plenty.
export const MCP_TIMEOUT_MS = 120_000;
export const MCP_WATCH_TIMEOUT_MS = 31 * 60_000;
export async function connectMcp(server, { timeoutMs = MCP_TIMEOUT_MS, watchTimeoutMs = MCP_WATCH_TIMEOUT_MS, signal } = {}) {
  const [clientSide, serverSide] = InMemoryTransport.createLinkedPair();
  await server.connect(serverSide);
  const client = new Client({ name: "ask-mesha-gemini", version: "1.0.0" });
  await client.connect(clientSide);
  const { tools } = await client.listTools();
  return {
    names: tools.map((t) => t.name),
    declarations: tools.map(mcpToolToDeclaration),
    async call(name, args) {
      const r = await client.callTool({ name, arguments: args || {} }, undefined, {
        timeout: name === "watch_tags" ? watchTimeoutMs : timeoutMs, ...(signal ? { signal } : {}),
      });
      const text = (r.content || []).filter((c) => c.type === "text").map((c) => c.text).join("\n");
      return { text, isError: Boolean(r.isError) };
    },
    async close() { await client.close().catch(() => {}); await server.close().catch(() => {}); },
  };
}

// Chat history (stored messages) -> Gemini contents. Text only; the newest 20 turns.
export function historyContents(history = []) {
  const turns = history.filter((m) => (m.role === "user" || m.role === "assistant") && String(m.content || "").trim()).slice(-20);
  const out = [];
  for (const m of turns) {
    const role = m.role === "user" ? "user" : "model";
    const text = String(m.content).slice(0, 8000);
    // Gemini wants alternating roles: merge consecutive same-role turns.
    if (out.length && out[out.length - 1].role === role) out[out.length - 1].parts[0].text += "\n\n" + text;
    else out.push({ role, parts: [{ text }] });
  }
  if (out.length && out[0].role !== "user") out.unshift({ role: "user", parts: [{ text: "(earlier conversation)" }] });
  if (out.length && out[out.length - 1].role === "user") out.push({ role: "model", parts: [{ text: "(no answer was saved for that question)" }] });
  return out;
}

// Retryable before anything streamed: rate limits / overload / transient 5xx.
export const isAuthExpired = (err) => Number(err?.status ?? err?.code) === 401 || /\b401\b|UNAUTHENTICATED|invalid authentication credentials/i.test(String(err?.message || ""));
// Jittered exponential backoff, ~63 s in total over 6 waits (1,2,4,8,16,32 s x 0.75-1.25), or the
// server's Retry-After when it sends one (capped at 60 s).
export const RETRY_WAITS = 6;
export function retryDelay(attempt, baseMs, err, rand = Math.random) {
  const ra = Number(err?.headers?.["retry-after"] ?? err?.retryAfter);
  if (Number.isFinite(ra) && ra > 0) return Math.min(60_000, ra * 1000);
  return Math.round(baseMs * 2 ** attempt * (0.75 + rand() * 0.5));
}
export function isRetryable(err) {
  const s = Number(err?.status ?? err?.code);
  if ([429, 500, 502, 503, 504].includes(s)) return true;
  return /\b(429|503|RESOURCE_EXHAUSTED|UNAVAILABLE|overloaded)\b/i.test(String(err?.message || ""));
}

const sleep = (ms, signal) => new Promise((r) => { const t = setTimeout(r, ms); signal?.addEventListener("abort", () => { clearTimeout(t); r(); }, { once: true }); });

export const MAX_STEPS_NOTE = "[status: lookup step limit reached; tools are now unavailable. Next output = the final answer to the CEO's question from the results above, noting anything left unchecked.]";
export const TIME_UP_NOTE = "[status: lookup time for this question has ended; tools are now unavailable. Next output = the final answer to the CEO's question, built only from the results above; figures the results do not confirm are reported as unconfirmed.]";

// The forced final turn sometimes writes the same answer twice back to back; keep one copy.
export function dedupeRepeatedAnswer(text) {
  const t = String(text || "");
  const s = t.trim();
  for (let i = Math.floor(s.length / 2) - 20; i <= Math.ceil(s.length / 2) + 20; i++) {
    if (i <= 40 || i >= s.length) continue;
    const a = s.slice(0, i).trim(), b = s.slice(i).trim();
    if (a && a === b) return a;
  }
  return t;
}
export const MAX_PARALLEL_TOOLS = 4;
// Thinking written as plain text (seen live: "thought\nWait, it timed out ... 4. Conclude.The data shows ...").
export const LEAKED_THOUGHT = /^\s*thought\s*\n/i;
// The answer starts where the reasoning runs into it without a break ("Conclude.The data shows"), else after the
// last blank-line-separated planning block. Returns "" when no answer can be found.
export function splitLeakedThought(text) {
  const t = String(text || "").replace(LEAKED_THOUGHT, "");
  // First sentence end glued to a capital: inside the answer, sentences are separated by a space or newline.
  const m = t.match(/[.!?](?=[A-Z])/);
  const cut = m ? m.index + 1 : -1;
  if (cut > 0) return t.slice(cut).trim();
  const blocks = t.split(/\n\s*\n/);
  return blocks.length > 1 ? blocks.slice(-Math.ceil(blocks.length / 3)).join("\n\n").trim() : "";
}
export const TRANSIENT_TOOL_ERROR = /fetch failed|ECONNRESET|ECONNREFUSED|server closed the connection|connection to server at .* failed|timeout expired|terminating connection/i;
// Images/PDFs sent to the model in one answer (attachments + read_file), base64 chars.
export const INLINE_BUDGET_CHARS = 20 * 1024 * 1024;
function appendUserText(convo, text) {
  const last = convo[convo.length - 1];
  if (last?.role === "user") last.parts.push({ text });
  else convo.push({ role: "user", parts: [{ text }] });
}
// Map with at most `limit` in flight, results in input order.
export async function mapLimit(items, limit, fn) {
  const out = new Array(items.length);
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, async () => {
    while (next < items.length) { const i = next++; out[i] = await fn(items[i], i); }
  }));
  return out;
}

export const EMPTY_TURN_NUDGE = "[status: the previous turn was empty. Continue the lookup or give the final answer to the CEO's question.]";

// The loop. tools: { declarations: [...], call(name, args) -> {text, isError, inline?} }.
// Returns { text (last turn), steps, usage, costUsd, error } where error is null | "error_max_turns" |
// "error_max_budget_usd" | a message. Throws only on abort or a non-retryable API failure.
export async function runAgent({
  ai, model, systemInstruction, contents, tools, onEvent = () => {}, signal,
  maxSteps = 40, budgetUsd = Infinity, thinkingLevel, env = process.env, retryDelayMs = 1000,
  fallbackModel = null, inlineBudget = INLINE_BUDGET_CHARS, deadlineMs = Infinity, now = Date.now, toolRetryDelayMs = 2000,
}) {
  const usage = { input: 0, output: 0, cached: 0, thoughts: 0 };
  let costUsd = 0;
  let lastText = "";
  let error = null;
  const convo = [...contents];
  let callSeq = 0;
  let nudged = false;
  const t0 = now();
  let inlineUsed = contents.reduce((n, c) => n + (c.parts || []).reduce((m, p) => m + String(p.inlineData?.data || "").length, 0), 0);
  for (let step = 0; ; step++) {
    if (signal?.aborted) throw new Error("client_aborted");
    // Out of steps, or out of time for this kind of answer: one last turn without tools to write the answer.
    const outOfTime = step > 0 && now() - t0 > deadlineMs;
    const finalOnly = step >= maxSteps || outOfTime;
    if (finalOnly) {
      if (!outOfTime) error = "error_max_turns";
      // Folded into the tool-results turn: two user turns in a row is not a valid Gemini history.
      appendUserText(convo, outOfTime ? TIME_UP_NOTE : MAX_STEPS_NOTE);
    }
    onEvent({ type: "step" });
    const config = {
      systemInstruction,
      abortSignal: signal,
      ...(finalOnly ? {} : { tools: [{ functionDeclarations: tools.declarations }] }),
      ...(thinkingLevel ? { thinkingConfig: { thinkingLevel } } : {}),
    };
    let modelParts = [];
    let calls = [];
    let turnText = "";
    let lastUsage = null;
    let authRetried = false;
    let leak = null;
    for (let attempt = 0; ; attempt++) {
      if (turnText) onEvent({ type: "turn_retry" }); // text of the failed try is withdrawn on screen
      modelParts = []; calls = []; turnText = ""; lastUsage = null;
      leak = null;
      try {
        const stream = await ai.models.generateContentStream({ model, contents: convo, config });
        for await (const chunk of stream) {
          if (signal?.aborted) throw new Error("client_aborted");
          if (chunk.usageMetadata) lastUsage = chunk.usageMetadata;
          const parts = chunk.candidates?.[0]?.content?.parts || [];
          for (const part of parts) {
            modelParts.push(part); // kept verbatim: Gemini 3 needs thoughtSignature parts echoed back
            if (part.functionCall) calls.push(part.functionCall);
            else if (typeof part.text === "string" && !part.thought && part.text) {
              turnText += part.text;
              // Gemini sometimes writes its thinking as plain text starting "thought\n". Hold the first
              // characters until that can be ruled out; a leaked turn is never streamed.
              if (leak === null && (turnText.trimStart().length >= 8 || !/^\s*t(h(o(u(g(h(t)?)?)?)?)?)?$/i.test(turnText))) {
                leak = LEAKED_THOUGHT.test(turnText);
                if (!leak) onEvent({ type: "text", text: turnText });
              } else if (leak === false) onEvent({ type: "text", text: part.text });
            }
          }
        }
        break;
      } catch (e) {
        if (signal?.aborted || e?.name === "AbortError") throw new Error("client_aborted");
        // Earlier turns (and their tool results) are kept in convo; only this turn is retried.
        if (isAuthExpired(e) && ai.invalidate && !authRetried) { authRetried = true; ai.invalidate(); attempt--; continue; }
        if (isRetryable(e)) {
          if (attempt < RETRY_WAITS) { await sleep(retryDelay(attempt, retryDelayMs, e), signal); continue; }
          // Still rate-limited after backoff (shared preview quota): finish the answer on the fallback model.
          if (fallbackModel && model !== fallbackModel) { onEvent({ type: "model_fallback", from: model, to: fallbackModel }); model = fallbackModel; attempt = -1; continue; }
        }
        throw e;
      }
    }
    if (leak === null && turnText && !LEAKED_THOUGHT.test(turnText)) onEvent({ type: "text", text: turnText }); // short turn
    else if (leak === null && turnText) leak = true;
    if (leak) {
      const answer = splitLeakedThought(turnText);
      onEvent({ type: "leak_stripped", chars: turnText.length - answer.length });
      turnText = answer;
      if (answer) onEvent({ type: "text", text: answer });
    }
    if (lastUsage) {
      usage.input += lastUsage.promptTokenCount || 0;
      usage.output += lastUsage.candidatesTokenCount || 0;
      usage.cached += lastUsage.cachedContentTokenCount || 0;
      usage.thoughts += lastUsage.thoughtsTokenCount || 0;
      costUsd += callCostUsd(model, lastUsage, env);
      onEvent({ type: "usage", costUsd, usage: { ...usage } });
    }
    if (turnText) lastText = turnText;
    convo.push({ role: "model", parts: modelParts.length ? modelParts : [{ text: "" }] });
    // A turn with no calls and no text (seen live after a get_skill call): ask once for the answer.
    if (!calls.length && !turnText && !finalOnly && !nudged) {
      nudged = true;
      appendUserText(convo, EMPTY_TURN_NUDGE);
      continue;
    }
    if (!calls.length || finalOnly) {
      onEvent({ type: "turn_end", final: true });
      return { text: finalOnly ? dedupeRepeatedAnswer(lastText) : lastText, steps: step + 1, usage, costUsd, error, model, forced: finalOnly };
    }
    onEvent({ type: "turn_end", final: false });
    if (costUsd >= budgetUsd) {
      // Same contract as the SDK's maxBudgetUsd: stop spending, keep what was written.
      return { text: lastText, steps: step + 1, usage, costUsd, error: "error_max_budget_usd", model };
    }
    const withIds = calls.map((c) => ({ ...c, _id: c.id || `call_${++callSeq}` }));
    // Out of time before these tools run (a long model turn): do not start them; the next turn answers.
    if (now() - t0 > deadlineMs) {
      convo.push({ role: "user", parts: withIds.map((c) => ({ functionResponse: { ...(c.id ? { id: c.id } : {}), name: c.name, response: { error: "[status: not run; lookup time has ended]" } } })) });
      continue;
    }
    for (const c of withIds) onEvent({ type: "tool_call", id: c._id, name: c.name, args: c.args || {} });
    const results = await mapLimit(withIds, MAX_PARALLEL_TOOLS, async (c) => {
      let r;
      for (let t = 0; t < 2; t++) {
        try { r = await tools.call(c.name, c.args || {}); } catch (e) { r = { text: `Tool failed: ${String(e?.message || e).slice(0, 500)}`, isError: true }; }
        // A dropped DB connection / proxy blip is retried once instead of costing the model a turn.
        if (!(r?.isError && TRANSIENT_TOOL_ERROR.test(String(r.text || ""))) || signal?.aborted) break;
        await sleep(toolRetryDelayMs, signal);
      }
      onEvent({ type: "tool_result", id: c._id, name: c.name, args: c.args || {}, text: r.text, isError: Boolean(r.isError) });
      // Image budget per answer: past it, the tool still answers in text but the image is not sent.
      if (r.inline) {
        const size = String(r.inline.data || "").length;
        if (inlineUsed + size > inlineBudget) r = { ...r, inline: undefined, text: `${r.text || ""}\n(Not shown: this answer already looked at as many images as it can.)` };
        else inlineUsed += size;
      }
      return { c, r };
    });
    if (signal?.aborted) throw new Error("client_aborted");
    convo.push({ role: "user", parts: results.map(({ c, r }) => ({
      functionResponse: {
        ...(c.id ? { id: c.id } : {}),
        name: c.name,
        // Errors go back to the model as data so it can fix the call (column hints, bad paths).
        response: r.isError ? { error: r.text || "error" } : { output: r.text || "(no output)" },
        ...(r.inline ? { parts: [{ inlineData: r.inline }] } : {}),
      },
    })) });
  }
}

// One tool-less call (the answer checker). Returns { text, costUsd }.
export async function generateOnce({ ai, model, systemInstruction, prompt, signal, env = process.env }) {
  const r = await ai.models.generateContent({ model, contents: [{ role: "user", parts: [{ text: prompt }] }], config: { systemInstruction, abortSignal: signal, responseMimeType: "application/json" } });
  return { text: r.text || "", costUsd: callCostUsd(model, r.usageMetadata || {}, env) };
}
