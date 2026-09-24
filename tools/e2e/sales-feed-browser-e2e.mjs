import { chromium } from "playwright";
import { execFileSync } from "node:child_process";

const BASE = "http://127.0.0.1:3423";
const OUT = process.argv[2];
const PW = process.argv[3];
const DSN = `postgres://postgres@127.0.0.1:15432/goatos_feedqa?sslmode=disable`;

function sql(q) {
  return execFileSync("psql", [DSN, "-Atc", q], { env: { ...process.env, PGPASSWORD: PW, PGAPPNAME: "claude" }, encoding: "utf8" }).trim();
}
function num(q) { const v = sql(q); return v === "" ? 0 : Number(v); }

const results = [];
function check(name, ok, detail = "") {
  results.push({ name, ok });
  console.log(`${ok ? "  PASS" : "  FAIL"}  ${name}${detail ? `  -- ${detail}` : ""}`);
}

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 1100 }, deviceScaleFactor: 1 });
const page = await ctx.newPage();
const pageErrors = [];
page.on("pageerror", (e) => pageErrors.push(String(e).slice(0, 160)));

async function goConfig() {
  await page.goto(`${BASE}/sales/config`, { waitUntil: "domcontentloaded" });
  await page.waitForSelector(".sellable-product-row", { timeout: 60000 });
}
const section = () => page.locator('section[aria-label="Items the farm sells"]');
const rowFor = (code) => section().locator(`[data-testid="sellable-product-row"][data-code="${code}"]`);
const addRow = () => section().locator('[data-testid="sellable-product-row"]').last();

async function saveRow(row) {
  await row.getByRole("button", { name: /^save item$/i }).click();
  await page.waitForTimeout(2000);
}
async function addItem({ name, kind, unit }) {
  const r = addRow();
  await r.locator('input[name="name"]').fill(name);
  await r.locator('select[name="kind"]').selectOption(kind);
  await r.locator('select[name="unit"]').selectOption(unit);
  await saveRow(r);
}

/** Opens the record-sale drawer on the CURRENT page instance (no reload). */
async function openDrawer() {
  await page.getByRole("link", { name: /record sale/i }).first().click();
  await page.waitForSelector('[data-testid="sale-line"]', { timeout: 30000 });
}
async function dropdown() {
  return page.evaluate(() => [...document.querySelectorAll('select[name="line_product_type_0"] option')].map((o) => o.value));
}
async function closeDrawer() {
  await page.keyboard.press("Escape");
  await page.waitForTimeout(400);
}

async function recordSale({ farm, lines, buyer, ack = false, status }) {
  // A confirmation is answered in the drawer the refusal REOPENED -- navigating afresh would drop
  // the very tick being tested, which is exactly what a person would not do.
  if (ack) {
    await page.waitForSelector('input[name="stock_shortfall_acknowledged"]', { timeout: 30000 });
  } else {
    await goConfig();
    await openDrawer();
  }
  if (!ack) {
  await page.locator("summary.move-date-button").first().click();
  await page.locator("button.move-date-day.today").first().click();
  await page.selectOption('select[name="farm"]', farm);
  for (const [i, l] of lines.entries()) {
    if (i > 0) {
      await page.getByRole("button", { name: /add another product/i }).click();
      await page.waitForTimeout(200);
    }
    await page.selectOption(`select[name="line_product_type_${i}"]`, l.product);
    await page.waitForTimeout(250);
    if (await page.locator(`select[name="line_breed_${i}"]`).count()) {
      await page.selectOption(`select[name="line_breed_${i}"]`, l.variant);
    }
    if (l.quantity !== undefined) {
      await page.fill(`input[name="line_quantity_${i}"]`, String(l.quantity));
      if (l.rate !== undefined) await page.fill(`input[name="line_rate_per_unit_${i}"]`, String(l.rate));
    } else {
      if (l.animals !== undefined) await page.fill(`input[name="line_animal_count_${i}"]`, String(l.animals));
      if (l.value !== undefined) await page.fill(`input[name="line_sales_value_${i}"]`, String(l.value));
    }
  }
  const vend = await page.evaluate(() => document.querySelector('select[name="buyer_vendor_id"]').options[1].value);
  await page.selectOption('select[name="buyer_vendor_id"]', vend);
  await page.fill('input[name="buyer_name"]', buyer);
  if (status) await page.selectOption('select[name="status"]', status);
  }
  if (ack) await page.check('input[name="stock_shortfall_acknowledged"]');
  await page.evaluate(() => {
    const url = new URL(location.href);
    ["action_status", "action_key", "action_detail"].forEach((k) => url.searchParams.delete(k));
    history.replaceState(null, "", url.toString());
  });
  const form = page.locator("form").filter({ has: page.locator('[name="line_product_type_0"]') });
  await form.getByRole("button", { name: /^save$/i }).click();
  await page.waitForFunction(() => new URL(location.href).searchParams.has("action_status") ||
    (document.querySelector('form [role="alert"]') && !document.querySelector('form button[type="submit"]:disabled')), null, { timeout: 30000 });
  const refusal = page.locator('form [role="alert"]');
  if (await refusal.count()) return {
    status: "error",
    key: await page.locator('[name="stock_shortfall_acknowledged"]').count() ? "action.sale_feed_stock_confirm" : "action.sale_record_failed",
    detail: await refusal.first().innerText(),
  };
  const u = new URL(page.url());
  return { status: u.searchParams.get("action_status"), key: u.searchParams.get("action_key"), detail: u.searchParams.get("action_detail") };
}

