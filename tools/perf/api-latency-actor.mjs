// Observe the server-authenticated identity using the exact benchmark credentials.
// Never persist credentials, JWTs, cookies, or the full profile response.
export async function readApiActor({ baseUrl, tenantId, bearerToken = "", cookie = "", timeoutMs = 30000, fetchImpl = fetch }) {
  // Backend JWT tenant takes precedence over the tenant header. Cookie-only
  // credentials cannot establish that binding, so certification fails closed.
  let claims;
  try {
    claims = JSON.parse(Buffer.from(bearerToken.split(".")[1], "base64url").toString());
  } catch { throw new Error("API actor certification requires a readable bearer JWT; cookie-only identity is unsupported"); }
  if (!claims || typeof claims !== "object" || Array.isArray(claims)) throw new Error("API actor certification requires JWT claims");
  if (claims.tenant_id && claims.tenant_id !== tenantId) throw new Error("API actor JWT tenant differs from requested tenant");
  const response = await fetchImpl(`${baseUrl}/app/me`, {
    headers: {
      Accept: "application/json", "X-GoatOS-Tenant-ID": tenantId,
      ...(bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}),
      ...(cookie ? { Cookie: cookie } : {}),
    },
    cache: "no-store", redirect: "error", signal: AbortSignal.timeout(timeoutMs),
  });
  if (!response.ok) throw new Error(`API actor lookup /app/me HTTP ${response.status}`);
  const body = await response.json();
  if (typeof body.actor_id !== "string" || !body.actor_id.trim()
    || typeof tenantId !== "string" || !tenantId.trim()) throw new Error("API /app/me authenticated actor identity is missing");
  return { user_id: body.actor_id, tenant_id: tenantId };
}

export function actorEvidenceFailures(report) {
  if (typeof report?.actor?.user_id !== "string" || !report.actor.user_id.trim()
    || typeof report?.actor?.tenant_id !== "string" || !report.actor.tenant_id.trim()
    || report.actor.tenant_id !== report.tenant_id || report.actor_identity_source !== "/app/me") {
    return ["authenticated actor identity must come from /app/me and match report tenant_id"];
  }
  return [];
}

export function assertNoActorHeaderOverrides(headers = {}) {
  for (const name of Object.keys(headers)) {
    if (["authorization", "cookie", "x-goatos-tenant-id", "x-goatos-actor-id"].includes(name.toLowerCase())) {
      throw new Error("endpoint headers must not override authenticated actor context");
    }
  }
}
