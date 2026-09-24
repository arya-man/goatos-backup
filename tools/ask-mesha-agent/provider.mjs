// Claude provider switch for ASK_MESHA_CLAUDE_AUTH=auto.
// The service carries BOTH Vertex env (runtime SA) and ANTHROPIC_API_KEY. A tiny Vertex
// probe decides which one each request uses: Vertex once it answers, the API key until then.
// Pure helpers are exported for tests; createProviderSwitch() holds the live state.
import { execFile } from "node:child_process";

export const PROVIDERS = ["vertex", "anthropic"];
export const PROBE_RETRY_MS = 15 * 60_000; // while on the API key: re-check Vertex every 15 min
export const PROBE_HEALTHY_MS = 60 * 60_000; // while on Vertex: re-confirm hourly
const METADATA_TOKEN_URL = "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token";
const VERTEX_ONLY_ENV = ["CLAUDE_CODE_USE_VERTEX", "ANTHROPIC_VERTEX_PROJECT_ID", "CLOUD_ML_REGION"];

// Auth mode -> fixed provider, or null for auto. oauth bills the Anthropic side too.
export function fixedProvider(mode) {
  if (mode === "vertex") return "vertex";
  if (mode === "api-key" || mode === "oauth") return "anthropic";
  return null;
}

// Mode from env. Unset keeps the pre-auto behaviour: Vertex iff CLAUDE_CODE_USE_VERTEX is set.
export function authMode(env) {
  const m = String(env.ASK_MESHA_CLAUDE_AUTH || "").trim();
  if (["auto", "vertex", "api-key", "oauth"].includes(m)) return m;
  return env.CLAUDE_CODE_USE_VERTEX ? "vertex" : "api-key";
}

// Per request: forced mode wins; in auto, Vertex only after a successful probe.
export function selectProvider(mode, state = {}) {
  return fixedProvider(mode) ?? (state.vertexOk ? "vertex" : "anthropic");
}

// Agent env for one provider. Vertex runs never see the API key (and vice versa), so the
// SDK can't silently pick the other backend.
export function envForProvider(provider, env) {
  const out = { ...env };
  if (provider === "vertex") {
    delete out.ANTHROPIC_API_KEY;
    delete out.CLAUDE_CODE_OAUTH_TOKEN;
    out.CLAUDE_CODE_USE_VERTEX = "1";
    out.CLOUD_ML_REGION ||= "global";
  }
  else for (const k of VERTEX_ONLY_ENV) delete out[k];
  return out;
}

export function vertexUrl({ project, region, model }) {
  const host = region === "global" ? "aiplatform.googleapis.com" : `${region}-aiplatform.googleapis.com`;
  return `https://${host}/v1/projects/${project}/locations/${region}/publishers/anthropic/models/${model}:rawPredict`;
}

// rawPredict response -> {ok, reason}. reason is a short class, never a body dump.
export function parseProbeResponse(status, bodyText = "") {
  if (status >= 200 && status < 300) {
    try {
      const b = JSON.parse(bodyText);
      if (b && (b.type === "message" || Array.isArray(b.content))) return { ok: true, reason: "ok" };
    } catch {}
    return { ok: false, reason: "bad_body" };
  }
  const t = String(bodyText).toLowerCase();
  if (status === 429) return { ok: false, reason: /quota|resource_exhausted/.test(t) ? "quota_429" : "rate_limited_429" };
  if (status === 403) return { ok: false, reason: "forbidden_403" };
  if (status === 404) return { ok: false, reason: "not_found_404" };
  if (status === 401) return { ok: false, reason: "unauthenticated_401" };
  return { ok: false, reason: `http_${status}` };
}

// Status code or error text that means "Vertex can't serve this now" (quota, permission,
// model not enabled) -> worth a retry on the API key. 5xx/overload is not: the key won't fix it.
export function isVertexUnavailable({ status = null, error = "" } = {}) {
  if ([429, 403, 404].includes(Number(status))) return true;
  const t = String(error || "").toLowerCase();
  if (["rate_limit", "model_not_found", "cloud_credential_error"].includes(t)) return true;
  return /\b(429|403|404)\b|resource[_ ]exhausted|quota|permission[_ ]denied|model_not_found|not found for|rate[_ ]limit/.test(t);
}

// Retry once on the API key only when: auto mode, the run was on Vertex, the failure is
// Vertex-availability-shaped, nothing reached the user yet, and we haven't retried already.
// toolCalls > 0: the run already did work (queries, a live watch); rerunning would repeat it.
export function shouldFallback({ mode, provider, status = null, error = "", streamed = false, attempt = 0, aborted = false, toolCalls = 0 }) {
  return mode === "auto" && provider === "vertex" && !streamed && attempt === 0 && !aborted && !toolCalls && isVertexUnavailable({ status, error });
}

