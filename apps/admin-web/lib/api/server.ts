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
import { getFirebaseIdTokenCookie } from "@/lib/auth/server-session";

type ErrorEnvelope = AppApiComponents["schemas"]["ErrorEnvelope"];

export type GoatSummary = AppApiComponents["schemas"]["GoatSummary"];
export type GoatPassportResponse = AppApiComponents["schemas"]["GoatPassportResponse"];
export type GoatSearchResponse = AppApiComponents["schemas"]["GoatSearchResponse"];
export type GoatTimelineResponse = AppApiComponents["schemas"]["GoatTimelineResponse"];
export type CorrectionRequestResponse = AppApiComponents["schemas"]["CorrectionRequestResponse"];
export type IdentifierType = AppApiComponents["schemas"]["IdentifierType"];
export type CreateCorrectionRequestBody = AppApiComponents["schemas"]["CreateCorrectionRequest"];

export type CounterGrain = AnalyticsApiComponents["schemas"]["CounterGrain"];
export type IdentityCountsResponse = AnalyticsApiComponents["schemas"]["IdentityCountsResponse"];

export type ReviewSummaryResponse = AdminApiComponents["schemas"]["ReviewSummaryResponse"];
export type ConflictListResponse = AdminApiComponents["schemas"]["ConflictListResponse"];
export type ConflictDetailResponse = AdminApiComponents["schemas"]["ConflictDetailResponse"];
export type ResolveConflictRequestBody = AdminApiComponents["schemas"]["ResolveConflictRequest"];
export type ResolveConflictResponse = AdminApiComponents["schemas"]["ResolveConflictResponse"];
export type CandidateListResponse = AdminApiComponents["schemas"]["CandidateListResponse"];
export type ReviewCandidateRequestBody = AdminApiComponents["schemas"]["ReviewCandidateRequest"];
export type ApproveCandidateRequestBody = AdminApiComponents["schemas"]["ApproveCandidateRequest"];
export type CandidateDecisionResponse = AdminApiComponents["schemas"]["CandidateDecisionResponse"];
export type ImportRunResponse = AdminApiComponents["schemas"]["ImportRunResponse"];
export type ImportRunListResponse = AdminApiComponents["schemas"]["ImportRunListResponse"];
export type ImportRunRow = AdminApiComponents["schemas"]["ImportRunRow"];
export type ImportRunRowsResponse = AdminApiComponents["schemas"]["ImportRunRowsResponse"];
export type LegacySyncDomain = AdminApiComponents["schemas"]["LegacySyncDomain"];
export type LegacySyncMode = AdminApiComponents["schemas"]["LegacySyncMode"];
export type LegacySyncSource = AdminApiComponents["schemas"]["LegacySyncSource"];
export type LegacySyncOverallStatusResponse = AdminApiComponents["schemas"]["LegacySyncOverallStatusResponse"];
export type LegacySyncRun = AdminApiComponents["schemas"]["LegacySyncRun"];
export type LegacySyncRunListResponse = AdminApiComponents["schemas"]["LegacySyncRunListResponse"];
export type LegacySyncRunDetailResponse = AdminApiComponents["schemas"]["LegacySyncRunDetailResponse"];
export type CreateLegacySyncRunRequest = AdminApiComponents["schemas"]["CreateLegacySyncRunRequest"];
export type CreateLegacySyncRunResponse = AdminApiComponents["schemas"]["CreateLegacySyncRunResponse"];
export type CancelLegacySyncRunResponse = AdminApiComponents["schemas"]["CancelLegacySyncRunResponse"];
export type AdminCorrectionRequestListResponse = AdminApiComponents["schemas"]["CorrectionRequestListResponse"];
export type AdminCorrectionRequestResponse = AdminApiComponents["schemas"]["CorrectionRequestResponse"];
export type ResolveCorrectionRequestBody = AdminApiComponents["schemas"]["ResolveCorrectionRequest"];
export type AdminGoatResponse = AdminApiComponents["schemas"]["AdminGoatResponse"];
export type AddIdentifierRequestBody = AdminApiComponents["schemas"]["AddIdentifierRequest"];
export type RetireIdentifierRequestBody = AdminApiComponents["schemas"]["RetireIdentifierRequest"];
export type ConflictState = AdminApiComponents["schemas"]["ConflictState"];
export type ConflictType = AdminApiComponents["schemas"]["ConflictType"];
export type ReviewGroup = AdminApiComponents["schemas"]["ReviewGroup"];
export type BulkResolveConflictsRequest = AdminApiComponents["schemas"]["BulkResolveConflictsRequest"];
export type BulkResolveConflictItem = AdminApiComponents["schemas"]["BulkResolveConflictItem"];
export type BulkResolveConflictsResult = AdminApiComponents["schemas"]["BulkResolveConflictsResult"];
export type ImportRowState = AdminApiComponents["schemas"]["ImportRowState"];
export type ReviewImportRowRequestBody = AdminApiComponents["schemas"]["ReviewImportRowRequest"];
export type ReviewImportRowResponse = AdminApiComponents["schemas"]["ReviewImportRowResponse"];
export type CorrectionRequestState = AdminApiComponents["schemas"]["CorrectionRequestState"];

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

