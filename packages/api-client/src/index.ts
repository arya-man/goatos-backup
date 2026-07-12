export type { paths as AdminApiPaths, components as AdminApiComponents } from "./generated/admin-api.js";
export type { paths as AnalyticsApiPaths, components as AnalyticsApiComponents } from "./generated/analytics-api.js";
export type { paths as AppApiPaths, components as AppApiComponents } from "./generated/app-api.js";

import type { paths as AdminApiPaths } from "./generated/admin-api.js";
import type { paths as AnalyticsApiPaths } from "./generated/analytics-api.js";
import type { paths as AppApiPaths } from "./generated/app-api.js";
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
  /**
   * Optional pluggable hook returning W3C trace-context headers (e.g. `traceparent`) to attach
   * to every request. This keeps the generated client decoupled from any specific tracing SDK
   * (Faro, OTel, etc.) — the caller supplies whatever active trace context it has (for example,
   * admin-web forwards the incoming request's `traceparent` header, which Faro's browser fetch
   * instrumentation attached on the browser -> Next.js hop) so the backend span chains onto the
   * same trace. Return `undefined` when no trace context is active.
   */
  getTraceHeaders?: () => HeadersInit | undefined;
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
  requestWithResponse<Response = unknown>(path: keyof Paths & string, options?: RequestOptions): Promise<{
    data: Response | null;
    response: globalThis.Response;
  }>;
};

export function createGoatOSClient<Paths>(options: GoatOSClientOptions): GoatOSClient<Paths> {
  const baseUrl = options.baseUrl.replace(/\/+$/, "");
  const fetchImpl = options.fetchImpl ?? fetch;

  return {
    async request<Response = unknown>(path: keyof Paths & string, requestOptions: RequestOptions = {}) {
      const result = await this.requestWithResponse<Response>(path, requestOptions);
      return result.data as Response;
    },
    async requestWithResponse<Response = unknown>(path: keyof Paths & string, requestOptions: RequestOptions = {}) {
      const url = new URL(path, `${baseUrl}/`);
      for (const [key, value] of Object.entries(requestOptions.query ?? {})) {
        if (value !== null && value !== undefined) {
          url.searchParams.set(key, String(value));
        }
      }

      const headers = new Headers(options.defaultHeaders);
      if (options.getTraceHeaders) {
        const traceHeaders = options.getTraceHeaders();
        if (traceHeaders) {
          for (const [key, value] of new Headers(traceHeaders)) {
            headers.set(key, value);
          }
        }
      }
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
      const { body: _body, query: _query, ...fetchOptions } = requestOptions;
      const init: RequestInit = { ...fetchOptions, headers };
      if (body !== undefined) {
        init.body = body;
      }

      const response = await fetchImpl(url, init);

      const responseBody = await parseResponse(response);
      if (!response.ok && response.status !== 304) {
        throw new GoatOSApiError(response.status, responseBody);
      }
      return { data: responseBody as Response | null, response };
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
