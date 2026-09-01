import { getCommandBoardCohortDays } from "@/lib/api/server";
import { badCell, cohortCellFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The administered-day split for ONE cohort matrix cell.
export async function GET(request: Request) {
  const cell = cohortCellFromRequest(request);
  if (!cell) return badCell();
  return jsonOrError(await getCommandBoardCohortDays(cell));
}
