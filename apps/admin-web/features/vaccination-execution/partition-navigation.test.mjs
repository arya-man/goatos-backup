import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const board = readFileSync(new URL("./execution-board.tsx", import.meta.url), "utf8");
const drilldown = readFileSync(new URL("./shed-drilldown.tsx", import.meta.url), "utf8");
const route = readFileSync(new URL("../../app/(admin)/vaccination/execution/sheds/[shedId]/page.tsx", import.meta.url), "utf8");
const server = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("vaccination execution drilldown uses exact shed identity only", () => {
  assert.doesNotMatch(board, /partition_label:\s*row\.partition_label\s*\?\?\s*undefined/);
  assert.doesNotMatch(route, /partitionLabel=\{one\(sp, "partition_label"\)\}/);
  assert.match(route, /<ShedExecutionDetailPage/);
  assert.match(drilldown, /getVaccinationExecutionShedDrilldown\(shedId, \{ asOf \}\)/);
  assert.doesNotMatch(server, /partition_label:\s*params\.partitionLabel/);
});
