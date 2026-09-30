// guard: legacy-css-ceiling (SYNC, merge of origin/main into the template branch).
//
// Legacy CSS may only shrink. A merge from main is the easy way to grow it again: main still ships
// fixes as mesha-theme.css rules (a222fbdd4 added .toxin-proof, 3b259ea96 three phone rules,
// cbce4dbf7 a pinned proof photo). On this branch those fixes are carried in template MUI + sx and
// the rules are not taken. This test pins a line ceiling per legacy stylesheet; lower a ceiling when
// you delete rules, never raise it. It also names the selectors main added so they cannot slip back.
import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";

const CEILING = {
  "app/mesha-theme.css": 3166,
  "app/frame.css": 398,
  "app/minimal-theme.css": 620,
  "app/globals.css": 132,
};

const lines = (rel) => readFileSync(new URL(`../${rel}`, import.meta.url), "utf8").split("\n").length - 1;

test("legacy stylesheets never grow past their ceiling", () => {
  for (const [rel, max] of Object.entries(CEILING)) {
    if (!existsSync(new URL(`../${rel}`, import.meta.url))) continue;
    assert.ok(lines(rel) <= max, `${rel} has ${lines(rel)} lines, ceiling ${max}: move the style into sx / a template component`);
  }
});

test("rules main added to mesha-theme.css stay out (carried in sx instead)", () => {
  const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");
  for (const selector of [".toxin-proof", "feed-stock-table td:nth-child(3)", "mortality-recent-card td .mono", ".sales-card .hd .tag", ".vr-image-proof", ".vr-image-link"]) {
    assert.ok(!css.includes(selector), `${selector} is back in mesha-theme.css`);
  }
});
