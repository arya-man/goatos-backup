export const API_LATENCY_POLICY_MS = Object.freeze({
  p90_ms: 300,
  p95_ms: 500,
  p99_ms: 1000,
});

export function normalizeApiLatencyEndpoints(endpoints) {
  return endpoints.map((endpoint, index) => normalizeApiLatencyEndpoint(endpoint, index));
}

export function normalizeApiLatencyEndpoint(endpoint, index = 0) {
  if (!endpoint || typeof endpoint !== "object" || Array.isArray(endpoint)) {
    throw new TypeError(`API latency endpoint at index ${index} must be an object`);
  }

  const name = endpoint.name || endpoint.path || `endpoint[${index}]`;
  const thresholds = {};
  for (const [key, ceilingMs] of Object.entries(API_LATENCY_POLICY_MS)) {
    const value = Number(endpoint[key] ?? ceilingMs);
    if (!Number.isFinite(value) || value <= 0) {
      throw new TypeError(`${name} ${key} must be a positive number`);
    }
    if (value > ceilingMs) {
      throw new RangeError(`${name} ${key}=${value}ms exceeds the hard ${ceilingMs}ms ceiling`);
    }
    thresholds[key] = value;
  }

  if (thresholds.p90_ms > thresholds.p95_ms || thresholds.p95_ms > thresholds.p99_ms) {
    throw new RangeError(`${name} latency thresholds must satisfy p90 <= p95 <= p99`);
  }

  return { ...endpoint, ...thresholds };
}
