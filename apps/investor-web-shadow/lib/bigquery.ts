import { BigQuery } from "@google-cloud/bigquery";
import * as fs from "fs";
import * as path from "path";

// LEGACY - DO NOT COPY INTO GOAT OS RUNTIME APPS.
// This shadow app preserves the old dashboard BigQuery access pattern only for
// migration comparison. Real admin/mobile apps must call Goat OS APIs backed by
// Postgres projections instead of querying BigQuery or Drive-backed tables.
const PROJECT_ID = process.env.BIGQUERY_PROJECT_ID || "goatos-sheets";
const DATASET = process.env.BIGQUERY_DATASET || "ceo_dashboard";

function createClient(): BigQuery {
  const keyPath = process.env.GOOGLE_APPLICATION_CREDENTIALS || "./service-account-key.json";
  const resolvedPath = path.resolve(keyPath);

  if (fs.existsSync(resolvedPath)) {
    return new BigQuery({
      projectId: PROJECT_ID,
      keyFilename: resolvedPath,
      scopes: [
        "https://www.googleapis.com/auth/bigquery",
        "https://www.googleapis.com/auth/drive.readonly",
      ],
    });
  }

  return new BigQuery({
    projectId: PROJECT_ID,
    scopes: [
      "https://www.googleapis.com/auth/bigquery",
      "https://www.googleapis.com/auth/drive.readonly",
    ],
  });
}

let _client: BigQuery | null = null;

function getClient(): BigQuery {
  if (!_client) {
    _client = createClient();
  }
  return _client;
}

/**
 * Execute a BigQuery SQL query and return typed rows.
 * Uses parameterized queries to prevent SQL injection.
 */
export async function queryBigQuery<T>(
  sql: string,
  params?: Record<string, unknown>
): Promise<T[]> {
  const client = getClient();

  try {
    const [rows] = await client.query({
      query: sql,
      params,
      location: "US",
    });
    return rows as T[];
  } catch (error) {
    console.error("BigQuery query error:", error);
    console.error("SQL:", sql);
    if (params) console.error("Params:", JSON.stringify(params));
    throw error;
  }
}

/**
 * Per-table dataset overrides.
 * Tables not listed here fall back to the default DATASET (ceo_dashboard).
 */
const TABLE_DATASET: Record<string, string> = {
  // goatsDB
  goats_db_clean_dev: "goatsDB",
  mother_kid_facts: "goatsDB",
  birth_analysis_view: "goatsDB",
  breedwise_kidding_8m: "goatsDB",
  v_birth_count_last_10_days: "goatsDB",
  kidding_frequency: "goatsDB",
  // farm
  daily_summary_dev: "farm",
  kids_counting_vs_weighing: "farm",
  adg_summary_age_shed: "farm",
  // procurement_farm
  load_Wise_pct_data: "procurement_farm",
  load_wise_procurement_with_status: "procurement_farm",
  breedwise_load_pct: "procurement_farm",
  procurement_db_loadwise_cost: "procurement_farm",
  // Shiftings
  deaths_monthly_trend_v: "Shiftings",
  mortality_genderwise: "Shiftings",
  cbe_kids_current_stage_days: "Shiftings",
  cpt_kids_current_stage_days: "Shiftings",
  // feedDB
  last_10_loads_feedwise: "feedDB",
  feed_daily_spend: "feedDB",
  feed_daily_expense_feedwise: "feedDB",
  feed_breed_age_daily: "feedDB",
  last_7_days_feed_per_animal: "feedDB",
  last_7_days_feed_per_animal_shedwise: "feedDB",
  // Feed consumption
  feedDB_clean: "feedDB",
  // crop_season
  seasons_clean: "crop_season",
};

/**
 * Safely serialize a BigQuery date/timestamp value to a YYYY-MM-DD string.
 * BigQuery returns date objects like { value: "2024-05-15" } instead of plain strings.
 */
export function serializeDate(d: unknown): string {
  if (!d) return "";
  if (typeof d === "object" && d !== null && "value" in d) {
    return String((d as { value: unknown }).value).split("T")[0];
  }
  return String(d).split("T")[0];
}

/** Helper: fully qualified table name */
export function table(name: string): string {
  const ds = TABLE_DATASET[name] || DATASET;
  return `\`${PROJECT_ID}.${ds}.${name}\``;
}
