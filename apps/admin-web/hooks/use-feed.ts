import { useQuery } from "@tanstack/react-query";
import type { FeedLoadRecord, SeasonRecord } from "@/lib/types";

export function useFeedStock(farm: string) {
  return useQuery<FeedLoadRecord[]>({
    queryKey: ["feed-stock", farm],
    queryFn: async () => {
      const res = await fetch(`/api/feed?farm=${farm}`);
      if (!res.ok) throw new Error("Failed to fetch feed stock");
      const json = await res.json();
      return json.data;
    },
  });
}

export function useSeasons() {
  return useQuery<SeasonRecord[]>({
    queryKey: ["seasons"],
    queryFn: async () => {
      const res = await fetch("/api/feed?type=seasons");
      if (!res.ok) throw new Error("Failed to fetch seasons");
      const json = await res.json();
      return json.data;
    },
  });
}
