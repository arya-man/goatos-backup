import { getCommandBoardDriveOptions } from "@/lib/api/server";
import { jsonOrError } from "../scope";

export const dynamic = "force-dynamic";

// The drive picker's full catalogue, keyset-paginated.
//
// The board carries only its first page (20). Shipping the catalogue eagerly was 448ms and 753 KB
// -- more than the endpoint's entire 512 KB budget -- for a dropdown.
export async function GET(request: Request) {
  const params = new URL(request.url).searchParams;
  const limit = Number(params.get("limit"));
  return jsonOrError(
    await getCommandBoardDriveOptions({
      parkId: params.get("park_id") ?? undefined,
      limit: Number.isFinite(limit) && limit > 0 ? limit : undefined,
      cursor: params.get("cursor") ?? undefined,
    }),
  );
}
