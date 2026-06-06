import { useQuery } from "@tanstack/react-query";

interface BirthsData {
  kpi: { totalBirths: number; avgKidsPerMother: number };
  kiddingEfficiency: { breed: string; pct: number }[];
  birthCountLast10Days: { date: string; count: number }[];
  kiddingFrequency: { breed: string; months: number }[];
  litterSize: { breed: string; size: number }[];
}

export function useBirths() {
  return useQuery<BirthsData>({
    queryKey: ["births"],
    queryFn: async () => {
      const res = await fetch("/api/births");
      if (!res.ok) throw new Error("Failed to fetch births data");
      const json = await res.json();
      return json.data;
    },
  });
}
