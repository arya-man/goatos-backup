import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  describeBrowser,
  getBrowserInstallId,
  isWellFormedVapidPublicKey,
  leadershipTaskDeepLink,
  mintInstallId,
  resetBrowserInstallId,
  resolveVapidKeyConfig,
  resolveWebPushState,
  vapidGetTokenOptions,
  VAPID_KEY_UNUSABLE_MESSAGE,
  WEB_PUSH_TOKEN_TIMEOUT_MS,
  withTimeout,
} from "./web-push-state.ts";

const repoAdminWeb = join(dirname(fileURLToPath(import.meta.url)), "..");
const serviceWorkerSource = readFileSync(join(repoAdminWeb, "public/firebase-messaging-sw.js"), "utf8");

/**
 * The worker's CODE, with comments stripped.
 *
 * Needed because the worker documents exactly what it must not do ("it does not trust data.href",
 * "no importScripts of a CDN bundle"), so a naive substring search over the raw file matches the
 * prose and the ban asserts the opposite of what it means. Crude but sufficient: this file has no
 * string literal containing a comment marker.
 */
const serviceWorkerCode = serviceWorkerSource
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/^[ \t]*\/\/.*$/gm, "")
  .replace(/([^:])\/\/.*$/gm, "$1");

test("an unsupported browser is reported as unsupported, with a reason", () => {
  const state = resolveWebPushState({
    supported: false,
    supportReason: "Notifications need a secure (https) connection.",
    configured: true,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(state.status, "unsupported");
  assert.equal(state.reason, "Notifications need a secure (https) connection.");
});

test("an unsupported browser never falls through to an actionable prompt, even when permission is granted", () => {
  // Regression shape: support is checked FIRST. Reordering the checks would offer an Enable
  // button on a browser that cannot subscribe, which reads as a broken button.
  const state = resolveWebPushState({
    supported: false,
    supportReason: "",
    configured: true,
    permission: "granted",
    hasRegistration: true,
    browserInstallId: "web-1",
  });
  assert.equal(state.status, "unsupported");
});

test("an UNUSABLE VAPID key is OUR gap and is reported as unconfigured, not as blocked", () => {
  // `configured: false` no longer means "nobody pasted a key" -- see resolveVapidKeyConfig. It
  // means the key that IS set cannot be used, which is the one configuration gap a person
  // cannot act on, so `unconfigured` (which offers no control) is still the honest answer.
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: false,
    configReason: VAPID_KEY_UNUSABLE_MESSAGE,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(state.status, "unconfigured");
  assert.equal(state.reason, VAPID_KEY_UNUSABLE_MESSAGE);
});

test("denied permission is blocked", () => {
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "denied",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(state.status, "blocked");
});

test("a dismissed prompt is retryable, NOT a refusal", () => {
  // The two are indistinguishable to the Notification API -- both leave permission at 'default'.
  // Reporting a dismissal as 'blocked' would send the person into browser site settings to undo
  // something they never did.
  const dismissed = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
    promptWasDismissed: true,
  });
  assert.equal(dismissed.status, "dismissed");
  assert.notEqual(dismissed.status, "blocked");

  const neverAsked = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(neverAsked.status, "prompt");
});

test("granted permission with no registration is actionable, never reported as enabled", () => {
  // This is the token-rotated / site-data-cleared case and it is common, not exotic. Reporting it
  // as enabled would leave a person believing they are subscribed to nothing.
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "granted",
    hasRegistration: false,
    browserInstallId: "web-abc",
  });
  assert.equal(state.status, "prompt");
});

test("granted permission with a registration is enabled and carries the browser id", () => {
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "granted",
    hasRegistration: true,
    browserInstallId: "web-abc",
  });
  assert.deepEqual(state, { status: "enabled", browserInstallId: "web-abc" });
});

test("an unavailable Notification API is treated as unanswered, not as granted", () => {
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "unavailable",
    hasRegistration: true,
    browserInstallId: "web-abc",
  });
  assert.equal(state.status, "prompt");
});

