import "server-only";

import { TENANT_CONTEXT_HEADER, type AppApiComponents } from "@goatos/api-client";
import { getServerConfig } from "@/lib/api/server";
import { resolveVapidKeyConfig, type WebPushVapidKey } from "@/lib/web-push-state";

/**
 * Server-only API adapter for the browser web push registration endpoints.
 *
 * WHY A RAW FETCH AND NOT THE GENERATED CLIENT: the three registration endpoints are now
 * DECLARED in contracts/openapi/app-api.yaml, so the row/response SHAPES below come from the
 * generated types rather than being hand-written. `@goatos/api-client` is an
 * openapi-typescript TYPE package -- it generates no request methods -- so the transport stays
 * a raw fetch through this adapter, and it is the contract's types that make it type-checked
 * against the backend.
 *
 * WHY IT LIVES HERE AND NOT IN THE SERVER ACTION: the API-access boundary rule is that a Server
 * Action goes through the authenticated server-only adapter layer rather than calling the network
 * itself. This IS that layer -- same `getServerConfig` that configures the generated client, same
 * bearer/tenant/traceparent headers, one place that resolves the session token.
 *
 * The push token never reaches a URL or a log: it rides in the POST body only. It is a
 * bearer-style push credential -- whoever holds it can notify that browser.
 */

/**
 * `code` is the backend's own machine-readable error code, carried alongside the person-facing
 * message. It exists for exactly one caller today -- lib/web-push.ts keys its recovery from
 * `browser_push_install_conflict` on it -- and a UI branch must never be taken on the SENTENCE,
 * which is backend-owned copy and may be reworded.
 */
export type WebPushResult<T> = { ok: true; data: T } | { ok: false; error: string; code?: string };

/**
 * One browser profile's registration, taken from the contract rather than re-declared here --
 * a hand-written twin of a schema the backend owns is a shape that can drift silently.
 */
export type BrowserPushRegistration = AppApiComponents["schemas"]["BrowserPushRegistration"];

export type { WebPushVapidKey };

const REGISTRATIONS_PATH = "/admin/notifications/browser-registrations";

/**
 * The VAPID application server key the Firebase JS SDK needs to mint a web registration token.
 *
 * It is a PUBLIC key (the browser transmits it to the push service on every subscribe; the
 * matching private key lives only in the Firebase project), so handing it to the client is
 * correct and not a leak. It is still read at RUNTIME rather than inlined as a NEXT_PUBLIC_*
 * build arg, for the same reason /api/auth/firebase-config resolves the Firebase config at
 * runtime: one image is deployed to more than one environment, and a key baked in at build time
 * would be the wrong project's key in the other one.
 *
 * ABSENT KEY IS NOT A DISABLED FEATURE, and this comment used to claim the opposite. The Firebase
 * JS SDK ships its own built-in default VAPID key pair and `getToken()` uses it whenever `vapidKey`
 * is not supplied; FCM holds the matching private key, so the token is fully deliverable with NO
 * project key configured -- proven end to end in real Chrome on this branch (a real token minted
 * with the variable unset, registered through the real endpoint, and a real push rendered by the
 * real service worker). Requiring the Firebase-console step gated a working channel on a key that
 * is not needed to deliver.
 *
 * What the old warning WAS right about is a key that is set and unusable: that produces a
 * subscription FCM accepts and can never deliver to, so it still fails loudly and is never
 * silently replaced by the default. The three-state rule lives in one pure, tested place --
 * `resolveVapidKeyConfig` in lib/web-push-state.ts -- so the server and the client cannot drift
 * about which of the three happened. This function is only the env read.
 *
 * Setting the variable remains fully supported and stays wired in infra/envs/{stg}: a project key
 * is how push gets provenance and independent rotation. It is no longer REQUIRED.
 */
export function readWebPushVapidKey(): WebPushResult<WebPushVapidKey> {
  const resolved = resolveVapidKeyConfig(process.env.GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY);
  if (!resolved.ok) return { ok: false, error: resolved.reason, code: "browser_push_vapid_key_unusable" };
  return { ok: true, data: resolved.key };
}

