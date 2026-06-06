import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import {
  getOverallADG,
  getADGByBreed,
  getADGByGender,
  getADGByStatus,
  getADGByGenderBreed,
  getADGByBreedStatus,
} from "@/lib/data/fattening";

export const dynamic = 'force-dynamic';

export async function GET(request: Request) {
  try {
    if (USE_BIGQUERY) {
      const { searchParams } = new URL(request.url);
      const level = searchParams.get("level");
      const breed = searchParams.get("breed");
      const gender = searchParams.get("gender");
      const status = searchParams.get("status");

      const conditions: string[] = [];
      const params: Record<string, unknown> = {};

      if (level) {
        conditions.push("level = @level");
        params.level = level;
      }
      if (breed) {
        conditions.push("breed = @breed");
        params.breed = breed;
      }
      if (gender) {
        conditions.push("gender = @gender");
        params.gender = gender;
      }
      if (status) {
        conditions.push("status = @status");
        params.status = status;
      }

      const whereClause = conditions.length
        ? `WHERE ${conditions.join(" AND ")}`
        : "";

      const rows = await queryBigQuery(
        `SELECT * FROM ${table("adg_summary_age_shed")} ${whereClause}`,
        Object.keys(params).length ? params : undefined
      );

      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({
      data: {
        overall: getOverallADG(),
        byBreed: getADGByBreed(),
        byGender: getADGByGender(),
        byStatus: getADGByStatus(),
        byGenderBreed: getADGByGenderBreed(),
        byBreedStatus: getADGByBreedStatus(),
      },
    });
  } catch (error) {
    console.error("Error fetching ADG data:", error);
    return NextResponse.json(
      { error: "Failed to fetch ADG data" },
      { status: 500 }
    );
  }
}
