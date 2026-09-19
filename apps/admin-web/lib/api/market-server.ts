import "server-only";

// Server-only generated-client fetchers for the MARKET SURVEY (maintainer decision 2026-09-14):
// the cities the procurement director phones each morning, the questions asked in each, and the
// prices recorded. Config and analytics live under Sales on admin-web; entry is the phone's
// Market tab. Same ApiResult envelope and helpers as lib/api/server.ts.
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type MarketCity = AppApiComponents["schemas"]["MarketCity"];
export type MarketQuestion = AppApiComponents["schemas"]["MarketQuestion"];
export type MarketConfig = AppApiComponents["schemas"]["MarketConfig"];
export type MarketCityWrite = AppApiComponents["schemas"]["MarketCityWrite"];
export type MarketQuestionWrite = AppApiComponents["schemas"]["MarketQuestionWrite"];
export type MarketAnalytics = AppApiComponents["schemas"]["MarketAnalytics"];
export type MarketSeries = AppApiComponents["schemas"]["MarketSeries"];
export type MarketLatestCell = AppApiComponents["schemas"]["MarketLatestCell"];
export type MarketSurveyDay = AppApiComponents["schemas"]["MarketSurveyDay"];
export type MarketCallTime = AppApiComponents["schemas"]["MarketCallTime"];

function idempotentHeaders(idempotencyKey: string) {
  return { "Idempotency-Key": idempotencyKey };
}

export async function getMarketConfig(): Promise<ApiResult<MarketConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<MarketConfig>("/market/config", { cache: "no-store" }));
}

export async function createMarketCity(body: MarketCityWrite, idempotencyKey: string): Promise<ApiResult<MarketCity>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketCity>("/market/cities", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function updateMarketCity(cityId: string, body: MarketCityWrite): Promise<ApiResult<MarketCity>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketCity>(`/market/cities/${encodeURIComponent(cityId)}` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

export async function createMarketQuestion(body: MarketQuestionWrite, idempotencyKey: string): Promise<ApiResult<MarketQuestion>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketQuestion>("/market/questions", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function updateMarketQuestion(questionId: string, body: MarketQuestionWrite): Promise<ApiResult<MarketQuestion>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketQuestion>(`/market/questions/${encodeURIComponent(questionId)}` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

/** The local IST time the day's calls open -- when the cards appear and the reminder goes. */
export async function setMarketCallTime(callTime: string): Promise<ApiResult<MarketCallTime>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketCallTime>("/market/config/call-time", {
      method: "PUT",
      cache: "no-store",
      body: { call_time: callTime },
    }),
  );
}

/** The analytics window. Blank bounds mean the backend's default (last 90 days ending today). */
export async function getMarketAnalytics(params: { from?: string; to?: string } = {}): Promise<ApiResult<MarketAnalytics>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MarketAnalytics>("/market/analytics", {
      cache: "no-store",
      query: compactQuery({ from: params.from, to: params.to }),
    }),
  );
}

/** Today's cards -- read on the analytics page for the "today's calls" tile. */
export async function getMarketSurveyDay(): Promise<ApiResult<MarketSurveyDay>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<MarketSurveyDay>("/app/market/survey", { cache: "no-store" }));
}

/** One person as the reporters list shows them (GET /market/reporters). */
export type MarketReporter = { person_id: string; display_name: string; title: string; park_label: string; reporter: boolean };

export async function getMarketReporters(): Promise<ApiResult<{ people: MarketReporter[] }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<{ people: MarketReporter[] }>("/market/reporters" as keyof AppApiPaths & string, { cache: "no-store" }));
}

export async function setMarketReporter(personId: string, enabled: boolean): Promise<ApiResult<{ people: MarketReporter[] }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<{ people: MarketReporter[] }>(`/market/reporters/${encodeURIComponent(personId)}` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body: { enabled },
    }),
  );
}
