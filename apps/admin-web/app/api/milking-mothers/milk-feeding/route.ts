import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";

// Pre-aggregated shape from milk_feeding_dashboard
// (one row per shed per day — already pivoted by BigQuery)
export interface MilkFeedingDashRow {
  report_date: string;
  farm: string;
  shed_tag: string;
  total_kids: number;
  // Fed in Attempt 1 per session
  fed_a1_s1: number; fed_a1_s2: number; fed_a1_s3: number; fed_a1_s4: number;
  // Refused Attempt 1 per session
  a1_refused_s1: number; a1_refused_s2: number; a1_refused_s3: number; a1_refused_s4: number;
  // A1 success % per session (pre-computed)
  a1_success_pct_s1: number; a1_success_pct_s2: number; a1_success_pct_s3: number; a1_success_pct_s4: number;
  // Recovered in Attempt 2 per session
  recovered_a2_s1: number; recovered_a2_s2: number; recovered_a2_s3: number; recovered_a2_s4: number;
  // ORS given per session
  ors_count_s1: number; ors_count_s2: number; ors_count_s3: number; ors_count_s4: number;
  // ORS refused per session (critical alert)
  ors_refused_s1: number; ors_refused_s2: number; ors_refused_s3: number; ors_refused_s4: number;
  // Daily aggregates (pre-computed)
  avg_a1_refused: number;
  avg_ors_given: number;
  overall_a1_success_pct: number;
  total_ors_refused: number;
  // Status
  has_ors_refusal_alert: boolean;
  health_status: "GOOD" | "WARNING" | "CRITICAL";
}

// ── Mock data (dev / table-not-yet-created fallback) ──
const MOCK_DATA: MilkFeedingDashRow[] = [
  // ── CBE ──
  {
    report_date: "2025-05-01", farm: "CBE", shed_tag: "K1", total_kids: 26,
    fed_a1_s1: 24, fed_a1_s2: 23, fed_a1_s3: 25, fed_a1_s4: 24,
    a1_refused_s1: 2, a1_refused_s2: 3, a1_refused_s3: 1, a1_refused_s4: 2,
    a1_success_pct_s1: 92.3, a1_success_pct_s2: 88.5, a1_success_pct_s3: 96.2, a1_success_pct_s4: 92.3,
    recovered_a2_s1: 1, recovered_a2_s2: 2, recovered_a2_s3: 1, recovered_a2_s4: 1,
    ors_count_s1: 1, ors_count_s2: 1, ors_count_s3: 0, ors_count_s4: 1,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 0, ors_refused_s4: 0,
    avg_a1_refused: 2.0, avg_ors_given: 0.8, overall_a1_success_pct: 92.3,
    total_ors_refused: 0, has_ors_refusal_alert: false, health_status: "WARNING",
  },
  {
    report_date: "2025-05-01", farm: "CBE", shed_tag: "K2", total_kids: 82,
    fed_a1_s1: 77, fed_a1_s2: 78, fed_a1_s3: 76, fed_a1_s4: 79,
    a1_refused_s1: 5, a1_refused_s2: 4, a1_refused_s3: 6, a1_refused_s4: 3,
    a1_success_pct_s1: 93.9, a1_success_pct_s2: 95.1, a1_success_pct_s3: 92.7, a1_success_pct_s4: 96.3,
    recovered_a2_s1: 3, recovered_a2_s2: 3, recovered_a2_s3: 4, recovered_a2_s4: 2,
    ors_count_s1: 2, ors_count_s2: 1, ors_count_s3: 2, ors_count_s4: 1,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 1, ors_refused_s4: 0,
    avg_a1_refused: 4.5, avg_ors_given: 1.5, overall_a1_success_pct: 94.5,
    total_ors_refused: 1, has_ors_refusal_alert: true, health_status: "WARNING",
  },
  {
    report_date: "2025-05-01", farm: "CBE", shed_tag: "K3", total_kids: 45,
    fed_a1_s1: 42, fed_a1_s2: 43, fed_a1_s3: 41, fed_a1_s4: 43,
    a1_refused_s1: 3, a1_refused_s2: 2, a1_refused_s3: 4, a1_refused_s4: 2,
    a1_success_pct_s1: 93.3, a1_success_pct_s2: 95.6, a1_success_pct_s3: 91.1, a1_success_pct_s4: 95.6,
    recovered_a2_s1: 3, recovered_a2_s2: 2, recovered_a2_s3: 3, recovered_a2_s4: 2,
    ors_count_s1: 0, ors_count_s2: 0, ors_count_s3: 1, ors_count_s4: 0,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 0, ors_refused_s4: 0,
    avg_a1_refused: 2.8, avg_ors_given: 0.3, overall_a1_success_pct: 93.9,
    total_ors_refused: 0, has_ors_refusal_alert: false, health_status: "WARNING",
  },
  {
    report_date: "2025-05-01", farm: "CBE", shed_tag: "Kids ICU", total_kids: 12,
    fed_a1_s1: 8, fed_a1_s2: 9, fed_a1_s3: 7, fed_a1_s4: 8,
    a1_refused_s1: 4, a1_refused_s2: 3, a1_refused_s3: 5, a1_refused_s4: 4,
    a1_success_pct_s1: 66.7, a1_success_pct_s2: 75.0, a1_success_pct_s3: 58.3, a1_success_pct_s4: 66.7,
    recovered_a2_s1: 2, recovered_a2_s2: 2, recovered_a2_s3: 3, recovered_a2_s4: 2,
    ors_count_s1: 2, ors_count_s2: 1, ors_count_s3: 2, ors_count_s4: 2,
    ors_refused_s1: 1, ors_refused_s2: 0, ors_refused_s3: 1, ors_refused_s4: 1,
    avg_a1_refused: 4.0, avg_ors_given: 1.8, overall_a1_success_pct: 66.7,
    total_ors_refused: 3, has_ors_refusal_alert: true, health_status: "CRITICAL",
  },
  // ── CPT ──
  {
    report_date: "2025-05-01", farm: "CPT", shed_tag: "K1", total_kids: 35,
    fed_a1_s1: 32, fed_a1_s2: 33, fed_a1_s3: 31, fed_a1_s4: 33,
    a1_refused_s1: 3, a1_refused_s2: 2, a1_refused_s3: 4, a1_refused_s4: 2,
    a1_success_pct_s1: 91.4, a1_success_pct_s2: 94.3, a1_success_pct_s3: 88.6, a1_success_pct_s4: 94.3,
    recovered_a2_s1: 2, recovered_a2_s2: 2, recovered_a2_s3: 3, recovered_a2_s4: 2,
    ors_count_s1: 1, ors_count_s2: 0, ors_count_s3: 1, ors_count_s4: 0,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 0, ors_refused_s4: 0,
    avg_a1_refused: 2.8, avg_ors_given: 0.5, overall_a1_success_pct: 92.1,
    total_ors_refused: 0, has_ors_refusal_alert: false, health_status: "WARNING",
  },
  {
    report_date: "2025-05-01", farm: "CPT", shed_tag: "K2", total_kids: 60,
    fed_a1_s1: 56, fed_a1_s2: 57, fed_a1_s3: 55, fed_a1_s4: 56,
    a1_refused_s1: 4, a1_refused_s2: 3, a1_refused_s3: 5, a1_refused_s4: 4,
    a1_success_pct_s1: 93.3, a1_success_pct_s2: 95.0, a1_success_pct_s3: 91.7, a1_success_pct_s4: 93.3,
    recovered_a2_s1: 3, recovered_a2_s2: 2, recovered_a2_s3: 3, recovered_a2_s4: 3,
    ors_count_s1: 1, ors_count_s2: 1, ors_count_s3: 2, ors_count_s4: 1,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 0, ors_refused_s4: 0,
    avg_a1_refused: 4.0, avg_ors_given: 1.3, overall_a1_success_pct: 93.3,
    total_ors_refused: 0, has_ors_refusal_alert: false, health_status: "WARNING",
  },
  {
    report_date: "2025-05-01", farm: "CPT", shed_tag: "K3", total_kids: 30,
    fed_a1_s1: 28, fed_a1_s2: 29, fed_a1_s3: 27, fed_a1_s4: 28,
    a1_refused_s1: 2, a1_refused_s2: 1, a1_refused_s3: 3, a1_refused_s4: 2,
    a1_success_pct_s1: 93.3, a1_success_pct_s2: 96.7, a1_success_pct_s3: 90.0, a1_success_pct_s4: 93.3,
    recovered_a2_s1: 2, recovered_a2_s2: 1, recovered_a2_s3: 2, recovered_a2_s4: 2,
    ors_count_s1: 0, ors_count_s2: 0, ors_count_s3: 1, ors_count_s4: 0,
    ors_refused_s1: 0, ors_refused_s2: 0, ors_refused_s3: 0, ors_refused_s4: 0,
    avg_a1_refused: 2.0, avg_ors_given: 0.3, overall_a1_success_pct: 93.3,
    total_ors_refused: 0, has_ors_refusal_alert: false, health_status: "WARNING",
  },
];

