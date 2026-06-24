import "server-only";

import { createAdminApiClient, createAppApiClient, GoatOSApiError } from "@goatos/api-client";
import type { AdminApiComponents, AdminApiPaths, AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { getFirebaseIdTokenCookie } from "@/lib/auth/server-session";

type ErrorEnvelope = AppApiComponents["schemas"]["ErrorEnvelope"];

export type GoatPassportResponse = AppApiComponents["schemas"]["GoatPassportResponse"];
export type GoatSearchResponse = AppApiComponents["schemas"]["GoatSearchResponse"];
export type GoatTimelineResponse = AppApiComponents["schemas"]["GoatTimelineResponse"];
export type IdentifierType = AppApiComponents["schemas"]["IdentifierType"];
export type ActionCenterObligation = AppApiComponents["schemas"]["ActionCenterObligation"];
export type ActionCenterResponse = AppApiComponents["schemas"]["ActionCenterResponse"];
export type VaccinationQueueItem = AppApiComponents["schemas"]["VaccinationQueueItem"];
export type VaccinationQueueResponse = AppApiComponents["schemas"]["VaccinationQueueResponse"];
export type ImpactPreviewInput = AppApiComponents["schemas"]["ImpactPreviewInput"];
export type ImpactPreviewResult = AppApiComponents["schemas"]["ImpactPreviewResult"];
export type VaccinationPassportDue = AppApiComponents["schemas"]["VaccinationPassportDue"];
export type VaccinationPassportHistoryItem = AppApiComponents["schemas"]["VaccinationPassportHistoryItem"];
export type VaccinationPassport = AppApiComponents["schemas"]["VaccinationPassport"];
export type AcceptVaccinationCompletionResponse = AppApiComponents["schemas"]["AcceptVaccinationCompletionResponse"];
export type RejectVaccinationCompletionResponse = AppApiComponents["schemas"]["RejectVaccinationCompletionResponse"];

export type AdminGoatResponse = AdminApiComponents["schemas"]["AdminGoatResponse"];
export type AddIdentifierRequestBody = AdminApiComponents["schemas"]["AddIdentifierRequest"];
export type RetireIdentifierRequestBody = AdminApiComponents["schemas"]["RetireIdentifierRequest"];

export type ApiErrorKind =
  | "missing_config"
  | "unauthorized"
  | "tenant_scope_mismatch"
  | "permission_denied"
  | "bad_request"
  | "not_found"
  | "backend_down"
  | "api_error";

export type ApiUiError = {
  kind: ApiErrorKind;
  status?: number;
  code?: string;
  message: string;
  traceId?: string;
  retryable?: boolean;
};

export type ApiResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: ApiUiError };

type ServerConfig = {
  baseUrl: string;
  bearerToken: string;
  tenantId: string;
};

export type HerdSearchParams = {
  limit: number;
  cursor?: string;
  q?: string;
  goat_id?: string;
  identifier_type?: IdentifierType;
  scope_key?: string;
  breed?: string;
  sex?: string;
  farm_id?: string;
  park_id?: string;
  location_id?: string;
  status?: string;
};

export type GoatTimelineParams = {
  goatId: string;
  limit: number;
  cursor?: string;
};

export async function getServerConfig(requireTenant = false): Promise<ApiResult<ServerConfig>> {
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080";
  const firebaseIdToken = await getFirebaseIdTokenCookie();
  const localBearerToken =
    process.env.GOATOS_ENV === "local" && process.env.GOATOS_AUTH_MODE === "bearer"
      ? process.env.GOATOS_BEARER_TOKEN
      : undefined;
  const bearerToken = localBearerToken ?? firebaseIdToken;
  const tenantId = process.env.GOATOS_TENANT_ID;

  if (!bearerToken) {
    return {
      ok: false,
      error: {
        kind: "unauthorized",
        status: 401,
        code: "firebase_session_missing",
        message: "Sign in with Google to start or refresh your admin session.",
      },
    };
  }
  if (requireTenant && !tenantId) {
    return {
      ok: false,
      error: {
        kind: "missing_config",
        message: "Server configuration missing: tenant id.",
      },
    };
  }

  return {
    ok: true,
    data: {
      baseUrl,
      bearerToken: bearerToken ?? "",
      tenantId: tenantId ?? "",
    },
  };
}

