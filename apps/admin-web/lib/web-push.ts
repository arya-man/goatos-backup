"use client";

import { getApp, getApps, initializeApp } from "firebase/app";
import { getMessaging, getToken, deleteToken, isSupported } from "firebase/messaging";
import { getFirebaseClientRuntimeConfig } from "@/lib/auth/firebase-client";
import {
  describeBrowser,
  detectWebPushSupport,
  getBrowserInstallId,
  resetBrowserInstallId,
  type WebPushState,
} from "@/lib/web-push-state";
import {
  getWebPushVapidKey,
  registerBrowserPush,
  unregisterBrowserPush,
} from "@/lib/web-push-actions";

/**
 * Browser (Chrome) web push for admin-web.
 *
 * WHY FCM WEB AND NOT RAW VAPID WEB PUSH. admin-web already ships the Firebase JS SDK for auth,
 * so `firebase/messaging` adds no dependency. getToken() returns ONE opaque FCM registration
 * token -- the identical shape the backend's existing FCM HTTP v1 send path already addresses on
 * the `push_fcm` channel, and the identical shape the Android device registry already stores. A
 * raw `PushManager.subscribe()` would instead hand us {endpoint, keys.p256dh, keys.auth}, which
 * fits no existing column and would oblige the backend to implement VAPID signing and aes128gcm
 * payload encryption itself. FCM does both for us. Fuller reasoning: migration 000347.
 *
 * THE PERMISSION PROMPT IS NEVER FIRED ON LOAD. Nothing here calls
 * Notification.requestPermission() except `enableWebPush`, which exists to be called from a click
 * handler. Chrome penalises a site that prompts on page load (the request is auto-denied under its
 * abusive-permission-request protections and the quieter UI is applied to the origin), and it is a
 * dark pattern regardless of what Chrome does about it. `refreshWebPushRegistration` is the
 * on-load half and is deliberately silent: it re-registers only a browser that has ALREADY
 * granted permission.
 *
 * The pure half of this module (feature detection, the permission state machine, the install id,
 * the deep link) lives in web-push-state.ts so it is unit-testable, and is re-exported here.
 */

export {
  describeBrowser,
  detectWebPushSupport,
  getBrowserInstallId,
  resetBrowserInstallId,
  leadershipTaskDeepLink,
  readNotificationPermission,
  resolveWebPushState,
} from "@/lib/web-push-state";
export type { WebPushState } from "@/lib/web-push-state";

const FIREBASE_APP_NAME = "goatos-admin-web";
const SERVICE_WORKER_PATH = "/firebase-messaging-sw.js";

/** Register (or reuse) the push service worker. */
async function ensureServiceWorker(): Promise<ServiceWorkerRegistration> {
  const existing = await navigator.serviceWorker.getRegistration(SERVICE_WORKER_PATH);
  if (existing) return existing;
  return navigator.serviceWorker.register(SERVICE_WORKER_PATH, { scope: "/" });
}

/** Reuse the auth app rather than initialising a second Firebase app for messaging. */
async function messagingApp() {
  const { config } = await getFirebaseClientRuntimeConfig();
  return getApps().some((candidate) => candidate.name === FIREBASE_APP_NAME)
    ? getApp(FIREBASE_APP_NAME)
    : initializeApp(config, FIREBASE_APP_NAME);
}

/**
 * Mint an FCM web registration token for this browser.
 *
 * The service worker registration is passed EXPLICITLY. Without it the SDK registers
 * `/firebase-messaging-sw.js` itself, with its own scope and options, which would mean two
 * registrations racing for one subscription.
 */
async function mintToken(vapidKey: string): Promise<string> {
  const app = await messagingApp();
  const registration = await ensureServiceWorker();
  const token = await getToken(getMessaging(app), {
    vapidKey,
    serviceWorkerRegistration: registration,
  });
  if (!token) throw new Error("This browser did not return a notification token.");
  return token;
}

/**
 * Mint a token and hand it to the backend. Shared by the click path and the silent refresh.
 *
 * THE SHARED-DESKTOP RETRY. The browser install id lives in this Chrome profile's localStorage and
 * SURVIVES SIGN-OUT, so on a shared office desktop the id this session presents may already carry
 * a COLLEAGUE's live registration. The backend refuses that with 409
 * `browser_push_install_conflict` -- it will not re-point another member's row at this session,
 * because the notification body (a leadership-task mention carries the task title and a note
 * excerpt) would then be delivered to whoever is sitting here. The answer is not to fail: it is to
 * take a fresh id for ourselves and register again, so this person receives their OWN
 * notifications on this desktop. The colleague's registration is untouched.
 *
 * Retried ONCE and never in a loop: a freshly minted id cannot collide, so a second conflict would
 * mean something else is wrong and retrying would only hide it.
 */
async function mintAndRegister(vapidKey: string, browserInstallId: string): Promise<WebPushState> {
  const token = await mintToken(vapidKey);
  const browserLabel = describeBrowser(navigator.userAgent);
  const result = await registerBrowserPush({ browserInstallId, token, browserLabel });
  if (result.ok) return { status: "enabled", browserInstallId };
  if (result.code !== "browser_push_install_conflict") {
    return { status: "error", reason: result.error };
  }
  const ownInstallId = resetBrowserInstallId();
  const retried = await registerBrowserPush({ browserInstallId: ownInstallId, token, browserLabel });
  if (!retried.ok) return { status: "error", reason: retried.error };
  return { status: "enabled", browserInstallId: ownInstallId };
}

