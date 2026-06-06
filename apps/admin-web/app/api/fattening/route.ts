/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, serializeDate, table } from "@/lib/bigquery";
import {
  getFatteningStages,
  getFatteningTotals,
  getOverallADG,
  getADGByGenderBreed,
  getADGByBreed,
  getADGByStatus,
  getADGByGender,
  getADGByBreedStatus,
  getKidsWeighedPct,
  getLoadADGByBreed,
  getAvgWeightByLoad,
  getLast5WeighingsByLoad,
  getWeightChanges,
} from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

function buildFarmwiseData(rows: any[]) {
  const num = (v: any) => Math.round(Number(v || 0) * 100) / 100;

  const buildMetrics = (r: any) => ({
    count: Number(r.goats_weighed_latest || 0),
    weight: num(r.total_weight_kg),
    avgWeight: num(r.avg_weight_kg),
    value: num(r.total_value),
    males: Number(r.male_goats || 0),
    females: Number(r.female_goats || 0),
    over30: Number(r.goats_gt_30kg || 0),
    over35: Number(r.goats_gt_35kg || 0),
  });

  const empty = { count: 0, weight: 0, avgWeight: 0, value: 0, males: 0, females: 0, over30: 0, over35: 0 };

  const cbeTotal = rows.find((r: any) => r.farm === "CBE" && r.shed_tag_out === "TOTAL");
  const cptTotal = rows.find((r: any) => r.farm === "CPT" && r.shed_tag_out === "TOTAL");
  const cbe = cbeTotal ? buildMetrics(cbeTotal) : empty;
  const cpt = cptTotal ? buildMetrics(cptTotal) : empty;
  const total = {
    count: cbe.count + cpt.count,
    weight: Math.round((cbe.weight + cpt.weight) * 100) / 100,
    avgWeight: (cbe.count + cpt.count) > 0 ? Math.round((cbe.weight + cpt.weight) / (cbe.count + cpt.count) * 100) / 100 : 0,
    value: Math.round((cbe.value + cpt.value) * 100) / 100,
    males: cbe.males + cpt.males,
    females: cbe.females + cpt.females,
    over30: cbe.over30 + cpt.over30,
    over35: cbe.over35 + cpt.over35,
  };

  const overview = { total, cbe, cpt };

  // Demographics: per-stage data grouped by farm
  const stages = ["F1", "F2", "K0", "K1", "K2", "K3"];
  const buildDemo = (farm: string | null) => {
    return stages.map((stage) => {
      const matching = rows.filter((r: any) =>
        r.shed_tag_out === stage && (farm === null || r.farm === farm)
      );
      if (matching.length === 0) {
        return { stage, count: null, weight: null, avgWeight: null, value: null, males: null, females: null };
      }
      const count = matching.reduce((s: number, r: any) => s + Number(r.goats_live || 0), 0);
      const weight = matching.reduce((s: number, r: any) => s + Number(r.total_weight_kg || 0), 0);
      const value = matching.reduce((s: number, r: any) => s + Number(r.total_value || 0), 0);
      const males = matching.reduce((s: number, r: any) => s + Number(r.male_goats || 0), 0);
      const females = matching.reduce((s: number, r: any) => s + Number(r.female_goats || 0), 0);
      return {
        stage,
        count,
        weight: Math.round(weight * 100) / 100,
        avgWeight: count > 0 ? Math.round(weight / count * 100) / 100 : 0,
        value: Math.round(value * 100) / 100,
        males,
        females,
      };
    });
  };

  const demographics = {
    all: buildDemo(null),
    cbe: buildDemo("CBE"),
    cpt: buildDemo("CPT"),
  };

  return { overview, demographics };
}

function getYesterdayIST() {
  const nowIST = new Date(new Date().toLocaleString("en-US", { timeZone: "Asia/Kolkata" }));
  nowIST.setDate(nowIST.getDate() - 1);
  return `${nowIST.getFullYear()}-${String(nowIST.getMonth() + 1).padStart(2, "0")}-${String(nowIST.getDate()).padStart(2, "0")}`;
}

