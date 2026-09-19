import "server-only";

// FARM VALUATION ASSUMPTIONS (maintainer instruction 2026-09-19): the decided figures Farm value
// and Load wise price the herd at, read and written on Sales Config. Typed by hand until the
// admin contract carries the schema; the wire shape is backend/internal/sales/adapters/http/
// valuation_handler.go.
import { createAppApiClient } from "@goatos/api-client";
import type { AppApiPaths } from "@goatos/api-client";
import { apiClientOptions, getServerConfig, request, type ApiResult } from "@/lib/api/server";

export type ValuationBucket = {
  bucket: string;
  label: string;
  fixed_weight_kg: number | null;
  price_per_kg: number;
  display_order: number;
};

export type ValuationAssumptions = {
  buckets: ValuationBucket[];
  sale_ready_kg: number;
  unsold_stock_price_rupees: number | null;
  row_version: number;
  updated_at?: string;
  updated_by_name?: string;
  limits: {
    price_per_kg_min: number;
    price_per_kg_max: number;
    fixed_weight_kg_min: number;
    fixed_weight_kg_max: number;
    sale_ready_kg_min: number;
    sale_ready_kg_max: number;
    unsold_stock_price_min: number;
    unsold_stock_price_max: number;
  };
};

export type ValuationWrite = Pick<ValuationAssumptions, "buckets" | "sale_ready_kg" | "unsold_stock_price_rupees" | "row_version">;

const PATH = "/sales/valuation-assumptions" as keyof AppApiPaths & string;

export async function getValuationAssumptions(): Promise<ApiResult<ValuationAssumptions>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<ValuationAssumptions>(PATH, { cache: "no-store" }));
}

export async function putValuationAssumptions(body: ValuationWrite): Promise<ApiResult<ValuationAssumptions>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<ValuationAssumptions>(PATH, { method: "PUT", cache: "no-store", body }));
}
