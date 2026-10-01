// Gemini (Vertex AI) agent for Ask Mesha. Auth is ADC only: the Cloud Run runtime service
// account (roles/aiplatform.user) or `gcloud auth application-default login` locally. No API keys.
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
import { execFileSync } from "node:child_process";
import { GoogleGenAI } from "@google/genai";
import { OAuth2Client } from "google-auth-library";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

// Newest Gemini on goatos-stg Vertex (location global), verified by a live generateContent 200
// on 2026-10-01: Pro = gemini-3.1-pro-preview (newest Pro served), Flash = gemini-3.8-flash.
export const DEFAULT_MODEL = "gemini-3.1-pro-preview";
export const DEFAULT_FAST_MODEL = "gemini-3.8-flash";

export function geminiConfig(env = process.env) {
  return {
    project: env.ASK_MESHA_GEMINI_PROJECT || env.GOOGLE_CLOUD_PROJECT || "goatos-stg",
    location: env.ASK_MESHA_GEMINI_LOCATION || "global",
    model: env.ASK_MESHA_MODEL || DEFAULT_MODEL,
    deepModel: env.ASK_MESHA_DEEP_MODEL || env.ASK_MESHA_MODEL || DEFAULT_MODEL,
    fastModel: env.ASK_MESHA_FAST_MODEL || DEFAULT_FAST_MODEL,
    checkModel: env.ASK_MESHA_CHECK_MODEL || env.ASK_MESHA_FAST_MODEL || DEFAULT_FAST_MODEL,
    maxSteps: Number(env.ASK_MESHA_MAX_STEPS) || 40,
  };
}

// ADC by default (Cloud Run runtime SA). Local dev whose ADC needs a browser re-auth can set
// ASK_MESHA_GEMINI_AUTH=gcloud: requests then carry `gcloud auth print-access-token` (the
// signed-in user), refreshed every 5 min (gcloud hands back its cached token, which may be near expiry). Never used on Cloud Run (K_SERVICE set).
export function createClient({ project, location }, env = process.env) {
  if (env.ASK_MESHA_GEMINI_AUTH === "gcloud" && !env.K_SERVICE) return gcloudUserClient({ project, location });
  return new GoogleGenAI({ vertexai: true, project, location });
}
function gcloudUserClient({ project, location }) {
  let cached = { client: null, at: 0 };
  const fresh = () => {
    if (cached.client && Date.now() - cached.at < 5 * 60_000) return cached.client;
    const token = execFileSync("gcloud", ["auth", "print-access-token"], { encoding: "utf8", timeout: 15_000 }).trim();
    const authClient = new OAuth2Client();
    authClient.setCredentials({ access_token: token, expiry_date: Date.now() + 10 * 60_000 });
    cached = { client: new GoogleGenAI({ vertexai: true, project, location, googleAuthOptions: { authClient } }), at: Date.now() };
    return cached.client;
  };
  return {
    // A 401 (gcloud handed back a token that just expired): drop the cached client, fetch a new token.
    invalidate() { cached = { client: null, at: 0 }; },
    models: {
      generateContentStream: (a) => fresh().models.generateContentStream(a),
      generateContent: (a) => fresh().models.generateContent(a),
    },
  };
}

// USD per 1M tokens (Vertex list prices; override with ASK_MESHA_PRICE_<IN|OUT>_PER_M when they change).
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

export const MAX_STEPS_NOTE = "You have used all your lookup steps. Answer now from what you already found, and say plainly what you could not finish checking.";
export const TIME_UP_NOTE = "Time is up for this answer. Answer now from what you already found (no more lookups), and say plainly what you could not finish checking.";
export const MAX_PARALLEL_TOOLS = 4;
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

export const EMPTY_TURN_NUDGE = "Continue: use the tools you need, then write the final answer for the user.";

// The loop. tools: { declarations: [...], call(name, args) -> {text, isError, inline?} }.
// Returns { text (last turn), steps, usage, costUsd, error } where error is null | "error_max_turns" |
// "error_max_budget_usd" | a message. Throws only on abort or a non-retryable API failure.
export async function runAgent({
  ai, model, systemInstruction, contents, tools, onEvent = () => {}, signal,
  maxSteps = 40, budgetUsd = Infinity, thinkingLevel, env = process.env, retryDelayMs = 1000,
  fallbackModel = null, inlineBudget = INLINE_BUDGET_CHARS, deadlineMs = Infinity, now = Date.now,
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
    for (let attempt = 0; ; attempt++) {
      if (turnText) onEvent({ type: "turn_retry" }); // text of the failed try is withdrawn on screen
      modelParts = []; calls = []; turnText = ""; lastUsage = null;
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
              onEvent({ type: "text", text: part.text });
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
      return { text: lastText, steps: step + 1, usage, costUsd, error, model };
    }
    onEvent({ type: "turn_end", final: false });
    if (costUsd >= budgetUsd) {
      // Same contract as the SDK's maxBudgetUsd: stop spending, keep what was written.
      return { text: lastText, steps: step + 1, usage, costUsd, error: "error_max_budget_usd", model };
    }
    const withIds = calls.map((c) => ({ ...c, _id: c.id || `call_${++callSeq}` }));
    for (const c of withIds) onEvent({ type: "tool_call", id: c._id, name: c.name, args: c.args || {} });
    const results = await mapLimit(withIds, MAX_PARALLEL_TOOLS, async (c) => {
      let r;
      try { r = await tools.call(c.name, c.args || {}); } catch (e) { r = { text: `Tool failed: ${String(e?.message || e).slice(0, 500)}`, isError: true }; }
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
