'use client';

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";

type Farm = "CBE" | "CPT";
type BabyType = "twins" | "triplets";

interface ParentRow {
  goatid: string;
  farm: string;
  breed: string;
  date: string;
  no_of_babies: number;
  kid_ids: string;
}

export default function ParentStockPage() {
  const [farm, setFarm] = useState<Farm>("CBE");
  const [babyType, setBabyType] = useState<BabyType>("twins");

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["parent-stock"],
    queryFn: () => fetch("/api/parent-stock").then((r) => r.json()).then((j) => j.data as ParentRow[]),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load parent stock data" onRetry={() => refetch()} />;

  const babyCount = babyType === "twins" ? 2 : 3;
  const filtered = (data ?? []).filter(
    (r) => r.farm === farm && r.no_of_babies === babyCount
  );

  return (
    <div className="flex flex-col h-full gap-6">
      <div className="space-y-2 shrink-0">
        <div className="flex flex-wrap items-center gap-1">
          {(["CBE", "CPT"] as Farm[]).map((f) => (
            <button
              key={f}
              onClick={() => setFarm(f)}
              className={
                farm === f
                  ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                  : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
              }
            >
              {f}
            </button>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-1">
          {([
            { key: "twins" as BabyType, label: "Twins" },
            { key: "triplets" as BabyType, label: "Triplets" },
          ]).map((t) => (
            <button
              key={t.key}
              onClick={() => setBabyType(t.key)}
              className={
                babyType === t.key
                  ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-xs font-medium text-[#14F1D9]"
                  : "rounded-lg px-3 py-1.5 text-xs font-medium text-[#8899AA] hover:text-[#14F1D9]"
              }
            >
              {t.label}
            </button>
          ))}
          <span className="text-xs text-[#8899AA] ml-2">{filtered.length} records</span>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-auto rounded-xl border border-[#334155]">
        <table className="w-full text-xs" style={{ minWidth: 440 }}>
          <thead className="sticky top-0 z-10">
            <tr className="border-b border-[#334155] bg-[#1A1D24]">
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Farm</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Mother ID</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Breed</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Kid IDs</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 ? (
              <tr>
                <td colSpan={4} className="px-3 py-8 text-center text-[#8899AA]">
                  No {babyType} records found for {farm}
                </td>
              </tr>
            ) : (
              filtered.map((r, i) => (
                <tr
                  key={`${r.goatid}-${i}`}
                  className="border-b border-[#334155] last:border-0 transition-colors hover:bg-[#22262E]/50"
                >
                  <td className="px-3 py-2.5 text-[#8899AA]">{r.farm}</td>
                  <td className="px-3 py-2.5 text-[#B0BEC5] font-medium">{r.goatid}</td>
                  <td className="px-3 py-2.5 text-[#8899AA]">{r.breed}</td>
                  <td className="px-3 py-2.5 text-[#8899AA]">{r.kid_ids}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );

}
