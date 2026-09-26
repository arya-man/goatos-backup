"use server";

import {
  getBrowserPushRegistrations,
  postBrowserPushRegistration,
  postBrowserPushEvent,
  postBrowserPushUnregister,
  readWebPushVapidKey,
  type BrowserPushRegistration,
  type WebPushResult,
  type WebPushVapidKey,
} from "@/lib/api/browser-push-server";

/**
 * Server actions behind the browser web push controls.
 *
 * Thin by design: each one validates its input, derives the operation's stable key, and delegates
 * to the authenticated server-only adapter in lib/api/browser-push-server.ts, which owns the
 * session token and the headers. Nothing here touches the network itself.
 *
 * REPLAY PROTECTION IS STRUCTURAL, NOT BOLTED ON. `browser_install_id` IS the operation's stable
 * key: the backend write is an upsert whose conflict target is (tenant_id, browser_install_id), so
 * re-sending the same registration converges on the same single row rather than creating a second
 * one, and unregister matches nothing the second time and reports removed=false. The key is sent
 * explicitly anyway (`stableMutationKey` below -> X-Idempotency-Key) so a retry is attributable in
 * the backend's own logs and so the guarantee is stated at the call site instead of being an
 * invisible property of one SQL clause.
 *
 * These calls are RETRIED BY THE CLIENT ON EVERY DASHBOARD LOAD by design -- Chrome rotates the
 * FCM token on its own schedule, so the register action is called far more often than it changes
 * anything. Convergence, not at-most-once delivery, is what makes that safe.
 */

// Type-only aliases stay out of this "use server" module: Turbopack registers every export of a
// server-action file as a server reference, and a type re-export becomes a runtime ReferenceError.
type WebPushActionResult<T> = WebPushResult<T>;

// Mirrors the backend's own ceilings (backend/internal/browserpush/registration.go) so an
// oversized value is refused here rather than spending a round trip to be refused there.
const MAX_BROWSER_INSTALL_ID = 200;
const MAX_TOKEN = 4096;
const MAX_BROWSER_LABEL = 120;

/**
 * How to ask the SDK for a token, resolved at runtime.
 *
 * ABSENT variable => `{ source: "sdk-default" }`, and push is OFFERED: the Firebase JS SDK's own
 * built-in default VAPID key mints a deliverable token, so no Firebase-console step is required to
 * turn the channel on. A set-but-unusable key is the only failure, and it fails loudly rather than
 * falling back to the default. See readWebPushVapidKey.
 */
export async function getWebPushVapidKey(): Promise<WebPushActionResult<WebPushVapidKey>> {
  return readWebPushVapidKey();
}

/**
 * Store or refresh this browser's push address.
 *
 * The push token is validated but NEVER logged, echoed, or put in a URL: it is a bearer-style
 * push credential, and whoever holds it can send a notification to that browser.
 */
export async function registerBrowserPush(input: {
  browserInstallId: string;
  token: string;
  browserLabel?: string;
}): Promise<WebPushActionResult<{ registration: BrowserPushRegistration; created: boolean }>> {
  const browserInstallId = requiredField(input.browserInstallId, MAX_BROWSER_INSTALL_ID);
  if (!browserInstallId) {
    return { ok: false, error: "This browser could not be identified. Reload the page and try again." };
  }
  const token = requiredField(input.token, MAX_TOKEN);
  if (!token) {
    return { ok: false, error: "This browser could not be registered for notifications. Try again or use another browser." };
  }
  // Control characters in a bearer credential are never legitimate and are exactly what a
  // log-injection or header-smuggling attempt looks like. Refused, never stripped: a stripped
  // token would be stored, would never resolve, and would report success.
  if (/[\x00-\x1f\x7f]/.test(token)) {
    return { ok: false, error: "This browser could not be registered for notifications. Try again or use another browser." };
  }
  const browserLabel = optionalField(input.browserLabel, MAX_BROWSER_LABEL);

  // The adapter's failure carries the backend's `code` (see WebPushResult): the caller keys its
  // shared-profile recovery on `browser_push_install_conflict`, never on the message.
  return postBrowserPushRegistration({
    browserInstallId,
    token,
    browserLabel,
    stableMutationKey: stableMutationKey("register", browserInstallId),
  });
}

/** Switch this browser off. Idempotent: removed=false when nothing active matched. */
export async function unregisterBrowserPush(input: {
  browserInstallId: string;
}): Promise<WebPushActionResult<{ removed: boolean }>> {
  const browserInstallId = requiredField(input.browserInstallId, MAX_BROWSER_INSTALL_ID);
  if (!browserInstallId) {
    return { ok: false, error: "This browser could not be identified. Reload the page and try again." };
  }
  return postBrowserPushUnregister({
    browserInstallId,
    stableMutationKey: stableMutationKey("unregister", browserInstallId),
  });
}

export async function recordBrowserPushEvent(input: {
  notificationRequestId: string;
  browserInstallId: string;
  eventType: "displayed" | "opened";
  traceId?: string;
}): Promise<WebPushActionResult<{ recorded: boolean }>> {
  const notificationRequestId = requiredField(input.notificationRequestId, 64);
  const browserInstallId = requiredField(input.browserInstallId, MAX_BROWSER_INSTALL_ID);
  if (!notificationRequestId || !browserInstallId) {
    return { ok: false, error: "This notification event could not be recorded." };
  }
  if (input.eventType !== "displayed" && input.eventType !== "opened") {
    return { ok: false, error: "This notification event is not supported." };
  }
  return postBrowserPushEvent({
    notificationRequestId,
    browserInstallId,
    eventType: input.eventType,
    traceId: optionalField(input.traceId, 128),
  });
}

/** List this person's own browser registrations, whatever their status. */
export async function listBrowserPushRegistrations(): Promise<
  WebPushActionResult<{ registrations: BrowserPushRegistration[] }>
> {
  return getBrowserPushRegistrations();
}

/**
 * The operation's stable key.
 *
 * Derived from the BROWSER, not from a random value or a clock: a fresh uuid per call would make
 * every retry a new operation, which is the opposite of what an idempotency key is for. The token
 * is deliberately NOT part of the key -- a rotated token on the same browser is the same
 * operation (refresh this browser's address), and including it would make every rotation a
 * distinct write.
 */
function stableMutationKey(operation: string, browserInstallId: string): string {
  return `browser-push:${operation}:${browserInstallId}`;
}

function requiredField(value: string | undefined, max: number): string {
  const trimmed = typeof value === "string" ? value.trim() : "";
  if (trimmed === "" || trimmed.length > max) return "";
  return trimmed;
}

// A display-only field is TRIMMED rather than refused: failing a registration over a cosmetic
// label would cost the person their notifications for nothing.
function optionalField(value: string | undefined, max: number): string {
  const trimmed = typeof value === "string" ? value.trim() : "";
  return trimmed.length > max ? trimmed.slice(0, max) : trimmed;
}
