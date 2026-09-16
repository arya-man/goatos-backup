import test from "node:test";
import assert from "node:assert/strict";

import { byParkThen, parkRank, parksInArrivalOrder } from "./park-order.ts";

// The park order is the backend's served order (CBE, then CPT), never the label's spelling:
// by full name "Channapatna" (CPT) sorts ahead of "Coimbatore" (CBE), which is the defect.
test("clusters rows by the served park order, not alphabetically, and keeps the within-order", () => {
  const order = ["Coimbatore", "Channapatna"];
  const rows = [
    { park: "Channapatna", pen: "Castro 2" },
    { park: "Coimbatore", pen: "Yashoda" },
    { park: "Channapatna", pen: "Castro 1" },
    { park: "Coimbatore", pen: "Castro 1" },
  ];
  const sorted = rows.slice().sort(byParkThen(order, (r) => r.park, (a, b) => a.pen.localeCompare(b.pen)));
  assert.deepEqual(
    sorted.map((r) => `${r.park}/${r.pen}`),
    ["Coimbatore/Castro 1", "Coimbatore/Yashoda", "Channapatna/Castro 1", "Channapatna/Castro 2"],
  );
});

test("an unknown park ranks after every served one, and ties keep arrival order", () => {
  const rank = parkRank(["CBE", "CPT"]);
  assert.equal(rank("CBE"), 0);
  assert.equal(rank("CPT"), 1);
  assert.equal(rank(""), 2);
  const rows = [{ park: "" , id: 1 }, { park: "CPT", id: 2 }, { park: "", id: 3 }, { park: "CBE", id: 4 }];
  assert.deepEqual(rows.slice().sort(byParkThen(["CBE", "CPT"], (r) => r.park)).map((r) => r.id), [4, 2, 1, 3]);
});

test("parksInArrivalOrder keeps the backend's order and drops repeats", () => {
  const rows = [{ p: "CBE" }, { p: "CBE" }, { p: "CPT" }, { p: "CBE" }];
  assert.deepEqual(parksInArrivalOrder(rows, (r) => r.p), ["CBE", "CPT"]);
});
