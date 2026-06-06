import { useQuery } from "@tanstack/react-query";
import type { CountingRecord } from "@/lib/types";

interface CountsResponse {
  date: string;
  farm: string;
  data: CountingRecord[];
}

export function useCounts(farm?: string, date?: string) {
  const params = new URLSearchParams();
  if (farm) params.set("farm", farm);
  if (date) params.set("date", date);
  const qs = params.toString();

  return useQuery<CountsResponse>({
    queryKey: ["counts", farm, date],
    queryFn: async () => {
      const res = await fetch(`/api/counts${qs ? `?${qs}` : ""}`);
      if (!res.ok) throw new Error("Failed to fetch counts");
      return res.json();
    },
  });
}
