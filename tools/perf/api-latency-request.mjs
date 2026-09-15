export const FRESH_AS_OF_NAME = "pr264_app_vaccination_execution_fresh_as_of";
export const VACCINATION_PATH = "/app/vaccination/execution?limit=20&include_filter_options=true&include_card_summaries=true";

export function validateRequestStrategy(endpoint) {
  if (endpoint.request_strategy === undefined || endpoint.request_strategy === null) return;
  if (endpoint.request_strategy !== "fresh_as_of" || endpoint.path !== VACCINATION_PATH
    || (endpoint.method ?? "GET") !== "GET") {
    throw new Error("fresh_as_of is supported only for the fixed vaccination execution GET workload");
  }
}

// A future live-as-of is clamped to server request time. It avoids the default
// 30-second response-cache key without changing the operational business date.
export function createRequestPlanner() {
  let last = 0;
  return (endpoint, now = Date.now()) => {
    validateRequestStrategy(endpoint);
    if (endpoint.request_strategy !== "fresh_as_of") return { request_path: endpoint.path, request_started_at: new Date(now).toISOString() };
    const stamp = Math.max(now + 1000, last + 1);
    if (stamp - now > 2000) throw new Error("fresh_as_of concurrency exceeds bounded future offset");
    last = stamp;
    return { request_path: `${endpoint.path}&as_of=${encodeURIComponent(new Date(stamp).toISOString())}`, request_started_at: new Date(now).toISOString() };
  };
}

export function freshAsOfEvidenceFailures(result) {
  const failures = [];
  if (result.request_strategy !== "fresh_as_of" || result.path !== VACCINATION_PATH
    || result.method !== "GET" || result.assertion?.type !== "array_min"
    || result.assertion?.path !== "rows" || !Number.isFinite(result.assertion?.min) || result.assertion.min < 1) failures.push("fresh_as_of workload/strategy/assertion differs from required workload");
  const seen = new Set();
  for (const sample of [...(result.warmup_response_observations ?? []), ...(result.response_observations ?? [])]) {
    try {
      const url = new URL(sample.request_path, "http://local.invalid");
      const values = url.searchParams.getAll("as_of");
      const stamp = values[0];
      url.searchParams.delete("as_of");
      const offset = Date.parse(stamp) - Date.parse(sample.request_started_at);
      if (values.length !== 1 || `${url.pathname}${url.search}` !== VACCINATION_PATH
        || url.origin !== "http://local.invalid" || url.hash || !Number.isFinite(offset)
        || offset < 1000 || offset > 2000 || seen.has(stamp)) throw new Error("invalid");
      seen.add(stamp);
    } catch { failures.push("fresh_as_of sample must preserve fixed query params and have a unique bounded future request timestamp"); }
  }
  return failures;
}

// Initial requests are evidence too: warmup cannot hide cache misses or pool
// startup latency. Keep this phase separate from steady-state percentiles.
export function warmupEvidenceFailures(result, expectedCount) {
  const times = result.warmup_samples_ms;
  const bytes = result.warmup_response_bytes;
  const observations = result.warmup_response_observations;
  const failures = [];
  if (!Number.isInteger(expectedCount) || expectedCount < 1
    || !Array.isArray(times) || times.length !== expectedCount
    || times.some((ms) => !Number.isFinite(ms) || ms < 0 || ms > 500)
    || result.warmup_max_ms !== Math.max(...(times ?? []))) {
    failures.push("warmup evidence must include every initial request with a hard 500ms maximum");
  }
  if (!Array.isArray(bytes) || bytes.length !== expectedCount
    || bytes.some((size) => !Number.isInteger(size) || size <= 0 || size > 1024 * 1024
      || size > result.response_bytes_threshold)) {
    failures.push("warmup response bytes must cover every initial request within the payload ceiling");
  }
  if (!Array.isArray(observations) || observations.length !== expectedCount
    || observations.some((item) => !Number.isFinite(item?.assertion_value)
      || item.assertion_value < Number(result.assertion?.min ?? 1)
      || !item.row_counts || typeof item.row_counts !== "object"
      || Object.values(item.row_counts).some((count) => !Number.isInteger(count) || count < 0)
      || !Array.isArray(item.degraded) || item.degraded.length !== 0)) {
    failures.push("warmup observations must preserve valid row counts and no degraded responses");
  }
  return failures;
}
