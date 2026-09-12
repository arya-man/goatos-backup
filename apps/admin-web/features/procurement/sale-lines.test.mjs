import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { newSaleLine, saleLinesTotals } from "./sale-lines.ts";

test("the running total sums every line and treats a half-typed or blank box as nothing", () => {
  const lines = [
    { ...newSaleLine(1, "Sheep"), breed: "Anantapur", animals: "10", weightKg: "300", value: "120000" },
    { ...newSaleLine(2, "Sheep"), breed: "Kenguri", animals: "5", weightKg: "140.5", value: "56000" },
    { ...newSaleLine(3, "Goat"), breed: "Sirohi", animals: "", weightKg: "12.", value: "45000" },
  ];
  assert.deepEqual(saleLinesTotals(lines), { value: 221000, animals: 15, weightKg: 452.5 });
  assert.deepEqual(saleLinesTotals([]), { value: 0, animals: 0, weightKg: 0 });
});

test("a new line starts blank on the product it was handed", () => {
  assert.deepEqual(newSaleLine(4, "Goat"), { id: 4, product: "Goat", breed: "", animals: "", weightKg: "", value: "" });
});

// The editor POSTS indexed field names and the server action READS them back; the two are in
// different files, so pin the shape once here rather than let one side rename and the other
// silently record a sale with no lines.
test("the editor's posted field names are exactly what readSaleForm reads", () => {
  const editor = readFileSync(new URL("./sale-lines-editor.tsx", import.meta.url), "utf8");
  const actions = readFileSync(new URL("./sales-actions.ts", import.meta.url), "utf8");
  for (const key of ["line_product_type", "line_breed", "line_animal_count", "line_total_weight_kg", "line_sales_value"]) {
    assert.match(editor, new RegExp("name=\\{`" + key + "_\\$\\{index\\}`\\}"), `editor posts ${key}`);
    assert.match(actions, new RegExp("`" + key + "_\\$\\{i\\}`"), `action reads ${key}`);
  }
  // The deal-level product/breed/value are NOT posted: the backend rolls them up from the lines.
  assert.doesNotMatch(actions, /requiredString\(formData, "product_type"\)/);
  assert.doesNotMatch(actions, /requiredString\(formData, "sales_value"\)/);
});
