import { useQuery } from "@tanstack/react-query";

interface PurchaseCostItem {
  label: string;
  costPerKg: number;
}

export function usePurchaseCost(type: string = "goats") {
  return useQuery<PurchaseCostItem[]>({
    queryKey: ["purchase-cost", type],
    queryFn: async () => {
      const res = await fetch(`/api/purchase-cost?type=${type}`);
      if (!res.ok) throw new Error("Failed to fetch purchase costs");
      const json = await res.json();
      return json.data;
    },
  });
}
