import { useQuery } from "@tanstack/react-query";

interface ShiftingRecord {
  rowNum: number;
  goatId: string;
  stage: string;
  daysInStage: number;
}

export function useShiftings() {
  return useQuery<ShiftingRecord[]>({
    queryKey: ["shiftings"],
    queryFn: async () => {
      const res = await fetch("/api/shiftings");
      if (!res.ok) throw new Error("Failed to fetch shiftings");
      const json = await res.json();
      return json.data;
    },
  });
}
