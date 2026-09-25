import assert from "node:assert/strict";
import { test } from "node:test";
import { groupMissingRates, groupRetiredFeedGaps } from "./missing-rates.ts";

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

test("a retired-only session is listed once with its pen count; an experiment pen by name", () => {
  const retired = { feed_item: "Hay", status: "blocked", blocked_reason: { code: "all_feeds_retired" } };
  const rows = [
    { ration_group: "Beetal", shed_tag: "K5", shed_label: "Castro", partition_label: "2", workflow: "normal", session_label: "Evening", items: [retired] },
    // The same pen from another grain: still one entry.
    { ration_group: "Sirohi", shed_tag: "K5", shed_label: "Castro", partition_label: "2", workflow: "normal", session_label: "Evening", items: [retired] },
    { ration_group: "", shed_tag: "F2", shed_label: "Godel 1", partition_label: "Part 3", workflow: "experiment", session_label: "Morning", items: [retired] },
    { ration_group: "", shed_tag: "F2", shed_label: "Godel 1", partition_label: "Part 3", workflow: "experiment", session_label: "Evening", items: [retired] },
    // A missing RATE is the other alert's, never this one's.
    { ration_group: "Beetal", shed_tag: "K4", shed_label: "Castro", partition_label: "1", workflow: "normal", session_label: "Morning",
      items: [{ feed_item: "Hay", status: "blocked", blocked_reason: { code: "no_ration_rate" } }] },
  ];
  assert.deepEqual(groupRetiredFeedGaps(rows, pen), {
    sessions: [{ session: "Evening", pens: 1 }],
    experimentPens: ["Godel 1 - Part 3"],
  });
  assert.deepEqual(groupMissingRates(rows, pen).map((gap) => gap.pens), [["Castro - 1"]]);
});
