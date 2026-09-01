import { getCommandBoardCohortExceptions } from "@/lib/api/server";
import { badCell, cohortCellFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The dose-sequence exceptions behind ONE cohort matrix cell.
export async function GET(request: Request) {
  const cell = cohortCellFromRequest(request);
  if (!cell) return badCell();
  return jsonOrError(await getCommandBoardCohortExceptions(cell));
}