// The store's balance, computed here INDEPENDENTLY of the code under test: what reached the farm,
// less what was fed, less what was sold. A test that asked the API for the number it is checking
// would pass whatever the API said.
//
// projection-review: membership=public.feed_purchases for one (farm, feed), one row per load;
// group_key=(farm_label, feed_item_key) in every branch, the same pair the three sides are joined
// and compared on; join_cardinality=each of f, d and s is GROUPED to one row per key BEFORE it is
// joined, so the loads side can never be fanned out by a feed day or a sale line -- b LEFT JOIN d
// LEFT JOIN s is 1:0..1 twice over; pagination=none, a whole-store figure; scope=one tenant, one
// farm label and one feed key, applied inside each branch rather than after the join.
const balance = (farm, feedKey) => num(`
WITH b AS (SELECT farm_label,feed_item_key,SUM(quantity_kg-consumed_at_import_kg) net,MIN(depletes_from) df,MIN(park_id::text) pk
           FROM feed_purchases WHERE tenant_id='00000000-0000-4000-8000-000000000001' AND delivery_status='reached' AND feed_item_key='${feedKey}' AND farm_label='${farm}' GROUP BY 1,2),
f AS (SELECT i.park_id,r.feed_item_key,i.feed_day,SUM(r.quantity_kg) kg FROM feed_direction_issues i
      JOIN feed_direction_issue_rows r ON r.tenant_id=i.tenant_id AND r.feed_direction_issue_id=i.feed_direction_issue_id
      WHERE i.tenant_id='00000000-0000-4000-8000-000000000001' AND i.state='locked' AND r.quantity_kg IS NOT NULL GROUP BY 1,2,3),
d AS (SELECT b.farm_label,b.feed_item_key,COALESCE(SUM(f.kg),0) kg FROM b LEFT JOIN f ON f.park_id=b.pk::uuid AND f.feed_item_key=b.feed_item_key AND f.feed_day>=b.df GROUP BY 1,2),
s AS (SELECT farm_label,feed_item_key,SUM(quantity_kg) kg FROM feed_sale_depletions WHERE tenant_id='00000000-0000-4000-8000-000000000001' GROUP BY 1,2)
SELECT round(b.net-d.kg-COALESCE(s.kg,0),1) FROM b JOIN d USING(farm_label,feed_item_key) LEFT JOIN s USING(farm_label,feed_item_key)`);

