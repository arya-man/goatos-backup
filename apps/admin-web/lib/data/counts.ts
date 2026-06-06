import type { CountingRecord, BreedCount, StatusCount, FarmCount } from "@/lib/types";
import type { FarmTabId } from "@/lib/constants";

// ── Counts KPI data per tab ──

interface CountsKPI {
  totalActive: number;
  farmValue: number;
  totalWeight: number;
  avgWeightPerGoat: number;
  breedsTracked: number;
}

const KPI_BY_TAB: Record<FarmTabId, CountsKPI> = {
  overall: {
    totalActive: 1936,
    farmValue: 3_51_72_010,
    totalWeight: 62784.84,
    avgWeightPerGoat: 32.43,
    breedsTracked: 7,
  },
  "core-farms": {
    totalActive: 1432,
    farmValue: 2_17_53_865,
    totalWeight: 38852.34,
    avgWeightPerGoat: 27.13,
    breedsTracked: 7,
  },
  cbe: {
    totalActive: 796,
    farmValue: 1_19_05_125,
    totalWeight: 21520.91,
    avgWeightPerGoat: 27.04,
    breedsTracked: 7,
  },
  cpt: {
    totalActive: 636,
    farmValue: 98_48_740,
    totalWeight: 17331.43,
    avgWeightPerGoat: 27.25,
    breedsTracked: 5,
  },
  holdings: {
    totalActive: 504,
    farmValue: 1_34_18_145,
    totalWeight: 23932.5,
    avgWeightPerGoat: 47.49,
    breedsTracked: 2,
  },
};

export function getCountsKPI(tab: FarmTabId): CountsKPI {
  return KPI_BY_TAB[tab];
}

// ── Breed counts ──

const BREED_COUNTS_OVERALL: BreedCount[] = [
  { breed: "Beetal", count: 770 },
  { breed: "Anantapur", count: 636 },
  { breed: "Kenguri", count: 255 },
  { breed: "Sojat", count: 174 },
  { breed: "Malai", count: 68 },
  { breed: "Osmanabadi", count: 32 },
  { breed: "Boer", count: 1 },
];

const BREED_COUNTS_CBE: BreedCount[] = [
  { breed: "Beetal", count: 389 },
  { breed: "Sojat", count: 174 },
  { breed: "Anantapur", count: 72 },
  { breed: "Malai", count: 68 },
  { breed: "Osmanabadi", count: 29 },
  { breed: "Boer", count: 1 },
];

const BREED_COUNTS_CPT: BreedCount[] = [
  { breed: "Anantapur", count: 364 },
  { breed: "Beetal", count: 272 },
];

const BREED_COUNTS_HOLDINGS: BreedCount[] = [
  { breed: "Kenguri", count: 255 },
  { breed: "Beetal", count: 200 },
];

export function getBreedCounts(tab: FarmTabId): BreedCount[] {
  switch (tab) {
    case "cbe": return BREED_COUNTS_CBE;
    case "cpt": return BREED_COUNTS_CPT;
    case "holdings": return BREED_COUNTS_HOLDINGS;
    case "core-farms": {
      const merged = new Map<string, number>();
      for (const b of [...BREED_COUNTS_CBE, ...BREED_COUNTS_CPT]) {
        merged.set(b.breed, (merged.get(b.breed) ?? 0) + b.count);
      }
      return Array.from(merged, ([breed, count]) => ({ breed, count })).sort((a, b) => b.count - a.count);
    }
    default: return BREED_COUNTS_OVERALL;
  }
}

// ── Status counts (shed_tag grouping) ──

const STATUS_COUNTS_OVERALL: StatusCount[] = [
  { status: "Non-Pregnant", count: 372 },
  { status: "Pregnant", count: 248 },
  { status: "F2-Male", count: 283 },
  { status: "F2-Female", count: 150 },
  { status: "K2", count: 164 },
  { status: "K1", count: 39 },
  { status: "K0", count: 19 },
  { status: "ICU-Non-Pregnant", count: 87 },
  { status: "Icu-Kid", count: 10 },
  { status: "Milking", count: 56 },
  { status: "Mother", count: 99 },
  { status: "Warmup", count: 47 },
  { status: "M0", count: 25 },
  { status: "Buck", count: 40 },
  { status: "Quarantine Kids", count: 8 },
  { status: "F0", count: 255 },
];

// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function getStatusCounts(_tab: FarmTabId): StatusCount[] {
  // TODO: Compute from BigQuery when USE_BIGQUERY is enabled
  return STATUS_COUNTS_OVERALL.sort((a, b) => b.count - a.count);
}

// ── Farm distribution ──

const FARM_DISTRIBUTION: FarmCount[] = [
  { farm: "CBE", count: 796, value: 1_19_05_125 },
  { farm: "CPT", count: 636, value: 98_48_740 },
  { farm: "Holdings", count: 504, value: 1_34_18_145 },
];

export function getFarmDistribution(): FarmCount[] {
  return FARM_DISTRIBUTION;
}

// ── Age breakdown ──

interface AgeBreakdown {
  adults: number;
  kids: number;
}

export function getAgeBreakdown(tab: FarmTabId): AgeBreakdown {
  switch (tab) {
    case "cbe": return { adults: 493, kids: 352 };
    case "cpt": return { adults: 301, kids: 347 };
    case "holdings": return { adults: 455, kids: 0 };
    case "core-farms": return { adults: 794, kids: 699 };
    default: return { adults: 1249, kids: 699 };
  }
}

// ── Fattening gender (F2 only) ──

interface GenderBreakdown {
  male: number;
  female: number;
}

export function getFatteningGender(tab: FarmTabId): GenderBreakdown {
  switch (tab) {
    case "cbe": return { male: 133, female: 63 };
    case "cpt": return { male: 213, female: 87 };
    default: return { male: 346, female: 150 };
  }
}

// ── Helpers for transforming CSV data ──

export function aggregateByField(
  records: CountingRecord[],
  field: keyof CountingRecord
): { name: string; value: number }[] {
  const map = new Map<string, number>();
  for (const r of records) {
    const key = String(r[field]);
    if (key) map.set(key, (map.get(key) ?? 0) + r.goat_count);
  }
  return Array.from(map, ([name, value]) => ({ name, value })).sort((a, b) => b.value - a.value);
}

export function filterByFarmTab(records: CountingRecord[], tab: FarmTabId): CountingRecord[] {
  switch (tab) {
    case "cbe": return records.filter((r) => r.farm === "CBE");
    case "cpt": return records.filter((r) => r.farm === "CPT");
    case "holdings": return records.filter((r) => r.farm === "Holding Farm");
    case "core-farms": return records.filter((r) => r.farm === "CBE" || r.farm === "CPT");
    default: return records;
  }
}
