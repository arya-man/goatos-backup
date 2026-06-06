'use client';

import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Building2, Users, DoorOpen } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { formatNumber, getShedCapacity } from "@/lib/constants";
import type { CountingRecord } from "@/lib/types";
import { cn } from "@/lib/utils";
import { getVacancyStatus, getVacancyClasses } from "@/lib/data/infra";
import { useMemo } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";

export default function ShedDetailPage() {
  const isMobile = useIsMobile();
  const { shed } = useParams<{ shed: string }>();
  const shedName = decodeURIComponent(shed);
  const router = useRouter();
  const searchParams = useSearchParams();
  const farm = searchParams.get("farm") || "CBE";

  const { data: allRecords = [] } = useQuery<CountingRecord[]>({
    queryKey: ["infra-shed-detail", farm],
    queryFn: () => fetch(`/api/infra?farm=${farm}`).then((r) => r.json()).then((j) => Array.isArray(j.data) ? j.data : []),
    staleTime: 15 * 60 * 1000,
  });

  // Filter for this shed
  const shedRecords = useMemo(
    () => (Array.isArray(allRecords) ? allRecords : []).filter((r) => r.shed === shedName),
    [allRecords, shedName]
  );

  // Aggregate breed counts
  const breedCounts = useMemo(() => {
    const map = new Map<string, number>();
    for (const r of shedRecords) {
      map.set(r.breed, (map.get(r.breed) ?? 0) + r.goat_count);
    }
    return Array.from(map.entries())
      .map(([breed, count]) => ({ name: breed, value: count }))
      .sort((a, b) => b.value - a.value);
  }, [shedRecords]);

  const totalCount = breedCounts.reduce((s, b) => s + b.value, 0);
  const capacity = getShedCapacity(shedName);
  const vacancy = capacity - totalCount;
  const vacStatus = getVacancyStatus(vacancy);
  const vacClasses = getVacancyClasses(vacStatus);

  return (
    <div className="space-y-6">
      {/* Back button */}
      <button
        onClick={() => router.back()}
        className="flex items-center gap-1.5 text-xs text-[#8899AA] hover:text-[#14F1D9] transition-colors"
      >
        <ArrowLeft size={14} />
        Back to Infra
      </button>

      {/* Shed title + vacancy badge */}
      <div className="flex items-center gap-3">
        <h1 className="text-lg font-semibold text-[#FFFFFF]">{shedName}</h1>
        <span className={cn("text-xs", vacClasses)}>
          {vacancy > 20 && `Vac ${vacancy}`}
          {vacancy >= 1 && vacancy <= 20 && `Vac ${vacancy}`}
          {vacancy === 0 && "Full"}
          {vacancy < 0 && (
            <span className="font-bold">{vacancy} OVER</span>
          )}
        </span>
      </div>

      {/* KPI scorecards */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <KPICard
          label="Count"
          value={formatNumber(totalCount)}
          icon={<Building2 size={18} />}
          delay={0}
        />
        <KPICard
          label="Capacity"
          value={formatNumber(capacity)}
          icon={<Users size={18} />}
          delay={1}
        />
        <KPICard
          label="Vacancy"
          value={formatNumber(vacancy)}
          icon={<DoorOpen size={18} />}
          delay={2}
          subtitle={capacity > 0 ? `${((vacancy / capacity) * 100).toFixed(1)}% of capacity` : undefined}
        />
      </div>

      {/* Breed distribution chart */}
      {breedCounts.length > 0 && (
        <ChartCard title="Breed Distribution" subtitle={`${breedCounts.length} breeds in this shed`}>
          <HorizontalBarChart data={breedCounts} valueLabel="Count" showLabels={!isMobile} />
        </ChartCard>
      )}

      {/* Records table */}
      {shedRecords.length > 0 && (
        <div className="rounded-xl border border-[#334155] bg-[#1A1D24] overflow-hidden animate-fade-up">
          <div className="p-4 border-b border-[#334155]">
            <h3 className="text-sm font-medium text-[#FFFFFF]">All Records</h3>
            <p className="text-xs text-[#8899AA] mt-0.5">
              {shedRecords.length} records for {shedName}
            </p>
          </div>

          {/* Header row */}
          <div className="hidden sm:grid sm:grid-cols-5 gap-2 px-4 py-2 bg-[#22262E] text-[10px] font-medium uppercase tracking-wider text-[#8899AA]">
            <span>Date</span>
            <span>Breed</span>
            <span>Age</span>
            <span className="text-right">Count</span>
            <span>Staff</span>
          </div>

          {/* Data rows */}
          <div className="divide-y divide-[#334155]">
            {shedRecords.map((record, idx) => (
              <div
                key={`${record.date}-${record.breed}-${idx}`}
                className="grid grid-cols-2 sm:grid-cols-5 gap-2 px-4 py-2.5 hover:bg-[#22262E] transition-colors"
              >
                <span className="text-xs text-[#B0BEC5]">{record.date}</span>
                <span className="text-xs text-[#FFFFFF] font-medium">{record.breed}</span>
                <span className="text-xs text-[#8899AA]">{record.age}</span>
                <span className="text-xs text-[#FFFFFF] font-medium sm:text-right">{record.goat_count}</span>
                <span className="text-xs text-[#8899AA] truncate">{record.staff}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Empty state */}
      {shedRecords.length === 0 && (
        <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-12 text-center animate-fade-up">
          <p className="text-sm text-[#8899AA]">No records found for this shed.</p>
        </div>
      )}
    </div>
  );
}