export type CountSearchParams = {
  grain: CounterGrain;
  limit: number;
  cursor?: string;
  custodian_party_id?: string;
  farm_id?: string;
  park_id?: string;
  shed_id?: string;
  cohort_id?: string;
  lifecycle_status?: string;
  reproductive_status?: string;
  growth_cohort_tag?: string;
  management_stage?: string;
  health_status?: string;
  identity_state?: AnalyticsApiComponents["schemas"]["IdentityState"];
  breed_id?: string;
  sex?: string;
};

export type ConflictSearchParams = {
  limit: number;
  cursor?: string;
  state?: ConflictState;
  conflict_type?: ConflictType;
  review_group?: ReviewGroup;
};

export type CandidateSearchParams = {
  limit: number;
  cursor?: string;
};

export type GoatTimelineParams = {
  goatId: string;
  limit: number;
  cursor?: string;
};

export type ImportRunRowsParams = {
  importRunId: string;
  limit: number;
  cursor?: string;
  processing_state?: ImportRowState;
  reason_code?: string;
};

export type ImportRunsParams = {
  limit: number;
};

export type LegacySyncRunsParams = {
  limit: number;
};

export type CorrectionRequestSearchParams = {
  limit: number;
  cursor?: string;
  state?: CorrectionRequestState;
};

