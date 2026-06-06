// ── Feed Consumption mock data ──

export const DAILY_EXPENDITURE = [
  { date: "Mar 1", value: 12500 },
  { date: "Mar 2", value: 11800 },
  { date: "Mar 3", value: 13200 },
  { date: "Mar 4", value: 14100 },
  { date: "Mar 5", value: 12900 },
  { date: "Mar 6", value: 13800 },
  { date: "Mar 7", value: 15200 },
  { date: "Mar 8", value: 14500 },
  { date: "Mar 9", value: 13100 },
  { date: "Mar 10", value: 16200 },
  { date: "Mar 11", value: 15800 },
  { date: "Mar 12", value: 14200 },
  { date: "Mar 13", value: 13500 },
  { date: "Mar 14", value: 15100 },
  { date: "Mar 15", value: 14800 },
];

export const DAILY_QUANTITY = [
  { date: "Mar 1", value: 850 },
  { date: "Mar 2", value: 820 },
  { date: "Mar 3", value: 890 },
  { date: "Mar 4", value: 910 },
  { date: "Mar 5", value: 870 },
  { date: "Mar 6", value: 900 },
  { date: "Mar 7", value: 950 },
  { date: "Mar 8", value: 930 },
  { date: "Mar 9", value: 880 },
  { date: "Mar 10", value: 1020 },
  { date: "Mar 11", value: 990 },
  { date: "Mar 12", value: 920 },
  { date: "Mar 13", value: 895 },
  { date: "Mar 14", value: 960 },
  { date: "Mar 15", value: 940 },
];

export const EXPENDITURE_BY_TYPE = [
  { date: "Mar 1", "Masoor Dhal Bhusa": 7500, Concentrate: 4200, "VGoats Grain Mix": 800 },
  { date: "Mar 3", "Masoor Dhal Bhusa": 7800, Concentrate: 4500, "VGoats Grain Mix": 900 },
  { date: "Mar 5", "Masoor Dhal Bhusa": 8200, Concentrate: 3900, "VGoats Grain Mix": 800 },
  { date: "Mar 7", "Masoor Dhal Bhusa": 8800, Concentrate: 5400, "VGoats Grain Mix": 1000 },
  { date: "Mar 9", "Masoor Dhal Bhusa": 7800, Concentrate: 4500, "VGoats Grain Mix": 800 },
  { date: "Mar 11", "Masoor Dhal Bhusa": 8900, Concentrate: 5900, "VGoats Grain Mix": 1000 },
  { date: "Mar 13", "Masoor Dhal Bhusa": 8500, Concentrate: 3800, "VGoats Grain Mix": 900 },
  { date: "Mar 15", "Masoor Dhal Bhusa": 8100, Concentrate: 5800, "VGoats Grain Mix": 900 },
];

export const FEED_DISTRIBUTION = [
  { name: "Masoor Dhal Bhusa", value: 52 },
  { name: "Concentrate", value: 28 },
  { name: "VGoats Grain Mix", value: 8 },
  { name: "Baking Soda", value: 7 },
  { name: "Others", value: 5 },
];

export const AGE_DISTRIBUTION = [
  { name: "Adult", value: 71.2 },
  { name: "Kid", value: 28.8 },
];

// ── Nutrition mock data ──

export const NUTRITION_STATUSES = ["Buck", "Pregnant", "Non-Pregnant", "Milking", "Mother", "ICU"];
export const NUTRITION_DAYS = ["Mar 11", "Mar 12", "Mar 13", "Mar 14", "Mar 15", "Mar 16", "Mar 17"];

function buildNutritionRows(base: number[]): { name: string; [key: string]: string | number }[] {
  return NUTRITION_STATUSES.map((status, si) => {
    const row: Record<string, string | number> = { name: status };
    NUTRITION_DAYS.forEach((day, di) => {
      const variation = ((si * 7 + di) % 5 - 2) * 0.1;
      row[day] = Math.round((base[di] + variation) * 100) / 100;
    });
    return row as { name: string; [key: string]: string | number };
  });
}

export const TOTAL_FEED_DATA = buildNutritionRows([2.8, 3.1, 2.9, 3.2, 3.0, 2.7, 3.3]);
export const CONCENTRATE_DATA = buildNutritionRows([1.2, 1.4, 1.3, 1.5, 1.3, 1.1, 1.4]);
export const MASOOR_DATA = buildNutritionRows([1.6, 1.7, 1.6, 1.7, 1.7, 1.6, 1.9]);

// ── Season crop colors (gray-mapped for consistency) ──

export const CROP_COLORS: Record<string, string> = {
  "Ground Nut Forage": "#14F1D9",
  "Bengal Gram Dal Forage": "#12D9C3",
  "Black Gram Dal Forage": "#10C1AD",
  "Sorghum": "#10FF98",
  "Moon Dal Forage": "#20E890",
  "Wheat": "#30D188",
  "Masoor Dal Forage": "#40BA80",
  "Soyabean": "#50A378",
  "Toor Dal": "#608C70",
  "Cluster Beans Forage": "#14D4E8",
  "Loong": "#14B8D0",
};
