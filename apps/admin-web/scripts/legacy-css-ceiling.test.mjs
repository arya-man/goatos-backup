// guard: legacy-css-ceiling (SYNC, merge of origin/main into the template branch).
//
// Legacy CSS may only shrink. A merge from main is the easy way to grow it again: main still ships
// fixes as mesha-theme.css rules (a222fbdd4 added .toxin-proof, 3b259ea96 three phone rules,
// cbce4dbf7 a pinned proof photo). On this branch those fixes are carried in template MUI + sx and
// the rules are not taken. This test pins a RULE ceiling per stylesheet; lower a ceiling when
// you delete rules, never raise it. It also names the selectors main added so they cannot slip back.
import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import { cssRuleFindings, stylesheetFiles } from "./lib/shrink-ratchets.mjs";

// FIXJ-CI (J1 P2-2): the ceiling counts style RULES, not lines (deleting comments or blank lines used
// to satisfy it), and covers every stylesheet: app/*.css, features/**/*.css, components/**/*.css,
// layouts/*.css and *.module.css. The per-file ceilings are the design:guard `legacy-css-rules`
// ratchet allowances (scripts/check-design-system-waivers/design-system-waivers.json), which only go
// down: lower them in the change that deletes rules (npm run design:guard:update-baseline).
const appRoot = new URL("../", import.meta.url).pathname;
const ratchet = JSON.parse(readFileSync(new URL("./check-design-system-waivers/design-system-waivers.json", import.meta.url), "utf8")).ratchet ?? {};

test("every stylesheet stays at or under its rule ceiling; a new stylesheet has none", () => {
  for (const rel of stylesheetFiles(appRoot)) {
    if (rel === "theme/fonts.css") continue;
    const rules = cssRuleFindings(readFileSync(new URL(`../${rel}`, import.meta.url), "utf8")).length;
    const allowed = ratchet[`legacy-css-rules|${rel}`]?.allowed ?? 0;
    assert.ok(rules <= allowed, `${rel} has ${rules} style rules, ceiling ${allowed}: move the style into sx / a template component`);
  }
});

test("the four legacy stylesheets keep an explicit rule ceiling", () => {
  for (const rel of ["app/mesha-theme.css", "app/frame.css", "app/minimal-theme.css", "app/globals.css"]) {
    if (!existsSync(new URL(`../${rel}`, import.meta.url))) continue;
    assert.ok(ratchet[`legacy-css-rules|${rel}`], `${rel} has no legacy-css-rules ceiling`);
  }
});

test("rules main added to mesha-theme.css stay out (carried in sx instead)", () => {
  const css = readFileSync(new URL("../app/mesha-theme.css", import.meta.url), "utf8");
  for (const selector of [".toxin-proof", "feed-stock-table td:nth-child(3)", "mortality-recent-card td .mono", ".sales-card .hd .tag", ".vr-image-proof", ".vr-image-link"]) {
    assert.ok(!css.includes(selector), `${selector} is back in mesha-theme.css`);
  }
});