// Answer cost after a fallback: the failed attempt's spend is added once to the retry's.
export function combinedCost(retryCost, failedAttemptCost) {
  if (!failedAttemptCost) return retryCost;
  return (retryCost ?? 0) + failedAttemptCost;
}

// Access token: GCE/Cloud Run metadata server first; locally gcloud if installed; else null.
export async function accessToken({ fetchImpl = fetch, execImpl = execFile, timeoutMs = 3000 } = {}) {
  try {
    const r = await fetchImpl(METADATA_TOKEN_URL, { headers: { "Metadata-Flavor": "Google" }, signal: AbortSignal.timeout(timeoutMs) });
    if (r.ok) {
      const j = await r.json();
      if (j?.access_token) return j.access_token;
    }
  } catch {}
  return new Promise((resolve) => {
    try {
      execImpl("gcloud", ["auth", "print-access-token"], { timeout: 10_000 }, (err, out) => resolve(err ? null : String(out).trim() || null));
    } catch { resolve(null); }
  });
}

// One tiny rawPredict (max_tokens 1). Returns {ok, reason}; never throws.
export async function probeVertex({ project, region, model, fetchImpl = fetch, tokenFn = () => accessToken({ fetchImpl }), timeoutMs = 20_000 }) {
  if (!project) return { ok: false, reason: "no_project" };
  const token = await tokenFn().catch(() => null);
  if (!token) return { ok: false, reason: "no_token" };
  try {
    const r = await fetchImpl(vertexUrl({ project, region, model }), {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify({ anthropic_version: "vertex-2023-10-16", max_tokens: 1, messages: [{ role: "user", content: "ok" }] }),
      signal: AbortSignal.timeout(timeoutMs),
    });
    return parseProbeResponse(r.status, await r.text().catch(() => ""));
  } catch (e) {
    return { ok: false, reason: e?.name === "TimeoutError" ? "timeout" : "network" };
  }
}

// Live switch. probe: async () => {ok, reason}. Timers are unref'd so tests/process exit aren't held.
export function createProviderSwitch({ mode, probe, log = (l) => console.log(l), now = () => Date.now(), setTimer = setTimeout, clearTimer = clearTimeout } = {}) {
  const state = { vertexOk: false, lastProbeAt: null, lastProbeReason: null, switchedAt: null, probing: null };
  let timer = null;
  let current = selectProvider(mode, state);

  function apply(next, why) {
    if (next !== current) {
      log(`[provider] switched to ${next} (${why})`);
      current = next;
      state.switchedAt = new Date(now()).toISOString();
    }
  }
  function schedule() {
    if (mode !== "auto") return;
    if (timer) clearTimer(timer);
    timer = setTimer(() => void runProbe(), state.vertexOk ? PROBE_HEALTHY_MS : PROBE_RETRY_MS);
    timer?.unref?.();
  }
  async function runProbe() {
    if (mode !== "auto") return state;
    if (state.probing) return state.probing;
    state.probing = (async () => {
      const r = await Promise.resolve().then(probe).catch(() => ({ ok: false, reason: "probe_error" }));
      state.vertexOk = Boolean(r?.ok);
      state.lastProbeAt = new Date(now()).toISOString();
      state.lastProbeReason = r?.reason ?? null;
      log(`[provider] vertex probe ${state.vertexOk ? "ok" : `unavailable: ${state.lastProbeReason}`}`);
      apply(selectProvider(mode, state), `probe ${state.lastProbeReason}`);
      state.probing = null;
      schedule();
      return state;
    })();
    return state.probing;
  }
  return {
    mode,
    start() { if (mode === "auto") void runProbe(); return this; },
    probeNow: runProbe,
    current: () => current,
    // A live Vertex run hit quota/permission: stop using it until the next probe says OK.
    markVertexDown(reason) {
      if (mode !== "auto" || !state.vertexOk) return;
      state.vertexOk = false;
      state.lastProbeReason = `runtime: ${String(reason || "").slice(0, 80)}`;
      apply("anthropic", "vertex run failed");
      schedule();
    },
    status: () => ({
      mode, provider: current, vertex_ok: state.vertexOk, last_probe_at: state.lastProbeAt,
      last_probe_reason: state.lastProbeReason, switched_at: state.switchedAt,
    }),
    stop() { if (timer) clearTimer(timer); timer = null; },
  };
}
