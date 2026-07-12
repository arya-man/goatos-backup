#!/usr/bin/env node
// Auth regression guard: proves the admin-web session route binds the long-lived
// Firebase refresh token to the user its (backend-verified) id token
// authenticated, so a caller cannot pair account A's id token with account B's
// refresh token and have the SSR session silently become B after ~1h.
//
// Exercises the REAL pure decision logic (resolveBoundRefreshToken) and the REAL
// uid decoder (firebaseUidFromToken) — not a copy — with injected fakes for the
// Firebase token exchange. Run: `node tools/agent-hooks/check-refresh-binding.mjs`
// (aliased `--self-test` for parity with the other guards).

import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const authDir = path.resolve(here, "../../apps/admin-web/lib/auth");

const { resolveBoundRefreshToken, refreshCookieState } = await import(
  path.join(authDir, "refresh-binding.ts")
);
const { firebaseUidFromToken } = await import(
  path.join(authDir, "session-cookie.ts")
);

let failures = 0;
function check(name, cond) {
  if (cond) {
    console.log(`  ok   ${name}`);
  } else {
    console.error(`  FAIL ${name}`);
    failures += 1;
  }
}

function jwt(claims) {
  const enc = (obj) => Buffer.from(JSON.stringify(obj)).toString("base64url");
  return `${enc({ alg: "none", typ: "JWT" })}.${enc(claims)}.sig`;
}

const future = Math.floor(Date.now() / 1000) + 3600;

// --- firebaseUidFromToken (real decoder) ---
check("uid decoded from sub claim", firebaseUidFromToken(jwt({ sub: "uidA", exp: future })) === "uidA");
check("uid null for non-jwt", firebaseUidFromToken("not-a-jwt") === null);
check("uid null when sub absent", firebaseUidFromToken(jwt({ exp: future })) === null);

// --- resolveBoundRefreshToken (real decision logic) ---
// Tracking wrapper so we can assert whether the exchange ran.
function tracked(impl) {
  const wrapped = async (rt) => {
    wrapped.calls += 1;
    return impl(rt);
  };
  wrapped.calls = 0;
  return wrapped;
}

{
  const exchange = tracked(async () => ({ idToken: jwt({ sub: "uidA", exp: future }), refreshToken: "rotatedA" }));
  const d = await resolveBoundRefreshToken("uidA", "refreshA", exchange, firebaseUidFromToken);
  check("match -> store rotated token", d.decision === "store" && d.refreshToken === "rotatedA");
  check("match -> exchange was called", exchange.calls === 1);
}

{
  const exchange = tracked(async () => ({ idToken: jwt({ sub: "uidB", exp: future }), refreshToken: "rotatedB" }));
  const d = await resolveBoundRefreshToken("uidA", "refreshB", exchange, firebaseUidFromToken);
  check("uid mismatch -> reject", d.decision === "reject");
  check("mismatch -> exchange was actually verified", exchange.calls === 1);
}

{
  const exchange = tracked(async () => null);
  const d = await resolveBoundRefreshToken("uidA", "refreshX", exchange, firebaseUidFromToken);
  check("exchange unavailable -> skip (id-token-only, no unverified store)", d.decision === "skip");
}

{
  const exchange = tracked(async () => ({ idToken: "", refreshToken: "r" }));
  const d = await resolveBoundRefreshToken("uidA", "refreshX", exchange, firebaseUidFromToken);
  check("exchange returns empty id token -> skip", d.decision === "skip");
}

{
  const exchange = tracked(async () => ({ idToken: jwt({ sub: "uidA", exp: future }), refreshToken: "r" }));
  const d = await resolveBoundRefreshToken("uidA", "   ", exchange, firebaseUidFromToken);
  check("no refresh token -> skip", d.decision === "skip");
  check("no refresh token -> exchange never called", exchange.calls === 0);
}

{
  const exchange = tracked(async () => ({ idToken: jwt({ sub: "uidA", exp: future }), refreshToken: "r" }));
  const d = await resolveBoundRefreshToken(null, "refreshA", exchange, firebaseUidFromToken);
  check("no id-token uid -> skip", d.decision === "skip");
  check("no id-token uid -> exchange never called", exchange.calls === 0);
}

// --- refreshCookieState (cookie the route writes for each decision) ---
const LONG = 60 * 60 * 24 * 14;
{
  const s = refreshCookieState({ decision: "store", refreshToken: "rotatedA" }, LONG);
  check("store -> persist rotated token for full lifetime", s.value === "rotatedA" && s.maxAge === LONG);
}
{
  const s = refreshCookieState({ decision: "skip" }, LONG);
  check("skip -> clear cookie (empty value, maxAge 0)", s.value === "" && s.maxAge === 0);
}

// --- route-level cookie behaviour: pre-existing A refresh cookie + B sign-in
//     whose exchange fails (skip) must CLEAR A's refresh cookie. ---
{
  // Mirror the route: it always writes FIREBASE_REFRESH_TOKEN_COOKIE from
  // refreshCookieState(binding). Model the browser cookie jar as a Map.
  const REFRESH_COOKIE = "goatos_firebase_refresh_token";
  const jar = new Map([[REFRESH_COOKIE, { value: "A-refresh-token", maxAge: LONG }]]);
  const applyRoute = (binding) => {
    const s = refreshCookieState(binding, LONG);
    jar.set(REFRESH_COOKIE, { value: s.value, maxAge: s.maxAge });
  };

  // B signs in; exchange outage -> resolveBoundRefreshToken returns skip.
  const skip = await resolveBoundRefreshToken("uidB", "B-refresh-token", tracked(async () => null), firebaseUidFromToken);
  check("account switch B sign-in with exchange failure -> skip", skip.decision === "skip");
  applyRoute(skip);
  const after = jar.get(REFRESH_COOKIE);
  check("switch A->B (exchange fail): A refresh cookie is cleared", after.value === "" && after.maxAge === 0);
}

if (failures > 0) {
  console.error(`\nrefresh-binding guard: ${failures} check(s) FAILED.`);
  process.exit(1);
}
console.log("\nrefresh-binding guard: all checks passed.");
