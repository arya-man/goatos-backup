export const API_LATENCY_POLICY_MS = Object.freeze({
  p90_ms: 300,
  p95_ms: 500,
  p99_ms: 1000,
});

export const API_RESPONSE_BYTES_CEILING = 1024 * 1024;

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

  const maxResponseBytes = Number(endpoint.max_response_bytes ?? API_RESPONSE_BYTES_CEILING);
  if (!Number.isInteger(maxResponseBytes) || maxResponseBytes <= 0) {
    throw new TypeError(`${name} max_response_bytes must be a positive integer`);
  }
  if (maxResponseBytes > API_RESPONSE_BYTES_CEILING) {
    throw new RangeError(`${name} max_response_bytes=${maxResponseBytes} exceeds the hard ${API_RESPONSE_BYTES_CEILING}-byte ceiling`);
  }

  return { ...endpoint, ...thresholds, max_response_bytes: maxResponseBytes };
}
