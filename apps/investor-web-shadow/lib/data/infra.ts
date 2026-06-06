import type { CountingRecord, ShedPartition } from "@/lib/types";
import { getShedCapacity } from "@/lib/constants";

// ── Infra KPIs ──

interface InfraKPI {
  count: number;
  capacity: number;
  vacancy: number;
}

export const INFRA_KPI_CBE: InfraKPI = {
  count: 845,
  capacity: 1136,
  vacancy: 291,
};

export const INFRA_KPI_CPT: InfraKPI = {
  count: 648,
  capacity: 780,
  vacancy: 132,
};

export function getInfraKPI(farm: "CBE" | "CPT"): InfraKPI {
  return farm === "CBE" ? INFRA_KPI_CBE : INFRA_KPI_CPT;
}

// ── Process counting records into shed partitions ──

export function buildShedPartitions(records: CountingRecord[]): ShedPartition[] {
  const map = new Map<string, { breeds: Map<string, number>; shedTag: string }>();

  for (const r of records) {
    if (!r.shed) continue;
    const key = r.shed;
    if (!map.has(key)) {
      map.set(key, { breeds: new Map(), shedTag: r.shed_tag });
    }
    const entry = map.get(key)!;
    entry.breeds.set(r.breed, (entry.breeds.get(r.breed) ?? 0) + r.goat_count);
  }

  return Array.from(map.entries()).map(([shed, { breeds, shedTag }]) => {
    const breedArr = Array.from(breeds, ([breed, count]) => ({ breed, count })).sort(
      (a, b) => b.count - a.count
    );
    const totalCount = breedArr.reduce((s, b) => s + b.count, 0);
    return {
      shed,
      shedTag,
      breeds: breedArr,
      totalCount,
      capacity: getShedCapacity(shed),
    };
  });
}

// ── Group partitions by shed name (e.g. "Mandela 1 - Part 3" → "MANDELA") ──

export interface ShedGroup {
  shedName: string;
  partitions: ShedPartition[];
  totalCount: number;
  totalCapacity: number;
}

export function groupByShedName(partitions: ShedPartition[]): ShedGroup[] {
  const map = new Map<string, ShedPartition[]>();

  for (const p of partitions) {
    // Extract base shed name from shed field
    // "Gandhi 1 - Part 2" → "Gandhi"
    // "Yashoda 10" → "Yashoda"
    const base = p.shed.replace(/ \d.*$/, "").trim();
    const key = base.toUpperCase();
    if (!map.has(key)) map.set(key, []);
    map.get(key)!.push(p);
  }

  return Array.from(map.entries())
    .map(([shedName, partitions]) => ({
      shedName,
      partitions: partitions.sort((a, b) => a.shed.localeCompare(b.shed, undefined, { numeric: true })),
      totalCount: partitions.reduce((s, p) => s + p.totalCount, 0),
      totalCapacity: partitions.reduce((s, p) => s + p.capacity, 0),
    }))
    .sort((a, b) => a.shedName.localeCompare(b.shedName, undefined, { numeric: true }));
}

// ── Vacancy classification ──

export type VacancyStatus = "green" | "neutral" | "grey" | "overcrowded";

export function getVacancyStatus(vacancy: number): VacancyStatus {
  if (vacancy > 20) return "green";
  if (vacancy > 0) return "neutral";
  if (vacancy === 0) return "grey";
  return "overcrowded";
}

export function getVacancyClasses(status: VacancyStatus): string {
  switch (status) {
    case "green":
      return "text-emerald-400";
    case "neutral":
      return "text-[#8899AA]";
    case "grey":
      return "text-[#8899AA]";
    case "overcrowded":
      return "text-red-400";
  }
}
