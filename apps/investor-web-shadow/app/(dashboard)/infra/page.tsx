'use client';

import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { Building2, Users, DoorOpen } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber } from "@/lib/constants";
import { buildShedPartitions, groupByShedName, getVacancyStatus, getVacancyClasses } from "@/lib/data/infra";
import type { CountingRecord } from "@/lib/types";
import { cn } from "@/lib/utils";

export default function InfraPage() {
  const [farm, setFarm] = useState<"CBE" | "CPT">("CBE");

  const { data: records = [], isLoading, error, refetch } = useQuery<CountingRecord[]>({
    queryKey: ["infra", farm],
    queryFn: async () => {
      const res = await fetch(`/api/infra?farm=${farm}`);
      if (!res.ok) throw new Error("Failed to fetch infra data");
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  // Fetch real capacity & vacancy from BigQuery
  const { data: capacityData } = useQuery<{ capacity: number; vacancy: number }>({
    queryKey: ["infra-capacity", farm],
    queryFn: async () => {
      const res = await fetch(`/api/infra/capacity?farm=${farm}`);
      if (!res.ok) return { capacity: 0, vacancy: 0 };
      const json = await res.json();
      return json.data ?? { capacity: 0, vacancy: 0 };
    },
    staleTime: 15 * 60 * 1000,
  });

  const shedGroups = useMemo(() => {
    if (!records || !Array.isArray(records)) return [];
    const partitions = buildShedPartitions(records);
    return groupByShedName(partitions);
  }, [records]);

  const kpi = useMemo(() => {
    const count = shedGroups.reduce((s, g) => s + g.totalCount, 0);
    const capacity = capacityData?.capacity ?? 0;
    const vacancy = capacity - count;
    return { count, capacity, vacancy };
  }, [shedGroups, capacityData]);

  const vacancyPct = kpi.capacity > 0
    ? ((kpi.vacancy / kpi.capacity) * 100).toFixed(1)
    : "0";

  return (
    <div className="space-y-6">
      {/* Farm toggle */}
      <div className="flex items-center gap-1">
        {(["CBE", "CPT"] as const).map((f) => (
          <button
            key={f}
            onClick={() => setFarm(f)}
            className={
              farm === f
                ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
            }
          >
            {f}
          </button>
        ))}
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorState message="Failed to load infra data" onRetry={() => refetch()} />}

      {!isLoading && !error && (
        <>
          {/* KPI scorecards */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <KPICard
              label="Count"
              value={formatNumber(kpi.count)}
              icon={<Building2 size={18} />}
              delay={0}
            />
            <KPICard
              label="Capacity"
              value={formatNumber(kpi.capacity)}
              icon={<Users size={18} />}
              delay={1}
            />
            <KPICard
              label="Vacancy"
              value={formatNumber(kpi.vacancy)}
              icon={<DoorOpen size={18} />}
              delay={2}
              subtitle={`${vacancyPct}% of capacity`}
            />
          </div>

          {/* Shed groups */}
          <div className="space-y-6">
            {shedGroups.map((group) => (
              <section key={group.shedName} className="space-y-3">
                <div className="flex items-center gap-2">
                  <h2 className="font-semibold text-sm text-[#FFFFFF]">
                    {group.shedName}
                  </h2>
                  <span className="text-[#8899AA] text-xs">
                    {group.partitions.length} records
                  </span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3">
                  {group.partitions.map((partition) => {
                    const vacancy = partition.capacity - partition.totalCount;
                    const vacStatus = getVacancyStatus(vacancy);
                    const vacClasses = getVacancyClasses(vacStatus);
                    const fillPct = partition.capacity > 0
                      ? Math.min((partition.totalCount / partition.capacity) * 100, 100)
                      : 0;

                    return (
                      <Link
                        key={partition.shed}
                        href={`/infra/${encodeURIComponent(partition.shed)}`}
                        className="bg-[#1A1D24] border border-[#334155] rounded-xl p-4 hover:border-[#334155] transition-colors animate-fade-up block relative"
                      >
                        <div className={cn("absolute top-3 right-3 text-[10px]", vacClasses)}>
                          {vacancy > 20 && `Vac ${vacancy}`}
                          {vacancy >= 1 && vacancy <= 20 && `Vac ${vacancy}`}
                          {vacancy === 0 && "Full"}
                          {vacancy < 0 && (
                            <span className="font-bold">{vacancy} OVER</span>
                          )}
                        </div>

                        <p className="text-xs font-medium text-[#FFFFFF] truncate pr-12">
                          {partition.shed}
                        </p>

                        <p className="text-[10px] text-[#8899AA] uppercase tracking-wider mt-0.5">
                          {partition.shedTag}
                        </p>

                        <p className="text-2xl font-bold text-[#FFFFFF] mt-2">
                          {partition.totalCount}
                        </p>

                        <div className="mt-2 space-y-0.5">
                          {partition.breeds.map((b) => (
                            <div key={b.breed} className="flex items-center text-[11px]">
                              <span className="text-[#8899AA]">{b.breed}</span>
                              <span className="text-[#B0BEC5] font-medium ml-auto">
                                {b.count}
                              </span>
                            </div>
                          ))}
                        </div>

                        <div className="mt-3">
                          <p className="text-[10px] text-[#8899AA]">
                            {partition.totalCount}/{partition.capacity}
                          </p>
                          <div className="h-1 rounded-full bg-[#22262E] mt-1">
                            <div
                              className={cn(
                                "h-1 rounded-full transition-all",
                                vacStatus === "green" && "bg-emerald-400",
                                vacStatus === "neutral" && "bg-[#8899AA]",
                                vacStatus === "grey" && "bg-[#8899AA]",
                                vacStatus === "overcrowded" && "bg-red-400"
                              )}
                              style={{ width: `${fillPct}%` }}
                            />
                          </div>
                        </div>
                      </Link>
                    );
                  })}
                </div>
              </section>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
