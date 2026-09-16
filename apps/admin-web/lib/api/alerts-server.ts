import "server-only";

// Server-only reads and the one write for the Alerts page (GET /alerts/rows, GET /alerts/config,
// PUT /alerts/config/{rule_key}). Same ApiResult envelope and helpers as lib/api/server.ts; no
// client fetch, no route handler. Scope is decided by the backend (park through grants); this
// module only forwards the filters.
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type AlertRow = AppApiComponents["schemas"]["AlertRow"];
export type AlertsPage = AppApiComponents["schemas"]["AlertsPage"];
export type AlertSeverity = AppApiComponents["schemas"]["AlertSeverity"];
export type AlertRuleConfig = AppApiComponents["schemas"]["AlertRuleConfig"];
export type AlertRuleConfigList = AppApiComponents["schemas"]["AlertRuleConfigList"];

export async function listAlerts(scope: { park: string; businessDate?: string }): Promise<ApiResult<AlertsPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AlertsPage>("/alerts/rows", {
      cache: "no-store",
      query: compactQuery({ park: scope.park, business_date: scope.businessDate }),
    }),
  );
}

/**
 * The Configure drawer's read. Gated on alerts.configure -- the page must only call this when
 * controlEnabled(pageContract, "configure_alerts", false) is true, so a reader's build never
 * renders a bare error card for a route it may not open.
 */
export async function getAlertRuleConfig(): Promise<ApiResult<AlertRuleConfigList>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<AlertRuleConfigList>("/alerts/config", { cache: "no-store" }));
}

/**
 * Switch one rule on or off and set its threshold. Both fields are sent every time -- a blank is
 * not a zero and not "keep the old value" -- and an out-of-range threshold is NOT clamped here:
 * the backend owns that refusal and must be allowed to make it.
 */
export async function setAlertRuleConfig(
  ruleKey: string,
  body: { enabled: boolean; threshold: number },
  idempotencyKey: string,
): Promise<ApiResult<AlertRuleConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/alerts/config/${encodeURIComponent(ruleKey)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<AlertRuleConfig>(path, {
      method: "PUT",
      cache: "no-store",
      // Derived, never random: the same (rule, on/off, threshold) is one logical act, so a
      // double-click or a retried Server Action is ONE write, and a different value under the
      // same key is refused by the backend rather than silently overwriting.
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export type AlertEventRule = AppApiComponents["schemas"]["AlertEventRule"];
export type AlertEventKind = AppApiComponents["schemas"]["AlertEventKind"];
export type AlertEventRuleRequest = AppApiComponents["schemas"]["AlertEventRuleRequest"];

/** Compose a new alert from the event catalog (POST /alerts/config/events). */
export async function createAlertEventRule(body: AlertEventRuleRequest, idempotencyKey: string): Promise<ApiResult<AlertEventRule>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AlertEventRule>("/alerts/config/events", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/** Change one composed alert (PUT /alerts/config/events/{rule_id}); the whole row is sent. */
export async function updateAlertEventRule(ruleId: string, body: AlertEventRuleRequest, idempotencyKey: string): Promise<ApiResult<AlertEventRule>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/alerts/config/events/${encodeURIComponent(ruleId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<AlertEventRule>(path, {
      method: "PUT",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/** Remove one composed alert (DELETE /alerts/config/events/{rule_id}). */
export async function deleteAlertEventRule(ruleId: string): Promise<ApiResult<void>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/alerts/config/events/${encodeURIComponent(ruleId)}` as keyof AppApiPaths & string;
  return request(() => client.request<void>(path, { method: "DELETE", cache: "no-store" }));
}
