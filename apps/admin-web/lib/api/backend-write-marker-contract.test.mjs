import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../..", import.meta.url));

// Every raw server-side fetch( call, keyed "file :: first argument". Each is GET-only, not a
// backend call, or a write that cannot change anything the short read cache answers (weighing /
// growth analytics). A new call is not covered by this list: it must call noteBackendWrite().
const EXEMPT = new Map([
  ["lib/api/server.ts :: input", "timedBackendFetch itself: marks every non-GET in finally"],
  ["lib/api/server.ts :: target", "proof-upload PUT: stores an evidence file, no cached read depends on it"],
  ["lib/api/browser-push-server.ts :: `${config.data.baseUrl}${path}`", "browser push registration: no cached read depends on it"],
  ["app/api/auth/session/route.ts :: `${baseUrl}/auth/session-events`", "session events: auth audit only"],
  ["app/api/ceo-ai/_forward.ts :: `${METADATA_IDENTITY_URL}?audience=${encodeURIComponent(audience)}`", "GCP metadata identity token, not the backend"],
  ["app/api/ceo-ai/_forward.ts :: `${agentUrl ?? baseUrl.replace(/\\/$/, \"\")}${path}`", "forwards CEO AI conversation create/update/delete and questions; no cached analytics depends on conversations"],
  ["app/api/herd-signals/export.csv/route.ts :: upstreamUrl", "GET only"],
  ["app/api/herd-signals/live/stream/route.ts :: upstreamUrl", "GET only"],
  ["app/api/herd-signals/live/route.ts :: upstreamUrl", "GET only"],
  ["lib/api/client.ts :: url", "browser client: same-origin /api/admin/roster URLs built in a variable"],
  ["lib/auth/firebase-refresh.ts :: `${SECURE_TOKEN_URL}?key=${encodeURIComponent(apiKey)}`", "Google secure-token refresh, not the backend"],
]);

function sourceFiles(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const path = join(dir, name);
    if (statSync(path).isDirectory()) out.push(...sourceFiles(path));
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\./.test(name)) out.push(path);
  }
  return out;
}

// Raw fetch( calls with their first argument; browser code only reaches the backend through the
// same-origin /api routes, which are themselves scanned here.
function rawFetchCalls(src) {
  const calls = [];
  const re = /(^|[^\w.])fetch\(/g;
  let m;
  while ((m = re.exec(src))) {
    const start = m.index + m[0].length;
    let depth = 0;
    let i = start;
    let quote = null;
    for (; i < src.length; i++) {
      const c = src[i];
      if (quote) {
        if (c === "\\") i++;
        else if (c === quote) quote = null;
        continue;
      }
      if (c === '"' || c === "'" || c === "`") quote = c;
      else if (c === "(" || c === "[" || c === "{") depth++;
      else if (c === ")" || c === "]" || c === "}") {
        if (depth === 0) break;
        depth--;
      } else if (c === "," && depth === 0) break;
    }
    calls.push({ index: m.index, arg: src.slice(start, i).trim() });
  }
  return calls;
}

function serverFetchCalls() {
  const found = [];
  for (const dir of ["app", "lib", "features", "components"]) {
    for (const file of sourceFiles(join(root, dir))) {
      const src = readFileSync(file, "utf8");
      if (/^\s*["']use client["']/.test(src)) continue;
      const rel = relative(root, file);
      const calls = rawFetchCalls(src);
      calls.forEach((call, n) => {
        if (/^["'`]\/api\//.test(call.arg)) return;
        const end = n + 1 < calls.length ? calls[n + 1].index : src.length;
        found.push({ key: `${rel} :: ${call.arg}`, marked: /noteBackendWrite\(/.test(src.slice(call.index, end)) });
      });
    }
  }
  return found;
}

test("every raw server-side fetch call stamps the write marker or is exempted with a reason", () => {
  const offenders = serverFetchCalls()
    .filter((call) => !call.marked && !EXEMPT.has(call.key))
    .map((call) => call.key);
  assert.deepEqual(offenders, [], "backend writes must call noteBackendWrite() (or be exempted with a reason)");
});

test("server.ts has raw fetch only in timedBackendFetch and the proof-upload PUT", () => {
  const keys = serverFetchCalls().filter((c) => c.key.startsWith("lib/api/server.ts ::")).map((c) => c.key);
  assert.deepEqual(keys, ["lib/api/server.ts :: input", "lib/api/server.ts :: target"]);
  const server = readFileSync(join(root, "lib/api/server.ts"), "utf8");
  const put = rawFetchCalls(server).find((c) => c.arg === "target");
  assert.match(server.slice(put.index, put.index + 120), /method: "PUT"/);
});

test("a second unmarked write in a file that already calls noteBackendWrite is caught", () => {
  const src = 'await fetch(a, init);\nawait noteBackendWrite();\nawait fetch(b, { method: "POST" });\n';
  const calls = rawFetchCalls(src);
  assert.deepEqual(calls.map((c) => c.arg), ["a", "b"]);
  assert.equal(/noteBackendWrite\(/.test(src.slice(calls[1].index)), false);
});

test("configuration sheet imports note the write after it completes", () => {
  const route = readFileSync(join(root, "app/api/admin/configuration-sheets/[...path]/route.ts"), "utf8");
  assert.match(route, /finally \{[\s\S]{0,200}?noteBackendWrite\(\)/);
});

test("timedBackendFetch stamps the marker after the write, in finally", () => {
  const server = readFileSync(join(root, "lib/api/server.ts"), "utf8");
  const body = server.slice(server.indexOf("async function timedBackendFetch"), server.indexOf("export function isAuthRequiredError"));
  const tryAt = body.indexOf("try {");
  assert.ok(body.indexOf("markCallerWrite") === -1 || body.indexOf("markCallerWrite") > tryAt);
  assert.match(body, /\} finally \{[\s\S]*?await noteBackendWrite\(\);/);
});

// A write the backend REFUSED (4xx) changed nothing, so it must not stamp the marker: the stamp is
// a cookie, a cookie set in a Server Action refreshes the page, and that refresh closed the open
// same-page drawer holding the refusal message (Configuration > Items & settings, 2026-09-25).
test("a refused write does not stamp the read-your-writes marker", () => {
  const src = readFileSync(join(root, "lib/api/server.ts"), "utf8");
  assert.match(src, /writeMayHaveLanded\(responseStatus\)\) await noteBackendWrite\(\)/);
  assert.match(src, /return !\(status >= 400 && status < 500\);/);
});
