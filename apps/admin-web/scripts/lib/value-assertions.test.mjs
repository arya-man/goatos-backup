import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import {
  checkValueExpect,
  formatNumber,
  isValueExpect,
  numberFrom,
  parseNumbers,
  resetQueryCache,
  saysSoundsHuman,
  validateValueExpect,
} from "./value-assertions.mjs";
import { loadFeatureAssertions } from "./feature-assertions.mjs";

// ---------------------------------------------------------------------------------------------
// A stand-in for the page.
//
// It answers `locator(css).count()` / `.nth(i).innerText()` the way Playwright does, from a
// fixture that says which selectors each element answers to and what it renders. That makes the
// mutation proof exact: the ONLY thing that changes between the green run and the red run is
// the text on the screen.
// ---------------------------------------------------------------------------------------------
function fakePage(elements) {
  const match = (css) => elements.filter((el) => el.css.includes(css));
  return {
    locator(css) {
      const found = match(css);
      const wrap = (el) => ({
        innerText: async () => el.text,
        getAttribute: async (name) => el.attrs?.[name] ?? null,
        isVisible: async () => true,
        waitFor: async () => {},
        evaluate: async () => {},
      });
      return {
        count: async () => found.length,
        nth: (i) => wrap(found[i]),
        first: () => wrap(found[0]),
        ...wrap(found[0] ?? { text: "" }),
      };
    },
    url: () => "https://dashboard.mesha.sg/feed/analytics",
  };
}

// ---------------------------------------------------------------------------------------------
// Reading a number off the screen
// ---------------------------------------------------------------------------------------------

test("the farm's own number formats are read correctly", () => {
  assert.deepEqual(parseNumbers("₹1,23,456"), [123456]); // Indian grouping
  assert.deepEqual(parseNumbers("₹4,321 / day · 52.3%"), [4321, 52.3]);
  assert.deepEqual(parseNumbers("312.5 kg"), [312.5]);
  assert.deepEqual(parseNumbers("—"), []); // a placeholder dash is not a zero
  assert.deepEqual(parseNumbers("No feed recorded"), []);
});

test("a text holding two numbers is an error, not a guess", () => {
  // Quietly taking the first number is how a check ends up comparing the percentage instead of
  // the rupees and reporting green for years.
  assert.match(numberFrom("₹4,321 / day · 52.3%", "only", "spend").error, /holds 2 numbers/);
  assert.equal(numberFrom("₹4,321 / day · 52.3%", "first", "spend").number, 4321);
  assert.equal(numberFrom("₹4,321 / day · 52.3%", "last", "spend").number, 52.3);
});

test("formatting a number for a person never prints floating-point noise", () => {
  assert.equal(formatNumber(0.1 + 0.2), "0.3");
  assert.equal(formatNumber(40), "40");
});

// ---------------------------------------------------------------------------------------------
// FEED SPEND SHARE — the judge's worked example, converted.
//
// 6fbc825f2 "Spend share only shows the feeds the farm buys" asserted `visible: "Feed spend
// share"` — the chart's HEADING. Put every feed back in the pie and the heading is still there,
// so the check passed on the broken product.
//
// The expected value here is a RECONCILIATION, not a fixture: the pie's legend and the per-feed
// cards under it render the same figure from the same money, by two different code paths. The
// pie is the path that once carried its own feed allowlist. Nothing to keep up to date, and it
// cannot go stale.
// ---------------------------------------------------------------------------------------------

const PIE_SLICE_VALUE = 'section.wchart[aria-label="Feed spend share"] span.muted';
const CARD_SPEND = '.chartcard .feed-item-strip > div:first-child .val:has-text("₹")';

const spendShareReconciliation = {
  reconcile: {
    left: { countOf: PIE_SLICE_VALUE, says: "the number of feeds in the spend share pie" },
    right: { countOf: CARD_SPEND, says: "the number of feeds showing a daily spend on their own card" },
    relation: "equals",
  },
};

const spendShareTotals = {
  reconcile: {
    left: { sumOf: PIE_SLICE_VALUE, pick: "first", says: "the daily spend added up across the spend share pie" },
    right: { sumOf: CARD_SPEND, says: "the daily spend added up across the per-feed cards" },
    relation: "equals",
    tolerance: 1,
  },
};

// The farm buys four feeds. The pie and the cards agree.
function healthyFeedPage() {
  return fakePage([
    { css: [PIE_SLICE_VALUE], text: "₹4,321 / day · 40.2%" },
    { css: [PIE_SLICE_VALUE], text: "₹3,180 / day · 29.6%" },
    { css: [PIE_SLICE_VALUE], text: "₹2,100 / day · 19.5%" },
    { css: [PIE_SLICE_VALUE], text: "₹1,145 / day · 10.7%" },
    { css: [CARD_SPEND], text: "₹4,321" },
    { css: [CARD_SPEND], text: "₹3,180" },
    { css: [CARD_SPEND], text: "₹2,100" },
    { css: [CARD_SPEND], text: "₹1,145" },
  ]);
}

