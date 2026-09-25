import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

// Execute the production TSX and shared table, including TanStack's row model.
const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(import.meta.url);
const modules = new Map();
function load(filename) {
  if (!path.extname(filename)) filename += existsSync(`${filename}.tsx`) ? ".tsx" : ".ts";
  if (modules.has(filename)) return modules.get(filename).exports;
  const loaded = { exports: {} };
  modules.set(filename, loaded);
  const source = ts.transpileModule(readFileSync(filename, "utf8"), {
    compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  vm.runInThisContext(`(function(require,module,exports){${source}\n})`, { filename })(
    // The table's header sort asks the page router for a new order; a static render never clicks.
    (name) => name === "next/navigation" ? { useRouter: () => ({ replace() {} }) }
      : name.startsWith("@/") ? load(path.join(root, name.slice(2)))
      : name.startsWith(".") ? load(path.resolve(path.dirname(filename), name)) : require(name),
    loaded, loaded.exports,
  );
  return loaded.exports;
}
const { BuyerTable } = load(path.join(root, "features/procurement/buyer-table.tsx"));
const keys = ["buyer_name", "phone_number", "purchases", "animals", "revenue", "repeat", "first_sale_date", "last_sale_date", "outstanding"];
const legacyKeys = [...keys.slice(0, 2), "category", "place", ...keys.slice(2), "share_pct"];
const row = {
  buyer_key: "buyer-1", buyer_name: "Buyer One", phone_number: "1234567890",
  category: "Trader", place: "Town", purchases: 1, animals: 2, revenue: 100,
  share_pct: 100, repeat: false, repeat_purchases: 0, product_types: [],
  first_sale_date: "2026-09-10", last_sale_date: "2026-09-10", outstanding: 0,
  cadence_lines: [], recency: "",
};
function render(columnKeys, showPhones = true, rows = [row]) {
  return renderToStaticMarkup(React.createElement(BuyerTable, {
    contract: { columns: columnKeys.map(key => ({ key, label: key, visible: true, sortable: key !== "phone_number" })) },
    rows, showPhones,
    order: { sort: "", dir: "desc" },
    labels: { ariaLabel: "Buyers", none: "None", repeat: "Repeat", oneTime: "One-time", settled: "Settled", empty: "Empty", sortAll: "sort all rows" },
  }));
}
test("previous backend columns render the same buyer table after rollback", () => {
  assert.equal(render(legacyKeys), render(keys));
  assert.equal(render(legacyKeys, true, []), render(keys, true, []));
});
test("phone suppression survives legacy contracts without hiding buyer detail", () => {
  const html = render(legacyKeys, false);
  assert.doesNotMatch(html, /1234567890|>phone_number</);
  assert.match(html, /Trader.*Town/);
});
test("unknown contract columns remain a hard error", () => {
  assert.throws(() => render([...keys, "unexpected"]), /no cell renderer.*unexpected/);
});

// "Sort all rows" (2026-09-25): the table no longer re-sorts the 25 rows it holds -- that ranked one
// page out of hundreds. It renders the rows in the order the BACKEND served them (which ordered
// every buyer), and a header click asks the page for a new whole-result order.
test("the buyer table renders the backend's whole-result order and never re-sorts its page", () => {
  const older = { ...row, buyer_key: "a", buyer_name: "Older", last_sale_date: "2026-09-01", revenue: 900 };
  const newer = { ...row, buyer_key: "b", buyer_name: "Newer", last_sale_date: "2026-09-10", revenue: 10 };
  // Served revenue-desc: Older (900) then Newer (10). A client-side sort on the default column
  // (newest last sale) would put Newer first.
  const html = renderToStaticMarkup(React.createElement(BuyerTable, {
    contract: { columns: keys.map(key => ({ key, label: key, visible: true, sortable: key !== "phone_number" })) },
    rows: [older, newer], showPhones: true,
    order: { sort: "revenue", dir: "desc" },
    labels: { ariaLabel: "Buyers", none: "None", repeat: "Repeat", oneTime: "One-time", settled: "Settled", empty: "Empty", sortAll: "sort all rows" },
  }));
  assert.ok(html.indexOf("Older") < html.indexOf("Newer"), "rows must stay in the served order");
  assert.match(html, /aria-sort="descending"[^>]*>\s*<button[^>]*aria-label="revenue — sort all rows"/);
});

test("both Sales tables send their order to the backend read", () => {
  for (const file of ["sales-buyer-analytics.tsx", "sales-farm-born.tsx"]) {
    const source = readFileSync(path.join(root, "features/procurement", file), "utf8");
    assert.match(source, /sort: order\.sort \|\| undefined,\s*dir: order\.sort \? order\.dir : undefined,/, file);
  }
  const server = readFileSync(path.join(root, "lib/api/procurement-server.ts"), "utf8");
  assert.match(server, /offset: params\.offset, sort: params\.sort, dir: params\.dir/);
  assert.match(server, /sort: params\.sort,\s*dir: params\.dir,/);
});
