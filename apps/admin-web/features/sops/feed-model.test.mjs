// FEED SOP: the editor model round-trips the seeded documents byte-faithfully and pre-checks the
// cheapest rules the backend enforces.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { emitFeed, feedProblems, parseFeed } from "./feed-model.ts";

const seedDir = new URL("../../../../backend/internal/feeddirection/domain/sopseed/", import.meta.url);
const seeds = {
  "feed.direction": JSON.parse(readFileSync(new URL("feed_direction.json", seedDir), "utf8")),
  "feed.packing": JSON.parse(readFileSync(new URL("feed_packing.json", seedDir), "utf8")),
  "feed.transport": JSON.parse(readFileSync(new URL("feed_transport.json", seedDir), "utf8")),
};
const label = (s) => s;

for (const [code, seed] of Object.entries(seeds)) {
  test(`${code}: the seeded document round-trips unchanged`, () => {
    const rows = parseFeed(code, { feed: seed });
    assert.ok(rows);
    assert.deepEqual(emitFeed(rows), seed);
    assert.deepEqual(feedProblems(rows, label), []);
  });
}

test("a photo beside the video, a video replaced by a photo, a dropped capture, a new question", () => {
  const rows = parseFeed("feed.direction", { feed: seeds["feed.direction"] });
  const d = rows.stages.distribution;
  d.proofs[1].kind = "photo";
  d.proofs.push({ id: "x", key: "trough_after", title: "Trough after", hint: "", kind: "photo", required: false });
  d.proofs = d.proofs.filter((p) => p.key !== "feed_distribution_water_video");
  d.questions.push({ id: "q", key: "clean", kind: "choice", title: "Trough clean?", hint: "", required: true, options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }], allowOther: false, min: "", max: "", unit: "", onlyIfQuestion: "", onlyIfValue: "" });
  assert.deepEqual(feedProblems(rows, label), []);
  const doc = emitFeed(rows);
  assert.deepEqual(doc.distribution.proofs.map((p) => [p.key, p.kind, p.required]), [
    ["feed_distribution_feed_weight_photo", "photo", true],
    ["feed_distribution_video", "photo", true],
    ["trough_after", "photo", false],
  ]);
  assert.equal(doc.distribution.questions[0].id, "clean");
  // The wastage card rides along untouched.
  assert.equal(doc.wastage.proofs[0].key, "feed_wastage_video");
});

test("no compulsory capture, a duplicate key and a blank title are named", () => {
  const rows = parseFeed("feed.packing", { feed: seeds["feed.packing"] });
  const p = rows.stages.packing;
  p.proofs[0].required = false;
  p.proofs.push({ id: "y", key: "feed_packing_video", title: "", hint: "", kind: "video", required: false });
  const problems = feedProblems(rows, label);
  assert.ok(problems.some((m) => /compulsory/.test(m)), problems.join("\n"));
  assert.ok(problems.some((m) => /used twice/.test(m)), problems.join("\n"));
  assert.ok(problems.some((m) => /needs a title/.test(m)), problems.join("\n"));
});

test("a direction document published without a wastage card shows the seeded one", () => {
  const rows = parseFeed("feed.direction", { feed: { schema_version: "goatos.sop-feed.v1", distribution: seeds["feed.direction"].distribution } });
  assert.equal(rows.stages.wastage.proofs[0].key, "feed_wastage_video");
});