// A suite that leaves its own rows behind reads its LAST run, not this one: the second pass
// found the item it had added, refused it as a duplicate, and every feed sale after it tripped
// the short-stock confirmation on a store its own earlier tests had drained. So it resets what it
// made -- and nothing else.
// Every sale this branch's testing has ever recorded, by hand or by suite. The store they drained
// is why a "covered" sale was tripping the short-stock confirmation on the second run.
const TEST_BUYERS = `buyer_name LIKE 'E2E %' OR buyer_name IN ('Ramesh Traders (feed)','Over Ask Traders','Mixed Basket Buyer','Tag Buyer')`;
sql(`DELETE FROM feed_sale_depletions WHERE deal_id IN (SELECT id FROM sales_deals WHERE ${TEST_BUYERS})`);
sql(`DELETE FROM sales_deals WHERE ${TEST_BUYERS}`);
sql(`DELETE FROM sellable_product_catalog WHERE product_code IN ('mineral_mix','straw_bales','ear_tags_goat')`);
console.log("reset: prior E2E sales and items cleared\n");

console.log("=== A. CONFIG CHANGES, AND WHETHER THEY REFLECT LIVE ===");
await goConfig();
const before = await section().locator('[data-testid="sellable-product-row"]').count();
await addItem({ name: "Mineral mix", kind: "other", unit: "kg" });
check("adding an item adds a row", (await section().locator('[data-testid="sellable-product-row"]').count()) === before + 1);
await openDrawer();
check("a new item is in the sale dropdown WITHOUT a reload", (await dropdown()).includes("Mineral mix"));
await closeDrawer();

// Rename it, on the same page, and watch the dropdown follow.
await rowFor("mineral_mix").locator('input[name="name"]').fill("Mineral mix (fine)");
await saveRow(rowFor("mineral_mix"));
await openDrawer();
const afterRename = await dropdown();
check("a rename shows in the dropdown live", afterRename.includes("Mineral mix (fine)") && !afterRename.includes("Mineral mix,"));
await closeDrawer();

// Change how it is sold: kg -> number. The sale form must ask a different question.
await rowFor("mineral_mix").locator('select[name="unit"]').selectOption("number");
await saveRow(rowFor("mineral_mix"));
await openDrawer();
await page.selectOption('select[name="line_product_type_0"]', "Mineral mix (fine)");
await page.waitForTimeout(300);
const labels = await page.evaluate(() => [...document.querySelectorAll('[data-testid="sale-line"] label')].map((l) => l.textContent.trim()));
check("changing kg -> number changes what the sale form asks", labels.some((l) => /how many/i.test(l)) && labels.some((l) => /each/i.test(l)), labels.join(" | "));
await closeDrawer();

// Switch it off; it must leave the dropdown but stay on the list.
await rowFor("mineral_mix").locator('input[name="in_use"]').uncheck();
await saveRow(rowFor("mineral_mix"));
await openDrawer();
check("an item switched off leaves the dropdown", !(await dropdown()).includes("Mineral mix (fine)"));
await closeDrawer();
check("a switched-off item is still on the list", (await rowFor("mineral_mix").count()) === 1);

// Back on again.
await rowFor("mineral_mix").locator('input[name="in_use"]').check();
await saveRow(rowFor("mineral_mix"));
await openDrawer();
check("switching it back on returns it to the dropdown", (await dropdown()).includes("Mineral mix (fine)"));
await closeDrawer();

// Two items may not share a name.
const dupRow = addRow();
await dupRow.locator('input[name="name"]').fill("Feed");
await dupRow.locator('select[name="kind"]').selectOption("other");
await saveRow(dupRow);
check("two items cannot share one name", (await section().innerText()).includes("already has that name"));


console.log("\n=== B. SELLING: STOCK, MONEY, AND THE RULES ===");
const KEY = "mesha_kids_concentrate";

// B1 -- one feed line the store can cover.
let b0 = balance("CPT", KEY);
let r = await recordSale({ farm: "CPT", buyer: "E2E covered", lines: [{ product: "Feed", variant: "Mesha Kids Concentrate", quantity: 100, rate: 40 }] });
check("a covered feed sale records", r.status === "success", r.key);
check("the store falls by exactly what was sold", balance("CPT", KEY) === Math.round((b0 - 100) * 10) / 10, `${b0} -> ${balance("CPT", KEY)}`);
check("the money is quantity x rate", num(`SELECT sales_value FROM sales_deals WHERE buyer_name='E2E covered'`) === 4000);

