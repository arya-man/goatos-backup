import "server-only";

// Server-only reads for the cross-module Work Board (GET /work-board/rows, /work-board/summary).
// Same ApiResult envelope and helpers as lib/api/server.ts; no client fetch, no route handler.
// Scope is decided by the backend (park through grants, modules through the caller's own
// permissions, owner through work_board.oversee); this module only forwards the filters.
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiComponents, AppApiPaths } from "@goatos/api-client";
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
  // lane reads ONE column: the lane's states, so each column pages on its own cursor.
  lane?: string;
  owner?: string;
};

function scopeQuery(scope: WorkBoardScope) {
  return {
    park: scope.park,
    business_date: scope.businessDate,
    module: scope.modules && scope.modules.length ? scope.modules.join(",") : undefined,
    state: scope.states && scope.states.length ? scope.states.join(",") : undefined,
    lane: scope.lane,
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

export type WorkBoardFlagRequest = AppApiComponents["schemas"]["WorkBoardFlagRequest"];
export type WorkBoardFlagResult = AppApiComponents["schemas"]["WorkBoardFlagResult"];

// Raises a flag on a board row to the park head (POST /work-board/flags). The Idempotency-Key
// makes a retried submit one act.
export async function raiseWorkBoardFlag(body: WorkBoardFlagRequest, idempotencyKey: string): Promise<ApiResult<WorkBoardFlagResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WorkBoardFlagResult>("/work-board/flags", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export type WorkBoardSubtask = AppApiComponents["schemas"]["WorkBoardSubtask"];
export type WorkBoardSubtaskPage = AppApiComponents["schemas"]["WorkBoardSubtaskPage"];

// One page of a row's subtasks, worst first (GET /work-board/rows/{row_key}/subtasks). Read
// inside the issue view only, for the row that is open: the board list never fans out.
export async function listWorkBoardSubtasks(
  rowKey: string,
  scope: { park: string; businessDate?: string; owner?: string },
  page: { limit?: number; cursor?: string } = {},
): Promise<ApiResult<WorkBoardSubtaskPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/work-board/rows/${encodeURIComponent(rowKey)}/subtasks` as keyof AppApiPaths & string;
  return request(() =>
    client.request<WorkBoardSubtaskPage>(path, {
      cache: "no-store",
      query: compactQuery({ park: scope.park, business_date: scope.businessDate, owner: scope.owner, limit: page.limit ?? 10, cursor: page.cursor }),
    }),
  );
}
