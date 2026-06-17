import assert from "node:assert/strict";

const { allowUnauditedSessionRefresh } = await import("../lib/auth/session-audit-policy.ts");
const { maxAgeForFirebaseIdToken } = await import("../lib/auth/session-cookie.ts");

const nowSeconds = Math.floor(Date.now() / 1000);
const unexpiredCookie = makeJwt({ exp: nowSeconds + 3600 });
const expiredCookie = makeJwt({ exp: nowSeconds - 10 });
const malformedThreePartCookie = "header.payload.signature";

assert.notEqual(maxAgeForFirebaseIdToken(unexpiredCookie), null);
assert.equal(maxAgeForFirebaseIdToken(expiredCookie), null);
assert.equal(maxAgeForFirebaseIdToken(malformedThreePartCookie), null);

assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 500, maxAgeForFirebaseIdToken(unexpiredCookie) !== null), true);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 502, maxAgeForFirebaseIdToken(unexpiredCookie) !== null), true);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 503, maxAgeForFirebaseIdToken(expiredCookie) !== null), false);
assert.equal(
  allowUnauditedSessionRefresh("auth.session_refresh", 503, maxAgeForFirebaseIdToken(malformedThreePartCookie) !== null),
  false,
);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 503, false), false);
assert.equal(allowUnauditedSessionRefresh("auth.session_refresh", 401, true), false);
assert.equal(allowUnauditedSessionRefresh("auth.sign_in", 503, true), false);
assert.equal(allowUnauditedSessionRefresh("auth.sign_out", 503, true), false);

console.log("session audit refresh policy ok");

function makeJwt(payload) {
  return `${base64url({ alg: "none", typ: "JWT" })}.${base64url(payload)}.sig`;
}

function base64url(value) {
  return Buffer.from(JSON.stringify(value)).toString("base64url");
}
