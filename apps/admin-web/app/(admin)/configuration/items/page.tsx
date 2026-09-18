import { ItemsPage, itemsPageParams, refRegistersOf, type ItemsPageData } from "@/features/configuration";
import { listConfigurationOptions, listConfigurationRegisters, listConfigurationRows } from "@/lib/api/configuration-server";
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
  const [rows, ...optionReads] = await Promise.all([
    register ? listConfigurationRows(register.key, { status: scope.status, q: scope.q, cursor: scope.cursor, limit: scope.limit, filters: scope.filters }) : Promise.resolve(null),
    ...refs.map((ref) => listConfigurationOptions(ref)),
  ]);
  const options: ItemsPageData["options"] = {};
  refs.forEach((ref, index) => {
    const read = optionReads[index];
    options[ref] = read && read.ok ? read.data.options : [];
  });
  const data: ItemsPageData = { registers, rows, options };
  return <ItemsPage searchParams={params} pageContract={pageContract} data={data} />;
}