export function getAdminRuntimeStatus() {
  return {
    baseUrl: process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080",
    hasLocalBearerFallback:
      process.env.GOATOS_ENV === "local" &&
      process.env.GOATOS_AUTH_MODE === "bearer" &&
      Boolean(process.env.GOATOS_BEARER_TOKEN),
    hasTenantId: Boolean(process.env.GOATOS_TENANT_ID),
  };
}

function apiClientOptions(config: ServerConfig) {
  return {
    baseUrl: config.baseUrl,
    bearerToken: config.bearerToken,
    tenantId: config.tenantId || undefined,
  };
}

export function isAuthRequiredError(error: ApiUiError): boolean {
  return error.kind === "unauthorized" || error.status === 401;
}

export function firstAuthRequiredError(
  ...results: Array<ApiResult<unknown> | null | undefined>
): ApiUiError | null {
  for (const result of results) {
    if (result && !result.ok && isAuthRequiredError(result.error)) {
      return result.error;
    }
  }
  return null;
}

export async function searchGoats(params: HerdSearchParams): Promise<ApiResult<GoatSearchResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<GoatSearchResponse>("/goats/search", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getGoatPassport(goatId: string): Promise<ApiResult<GoatPassportResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(goatId)}` as keyof AppApiPaths & string;
  return request(() => client.request<GoatPassportResponse>(path, { cache: "no-store" }));
}

export async function getGoatTimeline(params: GoatTimelineParams): Promise<ApiResult<GoatTimelineResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(params.goatId)}/timeline` as keyof AppApiPaths & string;
  return request(() =>
    client.request<GoatTimelineResponse>(path, {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        cursor: params.cursor,
      }),
    }),
  );
}

export async function getVaccinationActionCenter(
  params: { status?: string; dueBefore?: string; limit?: number } = {},
): Promise<ApiResult<ActionCenterResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ActionCenterResponse>("/action-center/obligations", {
      cache: "no-store",
      query: compactQuery({ status: params.status ?? "due", due_before: params.dueBefore, limit: params.limit ?? 100 }),
    }),
  );
}

export async function getVaccinationVerificationQueue(
  params: { limit?: number } = {},
): Promise<ApiResult<VaccinationQueueResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationQueueResponse>("/vaccination/verification-queue", {
      cache: "no-store",
      query: compactQuery({ limit: params.limit ?? 100 }),
    }),
  );
}

export async function previewVaccinationImpact(body: ImpactPreviewInput): Promise<ApiResult<ImpactPreviewResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<ImpactPreviewResult>("/protocols/vaccination/impact-preview", { method: "POST", cache: "no-store", body }));
}

export async function createProtocolDefinition(body: {
  code: string;
  name: string;
  category: string;
}): Promise<ApiResult<{ protocol_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<{ protocol_id: string }>("/protocols", { method: "POST", cache: "no-store", body }));
}

export async function createProtocolVersion(
  protocolId: string,
  body: { scope_type: string; scope_id?: string; version: number; effective_from: string; rule_dsl: unknown; proof_policy?: unknown },
): Promise<ApiResult<{ protocol_version_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/${encodeURIComponent(protocolId)}/versions` as keyof AppApiPaths & string;
  return request(() => client.request<{ protocol_version_id: string }>(path, { method: "POST", cache: "no-store", body }));
}

export async function addProtocolRule(versionId: string, body: Record<string, unknown>): Promise<ApiResult<{ rule_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/rules` as keyof AppApiPaths & string;
  return request(() => client.request<{ rule_id: string }>(path, { method: "POST", cache: "no-store", body }));
}

