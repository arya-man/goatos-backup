// ── Status code display names ──

const STATUS_DISPLAY: Record<string, string> = {
  F0: "Fattening",
  F1: "Fattening Stage 1",
  F2: "Fattening Stage 2",
  K0: "Kid Stage 0",
  K1: "Kid Stage 1",
  K2: "Kid Stage 2",
  K3: "Kid Stage 3",
  K4: "Kid Stage 4",
  M0: "Mother",
};

const COMPOUND_STATUS: Record<string, string> = {
  "F2-Male": "F2 Male",
  "F2-Female": "F2 Female",
  "Non-Pregnant": "Non Pregnant",
  "ICU-Kid": "ICU Kid",
  "ICU-Non-Pregnant": "ICU Non Pregnant",
  "Icu-Kid": "ICU Kid",
  "Quarantine Kids": "Quarantine Kids",
  "Post-Monsoon": "Post Monsoon",
};

/** Map status codes and compound statuses to display names */
export function displayStatus(raw: string): string {
  if (STATUS_DISPLAY[raw]) return STATUS_DISPLAY[raw];
  if (COMPOUND_STATUS[raw]) return COMPOUND_STATUS[raw];
  return raw;
}

// ── Label formatting ──

const SPECIAL_WORDS: Record<string, string> = {
  cbe: "CBE",
  cpt: "CPT",
  id: "ID",
  kg: "KG",
  adg: "ADG",
  icu: "ICU",
  mtd: "MTD",
  pct: "%",
};

/** Convert snake_case or raw field names to Title Case, preserving CBE/CPT as uppercase */
export function formatLabel(raw: string): string {
  if (!raw) return raw;
  // Check compound status first
  if (COMPOUND_STATUS[raw]) return COMPOUND_STATUS[raw];
  if (STATUS_DISPLAY[raw]) return STATUS_DISPLAY[raw];

  // Common field name overrides
  const FIELD_OVERRIDES: Record<string, string> = {
    shed_tag: "Status",
    goat_id: "Goat ID",
    animal_status: "Animal Status",
    load_display_label: "Load",
    month_season_label: "Month",
    mortality_percent: "Mortality %",
    landing_cost_per_kg: "Landing Cost Per KG",
    avg_weight_kg: "Avg Weight (KG)",
    days_in_stage: "Days In Stage",
    total_weight: "Total Weight",
    goat_count: "Count",
    farm_value: "Farm Value",
    delivered_last_8m_pct: "Delivered Last 8M %",
    approx_months_between_births: "Months Between Births",
    birth_count: "Birth Count",
    litter_size: "Litter Size",
    mother_breed: "Mother Breed",
    death_burden_pct: "Mortality %",
    mortality_pct_mtd: "Mortality %",
    kid_death_share_pct: "Kid Mortality %",
    abortion_share_pct: "Abortion %",
    adult_death_share_pct: "Adult Mortality %",
    death_share_pct: "Mortality %",
    total_mortality_pct_overall: "Total Mortality %",
    post_birth_death_pct: "Post Birth Mortality %",
    abortion_pct: "Abortion %",
    total_spend: "Total Spend",
    feed_per_animal: "Feed Per Animal",
  };

  if (FIELD_OVERRIDES[raw]) return FIELD_OVERRIDES[raw];

  // Replace underscores/hyphens with spaces, then title-case
  return raw
    .replace(/[_-]/g, " ")
    .split(" ")
    .map((word) => {
      const lower = word.toLowerCase();
      if (SPECIAL_WORDS[lower]) return SPECIAL_WORDS[lower];
      return word.charAt(0).toUpperCase() + word.slice(1).toLowerCase();
    })
    .join(" ");
}

/** Ensure CBE/CPT are always uppercase in any string */
export function ensureFarmCaps(text: string): string {
  return text
    .replace(/\bcbe\b/gi, "CBE")
    .replace(/\bcpt\b/gi, "CPT");
}

// ── Tooltip metric labels ──

export type MetricContext =
  | "counts"
  | "mortality-pct"
  | "mortality-count"
  | "adg"
  | "weight"
  | "births"
  | "feed-spend"
  | "feed-qty"
  | "feed-per-animal"
  | "purchase-cost"
  | "infra-count"
  | "infra-capacity"
  | "default";

const METRIC_LABELS: Record<MetricContext, string> = {
  counts: "No. of Animals",
  "mortality-pct": "Mortality Rate (%)",
  "mortality-count": "No. of Deaths",
  adg: "Avg Daily Gain (g)",
  weight: "Weight (KG)",
  births: "No. of Births",
  "feed-spend": "Expenditure (₹)",
  "feed-qty": "Quantity (KG)",
  "feed-per-animal": "Feed Per Animal (g)",
  "purchase-cost": "Cost Per KG (₹)",
  "infra-count": "No. of Animals",
  "infra-capacity": "Capacity",
  default: "Value",
};

export function getMetricLabel(context: MetricContext): string {
  return METRIC_LABELS[context] || METRIC_LABELS.default;
}

// ── Crop colors for Feed Seasons ──

export const CROP_COLORS: Record<string, string> = {
  "Ground Nut Forage": "#14F1D9",
  "Bengal Gram Dal Forage": "#12D9C3",
  "Black Gram Dal Forage": "#10C1AD",
  Sorghum: "#10FF98",
  "Moon Dal Forage": "#20E890",
  Wheat: "#30D188",
  "Masoor Dal Forage": "#40BA80",
  Soyabean: "#50A378",
  "Toor Dal": "#608C70",
  "Cluster Beans Forage": "#14D4E8",
  Loong: "#14B8D0",
};

