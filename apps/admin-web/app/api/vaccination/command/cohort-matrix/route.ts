import { getCommandBoardCohortMatrix } from "@/lib/api/server";
import { drilldownScopeFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The cohort matrix, as its own section.
//
// It shipped inline on the board until its three statements -- the cell aggregate, the true herd
// head count and the dose-sequence exception count -- were measured at ~420ms of the endpoint's
// ~850ms of SQL, which alone held GET /vaccination/command over a p90 300ms budget the latency
// policy will not let anyone raise. The grid now arrives a moment after the rest of the board.
export async function GET(request: Request) {
  return jsonOrError(await getCommandBoardCohortMatrix(drilldownScopeFromRequest(request)));
}
