"use client";

import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { cn } from "@/lib/utils";
import type { VaccinationRecord } from "@/app/api/vaccination/route";

// ── Helpers ────────────────────────────────────────────────────────────────

/** Format "2026-02-21" → "21 Feb 2026" */
function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr) return "";
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return "";
  return d.toLocaleDateString("en-GB", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  });
}

/** Return Tailwind classes for the cell based on vaccination_status */
function cellClasses(status: string | null | undefined): string {
  if (status === "Overdue") return "bg-red-500/20 text-red-400";
  if (status === "Due Soon") return "bg-amber-500/20 text-amber-400";
  if (status === "Up To Date") return "bg-emerald-500/10 text-[#B0BEC5]";
  return "text-[#8899AA]";
}

// ── Data types ─────────────────────────────────────────────────────────────

interface VaccineCell {
  last_vaccination_date: string | null;
  vaccination_status: string | null;
}

interface SubRow {
  age: string;
  animal_type: string;
  animal_count: number;
  vaccines: Record<string, VaccineCell>;
}

interface ShedRow {
  shedKey: string;
  shed: string;
  shed_tag: string;
  subRows: SubRow[];
}

// ── Data transform ─────────────────────────────────────────────────────────

function buildShedRows(records: VaccinationRecord[]): ShedRow[] {
  // Map: shedKey → Map<subRowKey, SubRow>
  const shedMap = new Map<string, Map<string, SubRow>>();
  const shedMeta = new Map<string, { shed: string; shed_tag: string }>();

  for (const r of records) {
    const shedKey = `${r.shed} - ${r.shed_tag}`;
    const subKey = `${r.age}|${r.animal_type}`;

    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, new Map());
      shedMeta.set(shedKey, { shed: r.shed, shed_tag: r.shed_tag });
    }

    const subMap = shedMap.get(shedKey)!;
    if (!subMap.has(subKey)) {
      subMap.set(subKey, {
        age: r.age,
        animal_type: r.animal_type,
        animal_count: r.animal_count,
        vaccines: {},
      });
    }

    const subRow = subMap.get(subKey)!;
    if (r.vaccine) {
      subRow.vaccines[r.vaccine] = {
        last_vaccination_date: r.last_vaccination_date,
        vaccination_status: r.vaccination_status,
      };
    }
  }

  return Array.from(shedMap.entries())
    .map(([shedKey, subMap]) => {
      const meta = shedMeta.get(shedKey)!;
      return {
        shedKey,
        shed: meta.shed,
        shed_tag: meta.shed_tag,
        subRows: Array.from(subMap.values()).sort((a, b) => {
          const ageOrder = a.age.localeCompare(b.age);
          if (ageOrder !== 0) return ageOrder;
          return a.animal_type.localeCompare(b.animal_type);
        }),
      };
    })
    .sort((a, b) => a.shedKey.localeCompare(b.shedKey, undefined, { numeric: true }));
}

// ── Page ───────────────────────────────────────────────────────────────────

export default function VaccinationPage() {
  const [farm, setFarm] = useState<"CBE" | "CPT">("CBE");

  const { data: response, isLoading, error, refetch } = useQuery<{
    farm: string;
    data: VaccinationRecord[];
  }>({
    queryKey: ["vaccination", farm],
    queryFn: async () => {
      const res = await fetch(`/api/vaccination?farm=${farm}`);
      if (!res.ok) throw new Error("Failed to fetch vaccination data");
      return res.json();
    },
    staleTime: 15 * 60 * 1000,
  });

  const vaccines = useMemo(() => {
    const seen = new Set<string>();
    const ordered: string[] = [];
    for (const r of response?.data ?? []) {
      if (r.vaccine && !seen.has(r.vaccine)) {
        seen.add(r.vaccine);
        ordered.push(r.vaccine);
      }
    }
    return ordered.sort((a, b) => a.localeCompare(b));
  }, [response]);

  const shedRows = useMemo(
    () => buildShedRows(response?.data ?? []),
    [response]
  );

  return (
    <div className="flex flex-col h-full gap-6">
      {/* Farm toggle */}
      <div className="flex flex-wrap items-center gap-1 shrink-0">
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
      {error && (
        <ErrorState
          message="Failed to load vaccination data"
          onRetry={() => refetch()}
        />
      )}

      {!isLoading && !error && shedRows.length === 0 && (
        <div className="text-center py-12 text-[#8899AA] text-sm">
          No vaccination data available for {farm}.
        </div>
      )}

      {!isLoading && !error && shedRows.length > 0 && (
        <div className="flex flex-col flex-1 gap-6 min-h-0">
          {/* Legend */}
          <div className="flex items-center gap-4 text-[11px] text-[#8899AA] shrink-0">
            <span className="font-medium text-[#B0BEC5]">Status:</span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-sm bg-red-500/30" />
              Overdue
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-sm bg-amber-500/30" />
              Due Soon
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-sm bg-emerald-500/20" />
              Up To Date
            </span>
            <span className="flex items-center gap-1.5">
              <span className="inline-block w-2.5 h-2.5 rounded-sm bg-[#22262E]" />
              No record
            </span>
          </div>

          <div className="flex-1 min-h-0 overflow-auto rounded-xl border border-[#334155]">
          <table className="w-full text-xs border-collapse">
            <thead className="sticky top-0 z-20">
              <tr className="bg-[#1A1D24] border-b border-[#334155]">
                <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap sticky left-0 bg-[#1A1D24] z-30">
                  Shed
                </th>
                <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">
                  Type
                </th>
                <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">
                  Animals
                </th>
                {vaccines.map((v) => (
                  <th
                    key={v}
                    className="px-3 py-2.5 text-center font-medium text-[#B0BEC5] whitespace-nowrap min-w-[110px]"
                  >
                    {v}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {shedRows.map((shed) =>
                shed.subRows.map((sub, subIdx) => (
                  <tr
                    key={`${shed.shedKey}-${sub.age}-${sub.animal_type}`}
                    className="border-b border-[#334155] hover:bg-[#22262E]/40 transition-colors"
                  >
                    {/* Shed name — only shown on first sub-row */}
                    {subIdx === 0 ? (
                      <td
                        rowSpan={shed.subRows.length}
                        className="px-3 py-2 align-top sticky left-0 bg-[#1A1D24] z-10 border-r border-[#334155]"
                      >
                        <p className="font-medium text-[#FFFFFF] whitespace-nowrap">
                          {shed.shed}
                        </p>
                        <p className="text-[10px] text-[#8899AA] uppercase tracking-wider mt-0.5">
                          {shed.shed_tag}
                        </p>
                      </td>
                    ) : null}

                    {/* Age + animal type */}
                    <td className="px-3 py-2 text-[#8899AA] whitespace-nowrap">
                      {sub.age} {sub.animal_type}
                    </td>

                    {/* Animal count */}
                    <td className="px-3 py-2 text-right text-[#B0BEC5] font-medium">
                      {sub.animal_count.toLocaleString()}
                    </td>

                    {/* Vaccine cells */}
                    {vaccines.map((v) => {
                      const cell = sub.vaccines[v];
                      return (
                        <td
                          key={v}
                          className={cn(
                            "px-3 py-2 text-center whitespace-nowrap",
                            cell ? cellClasses(cell.vaccination_status) : "text-[#334155]"
                          )}
                        >
                          {cell ? formatDate(cell.last_vaccination_date) || "—" : ""}
                        </td>
                      );
                    })}
                  </tr>
                ))
              )}
            </tbody>
          </table>
          </div>
        </div>
      )}
    </div>
  );
}
