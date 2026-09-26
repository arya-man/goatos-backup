"use client";

import { getApp, getApps, initializeApp } from "firebase/app";
import { getMessaging, getToken, deleteToken, isSupported } from "firebase/messaging";
import { getFirebaseClientRuntimeConfig } from "@/lib/auth/firebase-client";
import {
  describeBrowser,
  detectWebPushSupport,
  getBrowserInstallId,
  resetBrowserInstallId,
  withTimeout,
  vapidGetTokenOptions,
  WEB_PUSH_TOKEN_TIMEOUT_MS,
  type WebPushState,
  type WebPushVapidKey,
} from "@/lib/web-push-state";
import {
  getWebPushVapidKey,
  recordBrowserPushEvent,
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
 * payload encryption itself. FCM does both for us. Fuller reasoning: migration 000353.
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
  WEB_PUSH_TOKEN_TIMEOUT_MS,
  resolveVapidKeyConfig,
  isWellFormedVapidPublicKey,
  vapidGetTokenOptions,
  VAPID_KEY_UNUSABLE_MESSAGE,
} from "@/lib/web-push-state";
export type { WebPushState, WebPushVapidKey } from "@/lib/web-push-state";

const FIREBASE_APP_NAME = "goatos-admin-web";
const SERVICE_WORKER_PATH = "/firebase-messaging-sw.js";
/**
 * How long to wait for our own worker to activate before giving up on waiting and letting the SDK
 * try anyway. Matches the SDK's own `DEFAULT_REGISTRATION_TIMEOUT`, so the two halves agree about
 * how long activation is allowed to take, and sits inside the outer mint budget.
 */
const SERVICE_WORKER_ACTIVATION_TIMEOUT_MS = 10_000;

/** Sentinel for a mint that ran out of time. Never a token, so it cannot be mistaken for one. */
const MINT_TIMED_OUT = Symbol("web-push-mint-timed-out");

/**
 * Register (or reuse) the push service worker, AND WAIT FOR IT TO BE ACTIVE.
 *
 * THE WAIT IS THE BUG FIX, and it is here rather than in the SDK because of how the SDK is built.
 * `navigator.serviceWorker.register()` resolves as soon as the REGISTRATION OBJECT exists -- on a
 * cold profile its worker is still `installing` and `registration.active` is null. The Firebase SDK
 * knows this: `waitForRegistrationActive()` carries a comment saying MDN's claim that a registration
 * is ready after `register()` "doesn't seem to be the case in practice, causing the SDK to throw
 * errors when calling swRegistration.pushManager.subscribe() too soon after register()".
 *
 * But it only runs that wait on the path where the SDK registers the worker ITSELF
 * (`registerDefaultSw`). `updateSwReg()` -- the path taken when a caller PASSES
 * `serviceWorkerRegistration`, which we do, deliberately, so two registrations are not racing for
 * one subscription -- simply assigns the registration and returns. So the explicit-registration
 * path silently opts OUT of the one safeguard the SDK has, and `getToken()` goes straight to
 * `pushManager.subscribe()` on a worker that is not running yet.
 *
 * That is exactly what was observed: a cold `getToken()` never resolved and never rejected, while
 * the one run that called `pushManager.subscribe()` first -- which forces activation -- got a token
 * in under a second. Passing the registration is still right; the missing half was awaiting it.
 *
 * `navigator.serviceWorker.ready` is NOT the check. It resolves for the registration that controls
 * THIS PAGE, which on a first visit is a worker that does not exist yet, and on a page loaded before
 * this worker shipped is the OLD one -- so it can resolve while the registration we are about to
 * hand to the SDK is still installing, or hang forever when nothing controls the page. Waiting on
 * OUR OWN registration's own worker is the thing that has to be true.
 */
async function ensureServiceWorker(): Promise<ServiceWorkerRegistration> {
  const existing = await navigator.serviceWorker.getRegistration(SERVICE_WORKER_PATH);
  // serial-await: allow registering depends on whether a registration already exists, and the
  // activation wait depends on the registration we end up with. This ORDER is the whole fix --
  // the SDK only awaits activation on the branch where it registers the worker itself, so handing
  // it our own registration skipped that wait and getToken() hung forever on a cold profile.
  const registration = existing ?? (await navigator.serviceWorker.register(SERVICE_WORKER_PATH, { scope: "/" }));
  // serial-await: allow the activation wait depends on the registration resolved above.
  await waitForActiveWorker(registration);
  return registration;
}

/**
 * Resolve once this registration has a RUNNING worker.
 *
 * Deliberately RESOLVES rather than rejects when it cannot tell (no active and no incoming worker,
 * or the state change never arrives): the SDK still applies its own 10s activation wait on top, and
 * the outer timeout still bounds the whole mint. Throwing our own error here would replace a real,
 * describable SDK failure with a vaguer one of ours. What must not happen is waiting FOREVER, and
 * the timer guarantees that.
 */
