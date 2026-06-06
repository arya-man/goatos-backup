// ── Counting data (CSV row) ──

export interface CountingRecord {
  date: string;
  farm: string;
  shed: string;
  shed_tag: string;
  breed: string;
  age: string;
  goat_count: number;
  staff: string;
  shed_name: string;
}

// ── Daily summary (CSV row) ──

export interface DailySummaryRecord {
  date: string;
  farm_total_count: number;
  farm_total_weight: number;
  farm_total_weight_value: number;
  cbe_summary_count: number;
  cbe_summary_weight: number;
  cbe_summary_weight_value: number;
  cpt_summary_count: number;
  cpt_summary_weight: number;
  cpt_summary_weight_value: number;
  procurement_summary_count: number;
  procurement_summary_weight: number;
  procurement_summary_weight_value: number;
  adults_total_count: number;
  adults_total_weight: number;
  adults_total_weight_value: number;
  cbe_adults_count: number;
  cbe_adults_weight: number;
  cbe_adults_weight_value: number;
  cpt_adults_count: number;
  cpt_adults_weight: number;
  cpt_adults_weight_value: number;
  kids_total_count: number;
  kids_total_weight: number;
  kids_total_weight_value: number;
  cbe_kids_count: number;
  cbe_kids_weight: number;
  cbe_kids_weight_value: number;
  cpt_kids_count: number;
  cpt_kids_weight: number;
  cpt_kids_weight_value: number;
}

// ── Feed load (CSV row) ──

export interface FeedLoadRecord {
  farm: string;
  load_id: number;
  load_type: string;
  purchase_date: string;
  total_purchased_qty: number;
  consumption_per_day: number;
  current_stock: number;
  days_stock_can_last: number;
  days_consumed_so_far: number;
  farm_load_total_consumed_qty: number;
  last_load_total_cost: number;
  paid_so_far: number;
  pending_amount: number;
  payment_status: string;
  last_payment_date: string;
  last_payment_amount: number;
  purchase_cost: number;
  transport_cost: number;
}

// ── Feed load summary (feedDB_load_summary) ──

export interface FeedLoadSummaryRecord {
  farm: string;
  load_id: number;
  feed: string;
  purchase_date: string;
  consumption_start_date: string;
  avg_consumption_per_day: number;
  days_consumed_so_far: number;
  days_stock_can_last: number;
  total_purchased_qty: number;
}

// ── Season (CSV row) ──

export interface SeasonRecord {
  Start_Date: string;
  End_Date: string;
  Region: string;
  Crop: string;
}

// ── Aggregated types for UI ──

export interface BreedCount {
  breed: string;
  count: number;
}

export interface StatusCount {
  status: string;
  count: number;
}

export interface FarmCount {
  farm: string;
  count: number;
  value: number;
}

export interface ShedPartition {
  shed: string;
  shedTag: string;
  breeds: { breed: string; count: number }[];
  totalCount: number;
  capacity: number;
}

export interface MortalityBreedRow {
  breed: string;
  kidMortality: number;
  kidAbortion: number;
  adultMortality: number;
  totalMortality: number;
}

export interface MortalityFarmRow {
  farm: string;
  kidMortality: number;
  kidAbortion: number;
  adultMortality: number;
  totalMortality: number;
}

export interface FeedStockSummary {
  loadType: string;
  currentStock: number;
  daysLeft: number;
  totalCost: number;
  loadCount: number;
  pendingAmount: number;
  health: "stocked" | "low" | "out";
  loadId: number;
  consumptionStarted: boolean;
  hasNextLoad: boolean;
}

export type FarmTab = "overall" | "core-farms" | "cbe" | "cpt" | "holdings";
