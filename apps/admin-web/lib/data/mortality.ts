import type { MortalityBreedRow, MortalityFarmRow } from "@/lib/types";

// ── Summary stats ──

export interface MortalitySummary {
  totalDeaths: number;
  kidDeaths: number;
  adultDeaths: number;
  abortions: number;
  mortalityRate: string;
}

export const MORTALITY_SUMMARY_OVERALL: MortalitySummary = {
  totalDeaths: 47,
  kidDeaths: 18,
  adultDeaths: 29,
  abortions: 12,
  mortalityRate: "2.4%",
};

export const MORTALITY_SUMMARY_MONTH: MortalitySummary = {
  totalDeaths: 10,
  kidDeaths: 4,
  adultDeaths: 6,
  abortions: 3,
  mortalityRate: "0.5%",
};

// ── Breed-wise ──

export const MORTALITY_BY_BREED: MortalityBreedRow[] = [
  { breed: "Anantapur", kidMortality: 9.45, kidAbortion: 1.72, adultMortality: 19.42, totalMortality: 14.66 },
  { breed: "Osmanabadi", kidMortality: 14.17, kidAbortion: 15.52, adultMortality: 2.88, totalMortality: 8.27 },
  { breed: "Sojat", kidMortality: 21.26, kidAbortion: 10.34, adultMortality: 24.46, totalMortality: 22.93 },
  { breed: "Beetal", kidMortality: 23.62, kidAbortion: 22.41, adultMortality: 33.81, totalMortality: 28.95 },
  { breed: "Malai", kidMortality: 31.5, kidAbortion: 43.1, adultMortality: 18.71, totalMortality: 24.81 },
];

// ── Farm-wise ──

export const MORTALITY_BY_FARM: MortalityFarmRow[] = [
  { farm: "CBE", kidMortality: 18.3, kidAbortion: 12.5, adultMortality: 22.1, totalMortality: 20.4 },
  { farm: "CPT", kidMortality: 15.8, kidAbortion: 8.2, adultMortality: 16.9, totalMortality: 16.4 },
  { farm: "Holding", kidMortality: 5.2, kidAbortion: 2.1, adultMortality: 8.7, totalMortality: 7.1 },
];

// ── Load-wise ──

export interface MortalityLoadRow {
  load: string;
  date: string;
  mortality: number;
  breed: string;
  vendor: string;
}

export const MORTALITY_BY_LOAD: MortalityLoadRow[] = [
  { load: "Load #201", date: "2025-12-05", mortality: 8.2, breed: "Beetal", vendor: "Vendor A" },
  { load: "Load #204", date: "2025-12-18", mortality: 12.5, breed: "Sojat", vendor: "Vendor B" },
  { load: "Load #208", date: "2025-12-24", mortality: 6.1, breed: "Anantapur", vendor: "Vendor A" },
  { load: "Load #215", date: "2026-01-17", mortality: 15.3, breed: "Malai", vendor: "Vendor C" },
  { load: "Load #218", date: "2026-01-20", mortality: 9.8, breed: "Beetal", vendor: "Vendor B" },
  { load: "Load #228", date: "2026-02-16", mortality: 11.2, breed: "Sojat", vendor: "Vendor A" },
  { load: "Load #234", date: "2026-03-09", mortality: 4.5, breed: "Osmanabadi", vendor: "Vendor C" },
];

// ── By Delivery ──

export interface DeliveryRow {
  name: string;
  value: number;
}

export const MOTHER_MORTALITY_BREED: DeliveryRow[] = [
  { name: "Beetal", value: 3.2 },
  { name: "Anantapur", value: 2.8 },
  { name: "Sojat", value: 4.1 },
  { name: "Malai", value: 5.5 },
  { name: "Osmanabadi", value: 1.9 },
];

export const MOTHER_MORTALITY_LITTER: DeliveryRow[] = [
  { name: "Single", value: 1.2 },
  { name: "Twins", value: 3.5 },
  { name: "Triplets", value: 8.7 },
  { name: "Quadruplets", value: 15.2 },
];

export const KIDS_MORTALITY_LITTER: DeliveryRow[] = [
  { name: "Single", value: 5.1 },
  { name: "Twins", value: 8.3 },
  { name: "Triplets", value: 18.5 },
  { name: "Quadruplets", value: 28.9 },
];

export const KIDS_MORTALITY_SPLIT = [
  { name: "Single", death: 4.2, abortion: 0.9 },
  { name: "Twins", death: 6.1, abortion: 2.2 },
  { name: "Triplets", death: 12.3, abortion: 6.2 },
  { name: "Quadruplets", death: 18.5, abortion: 10.4 },
];

// ── Trends ──

export const TRENDS_BY_STATUS: DeliveryRow[] = [
  { name: "Pregnant", value: 12 },
  { name: "Non-Pregnant", value: 8 },
  { name: "Milking", value: 5 },
  { name: "Mother", value: 7 },
  { name: "ICU", value: 15 },
];

export const TRENDS_BY_HOUSING: DeliveryRow[] = [
  { name: "Gandhi", value: 8 },
  { name: "Mandela", value: 11 },
  { name: "Godel", value: 6 },
  { name: "Castro", value: 9 },
  { name: "Sumathi", value: 5 },
  { name: "Yashoda", value: 8 },
];

export const TRENDS_BY_SEASON: DeliveryRow[] = [
  { name: "Summer", value: 18 },
  { name: "Monsoon", value: 12 },
  { name: "Winter", value: 9 },
  { name: "Post-Monsoon", value: 8 },
];

export const TRENDS_BY_GENDER: DeliveryRow[] = [
  { name: "Male", value: 22 },
  { name: "Female", value: 25 },
];