function buildCountOverview(
  summaryRow: any,
  genderRows: any[],
  fallbackOverview: ReturnType<typeof buildFarmwiseData>["overview"]
) {
  const num = (v: any) => Math.round(Number(v || 0) * 100) / 100;
  const genderMap = genderRows.reduce((map: Record<string, { males: number; females: number }>, row: any) => {
    const farm = String(row.farm || "").toUpperCase();
    const gender = String(row.fattening_gender || "").toLowerCase();
    if (!map[farm]) map[farm] = { males: 0, females: 0 };
    if (gender === "male") map[farm].males += Number(row.total_count || 0);
    if (gender === "female") map[farm].females += Number(row.total_count || 0);
    return map;
  }, {});

  const buildMetrics = (
    baseMetrics: typeof fallbackOverview.total,
    genderMetrics: { males: number; females: number } | undefined,
    countValue: any,
    weightValue: any,
    farmValue: any
  ) => {
    const count = Number(countValue || 0);
    const weight = num(weightValue);

    return {
      ...baseMetrics,
      count,
      weight,
      avgWeight: count > 0 ? Math.round((weight / count) * 100) / 100 : 0,
      value: num(farmValue),
      males: genderMetrics?.males ?? baseMetrics.males,
      females: genderMetrics?.females ?? baseMetrics.females,
    };
  };

  const cbeGender = genderMap.CBE;
  const cptGender = genderMap.CPT;
  const totalGender = {
    males: Number(cbeGender?.males || 0) + Number(cptGender?.males || 0),
    females: Number(cbeGender?.females || 0) + Number(cptGender?.females || 0),
  };

  const cbe = buildMetrics(
    fallbackOverview.cbe,
    cbeGender,
    summaryRow.cbe_kids_count,
    summaryRow.cbe_kids_weight,
    summaryRow.cbe_kids_weight_value
  );
  const cpt = buildMetrics(
    fallbackOverview.cpt,
    cptGender,
    summaryRow.cpt_kids_count,
    summaryRow.cpt_kids_weight,
    summaryRow.cpt_kids_weight_value
  );
  const total = buildMetrics(
    fallbackOverview.total,
    totalGender,
    summaryRow.kids_total_count,
    summaryRow.kids_total_weight,
    summaryRow.kids_total_weight_value
  );

  return { total, cbe, cpt };
}

// Only these loads are displayed on the Loadwise tab
const ALLOWED_PURCHASED_LOADS = new Set(["100", "101", "113", "126"]);
const ALLOWED_VGOAT_LOADS = new Set(["K2", "F2"]);
const ALLOWED_WEIGHT_LABELS = new Set(["100\nGreen Fresh Farm", "101\nDr Praneeth", "113\nNutriplus Foods Pvt Ltd.", "126\nRamesh Reddy", "F2\nVgoat", "K2\nVgoat"]);

const LOAD_SORT_ORDER = [
  "113\nNutriplus Foods Pvt Ltd.",
  "126\nRamesh Reddy",
  "101\nDr Praneeth",
  "100\nGreen Fresh Farm",
  "F2\nVgoat",
  "K2\nVgoat",
];
const loadSortIndex = (label: string) => {
  const idx = LOAD_SORT_ORDER.indexOf(label);
  return idx < 0 ? 999 : idx;
};

