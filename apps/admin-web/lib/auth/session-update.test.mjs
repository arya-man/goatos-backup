import assert from "node:assert/strict";
import test from "node:test";
import { authEventFailure, planSessionUpdate } from "./session-update.ts";

test("a busy auth database stays a 503 with its Retry-After; other 5xx collapse to 502", () => {
  assert.deepEqual(authEventFailure(503, "auth_database_busy", "5"), {
    ok: false,
    status: 503,
    error: "auth_database_busy",
    retryAfter: "5",
  });
  assert.deepEqual(authEventFailure(500, null, null), { ok: false, status: 502, error: "auth_audit_failed" });
  assert.deepEqual(authEventFailure(403, "email_not_allowed", null), { ok: false, status: 403, error: "email_not_allowed" });
});

test("the plan carries the backend Retry-After to the route", async () => {
  const plan = await planSessionUpdate({
    resolveBinding: async () => ({ decision: "skip" }),
    recordEvent: async () => authEventFailure(503, "auth_database_busy", "5"),
  });
  assert.deepEqual(plan, {
    outcome: "audit_failed",
    status: 503,
    error: "auth_database_busy",
    retryAfter: "5",
    eventRecorded: false,
  });
});
