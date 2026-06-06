import { useQuery } from "@tanstack/react-query";

interface FatteningData {
  stages: unknown[];
  totals: unknown;
  adg: { overall: number; cbe: number; cpt: number };
  adgByGenderBreed: unknown[];
  adgByBreed: unknown[];
  adgByStatus: unknown[];
  adgByGender: unknown[];
  adgByBreedStatus: unknown[];
  kidsWeighedPct: unknown[];
  loadADGByBreed: unknown[];
  avgWeightByLoad: unknown[];
  last5Weighings: unknown[];
  weightChanges: unknown[];
}

export function useFattening() {
  return useQuery<FatteningData>({
    queryKey: ["fattening"],
    queryFn: async () => {
      const res = await fetch("/api/fattening");
      if (!res.ok) throw new Error("Failed to fetch fattening data");
      const json = await res.json();
      return json.data;
    },
  });
}