test("the spend share check is green when the pie and the cards show the same feeds", async () => {
  assert.equal(await checkValueExpect(healthyFeedPage(), spendShareReconciliation), null);
  assert.equal(await checkValueExpect(healthyFeedPage(), spendShareTotals), null);
});

test("MUTATION: restore the bug — a feed the farm buys drops out of the pie — and the check goes red", async () => {
  // This is 6fbc825f2 reverted: the pie keeps its own allowlist, so UHT Milk has a card with a
  // daily spend on it and no slice. The OLD assertion (the heading "Feed spend share") is still
  // perfectly true on this page.
  const broken = fakePage([
    { css: [PIE_SLICE_VALUE], text: "₹4,321 / day · 44.9%" },
    { css: [PIE_SLICE_VALUE], text: "₹3,180 / day · 33.1%" },
    { css: [PIE_SLICE_VALUE], text: "₹2,100 / day · 21.9%" },
    { css: [CARD_SPEND], text: "₹4,321" },
    { css: [CARD_SPEND], text: "₹3,180" },
    { css: [CARD_SPEND], text: "₹2,100" },
    { css: [CARD_SPEND], text: "₹1,145" }, // UHT Milk: a card, no slice
  ]);
  const miss = await checkValueExpect(broken, spendShareReconciliation);
  assert.ok(miss, "a feed missing from the pie must be reported");
  assert.match(miss.what, /the number of feeds in the spend share pie says 3/);
  assert.match(miss.what, /the number of feeds showing a daily spend on their own card comes to 4/);

  // And the money version catches it too, by ₹1,145 a day.
  const totals = await checkValueExpect(broken, spendShareTotals);
  assert.ok(totals);
  assert.match(totals.what, /9601/);
  assert.match(totals.what, /10746/);
});

test("MUTATION: a slice whose value drifts from its own card goes red on the money, not the count", async () => {
  const drifted = fakePage([
    { css: [PIE_SLICE_VALUE], text: "₹4,321 / day · 40.2%" },
    { css: [PIE_SLICE_VALUE], text: "₹3,180 / day · 29.6%" },
    { css: [CARD_SPEND], text: "₹4,321" },
    { css: [CARD_SPEND], text: "₹2,180" }, // a thousand rupees a day adrift
  ]);
  assert.equal(await checkValueExpect(drifted, spendShareReconciliation), null, "the counts still agree");
  const totals = await checkValueExpect(drifted, spendShareTotals);
  assert.ok(totals, "the money must not agree");
  assert.match(totals.what, /7501/);
});

test("the finding is a sentence about the farm's screen, with no selector in it", async () => {
  const broken = fakePage([
    { css: [PIE_SLICE_VALUE], text: "₹4,321 / day · 100.0%" },
    { css: [CARD_SPEND], text: "₹4,321" },
    { css: [CARD_SPEND], text: "₹1,145" },
  ]);
  const miss = await checkValueExpect(broken, spendShareReconciliation);
  assert.ok(saysSoundsHuman(miss.what), miss.what);
  for (const word of ["span", "css", "chartcard", "aria-label", "wchart", ".val"]) {
    assert.ok(!miss.what.includes(word), `the sentence must not mention ${word}: ${miss.what}`);
  }
});

test("a page with no pie at all is not a failure — it is nothing to reconcile", async () => {
  const empty = fakePage([]);
  const miss = await checkValueExpect(empty, spendShareReconciliation);
  assert.ok(miss?.missing, "an absent pie must report as absent, not as a wrong number");
});

// ---------------------------------------------------------------------------------------------
// The other comparison forms
// ---------------------------------------------------------------------------------------------

test("an exact label check catches the doubled pen label the old vocabulary could not", async () => {
  const good = fakePage([{ css: [".penlabel"], text: "Castro 1" }]);
  const bad = fakePage([{ css: [".penlabel"], text: "Castro 1 1" }]);
  const expectation = { equals: { css: ".penlabel", is: "Castro 1", says: "the pen named at the top of the weighing sheet" } };
  assert.equal(await checkValueExpect(good, expectation), null);
  const miss = await checkValueExpect(bad, expectation);
  assert.match(miss.what, /reads "Castro 1 1" but should read "Castro 1"/);
});

test("a comparison that is not an equality still reads as English", async () => {
  const expectation = {
    reconcile: {
      left: { css: ".cause .val", pick: "first", says: "the deaths whose cause is established" },
      right: { css: ".deaths .val", says: "the deaths tile" },
      relation: "atMost",
    },
  };
  const page = fakePage([{ css: [".cause .val"], text: "99999 / 6" }, { css: [".deaths .val"], text: "6" }]);
  const miss = await checkValueExpect(page, expectation);
  assert.equal(miss.what, "the deaths whose cause is established says 99999, and that should be no more than the deaths tile says 6");
});

