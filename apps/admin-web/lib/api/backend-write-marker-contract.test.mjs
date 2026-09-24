import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../..", import.meta.url));

// Raw backend fetches that are allowed to skip noteBackendWrite(): each is either GET-only or a
// write that cannot change anything the short read cache answers (weighing / growth analytics).
const EXEMPT = new Map([
  ["lib/api/server.ts", "timedBackendFetch itself marks writes; the proof-upload PUT only stores an evidence file"],
  ["lib/api/browser-push-server.ts", "browser push registration: no cached read depends on it"],
  ["app/api/auth/session/route.ts", "session events: auth only"],
  ["app/api/ceo-ai/_forward.ts", "CEO AI chat: read-only questions"],
  ["app/api/herd-signals/export.csv/route.ts", "GET only"],
  ["app/api/herd-signals/live/stream/route.ts", "GET only"],
  ["app/api/herd-signals/live/route.ts", "GET only"],
  ["features/preventive-care-vaccination/command-board-drilldowns.ts", "GET only"],
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

test("every raw backend fetch outside timedBackendFetch stamps the write marker or is exempted", () => {
  const offenders = [];
  for (const dir of ["app", "lib", "features", "components"]) {
    for (const file of sourceFiles(join(root, dir))) {
      const src = readFileSync(file, "utf8");
      if (!/\bbaseUrl\b/.test(src) || !/(?<![\w.])fetch\(/.test(src)) continue;
      const rel = relative(root, file);
      if (EXEMPT.has(rel) || /noteBackendWrite\(/.test(src)) continue;
      offenders.push(rel);
    }
  }
  assert.deepEqual(offenders, [], "backend writes must call noteBackendWrite() (or be exempted with a reason)");
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
