import { getCommandBoardShedDoseMatrix } from "@/lib/api/server";
import { drilldownScopeFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The shed x dose grid, as its own section.
//
// It shipped inline on the board until two measurements, in this order. Interning its shed
// identities and dose labels cut the section from 408KB to 155KB (the whole board payload from
// 441KB to 207KB), and the board's p90 barely moved -- the cost was the round trip, not the
// payload. Taking the section off first paint is what moved it: p90 343 with it inline, p90
// 251-281 without, against a p90 300ms budget the latency policy will not let anyone raise.
export async function GET(request: Request) {
  return jsonOrError(await getCommandBoardShedDoseMatrix(drilldownScopeFromRequest(request)));
}
