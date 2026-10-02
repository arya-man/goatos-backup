import test from "node:test";
import assert from "node:assert/strict";

import { openablePageKeys, pagesToSave, webPermissionsFromTicks } from "./access-pages.ts";

const rows = [
  {
    module_key: "sales",
    pages: [
      { page_key: "sales-sold", required_permissions: ["sales.read"] },
      { page_key: "sales-vendors", required_permissions: ["vendor.sales.read"] },
      { page_key: "sales-sops", required_permissions: ["sop.read"] },
    ],
    web_level_permissions: { view: ["sales.read"], do: ["sales.read", "sales.write"] },
  },
  {
    module_key: "vendors",
    pages: [{ page_key: "procurement-vendors", required_permissions: ["vendor.supply.read"] }],
    web_level_permissions: { view: ["vendor.sales.read", "vendor.supply.read"] },
  },
  { module_key: "config", pages: [], web_level_permissions: { view: ["sop.read"] } },
];

test("a module switched on for the first time offers its screens straight away", () => {
  const ticks = { sales: { web: ["view"], pages: [] } };
  const held = webPermissionsFromTicks(rows, ticks);
  assert.deepEqual(openablePageKeys(rows[0], held), ["sales-sold"]);
  // ...and the save carries them, so ONE save is enough.
  assert.deepEqual(pagesToSave(rows, ticks).sales, ["sales-sold"]);
});

test("a screen's authority can come from another module's web tick", () => {
  const ticks = { sales: { web: ["view"], pages: [] }, config: { web: ["view"], pages: [] } };
  assert.deepEqual(openablePageKeys(rows[0], webPermissionsFromTicks(rows, ticks)), ["sales-sold", "sales-sops"]);
});

test("phone ticks never open a web screen", () => {
  // Only `web` levels are read; a phone-only Vendors grant contributes nothing.
  const ticks = { sales: { web: ["view"], pages: [] }, vendors: { web: [], pages: [] } };
  assert.ok(!openablePageKeys(rows[0], webPermissionsFromTicks(rows, ticks)).includes("sales-vendors"));
});

test("ticks the current levels no longer open are dropped, and a module off keeps none", () => {
  const ticks = {
    sales: { web: ["view"], pages: ["sales-sold", "sales-vendors"] },
    vendors: { web: [], pages: ["procurement-vendors"] },
  };
  const saved = pagesToSave(rows, ticks);
  assert.deepEqual(saved.sales, ["sales-sold"]);
  assert.deepEqual(saved.vendors, []);
});

test("a narrowed choice is kept as the admin left it", () => {
  const ticks = {
    sales: { web: ["view"], pages: ["sales-sold"] },
    config: { web: ["view"], pages: [] },
  };
  assert.deepEqual(pagesToSave(rows, ticks).sales, ["sales-sold"]);
});
