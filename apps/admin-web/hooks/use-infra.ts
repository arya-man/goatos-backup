import { useQuery } from "@tanstack/react-query";
import type { CountingRecord } from "@/lib/types";

export function useInfra(farm: string) {
  return useQuery<CountingRecord[]>({
    queryKey: ["infra", farm],
    queryFn: async () => {
      const res = await fetch(`/api/infra?farm=${farm}`);
      if (!res.ok) throw new Error("Failed to fetch infra data");
      const json = await res.json();
      return json.data;
    },
  });
}
