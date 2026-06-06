import { useQuery } from "@tanstack/react-query";

interface MortalityAllResponse {
  summary: Record<string, unknown>;
  breedwise: Record<string, unknown>[];
  farmwise: Record<string, unknown>[];
  loadwise: Record<string, unknown>[];
  delivery: Record<string, unknown>;
  trends: Record<string, unknown>;
}

/** Legacy hook: fetches all mortality data from the combined endpoint */
export function useMortality(period: string = "overall") {
  return useQuery<MortalityAllResponse>({
    queryKey: ["mortality", period],
    queryFn: async () => {
      const res = await fetch(`/api/mortality?period=${period}`);
      if (!res.ok) throw new Error("Failed to fetch mortality data");
      return res.json();
    },
  });
}

/** Granular hook: fetches from a specific mortality sub-route */
export function useMortalityEndpoint<T = unknown[]>(
  endpoint: string,
  params?: Record<string, string>
) {
  const qs = params ? new URLSearchParams(params).toString() : "";
  return useQuery<{ data: T }>({
    queryKey: ["mortality", endpoint, params],
    queryFn: async () => {
      const res = await fetch(`/api/mortality/${endpoint}${qs ? `?${qs}` : ""}`);
      if (!res.ok) throw new Error(`Failed to fetch mortality/${endpoint}`);
      return res.json();
    },
  });
}