// B2 -- the SAME feed twice in one sale draws twice.
b0 = balance("CPT", KEY);
r = await recordSale({ farm: "CPT", buyer: "E2E twice", lines: [
  { product: "Feed", variant: "Mesha Kids Concentrate", quantity: 50, rate: 40 },
  { product: "Feed", variant: "Mesha Kids Concentrate", quantity: 30, rate: 41 },
]});
check("two lines of one feed both record", r.status === "success", r.key);
check("and the store falls by both, not one", balance("CPT", KEY) === Math.round((b0 - 80) * 10) / 10, `${b0} -> ${balance("CPT", KEY)}`);

// B3 -- two DIFFERENT feeds in one sale.
const b0k = balance("CPT", KEY), b0d = balance("CPT", "dry_masoor_bhusa");
r = await recordSale({ farm: "CPT", buyer: "E2E two feeds", lines: [
  { product: "Feed", variant: "Mesha Kids Concentrate", quantity: 20, rate: 40 },
  { product: "Feed", variant: "Dry Masoor Bhusa", quantity: 25, rate: 18 },
]});
check("two different feeds in one sale record", r.status === "success", r.key);
check("each feed falls by its own quantity", balance("CPT", KEY) === Math.round((b0k - 20) * 10) / 10 && balance("CPT", "dry_masoor_bhusa") === Math.round((b0d - 25) * 10) / 10);

// B4 -- a mixed sale: animals + feed + a counted item.
const b0mix = balance("CBE", "dry_masoor_bhusa");
r = await recordSale({ farm: "CBE", buyer: "E2E mixed", lines: [
  { product: "Goat", variant: "Sojat", animals: 9, value: 72000 },
  { product: "Feed", variant: "Dry Masoor Bhusa", quantity: 40, rate: 18 },
  { product: "Sheep tags", variant: "Sheep tags", quantity: 50, rate: 12 },
]});
check("a mixed animal + feed + counted sale records", r.status === "success", r.key);
check("it rolls up to Mixed", sql(`SELECT product_type FROM sales_deals WHERE buyer_name='E2E mixed'`) === "Mixed");
check("its animals count only the animal line", num(`SELECT animal_count FROM sales_deals WHERE buyer_name='E2E mixed'`) === 9);
check("its money adds all three lines", num(`SELECT sales_value FROM sales_deals WHERE buyer_name='E2E mixed'`) === 72000 + 720 + 600);
check("only the FEED line touched the store", balance("CBE", "dry_masoor_bhusa") === Math.round((b0mix - 40) * 10) / 10);
check("the counted item drew on no store at all", num(`SELECT count(*) FROM feed_sale_depletions s JOIN sales_deals d ON d.id=s.deal_id WHERE d.buyer_name='E2E mixed'`) === 1);

// B5 -- short stock: refused once, then recorded on the tick.
const bShort = balance("CPT", KEY);
r = await recordSale({ farm: "CPT", buyer: "E2E short", lines: [{ product: "Feed", variant: "Mesha Kids Concentrate", quantity: bShort + 500, rate: 40 }] });
check("selling more than the store holds is refused", r.key === "action.sale_feed_stock_confirm", r.key);
check("short-stock refusal preserves the entered sale",
  await page.inputValue('[name="line_quantity_0"]') === String(bShort + 500) &&
  await page.inputValue('[name="line_rate_per_unit_0"]') === "40" &&
  await page.inputValue('[name="buyer_name"]') === "E2E short" &&
  await page.inputValue('[name="farm"]') === "CPT");
// The figure is the STOCK TAB's own, which folds the transitional split concentrates into one
// family; this test's raw SQL does not, so the two legitimately differ. What matters is that the
// desk is told the feed, the farm and both quantities.
check("the refusal names the feed, the farm and both figures",
  /CPT/.test(r.detail || "") && /Mesha Kids Concentrate/.test(r.detail || "") &&
  /in the store/.test(r.detail || "") && /this sale takes/.test(r.detail || ""), r.detail);
