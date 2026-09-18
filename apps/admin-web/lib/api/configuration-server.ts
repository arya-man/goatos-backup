import "server-only";

// Server-only generated-client fetchers for CONFIGURATION -> ITEMS AND SETTINGS (maintainer
// instruction 2026-09-18): the farm's reference registers. Reads ride configuration.read; every
// write rides configuration.write on the route table, so this module only forwards the body and
// reports the outcome. Same ApiResult envelope and helpers as lib/api/server.ts.
import { createAdminApiClient } from "@goatos/api-client";
import type { AdminApiComponents, AdminApiPaths } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type ConfigurationRegister = AdminApiComponents["schemas"]["ConfigurationRegister"];
export type ConfigurationColumn = AdminApiComponents["schemas"]["ConfigurationColumn"];
export type ConfigurationOption = AdminApiComponents["schemas"]["ConfigurationOption"];
export type ConfigurationGroup = AdminApiComponents["schemas"]["ConfigurationGroup"];
export type ConfigurationRow = AdminApiComponents["schemas"]["ConfigurationRow"];
export type ConfigurationRefOption = AdminApiComponents["schemas"]["ConfigurationRefOption"];
export type ConfigurationUsage = AdminApiComponents["schemas"]["ConfigurationUsage"];
export type ConfigurationRegistersResponse = AdminApiComponents["schemas"]["ConfigurationRegistersResponse"];
export type ConfigurationListResponse = AdminApiComponents["schemas"]["ConfigurationListResponse"];
export type ConfigurationRowResponse = AdminApiComponents["schemas"]["ConfigurationRowResponse"];
export type ConfigurationOptionsResponse = AdminApiComponents["schemas"]["ConfigurationOptionsResponse"];
export type ConfigurationUsageResponse = AdminApiComponents["schemas"]["ConfigurationUsageResponse"];
export type ConfigurationRowWrite = AdminApiComponents["schemas"]["ConfigurationRowWrite"];
export type ConfigurationStatusWrite = AdminApiComponents["schemas"]["ConfigurationStatusWrite"];
export type ConfigurationFieldError = AdminApiComponents["schemas"]["ConfigurationFieldError"];

function idempotentHeaders(idempotencyKey: string) {
  return { "Idempotency-Key": idempotencyKey };
}

function registerPath(register: string, suffix = ""): keyof AdminApiPaths & string {
  return `/admin/configuration/${encodeURIComponent(register)}${suffix}` as keyof AdminApiPaths & string;
}

function rowPath(register: string, rowId: string, suffix = ""): keyof AdminApiPaths & string {
  return `/admin/configuration/${encodeURIComponent(register)}/${encodeURIComponent(rowId)}${suffix}` as keyof AdminApiPaths & string;
}

/** The register catalog (definitions the page renders from) and the rail counts. */
export async function listConfigurationRegisters(): Promise<ApiResult<ConfigurationRegistersResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationRegistersResponse>("/admin/configuration/registers", { cache: "no-store" }));
}

/** One keyset page of a register; `filters` are column key -> value, sent as `f.<column>`. */
export async function listConfigurationRows(
  register: string,
  params: { status?: string; q?: string; cursor?: string; limit?: number; filters?: Record<string, string> } = {},
): Promise<ApiResult<ConfigurationListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const query: Record<string, string | number | undefined> = { status: params.status, q: params.q, cursor: params.cursor, limit: params.limit };
  for (const [key, value] of Object.entries(params.filters ?? {})) if (value) query[`f.${key}`] = value;
  return request(() => client.request<ConfigurationListResponse>(registerPath(register), { cache: "no-store", query: compactQuery(query) }));
}

/** The catalogue layout's Lists panel: every category, active and archived, with its counts. */
export async function listCatalogueLists(): Promise<ApiResult<ConfigurationListResponse>> {
  return listConfigurationRows("categories", { status: "all", limit: 200 });
}

/** One reference_lists row: the list a dynamic `ref:<key>` register renders. */
export async function getReferenceList(listKey: string): Promise<ApiResult<ConfigurationRowResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationRowResponse>(rowPath("reference_lists", listKey), { cache: "no-store" }));
}

/** Every active row of a register as ref choices for a drawer select. */
export async function listConfigurationOptions(register: string): Promise<ApiResult<ConfigurationOptionsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationOptionsResponse>(registerPath(register, "/options"), { cache: "no-store" }));
}

export async function getConfigurationRowUsage(register: string, rowId: string): Promise<ApiResult<ConfigurationUsageResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationUsageResponse>(rowPath(register, rowId, "/usage"), { cache: "no-store" }));
}

export async function createConfigurationRow(register: string, body: ConfigurationRowWrite, idempotencyKey: string): Promise<ApiResult<ConfigurationRowResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationRowResponse>(registerPath(register), { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }));
}

export async function updateConfigurationRow(register: string, rowId: string, body: ConfigurationRowWrite, idempotencyKey: string): Promise<ApiResult<ConfigurationRowResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationRowResponse>(rowPath(register, rowId), { method: "PUT", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }));
}

export async function setConfigurationRowStatus(register: string, rowId: string, body: ConfigurationStatusWrite, idempotencyKey: string): Promise<ApiResult<ConfigurationRowResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<ConfigurationRowResponse>(rowPath(register, rowId, "/status"), { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }));
}

export async function deleteConfigurationRow(register: string, rowId: string, rowVersion: number, idempotencyKey: string): Promise<ApiResult<void>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<void>(rowPath(register, rowId), { method: "DELETE", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body: { row_version: rowVersion } }));
}
