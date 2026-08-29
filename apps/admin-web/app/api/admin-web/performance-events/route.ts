import { NextResponse } from "next/server";

type PerformanceEventBody = {
  event_name?: unknown;
  surface?: unknown;
  route?: unknown;
  occurred_at?: unknown;
  payload?: unknown;
};

const MAX_STRING = 500;
const MAX_ARRAY_ITEMS = 20;
const ALLOWED_EVENT_PREFIXES = [
  "feed_config_filter_apply_",
  "admin_route_",
  "admin_backend_api_",
];

export async function POST(request: Request) {
  let body: PerformanceEventBody;
  try {
    body = (await request.json()) as PerformanceEventBody;
  } catch {
    return NextResponse.json({ ok: false, error: "invalid_json" }, { status: 400 });
  }

  const eventName = boundedString(body.event_name);
  if (!eventName || !ALLOWED_EVENT_PREFIXES.some((prefix) => eventName.startsWith(prefix))) {
    return NextResponse.json({ ok: false, error: "unsupported_event" }, { status: 400 });
  }

  const payload = sanitizePayload(body.payload);
  console.info(JSON.stringify({
    severity: "INFO",
    message: eventName,
    event_name: eventName,
    surface: boundedString(body.surface),
    route: boundedString(body.route),
    occurred_at: boundedString(body.occurred_at),
    payload,
  }));

  return NextResponse.json({ ok: true });
}

function sanitizePayload(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const out: Record<string, unknown> = {};
  for (const [key, raw] of Object.entries(value)) {
    out[key] = sanitizeValue(raw);
  }
  return out;
}

function sanitizeValue(value: unknown): unknown {
  if (typeof value === "string") return boundedString(value);
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value === "boolean" || value === null) return value;
  if (Array.isArray(value)) return value.slice(0, MAX_ARRAY_ITEMS).map(sanitizeValue);
  return undefined;
}

function boundedString(value: unknown): string {
  if (typeof value !== "string") return "";
  const trimmed = value.trim();
  return trimmed.length > MAX_STRING ? trimmed.slice(0, MAX_STRING) : trimmed;
}
