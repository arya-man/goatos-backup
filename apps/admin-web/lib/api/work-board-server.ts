import "server-only";

// Server-only reads for the cross-module Work Board (GET /work-board/rows, /work-board/summary).
// Same ApiResult envelope and helpers as lib/api/server.ts; no client fetch, no route handler.
// Scope is decided by the backend (park through grants, modules through the caller's own
// permissions, owner through work_board.oversee); this module only forwards the filters.
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiComponents } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type WorkBoardRow = AppApiComponents["schemas"]["WorkBoardRow"];
export type WorkBoardRowsPage = AppApiComponents["schemas"]["WorkBoardRowsPage"];
export type WorkBoardSummary = AppApiComponents["schemas"]["WorkBoardSummary"];
export type WorkBoardModule = AppApiComponents["schemas"]["WorkBoardModule"];
export type WorkBoardLane = AppApiComponents["schemas"]["WorkBoardLane"];
export type WorkBoardWorkState = AppApiComponents["schemas"]["WorkBoardWorkState"];

export type WorkBoardScope = {
  park: string;
  businessDate?: string;
  modules?: string[];
  states?: string[];
  owner?: string;
};

function scopeQuery(scope: WorkBoardScope) {
  return {
    park: scope.park,
    business_date: scope.businessDate,
    module: scope.modules && scope.modules.length ? scope.modules.join(",") : undefined,
    state: scope.states && scope.states.length ? scope.states.join(",") : undefined,
    owner: scope.owner,
  };
}

export async function listWorkBoardRows(
  scope: WorkBoardScope,
  page: { limit?: number; cursor?: string } = {},
): Promise<ApiResult<WorkBoardRowsPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WorkBoardRowsPage>("/work-board/rows", {
      cache: "no-store",
      query: compactQuery({ ...scopeQuery(scope), limit: page.limit ?? 25, cursor: page.cursor }),
    }),
  );
}

export async function getWorkBoardSummary(scope: WorkBoardScope): Promise<ApiResult<WorkBoardSummary>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WorkBoardSummary>("/work-board/summary", {
      cache: "no-store",
      query: compactQuery(scopeQuery(scope)),
    }),
  );
}
