import "server-only";

import { getServerConfig } from "@/lib/api/server";

/**
 * Server-only API adapter for the browser web push registration endpoints.
 *
 * WHY A RAW FETCH AND NOT THE GENERATED CLIENT: the three registration endpoints are not in
 * contracts/openapi/app-api.yaml yet (the contract is owned elsewhere while this lands; the
 * required spec is written out separately for it to be merged into). `@goatos/api-client` is
 * generated FROM that contract, so it has no method for a path the contract does not declare.
 *
 * WHY IT LIVES HERE AND NOT IN THE SERVER ACTION: the API-access boundary rule is that a Server
 * Action goes through the authenticated server-only adapter layer rather than calling the network
 * itself. This IS that layer -- same `getServerConfig` that configures the generated client, same
 * bearer/tenant/traceparent headers, one place that resolves the session token. When the contract
 * lands, the three functions below move onto the generated client and `lib/web-push-actions.ts`
 * does not change.
 *
 * The push token never reaches a URL or a log: it rides in the POST body only. It is a
 * bearer-style push credential -- whoever holds it can notify that browser.
 */

export type WebPushResult<T> = { ok: true; data: T } | { ok: false; error: string };

export type BrowserPushRegistration = {
  browser_registration_id: string;
  workforce_member_id: string;
  provider: string;
  browser_install_id: string;
  browser_label: string;
  status: "active" | "stale" | "unsubscribed";
  created_at: string;
  last_seen_at: string;
  stale_at?: string;
  stale_reason?: string;
  row_version: number;
};

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
 * Absent key => push is simply unavailable and the UI says so. It must NEVER fall back to a
 * hardcoded default: a wrong VAPID key produces a token FCM accepts and can never deliver to.
 */
export function readWebPushVapidKey(): WebPushResult<string> {
  const key = process.env.GOATOS_FIREBASE_WEB_PUSH_VAPID_KEY?.trim();
  if (!key) {
    return { ok: false, error: "Browser notifications are not configured for this environment yet." };
  }
  return { ok: true, data: key };
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
  if (config.data.tenantId) headers["X-Tenant-Id"] = config.data.tenantId;
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
      return { ok: false, error: backendErrorMessage(text, response.status) };
    }
    return { ok: true, data: (text ? JSON.parse(text) : {}) as T };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "Could not reach the notification service." };
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
