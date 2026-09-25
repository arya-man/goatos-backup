import assert from "node:assert/strict";
import test from "node:test";

import { modulesWithWork, withLoadedDegradedCards } from "./work-board-counts.ts";

const summary = { total: 12, by_lane: { todo: 5, in_progress: 4, in_review: 3, done: 0 }, by_module: { feed: 12 }, park_id: "" };

test("a timed-out module's loaded cards are added to their columns", () => {
  const cards = [
    { parkKey: "cbe", lane: "todo", module: "vaccination" },
    { parkKey: "cbe", lane: "todo", module: "vaccination" },
    { parkKey: "cbe", lane: "in_review", module: "vaccination" },
    { parkKey: "cbe", lane: "todo", module: "feed" }, // counted fine: already in the numbers
    { parkKey: "cpt", lane: "todo", module: "vaccination" }, // counted fine at the other park
  ];
  const got = withLoadedDegradedCards(summary, cards, new Map([["cbe", new Set(["vaccination"])]]));
  assert.deepEqual(got.by_lane, { todo: 7, in_progress: 4, in_review: 4, done: 0 });
  assert.equal(got.total, 15);
  assert.equal(got.by_module.vaccination, 3);
  assert.equal(got.by_module.feed, 12);
  assert.equal(got.park_id, "");
});

test("nothing timed out leaves the server's numbers exactly as they were", () => {
  const got = withLoadedDegradedCards(summary, [{ parkKey: "cbe", lane: "todo", module: "feed" }], new Map());
  assert.equal(got, summary);
});

test("the module menu hides modules with no work, but keeps timed-out and selected ones", () => {
  const options = [{ key: "feed" }, { key: "toxin" }, { key: "procurement" }, { key: "vaccination" }, { key: "health" }];
  const got = modulesWithWork(options, { feed: 4, toxin: 0 }, new Set(["vaccination"]), ["health"]);
  assert.deepEqual(got.map((o) => o.key), ["feed", "vaccination", "health"]);
});