/**
 * The explicit user action. CALL THIS FROM A CLICK HANDLER AND NOWHERE ELSE.
 *
 * Chrome requires a user gesture for the prompt to be shown at all in its quieter UI, and treats
 * a load-time request as abusive. It is also simply the right thing: a person who has not asked
 * for notifications should not be interrupted by a browser-level modal.
 */
export async function enableWebPush(): Promise<WebPushState> {
  const support = detectWebPushSupport();
  if (!support.supported) return { status: "unsupported", reason: support.reason };

  const supportedByFcm = await isSupported().catch(() => false);
  if (!supportedByFcm) {
    return { status: "unsupported", reason: "This browser cannot receive background notifications." };
  }

  const vapid = await getWebPushVapidKey();
  if (!vapid.ok) return { status: "unconfigured", reason: vapid.error };

  // Already denied: requestPermission() resolves 'denied' immediately without showing anything,
  // so calling it would look to the person like the button did nothing at all.
  if (Notification.permission === "denied") return { status: "blocked" };

  let permission: NotificationPermission = Notification.permission;
  if (permission !== "granted") {
    permission = await Notification.requestPermission();
  }
  if (permission === "denied") return { status: "blocked" };
  if (permission !== "granted") {
    // Closed without choosing. Still 'default', so it is retryable and must NOT be reported as a
    // refusal -- telling someone they blocked notifications when they merely dismissed a dialog
    // sends them into browser settings for nothing.
    return { status: "dismissed" };
  }

  try {
    return await mintAndRegister(vapid.data, getBrowserInstallId());
  } catch (error) {
    return { status: "error", reason: error instanceof Error ? error.message : "Could not turn on notifications." };
  }
}

/**
 * The on-load half, and it is SILENT: it never prompts, and does nothing at all unless permission
 * is ALREADY 'granted'.
 *
 * It exists because a granted subscription is not permanent. Chrome rotates an FCM web token on
 * its own schedule and invalidates the subscription outright on profile clear or a long idle
 * stretch, telling the server nothing. Re-reading the token on each dashboard load and upserting
 * it is what keeps a browser reachable; the backend's upsert is keyed on the browser, so a
 * same-token call is a cheap last_seen_at touch and a new-token call is the refresh.
 *
 * It also handles the reverse: permission revoked in site settings while a registration is still
 * stored. Chrome reports 'denied', we unregister, and the backend stops addressing a browser that
 * would silently drop everything -- the web equivalent of the phone's notifications_enabled=false
 * (migration 000066), which a browser gives the server no signal for.
 */
export async function refreshWebPushRegistration(): Promise<WebPushState> {
  const support = detectWebPushSupport();
  if (!support.supported) return { status: "unsupported", reason: support.reason };

  if (Notification.permission === "denied") {
    // Best effort, and deliberately not surfaced: the person did not ask for a result here.
    await unregisterBrowserPush({ browserInstallId: getBrowserInstallId() }).catch(() => undefined);
    return { status: "blocked" };
  }
  if (Notification.permission !== "granted") return { status: "prompt" };

  const supportedByFcm = await isSupported().catch(() => false);
  if (!supportedByFcm) {
    return { status: "unsupported", reason: "This browser cannot receive background notifications." };
  }
  const vapid = await getWebPushVapidKey();
  if (!vapid.ok) return { status: "unconfigured", reason: vapid.error };

  try {
    return await mintAndRegister(vapid.data, getBrowserInstallId());
  } catch (error) {
    return { status: "error", reason: error instanceof Error ? error.message : "Could not refresh notifications." };
  }
}

/**
 * Turn notifications off for this browser.
 *
 * BOTH halves, in this order: delete the FCM token so the browser's own subscription is released
 * (otherwise Chrome keeps waking a worker that has nothing to show), then tell the backend to stop
 * addressing it. The backend call runs even if the token delete throws -- an unregister that
 * leaves a live row behind is the failure that matters, because that row keeps receiving.
 */
export async function disableWebPush(): Promise<WebPushState> {
  const browserInstallId = getBrowserInstallId();
  try {
    await deleteToken(getMessaging(await messagingApp()));
  } catch {
    // Keep going: the server-side unregister is the part that stops delivery.
  }
  const result = await unregisterBrowserPush({ browserInstallId });
  if (!result.ok) return { status: "error", reason: result.error };
  return { status: "prompt" };
}

/**
 * The service worker asks the page to route itself when it focused a tab it could not navigate
 * (see focusOrOpen in public/firebase-messaging-sw.js). Returns an unsubscribe function.
 */
export function listenForPushNavigation(navigate: (link: string) => void): () => void {
  if (typeof window === "undefined" || !("serviceWorker" in navigator)) return () => undefined;
  const handler = (event: MessageEvent) => {
    const data = event.data as { type?: string; link?: string } | null;
    if (!data || data.type !== "mesha-push-navigate") return;
    if (typeof data.link !== "string" || !data.link.startsWith("/")) return;
    navigate(data.link);
  };
  navigator.serviceWorker.addEventListener("message", handler);
  return () => navigator.serviceWorker.removeEventListener("message", handler);
}
