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
export type CountsView = AnalyticsApiComponents["schemas"]["CountsView"];
export type CountsDashboardResponse = AnalyticsApiComponents["schemas"]["CountsDashboardResponse"];
export type MortalityPeriod = AnalyticsApiComponents["schemas"]["MortalityPeriod"];
export type MortalityDashboardResponse = AnalyticsApiComponents["schemas"]["MortalityDashboardResponse"];
export type CreateCountsSyncRunRequest = AdminApiComponents["schemas"]["CreateCountsSyncRunRequest"];
export type CountsSyncRunResponse = AdminApiComponents["schemas"]["CountsSyncRunResponse"];
export type CreateMortalitySyncRunRequest = AdminApiComponents["schemas"]["CreateMortalitySyncRunRequest"];
export type MortalitySyncRunResponse = AdminApiComponents["schemas"]["MortalitySyncRunResponse"];

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
export type LocationListResponse = AdminApiComponents["schemas"]["LocationListResponse"];
export type LocationResponse = AdminApiComponents["schemas"]["LocationResponse"];
export type LocationMutationResponse = AdminApiComponents["schemas"]["LocationMutationResponse"];
export type LocationDeleteResponse = AdminApiComponents["schemas"]["LocationDeleteResponse"];
export type LocationAliasListResponse = AdminApiComponents["schemas"]["LocationAliasListResponse"];
export type LocationAliasResponse = AdminApiComponents["schemas"]["LocationAliasResponse"];
export type LocationCapacityListResponse = AdminApiComponents["schemas"]["LocationCapacityListResponse"];
export type LocationCapacityResponse = AdminApiComponents["schemas"]["LocationCapacityResponse"];
export type LocationReviewListResponse = AdminApiComponents["schemas"]["LocationReviewListResponse"];
export type LocationReviewItemResponse = AdminApiComponents["schemas"]["LocationReviewItemResponse"];
export type LocationUsageResponse = AdminApiComponents["schemas"]["LocationUsageResponse"];
export type CreateLocationRequestBody = AdminApiComponents["schemas"]["CreateLocationRequest"];
export type UpdateLocationRequestBody = AdminApiComponents["schemas"]["UpdateLocationRequest"];
export type RetireLocationRequestBody = AdminApiComponents["schemas"]["RetireLocationRequest"];
export type DeleteLocationRequestBody = AdminApiComponents["schemas"]["DeleteLocationRequest"];
export type CreateLocationAliasRequestBody = AdminApiComponents["schemas"]["CreateLocationAliasRequest"];
export type UpdateLocationAliasRequestBody = AdminApiComponents["schemas"]["UpdateLocationAliasRequest"];
export type RetireLocationAliasRequestBody = AdminApiComponents["schemas"]["RetireLocationAliasRequest"];
export type DeleteLocationAliasRequestBody = AdminApiComponents["schemas"]["DeleteLocationAliasRequest"];
export type CreateLocationCapacityRequestBody = AdminApiComponents["schemas"]["CreateLocationCapacityRequest"];
export type UpdateLocationCapacityRequestBody = AdminApiComponents["schemas"]["UpdateLocationCapacityRequest"];
export type DeleteLocationCapacityRequestBody = AdminApiComponents["schemas"]["DeleteLocationCapacityRequest"];
export type CreateLocationReviewItemRequestBody = AdminApiComponents["schemas"]["CreateLocationReviewItemRequest"];
export type ResolveLocationReviewItemRequestBody = AdminApiComponents["schemas"]["ResolveLocationReviewItemRequest"];
export type OperatorListResponse = AdminApiComponents["schemas"]["OperatorListResponse"];
export type OperatorResponse = AdminApiComponents["schemas"]["OperatorResponse"];
export type GrantListResponse = AdminApiComponents["schemas"]["GrantListResponse"];
export type DeviceListResponse = AdminApiComponents["schemas"]["DeviceListResponse"];
export type SourceCandidateListResponse = AdminApiComponents["schemas"]["SourceCandidateListResponse"];
export type SourceCandidateResponse = AdminApiComponents["schemas"]["SourceCandidateResponse"];
export type StatusChangeRequest = AdminApiComponents["schemas"]["StatusChangeRequest"];
export type MapSourceCandidateRequest = AdminApiComponents["schemas"]["MapSourceCandidateRequest"];
export type RejectSourceCandidateRequest = AdminApiComponents["schemas"]["RejectSourceCandidateRequest"];
export type SOPListResponse = AdminApiComponents["schemas"]["SOPListResponse"];
export type SOPResponse = AdminApiComponents["schemas"]["SOPResponse"];
export type SOPVersionResponse = AdminApiComponents["schemas"]["SOPVersionResponse"];
export type DryRunResponse = AdminApiComponents["schemas"]["DryRunResponse"];
export type TaskListResponse = AdminApiComponents["schemas"]["TaskListResponse"];
export type TaskResponse = AdminApiComponents["schemas"]["TaskResponse"];
export type CreateSOPRequest = AdminApiComponents["schemas"]["CreateSOPRequest"];
export type CreateSOPVersionRequest = AdminApiComponents["schemas"]["CreateSOPVersionRequest"];
export type RowVersionRequest = AdminApiComponents["schemas"]["RowVersionRequest"];
export type DryRunRequest = AdminApiComponents["schemas"]["DryRunRequest"];
export type CreateTaskRequest = AdminApiComponents["schemas"]["CreateTaskRequest"];
export type AssignTaskRequest = AdminApiComponents["schemas"]["AssignTaskRequest"];
export type ReviewTaskRequest = AdminApiComponents["schemas"]["ReviewTaskRequest"];

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

