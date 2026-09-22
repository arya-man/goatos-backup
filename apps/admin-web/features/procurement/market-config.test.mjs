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
  assert.match(valuation, /className="tablewrap sales-valuation-tablewrap"/);
  assert.match(css, /\.sales-valuation-tablewrap\{[^}]*overflow-x:auto/);
  assert.match(css, /\.sales-valuation-tablewrap\{[^}]*-webkit-overflow-scrolling:touch/);
});
