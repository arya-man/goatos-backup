import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { VisualBaseline } from "./visual-baseline.mjs";

function setup(waived) {
  const dir = mkdtempSync(join(tmpdir(), "visual-baseline-"));
  writeFileSync(join(dir, "waivers.json"), JSON.stringify({ note: "t", waived }));
  return dir;
}

test("--update-baseline never adds an integrity finding to the waivers", () => {
  const dir = setup(["clipped-text|home|desktop|dark|h1"]);
  const b = new VisualBaseline({ lane: "t", manifestDir: dir, pngDir: null, diffDir: null, updateBaseline: true, requireBaseline: false });
  const live = b.unwaived("home|desktop|dark", [
    { check: "clipped-text", target: "h1", detail: "known" },
    { check: "double-skeleton", target: "main", detail: "NEW" },
  ]);
  assert.deepEqual(live.map((f) => f.key), ["double-skeleton|home|desktop|dark|main"]);
  const next = b.nextWaivers();
  assert.deepEqual(next.waived, ["clipped-text|home|desktop|dark|h1"]);
  assert.deepEqual(next.added, []);
  b.writeUpdated({ waiverNote: "n" });
  const written = JSON.parse(readFileSync(join(dir, "waivers.json"), "utf8"));
  assert.deepEqual(written.waived, ["clipped-text|home|desktop|dark|h1"]);
});

test("--update-baseline prunes a waiver that no longer reproduces in a visited context, keeps unvisited ones", () => {
  const dir = setup(["clipped-text|home|desktop|dark|h1", "clipped-text|verify|desktop|dark|h1"]);
  const b = new VisualBaseline({ lane: "t", manifestDir: dir, pngDir: null, diffDir: null, updateBaseline: true, requireBaseline: false });
  b.unwaived("home|desktop|dark", []);
  assert.deepEqual(b.nextWaivers().waived, ["clipped-text|verify|desktop|dark|h1"]);
});

test("only an explicit --waive reason records new findings, and the reason is kept beside the key", () => {
  const dir = setup([]);
  const b = new VisualBaseline({ lane: "t", manifestDir: dir, pngDir: null, diffDir: null, updateBaseline: false, requireBaseline: true, waive: { reason: "known 2-phase on legacy route until PR-300" } });
  b.unwaived("home|desktop|dark", [{ check: "double-skeleton", target: "main", detail: "x" }]);
  const next = b.nextWaivers();
  assert.deepEqual(next.waived, ["double-skeleton|home|desktop|dark|main"]);
  b.writeUpdated({ waiverNote: "n" });
  const written = JSON.parse(readFileSync(join(dir, "waivers.json"), "utf8"));
  assert.equal(written.reasons["double-skeleton|home|desktop|dark|main"], "known 2-phase on legacy route until PR-300");
});

test("a blank --waive reason is not a waiver", () => {
  const dir = setup([]);
  const b = new VisualBaseline({ lane: "t", manifestDir: dir, pngDir: null, diffDir: null, updateBaseline: false, requireBaseline: true, waive: { reason: "  " } });
  b.unwaived("home|desktop|dark", [{ check: "double-skeleton", target: "main", detail: "x" }]);
  assert.deepEqual(b.nextWaivers().waived, []);
  b.writeUpdated({ waiverNote: "n" });
  assert.equal(existsSync(join(dir, "manifest.json")), false);
});
