import assert from "node:assert/strict";
import { test } from "node:test";
import { groupMissingRates } from "./missing-rates.ts";

const pen = (row) => (row.partition_label ? `${row.shed_label} - ${row.partition_label}` : row.shed_label);

test("each missing combination lists every pen it blocks, once, in natural order", () => {
  const blocked = { feed_item: "Hay", status: "blocked", blocked_reason: { code: "no_ration_rate" } };
  const rows = [
    { ration_group: "Beetal", shed_tag: "K5", shed_label: "Godel 1", partition_label: "Part 10", items: [blocked] },
    { ration_group: "Beetal", shed_tag: "K5", shed_label: "Godel 1", partition_label: "Part 2", items: [blocked] },
    // The same pen's second session: counted once.
    { ration_group: "Beetal", shed_tag: "K5", shed_label: "Godel 1", partition_label: "Part 2", items: [blocked] },
    { ration_group: "Beetal", shed_tag: "K4", shed_label: "Castro", partition_label: "1", items: [{ feed_item: "Hay", status: "resolved" }] },
  ];
  assert.deepEqual(groupMissingRates(rows, pen), [
    { rationGroup: "Beetal", shedTag: "K5", feedItem: "Hay", pens: ["Godel 1 - Part 2", "Godel 1 - Part 10"] },
  ]);
});

test("a cell blocked for another reason is not a missing rate", () => {
  const rows = [
    {
      ration_group: "Beetal", shed_tag: "?", shed_label: "Castro", partition_label: "1",
      items: [{ feed_item: "Hay", status: "blocked", blocked_reason: { code: "unknown_shed_tag" } }],
    },
  ];
  assert.deepEqual(groupMissingRates(rows, pen), []);
});