/** Store or refresh this browser's push address. */
export async function postBrowserPushRegistration(input: {
  browserInstallId: string;
  token: string;
  browserLabel?: string;
  stableMutationKey?: string;
}): Promise<WebPushResult<{ registration: BrowserPushRegistration; created: boolean }>> {
  return callBackend(
    REGISTRATIONS_PATH,
    "POST",
    {
      browser_install_id: input.browserInstallId,
      token: input.token,
      browser_label: input.browserLabel ?? "",
    },
    input.stableMutationKey,
  );
}

/** Switch this browser off. Idempotent: an already-unsubscribed browser returns removed=false. */
export async function postBrowserPushUnregister(input: {
  browserInstallId: string;
  stableMutationKey?: string;
}): Promise<WebPushResult<{ removed: boolean }>> {
  return callBackend(
    `${REGISTRATIONS_PATH}/unregister`,
    "POST",
    { browser_install_id: input.browserInstallId },
    input.stableMutationKey,
  );
}

export async function postBrowserPushEvent(input: {
  notificationRequestId: string;
  browserInstallId: string;
  eventType: "displayed" | "opened";
  traceId?: string;
}): Promise<WebPushResult<{ recorded: boolean }>> {
  return callBackend("/admin/notifications/browser-events", "POST", {
    notification_request_id: input.notificationRequestId,
    browser_install_id: input.browserInstallId,
    event_type: input.eventType,
    trace_id: input.traceId ?? "",
  });
}

/** List this person's own browser registrations, whatever their status. */
export async function getBrowserPushRegistrations(): Promise<
  WebPushResult<{ registrations: BrowserPushRegistration[] }>
> {
  return callBackend(REGISTRATIONS_PATH, "GET");
}

async function callBackend<T>(
  path: string,
  method: "GET" | "POST",
  body?: unknown,
  idempotencyKey?: string,
): Promise<WebPushResult<T>> {
  const config = await getServerConfig();
  if (!config.ok) {
    return { ok: false, error: config.error.message };
  }
  const headers: Record<string, string> = {
    Authorization: `Bearer ${config.data.bearerToken}`,
    Accept: "application/json",
  };
  // The backend reads the tenant ONLY from this header (Firebase tokens carry no tenant claim), so
  // the name must be the client's constant, not a hand-typed twin: a mistyped header meant every
  // register/list call came back 401 "tenant context is required" and the bell kept offering
  // "Enable notifications" to a browser that had already granted permission.
  if (config.data.tenantId) headers[TENANT_CONTEXT_HEADER] = config.data.tenantId;
  if (config.data.traceparent) headers.traceparent = config.data.traceparent;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  // The operation's stable key, derived from the browser (see stableMutationKey in
  // lib/web-push-actions.ts). Both writes are already convergent by their natural key -- register
  // is an upsert on (tenant, browser_install_id), unregister matches nothing the second time --
  // so this makes a retry ATTRIBUTABLE in the backend's logs rather than providing the guarantee.
  if (idempotencyKey) headers["X-Idempotency-Key"] = idempotencyKey;

  try {
    const response = await fetch(`${config.data.baseUrl}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      cache: "no-store",
    });
    const text = await response.text();
    if (!response.ok) {
      // Surface the backend's own message when it sent one: its refusals name the exact field, and
      // replacing them with a generic sentence would hide the answer from the reader.
      return { ok: false, error: backendErrorMessage(text, response.status), code: backendErrorCode(text) };
    }
    return { ok: true, data: (text ? JSON.parse(text) : {}) as T };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "Could not reach the notification service." };
  }
}

function backendErrorCode(text: string): string | undefined {
  try {
    const parsed = JSON.parse(text) as { code?: string };
    const code = parsed?.code?.trim();
    return code ? code : undefined;
  } catch {
    return undefined;
  }
}

function backendErrorMessage(text: string, status: number): string {
  try {
    const parsed = JSON.parse(text) as { message?: string; code?: string };
    if (parsed?.message) return parsed.message;
    if (parsed?.code) return parsed.code;
  } catch {
    // Not JSON; fall through.
  }
  return `Notification service returned ${status}.`;
}