export async function GET(request: Request) {
  const { searchParams } = new URL(request.url);
  const dateParam = searchParams.get("date"); // YYYY-MM-DD or null (→ most recent)

  try {
    if (USE_BIGQUERY) {
      try {
        const dateFilter = dateParam
          ? `WHERE FORMAT_DATE('%Y-%m-%d', report_date) = '${dateParam}'`
          : `WHERE report_date = (SELECT MAX(report_date) FROM ${table("milk_feeding_dashboard")})`;

        const rows = await queryBigQuery<MilkFeedingDashRow>(
          `SELECT
             FORMAT_DATE('%Y-%m-%d', report_date) AS report_date,
             farm, shed_tag, total_kids,
             fed_a1_s1, fed_a1_s2, fed_a1_s3, fed_a1_s4,
             a1_refused_s1, a1_refused_s2, a1_refused_s3, a1_refused_s4,
             a1_success_pct_s1, a1_success_pct_s2, a1_success_pct_s3, a1_success_pct_s4,
             recovered_a2_s1, recovered_a2_s2, recovered_a2_s3, recovered_a2_s4,
             ors_count_s1, ors_count_s2, ors_count_s3, ors_count_s4,
             ors_refused_s1, ors_refused_s2, ors_refused_s3, ors_refused_s4,
             avg_a1_refused, avg_ors_given, overall_a1_success_pct,
             total_ors_refused, has_ors_refusal_alert, health_status
           FROM ${table("milk_feeding_dashboard")}
           ${dateFilter}
           ORDER BY farm, shed_tag`
        );
        return NextResponse.json({ data: rows });
      } catch (bqErr: unknown) {
        const code = (bqErr as { code?: number })?.code;
        if (code === 404) {
          console.warn("milk_feeding_dashboard not found in BigQuery — using mock data");
        } else {
          throw bqErr;
        }
      }
    }

    const mockFiltered = dateParam
      ? MOCK_DATA.filter((r) => r.report_date === dateParam)
      : MOCK_DATA;
    return NextResponse.json({ data: mockFiltered });
  } catch (error) {
    console.error("Error fetching milk feeding data:", error);
    return NextResponse.json(
      { error: "Failed to fetch milk feeding data" },
      { status: 500 }
    );
  }
}
