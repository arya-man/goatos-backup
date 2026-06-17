import { NextRequest } from "next/server";
import { getServerConfig, type ApiUiError } from "@/lib/api/server";

export const dynamic = "force-dynamic";

export async function GET(request: NextRequest) {
  const searchParams = request.nextUrl.searchParams;
  const importRunId = searchParams.get("import_run_id")?.trim();
  if (!importRunId) {
    return textError(400, "Missing import_run_id.");
  }

  const config = await getServerConfig();
  if (!config.ok) {
    return apiError(config.error);
  }

  const backendUrl = new URL(`/admin/import-runs/${encodeURIComponent(importRunId)}/rows.csv`, config.data.baseUrl);
  for (const key of ["scope", "processing_state", "reason_code"]) {
    const value = searchParams.get(key)?.trim();
    if (value) backendUrl.searchParams.set(key, value);
  }

  const backendResponse = await fetch(backendUrl, {
    cache: "no-store",
    headers: {
      Authorization: `Bearer ${config.data.bearerToken}`,
    },
  });

  if (!backendResponse.ok) {
    return new Response(await backendResponse.text(), {
      status: backendResponse.status,
      headers: {
        "Cache-Control": "no-store",
        "Content-Type": backendResponse.headers.get("Content-Type") ?? "application/json",
        "X-Content-Type-Options": "nosniff",
      },
    });
  }

  return new Response(backendResponse.body, {
    status: backendResponse.status,
    headers: {
      "Cache-Control": "no-store",
      "Content-Disposition": backendResponse.headers.get("Content-Disposition") ?? `attachment; filename="mesha-import-review-${importRunId}.csv"`,
      "Content-Type": backendResponse.headers.get("Content-Type") ?? "text/csv; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
  });
}

function apiError(error: ApiUiError) {
  return Response.json(
    {
      code: error.code ?? error.kind,
      message: error.message,
      trace_id: error.traceId,
    },
    { status: error.status ?? (error.kind === "missing_config" ? 500 : 502) },
  );
}

function textError(status: number, message: string) {
  return new Response(message, {
    status,
    headers: {
      "Cache-Control": "no-store",
      "Content-Type": "text/plain; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
