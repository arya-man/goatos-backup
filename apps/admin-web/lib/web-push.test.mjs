import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  describeBrowser,
  leadershipTaskDeepLink,
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
