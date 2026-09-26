import { test } from "node:test";
import assert from "node:assert/strict";
import { PNG } from "pngjs";
import { fingerprintPng, hashDistance, integrityKey, judgeLoadPhases, RENDER_INTEGRITY_PROBE, RENDER_INTEGRITY_CHECKS } from "./render-integrity.mjs";

function png(width, height, paint) {
  const image = new PNG({ width, height });
  for (let y = 0; y < height; y += 1) for (let x = 0; x < width; x += 1) {
    const i = (y * width + x) * 4;
    const v = paint(x, y);
    image.data[i] = v; image.data[i + 1] = v; image.data[i + 2] = v; image.data[i + 3] = 255;
  }
  return image;
}

test("fingerprint is stable and hash distance separates moved pictures", () => {
  const a = png(64, 64, (x) => (x < 32 ? 0 : 255));
  const b = png(64, 64, (x) => (x < 32 ? 0 : 255));
  const c = png(64, 64, (x, y) => (y < 32 ? 0 : 255));
  const fa = fingerprintPng(a);
  assert.equal(fa.width, 64);
  assert.equal(fa.hash.length, 64);
  assert.equal(fa.sha256, fingerprintPng(b).sha256);
  assert.equal(hashDistance(fa.hash, fingerprintPng(b).hash), 0);
  assert.ok(hashDistance(fa.hash, fingerprintPng(c).hash) > 0.3, "rotated split must be far apart");
});

test("integrity key is check|context|target and the probe covers the documented checks", () => {
  assert.equal(integrityKey("kit-kpicard--default|desktop|dark", { check: "clipped-text", target: "span.x" }), "clipped-text|kit-kpicard--default|desktop|dark|span.x");
  const source = RENDER_INTEGRITY_PROBE.toString();
  for (const check of RENDER_INTEGRITY_CHECKS.filter((c) => !["console-error", "double-skeleton", "blank-frame"].includes(c))) {
    assert.ok(source.includes(`"${check}"`), `probe must be able to emit ${check}`);
  }
});

test("load phases ignore the runner's warm-up document (the app renders /robots.txt as its 404 page)", () => {
  const withWarmup = [
    { t: 0, skeleton: 10, content: 0, shell: true, href: "http://x/robots.txt", docStart: 0 },
    { t: 100, skeleton: 0, content: 40, shell: true, href: "http://x/robots.txt", docStart: 0 },
    { t: 200, skeleton: 10, content: 0, shell: true, href: "http://x/sales/sold", docStart: 200 },
    { t: 300, skeleton: 10, content: 2, shell: true, href: "http://x/sales/sold", docStart: 200 },
    { t: 400, skeleton: 0, content: 40, shell: true, href: "http://x/sales/sold", docStart: 200 },
  ];
  assert.equal(judgeLoadPhases(withWarmup).map((f) => f.check).join(","), "double-skeleton");
  assert.deepEqual(judgeLoadPhases(withWarmup, { ignorePaths: ["/robots.txt"] }), []);
});

test("load phases: one skeleton then content is fine; skeleton→content→skeleton and blank frames fail", () => {
  const ok = [{ t: 0, skeleton: 10, content: 0, shell: true }, { t: 100, skeleton: 10, content: 2, shell: true }, { t: 200, skeleton: 0, content: 40, shell: true }];
  assert.deepEqual(judgeLoadPhases(ok), []);
  const twice = [{ t: 0, skeleton: 10, content: 0, shell: true }, { t: 100, skeleton: 0, content: 40, shell: true }, { t: 200, skeleton: 12, content: 1, shell: true }, { t: 300, skeleton: 0, content: 40, shell: true }];
  assert.equal(judgeLoadPhases(twice).map((f) => f.check).join(","), "double-skeleton");
  const chain = [{ t: 0, docStart: 0, skeleton: 10, content: 0, shell: true }, { t: 100, docStart: 0, skeleton: 10, content: 0, shell: true }, { t: 200, docStart: 180, skeleton: 12, content: 0, shell: true }, { t: 400, docStart: 180, skeleton: 0, content: 40, shell: true }];
  assert.equal(judgeLoadPhases(chain).map((f) => f.check).join(","), "double-skeleton", "a redirect that remounts the skeleton is a second phase");
  const blank = [{ t: 0, skeleton: 0, content: 0, shell: false }, { t: 200, skeleton: 0, content: 0, shell: false }, { t: 400, skeleton: 0, content: 30, shell: true }];
  assert.equal(judgeLoadPhases(blank).map((f) => f.check).join(","), "blank-frame");
});

test("raw floats and gauge centres are checked inside Apex charts too", () => {
  const source = RENDER_INTEGRITY_PROBE.toString();
  assert.ok(source.includes(".apexcharts-datalabel-value"), "chart data labels must be scanned for raw floats");
  assert.ok(source.includes(".kit-radial[aria-label]"), "a RadialStat centre must equal its aria-label");
});
