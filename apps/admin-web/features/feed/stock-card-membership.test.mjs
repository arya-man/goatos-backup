import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./feed-analytics.tsx", import.meta.url), "utf8");
const stockCards = source.slice(source.indexOf("function StockCards"), source.indexOf("function StockCards") + 4000);

// The card set is the BACKEND's decision (maintainer decision 2026-09-06): it
// serves the farm's active feed vocabulary, and every served row is a card.
// Re-adding a client-side filter is the regression -- it is how a bought but
// unfed load became invisible, and it is invisible in review because the page
// still renders correctly for every feed that happens to be in use.
test("the stock card grid renders every served row and filters none out", () => {
  assert.match(stockCards, /const active = stock\?\.items \?\? \[\];/);
  assert.doesNotMatch(stockCards, /\.filter\(\s*\(item\)\s*=>\s*item\.days_left/);
  assert.doesNotMatch(stockCards, /days_left !== null/);
});

// A not-started card must lead with the kg actually in the store; a days-left
// figure it does not have must never be invented, and it must not read as low
// stock -- a full untouched load is the opposite of nearly out.
test("a not-started card shows kg in store, never a days-left or a low-stock tag", () => {
  assert.match(stockCards, /item\.not_started\s*\?\s*`\$\{nf\(num\(item\.balance_kg\)\)\} \$\{fa\(pageContract, "unit\.kg"\)\}`/);
  assert.match(stockCards, /item\.not_started \? <span className="tag t-ok">\{fa\(pageContract, "stock\.not_started"\)\}<\/span> : null/);
  assert.match(stockCards, /item\.low_stock \? <span className="tag t-dng">\{fa\(pageContract, "stock\.low"\)\}<\/span> : null/);
});

// Backend owns the words (copy firewall): the card's copy comes from the page
// contract, never a literal typed into the component.
test("not-started copy comes from the page contract, not a frontend literal", () => {
  assert.match(stockCards, /fa\(pageContract, "stock\.not_started"\)/);
  assert.match(stockCards, /fa\(pageContract, "stock\.not_started_sub"\)/);
  assert.doesNotMatch(stockCards, /"Feeding not started"/);
  assert.doesNotMatch(stockCards, /"In store, none given yet"/);
});
