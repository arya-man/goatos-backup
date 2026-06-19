import { createAppApiClient, type AppApiComponents, type AppApiPaths } from "@goatos/api-client";

export type BootstrapResponse = AppApiComponents["schemas"]["BootstrapResponse"];
export type TaskListResponse = AppApiComponents["schemas"]["TaskListResponse"];
export type TaskResponse = AppApiComponents["schemas"]["TaskResponse"];
export type SOPVersionResponse = AppApiComponents["schemas"]["SOPVersionResponse"];
export type SubmitTaskRequest = AppApiComponents["schemas"]["SubmitTaskRequest"];
export type SubmissionResponse = AppApiComponents["schemas"]["SubmissionResponse"];

export type OperatorApiConfig = {
  baseUrl: string;
  bearerToken: string;
  tenantId: string;
};

export function createOperatorApi(config: OperatorApiConfig) {
  const client = createAppApiClient({
    baseUrl: config.baseUrl,
    bearerToken: config.bearerToken,
    tenantId: config.tenantId,
  });
  return {
    bootstrap(deviceId?: string) {
      return client.request<BootstrapResponse>("/app/bootstrap", deviceId ? { cache: "no-store", query: { device_id: deviceId } } : { cache: "no-store" });
    },
    listTasks(state?: string) {
      return client.request<TaskListResponse>("/app/tasks", state ? { cache: "no-store", query: { state } } : { cache: "no-store" });
    },
    getTask(taskId: string) {
      return client.request<TaskResponse>(`/app/tasks/${encodeURIComponent(taskId)}` as keyof AppApiPaths & string, { cache: "no-store" });
    },
    getSOPVersion(sopVersionId: string) {
      return client.request<SOPVersionResponse>(`/app/sop-versions/${encodeURIComponent(sopVersionId)}` as keyof AppApiPaths & string, { cache: "no-store" });
    },
    submitTask(taskId: string, body: SubmitTaskRequest) {
      return client.request<SubmissionResponse>(`/app/tasks/${encodeURIComponent(taskId)}/submissions` as keyof AppApiPaths & string, {
        method: "POST",
        cache: "no-store",
        body,
      });
    },
  };
}
