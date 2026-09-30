import { RoutinesPage, routinesPageParams, type RoutinesPageData } from "@/features/pen-routines";
import { getPenRoutineCatalog, listPenRoutineParkTasks, listPenRoutines, listPenRoutineTabs } from "@/lib/api/pen-routines-server";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Routines -- recurring pen checks per park (maintainer instruction 2026-09-16,
// docs/decisions/pen-routines.md). Three reads: the routines (all parks or the chosen one), ONE
// catalog for the page's park (the drawer's pens, people and vocabularies -- a routine belongs to
// one park, so the drawer only ever needs one), and one page of that park's tasks for the chosen
// business day. Switching the drawer to another park is a real navigation (`?park=`), never a
// second catalog read from this page.
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("pen-routines")]);
  const scope = routinesPageParams(params, pageContract);
  const list = await listPenRoutines({ park_id: scope.parkId });
  const parks = list.ok ? list.data.parks : [];
  const todayPark = (scope.parkId ? parks.find((park) => park.park_id === scope.parkId) : parks[0]) ?? null;
  const [catalog, tasks, tabs, allRoutines] = await Promise.all([
    todayPark ? getPenRoutineCatalog(todayPark.park_id) : Promise.resolve(null),
    todayPark
      ? listPenRoutineParkTasks({
          park_id: todayPark.park_id,
          business_date: scope.businessDate,
          routine_id: scope.routineId,
          cursor: scope.cursor,
          limit: scope.limit,
        })
      : Promise.resolve(null),
    // Phone tabs: where routines appear on the phone. A tab may carry routines of either park, so
    // its picker needs every park's routines -- the list above already is that unless a park is chosen.
    listPenRoutineTabs(),
    scope.parkId ? listPenRoutines({}) : Promise.resolve(list),
  ]);
  const data: RoutinesPageData = {
    list,
    catalog: catalog && catalog.ok ? catalog.data : null,
    tasks,
    todayPark,
    businessDate: scope.businessDate,
    tabs,
    tabRoutines: allRoutines.ok ? allRoutines.data.rows : [],
  };
  return <RoutinesPage searchParams={params} pageContract={pageContract} data={data} />;
}
