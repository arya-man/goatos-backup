'use client';

import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { getShiftingColor, SHIFTING_THRESHOLDS, SHIFTING_RED } from "@/lib/display-utils";

interface ShiftingRecord {
  rowNum: number;
  goatId: string;
  stage: string;
  daysInStage: number;
}

const FARM_TABS = ["CBE", "CPT"] as const;
type FarmTab = (typeof FARM_TABS)[number];

const STAGE_TABS = ["K1", "K2", "K3"] as const;
type StageFilter = (typeof STAGE_TABS)[number];

const API_URL: Record<FarmTab, string> = {
  CBE: "/api/shiftings",
  CPT: "/api/shiftings/cpt",
};

export default function ShiftingsPage() {
  const [farmTab, setFarmTab] = useState<FarmTab>("CBE");
  const [stageFilter, setStageFilter] = useState<StageFilter>("K1");

  const { data: allRecords = [], isLoading, error, refetch } = useQuery<ShiftingRecord[]>({
    queryKey: ["shiftings", farmTab],
    queryFn: async () => {
      const res = await fetch(API_URL[farmTab]);
      if (!res.ok) throw new Error("Failed to fetch shiftings");
      const json = await res.json();
      return json.data;
    },
    staleTime: 15 * 60 * 1000,
  });

  const overdueSummary = useMemo(() => {
    return STAGE_TABS.map((stage) => {
      const threshold = SHIFTING_THRESHOLDS[stage] ?? 0;
      const overdue = allRecords.filter(
        (r) => r.stage === stage && r.daysInStage > threshold
      ).length;
      return { stage, threshold, overdue };
    });
  }, [allRecords]);

  const records = useMemo(
    () => allRecords.filter((r) => r.stage === stageFilter),
    [allRecords, stageFilter]
  );

  const maxDays = Math.max(...records.map((r) => r.daysInStage), 1);

  return (
    <div className="flex flex-col h-full gap-6">
      {/* Farm toggle */}
      <div className="flex flex-wrap items-center gap-1 shrink-0">
        {FARM_TABS.map((f) => (
          <button
            key={f}
            onClick={() => setFarmTab(f)}
            className={
              farmTab === f
                ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
            }
          >
            {f}
          </button>
        ))}
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorState message={`Failed to load ${farmTab} shiftings data`} onRetry={() => refetch()} />}

      {!isLoading && !error && (
        <div className="flex flex-col flex-1 gap-6 min-h-0">
          {/* Overdue summary */}
          <div className="grid grid-cols-3 gap-3 shrink-0">
            {overdueSummary.map(({ stage, threshold, overdue }) => (
              <div
                key={stage}
                className="rounded-xl border border-[#334155] bg-[#1A1D24] p-3 transition-colors hover:border-[#334155]"
              >
                <div className="flex items-center gap-1.5">
                  <AlertTriangle size={12} style={{ color: overdue > 0 ? SHIFTING_RED : "#A0A0A0" }} />
                  <p className="text-[10px] uppercase tracking-wider text-[#8899AA]">
                    {stage} Overdue <span className="normal-case">(&gt;{threshold}d)</span>
                  </p>
                </div>
                <p
                  className="text-lg font-bold mt-1"
                  style={{ color: overdue > 0 ? SHIFTING_RED : "#C0C0C0" }}
                >
                  {overdue}
                </p>
              </div>
            ))}
          </div>

          {/* Stage tabs */}
          <div className="flex flex-wrap items-center gap-1 shrink-0">
            {STAGE_TABS.map((s) => (
              <button
                key={s}
                onClick={() => setStageFilter(s)}
                className={
                  stageFilter === s
                    ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                    : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
                }
              >
                {s}
              </button>
            ))}
          </div>

          {/* Table — fills remaining height, rows scroll inside */}
          <div className="flex-1 min-h-0 overflow-auto rounded-xl border border-[#334155]">
            <table className="w-full text-xs" style={{ minWidth: 420 }}>
              <thead className="sticky top-0 z-10">
                <tr className="border-b border-[#334155] bg-[#1A1D24]">
                  <th className="px-3 py-2.5 text-left font-medium text-[#8899AA] w-12 whitespace-nowrap">#</th>
                  <th className="px-3 py-2.5 text-left font-medium text-[#8899AA] whitespace-nowrap">Goat ID</th>
                  <th className="px-3 py-2.5 text-left font-medium text-[#8899AA] whitespace-nowrap">Stage</th>
                  <th className="px-3 py-2.5 text-left font-medium text-[#8899AA] whitespace-nowrap">Days in Current Stage</th>
                </tr>
              </thead>
              <tbody>
                {records.length === 0 && (
                  <tr>
                    <td colSpan={4} className="px-3 py-8 text-center text-[#8899AA]">
                      No records for {stageFilter}
                    </td>
                  </tr>
                )}
                {records.map((r, i) => {
                  const barPct = (r.daysInStage / maxDays) * 100;
                  return (
                    <tr
                      key={r.rowNum}
                      className="border-b border-[#334155] transition-colors hover:bg-[#22262E]/50"
                    >
                      <td className="px-3 py-2.5 text-[#8899AA]">{i + 1}</td>
                      <td className="px-3 py-2.5 text-[#B0BEC5] font-medium">{r.goatId}</td>
                      <td className="px-3 py-2.5">
                        <span className="inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium bg-[#22262E] text-[#B0BEC5]">
                          {r.stage}
                        </span>
                      </td>
                      <td className="px-3 py-2.5">
                        <div className="flex items-center gap-3">
                          <span className="font-medium w-8 text-right" style={{ color: getShiftingColor(r.stage, r.daysInStage) }}>{r.daysInStage}</span>
                          <div className="flex-1 h-1.5 rounded-full bg-[#22262E] max-w-[200px]">
                            <div
                              className="h-1.5 rounded-full transition-all"
                              style={{ width: `${barPct}%`, backgroundColor: getShiftingColor(r.stage, r.daysInStage) }}
                            />
                          </div>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
