/**
 * Pure browser-push helpers: feature detection, the permission state machine, the browser install
 * id, the browser label and the deep link.
 *
 * SPLIT OUT FROM web-push.ts ON PURPOSE. Everything here is decidable without the Firebase SDK
 * and without a server action, which is what lets `node --test` exercise the ACTUAL state machine
 * rather than a re-implementation of it that could drift from what ships. web-push.ts re-exports
 * all of it, so callers still import from one place.
 */

const INSTALL_ID_STORAGE_KEY = "mesha.web-push.browser-install-id";

/**
 * Every state the control can be in, and every one of them is a real case a CEO hits:
 *
 *  unsupported   this browser has no Push API / Service Worker / Notification, or the origin is
 *                not secure (an http:// staging host has no web push at all). The control must
 *                say so, not sit there doing nothing on click.
 *  unconfigured  the environment's VAPID key is SET BUT UNUSABLE (see resolveVapidKeyConfig).
 *                Our gap, not the person's, and no amount of clicking fixes it. An ABSENT key is
 *                deliberately NOT this state: the Firebase JS SDK carries its own default VAPID
 *                key pair and mints a fully deliverable token without one, so "nobody has pasted
 *                a key from the Firebase console" must not read as "the feature does not exist".
 *  blocked       permission is 'denied'. THE PROMPT CANNOT BE SHOWN AGAIN from script — only the
 *                person can undo it, in the browser's own site settings. Say where.
 *  dismissed     the prompt was shown and closed without a choice, so permission is still
 *                'default'. Retryable: this is NOT a refusal, and must not be reported as one.
 *  prompt        permission is 'default', or granted-but-not-registered. Actionable.
 *  enabled       permission granted and a token is registered with the backend.
 *  timed_out     the browser accepted the permission but the token mint never came back. RETRYABLE
 *                and deliberately its OWN state, because the three neighbours all say something
 *                false about it: it is NOT `blocked` (the person granted permission -- sending them
 *                into site settings to undo a refusal they never made is the same defect as
 *                reporting a dismissal as a refusal), it is NOT `unconfigured` (that is OUR
 *                unusable VAPID key, which no amount of clicking fixes), and it is NOT `dismissed` (the
 *                prompt was answered). Nothing is known to be wrong, so the only honest thing to
 *                say is "that did not come back, try again" -- and the control must OFFER that
 *                retry. An infinite spinner is the one outcome that is worse than the feature not
 *                existing: it reports nothing and offers nothing.
 *  error         something failed (offline, token mint refused, backend rejected).
 */
export type WebPushState =
  | { status: "unsupported"; reason: string }
  | { status: "unconfigured"; reason: string }
  | { status: "blocked" }
  | { status: "dismissed" }
  | { status: "prompt" }
  | { status: "enabled"; browserInstallId: string }
  | { status: "timed_out" }
  | { status: "error"; reason: string };

/**
 * Feature detection, in the browser, with no side effects and no prompt.
 *
 * The SDK's own async `isSupported()` is checked separately by the caller and is NOT redundant
 * with these: it also rejects environments where the APIs exist but FCM cannot work (no
 * IndexedDB in a private window, for instance).
 */
export function detectWebPushSupport(): { supported: boolean; reason: string } {
  if (typeof window === "undefined") return { supported: false, reason: "Not running in a browser." };
  if (!("serviceWorker" in navigator)) {
    return { supported: false, reason: "This browser does not support background notifications." };
  }
  if (!("PushManager" in window)) {
    return { supported: false, reason: "This browser does not support web push." };
  }
  if (!("Notification" in window)) {
    return { supported: false, reason: "This browser does not support notifications." };
  }
  if (!window.isSecureContext) {
    return { supported: false, reason: "Notifications need a secure (https) connection." };
  }
  return { supported: true, reason: "" };
}

/** The current permission, normalised. Reading it never prompts. */
export function readNotificationPermission(): NotificationPermission | "unavailable" {
  if (typeof window === "undefined" || !("Notification" in window)) return "unavailable";
  return Notification.permission;
}

/**
 * Map a permission plus a stored-registration flag onto the control's state. Pure, so this is
 * where the decisions live and where the test points.
 */