test("a numeric bound fires on an impossible figure", async () => {
  const expectation = { value: { css: ".kpi .val", says: "the average weight on the shed card", atMost: 120, pick: "first" } };
  assert.equal(await checkValueExpect(fakePage([{ css: [".kpi .val"], text: "31.4 kg" }]), expectation), null);
  const miss = await checkValueExpect(fakePage([{ css: [".kpi .val"], text: "3140 kg" }]), expectation);
  assert.match(miss.what, /shows 3140, which should be at most 120/);
});

test("a pattern check can forbid a shape without forbidding ordinary words", async () => {
  const expectation = {
    matches: {
      css: ".penlabel",
      pattern: "^\\s*$|undefined|NaN",
      expect: false,
      shouldRead: "is a gap where the pen's name should be",
      says: "the pen named on the row",
    },
  };
  assert.equal(await checkValueExpect(fakePage([{ css: [".penlabel"], text: "Godel 1 - Part 3" }]), expectation), null);
  const miss = await checkValueExpect(fakePage([{ css: [".penlabel"], text: "undefined" }]), expectation);
  assert.match(miss.what, /is a gap where the pen's name should be/);
});

test("a figure that could not be read is NOT ATTEMPTED — never a pass, never an accusation", async () => {
  // The read-only farm database is not reachable from every run. When it is not, a check that
  // needs it has proved nothing, and says so. It must not go green and it must not accuse the
  // product either — that is the failure that put five unearned verdicts in one night.
  const queries = new Map([["herd-alive-count", { id: "herd-alive-count", sql: "select 1 limit 1", says: "the animals on the farm" }]]);
  const expectation = {
    reconcile: {
      left: { css: ".herd-total .val", says: "the herd total on the screen" },
      right: { query: "herd-alive-count", says: "the number of animals the farm's records hold" },
    },
  };
  delete process.env.GOATOS_STG_READONLY_DATABASE_URL;
  resetQueryCache();
  const miss = await checkValueExpect(fakePage([{ css: [".herd-total .val"], text: "812" }]), expectation, { queries });
  assert.ok(miss?.notAttempted, `with no read-only database this must say it did not run, got ${JSON.stringify(miss)}`);
  assert.ok(!miss.valueMismatch);
  assert.match(miss.what, /was not reachable/);
});

test("a query nobody registered is a fault in the check, not a verdict on the product", async () => {
  resetQueryCache();
  const expectation = { value: { query: "no-such-figure", says: "a figure", equals: 1 } };
  const miss = await checkValueExpect(fakePage([]), expectation, { queries: new Map() });
  assert.ok(miss);
  assert.ok(!miss.valueMismatch);
});

// ---------------------------------------------------------------------------------------------
// The catalogue guard — "evidence path invented" must not be committable
// ---------------------------------------------------------------------------------------------

test("a comparison side must say, in the farm's words, what a person sees", () => {
  assert.deepEqual(validateValueExpect(spendShareReconciliation), []);
  const nameless = { reconcile: { left: { countOf: ".a" }, right: { countOf: ".b" } } };
  assert.ok(validateValueExpect(nameless).some((p) => /in the farm's words/.test(p)));
});

test("a pinned number must justify itself, because it is the one source that goes stale", () => {
  const pinned = { value: { css: ".x", equals: 10, says: "the thing" } };
  assert.deepEqual(validateValueExpect(pinned), []);
  const badSide = { reconcile: { left: { css: ".x", says: "a thing" }, right: { literal: 10, says: "ten" } } };
  assert.ok(validateValueExpect(badSide).some((p) => /why and when/.test(p)));
});

test("a side cannot be two things at once", () => {
  const muddled = { reconcile: { left: { css: ".x", countOf: ".y", says: "a thing" }, right: { css: ".z", says: "another" } } };
  assert.ok(validateValueExpect(muddled).some((p) => /exactly one of/.test(p)));
});

test("every value assertion in the manifest passes the guard", () => {
  let converted = 0;
  for (const entry of loadFeatureAssertions()) {
    for (const expectation of entry.expect ?? []) {
      if (!isValueExpect(expectation)) continue;
      converted += 1;
      assert.deepEqual(validateValueExpect(expectation, entry.sha), [], `${entry.sha} ${entry.title}`);
      assert.ok(entry.provenance, `${entry.sha} must say where its expected value comes from`);
      assert.ok(["reconciliation", "read-only-query", "pinned"].includes(entry.provenance), `${entry.sha} provenance: ${entry.provenance}`);
    }
  }
  assert.ok(converted > 0, "the manifest must actually carry value assertions");
});

test("the runner refuses an expectation it does not understand instead of passing it", () => {
  const source = readFileSync(new URL("./feature-assertions.mjs", import.meta.url), "utf8");
  assert.match(source, /this check asks for something the sweep cannot do/);
  assert.match(source, /feature_not_attempted=/);
});