function buildLoadwiseData(loadAdgRows: any[], loadWeightRows: any[]) {
  const adgNum = (r: any) => Math.round(Number(r.adg_grams_per_day || 0) * 100) / 100;

  // Load-wise ADG by Breed:
  // Vgoat loads (K2, F2): breed-gender breakdown from VGOAT_LOAD_BREED_GENDER
  // Purchased loads (100, 101, 113): overall ADG from PURCHASED_LOAD
  const vgoatBreedGender = loadAdgRows.filter((r: any) =>
    r.level === "VGOAT_LOAD_BREED_GENDER" &&
    ALLOWED_VGOAT_LOADS.has(r.load_id) &&
    r.breed && r.breed.toLowerCase() !== "mixed" &&
    r.gender && r.gender !== "Mix"
  ).map((r: any) => ({
    load: r.load_vendor_label || `${r.load_id}\nVgoat`,
    breed: `${r.breed} - ${r.gender}`,
    adg: adgNum(r),
  }));

  // Purchased loads: aggregate INDIVIDUAL level by breed-gender
  const purchasedIndividuals = loadAdgRows.filter((r: any) =>
    r.level === "INDIVIDUAL" &&
    ALLOWED_PURCHASED_LOADS.has(String(r.load_id)) &&
    r.breed && r.gender && r.gender !== "Mix"
  );
  // Group by load+breed+gender and average the ADG
  const purchasedMap: Record<string, { label: string; breed: string; gender: string; sum: number; count: number }> = {};
  for (const r of purchasedIndividuals) {
    const key = `${r.load_id}|${r.breed}|${r.gender}`;
    const label = r.load_vendor_label || `${r.load_id}`;
    if (!purchasedMap[key]) purchasedMap[key] = { label, breed: r.breed, gender: r.gender, sum: 0, count: 0 };
    purchasedMap[key].sum += Number(r.adg_grams_per_day || 0);
    purchasedMap[key].count += 1;
  }
  const purchasedLoads = Object.values(purchasedMap).map((v) => ({
    load: v.label,
    breed: `${v.breed} - ${v.gender}`,
    adg: Math.round(v.sum / v.count * 100) / 100,
  }));

  const loadADGByBreed = [...vgoatBreedGender, ...purchasedLoads];

  // Filter weighing rows to allowed loads only
  const filteredWeightRows = loadWeightRows.filter((r: any) => {
    const label = r.metric_label || "";
    return ALLOWED_WEIGHT_LABELS.has(label);
  });

  // Average weight by load: use latest week's avg_weight_kg per load
  const weightByLoad: Record<string, { label: string; maxWeek: number; avgWeight: number }> = {};
  for (const r of filteredWeightRows) {
    const key = r.metric_label || `Load ${r.load_id}`;
    const weekOrder = Number(r.week_order || 0);
    const avgWt = Number(r.avg_weight_kg || 0);
    if (!avgWt) continue;
    if (!weightByLoad[key] || weekOrder > weightByLoad[key].maxWeek) {
      weightByLoad[key] = {
        label: key,
        maxWeek: weekOrder,
        avgWeight: Math.round(avgWt * 100) / 100,
      };
    }
  }
  const avgWeightByLoad = Object.values(weightByLoad)
    .map((v) => ({ load: v.label, avgWeight: v.avgWeight }))
    .sort((a, b) => (loadSortIndex(a.load) - loadSortIndex(b.load)));

  // Last 5 weekly ADG by load: pivot last_7_days_gain_g_per_day (positive values only) into { load, w1..w5 }
  const adgWeekMap: Record<string, Record<string, number>> = {};
  for (const r of filteredWeightRows) {
    const key = r.metric_label || `Load ${r.load_id}`;
    const weekOrder = Number(r.week_order || 0);
    const raw = r.last_7_days_gain_g_per_day;
    const adg = raw != null ? Number(raw) : null;
    if (adg == null || weekOrder < 1 || weekOrder > 5) continue;
    if (!adgWeekMap[key]) adgWeekMap[key] = {};
    adgWeekMap[key][`w${weekOrder}`] = Math.round(adg);
  }
  // Include all loads that have at least one non-null ADG week (negative shown as signed bars)
  const last5Weighings = Object.entries(adgWeekMap).map(([load, weeks]) => ({
    load,
    w1: weeks.w1,
    w2: weeks.w2,
    w3: weeks.w3,
    w4: weeks.w4,
    w5: weeks.w5,
  }));

  // Last 7 Days Gain: use last_7_days_gain_g_per_day from the latest week per load
  const gainByLoad: Record<string, { label: string; maxWeek: number; gain: number | null }> = {};
  for (const r of filteredWeightRows) {
    const key = r.metric_label || `Load ${r.load_id}`;
    const weekOrder = Number(r.week_order || 0);
    if (!gainByLoad[key] || weekOrder > gainByLoad[key].maxWeek) {
      const raw = r.last_7_days_gain_g_per_day;
      gainByLoad[key] = {
        label: key,
        maxWeek: weekOrder,
        gain: raw != null ? Math.round(Number(raw) * 100) / 100 : null,
      };
    }
  }
  const weightChanges = Object.values(gainByLoad).map((v) => ({
    load: v.label,
    gain: v.gain,
  }));

  const sortedLast5 = [...last5Weighings].sort((a, b) => loadSortIndex(a.load) - loadSortIndex(b.load));
  const adgLoadSet = new Set(last5Weighings.map((d) => d.load));
  const sortedWeightChanges = [...weightChanges]
    .filter((d) => adgLoadSet.has(d.load))
    .sort((a, b) => loadSortIndex(a.load) - loadSortIndex(b.load));

  return { loadADGByBreed, avgWeightByLoad, last5Weighings: sortedLast5, weightChanges: sortedWeightChanges };
}

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const yesterdayIST = getYesterdayIST();
      const [farmwiseRows, adgRows, weighingRows, loadAdgRows, loadWeightRows, loadwiseSummaryRows, loadSalesComparisonRows, countSummaryRows, countGenderRows] = await Promise.all([
        queryBigQuery(`SELECT * FROM ${table("growth_farmwise_weighing")}`),
        queryBigQuery(`SELECT * FROM ${table("adg_summary_age_shed")}`),
        queryBigQuery(`SELECT * FROM ${table("kids_counting_vs_weighing")}`),
        queryBigQuery(`SELECT * FROM ${table("adg_goat_last2")}`),
        queryBigQuery(`SELECT * FROM ${table("weighingprogression_loadwise")}`),
        queryBigQuery(`SELECT * FROM ${table("loadwise_summary")} ORDER BY Load_ID DESC`),
        queryBigQuery(`
          SELECT
            CAST(c.Load_ID AS STRING) AS load_id,
            ANY_VALUE(ls.Vendor) AS vendor,
            MAX(c.total_purchase_cost) AS total_purchase_cost,
            SUM(c.total_sales_revenue) AS total_sales_revenue
          FROM ${table("fattening_load_sales_comparison")} c
          LEFT JOIN (
            SELECT Load_ID, ANY_VALUE(Vendor) AS Vendor
            FROM ${table("loadwise_summary")}
            GROUP BY Load_ID
          ) ls
            ON TRIM(CAST(ls.Load_ID AS STRING)) = TRIM(CAST(c.Load_ID AS STRING))
          WHERE c.attrition_pct >= 90
          GROUP BY load_id
          ORDER BY load_id
        `),
        queryBigQuery(`SELECT * FROM ${table("daily_summary_dev")} WHERE date = @yesterday`, { yesterday: yesterdayIST }),
        queryBigQuery(
          `SELECT farm, fattening_gender, SUM(total_count) AS total_count
           FROM ${table("counting_kpis_daily")}
           WHERE as_of_date = @yesterday
             AND metric_name IN ('kids_by_gender', 'genderwise_fattening_count')
             AND LOWER(farm) IN ('cbe', 'cpt')
           GROUP BY farm, fattening_gender`,
          { yesterday: yesterdayIST }
        ),
      ]);

      // Group ADG rows by level
      const adgArr = adgRows as any[];
      const byLevel = (lvl: string) => adgArr.filter((r: any) => r.level === lvl);

      const adgNum = (r: any) => Math.round(Number(r.adg_grams_per_day || 0) * 100) / 100;

      // Overall & farm-level ADG
      const totalRow = byLevel("TOTAL")[0];
      const farmRows = byLevel("FARM");
      const cbeRow = farmRows.find((r: any) => r.farm === "CBE");
      const cptRow = farmRows.find((r: any) => r.farm === "CPT");

      // ADG by Breed — descending by ADG, exclude null breed
      const adgByBreed = byLevel("BREED")
        .filter((r: any) => r.breed)
        .map((r: any) => ({ breed: r.breed, adg: adgNum(r) }))
        .sort((a: any, b: any) => b.adg - a.adg);

      // ADG by Gender — descending by ADG, exclude null gender
      const adgByGender = byLevel("GENDER")
        .filter((r: any) => r.gender)
        .map((r: any) => ({ name: r.gender, value: adgNum(r) }))
        .sort((a: any, b: any) => b.value - a.value);

      // ADG by Gender & Breed — exclude null gender/breed and "Mixed" breed, descending by total ADG
      const genderBreedRows = byLevel("BREED_GENDER")
        .filter((r: any) => r.gender && r.breed && r.breed.toLowerCase() !== "mixed");
      const genderBreedMap: Record<string, { male: number; female: number }> = {};
      for (const r of genderBreedRows) {
        const breed = r.breed || "";
        if (!genderBreedMap[breed]) genderBreedMap[breed] = { male: 0, female: 0 };
        if (r.gender === "Male") genderBreedMap[breed].male = adgNum(r);
        else if (r.gender === "Female") genderBreedMap[breed].female = adgNum(r);
      }
      const adgByGenderBreed = Object.entries(genderBreedMap)
        .map(([breed, vals]) => ({ breed, male: vals.male, female: vals.female }))
        .sort((a, b) => (b.male + b.female) - (a.male + a.female));

      // ADG by Status — descending by ADG
      const adgByStatus = byLevel("SHED_TAG")
        .map((r: any) => ({ status: r.shed_tag, adg: adgNum(r) }))
        .sort((a: any, b: any) => b.adg - a.adg);

      // ADG by Breed & Status — pivot so status (shed_tag) is on Y-axis, breeds are stacked segments
      const shedTagBreedRows = byLevel("SHED_TAG_BREED")
        .filter((r: any) => r.breed && r.shed_tag);
      const breeds = [...new Set(shedTagBreedRows.map((r: any) => r.breed))];
      const statusBreedMap: Record<string, Record<string, number>> = {};
      for (const r of shedTagBreedRows) {
        const status = r.shed_tag || "";
        if (!statusBreedMap[status]) statusBreedMap[status] = {};
        statusBreedMap[status][r.breed] = adgNum(r);
      }
      const adgByBreedStatus = Object.entries(statusBreedMap).map(([status, vals]) => ({
        name: status,
        ...vals,
      }));

      const farmwise = buildFarmwiseData(farmwiseRows as any[]);
      const countSummary = (countSummaryRows as any[])[0];

      return NextResponse.json({
        data: {
          stages: farmwiseRows,
          totals: {},
          overview: countSummary ? buildCountOverview(countSummary, countGenderRows as any[], farmwise.overview) : farmwise.overview,
          demographics: farmwise.demographics,
          adg: {
            overall: totalRow ? adgNum(totalRow) : 0,
            cbe: cbeRow ? adgNum(cbeRow) : 0,
            cpt: cptRow ? adgNum(cptRow) : 0,
          },
          adgByGenderBreed,
          adgByBreed,
          adgByStatus,
          adgByGender,
          adgByBreedStatus,
          adgByBreedStatusKeys: breeds,
          kidsWeighedPct: (() => {
            const wRows = weighingRows as any[];
            // Group by week and farm, compute totals for "total" view
            const weekMap: Record<number, Record<string, { counted: number; weighed: number; pct: number }>> = {};
            for (const r of wRows) {
              const wn = Number(r.week_number || 0);
              const farm = String(r.farm || "").toUpperCase();
              if (!weekMap[wn]) weekMap[wn] = {};
              weekMap[wn][farm] = {
                counted: Number(r.total_kids_counted || 0),
                weighed: Number(r.total_kids_weighed || 0),
                pct: Math.round(Number(r.weighing_percentage || 0) * 100) / 100,
              };
            }
            const weeks = Object.keys(weekMap).map(Number).sort((a, b) => a - b);
            const total = weeks.map((wn) => {
              const farms = weekMap[wn];
              const counted = Object.values(farms).reduce((s, f) => s + f.counted, 0);
              const weighed = Object.values(farms).reduce((s, f) => s + f.weighed, 0);
              return { week: `W${wn}`, pct: counted > 0 ? Math.round(weighed / counted * 10000) / 100 : 0 };
            });
            const byFarm = (f: string) => weeks.map((wn) => {
              const d = weekMap[wn]?.[f];
              return { week: `W${wn}`, pct: d ? d.pct : 0 };
            });
            return { total, cbe: byFarm("CBE"), cpt: byFarm("CPT") };
          })(),
          ...buildLoadwiseData(loadAdgRows as any[], loadWeightRows as any[]),
          loadwiseSummary: (loadwiseSummaryRows as any[]).map((r: any) => ({
            loadId: String(r.Load_ID ?? ""),
            vendor: String(r.Vendor ?? ""),
            procured: Number(r.procured ?? 0),
            sales: Number(r.sales ?? 0),
            mortality: Number(r.mortality ?? 0),
            currentCount: Number(r.current_count ?? 0),
            mortalityRatePct: Number(r.mortality_rate_pct ?? 0),
          })),
          loadAge: (() => {
            const byLoad = new Map<string, { loadId: string; vendor: string; unloadedDate: string; daysSinceUnloading: number }>();
            for (const r of loadwiseSummaryRows as any[]) {
              const loadId = String(r.Load_ID ?? "");
              const vendor = String(r.Vendor ?? "");
              const key = `${loadId}|${vendor}`;
              const daysSinceUnloading = Number(r.days_in_farm ?? 0);
              const existing = byLoad.get(key);
              if (!existing || daysSinceUnloading > existing.daysSinceUnloading) {
                byLoad.set(key, {
                  loadId,
                  vendor,
                  unloadedDate: serializeDate(r.unloaded_date),
                  daysSinceUnloading,
                });
              }
            }
            return Array.from(byLoad.values());
          })(),
          loadSalesComparison: (loadSalesComparisonRows as any[]).map((r: any) => ({
            loadId: String(r.load_id ?? ""),
            vendor: String(r.vendor ?? ""),
            purchaseCost: Number(r.total_purchase_cost ?? 0),
            salesCost: Number(r.total_sales_revenue ?? 0),
          })),
        },
      });
    }

    return NextResponse.json({
      data: {
        stages: getFatteningStages(),
        totals: getFatteningTotals(),
        adg: getOverallADG(),
        adgByGenderBreed: getADGByGenderBreed(),
        adgByBreed: getADGByBreed(),
        adgByStatus: getADGByStatus(),
        adgByGender: getADGByGender(),
        adgByBreedStatus: getADGByBreedStatus(),
        kidsWeighedPct: getKidsWeighedPct(),
        loadADGByBreed: getLoadADGByBreed(),
        avgWeightByLoad: getAvgWeightByLoad(),
        last5Weighings: getLast5WeighingsByLoad(),
        weightChanges: getWeightChanges(),
        loadAge: [],
      },
    });
  } catch (error) {
    console.error("Error fetching fattening data:", error);
    return NextResponse.json(
      { error: "Failed to fetch fattening data" },
      { status: 500 }
    );
  }
}