test("the browser label reads the user agent in the right order", () => {
  // Edge and Opera both carry "Chrome", and Chrome carries "Safari": a naive check order labels
  // every Chromium browser "Google Chrome" or everything "Safari".
  assert.equal(
    describeBrowser("Mozilla/5.0 (Macintosh) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"),
    "Google Chrome",
  );
  assert.equal(
    describeBrowser("Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0"),
    "Microsoft Edge",
  );
  assert.equal(
    describeBrowser("Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/139.0.0.0 Safari/537.36 OPR/124.0.0.0"),
    "Opera",
  );
  assert.equal(describeBrowser("Mozilla/5.0 (X11; Linux) Gecko/20100101 Firefox/141.0"), "Firefox");
  assert.equal(
    describeBrowser("Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15"),
    "Safari",
  );
  assert.equal(describeBrowser(""), "Browser");
});

test("the leadership-task deep link is the web route, scoped to team progress", () => {
  assert.equal(
    leadershipTaskDeepLink("6f2b1b62-0000-4000-8000-000000000001"),
    "/tasks?scope=team_progress&task=6f2b1b62-0000-4000-8000-000000000001",
  );
});

test("a leadership-task deep link with no task id still opens the list", () => {
  assert.equal(leadershipTaskDeepLink(""), "/tasks?scope=team_progress");
  assert.equal(leadershipTaskDeepLink("   "), "/tasks?scope=team_progress");
});

test("the task id is encoded, so a hostile id cannot forge query parameters", () => {
  const link = leadershipTaskDeepLink("abc&scope=admin");
  assert.equal(link, "/tasks?scope=team_progress&task=abc%26scope%3Dadmin");
  assert.equal(link.split("scope=").length, 2);
});

test("every leadership-task notification type deep-links to the task", () => {
  // A type missing from DEEP_LINKS still SHOWS the notification but sends the click to "/",
  // which reads as a broken notification rather than a missing one. These four are the types
  // the bridge actually queues (leadership_task_activity_notify_consumer.go and
  // leadership_task_notify_consumer.go), so any new type must be added here too.
  for (const type of [
    "leadership_task_raised",
    "leadership_task_done",
    "leadership_task_status",
    "leadership_task_mentioned",
    "leadership_task_commented",
    "leadership_task_updated",
  ]) {
    assert.match(
      serviceWorkerCode,
      new RegExp(`${type}:\\s*\\(data\\)\\s*=>\\s*leadershipTaskLink\\(data\\)`),
      `${type} is not in the service worker DEEP_LINKS map, so its click would land on "/"`,
    );
  }
});

test("the service worker builds the SAME web route as the app", () => {
  // The worker is served verbatim from public/ and cannot import the app's helper, so the route
  // shape exists twice. This is the only thing standing between that and silent drift, which
  // would send every notification click to a page that does not exist.
  assert.match(serviceWorkerCode, /\/tasks\?scope=team_progress&task=\$\{encodeURIComponent\(taskId\)\}/);
  assert.match(serviceWorkerCode, /return "\/tasks\?scope=team_progress";/);
});

test("the service worker does not trust the notification's own href/target", () => {
  // data.href and data.target are the ANDROID routes (/leadership-tasks/<id>), composed for the
  // phone. Opening one in Chrome lands on a route the dashboard does not have.
  assert.ok(
    !/data\.(href|target)/.test(serviceWorkerCode),
    "the service worker must not read data.href or data.target",
  );
});