function waitForActiveWorker(registration: ServiceWorkerRegistration): Promise<void> {
  if (registration.active) return Promise.resolve();
  const pending = registration.installing ?? registration.waiting;
  if (!pending) return Promise.resolve();
  const incoming: ServiceWorker = pending;
  return new Promise<void>((resolve) => {
    const timer = setTimeout(finish, SERVICE_WORKER_ACTIVATION_TIMEOUT_MS);
    function finish() {
      clearTimeout(timer);
      incoming.removeEventListener("statechange", onStateChange);
      resolve();
    }
    function onStateChange() {
      // 'redundant' is a dead end (the worker was replaced or failed), so stop waiting on it and
      // let the SDK and the outer timeout speak; only 'activated' means it is running.
      if (incoming.state === "activated" || incoming.state === "redundant") finish();
    }
    incoming.addEventListener("statechange", onStateChange);
    // The worker may have activated between the checks above and this listener being attached.
    if (registration.active) finish();
  });
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
 *
 * THE VAPID KEY MAY BE GENUINELY ABSENT from the options, and that is a supported configuration,
 * not a degraded one: `vapidGetTokenOptions` spreads in a `vapidKey` property only for a project
 * key, so the SDK-default case passes an options object that HAS NO `vapidKey` KEY AT ALL -- not
 * `""`, not an explicit `undefined`. The SDK then takes its own `DEFAULT_VAPID_KEY`, whose private
 * half FCM holds, and the token delivers. See resolveVapidKeyConfig for the three-state rule.
 */
async function mintToken(vapidKey: WebPushVapidKey): Promise<string | typeof MINT_TIMED_OUT> {
  // These two are independent -- the Firebase app does not need the registration and vice versa --
  // so they are started together. getToken() below needs BOTH, which is what makes it serial.
  const [app, registration] = await Promise.all([messagingApp(), ensureServiceWorker()]);
  // BOUNDED, AS A BELT. `ensureServiceWorker` above removes the known cause of the hang, but
  // `getToken()` still reaches Chrome's GCM registration, which can enter a silent multi-minute
  // backoff for environmental reasons no code here can see or fix. An unbounded await on it means
  // one bad day turns the CEO's click into a spinner with no error and no way to retry. A deadline
  // converts "unknown" into "visible and retryable", which is the only outcome we can promise.
  // serial-await: allow getToken() consumes both the app and the registration resolved above.
  const token = await withTimeout<string | typeof MINT_TIMED_OUT>(
    getToken(getMessaging(app), { ...vapidGetTokenOptions(vapidKey), serviceWorkerRegistration: registration }),
    WEB_PUSH_TOKEN_TIMEOUT_MS,
    () => MINT_TIMED_OUT,
  );
  if (token === MINT_TIMED_OUT) return MINT_TIMED_OUT;
  if (!token) throw new Error("This browser could not be registered for notifications. Try again or use another browser.");
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
async function mintAndRegister(vapidKey: WebPushVapidKey, browserInstallId: string): Promise<WebPushState> {
  const token = await mintToken(vapidKey);
  // Ran out of time. Nothing was sent to the backend, so there is nothing to undo and nothing to
  // report as broken -- the honest state is the retryable one, and the next click starts clean.
  if (token === MINT_TIMED_OUT) return { status: "timed_out" };
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

  // Only a SET-BUT-UNUSABLE key fails here. An unset variable resolves to the SDK's own default
  // key and proceeds, so a CEO on an environment where nobody has visited the Firebase console
  // still gets a working Enable button rather than "not configured for this environment".
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

export async function recordCurrentBrowserPushEvent(input: {
  notificationRequestId: string;
  eventType: "displayed" | "opened";
  traceId?: string;
}): Promise<void> {
  const notificationRequestId = input.notificationRequestId.trim();
  if (!notificationRequestId) return;
  await recordBrowserPushEvent({
    notificationRequestId,
    eventType: input.eventType,
    traceId: input.traceId,
    browserInstallId: getBrowserInstallId(),
  }).catch(() => undefined);
}

export function listenForPushReceipts(): () => void {
  if (typeof window === "undefined" || !("serviceWorker" in navigator)) return () => undefined;
  const handler = (event: MessageEvent) => {
    const data = event.data as {
      type?: string;
      eventType?: "displayed" | "opened";
      notificationRequestId?: string;
      traceId?: string;
    } | null;
    if (!data || data.type !== "mesha-push-receipt") return;
    if (data.eventType !== "displayed" && data.eventType !== "opened") return;
    void recordCurrentBrowserPushEvent({
      notificationRequestId: data.notificationRequestId ?? "",
      eventType: data.eventType,
      traceId: data.traceId,
    });
  };
  navigator.serviceWorker.addEventListener("message", handler);
  return () => navigator.serviceWorker.removeEventListener("message", handler);
}
