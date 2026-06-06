/* eslint-disable @typescript-eslint/no-explicit-any */
'use client';

import { useState } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";
import { useQuery } from "@tanstack/react-query";
import { Stethoscope, X } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber } from "@/lib/constants";

type FarmFilter = "total" | "cbe" | "cpt";

interface SickPerDayRow { date: string; count: number; }
interface DiseaseRow { name: string; value: number; }
interface SickGoat { goatId: string; problemId: string; problemName: string; farm: string; date: string; }

interface GoatsHealthData {
  kpi: { total: number; cbe: number; cpt: number };
  sickGoatsList: { total: SickGoat[]; cbe: SickGoat[]; cpt: SickGoat[] };
  sickPerDay: { total: SickPerDayRow[]; cbe: SickPerDayRow[]; cpt: SickPerDayRow[] };
  topDiseases: { total: DiseaseRow[]; cbe: DiseaseRow[]; cpt: DiseaseRow[] };
}

const farmLabels: Record<FarmFilter, string> = { total: "Total", cbe: "CBE", cpt: "CPT" };

export default function GoatsHealthPage() {
  const isMobile = useIsMobile();
  const [farm, setFarm] = useState<FarmFilter>("total");
  const [showModal, setShowModal] = useState(false);

  const { data, isLoading, error, refetch } = useQuery<GoatsHealthData>({
    queryKey: ["goats-health"],
    queryFn: () =>
      fetch("/api/goats-health")
        .then((r) => r.json())
        .then((j) => j.data),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load goats health data" onRetry={() => refetch()} />;

  const kpi = data?.kpi ?? { total: 0, cbe: 0, cpt: 0 };
  const sickGoatsList = data?.sickGoatsList ?? { total: [], cbe: [], cpt: [] };
  const sickPerDay = data?.sickPerDay ?? { total: [], cbe: [], cpt: [] };
  const topDiseases = data?.topDiseases ?? { total: [], cbe: [], cpt: [] };

  const currentSickCount = kpi[farm];
  const currentSickList = sickGoatsList[farm] ?? [];

  const currentSickPerDay = (() => {
    const dataMap = new Map<string, number>();
    for (const d of (sickPerDay[farm] ?? [])) {
      const key = typeof d.date === "object" ? (d.date as any).value : d.date;
      dataMap.set(String(key), d.count);
    }
    const days: { name: string; value: number }[] = [];
    for (let i = 9; i >= 0; i--) {
      const dt = new Date();
      dt.setDate(dt.getDate() - i);
      const yyyy = dt.getFullYear();
      const mm = String(dt.getMonth() + 1).padStart(2, "0");
      const dd = String(dt.getDate()).padStart(2, "0");
      const key = `${yyyy}-${mm}-${dd}`;
      days.push({ name: `${dd}-${mm}`, value: dataMap.get(key) ?? 0 });
    }
    return days;
  })();

  const currentTopDiseases = topDiseases[farm] ?? [];

  return (
    <div className="space-y-6">
      {/* Farm toggle */}
      <div className="flex flex-wrap items-center gap-1">
        {(["total", "cbe", "cpt"] as FarmFilter[]).map((f) => (
          <button
            key={f}
            onClick={() => setFarm(f)}
            className={
              farm === f
                ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
            }
          >
            {farmLabels[f]}
          </button>
        ))}
      </div>

      {/* KPI card below toggle */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <button onClick={() => setShowModal(true)} className="text-left focus:outline-none">
          <KPICard
            label="Currently Sick Goats"
            value={formatNumber(currentSickCount)}
            icon={<Stethoscope size={18} />}
            delay={0}
            variant="negative"
          />
        </button>
      </div>

      {/* New sick goats per day */}
      <ChartCard
        title="New Sick Goats per Day (Last 10 Days)"
        subtitle="Distinct goats newly recorded sick each day"
      >
        <VerticalBarChart data={currentSickPerDay} valueLabel="Sick Goats" showLabels={!isMobile} />
      </ChartCard>

      {/* Top diseases */}
      <ChartCard
        title="Most Reported Diseases (Last 10 Days)"
        subtitle="By number of recorded cases"
      >
        <HorizontalBarChart data={currentTopDiseases} valueLabel="Cases" showLabels={!isMobile} />
      </ChartCard>

      {/* Sick goats modal */}
      {showModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4"
          onClick={() => setShowModal(false)}
        >
          <div
            className="relative w-full max-w-2xl max-h-[80vh] flex flex-col rounded-xl border border-[#334155] bg-[#1A1D24] shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-center justify-between px-5 py-4 border-b border-[#334155]">
              <div>
                <p className="text-sm font-semibold text-[#FFFFFF]">
                  Currently Sick Goats — {farmLabels[farm]}
                </p>
                <p className="text-xs text-[#8899AA] mt-0.5">{currentSickList.length} active cases (status: Open)</p>
              </div>
              <button
                onClick={() => setShowModal(false)}
                className="flex h-8 w-8 items-center justify-center rounded-lg text-[#8899AA] hover:text-[#FFFFFF] hover:bg-[#22262E] transition-colors"
              >
                <X size={16} />
              </button>
            </div>

            {/* Table */}
            <div className="overflow-auto">
              <table className="w-full text-xs" style={{ minWidth: 500 }}>
                <thead className="sticky top-0 z-10 bg-[#1A1D24]">
                  <tr className="border-b border-[#334155]">
                    <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Goat ID</th>
                    <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Problem ID</th>
                    <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Problem Name</th>
                    <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Farm</th>
                    <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Date</th>
                  </tr>
                </thead>
                <tbody>
                  {currentSickList.length === 0 ? (
                    <tr>
                      <td colSpan={5} className="px-4 py-8 text-center text-[#8899AA]">No active cases found</td>
                    </tr>
                  ) : (
                    currentSickList.map((g, i) => (
                      <tr key={i} className="border-b border-[#334155]/50 hover:bg-[#22262E]/50 transition-colors">
                        <td className={`px-3 py-2.5 font-medium ${g.goatId === "No tag" ? "italic text-[#8899AA]" : "text-[#14F1D9]"}`}>{g.goatId}</td>
                        <td className="px-3 py-2.5 text-[#8899AA]">{g.problemId}</td>
                        <td className="px-3 py-2.5 text-[#B0BEC5]">{g.problemName}</td>
                        <td className="px-3 py-2.5 text-[#8899AA]">{g.farm}</td>
                        <td className="px-3 py-2.5 text-[#8899AA]">{g.date.split("-").reverse().join("/")}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
