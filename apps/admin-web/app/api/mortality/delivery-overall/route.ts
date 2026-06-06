import { NextResponse } from "next/server";
import { USE_BIGQUERY } from "@/lib/constants";
import { queryBigQuery, table } from "@/lib/bigquery";
import { MOTHER_MORTALITY_LITTER, KIDS_MORTALITY_LITTER } from "@/lib/data/mortality";

export const dynamic = 'force-dynamic';

export async function GET() {
  try {
    if (USE_BIGQUERY) {
      const rows = await queryBigQuery(
        `SELECT * FROM ${table("mother_litter_size_dev_overall")}`
      );
      return NextResponse.json({ data: rows });
    }

    // Mock fallback
    return NextResponse.json({
      data: {
        motherByLitter: MOTHER_MORTALITY_LITTER,
        kidsByLitter: KIDS_MORTALITY_LITTER,
      },
    });
  } catch (error) {
    console.error("Error fetching delivery-overall mortality:", error);
    return NextResponse.json(
      { error: "Failed to fetch delivery-overall mortality data" },
      { status: 500 }
    );
  }
}
