import { ItemsPage, itemsPageParams, refRegistersOf, type ItemsPageData } from "@/features/configuration";
import { getReferenceList, listCatalogueLists, listConfigurationOptions, listConfigurationRegisters, listConfigurationRows } from "@/lib/api/configuration-server";
import { requireAdminWebPageContract } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";

export const dynamic = "force-dynamic";

// Configuration -> Items and settings (maintainer instruction 2026-09-18). Three reads: the
// register catalog with the rail counts, ONE page of the selected register, and the ref options
// of every register the selected one's columns point at (at most three, never a fan-out over
// rows). Switching register is a real navigation (`?register=`).
export default async function Page({ searchParams }: { searchParams: Promise<RouteSearchParams> }) {
  const [params, pageContract] = await Promise.all([searchParams, requireAdminWebPageContract("configuration-items")]);
  const scope = itemsPageParams(params, pageContract);
  const registers = await listConfigurationRegisters();
  const register = registers.ok ? registers.data.registers.find((item) => item.key === scope.register) : undefined;
  const refs = refRegistersOf(register);
  const catalogue = register?.layout === "catalogue";
  const [rows, listsRead, openListRead, ...optionReads] = await Promise.all([
    register ? listConfigurationRows(register.key, { status: scope.status, q: scope.q, cursor: scope.cursor, limit: scope.limit, filters: scope.filters }) : Promise.resolve(null),
    // The catalogue layout's Lists panel: a different register (the categories), one bounded read.
    catalogue ? listCatalogueLists() : Promise.resolve(null),
    // A dynamic reference-list register: the reference_lists row it renders, for its own drawer.
    register?.list_key ? getReferenceList(register.list_key) : Promise.resolve(null),
    ...refs.map((ref) => listConfigurationOptions(ref)),
  ]);
  const options: ItemsPageData["options"] = {};
  const loadErrors: ItemsPageData["loadErrors"] = [];
  if (listsRead && !listsRead.ok) loadErrors.push(listsRead.error);
  if (openListRead && !openListRead.ok) loadErrors.push(openListRead.error);
  refs.forEach((ref, index) => {
    const read = optionReads[index];
    if (read && read.ok) {
      options[ref] = read.data.options;
      return;
    }
    if (read && !read.ok) loadErrors.push(read.error);
    options[ref] = [];
  });
  const data: ItemsPageData = { registers, rows, options, lists: listsRead && listsRead.ok ? listsRead.data.rows : null, openList: openListRead && openListRead.ok ? openListRead.data.row : null, loadErrors };
  return <ItemsPage searchParams={params} pageContract={pageContract} data={data} />;
}
