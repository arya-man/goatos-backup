import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const section = readFileSync(new URL("./market-config-section.tsx", import.meta.url), "utf8");
const form = readFileSync(new URL("./market-config-form.tsx", import.meta.url), "utf8");
const actions = readFileSync(new URL("./market-actions.ts", import.meta.url), "utf8");
const valuation = readFileSync(new URL("./valuation-section.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

// Maintainer report 2026-09-15: "when I add any city or make any change the whole page is
// loading and I am going to top". The market forms used to redirect back to the page with
// ?notice=, which is a full navigation. Every write now lands in place.
test("market config actions return their outcome and never redirect", () => {
  assert.doesNotMatch(actions, /actionRedirect\(|redirect\(/);
  assert.doesNotMatch(actions, /return_to/);
  for (const name of [
    "addMarketCityAction",
    "updateMarketCityAction",
    "addMarketQuestionAction",
    "updateMarketQuestionAction",
    "setMarketCallTimeAction",
  ]) {
    assert.match(
      actions,
      new RegExp(`export async function ${name}\\(previous: MarketActionState, formData: FormData\\): Promise<MarketActionState>`),
      `${name} must take the previous outcome and return the next one`,
    );
  }
  // A successful write re-reads the section's data inside the same response.
  assert.match(actions, /revalidatePath\(SALES_CONFIG_PATH\)/);
});

test("every market form posts through the in-place client wrapper", () => {
  assert.doesNotMatch(section, /<form\b/, "no raw <form action=...> left in the section");
  assert.doesNotMatch(section, /return_to/);
  assert.equal((section.match(/<MarketConfigForm\b/g) || []).length, 5, "call time, add city, city row, add question, question row");
  assert.match(form, /useActionState\(action, INITIAL\)/);
  assert.doesNotMatch(form, /useRouter|router\.push|redirect\(/);
});

test("the wrapper composes no copy of its own: outcomes come resolved from the page contract", () => {
  assert.match(section, /copy\(pageContract, "action\.market_city_saved"\)/);
  assert.match(section, /copy\(pageContract, "action\.market_duplicate"\)/);
  assert.match(form, /outcomes\[state\.code\]/);
});

test("sales valuation table has a mobile horizontal scroll owner", () => {
  // Template Scrollbar owns the sideways scroll; the table keeps a min width so no cell clips
  // (FJ3 P1-3: stage names cut to "Fattenir" at 1440 and "Milk trainin" at 390).
  assert.match(valuation, /<Scrollbar>\s*<Table size="small" sx=\{\{ minWidth: 960/);
  assert.doesNotMatch(valuation, /<select\b|<input(?![^>]*type="hidden")|className="chip|<details\b/, "valuation uses MUI fields, Autocomplete chips, no native controls");
});

// guard: sales-config-no-legacy-css (R3SP 2026-09-27). The template rebuild of /sales/config kept
// a few class hooks, and legacy stylesheet rules on them repainted MUI parts: `.sellable-product-row
// input{box-sizing:border-box}` collapsed the MUI "Item name" input to ~24px and
// `.market-config-line input` drew a second border inside every TextField. A class a converted
// Sales Config file still renders must not be styled by a legacy stylesheet.
test("sales config classes are not styled by the legacy stylesheets", () => {
  const files = ["./sales-config.tsx", "./valuation-section.tsx", "./market-config-section.tsx", "./market-reporters-section.tsx", "./sellable-products-section.tsx", "./market-config-form.tsx"];
  const classes = new Set();
  for (const file of files) {
    const src = readFileSync(new URL(file, import.meta.url), "utf8");
    for (const m of src.matchAll(/className="([^"]+)"/g)) for (const c of m[1].split(/\s+/)) if (c) classes.add(c);
  }
  // Shared hooks owned elsewhere (page root, cell links, table-scoped nowrap) are allowed.
  for (const shared of ["celllink", "sales-deals-table", "screen", "on"]) classes.delete(shared);
  const sheets = ["../../app/mesha-theme.css", "../../app/frame.css", "../../app/minimal-theme.css"].map((p) => readFileSync(new URL(p, import.meta.url), "utf8"));
  const hits = [];
  for (const cls of classes) {
    const re = new RegExp(`\\.${cls.replace(/[-]/g, "\\-")}(?![\\w-])`);
    if (sheets.some((css) => re.test(css))) hits.push(cls);
  }
  assert.deepEqual(hits, [], `legacy CSS still styles: ${hits.join(", ")}`);
});

// guard: sales-config-items-panel: the What-we-sell card sits outside the tab's keyed panel (it
// shares its list with the record-sale drawer), so it carries its own URL panel on `tab` — or a
// tab click leaves the stale items card on screen until the server answers.
test("the items card is wrapped in a URL panel keyed on the tab", () => {
  const items = readFileSync(new URL("./sales-config-items.tsx", import.meta.url), "utf8");
  assert.match(items, /<UrlPanel watch=\{\["tab"\]\} fallback=\{null\}>\s*\{showProducts \?/);
});
