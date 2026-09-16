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
    (name) => name.startsWith("@/") ? load(path.join(root, name.slice(2)))
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
    labels: { ariaLabel: "Buyers", none: "None", repeat: "Repeat", oneTime: "One-time", settled: "Settled", empty: "Empty" },
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