// ── Shifting thresholds ──

export const SHIFTING_THRESHOLDS: Record<string, number> = {
  K1: 7,
  K2: 45,
  K3: 7,
};

export const SHIFTING_RED = "#E76B7A";
export const SHIFTING_GREEN = "#14F1D9";

export function getShiftingColor(stage: string, days: number): string {
  const threshold = SHIFTING_THRESHOLDS[stage];
  if (threshold == null) return SHIFTING_GREEN;
  return days > threshold ? SHIFTING_RED : SHIFTING_GREEN;
}

// ── Chart description subtitles ──

export const CHART_DESCRIPTIONS: Record<string, string> = {
  // Counts
  "Count by Status": "Number of active goats grouped by their current lifecycle status",
  "Count by Breed": "Distribution of active goats across different breeds",
  "Count by Status & Breed": "Breakdown of each status category by breed (log scale)",
  "Status by Breed": "Breakdown of each status category by breed (log scale)",
  "Farm Distribution": "Percentage share of goats across farms",
  "Count Distribution by Farm": "Percentage share of goats across farms",
  "Count by Farm": "Distribution of active goats across farms",
  "Count by Age": "Split of livestock between adult and kid categories",
  "Fattening by Gender": "Gender distribution of animals in fattening stages",
  "Adults by Gender": "Gender distribution of adults",
  "Kids by Gender": "Gender distribution of kids",
  "K0 to K3 Kids by Gender": "Gender distribution of kid animals",

  // Mortality
  "Kids Mortality & Abortion by Breed": "Share of total deaths attributed to each breed",
  "Adults Mortality by Breed": "Adult death rate per breed",
  "Total Mortality by Breed": "Combined mortality rate per breed",
  "Kids Mortality & Abortion by Farm": "Death distribution across different farm locations",
  "Adults Mortality by Farm": "Adult death distribution across farms",
  "Total Mortality by Farm": "Overall death distribution across farms",
  "Load-wise Mortality %": "Mortality percentage by purchase load",
  "Breed-wise Mortality by Vendor": "Mortality breakdown by vendor and breed",
  "Mother Mortality by Breed": "Share of delivering mothers that died, by breed",
  "Mother Mortality by Litter Size": "Mother death rate by litter size at birth",
  "Kids Mortality by Litter Size": "Kid death rate based on litter size at birth",
  "Kids Mortality Split": "Death vs abortion split by litter size",
  "Mortality by Status": "Death burden percentage by animal status category",
  "Mortality by Housing": "Death distribution by housing or shed type",
  "Mortality by Season": "Monthly death counts grouped by season",
  "Mortality by Gender": "Mortality rate comparison between male and female animals",

  // Births
  "Total Births": "Cumulative birth events recorded",
  "Avg Kids per Mother": "Average litter size across all deliveries",
  "Kidding Efficiency by Breed": "Percentage of mothers that delivered in the last 8 months, by breed",
  "Birth Count – Last 10 Days": "Daily birth counts over the most recent 10 day period",
  "Kidding Frequency by Breed": "Average months between successive births for each breed",
  "Litter Size by Breed": "Average number of kids per delivery by breed",

  // Fattening
  "ADG by Gender & Breed": "Average daily weight gain by gender and breed",
  "ADG by Breed": "Average daily weight gain in grams for each breed",
  "ADG by Status": "Average daily weight gain by animal lifecycle status",
  "ADG by Gender": "Average daily weight gain comparison between male and female animals",
  "ADG by Breed & Status": "Average daily gain stacked by breed and status",
  "Percentage weighed per week": "Percentage of kids weighed each week",
  "Load-wise ADG by Breed": "Average daily gain by purchase load, breed, and gender",
  "Average Weight by Load": "Average weight progression by purchase load",
  "Last 5 Weighings & Weight Changes": "Weight trend and recent changes by load",

  // Purchase Cost
  "Landing Cost per Kg – Goats": "Purchase cost per kilogram for each goat load or vendor",
  "Landing Cost per Kg – Sheep": "Purchase cost per kilogram for each sheep load or vendor",

  // Feed
  "Daily Feed Expenditure": "Daily total feed spending trend",
  "Daily Feed Quantity": "Daily feed quantity consumed in kilograms",
  "Expenditure by Feed Type": "Spending trend for top feed types",
  "Feed Distribution": "Proportion of total feed consumed by each feed type in last 30 days",
  "Age Distribution": "Feed consumption split between adult and kid animals",
  "Feed Seasons Calendar": "Regional feed crop availability across months",
  "Total Feed per Animal": "Daily feed per animal over the last 7 days by status",
  "Concentrate per Animal": "Daily concentrate per animal over the last 7 days by status",
  "Masoor Bhusa per Animal": "Daily masoor bhusa per animal over the last 7 days by status",

  // Infra
  Count: "Current occupancy versus total capacity per farm",
  Capacity: "Total housing capacity per farm",
  Vacancy: "Available space versus total capacity",

  // Shiftings
  "Kids Stage Tracking": "Kids due for stage shifting based on days spent in current stage",

  // Summary
  "Daily (Yesterday)": "Event counts for births, deaths, purchases, sales, and abortions",
  "Weekly (Last 7 Days)": "Event counts for births, deaths, purchases, sales, and abortions",
  "Monthly (Last 30 Days)": "Event counts for births, deaths, purchases, sales, and abortions",
};

export function getChartDescription(title: string): string {
  return CHART_DESCRIPTIONS[title] || "";
}