export function resolveWebPushState(input: {
  supported: boolean;
  supportReason: string;
  configured: boolean;
  permission: NotificationPermission | "unavailable";
  hasRegistration: boolean;
  browserInstallId: string;
  promptWasDismissed?: boolean;
  /** Why the environment's key is unusable, when `configured` is false. */
  configReason?: string;
}): WebPushState {
  if (!input.supported) {
    return {
      status: "unsupported",
      reason: input.supportReason || "Notifications are not available in this browser.",
    };
  }
  // `configured` is NOT "someone pasted a VAPID key": an absent key resolves to the SDK's own
  // default key and is configured enough to deliver. It is false only when the environment set a
  // key that cannot be used, which is the one configuration gap a person cannot act on.
  if (!input.configured) {
    return { status: "unconfigured", reason: input.configReason || VAPID_KEY_UNUSABLE_MESSAGE };
  }
  if (input.permission === "denied") return { status: "blocked" };
  if (input.permission === "granted") {
    // Granted but NOT registered is a real and common state, not a contradiction: the person
    // granted permission before, then cleared site data, or Chrome rotated the token and the send
    // path pruned the old registration. It must read as actionable, never as enabled.
    return input.hasRegistration
      ? { status: "enabled", browserInstallId: input.browserInstallId }
      : { status: "prompt" };
  }
  // 'default' or 'unavailable': unanswered. A prompt shown and closed also leaves 'default', which
  // the API cannot distinguish from never-asked — hence the caller passing what it observed.
  if (input.promptWasDismissed) return { status: "dismissed" };
  return { status: "prompt" };
}

/**
 * This browser profile's stable id.
 *
 * It is the upsert key: it makes a re-register from the same Chrome profile refresh ONE row
 * instead of leaving a second live registration behind, and it is what lets the backend address
 * one browser rather than all of a person's browsers. It is a random opaque id and identifies no
 * person — the person comes from the session, server-side.
 *
 * localStorage is per-origin AND per-profile, which is exactly the grain wanted. Clearing site
 * data mints a new id; the orphaned registration dies with the profile's token and is pruned by
 * the send path. That is the designed lifecycle, not a leak.
 */
export function getBrowserInstallId(): string {
  if (typeof window === "undefined") return "";
  try {
    const existing = window.localStorage.getItem(INSTALL_ID_STORAGE_KEY);
    if (existing && existing.trim() !== "") return existing;
    const minted = mintInstallId();
    window.localStorage.setItem(INSTALL_ID_STORAGE_KEY, minted);
    return minted;
  } catch {
    // Storage blocked (private window, blocked site data). A per-session id still works for this
    // tab: the registration does not survive a reload, and the dead row is pruned on its first
    // failed send rather than lingering forever.
    return mintInstallId();
  }
}

/**
 * Mint and STORE a fresh id for this browser profile, discarding the current one.
 *
 * The recovery for 409 `browser_push_install_conflict`: the profile's stored id already carries a
 * colleague's live registration (the id survives sign-out, so everyone who signs in on one office
 * desktop inherits it) and the caller could not prove it is at that browser, because Chrome had
 * since rotated the token. A fresh id is the honest answer -- it costs nothing, it is opaque and
 * identifies no person, and it is what lets this person receive their OWN notifications here
 * instead of silently receiving none. The colleague's registration is left exactly as it is.
 *
 * Storage failures are swallowed on purpose: the minted id still works for this tab, which is the
 * same degraded-but-functional path getBrowserInstallId already takes in a private window.
 */
export function resetBrowserInstallId(): string {
  const minted = mintInstallId();
  if (typeof window === "undefined") return minted;
  try {
    window.localStorage.setItem(INSTALL_ID_STORAGE_KEY, minted);
  } catch {
    // Storage blocked; the id is still usable for this session.
  }
  return minted;
}