test("the service worker always shows a notification and always handles a click", () => {
  // A push handler that can finish without showNotification gets the origin's push permission
  // revoked by Chrome; a notification with no click handler reads as broken.
  assert.match(serviceWorkerCode, /addEventListener\("push"/);
  assert.match(serviceWorkerCode, /addEventListener\("notificationclick"/);
  assert.match(serviceWorkerCode, /showNotification\(/);
});

test("the service worker looks for uncontrolled windows when focusing a tab", () => {
  // Without includeUncontrolled the worker sees NOTHING on its first activation -- every tab the
  // person already has open predates it -- so the first click would open a duplicate tab.
  assert.match(serviceWorkerCode, /includeUncontrolled:\s*true/);
});

test("the service worker imports no external script", () => {
  // importScripts of a CDN bundle inside a worker is a third-party dependency on the delivery
  // path and a second copy of the Firebase config to keep in sync.
  assert.ok(!/importScripts/.test(serviceWorkerCode), "the service worker must not importScripts");
});

/**
 * The shared-office-desktop recovery, at the layer that owns the id.
 *
 * The install id is per-BROWSER-PROFILE and survives sign-out, so on a shared desktop the stored
 * id can already carry a colleague's live registration; the backend refuses to re-point that row
 * (409 browser_push_install_conflict) because the colleague's notification body would then land on
 * this screen. Minting a fresh id is how this person gets their OWN registration for the same
 * browser instead of silently receiving nothing -- so the reset must actually REPLACE the stored
 * value, or the retry would present the same conflicting id again.
 */
function withFakeLocalStorage(initial) {
  const store = new Map(Object.entries(initial ?? {}));
  const previous = globalThis.window;
  globalThis.window = {
    localStorage: {
      getItem: (key) => (store.has(key) ? store.get(key) : null),
      setItem: (key, value) => void store.set(key, String(value)),
    },
  };
  return {
    store,
    restore: () => {
      if (previous === undefined) delete globalThis.window;
      else globalThis.window = previous;
    },
  };
}

const INSTALL_ID_KEY = "mesha.web-push.browser-install-id";

test("resetBrowserInstallId replaces the stored id so the retry cannot reuse the conflicting one", () => {
  const fake = withFakeLocalStorage({ [INSTALL_ID_KEY]: "web-colleague-owned" });
  try {
    assert.equal(getBrowserInstallId(), "web-colleague-owned");
    const fresh = resetBrowserInstallId();
    assert.notEqual(fresh, "web-colleague-owned");
    assert.match(fresh, /^web-/);
    // Persisted, not just returned: the next dashboard load must keep registering as this person.
    assert.equal(fake.store.get(INSTALL_ID_KEY), fresh);
    assert.equal(getBrowserInstallId(), fresh);
  } finally {
    fake.restore();
  }
});

test("a fresh install id is opaque, and two of them never collide", () => {
  const fake = withFakeLocalStorage({});
  try {
    const first = resetBrowserInstallId();
    const second = resetBrowserInstallId();
    assert.notEqual(first, second);
    // It identifies a browser, never a person: nothing in it is derived from the session.
    assert.match(first, /^web-[0-9a-z-]+$/i);
    assert.notEqual(mintInstallId(), mintInstallId());
  } finally {
    fake.restore();
  }
});

test("the register path retries a browser-profile conflict exactly once, on the backend CODE", () => {
  const source = readFileSync(join(repoAdminWeb, "lib/web-push.ts"), "utf8");
  const code = source
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/^[ \t]*\/\/.*$/gm, "");
  // Keyed on the machine-readable code, never on the person-facing sentence, which is
  // backend-owned copy and may be reworded.
  assert.ok(code.includes('result.code !== "browser_push_install_conflict"'));
  assert.ok(code.includes("resetBrowserInstallId()"));
  // Exactly two register calls on the conflict path -- the original and one retry. A loop here
  // would hammer the endpoint and hide a genuine fault behind an id churn.
  assert.equal((code.match(/(?<!un)registerBrowserPush\(/g) ?? []).length, 2);
});

/**
 * THE BOUNDED TOKEN MINT.
 *
 * The defect: `getToken()` called cold never resolved and never rejected -- so the CEO accepted
 * Chrome's permission prompt and then watched a spinner forever, with no error and no way to retry.
 * These stub a never-resolving `getToken()` rather than fighting Chrome's GCM backoff, which is
 * both the reliable proof and the only one that can run in CI.
 *
 * The bar these hold to is the same one the dismissal tests above hold to: DO NOT TELL THE PERSON
 * SOMETHING FALSE. A dismissal must not be reported as a refusal, and a timeout must not be either
 * -- they granted permission, so `blocked` would send them into site settings to undo a decision
 * they never made, and `unconfigured` would blame a missing VAPID key that clicking cannot fix.
 */

/** The production shape of the mint, with `getToken` injected so a test can stall it. */
async function mintWithTimeout(getToken, timeoutMs = WEB_PUSH_TOKEN_TIMEOUT_MS) {
  const TIMED_OUT = Symbol("timed-out");
  const token = await withTimeout(getToken(), timeoutMs, () => TIMED_OUT);
  if (token === TIMED_OUT) return { status: "timed_out" };
  if (!token) throw new Error("This browser did not return a notification token.");
  return { status: "enabled", browserInstallId: "web-abc" };
}

test("a getToken that never settles produces the RETRYABLE timeout state, not an endless pending", () => {
  // The exact reproduction, with the hang made deterministic: a promise nobody ever settles.
  return (async () => {
    const neverSettles = () => new Promise(() => {});
    const state = await mintWithTimeout(neverSettles, 10);
    assert.deepEqual(state, { status: "timed_out" });
  })();
});

test("a timeout is NOT reported as blocked and NOT as unconfigured", async () => {
  const state = await mintWithTimeout(() => new Promise(() => {}), 10);
  // blocked means the person REFUSED -- it sends them to browser site settings. They granted.
  assert.notEqual(state.status, "blocked");
  // unconfigured means OUR missing VAPID key -- it offers no control at all. We had the key.
  assert.notEqual(state.status, "unconfigured");
  // dismissed means the prompt was closed unanswered. It was answered.
  assert.notEqual(state.status, "dismissed");
  assert.equal(state.status, "timed_out");
});

test("a retry after a timeout can still succeed, so the timeout is a dead end for nobody", async () => {
  // First click stalls; the second returns a token. The timeout must leave NO state behind that
  // poisons the retry -- otherwise the person is stuck exactly as they were with the spinner.
  let attempt = 0;
  const flakyGetToken = () => {
    attempt += 1;
    if (attempt === 1) return new Promise(() => {});
    return Promise.resolve("fcm-token-142-chars");
  };

  const first = await mintWithTimeout(flakyGetToken, 10);
  assert.equal(first.status, "timed_out");

  const second = await mintWithTimeout(flakyGetToken, 10);
  assert.equal(second.status, "enabled");
  assert.equal(attempt, 2);
});

test("the timeout does NOT fire when the token arrives promptly", async () => {
  // The healthy path returns in well under a second. A deadline that fired anyway would break the
  // feature in the name of fixing it.
  const started = Date.now();
  const state = await mintWithTimeout(() => Promise.resolve("fcm-token-142-chars"), 5_000);
  assert.equal(state.status, "enabled");
  assert.ok(Date.now() - started < 1_000, "a resolved mint must not wait out the deadline");
});

test("withTimeout returns the work's own value and rejects with the work's own error", async () => {
  assert.equal(await withTimeout(Promise.resolve("token"), 5_000, () => "timeout"), "token");
  // A real, describable failure must surface AS ITSELF. Swallowing it into a generic timeout would
  // hide the one case where we can actually tell the person what went wrong.
  await assert.rejects(
    withTimeout(Promise.reject(new Error("messaging/token-subscribe-failed")), 5_000, () => "timeout"),
    /token-subscribe-failed/,
  );
});

test("withTimeout clears its timer, so a settled mint leaves no pending work behind", async () => {
  // Proof by behaviour rather than by inspection: node --test hangs at exit on a live timer, so a
  // long deadline that was not cleared would stall this file for its full duration.
  await withTimeout(Promise.resolve("token"), 60_000, () => "timeout");
  await assert.rejects(withTimeout(Promise.reject(new Error("boom")), 60_000, () => "timeout"), /boom/);
});

test("the token budget sits ABOVE the SDK's own 10s activation wait", () => {
  // The SDK bounds its wait for the worker to activate at DEFAULT_REGISTRATION_TIMEOUT = 10_000 and
  // then rejects with a real message. Ours must be comfortably larger, or our stopwatch fires first
  // and flattens a failure the SDK could describe into a generic timeout.
  assert.ok(
    WEB_PUSH_TOKEN_TIMEOUT_MS > 10_000,
    "the mint budget must exceed the SDK's 10s activation wait so SDK errors surface as themselves",
  );
  // And it must stay within human patience -- this is a ceiling on a hang, not a latency target.
  assert.ok(WEB_PUSH_TOKEN_TIMEOUT_MS <= 30_000);
});

/**
 * The ROOT CAUSE, asserted on the source.
 *
 * The SDK's `updateSwReg()` calls `waitForRegistrationActive()` ONLY when it registers the worker
 * itself. Passing `serviceWorkerRegistration` -- which mintToken does on purpose, so two
 * registrations are not racing for one subscription -- takes the branch that just assigns it, so
 * the explicit-registration path opts OUT of the SDK's only activation safeguard and
 * `pushManager.subscribe()` runs against a worker that is not started yet. Awaiting activation
 * ourselves is what makes the cold path reliable; the timeout above is the belt, not the fix.
 */
test("the mint waits for its OWN registration's worker to activate before calling getToken", () => {
  const source = readFileSync(join(repoAdminWeb, "lib/web-push.ts"), "utf8");
  const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^[ \t]*\/\/.*$/gm, "");

  // The registration is still passed explicitly -- the fix must not be "let the SDK register it".
  assert.ok(code.includes("serviceWorkerRegistration: registration"));
  // ...and it is awaited to 'activated' rather than used straight out of register().
  assert.ok(code.includes("waitForActiveWorker("), "ensureServiceWorker must await worker activation");
  assert.match(code, /state === "activated"/);
  // navigator.serviceWorker.ready answers for the worker CONTROLLING THE PAGE, which on a first
  // visit does not exist and on an older page is the previous worker -- so it can resolve while the
  // registration we hand the SDK is still installing, or never resolve at all.
  assert.ok(
    !/serviceWorker\.ready/.test(code),
    "must wait on our own registration's worker, not on navigator.serviceWorker.ready",
  );
  // The getToken await is bounded. An unbounded one is the defect itself.
  // Allows the type argument the sentinel forces (`withTimeout<string | typeof MINT_TIMED_OUT>`).
  assert.match(code, /withTimeout\s*(?:<[^>]*>)?\s*\(/, "the getToken await must be bounded");
  assert.ok(code.includes("WEB_PUSH_TOKEN_TIMEOUT_MS"));
});


/**
 * THE THREE-STATE VAPID RULE.
 *
 * Held to the same bar as the timeout tests above: DO NOT TELL THE PERSON SOMETHING FALSE. Two
 * different falsehoods are possible here and they point in opposite directions, which is why an
 * absent key and a malformed one must NOT resolve the same way.
 *
 *   ABSENT  -> saying "not configured for this environment" is false. The Firebase JS SDK carries
 *              its own default VAPID key pair and `getToken()` uses it when none is supplied; FCM
 *              holds the matching private key. Proven end to end in real Chrome on this branch: a
 *              real 142-char token minted with the variable UNSET, registered through the real
 *              backend endpoint, and a real push rendered by the real service worker. Telling a
 *              CEO the feature does not exist here withholds a channel that works.
 *
 *   MALFORMED -> silently using the default instead would be the OTHER falsehood, and the worse
 *              one: somebody set a key deliberately, and a token minted against a key FCM accepts
 *              but can never deliver to reports "enabled" and delivers nothing. Push that lies
 *              about being on is worse than push that is honestly off.
 */

// A real uncompressed P-256 point: 0x04 || X(32) || Y(32), base64url, 87 chars. Generated with
// crypto.generateKeyPairSync("ec", { namedCurve: "prime256v1" }) -- the exact shape the Firebase
// console's "Web Push certificate" field hands you.
const REAL_SHAPED_VAPID_KEY =
  "BCOy_XY8z8PxXc-E6gvmOx2Mcv2Cm4UPkm_os9ddFPCrgE4WjgZ9AXRb0qtwVHGYzXA5z89mH64NhMe4P47rOL0";

test("an ABSENT key means USE THE SDK DEFAULT, and the control is offered rather than unconfigured", () => {
  for (const absent of [undefined, null, "", "   ", "\n\t "]) {
    const resolved = resolveVapidKeyConfig(absent);
    assert.equal(resolved.ok, true, `absent key ${JSON.stringify(absent)} must not fail`);
    assert.deepEqual(resolved.key, { source: "sdk-default" });
  }
  // The consequence that matters: a supported browser that has never been asked reads as
  // ACTIONABLE. It must not read `unconfigured`, which renders a sentence and no button.
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: true,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(state.status, "prompt");
});

test("the SDK-default case genuinely OMITS vapidKey -- the property is absent, not empty", () => {
  const options = vapidGetTokenOptions({ source: "sdk-default" });
  // The load-bearing assertion. `{ vapidKey: "" }` and `{ vapidKey: undefined }` both happen to
  // reach the SDK default today (`updateVapidKey` branches on `!!vapidKey`), so this is not about
  // correctness of delivery -- it is about not encoding "we supplied an empty key on purpose".
  assert.equal("vapidKey" in options, false, "vapidKey must not be a key of the options object");
  assert.deepEqual(Object.keys(options), []);
  // And spread into the real call shape it still contributes nothing.
  const call = { ...options, serviceWorkerRegistration: "sw" };
  assert.deepEqual(Object.keys(call).sort(), ["serviceWorkerRegistration"]);
});

test("a PRESENT key is used VERBATIM and reaches getToken as vapidKey", () => {
  const resolved = resolveVapidKeyConfig(`  ${REAL_SHAPED_VAPID_KEY}  `);
  assert.equal(resolved.ok, true);
  // Trimmed (surrounding whitespace in an env var is an operator typo, not part of the key) but
  // otherwise untouched -- a key is an exact byte string and must never be normalised.
  assert.deepEqual(resolved.key, { source: "project", key: REAL_SHAPED_VAPID_KEY });
  assert.deepEqual(vapidGetTokenOptions(resolved.key), { vapidKey: REAL_SHAPED_VAPID_KEY });
});

test("a MALFORMED key FAILS LOUDLY and never falls back to the SDK default", () => {
  const malformed = {
    "too short": "BCOy_XY8",
    "too long": `${REAL_SHAPED_VAPID_KEY}AA`,
    "standard base64 padding, not base64url": `${REAL_SHAPED_VAPID_KEY.slice(0, 85)}+/=`,
    "right length, wrong point prefix": `A${REAL_SHAPED_VAPID_KEY.slice(1)}`,
    "a placeholder somebody left in Terraform": "CHANGEME",
    "the whole console line pasted in": `vapidKey=${REAL_SHAPED_VAPID_KEY}`,
  };
  for (const [why, key] of Object.entries(malformed)) {
    const resolved = resolveVapidKeyConfig(key);
    assert.equal(resolved.ok, false, `${why} must be refused`);
    assert.equal(resolved.reason, VAPID_KEY_UNUSABLE_MESSAGE);
    // The trap being closed: a falsy `key` would have been read as "absent" one layer up and
    // quietly taken the default, minting a token FCM accepts and can never deliver to.
    assert.equal("key" in resolved, false, `${why} must not yield a usable key`);
  }
});

test("the shape check accepts the SDK's OWN default key, so the rule cannot be self-contradictory", () => {
  // DEFAULT_VAPID_KEY from @firebase/messaging. If our validator rejected the very key the SDK
  // falls back to, the two halves of this feature would disagree about what a VAPID key looks
  // like -- and a project key copied from the same console would be refused for the same reason.
  const sdkDefault = "BDOU99-h67HcA6JeFXHbSNMu7e2yNNu3RzoMj8TM4W88jITfq7ZmPvIM1Iv-4_l2LxQcYwhqby2xGpWwzjfAnG4";
  assert.equal(isWellFormedVapidPublicKey(sdkDefault), true);
  assert.equal(isWellFormedVapidPublicKey(REAL_SHAPED_VAPID_KEY), true);
});

test("the server adapter reads the env var through the ONE pure rule, and never re-implements it", () => {
  // web-push.test.mjs cannot import a "server-only" module, so this reads the source: the point
  // is that there is exactly one place the three states are decided, so the client's state
  // machine and the server's read cannot drift about which of them happened.
  const source = readFileSync(join(repoAdminWeb, "lib/api/browser-push-server.ts"), "utf8");
  const code = source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^[ \t]*\/\/.*$/gm, "");
  assert.match(code, /resolveVapidKeyConfig\(process\.env\.GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY\)/);
  // The retired behaviour: an absent key returning ok:false with "not configured". If this
  // reappears the feature is gated on the Firebase-console step again.
  assert.ok(
    !/Browser notifications are not configured for this environment yet/.test(code),
    "an absent key must no longer report the feature as unconfigured",
  );
});
