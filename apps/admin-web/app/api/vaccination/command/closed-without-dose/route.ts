import { NextResponse } from "next/server";
import { getCommandBoardClosedWithoutDose } from "@/lib/api/server";
import { drilldownScopeFromRequest, jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The animals behind the command board's closedWithoutDose tile.
//
// A route handler rather than part of the page render: the tile's drawer opens from data the board
// already has and fetches only the missing detail inside it, so opening it must not re-run the
// route (the local-overlay rule).
export async function GET(request: Request) {
  return jsonOrError(await getCommandBoardClosedWithoutDose(drilldownScopeFromRequest(request)));
}
