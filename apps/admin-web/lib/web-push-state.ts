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
 *  unconfigured  the environment has no VAPID key. Our gap, not the person's.
 *  blocked       permission is 'denied'. THE PROMPT CANNOT BE SHOWN AGAIN from script — only the
 *                person can undo it, in the browser's own site settings. Say where.
 *  dismissed     the prompt was shown and closed without a choice, so permission is still
 *                'default'. Retryable: this is NOT a refusal, and must not be reported as one.
 *  prompt        permission is 'default', or granted-but-not-registered. Actionable.
 *  enabled       permission granted and a token is registered with the backend.
 *  error         something failed (offline, token mint refused, backend rejected).
 */
export type WebPushState =
  | { status: "unsupported"; reason: string }
  | { status: "unconfigured"; reason: string }
  | { status: "blocked" }
  | { status: "dismissed" }
  | { status: "prompt" }
  | { status: "enabled"; browserInstallId: string }
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
}): WebPushState {
  if (!input.supported) {
    return {
      status: "unsupported",
      reason: input.supportReason || "Notifications are not available in this browser.",
    };
  }
  if (!input.configured) {
    return { status: "unconfigured", reason: "Browser notifications are not configured for this environment yet." };
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
