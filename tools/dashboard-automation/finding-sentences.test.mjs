// The sweep's failure sentences are written for people, so they no longer carry the check code.
// Slack's headline classifier reads those same sentences. This pins the two together: every bug
// family must still reach a named Slack headline, or a real break would arrive in the channel
// under the generic "something failed" card.
import test from "node:test";
import assert from "node:assert/strict";
import { staticIssueRules as issueRules } from "./lib/issue-rules.mjs";
import { REGRESSION_PATTERNS, findingSentence } from "../../apps/admin-web/scripts/lib/regression-checks.mjs";

const SAMPLE = { text: "Weekly growth", peer: "331 animals" };
// The J-raw-text family says three different things depending on what leaked.
const DETAILS = {
  "J-raw-text": ['ISO date "2026-09-07" in table (farm reads DD-MM-YYYY)', 'doubled label "Coimbatore · Coimbatore"', 'snake_case code "feed_item"'],
};

function classify(sentence) {
  const rule = issueRules().find(([re]) => re.test(sentence));
  return rule ? rule[1] : null;
}

test("every regression family's sentence still reaches a named Slack headline", () => {
  for (const pattern of Object.keys(REGRESSION_PATTERNS)) {
    for (const detail of DETAILS[pattern] ?? [""]) {
      const sentence = findingSentence({ pattern, detail, ...SAMPLE });
      const label = classify(sentence);
      assert.ok(label, `${pattern}: no Slack rule matches "${sentence}"`);
      assert.ok(!/[-_]/.test(label.replace(/ - /g, " ")) || /\//.test(label), `${pattern}: Slack label reads like a code: ${label}`);
    }
  }
});

test("the ISO-date and doubled-label leaks keep their own distinct headlines", () => {
  const iso = findingSentence({ pattern: "J-raw-text", detail: 'ISO date "2026-09-07" in table (farm reads DD-MM-YYYY)', ...SAMPLE });
  const doubled = findingSentence({ pattern: "J-raw-text", detail: 'doubled label "A · A"', ...SAMPLE });
  const code = findingSentence({ pattern: "J-raw-text", detail: 'snake_case code "feed_item"', ...SAMPLE });
  assert.match(classify(iso), /Date shown as/);
  assert.match(classify(doubled), /repeated twice/);
  assert.match(classify(code), /Internal code/);
});

test("the old coded sentences still classify, so historical receipts keep their headlines", () => {
  const legacy = [
    ['[A-svg-text-tiny] text "25/08/2026" renders at ~7.5px (<8px)', /too small to read/],
    ['[A-chart-label-column-narrow] span.wbl "x" label column 18px (<80px)', /Chart labels/],
    ['[text-overlap] span.wbl-text "Non-elevated pen" overlaps span.wgl-i "i"', /on top of other text/],
    ['[C-cell-mid-word-wrap] td "Warmup" "Warmup" split over 2 lines', /broken across two lines/],
  ];
  for (const [sentence, expected] of legacy) assert.match(classify(sentence) ?? "", expected, sentence);
});
