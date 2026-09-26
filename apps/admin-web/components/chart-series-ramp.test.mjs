import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// The grouped bars used to draw --brand beside --teal: two greens at a normal-vision ΔE of 12.6,
// which a reader simply cannot separate. The fix is a chart ramp of its own, validated on all pairs
// in BOTH modes, kept apart from the status colours so a bar's hue is an identity, not a verdict.

const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");
const callers = [
  ["../features/procurement/loadwise-section.tsx", "animalSeries"],
  ["../features/counts/counts-breakdown-loads.tsx", "series"],
];

test("every grouped bar tone resolves to a slot of the chart ramp", () => {
  for (const mode of [/:root\{([\s\S]*?)\n  \}/, /:root\.light\{([\s\S]*?)\n  \}/]) {
    const block = css.match(mode);
    assert.ok(block, "both modes must declare the ramp");
    for (const slot of ["--chart-1", "--chart-2", "--chart-3", "--chart-4"]) {
      assert.match(block[1], new RegExp(`${slot}:#[0-9A-Fa-f]{6}`), `${slot} must be stepped for this mode`);
    }
  }
  // A tone that still points at a status token would drift away from the validated set. The
  // grouped columns (ApexCharts) map each tone to its colour in TONE_VAR.
  const grouped = readFileSync(new URL("./grouped-columns.tsx", import.meta.url), "utf8");
  const map = grouped.match(/const TONE_VAR[^=]*=\s*\{([\s\S]*?)\};/);
  assert.ok(map, "the tone map must be present");
  // okHatch is the ok slot drawn striped (a pattern fill), not a colour of its own.
  const all = [...map[1].matchAll(/(\w+):\s*"([^"]+)"/g)];
  assert.deepEqual(all.find((t) => t[1] === "okHatch")?.slice(2), ["var(--chart-2)"], "okHatch is the striped ok slot");
  const tones = all.filter((t) => t[1] !== "okHatch");
  assert.equal(tones.length, 4, "one tone per ramp slot");
  assert.equal(new Set(tones.map((t) => t[2])).size, tones.length, "no two tone names share a colour");
  for (const [, tone, colour] of tones) {
    assert.match(colour, /^var\(--chart-[1-4]\)$/, `tone ${tone} must take a ramp slot, not ${colour}`);
  }
});

test("no chart gives two of its own series the same slot", () => {
  // One name per slot now (the old `teal` drew the `warn` amber, which is how the Counts chart drew
  // two bars in one colour); the map still resolves names to slots in case one comes back.
  const slotOf = { info: 1, ok: 2, danger: 3, warn: 4 };
  for (const [file] of callers) {
    const source = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const group of source.matchAll(/=\s*\[\s*\n((?:\s*(?:\/\/[^\n]*\n|\.\.\.[^\n]*\n|\{ key:[^\n]*\n))+)\s*\]/g)) {
      const tones = [...group[1].matchAll(/tone: "(\w*)"/g)].map((m) => slotOf[m[1]] ?? m[1]);
      if (tones.length < 2) continue;
      assert.equal(new Set(tones).size, tones.length, `two series share a colour slot: ${tones.join(",")} in ${file}`);
    }
  }
});
