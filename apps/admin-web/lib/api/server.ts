import "server-only";

import {
  createAdminApiClient,
  createAnalyticsApiClient,
  createAppApiClient,
  GoatOSApiError,
} from "@goatos/api-client";
import type {
  AdminApiComponents,
  AdminApiPaths,
  AnalyticsApiComponents,
  AppApiComponents,
  AppApiPaths,
} from "@goatos/api-client";

type ErrorEnvelope = AppApiComponents["schemas"]["ErrorEnvelope"];

export type GoatSummary = AppApiComponents["schemas"]["GoatSummary"];
export type GoatPassportResponse = AppApiComponents["schemas"]["GoatPassportResponse"];
export type GoatSearchResponse = AppApiComponents["schemas"]["GoatSearchResponse"];
export type IdentifierType = AppApiComponents["schemas"]["IdentifierType"];

export type CounterGrain = AnalyticsApiComponents["schemas"]["CounterGrain"];
export type IdentityCountsResponse = AnalyticsApiComponents["schemas"]["IdentityCountsResponse"];

export type ConflictListResponse = AdminApiComponents["schemas"]["ConflictListResponse"];
export type ConflictDetailResponse = AdminApiComponents["schemas"]["ConflictDetailResponse"];
export type CandidateListResponse = AdminApiComponents["schemas"]["CandidateListResponse"];
export type ConflictState = AdminApiComponents["schemas"]["ConflictState"];
export type ConflictType = AdminApiComponents["schemas"]["ConflictType"];

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
  identifier_type?: IdentifierType;
  scope_key?: string;
  farm_id?: string;
  park_id?: string;
  location_id?: string;
  status?: string;
};

export type CountSearchParams = {
  grain: CounterGrain;
  limit: number;
  cursor?: string;
};

export type ConflictSearchParams = {
  limit: number;
  cursor?: string;
  state?: ConflictState;
  conflict_type?: ConflictType;
};

export type CandidateSearchParams = {
  limit: number;
  cursor?: string;
};

function getServerConfig(requireTenant = false): ApiResult<ServerConfig> {
  const missing: string[] = [];
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080";
  const bearerToken = process.env.GOATOS_BEARER_TOKEN;
  const tenantId = process.env.GOATOS_TENANT_ID;

  if (!bearerToken) {
    missing.push("GOATOS_BEARER_TOKEN");
  }
  if (requireTenant && !tenantId) {
    missing.push("GOATOS_TENANT_ID");
  }
  if (missing.length > 0) {
    return {
      ok: false,
      error: {
        kind: "missing_config",
        message: `Server configuration missing: ${missing.join(", ")}`,
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
    hasBearerToken: Boolean(process.env.GOATOS_BEARER_TOKEN),
    hasTenantId: Boolean(process.env.GOATOS_TENANT_ID),
  };
}

export async function searchGoats(params: HerdSearchParams): Promise<ApiResult<GoatSearchResponse>> {
  const config = getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<GoatSearchResponse>("/goats/search", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getGoatPassport(goatId: string): Promise<ApiResult<GoatPassportResponse>> {
  const config = getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/goats/${encodeURIComponent(goatId)}` as keyof AppApiPaths & string;
  return request(() => client.request<GoatPassportResponse>(path, { cache: "no-store" }));
}

export async function getIdentityCounts(params: CountSearchParams): Promise<ApiResult<IdentityCountsResponse>> {
  const config = getServerConfig(true);
  if (!config.ok) return config;
  const client = createAnalyticsApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<IdentityCountsResponse>("/analytics/identity/counts", {
      cache: "no-store",
      query: compactQuery({ ...params, tenant_id: config.data.tenantId }),
    }),
  );
}

export async function listConflicts(params: ConflictSearchParams): Promise<ApiResult<ConflictListResponse>> {
  const config = getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<ConflictListResponse>("/admin/identity/conflicts", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getConflictDetail(conflictId: string): Promise<ApiResult<ConflictDetailResponse>> {
  const config = getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/conflicts/${encodeURIComponent(conflictId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<ConflictDetailResponse>(path, { cache: "no-store" }));
}

export async function listCandidates(params: CandidateSearchParams): Promise<ApiResult<CandidateListResponse>> {
  const config = getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<CandidateListResponse>("/admin/identity/candidates", {
      cache: "no-store",
      query: compactQuery(params),
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
        message: "Bearer authentication failed. Mint a local dev token and keep it in the server environment.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403 && code === "tenant_scope_mismatch") {
      return {
        kind: "tenant_scope_mismatch",
        status: error.status,
        code,
        message: "GOATOS_TENANT_ID does not match the bearer token tenant.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403) {
      return {
        kind: "permission_denied",
        status: error.status,
        code: code ?? "permission_denied",
        message: "Token is valid, but the active DB grants do not allow this view.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 400) {
      return {
        kind: "bad_request",
        status: error.status,
        code,
        message: envelope?.message ?? "The backend rejected this query. Check filters, cursor, tenant, and limit.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 404) {
      return {
        kind: "not_found",
        status: error.status,
        code,
        message: envelope?.message ?? "Not found, or this token is not allowed to view it.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    return {
      kind: "api_error",
      status: error.status,
      code,
      message: envelope?.message ?? `Goat OS API returned ${error.status}.`,
      traceId: envelope?.trace_id,
      retryable: envelope?.retryable,
    };
  }
  if (error instanceof TypeError) {
    return {
      kind: "backend_down",
      message: "Backend API is not reachable from the admin-web server.",
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
