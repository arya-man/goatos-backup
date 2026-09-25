import assert from "node:assert/strict";
import test from "node:test";

import { penDetailParams, vaccinationCurrentViewScope } from "./shed-scope.ts";

test("vaccination current-view scope strips historical as_of instead of emptying the live board", () => {
  const scope = { asOf: "2026-07-01", domain: "pc.vaccination" };

  assert.deepEqual(vaccinationCurrentViewScope(scope), {});
});

test("vaccination current-view scope still preserves park_id", () => {
  const park = "30000000-0000-4000-8000-000000000001";
  const scope = { parkId: park, asOf: "2026-07-01" };

  assert.deepEqual(vaccinationCurrentViewScope(scope), { parkId: park });
});

test("a pen board row opens its own pen, not the whole building", () => {
  assert.deepEqual(penDetailParams({ partitionLabel: "2" }, "/vaccination#sheds"), {
    partition_label: "2",
    ret: "/vaccination#sheds",
  });
  assert.deepEqual(penDetailParams({ partitionLabel: "Part 3" }, "r"), { partition_label: "Part 3", ret: "r" });
  assert.deepEqual(penDetailParams({ partitionLabel: null }, "r"), { partition_label: undefined, ret: "r" });
});

test("the pen board builds its detail link through penDetailParams", async () => {
  const { readFileSync } = await import("node:fs");
  const board = readFileSync(new URL("./shed-board.tsx", import.meta.url), "utf8");
  assert.match(board, /penDetailParams\(row, hrefWith\(\{\}\)\)/);
});
