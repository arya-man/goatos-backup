export type { paths as AdminApiPaths, components as AdminApiComponents } from "./generated/admin-api";
export type { paths as AnalyticsApiPaths, components as AnalyticsApiComponents } from "./generated/analytics-api";
export type { paths as AppApiPaths, components as AppApiComponents } from "./generated/app-api";

import type { paths as AdminApiPaths } from "./generated/admin-api";
import type { paths as AnalyticsApiPaths } from "./generated/analytics-api";
import type { paths as AppApiPaths } from "./generated/app-api";
import { AUTH_SESSION_USER_AGENT_HEADER, TENANT_CONTEXT_HEADER } from "./constants.js";

export { AUTH_SESSION_USER_AGENT_HEADER, TENANT_CONTEXT_HEADER } from "./constants.js";

export type ApiFamily = "app" | "admin" | "analytics";

export type GoatOSApiPaths = {
  app: AppApiPaths;
  admin: AdminApiPaths;
  analytics: AnalyticsApiPaths;
};

export type RequestOptions = Omit<RequestInit, "body" | "headers"> & {
  headers?: HeadersInit;
  query?: Record<string, string | number | boolean | null | undefined>;
  body?: unknown;
};

export type GoatOSClientOptions = {
  baseUrl: string;
  bearerToken?: string;
  tenantId?: string;
  fetchImpl?: typeof fetch;
  defaultHeaders?: HeadersInit;
};

export class GoatOSApiError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, body: unknown) {
    super(`Goat OS API request failed with status ${status}`);
    this.name = "GoatOSApiError";
    this.status = status;
    this.body = body;
  }
}

export type GoatOSClient<Paths> = {
  request<Response = unknown>(path: keyof Paths & string, options?: RequestOptions): Promise<Response>;
};

export function createGoatOSClient<Paths>(options: GoatOSClientOptions): GoatOSClient<Paths> {
  const baseUrl = options.baseUrl.replace(/\/+$/, "");
  const fetchImpl = options.fetchImpl ?? fetch;

  return {
    async request<Response = unknown>(path: keyof Paths & string, requestOptions: RequestOptions = {}) {
      const url = new URL(path, `${baseUrl}/`);
      for (const [key, value] of Object.entries(requestOptions.query ?? {})) {
        if (value !== null && value !== undefined) {
          url.searchParams.set(key, String(value));
        }
      }

      const headers = new Headers(options.defaultHeaders);
      for (const [key, value] of new Headers(requestOptions.headers)) {
        headers.set(key, value);
      }
      if (options.tenantId) {
        headers.set(TENANT_CONTEXT_HEADER, options.tenantId);
      }
      if (options.bearerToken) {
        headers.set("Authorization", `Bearer ${options.bearerToken}`);
      }

      let body: BodyInit | undefined;
      if (requestOptions.body !== undefined) {
        headers.set("Content-Type", headers.get("Content-Type") ?? "application/json");
        body = JSON.stringify(requestOptions.body);
      }

      const response = await fetchImpl(url, {
        ...requestOptions,
        headers,
        body,
      });

      const responseBody = await parseResponse(response);
      if (!response.ok) {
        throw new GoatOSApiError(response.status, responseBody);
      }
      return responseBody as Response;
    },
  };
}

export function createAppApiClient(options: GoatOSClientOptions): GoatOSClient<AppApiPaths> {
  return createGoatOSClient<AppApiPaths>(options);
}

export function createAdminApiClient(options: GoatOSClientOptions): GoatOSClient<AdminApiPaths> {
  return createGoatOSClient<AdminApiPaths>(options);
}

export function createAnalyticsApiClient(options: GoatOSClientOptions): GoatOSClient<AnalyticsApiPaths> {
  return createGoatOSClient<AnalyticsApiPaths>(options);
}

async function parseResponse(response: Response): Promise<unknown> {
  if (response.status === 204) {
    return null;
  }
  const text = await response.text();
  if (text === "") {
    return null;
  }
  const contentType = response.headers.get("Content-Type") ?? "";
  if (contentType.includes("application/json")) {
    return JSON.parse(text);
  }
  return text;
}
