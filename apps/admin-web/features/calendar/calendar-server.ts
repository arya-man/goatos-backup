import "server-only";

// Calendar vaccination read adapter — live generated client (@goatos/api-client).
//
// Mirrors the getVaccinationActionCenter pattern in lib/api/server.ts: getServerConfig → createAppApiClient
// → client.request<T>(...) through the shared request() envelope. Bounded params per the handoff API Plan
// (limit default 100/max 200; date_to defaults to date_from + 30d server-side, max inclusive 45d).
import {
  apiClientOptions,
  compactQuery,
  getServerConfig,
  request,
  type ApiResult,
} from "@/lib/api/server";
import { createAppApiClient, type AppApiComponents } from "@goatos/api-client";
import type { CalendarDriveTargetListResponse, CalendarEventDetail, CalendarEventListResponse, CalendarOwnerFilter, CalendarStatus } from "./calendar-contract";

type CalendarActionResponse = AppApiComponents["schemas"]["CalendarActionResponse"];
type CalendarNudgeRequest = AppApiComponents["schemas"]["CalendarNudgeRequest"];
type CalendarSnoozeRequest = AppApiComponents["schemas"]["CalendarSnoozeRequest"];

const DEFAULT_LIMIT = 100;
const MAX_LIMIT = 200;

export interface CalendarListParams {
  parkId?: string;
  shedId?: string;
  ownerKey?: CalendarOwnerFilter;
  status?: CalendarStatus;
  dateFrom?: string;
  dateTo?: string;
  cursor?: string;
  limit?: number;
  includeDateMarkers?: boolean;
}

export async function getCalendarVaccinationEvents(params: CalendarListParams = {}): Promise<ApiResult<CalendarEventListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CalendarEventListResponse>("/calendar/vaccination/events", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        owner_key: params.ownerKey,
        status: params.status,
        date_from: params.dateFrom,
        date_to: params.dateTo,
        cursor: params.cursor,
        limit: Math.min(params.limit ?? DEFAULT_LIMIT, MAX_LIMIT),
        include_date_markers: params.includeDateMarkers ? true : undefined,
      }),
    }),
  );
}

const DEFAULT_TARGET_LIMIT = 10;
const MAX_TARGET_LIMIT = 50;

export async function getCalendarVaccinationEventDetail(eventId: string): Promise<ApiResult<CalendarEventDetail>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/calendar/vaccination/events/${encodeURIComponent(eventId)}` as Parameters<typeof client.request>[0];
  return request(() => client.request<CalendarEventDetail>(path, { cache: "no-store" }));
}

export async function getCalendarDriveTargets(
  eventId: string,
  params: { cursor?: string; limit?: number } = {},
): Promise<ApiResult<CalendarDriveTargetListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/calendar/vaccination/events/${encodeURIComponent(eventId)}/targets` as Parameters<typeof client.request>[0];
  return request(() =>
    client.request<CalendarDriveTargetListResponse>(path, {
      cache: "no-store",
      query: compactQuery({
        cursor: params.cursor,
        limit: Math.min(params.limit ?? DEFAULT_TARGET_LIMIT, MAX_TARGET_LIMIT),
      }),
    }),
  );
}

// Nudge — idempotent reminder/escalation send through the backend NotificationGateway. The Idempotency-Key
// header (8–200 chars) makes an accidental double-submit a no-op replay.
export async function sendCalendarNudge(eventId: string, body: CalendarNudgeRequest, idempotencyKey: string): Promise<ApiResult<CalendarActionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/calendar/vaccination/events/${encodeURIComponent(eventId)}/nudge` as Parameters<typeof client.request>[0];
  return request(() => client.request<CalendarActionResponse>(path, { method: "POST", cache: "no-store", headers: { "Idempotency-Key": idempotencyKey }, body }));
}

// Snooze — records a durable snooze keyed to the underlying obligation/batch/task; never mutates obligation
// truth. Idempotent via the same header contract.
export async function snoozeCalendarEvent(eventId: string, body: CalendarSnoozeRequest, idempotencyKey: string): Promise<ApiResult<CalendarActionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/calendar/vaccination/events/${encodeURIComponent(eventId)}/snooze` as Parameters<typeof client.request>[0];
  return request(() => client.request<CalendarActionResponse>(path, { method: "POST", cache: "no-store", headers: { "Idempotency-Key": idempotencyKey }, body }));
}