export async function publishProtocolVersion(versionId: string): Promise<ApiResult<Record<string, never>>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/publish` as keyof AppApiPaths & string;
  return request(() => client.request<Record<string, never>>(path, { method: "POST", cache: "no-store" }));
}

export async function getGoatVaccinationPassport(goatId: string): Promise<ApiResult<VaccinationPassport>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(goatId)}/passport` as keyof AppApiPaths & string;
  return request(() => client.request<VaccinationPassport>(path, { cache: "no-store" }));
}

export async function acceptVaccinationCompletion(
  completionId: string,
): Promise<ApiResult<AcceptVaccinationCompletionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/completions/${encodeURIComponent(completionId)}/accept` as keyof AppApiPaths & string;
  return request(() => client.request<AcceptVaccinationCompletionResponse>(path, { method: "POST", cache: "no-store" }));
}

export async function rejectVaccinationCompletion(
  completionId: string,
  reason: string,
): Promise<ApiResult<RejectVaccinationCompletionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/completions/${encodeURIComponent(completionId)}/reject` as keyof AppApiPaths & string;
  return request(() => client.request<RejectVaccinationCompletionResponse>(path, { method: "POST", cache: "no-store", body: { reason } }));
}

export async function addGoatIdentifier(
  goatId: string,
  body: AddIdentifierRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/identifiers` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function retireGoatIdentifier(
  goatId: string,
  identifierId: string,
  body: RetireIdentifierRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/identifiers/${encodeURIComponent(identifierId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

async function request<T>(fn: () => Promise<T>): Promise<ApiResult<T>> {
  try {
    return { ok: true, data: await fn() };
  } catch (error) {
    return { ok: false, error: normalizeApiError(error) };
  }
}

function normalizeApiError(error: unknown): ApiUiError {
  if (error instanceof GoatOSApiError) {
    const envelope = parseEnvelope(error.body);
    const code = envelope?.code;
    if (error.status === 401) {
      return {
        kind: "unauthorized",
        status: error.status,
        code,
        message: "Your Google sign-in session is missing or expired. Sign in again to refresh the admin session.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403 && code === "tenant_scope_mismatch") {
      return {
        kind: "tenant_scope_mismatch",
        status: error.status,
        code,
        message: "Configured tenant does not match the signed-in admin session.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403) {
      return {
        kind: "permission_denied",
        status: error.status,
        code: code ?? "permission_denied",
        message: "Signed in, but the active DB grants do not allow this view.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 400) {
      return {
        kind: "bad_request",
        status: error.status,
        code,
        message: envelope?.message ?? "The service rejected these filters. Check the selected tenant, cursor, and limit.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 404) {
      return {
        kind: "not_found",
        status: error.status,
        code,
        message: envelope?.message ?? "Not found, or this admin token is not allowed to view it.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    return {
      kind: "api_error",
      status: error.status,
      code,
      message: envelope?.message ?? `Backend service returned ${error.status}.`,
      traceId: envelope?.trace_id,
      retryable: envelope?.retryable,
    };
  }
  if (error instanceof TypeError) {
    return {
      kind: "backend_down",
      message: "Backend service is not reachable from the Mesha admin server.",
    };
  }
  return {
    kind: "api_error",
    message: error instanceof Error ? error.message : "Unexpected API error.",
  };
}

function parseEnvelope(body: unknown): ErrorEnvelope | null {
  if (!body || typeof body !== "object") return null;
  const maybe = body as Partial<ErrorEnvelope>;
  if (typeof maybe.code !== "string" || typeof maybe.message !== "string") {
    return null;
  }
  return maybe as ErrorEnvelope;
}

function compactQuery(values: Record<string, string | number | boolean | null | undefined>) {
  const query: Record<string, string | number | boolean> = {};
  for (const [key, value] of Object.entries(values)) {
    if (value === null || value === undefined || value === "") continue;
    query[key] = value;
  }
  return query;
}
