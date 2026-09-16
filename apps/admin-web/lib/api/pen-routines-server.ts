import "server-only";

// Server-only generated-client fetchers for PEN ROUTINES (maintainer instruction 2026-09-16,
// docs/decisions/pen-routines.md): the rule the CXO writes per park -- scope, cadence, evidence,
// review, people -- and the tasks the kernel raised from it. Reads ride pen_routines.read; every
// write rides pen_routines.configure on the route table, so this module only forwards the body
// and reports the outcome. Same ApiResult envelope and helpers as lib/api/server.ts.
import { createAdminApiClient } from "@goatos/api-client";
import type { AdminApiComponents, AdminApiPaths } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type PenRoutineRow = AdminApiComponents["schemas"]["PenRoutineRow"];
export type PenRoutineWrite = AdminApiComponents["schemas"]["PenRoutineWrite"];
export type PenRoutineStatusWrite = AdminApiComponents["schemas"]["PenRoutineStatusWrite"];
export type PenRoutineCatalog = AdminApiComponents["schemas"]["PenRoutineCatalogResponse"];
export type PenRoutineCatalogPen = AdminApiComponents["schemas"]["PenRoutineCatalogPen"];
export type PenRoutinePerson = AdminApiComponents["schemas"]["PenRoutinePerson"];
export type PenRoutineKeyLabel = AdminApiComponents["schemas"]["PenRoutineKeyLabel"];
export type PenRoutineEvidence = AdminApiComponents["schemas"]["PenRoutineEvidence"];
export type PenRoutineQuestion = AdminApiComponents["schemas"]["PenRoutineQuestion"];
export type PenRoutineOption = AdminApiComponents["schemas"]["PenRoutineOption"];
export type PenRoutineProofRule = AdminApiComponents["schemas"]["PenRoutineProofRule"];
export type PenRoutineStep = AdminApiComponents["schemas"]["PenRoutineStep"];
export type PenRoutineTaskRow = AdminApiComponents["schemas"]["PenRoutineTaskRow"];
export type PenRoutineTaskSummary = AdminApiComponents["schemas"]["PenRoutineTaskSummary"];
export type PenRoutinePark = AdminApiComponents["schemas"]["PenRoutinePark"];
export type PenRoutineListResponse = AdminApiComponents["schemas"]["PenRoutineListResponse"];
export type PenRoutineDetailResponse = AdminApiComponents["schemas"]["PenRoutineDetailResponse"];
export type PenRoutineTaskListResponse = AdminApiComponents["schemas"]["PenRoutineTaskListResponse"];

function idempotentHeaders(idempotencyKey: string) {
  return { "Idempotency-Key": idempotencyKey };
}

/** The routines of one park, or of every park the caller may see when `park_id` is absent. */
export async function listPenRoutines(params: { park_id?: string } = {}): Promise<ApiResult<PenRoutineListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineListResponse>("/admin/pen-routines", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

/** The drawer's vocabulary for ONE park: its active pens, the people offered, and the closed enums. */
export async function getPenRoutineCatalog(parkId: string): Promise<ApiResult<PenRoutineCatalog>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineCatalog>("/admin/pen-routines/catalog", {
      cache: "no-store",
      query: { park_id: parkId },
    }),
  );
}

/** One keyset page of a park's tasks on one business day, plus the whole-filter summary. */
export async function listPenRoutineParkTasks(params: {
  park_id: string;
  business_date?: string;
  routine_id?: string;
  cursor?: string;
  limit?: number;
}): Promise<ApiResult<PenRoutineTaskListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineTaskListResponse>("/admin/pen-routines/tasks", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getPenRoutine(routineId: string): Promise<ApiResult<PenRoutineDetailResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineDetailResponse>(`/admin/pen-routines/${encodeURIComponent(routineId)}` as keyof AdminApiPaths & string, {
      cache: "no-store",
    }),
  );
}

export async function createPenRoutine(body: PenRoutineWrite, idempotencyKey: string): Promise<ApiResult<PenRoutineDetailResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineDetailResponse>("/admin/pen-routines", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

/** Every update is a new VERSION on the backend; `body.row_version` fences the drawer's read. */
export async function updatePenRoutine(
  routineId: string,
  body: PenRoutineWrite,
  idempotencyKey: string,
): Promise<ApiResult<PenRoutineDetailResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineDetailResponse>(`/admin/pen-routines/${encodeURIComponent(routineId)}` as keyof AdminApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function setPenRoutineStatus(
  routineId: string,
  body: PenRoutineStatusWrite,
  idempotencyKey: string,
): Promise<ApiResult<PenRoutineDetailResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PenRoutineDetailResponse>(
      `/admin/pen-routines/${encodeURIComponent(routineId)}/status` as keyof AdminApiPaths & string,
      {
        method: "POST",
        cache: "no-store",
        headers: idempotentHeaders(idempotencyKey),
        body,
      },
    ),
  );
}
