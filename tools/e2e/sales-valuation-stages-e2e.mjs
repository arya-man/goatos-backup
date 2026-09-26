// THE STAGES THE HERD IS VALUED IN ARE THE FARM'S (maintainer instruction 2026-09-24). This walks
// the thing that was asked for, in the browser, on the live page: a stage with animals and no price
// stands on the config screen asking to be added; adding it and typing two figures makes a card
// appear on Farm value and moves the farm's value by exactly those figures.
import { chromium } from "playwright";
import { execFileSync } from "node:child_process";

const BASE = "http://127.0.0.1:3423";
const PW = process.argv[2];
const DSN = `postgres://postgres@127.0.0.1:15432/goatos_feedqa?sslmode=disable`;
const sql = (q) => execFileSync("psql", [DSN, "-Atc", q], { env: { ...process.env, PGPASSWORD: PW, PGAPPNAME: "claude" }, encoding: "utf8" }).trim();

const results = [];
const check = (name, ok, detail = "") => {
  results.push({ name, ok });
  console.log(`${ok ? "  PASS" : "  FAIL"}  ${name}${detail ? `  -- ${detail}` : ""}`);
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
const page = await ctx.newPage();
const pageErrors = [];
page.on("pageerror", (e) => pageErrors.push(String(e).slice(0, 200)));
const val = () => page.locator('[data-testid="valuation-section"]');

async function goConfig() {
  await page.goto(`${BASE}/sales/config`, { waitUntil: "domcontentloaded" });
  await val().waitFor({ timeout: 60000 });
}
async function farmValue() {
  await page.goto(`${BASE}/sales/farm-value`, { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(2500);
  return page.content();
}

// The suite asserts from a KNOWN state and must therefore create it: any Warmup stage left by an
// earlier run is removed first, so a green run means the rules held rather than that the stage
// happened to be there already.
sql(`UPDATE sales_valuation_assumptions SET
       stages = (SELECT COALESCE(jsonb_agg(s), '[]'::jsonb) FROM jsonb_array_elements(stages) s WHERE s->>'stage' <> 'warmup'),
       buckets = (SELECT COALESCE(jsonb_agg(b), '[]'::jsonb) FROM jsonb_array_elements(buckets) b WHERE b->>'bucket' NOT LIKE 'warmup_%')
     WHERE stages @> '[{"stage":"warmup"}]'::jsonb`);
console.log("reset: any Warmup stage from an earlier run removed");

console.log("\n=== A. THE SCREEN SHOWS WHAT NOTHING PRICES ===");
await goConfig();
const unvalued = val().locator('[data-testid="valuation-unvalued"]');
check("a stage with animals and no price is named on the screen", await unvalued.isVisible());
check("and it is the one the herd actually has", (await unvalued.textContent())?.includes("Warmup") ?? false, (await unvalued.textContent())?.replace(/\s+/g, " ").trim().slice(0, 120));
check("with the animals standing in it", (await unvalued.textContent())?.includes("58") ?? false);

console.log("\n=== B. THE SIX STAGES THAT WERE HARD-CODED ARE NOW ROWS ===");
const stageRows = val().locator("tbody tr");
check("every stage is a row the farm can edit", (await stageRows.count()) === 6, `${await stageRows.count()} rows`);
check("each says which herd stages it covers", (await val().locator('tr[data-stage="adult"]').textContent())?.includes("Non-Pregnant") ?? false);
check("and the clinical rules are rows too, not code", (await val().locator('tr[data-stage="K2"]').textContent())?.includes("ICU-Kid") ?? false);

console.log("\n=== C. ADDING THE STAGE, AND WHAT IT DOES TO THE FARM'S VALUE ===");
const beforeHtml = await farmValue();
// Warmup IS on Farm value before -- in the not-valued list, which is the honest state: 58 animals
// nothing prices, named, rather than folded into a bucket nobody chose for them.
check("Farm value names the unpriced animals rather than hiding them", beforeHtml.includes("Warmup"));
check("but carries no card for them, because nothing values them yet", !/warmup_(female|male)/.test(beforeHtml));

await goConfig();
await val().locator('[data-testid="valuation-add-register-Warmup"]').click();
await page.waitForTimeout(300);
check("the stage lands as a row of its own", await val().locator('tr[data-stage="warmup"]').isVisible());
check("already covering the herd stage it was added from", (await val().locator('tr[data-stage="warmup"]').textContent())?.includes("Warmup") ?? false);
// 58 kids at 12 kg and 520 rupees a kg, the same on both rows: 58 x 12 x 520 = 361,920.
for (const g of ["female", "male"]) {
  await val().locator(`input[name="weight_warmup_${g}"]`).fill("12");
  await val().locator(`[data-testid="valuation-price-warmup_${g}"]`).fill("520");
}
await val().locator('[data-testid="valuation-save"]').click();
await page.waitForTimeout(3500);
check("the save lands", (await val().textContent())?.includes("Valuation saved") ?? false);
const stored = sql(`SELECT jsonb_array_length(stages) FROM sales_valuation_assumptions`);
check("and the stage is stored as data, not code", stored === "7", `${stored} stages stored`);

console.log("\n=== D. THE CARD COMES, AND THE NUMBER MOVES BY EXACTLY THAT ===");
const afterHtml = await farmValue();
check("a Warmup card appears on Farm value", afterHtml.includes("Warmup"));
const api = JSON.parse(execFileSync("curl", ["-s", "-H", `Authorization: Bearer ${process.argv[3]}`, "http://127.0.0.1:8123/sales/overview?farm=all"], { encoding: "utf8" }));
const fv = api.farm_valuation;
const warmup = fv.buckets.filter((b) => b.bucket.startsWith("warmup_"));
check("both genders of it are valued", warmup.length === 2);
check("every animal that was not valued now is", fv.excluded_animals === 0 && fv.valued_animals === 1569, `valued ${fv.valued_animals}, not valued ${fv.excluded_animals}`);
const added = warmup.reduce((t, b) => t + b.animal_count * b.weight_kg * b.price_per_kg, 0);
check("the farm's value rose by exactly the figures typed", Math.abs(fv.total_value_rupees - (27480543.57876077 + 361920)) < 1, `now ${Math.round(fv.total_value_rupees)}, added ${Math.round(added)}`);
check("counted over the 58 animals that stood in it", warmup.reduce((t, b) => t + b.animal_count, 0) === 58);

console.log("\n=== E. AND THE RULES HOLD ===");
await goConfig();
// Covers is the template Autocomplete (chips): open its listbox and read what it offers.
const adultAdd = val().locator('[data-testid="valuation-covers-add-adult"]');
await adultAdd.click();
const offered = await page.locator('[role="listbox"] [role="option"]').allTextContents();
await page.keyboard.press("Escape");
check("a herd stage already valued is not offered twice", !offered.some((o) => o.includes("Non-Pregnant")), offered.filter((o) => o).slice(0, 4).join(" | "));
check("and one nothing values still is", offered.some((o) => o.includes("Flushing")));
// Removing a stage un-values its animals -- visibly, on the not-valued list, never silently.
await val().locator('[data-testid="valuation-stage-remove-warmup"]').click();
await val().locator('[data-testid="valuation-save"]').click();
await page.waitForTimeout(3500);
const back = JSON.parse(execFileSync("curl", ["-s", "-H", `Authorization: Bearer ${process.argv[3]}`, "http://127.0.0.1:8123/sales/overview?farm=all"], { encoding: "utf8" })).farm_valuation;
check("removing a stage puts its animals back on the not-valued list", back.excluded_animals === 58 && back.not_valued[0].label === "Warmup");
check("and takes its money back out", Math.abs(back.total_value_rupees - 27480543.57876077) < 1, String(Math.round(back.total_value_rupees)));

console.log(`\npage errors: ${pageErrors.length ? pageErrors.join(" | ") : "none"}`);
const passed = results.filter((r) => r.ok).length;
console.log(`\nTOTAL: ${passed}/${results.length} passed`);
await browser.close();
