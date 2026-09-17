/*
 * Mesha admin-web web push service worker.
 *
 * WHY IT IS NAMED firebase-messaging-sw.js AND STILL CONTAINS NO FIREBASE SDK.
 * The client registers this file explicitly and hands the registration to
 * getToken({ serviceWorkerRegistration }), so the name is not load-bearing for us. It is kept
 * because `/firebase-messaging-sw.js` is the path the Firebase JS SDK falls back to when no
 * registration is passed: if any future call site forgets the option, it finds a real worker here
 * instead of a 404 and a silently token-less browser.
 *
 * The SDK's own messaging-in-the-worker helpers are deliberately NOT used. FCM web delivers
 * through the standard Web Push transport, so the browser fires a plain `push` event in whichever
 * worker holds the subscription -- this one -- carrying the message JSON. Handling that directly
 * means no importScripts of a CDN bundle inside the worker, no second copy of the Firebase config
 * to keep in sync with /api/auth/firebase-config, and full control over what the notification says
 * and where a click lands.
 *
 * NO BUILD STEP TOUCHES THIS FILE. It is served verbatim from public/, so it must stay plain ES
 * that every Chrome we support parses on first read -- a syntax error here does not fail a build,
 * it silently stops every notification.
 */

"use strict";

// A push whose payload cannot be read at all still shows SOMETHING. Chrome's own fallback for a
// push event that displays nothing is the "This site has been updated in the background" notice,
// which reads to a CEO as a bug; and a worker that never calls showNotification enough times has
// its push permission revoked by the browser.
const FALLBACK_TITLE = "Mesha";
const FALLBACK_BODY = "You have a new notification.";

// The deep links this worker knows how to build, keyed on the notification_type the backend sends.
//
// IT DOES NOT TRUST data.href / data.target. Those exist and look exactly like what a click
// handler wants -- but they are the ANDROID routes (`/leadership-tasks/<id>`), composed for the
// phone by the same notification the browser is now reading. Opening one in Chrome lands on a
// route the dashboard does not have. The web route for the same fact is built here, from the ids
// in the payload, which is the only place that knows what the web IA looks like.
const DEEP_LINKS = {
  leadership_task_raised: (data) => leadershipTaskLink(data),
  leadership_task_done: (data) => leadershipTaskLink(data),
  leadership_task_status: (data) => leadershipTaskLink(data),
  leadership_task_mentioned: (data) => leadershipTaskLink(data),
  leadership_task_commented: (data) => leadershipTaskLink(data),
  leadership_task_updated: (data) => leadershipTaskLink(data),
};

// Every leadership-task notification opens the task itself on the team-progress scope, which is
// the tenant-wide list the task is guaranteed to appear in whoever is reading.
function leadershipTaskLink(data) {
  const taskId = typeof data.task_id === "string" ? data.task_id.trim() : "";
  if (!taskId) return "/tasks?scope=team_progress";
  return `/tasks?scope=team_progress&task=${encodeURIComponent(taskId)}`;
}

// A notification_type this worker has no link for still opens the dashboard rather than nothing:
// a click that does nothing is indistinguishable from a broken notification.
function resolveLink(data) {
  const type = typeof data.notification_type === "string" ? data.notification_type.trim() : "";
  const build = DEEP_LINKS[type];
  if (typeof build === "function") {
    try {
      const link = build(data);
      if (typeof link === "string" && link.startsWith("/")) return link;
    } catch {
      // Fall through to the dashboard.
    }
  }
  return "/";
}

// FCM hands the worker either {notification, data} (the shape the backend's gateway sends today)
// or a bare data object. Read both, and never let a malformed payload throw out of the handler --
// an exception here means no notification at all.
function readPayload(event) {
  let raw = {};
  try {
    raw = event.data ? event.data.json() : {};
  } catch {
    raw = {};
  }
  if (!raw || typeof raw !== "object") raw = {};
  const data = raw.data && typeof raw.data === "object" ? raw.data : raw;
  const notification = raw.notification && typeof raw.notification === "object" ? raw.notification : {};
  const title = pickString(notification.title, data.title) || FALLBACK_TITLE;
  const body = pickString(notification.body, data.body) || FALLBACK_BODY;
  return { data, title, body };
}

function pickString(...candidates) {
  for (const candidate of candidates) {
    if (typeof candidate === "string" && candidate.trim() !== "") return candidate.trim();
  }
  return "";
}

self.addEventListener("install", () => {
  // Take over immediately rather than waiting for every tab to close. A worker that only
  // activates on the next full browser restart means the person who just clicked "Enable
  // notifications" gets nothing today.
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  const { data, title, body } = readPayload(event);
  const link = resolveLink(data);
  // tag COLLAPSES repeat notifications about the same thing instead of stacking five of them: a
  // task that changes status three times should leave one entry in the tray, the latest. The
  // group_key the backend sends is per-feature, so the task id is appended where we have one --
  // collapsing two DIFFERENT tasks into one notification would hide work.
  const tag = pickString(data.task_id, data.calendar_event_id, data.group_key, data.notification_type) || "mesha";
  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      tag,
      // The tray entry is replaced silently only when it is genuinely the same subject; a new
      // subject re-alerts. renotify requires a tag, which is why it is set unconditionally above.
      renotify: true,
      icon: "/data/logo.png",
      badge: "/data/logo.png",
      data: { link, notificationType: data.notification_type || "", traceId: data.trace_id || "" },
    })
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const link = event.notification.data && typeof event.notification.data.link === "string" ? event.notification.data.link : "/";
  event.waitUntil(focusOrOpen(link));
});

/*
 * A CEO almost always already has the dashboard open, so the default must be to REUSE that tab,
 * not to pile up a new one per notification.
 *
 * Three cases, in order:
 *   1. A tab is already on the exact target -> focus it and stop. Re-navigating it would throw
 *      away scroll position and any drawer state for no gain.
 *   2. Some other Mesha tab is open -> focus it and navigate it to the target. navigate() can be
 *      unavailable (older Chrome, a client that is not a window), so its failure falls through to
 *      opening a window rather than leaving a focused tab on the wrong page.
 *   3. Nothing is open -> open a new window.
 *
 * includeUncontrolled: true is required for case 1 and 2 to see anything at all: tabs loaded
 * BEFORE this worker activated are not controlled by it, and on the very first registration that
 * is every tab the person has.
 */
async function focusOrOpen(link) {
  const target = new URL(link, self.location.origin);
  let clients = [];
  try {
    clients = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
  } catch {
    clients = [];
  }

  const sameOrigin = clients.filter((client) => {
    try {
      return new URL(client.url).origin === self.location.origin;
    } catch {
      return false;
    }
  });

  for (const client of sameOrigin) {
    try {
      const current = new URL(client.url);
      if (current.pathname === target.pathname && current.search === target.search) {
        await client.focus();
        return;
      }
    } catch {
      // Ignore an unparseable client url and keep looking.
    }
  }

  for (const client of sameOrigin) {
    try {
      const focused = await client.focus();
      const reusable = focused || client;
      if (typeof reusable.navigate === "function") {
        await reusable.navigate(target.href);
        return;
      }
      // Focused a tab but cannot steer it. Opening a window as well would leave the person
      // looking at the wrong page in the focused tab, so tell the page to route itself; the app
      // listens for this in lib/web-push.ts.
      reusable.postMessage({ type: "mesha-push-navigate", link: target.pathname + target.search });
      return;
    } catch {
      // Try the next client.
    }
  }

  if (self.clients.openWindow) {
    await self.clients.openWindow(target.href);
  }
}