export async function getServerConfig(requireTenant = false): Promise<ApiResult<ServerConfig>> {
  const missing: string[] = [];
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080";
  const firebaseIdToken = await getFirebaseIdTokenCookie();
  const localBearerToken =
    process.env.GOATOS_ENV === "local" && process.env.GOATOS_AUTH_MODE === "bearer"
      ? process.env.GOATOS_BEARER_TOKEN
      : undefined;
  const bearerToken = firebaseIdToken ?? localBearerToken;
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
    missing.push("GOATOS_TENANT_ID");
  }
  if (missing.length > 0) {
    const labels = missing.map((item) => (item === "GOATOS_TENANT_ID" ? "tenant id" : item));
    return {
      ok: false,
      error: {
        kind: "missing_config",
        message: `Server configuration missing: ${labels.join(", ")}.`,
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
    importRunId: process.env.GOATOS_IMPORT_RUN_ID?.trim() || null,
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
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/goats/${encodeURIComponent(goatId)}` as keyof AppApiPaths & string;
  return request(() => client.request<GoatPassportResponse>(path, { cache: "no-store" }));
}

export async function getGoatTimeline(params: GoatTimelineParams): Promise<ApiResult<GoatTimelineResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
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

export async function createCorrectionRequest(
  body: CreateCorrectionRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<CorrectionRequestResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<CorrectionRequestResponse>("/identity/correction-requests", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getIdentityCounts(params: CountSearchParams): Promise<ApiResult<IdentityCountsResponse>> {
  const config = await getServerConfig(true);
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

export async function getReviewSummary(): Promise<ApiResult<ReviewSummaryResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<ReviewSummaryResponse>("/admin/identity/review-summary", { cache: "no-store" }),
  );
}

export async function listConflicts(params: ConflictSearchParams): Promise<ApiResult<ConflictListResponse>> {
  const config = await getServerConfig();
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
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/conflicts/${encodeURIComponent(conflictId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<ConflictDetailResponse>(path, { cache: "no-store" }));
}

export async function resolveIdentityConflict(
  conflictId: string,
  body: ResolveConflictRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<ResolveConflictResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/conflicts/${encodeURIComponent(conflictId)}/resolve` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ResolveConflictResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function bulkResolveConflicts(body: BulkResolveConflictsRequest): Promise<ApiResult<BulkResolveConflictsResult>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<BulkResolveConflictsResult>("/admin/identity/conflicts/bulk-resolve", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function listCandidates(params: CandidateSearchParams): Promise<ApiResult<CandidateListResponse>> {
  const config = await getServerConfig();
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

export async function rejectIdentityCandidate(
  candidateId: string,
  body: ReviewCandidateRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<CandidateDecisionResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/candidates/${encodeURIComponent(candidateId)}/reject` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<CandidateDecisionResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function approveIdentityCandidate(
  candidateId: string,
  body: ApproveCandidateRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<CandidateDecisionResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/candidates/${encodeURIComponent(candidateId)}/approve` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<CandidateDecisionResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getImportRun(importRunId: string): Promise<ApiResult<ImportRunResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/import-runs/${encodeURIComponent(importRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<ImportRunResponse>(path, { cache: "no-store" }));
}

export async function listImportRuns(params: ImportRunsParams): Promise<ApiResult<ImportRunListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<ImportRunListResponse>("/admin/import-runs", {
      cache: "no-store",
      query: compactQuery({ limit: params.limit }),
    }),
  );
}

export async function listImportRunRows(params: ImportRunRowsParams): Promise<ApiResult<ImportRunRowsResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/import-runs/${encodeURIComponent(params.importRunId)}/rows` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ImportRunRowsResponse>(path, {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        cursor: params.cursor,
        processing_state: params.processing_state,
        reason_code: params.reason_code,
      }),
    }),
  );
}

export async function reviewImportRunRow(
  importRunId: string,
  importRowId: string,
  body: ReviewImportRowRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<ReviewImportRowResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path =
    `/admin/import-runs/${encodeURIComponent(importRunId)}/rows/${encodeURIComponent(importRowId)}/review` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ReviewImportRowResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getLegacySyncStatus(): Promise<ApiResult<LegacySyncOverallStatusResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<LegacySyncOverallStatusResponse>("/admin/legacy-sync/status", { cache: "no-store" }),
  );
}

export async function listLegacySyncRuns(params: LegacySyncRunsParams): Promise<ApiResult<LegacySyncRunListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<LegacySyncRunListResponse>("/admin/legacy-sync/runs", {
      cache: "no-store",
      query: compactQuery({ limit: params.limit }),
    }),
  );
}

export async function getLegacySyncRun(syncRunId: string): Promise<ApiResult<LegacySyncRunDetailResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/legacy-sync/runs/${encodeURIComponent(syncRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<LegacySyncRunDetailResponse>(path, { cache: "no-store" }));
}

export async function createLegacySyncRun(body: CreateLegacySyncRunRequest): Promise<ApiResult<CreateLegacySyncRunResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<CreateLegacySyncRunResponse>("/admin/legacy-sync/runs", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function cancelLegacySyncRun(syncRunId: string): Promise<ApiResult<CancelLegacySyncRunResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/legacy-sync/runs/${encodeURIComponent(syncRunId)}/cancel` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<CancelLegacySyncRunResponse>(path, {
      method: "POST",
      cache: "no-store",
    }),
  );
}

export async function adminListCorrectionRequests(params: CorrectionRequestSearchParams): Promise<ApiResult<AdminCorrectionRequestListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  return request(() =>
    client.request<AdminCorrectionRequestListResponse>("/admin/identity/correction-requests", {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        cursor: params.cursor,
        state: params.state,
      }),
    }),
  );
}

export async function resolveCorrectionRequest(
  correctionRequestId: string,
  body: ResolveCorrectionRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminCorrectionRequestResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
  const path = `/admin/identity/correction-requests/${encodeURIComponent(correctionRequestId)}/resolve` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminCorrectionRequestResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function addGoatIdentifier(
  goatId: string,
  body: AddIdentifierRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
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
  const client = createAdminApiClient({
    baseUrl: config.data.baseUrl,
    bearerToken: config.data.bearerToken,
  });
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