export type CountsDashboardParams = {
  view?: CountsView;
  snapshot_date?: string;
};

export type MortalityDashboardParams = {
  period?: MortalityPeriod;
};

export type LocationSearchParams = {
  limit?: number;
  offset?: number;
  type?: string;
  status?: string;
  parent_location_id?: string;
  search?: string;
  alias?: string;
};

export type LocationReviewSearchParams = {
  limit?: number;
  status?: string;
  review_type?: string;
};

export type OperatorSearchParams = {
  limit?: number;
  status?: string;
  role_hint?: string;
  location_id?: string;
  search?: string;
};

export type SourceCandidateSearchParams = {
  limit?: number;
  status?: string;
  source_system?: string;
};

export type SOPSearchParams = {
  limit?: number;
  status?: string;
};

export type TaskSearchParams = {
  limit?: number;
  state?: string;
  assigned_to?: string;
  scope_type?: string;
  scope_id?: string;
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

export async function createCorrectionRequest(
  body: CreateCorrectionRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<CorrectionRequestResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
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
  const client = createAnalyticsApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<IdentityCountsResponse>("/analytics/identity/counts", {
      cache: "no-store",
      query: compactQuery({ ...params, tenant_id: config.data.tenantId }),
    }),
  );
}

export async function getCountsDashboard(params: CountsDashboardParams = {}): Promise<ApiResult<CountsDashboardResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAnalyticsApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CountsDashboardResponse>("/analytics/counts/dashboard", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getMortalityDashboard(params: MortalityDashboardParams = {}): Promise<ApiResult<MortalityDashboardResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAnalyticsApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MortalityDashboardResponse>("/analytics/mortality/dashboard", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function createCountsSyncRun(
  body: CreateCountsSyncRunRequest,
  idempotencyKey: string,
): Promise<ApiResult<CountsSyncRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CountsSyncRunResponse>("/admin/counts/sync-runs", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getCountsSyncRun(syncRunId: string): Promise<ApiResult<CountsSyncRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/counts/sync-runs/${encodeURIComponent(syncRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<CountsSyncRunResponse>(path, { cache: "no-store" }));
}

export async function createMortalitySyncRun(
  body: CreateMortalitySyncRunRequest,
  idempotencyKey: string,
): Promise<ApiResult<MortalitySyncRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MortalitySyncRunResponse>("/admin/mortality/sync-runs", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getMortalitySyncRun(syncRunId: string): Promise<ApiResult<MortalitySyncRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/mortality/sync-runs/${encodeURIComponent(syncRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<MortalitySyncRunResponse>(path, { cache: "no-store" }));
}

export async function listLocations(params: LocationSearchParams = {}): Promise<ApiResult<LocationListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationListResponse>("/admin/locations", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getLocation(locationId: string): Promise<ApiResult<LocationResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<LocationResponse>(path, { cache: "no-store" }));
}

export async function createLocation(
  body: CreateLocationRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationMutationResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationMutationResponse>("/admin/locations", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function updateLocation(
  locationId: string,
  body: UpdateLocationRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationMutationResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationMutationResponse>(path, {
      method: "PATCH",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function retireLocation(
  locationId: string,
  body: RetireLocationRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationMutationResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationMutationResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function deleteLocation(
  locationId: string,
  body: DeleteLocationRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationDeleteResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationDeleteResponse>(path, {
      method: "DELETE",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listLocationChildren(locationId: string, limit = 100): Promise<ApiResult<LocationListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/children` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationListResponse>(path, {
      cache: "no-store",
      query: compactQuery({ limit }),
    }),
  );
}

export async function getLocationUsage(locationId: string): Promise<ApiResult<LocationUsageResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/usage` as keyof AdminApiPaths & string;
  return request(() => client.request<LocationUsageResponse>(path, { cache: "no-store" }));
}

export async function listLocationAliases(locationId: string, limit = 100): Promise<ApiResult<LocationAliasListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/aliases` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationAliasListResponse>(path, {
      cache: "no-store",
      query: compactQuery({ limit }),
    }),
  );
}

export async function createLocationAlias(
  locationId: string,
  body: CreateLocationAliasRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationAliasResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/aliases` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationAliasResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function updateLocationAlias(
  locationId: string,
  aliasId: string,
  body: UpdateLocationAliasRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationAliasResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/aliases/${encodeURIComponent(aliasId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationAliasResponse>(path, {
      method: "PATCH",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function retireLocationAlias(
  locationId: string,
  aliasId: string,
  body: RetireLocationAliasRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationAliasResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/aliases/${encodeURIComponent(aliasId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationAliasResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function deleteLocationAlias(
  locationId: string,
  aliasId: string,
  body: DeleteLocationAliasRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationDeleteResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/aliases/${encodeURIComponent(aliasId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationDeleteResponse>(path, {
      method: "DELETE",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listLocationCapacity(locationId: string, limit = 100): Promise<ApiResult<LocationCapacityListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/capacity` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationCapacityListResponse>(path, {
      cache: "no-store",
      query: compactQuery({ limit }),
    }),
  );
}

export async function createLocationCapacity(
  locationId: string,
  body: CreateLocationCapacityRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationCapacityResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/capacity` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationCapacityResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function updateLocationCapacity(
  locationId: string,
  capacityRecordId: string,
  body: UpdateLocationCapacityRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationCapacityResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/capacity/${encodeURIComponent(capacityRecordId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationCapacityResponse>(path, {
      method: "PATCH",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function deleteLocationCapacity(
  locationId: string,
  capacityRecordId: string,
  body: DeleteLocationCapacityRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationDeleteResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/locations/${encodeURIComponent(locationId)}/capacity/${encodeURIComponent(capacityRecordId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationDeleteResponse>(path, {
      method: "DELETE",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listLocationReviewItems(params: LocationReviewSearchParams = {}): Promise<ApiResult<LocationReviewListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationReviewListResponse>("/admin/location-review-items", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function createLocationReviewItem(
  body: CreateLocationReviewItemRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationReviewItemResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationReviewItemResponse>("/admin/location-review-items", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function resolveLocationReviewItem(
  reviewId: string,
  body: ResolveLocationReviewItemRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<LocationReviewItemResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/location-review-items/${encodeURIComponent(reviewId)}/resolve` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<LocationReviewItemResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listOperators(params: OperatorSearchParams = {}): Promise<ApiResult<OperatorListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OperatorListResponse>("/admin/operators", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getOperator(operatorId: string): Promise<ApiResult<OperatorResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operators/${encodeURIComponent(operatorId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<OperatorResponse>(path, { cache: "no-store" }));
}

export async function listOperatorGrants(operatorId: string): Promise<ApiResult<GrantListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operators/${encodeURIComponent(operatorId)}/grants` as keyof AdminApiPaths & string;
  return request(() => client.request<GrantListResponse>(path, { cache: "no-store" }));
}

export async function listOperatorDevices(operatorId: string): Promise<ApiResult<DeviceListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operators/${encodeURIComponent(operatorId)}/devices` as keyof AdminApiPaths & string;
  return request(() => client.request<DeviceListResponse>(path, { cache: "no-store" }));
}

export async function activateOperator(operatorId: string, body: StatusChangeRequest): Promise<ApiResult<OperatorResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operators/${encodeURIComponent(operatorId)}/activate` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<OperatorResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function deactivateOperator(operatorId: string, body: StatusChangeRequest): Promise<ApiResult<OperatorResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operators/${encodeURIComponent(operatorId)}/deactivate` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<OperatorResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function listOperatorSourceCandidates(params: SourceCandidateSearchParams = {}): Promise<ApiResult<SourceCandidateListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SourceCandidateListResponse>("/admin/operator-source-candidates", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function mapOperatorSourceCandidate(candidateId: string, body: MapSourceCandidateRequest): Promise<ApiResult<SourceCandidateResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operator-source-candidates/${encodeURIComponent(candidateId)}/map` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SourceCandidateResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function rejectOperatorSourceCandidate(candidateId: string, body: RejectSourceCandidateRequest): Promise<ApiResult<SourceCandidateResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/operator-source-candidates/${encodeURIComponent(candidateId)}/reject` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SourceCandidateResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function listSOPs(params: SOPSearchParams = {}): Promise<ApiResult<SOPListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SOPListResponse>("/admin/sops", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getSOP(sopId: string): Promise<ApiResult<SOPResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<SOPResponse>(path, { cache: "no-store" }));
}

export async function createSOP(body: CreateSOPRequest): Promise<ApiResult<SOPResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SOPResponse>("/admin/sops", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function createSOPVersion(sopId: string, body: CreateSOPVersionRequest): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}/versions` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SOPVersionResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function dryRunSOPVersion(sopId: string, sopVersionId: string, body: DryRunRequest): Promise<ApiResult<DryRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(sopVersionId)}/dry-run` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<DryRunResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function publishSOPVersion(sopId: string, sopVersionId: string, body: RowVersionRequest): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(sopVersionId)}/publish` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SOPVersionResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function retireSOPVersion(sopId: string, sopVersionId: string, body: RowVersionRequest): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(sopVersionId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SOPVersionResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function listTasks(params: TaskSearchParams = {}): Promise<ApiResult<TaskListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<TaskListResponse>("/admin/tasks", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getTask(taskId: string): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<TaskResponse>(path, { cache: "no-store" }));
}

export async function createTask(body: CreateTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<TaskResponse>("/admin/tasks", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function assignTask(taskId: string, body: AssignTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/assign` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<TaskResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function verifyTask(taskId: string, body: ReviewTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/verify` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<TaskResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function reworkTask(taskId: string, body: ReviewTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/rework` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<TaskResponse>(path, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function getReviewSummary(): Promise<ApiResult<ReviewSummaryResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ReviewSummaryResponse>("/admin/identity/review-summary", { cache: "no-store" }),
  );
}

export async function listConflicts(params: ConflictSearchParams): Promise<ApiResult<ConflictListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/import-runs/${encodeURIComponent(importRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<ImportRunResponse>(path, { cache: "no-store" }));
}

export async function listImportRuns(params: ImportRunsParams): Promise<ApiResult<ImportRunListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LegacySyncOverallStatusResponse>("/admin/legacy-sync/status", { cache: "no-store" }),
  );
}

export async function listLegacySyncRuns(params: LegacySyncRunsParams): Promise<ApiResult<LegacySyncRunListResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/legacy-sync/runs/${encodeURIComponent(syncRunId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<LegacySyncRunDetailResponse>(path, { cache: "no-store" }));
}

export async function createLegacySyncRun(body: CreateLegacySyncRunRequest): Promise<ApiResult<CreateLegacySyncRunResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
  const client = createAdminApiClient(apiClientOptions(config.data));
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