check("nothing was recorded while it was refused", num(`SELECT count(*) FROM sales_deals WHERE buyer_name='E2E short'`) === 0);
r = await recordSale({ farm: "CPT", buyer: "E2E short", ack: true, lines: [{ product: "Feed", variant: "Mesha Kids Concentrate", quantity: bShort + 500, rate: 40 }] });
check("the same sale records once confirmed", r.status === "success", r.key);
check("and the store goes negative honestly", balance("CPT", KEY) === -500);

// B6 -- an expected sale moves no feed until it closes.
const bStatus = balance("CBE", KEY);
r = await recordSale({ farm: "CBE", buyer: "E2E pending", status: "In Discussion", lines: [{ product: "Feed", variant: "Mesha Kids Concentrate", quantity: 60, rate: 40 }] });
check("a sale still in discussion records", r.status === "success", r.key);
check("but moves no feed", balance("CBE", KEY) === bStatus, `${bStatus} -> ${balance("CBE", KEY)}`);
const dealID = sql(`SELECT id FROM sales_deals WHERE buyer_name='E2E pending'`);
await page.goto(`${BASE}/sales/config?deal_id=${dealID}`, { waitUntil: "domcontentloaded" });

// The deal drawer's own status form (#sds-status), not the record form's status field.
await page.waitForSelector("#sds-status", { timeout: 30000 });
const statusForm = page.locator("form").filter({ has: page.locator("#sds-status") });
await statusForm.locator("#sds-status").selectOption("Deal Closed");
await statusForm.locator('button[type="submit"]').first().click();
await page.waitForTimeout(3000);
check("closing it takes the feed off the store", balance("CBE", KEY) === Math.round((bStatus - 60) * 10) / 10, `${bStatus} -> ${balance("CBE", KEY)}`);

// B7 -- the rules that refuse a bad line.
r = await recordSale({ farm: "CPT", buyer: "E2E noqty", lines: [{ product: "Feed", variant: "Mesha Kids Concentrate", quantity: 0, rate: 40 }] });
check("a feed line of zero is refused, naming the field", r.status === "error" && /quantity/i.test(r.detail || ""), `${r.key} :: ${r.detail}`);

// A missing rate is stopped by the FORM before it can be sent -- the field is required, so the
// browser refuses the submit and the sale never leaves the screen. That is the honest check here:
// the server rule is pinned by its own Go test.
await goConfig();
await openDrawer();
await page.selectOption('select[name="line_product_type_0"]', "Feed");
await page.waitForTimeout(250);
await page.selectOption('select[name="line_breed_0"]', "Mesha Kids Concentrate");
await page.fill('input[name="line_quantity_0"]', "10");
const rateValid = await page.evaluate(() => document.querySelector('input[name="line_rate_per_unit_0"]').checkValidity());
check("a feed line with no rate cannot even be submitted", rateValid === false, `rate field reports valid=${rateValid}`);
await closeDrawer();

// B8 -- an animal lot still records as a lot.
r = await recordSale({ farm: "CBE", buyer: "E2E animals", lines: [{ product: "Goat", variant: "Malai", animals: 5, value: 41000 }] });
check("an animal lot records at its negotiated price", r.status === "success" && num(`SELECT sales_value FROM sales_deals WHERE buyer_name='E2E animals'`) === 41000, r.key);
check("and takes nothing off any store", num(`SELECT count(*) FROM feed_sale_depletions s JOIN sales_deals d ON d.id=s.deal_id WHERE d.buyer_name='E2E animals'`) === 0);

await browser.close();
console.log("\npage errors:", pageErrors.length ? pageErrors.join(" | ") : "none");
const failed = results.filter((r) => !r.ok);
console.log(`\nTOTAL: ${results.length - failed.length}/${results.length} passed`);
process.exit(failed.length ? 1 : 0);