export function mintInstallId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return `web-${crypto.randomUUID()}`;
  }
  return `web-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/**
 * A short human label for the browser, so a person with three registrations can tell them apart.
 * Derived from the user agent, which is unreliable by design — hence the honest "Browser"
 * fallback rather than a guess. Display only; never a routing or matching input.
 */
export function describeBrowser(userAgent: string): string {
  const ua = userAgent || "";
  // Order matters: Edge and Opera both carry "Chrome", and Chrome carries "Safari".
  if (/Edg\//.test(ua)) return "Microsoft Edge";
  if (/OPR\//.test(ua)) return "Opera";
  if (/Chromium\//.test(ua)) return "Chromium";
  if (/Chrome\//.test(ua)) return "Google Chrome";
  if (/Firefox\//.test(ua)) return "Firefox";
  if (/Safari\//.test(ua)) return "Safari";
  return "Browser";
}

/**
 * The web deep link for a leadership-task notification, mirrored from the service worker
 * (public/firebase-messaging-sw.js) so the two cannot disagree about where a click lands. The
 * worker cannot import this file — it is served verbatim from public/ with no build step — so the
 * test asserts the worker's own source produces the same route.
 */
export function leadershipTaskDeepLink(taskId: string): string {
  const trimmed = (taskId || "").trim();
  if (!trimmed) return "/tasks?scope=team_progress";
  return `/tasks?scope=team_progress&task=${encodeURIComponent(trimmed)}`;
}

/**
 * How long to wait for the FCM token mint before calling it a timeout.
 *
 * TWENTY SECONDS, chosen against the SDK's OWN clock rather than picked for feel. The Firebase
 * messaging SDK bounds its internal wait for the service worker to become active at 10s
 * (`DEFAULT_REGISTRATION_TIMEOUT`) and then rejects with a real error. This budget must sit
 * comfortably ABOVE that, so a failure the SDK can describe surfaces as ITSELF -- with its own
 * message, on the `error` state -- instead of being flattened into a generic timeout by a stopwatch
 * that fired first. The remaining headroom covers the two network round trips that follow it (the
 * installations FID, then the token registration) on a farm office connection.
 *
 * It is a CEILING on an unbounded wait, not a latency target: the healthy path returns in well
 * under a second, so a person who sees this waited through something genuinely pathological.
 */
export const WEB_PUSH_TOKEN_TIMEOUT_MS = 20_000;

/**
 * Race a promise against a deadline.
 *
 * Kept here, in the pure half, for the same reason the state machine is: this is the thing the
 * defect was ABOUT, so it has to be exercisable by `node --test` without the Firebase SDK in the
 * room -- a test that stubs a never-resolving `getToken()` is the only honest proof that the
 * timeout fires at all.
 *
 * `onTimeout` decides what expiry MEANS; this function never invents a state of its own. The timer
 * is always cleared, including when the work settles first, so a resolved mint cannot leave a live
 * timer behind holding the event loop open.
 *
 * NOTE THAT THE WORK IS NOT CANCELLED, because a promise cannot be. A `getToken()` that was merely
 * slow may still resolve after the deadline and mint a perfectly good token; the caller has already
 * moved on, and the next attempt finds that token cached. That is a harmless duplicate, whereas
 * leaving the UI pending forever is the bug being fixed.
 */
export async function withTimeout<T>(
  work: Promise<T>,
  timeoutMs: number,
  onTimeout: () => T,
): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      work,
      new Promise<T>((resolve) => {
        timer = setTimeout(() => resolve(onTimeout()), timeoutMs);
      }),
    ]);
  } finally {
    if (timer !== undefined) clearTimeout(timer);
  }
}

/**
 * How the Firebase JS SDK is to be asked for a token: with THIS project's VAPID key, or with the
 * SDK's own built-in default key pair.
 */
export type WebPushVapidKey = { source: "project"; key: string } | { source: "sdk-default" };

/**
 * The sentence shown when the environment's key is set but unusable. One place, because the
 * server adapter and the client state machine must not disagree about what happened.
 */
export const VAPID_KEY_UNUSABLE_MESSAGE =
  "This environment's browser-notification key is not a usable VAPID key, so notifications are off until it is corrected or removed.";

/**
 * A VAPID application server key is a P-256 public key as an UNCOMPRESSED EC point: the 0x04
 * prefix byte plus the 32-byte X and 32-byte Y coordinates = 65 bytes, base64url-encoded to 87
 * characters with no padding. Both `pushManager.subscribe()` and the FCM registration call reject
 * anything else, so the shape is checkable here rather than discovered as an opaque SDK throw
 * inside a click handler.
 */
const VAPID_KEY_BASE64URL_LENGTH = 87;
const VAPID_KEY_BYTE_LENGTH = 65;
const VAPID_KEY_UNCOMPRESSED_POINT_PREFIX = 0x04;

/**
 * THE THREE-STATE RULE FOR `GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY`, and the whole point of this
 * function is that the three states are genuinely different things:
 *
 *  ABSENT / EMPTY  -> `{ source: "sdk-default" }`. PROCEED. The Firebase JS SDK ships a built-in
 *      default VAPID key pair (`DEFAULT_VAPID_KEY` in @firebase/messaging) and `getToken()` uses
 *      it whenever `vapidKey` is falsy, FCM holds the matching private key, and the resulting
 *      token is fully deliverable -- proven end to end in Chrome on this branch: a real 142-char
 *      token minted with NO key configured, registered through the real backend endpoint, and a
 *      real push displayed by the real service worker with the right deep link. So an absent key
 *      is not a broken feature, and the control is OFFERED.
 *
 *  PRESENT         -> `{ source: "project", key }`, used VERBATIM. This is what a project key is
 *      for: provenance and independent rotation, which the SDK default cannot give.
 *
 *  PRESENT BUT MALFORMED -> a FAILURE, loudly, and deliberately NOT a silent fall back to the
 *      default. This is the one case the old code's warning was actually about: a key that is the
 *      wrong shape (or the wrong project's) yields a subscription FCM will accept and can never
 *      deliver to, and a silent fallback would turn a typo in Terraform into a channel that
 *      reports "enabled" and delivers nothing. Somebody set a key on purpose; if it is unusable,
 *      say so instead of quietly ignoring them.
 */
export function resolveVapidKeyConfig(
  raw: string | undefined | null,
): { ok: true; key: WebPushVapidKey } | { ok: false; reason: string } {
  const trimmed = typeof raw === "string" ? raw.trim() : "";
  if (trimmed === "") return { ok: true, key: { source: "sdk-default" } };
  if (!isWellFormedVapidPublicKey(trimmed)) return { ok: false, reason: VAPID_KEY_UNUSABLE_MESSAGE };
  return { ok: true, key: { source: "project", key: trimmed } };
}

/** Shape check only. A well-formed key for the WRONG project is undetectable here, and is why a
 * malformed one must fail rather than be papered over. */
export function isWellFormedVapidPublicKey(candidate: string): boolean {
  if (candidate.length !== VAPID_KEY_BASE64URL_LENGTH) return false;
  if (!/^[A-Za-z0-9_-]+$/.test(candidate)) return false;
  const bytes = decodeBase64Url(candidate);
  if (!bytes || bytes.length !== VAPID_KEY_BYTE_LENGTH) return false;
  return bytes[0] === VAPID_KEY_UNCOMPRESSED_POINT_PREFIX;
}

function decodeBase64Url(value: string): Uint8Array | undefined {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/") + "=".repeat((4 - (value.length % 4)) % 4);
  try {
    // atob exists in the browser and in Node >= 16, which covers both the client and `node --test`.
    const binary = atob(padded);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
    return bytes;
  } catch {
    return undefined;
  }
}

/**
 * The `getToken()` options that carry the key -- and for the SDK default, THE PROPERTY IS ABSENT,
 * not `""` and not an explicit `undefined`.
 *
 * Verified against the SDK's actual code, not assumed: `updateVapidKey(messaging, options?.vapidKey)`
 * in @firebase/messaging does `if (!!vapidKey) { messaging.vapidKey = vapidKey } else if
 * (!messaging.vapidKey) { messaging.vapidKey = DEFAULT_VAPID_KEY }`. So a falsy value takes the
 * default -- `""` would in fact work. Omitting the property anyway is the honest encoding of
 * "we are not supplying a key", it is what the SDK's own typings describe, and it cannot be
 * re-read later as "we supplied an empty key on purpose".
 */
export function vapidGetTokenOptions(key: WebPushVapidKey): { vapidKey?: string } {
  return key.source === "project" ? { vapidKey: key.key } : {};
}
