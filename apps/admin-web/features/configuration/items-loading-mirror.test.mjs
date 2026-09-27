import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: configuration-items-loading (SK1). /configuration/items had no loading.tsx (a sidebar click left
// the old page up). Its loading.tsx and route-skeleton entry draw the page: header + action, the Grid
// of the register rail (NavRailSkeleton) and the register card, whose twin is also the page's
// register-switch fallback.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("configuration items has a registered loading twin that mirrors the page", () => {
  const loading = read("../../app/(admin)/configuration/items/loading.tsx");
  const page = read("./items-page.tsx");
  assert.match(loading, /size: ITEMS_RAIL_SIZE, node: <NavRailSkeleton groups=\{ITEMS_RAIL_GROUPS\} \/>/);
  assert.match(loading, /size: ITEMS_REGISTER_SIZE, node: <ItemsRegisterSkeleton /);
  assert.match(page, /<Grid size=\{ITEMS_RAIL_SIZE\}>/);
  assert.match(page, /<Grid size=\{ITEMS_REGISTER_SIZE\}>/);
  assert.match(page, /fallback=\{<ItemsRegisterSkeleton rows=\{params\.limit\} \/>\}/);
  assert.match(read("../../components/route-skeleton.tsx"), /\[\/\^\\\/configuration\\\/items\(\?:\\\/\|\$\)\/, L\d+\]/);
});
