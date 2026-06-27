import "server-only";

// Parks list for the top-bar scope control. Fetched server-side (in AdminShell) from the admin locations
// read API and passed to the client shell so the top bar can show a human park label while the URL/API
// carry the backend-safe location UUID. On any error it returns [] — the shell then shows the raw scope id
// behind a "Selected park" label rather than crashing.
import { createAdminApiClient } from "@goatos/api-client";
import type { AdminApiComponents } from "@goatos/api-client";
import { apiClientOptions, compactQuery, getServerConfig, request } from "@/lib/api/server";
import type { Park } from "@/lib/scope";

type LocationSummary = AdminApiComponents["schemas"]["LocationSummary"];
type LocationListResponse = AdminApiComponents["schemas"]["LocationListResponse"];

export async function getScopeParks(): Promise<Park[]> {
  const config = await getServerConfig(true);
  if (!config.ok) return [];
  const client = createAdminApiClient(apiClientOptions(config.data));
  const res = await request(() =>
    client.request<LocationListResponse>("/admin/locations", {
      cache: "no-store",
      query: compactQuery({ type: "park", status: "active", limit: 500 }),
    }),
  );
  if (!res.ok) return [];
  return res.data.items.map((l: LocationSummary) => ({ id: l.location_id, code: l.location_code, name: l.name }));
}
