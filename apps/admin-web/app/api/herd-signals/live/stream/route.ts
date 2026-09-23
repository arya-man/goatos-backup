import { NextResponse, type NextRequest } from "next/server";
import { getServerConfig } from "@/lib/api/server";

const NO_STORE = { "Cache-Control": "no-store" } as const;

export async function GET(request: NextRequest): Promise<Response> {
  const config = await getServerConfig(true);
  if (!config.ok) {
    const status = config.error.status ?? (config.error.kind === "unauthorized" ? 401 : 503);
    return NextResponse.json(
      { error: config.error.kind === "unauthorized" ? "unauthorized" : "herd_signals_unreachable", message: config.error.message },
      { status, headers: NO_STORE },
    );
  }

  const upstreamUrl = new URL(`${config.data.baseUrl}/herd-signals/live/stream`);
  request.nextUrl.searchParams.forEach((value, key) => {
    upstreamUrl.searchParams.append(key, value);
  });

  const headers: Record<string, string> = {
    Authorization: `Bearer ${config.data.bearerToken}`,
    Accept: "text/event-stream, application/json",
  };
  if (config.data.tenantId) headers["X-GoatOS-Tenant-ID"] = config.data.tenantId;
  if (config.data.traceparent) headers.traceparent = config.data.traceparent;

  let upstream: Response;
  try {
    upstream = await fetch(upstreamUrl, { method: "GET", headers, cache: "no-store", signal: request.signal });
  } catch (error: unknown) {
    return NextResponse.json(
      {
        error: "herd_signals_unreachable",
        message: error instanceof Error ? error.message : "Herd Signals stream is not reachable.",
      },
      { status: 503, headers: NO_STORE },
    );
  }

  const contentType = upstream.headers.get("Content-Type") ?? "";
  if (contentType.includes("text/event-stream") && upstream.body) {
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "Content-Type": "text/event-stream; charset=utf-8",
        "Cache-Control": "no-store, no-transform",
        Connection: "keep-alive",
        "X-Accel-Buffering": "no",
      },
    });
  }

  const text = await upstream.text();
  return new Response(text, {
    status: upstream.status,
    headers: { "Content-Type": contentType || "application/json", ...NO_STORE },
  });
}
