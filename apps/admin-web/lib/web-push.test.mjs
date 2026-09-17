import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  describeBrowser,
  getBrowserInstallId,
  leadershipTaskDeepLink,
  mintInstallId,
  resetBrowserInstallId,
  resolveWebPushState,
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

test("a missing VAPID key is OUR gap and is reported as unconfigured, not as blocked", () => {
  const state = resolveWebPushState({
    supported: true,
    supportReason: "",
    configured: false,
    permission: "default",
    hasRegistration: false,
    browserInstallId: "",
  });
  assert.equal(state.status, "unconfigured");
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
