import assert from "node:assert/strict";

const { allowUnauditedSessionRefresh } = await import("../lib/auth/session-audit-policy.ts");

assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 500, true), true);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 502, true), true);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 503, false), false);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 401, true), false);
assert.equal(allowUnauditedSessionRefresh("auth.sign_in", 503, true), false);
assert.equal(allowUnauditedSessionRefresh("auth.sign_out", 503, true), false);

console.log("session audit refresh policy ok");
