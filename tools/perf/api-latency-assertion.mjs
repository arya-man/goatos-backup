export function assertPayload(endpoint, payload) {
  const assertion = endpoint.assertion;
  if (!assertion) return;
  const value = String(assertion.path ?? "").split(".").filter(Boolean).reduce((current, key) => current?.[key], payload);
  if (assertion.type === "array_contains") {
    if (!Array.isArray(value) || !value.includes(assertion.value)) {
      throw new Error(`${endpoint.name} assertion ${assertion.path} must contain ${assertion.value}`);
    }
    return;
  }
  if (assertion.type === "array_min") {
    if (!Array.isArray(value) || value.length < Number(assertion.min ?? 1)) {
      throw new Error(`${endpoint.name} assertion ${assertion.path} requires at least ${assertion.min ?? 1} rows`);
    }
    return;
  }
  if (assertion.type === "number_min") {
    if (!Number.isFinite(Number(value)) || Number(value) < Number(assertion.min ?? 1)) {
      throw new Error(`${endpoint.name} assertion ${assertion.path} requires value >= ${assertion.min ?? 1}`);
    }
    return;
  }
  throw new Error(`${endpoint.name} has unsupported assertion type ${assertion.type}`);
}
