import { NextResponse, type NextRequest } from "next/server";
import { getServerConfig } from "@/lib/api/server";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

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

  const upstreamUrl = new URL(`${config.data.baseUrl}/herd-signals/live`);
  request.nextUrl.searchParams.forEach((value, key) => {
    upstreamUrl.searchParams.append(key, value);
  });

  const headers: Record<string, string> = {
    Authorization: `Bearer ${config.data.bearerToken}`,
    Accept: "application/json",
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
        message: error instanceof Error ? error.message : "Herd Signals live data is not reachable.",
      },
      { status: 503, headers: NO_STORE },
    );
  }

  const text = await upstream.text();
  return new Response(text, {
    status: upstream.status,
    headers: {
      "Content-Type": upstream.headers.get("Content-Type") ?? "application/json",
      ...NO_STORE,
    },
  });
}
