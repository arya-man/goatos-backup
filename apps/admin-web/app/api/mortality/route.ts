/* eslint-disable @typescript-eslint/no-explicit-any */
import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import {
  MORTALITY_SUMMARY_OVERALL,
  MORTALITY_SUMMARY_MONTH,
  MORTALITY_BY_BREED,
  MORTALITY_BY_FARM,
  MORTALITY_BY_LOAD,
  MOTHER_MORTALITY_BREED,
  MOTHER_MORTALITY_LITTER,
  KIDS_MORTALITY_LITTER,
  KIDS_MORTALITY_SPLIT,
  TRENDS_BY_STATUS,
  TRENDS_BY_HOUSING,
  TRENDS_BY_SEASON,
  TRENDS_BY_GENDER,
} from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    const { searchParams } = new URL(request.url);
    const period = searchParams.get("period") || "overall";

    if (USE_BIGQUERY) {
      const isThisMonth = period === "this-month";

      const breedTable = isThisMonth
        ? table("mortality_this_month_dev")
        : table("mortality_overall_breedwise_dev");

      // Delivery tables differ between overall and this-month
      const deliveryBreedTable = isThisMonth
        ? table("last_month_mother_mortality_breed_dev")
        : table("mother_litter_size_dev_breedwise");
      const deliveryLitterTable = isThisMonth
        ? table("last_month_mother_mortality_litter_dev")
        : table("mother_litter_size_dev_overall");
      const deliverySplitTable = isThisMonth
        ? table("last_month_mortality_by_litter_size_view")
        : table("mortality_by_litter_size_overall_dev");

      const [
        breedRows,
        farmRows,
        loadRows,
        trendRows,
        genderRows,
        deliveryRows,
        deliveryOverallRows,
        deliverySplitRows,
        procStatusRows,
        breedLoadRows,
        summaryRows,
      ] = await Promise.all([
        queryBigQuery(`SELECT * FROM ${breedTable}`),
        queryBigQuery(`SELECT * FROM ${table(isThisMonth ? "mortality_this_month_farmwise_dev" : "overall_farmwise_mortality_dev")}`),
        queryBigQuery(`SELECT * FROM ${table("load_Wise_pct_data")}`),
        queryBigQuery(`SELECT * FROM ${table("deaths_monthly_trend_v")}`),
        isThisMonth
          ? queryBigQuery(`SELECT gender, mortality_pct_mtd FROM ${table("mortality_trend_dev")} WHERE section = 'mortality_genderwise_table' AND gender IS NOT NULL AND TRIM(gender) != ''`)
          : queryBigQuery(`SELECT * FROM ${table("mortality_genderwise")}`),
        queryBigQuery(`SELECT * FROM ${deliveryBreedTable}`),
        queryBigQuery(`SELECT * FROM ${deliveryLitterTable}`),
        queryBigQuery(`SELECT * FROM ${deliverySplitTable}`),
        queryBigQuery(`SELECT Farm, animal_status, COUNT(*) as count FROM ${table("load_wise_procurement_with_status")} GROUP BY Farm, animal_status`),
        queryBigQuery(`SELECT * FROM ${table("breedwise_load_pct")}`),
        queryBigQuery(`SELECT * FROM ${table("mortality_total_dev")}`),
      ]);

      const round2 = (v: any) => parseFloat(Number(v || 0).toFixed(2));

      // Filter out rows with empty/null/Mixed breed
      const filteredBreedRows = (breedRows as any[])
        .filter((r: any) => {
          const b = String(r.breed || '').trim().toLowerCase();
          return b !== '' && b !== 'mixed';
        });

      // Column names differ: overall uses plain names, this-month uses _mtd suffix
      const byBreedCols = isThisMonth
        ? { kid: "kid_death_share_pct_mtd", abort: "abortion_share_pct_mtd", adult: "adult_death_share_pct_mtd", total: "death_share_pct_mtd" }
        : { kid: "kid_death_share_pct", abort: "abortion_share_pct", adult: "adult_death_share_pct", total: "death_share_pct" };

      const withinBreedCols = isThisMonth
        ? { kid: "kid_mortality_pct_mtd", abort: "abortion_pct_mtd", adult: "adult_mortality_pct_mtd", total: "total_mortality_pct_mtd" }
        : { kid: "kid_mortality_pct_overall", abort: "abortion_pct_overall", adult: "adult_mortality_pct_overall", total: "total_mortality_pct_overall" };

      // "By Breed" = share of total deaths attributed to each breed (sorted by totalMortality asc)
      const breedwise = filteredBreedRows.map((r: any) => ({
        breed: r.breed,
        kidMortality: round2(r[byBreedCols.kid]),
        kidAbortion: round2(r[byBreedCols.abort]),
        adultMortality: round2(r[byBreedCols.adult]),
        totalMortality: round2(r[byBreedCols.total]),
      })).sort((a, b) => a.totalMortality - b.totalMortality);

      // "Within Breed" = % of animals within that breed that died (sorted by totalMortality asc)
      const breedwiseWithin = filteredBreedRows.map((r: any) => ({
        breed: r.breed,
        kidMortality: round2(r[withinBreedCols.kid]),
        kidAbortion: round2(r[withinBreedCols.abort]),
        adultMortality: round2(r[withinBreedCols.adult]),
        totalMortality: round2(r[withinBreedCols.total]),
      })).sort((a, b) => a.totalMortality - b.totalMortality);

      const filteredFarmRows = (farmRows as any[])
        .filter((r: any) => {
          const farm = r.farm || r.shed_tag || '';
          return farm && String(farm).trim() !== '';
        });

      const byFarmCols = isThisMonth
        ? { kid: "kid_mortality_pct_mtd", abort: "kid_abortion_pct_mtd", adult: "adult_mortality_pct_mtd", total: "total_mortality_pct_mtd" }
        : { kid: "kid_deaths_share_of_total_pct", abort: "abortions_share_of_total_pct", adult: "adult_deaths_share_of_total_pct", total: "total_deaths_share_of_total_pct" };

      const withinFarmCols = isThisMonth
        ? { kid: "kid_deaths_share_of_total_pct_mtd", abort: "abortions_share_of_total_pct_mtd", adult: "adult_deaths_share_of_total_pct_mtd", total: "total_deaths_share_of_total_pct_mtd" }
        : { kid: "kid_mortality_pct_overall", abort: "abortion_pct_overall", adult: "adult_mortality_pct_overall", total: "farm_mortality_pct_overall" };

      // "By Farm" = share of total deaths attributed to each farm
      const farmwise = filteredFarmRows.map((r: any) => ({
        farm: r.farm || r.shed_tag || '',
        kidMortality: round2(r[byFarmCols.kid]),
        kidAbortion: round2(r[byFarmCols.abort]),
        adultMortality: round2(r[byFarmCols.adult]),
        totalMortality: round2(r[byFarmCols.total]),
      }));

      // "Within Farm" = % of animals within that farm that died
      const farmwiseWithin = filteredFarmRows.map((r: any) => ({
        farm: r.farm || r.shed_tag || '',
        kidMortality: round2(r[withinFarmCols.kid]),
        kidAbortion: round2(r[withinFarmCols.abort]),
        adultMortality: round2(r[withinFarmCols.adult]),
        totalMortality: round2(r[withinFarmCols.total]),
      }));

      const loadwise = (loadRows as any[])
        .filter((r: any) => {
          const label = String(r.load_vendor_label || r.Load_ID || '').trim();
          return label !== '';
        })
        .map((r: any) => {
          const rawLabel = String(r.load_vendor_label || r.Load_ID || '');
          return {
            name: rawLabel.replace(/\n/g, ' – '),
            value: round2(r.mortality_percent),
            vendor: r.Vendor || r.vendor || '',
            breed: r.Breed || r.breed || '',
            mortality: round2(r.mortality_percent),
            display: r.display || '',
          };
        });

      // Aggregate season trends by month_season_label (multiple rows per month)
      const seasonMap = new Map<string, number>();
      for (const r of trendRows as any[]) {
        const label = String(r.month_season_label || '').replace(/\n/g, ' ').trim();
        if (!label) continue;
        seasonMap.set(label, (seasonMap.get(label) ?? 0) + Number(r.deaths || 0));
      }
      const trendsBySeason = Array.from(seasonMap.entries()).map(([name, value]) => ({
        name,
        value: round2(value),
      }));

      const trendsByGender = (genderRows as any[])
        .filter((r: any) => r.gender && String(r.gender).trim() !== '')
        .map((r: any) => ({
          name: r.gender,
          value: round2(isThisMonth ? r.mortality_pct_mtd : r.mortality_pct),
        }));

      // Derive byStatus (shed_tag) and byHousing (shed)
      let trendsByStatus: { name: string; value: number }[];
      let trendsByHousing: { name: string; value: number }[];

      if (isThisMonth) {
        // This Month: use mortality_trend_dev with section = mortalitybystatus
        const mtdStatusRows = await queryBigQuery(
          `SELECT shed_tag, shed, death_burden_pct FROM ${table("mortality_trend_dev")} WHERE section = 'mortalitybystatus'`
        );
        const statusMap = new Map<string, number>();
        const housingMap = new Map<string, number>();
        for (const r of mtdStatusRows as any[]) {
          const tag = String(r.shed_tag ?? '').trim();
          const shed = String(r.shed ?? '').trim();
          const pct = round2(r.death_burden_pct);
          if (tag) statusMap.set(tag, (statusMap.get(tag) ?? 0) + pct);
          if (shed) housingMap.set(shed, (housingMap.get(shed) ?? 0) + pct);
        }
        trendsByStatus = Array.from(statusMap.entries())
          .map(([name, value]) => ({ name, value: round2(value) }))
          .sort((a, b) => b.value - a.value);
        trendsByHousing = Array.from(housingMap.entries())
          .map(([name, value]) => ({ name, value: round2(value) }))
          .sort((a, b) => b.value - a.value);
      } else {
        // Overall: COUNT(DISTINCT goat_id) per status/housing, then percent of total
        const deathsFactRows = await queryBigQuery(`SELECT goat_id, shed_tag, shed FROM ${table("deaths_fact_dev")}`);
        const statusGoats = new Map<string, Set<string>>();
        const housingGoats = new Map<string, Set<string>>();
        const allStatusGoats = new Set<string>();
        const allHousingGoats = new Set<string>();
        for (const r of deathsFactRows as any[]) {
          const goatId = String(r.goat_id ?? '').trim();
          if (!goatId) continue;
          const tag = String(r.shed_tag ?? '').trim();
          const shed = String(r.shed ?? '').trim();
          if (tag) {
            if (!statusGoats.has(tag)) statusGoats.set(tag, new Set());
            statusGoats.get(tag)!.add(goatId);
            allStatusGoats.add(goatId);
          }
          if (shed) {
            if (!housingGoats.has(shed)) housingGoats.set(shed, new Set());
            housingGoats.get(shed)!.add(goatId);
            allHousingGoats.add(goatId);
          }
        }
        const totalDistinctStatus = allStatusGoats.size;
        const totalDistinctHousing = allHousingGoats.size;
        trendsByStatus = Array.from(statusGoats.entries())
          .map(([name, goats]) => ({ name, value: round2(totalDistinctStatus > 0 ? (goats.size / totalDistinctStatus) * 100 : 0) }))
          .sort((a, b) => b.value - a.value);
        trendsByHousing = Array.from(housingGoats.entries())
          .map(([name, goats]) => ({ name, value: round2(totalDistinctHousing > 0 ? (goats.size / totalDistinctHousing) * 100 : 0) }))
          .sort((a, b) => b.value - a.value);
      }

      const motherByBreed = (deliveryRows as any[])
        .filter((r: any) => {
          const b = String(r.mother_breed || r.breed || '').trim().toLowerCase();
          return b !== '' && b !== 'mixed';
        })
        .map((r: any) => ({
          name: r.mother_breed || r.breed,
          value: round2(
            isThisMonth
              ? (r.pct_of_total_delivering_mothers_last30 ?? r.pct_of_total_delivering_mothers)
              : (r.pct_of_total_delivering_mothers ?? r.mortality_pct)
          ),
        }));

      // Mother mortality by litter size
      const motherByLitter = (deliveryOverallRows as any[]).map((r: any) => ({
        name: String(r.litter_size || ''),
        value: round2(
          isThisMonth
            ? (r.mortality_pct_last_30_days ?? r.mortality_pct)
            : (r.mortality_pct)
        ),
      }));

      // Kids mortality by litter size (from split table — total including abortions)
      const kidsByLitter = (deliverySplitRows as any[]).map((r: any) => ({
        name: String(r.litter_size || ''),
        value: round2(r.mortality_pct_incl_abort),
      }));

      const kidsSplit = (deliverySplitRows as any[]).map((r: any) => ({
        name: String(r.litter_size || ''),
        death: round2(r.post_birth_death_pct),
        abortion: round2(r.abortion_pct),
      }));

      // Mortality by farm from procurement status data
      const deathStatuses = new Set(['dead', 'abortion', 'died']);
      const farmDeathCounts = new Map<string, number>();
      let totalDeathCount = 0;
      for (const r of procStatusRows as any[]) {
        const farm = String(r.Farm || '').trim();
        const status = String(r.animal_status || '').trim().toLowerCase();
        if (!farm || !deathStatuses.has(status)) continue;
        const count = Number(r.count || 0);
        farmDeathCounts.set(farm, (farmDeathCounts.get(farm) ?? 0) + count);
        totalDeathCount += count;
      }
      const loadwiseFarmMortality = Array.from(farmDeathCounts.entries()).map(([name, count]) => ({
        name,
        value: totalDeathCount > 0 ? round2((count / totalDeathCount) * 100) : 0,
      }));

      // Breed-wise mortality by vendor (from breedwise_load_pct table)
      const breedByVendor = (breedLoadRows as any[])
        .filter((r: any) => {
          const breed = String(r.Breed || r.breed || '').trim();
          const vendor = String(r.Vendor || r.vendor || '').trim();
          return breed !== '' && vendor !== '' && breed.toLowerCase() !== 'boer';
        })
        .map((r: any) => ({
          breed: String(r.Breed || r.breed || '').trim(),
          vendor: String(r.Vendor || r.vendor || '').trim(),
          mortality: round2(r.mortality_percent),
        }))
        .filter((r: any) => r.mortality > 0);

      // Summary from mortality_total_dev table
      const summaryRow = (summaryRows as any[])[0] || {};
      const totalDeaths = Number(isThisMonth ? summaryRow.total_deaths_mtd : summaryRow.total_deaths_overall) || 0;
      const kidDeaths = Number(isThisMonth ? summaryRow.kid_deaths_mtd : summaryRow.kid_deaths_overall) || 0;
      const adultDeaths = Number(isThisMonth ? summaryRow.adult_deaths_mtd : summaryRow.adult_deaths_overall) || 0;
      const totalBase = Number(summaryRow.total_base || 0);
      const mortalityRate = isThisMonth
        ? (totalBase > 0 ? `${round2((totalDeaths / totalBase) * 100)}%` : "0%")
        : `${round2(Number(summaryRow.overall_mortality_pct_full_base_all_time || 0))}%`;
      const kidMortalityRate = isThisMonth
        ? `${round2(Number(summaryRow.overall_mortality_pct_births_only || 0))}%`
        : `${round2(Number(summaryRow.overall_mortality_pct_births_only_all_time || 0))}%`;
      const adultMortalityRate = isThisMonth
        ? `${round2(Number(summaryRow.overall_mortality_pct_purchased_only || 0))}%`
        : `${round2(Number(summaryRow.overall_mortality_pct_purchased_only_all_time || 0))}%`;

      return NextResponse.json({
        summary: {
          totalDeaths,
          kidDeaths,
          adultDeaths,
          mortalityRate,
          kidMortalityRate,
          adultMortalityRate,
        },
        breedwise,
        breedwiseWithin,
        farmwise,
        farmwiseWithin,
        loadwise,
        loadwiseFarmMortality,
        breedByVendor,
        delivery: {
          motherByBreed,
          motherByLitter,
          kidsByLitter,
          kidsSplit,
        },
        trends: {
          byStatus: trendsByStatus,
          byHousing: trendsByHousing,
          bySeason: trendsBySeason,
          byGender: trendsByGender,
        },
      });
    }

    const summary = period === "month" ? MORTALITY_SUMMARY_MONTH : MORTALITY_SUMMARY_OVERALL;

    return NextResponse.json({
      summary,
      breedwise: MORTALITY_BY_BREED,
      breedwiseWithin: MORTALITY_BY_BREED,
      farmwise: MORTALITY_BY_FARM,
      farmwiseWithin: MORTALITY_BY_FARM,
      loadwise: MORTALITY_BY_LOAD,
      delivery: {
        motherByBreed: MOTHER_MORTALITY_BREED,
        motherByLitter: MOTHER_MORTALITY_LITTER,
        kidsByLitter: KIDS_MORTALITY_LITTER,
        kidsSplit: KIDS_MORTALITY_SPLIT,
      },
      trends: {
        byStatus: TRENDS_BY_STATUS,
        byHousing: TRENDS_BY_HOUSING,
        bySeason: TRENDS_BY_SEASON,
        byGender: TRENDS_BY_GENDER,
      },
    });
  } catch (error) {
    console.error("Error fetching mortality data:", error);
    return NextResponse.json({ error: "Failed to fetch mortality data" }, { status: 500 });
  }
}
