import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { LOCK_HELD_ENV, assertSweepPermitted, classifyTarget, isProtectedHost, requestDelayMs } from "./sweep-safety.mjs";

const held = { [LOCK_HELD_ENV]: "1" };
const ok = (extra = {}) => ({ GOATOS_ADMIN_WEB_BASE_URL: "http://127.0.0.1:3300", ...held, ...extra });

test("saying nothing gets you nothing, never the live farm", () => {
  // The default that mattered: this used to fall through to dashboard.mesha.sg.
  assert.throws(() => assertSweepPermitted({ env: { ...held } }), /will not guess/);
  assert.throws(() => assertSweepPermitted({ env: { ...held, GOATOS_ADMIN_WEB_BASE_URL: "  " } }), /will not guess/);
});

test("every host that serves the farm is protected, including subdomains", () => {
  for (const host of ["dashboard.mesha.sg", "api.goatos.mesha.sg", "stg-api.dashboard.mesha.sg", "DASHBOARD.MESHA.SG", "a.dashboard.mesha.sg"]) {
    assert.ok(isProtectedHost(host), `${host} must be protected`);
    assert.equal(classifyTarget(`https://${host}/tasks`), "protected");
  }
  assert.equal(classifyTarget("http://127.0.0.1:3300"), "local");
  assert.equal(classifyTarget("https://review-7.internal.example"), "other");
});

test("the live farm must be asked for out loud", () => {
  const env = { ...held, GOATOS_ADMIN_WEB_BASE_URL: "https://dashboard.mesha.sg" };
  assert.throws(() => assertSweepPermitted({ env, pageLoads: 292 }), /ask for it out loud/);
  assert.throws(() => assertSweepPermitted({ env, pageLoads: 292 }), /292 pages/);
  const allowed = assertSweepPermitted({ env: { ...env, GOATOS_SWEEP_ALLOW_PRODUCTION: "1" }, pageLoads: 292 });
  assert.equal(allowed.target, "protected");
});

test("a sweep outside the run lock refuses, whatever it points at", () => {
  // §1's guarantee is that a SECOND sweep refuses rather than queues, and that
  // holds for staging too: two sweeps of one box still queue behind each other.
  for (const url of ["http://127.0.0.1:3300", "https://review-7.internal.example", "https://dashboard.mesha.sg"]) {
    const env = { GOATOS_ADMIN_WEB_BASE_URL: url, GOATOS_SWEEP_ALLOW_PRODUCTION: "1" };
    assert.throws(() => assertSweepPermitted({ env }), /must run inside the automation run lock/, url);
  }
});

test("it must not grow a second lock of its own", () => {
  // Two locks do not see each other, so the refusal must come from the one
  // run-oci.sh already holds.
  // Scan the CODE, not the prose. The first version of this test read the whole
  // file and fired on its own comments explaining why there is no second lock —
  // a false positive in the check that exists to prevent false positives.
  const source = readFileSync(new URL("./sweep-safety.mjs", import.meta.url), "utf8");
  // Strip comments AND the text inside quotes: the refusal message names the
  // flock it defers to, and matching on that word flagged the deferral itself.
  // What identifies a second lock is an API that TAKES one, not a word.
  const code = source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n").filter((l) => !l.trim().startsWith("//")).join("\n")
    .replace(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|`(?:[^`\\]|\\.)*`/g, '""');
  const TAKES_A_LOCK = /openSync\s*\(|flockSync|proper-lockfile|lockfileSync|mkdirSync\s*\([^)]*lock/i;
  assert.ok(!TAKES_A_LOCK.test(code), "this module must not take a lock of its own; it defers to run-oci.sh's flock");
  // The scan is not vacuous: it sees a real one when there is one.
  assert.ok(TAKES_A_LOCK.test(`${code}\nconst fd = openSync(LOCK, "wx");`), "the scan can detect a lock if one is added");
});

test("a browser that is already alive stops the sweep", () => {
  assert.throws(() => assertSweepPermitted({ env: ok(), browsersRunning: 3 }), /already running/);
  assert.doesNotThrow(() => assertSweepPermitted({ env: ok(), browsersRunning: 0 }));
});

test("a local sweep inside the lock is allowed, with no ceremony", () => {
  const allowed = assertSweepPermitted({ env: ok(), pageLoads: 292 });
  assert.equal(allowed.target, "local");
  assert.equal(allowed.baseUrl, "http://127.0.0.1:3300");
});

test("pacing comes from the same knob run-oci.sh sets", () => {
  assert.equal(requestDelayMs({}), 150);
  assert.equal(requestDelayMs({ GOATOS_SMOKE_REQUEST_DELAY_MS: "400" }), 400);
  assert.equal(requestDelayMs({ GOATOS_SMOKE_REQUEST_DELAY_MS: "nonsense" }), 150, "a bad value falls back to the safe pace, never to zero");
});
