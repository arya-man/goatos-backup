import { useQuery } from "@tanstack/react-query";

interface SummaryPeriod {
  births: number;
  deaths: number;
  purchases: number;
  sales: number;
  abortions: number;
}

interface SummaryData {
  daily: SummaryPeriod;
  weekly: SummaryPeriod;
  monthly: SummaryPeriod;
}

export function useSummary() {
  return useQuery<SummaryData>({
    queryKey: ["summary"],
    queryFn: async () => {
      const res = await fetch("/api/summary");
      if (!res.ok) throw new Error("Failed to fetch summary");
      const json = await res.json();
      return json.data;
    },
  });
}
