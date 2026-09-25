// Value operators (equals / compare / stable) and the read-only carriesIntoDialog step, proved
// against a fake screen: what is on it is exactly what each test says is on it.
import assert from "node:assert/strict";
import test from "node:test";
import {
  WRITE_WORDS,
  assertFeaturesPresent,
  isValueExpect,
  numberIn,
  openerIsDisclosure,
  sameShownValue,
} from "./feature-assertions.mjs";

// Minimal DOM classes so the in-page readers (instanceof checks) run in node.
class FakeEl {
  constructor({ text = "", attrs = {} } = {}) { this.innerText = text; this.textContent = text; this.attrs = attrs; }
  getAttribute(name) { return this.attrs[name] ?? null; }
  setAttribute(name, value) { this.attrs[name] = value; }
}
class FakeSelect extends FakeEl {
  constructor({ options, selected = 0, attrs } = {}) { super({ attrs }); this.options = options; this.selected = selected; }
  get selectedOptions() { return [{ textContent: this.options[this.selected] }]; }
}
class FakeInput extends FakeEl {
  constructor({ value = "", attrs } = {}) { super({ attrs }); this.value = value; }
}
globalThis.HTMLSelectElement ??= FakeSelect;
globalThis.HTMLInputElement ??= FakeInput;
globalThis.HTMLTextAreaElement ??= class {};

const cell = (text, visible = true) => ({ visible, el: new FakeEl({ text }) });

function makeLocator(get, hooks = {}) {
  const items = () => get();
  const one = (i) => makeLocator(() => items().slice(i, i + 1), hooks);
  const loc = {
    first: () => one(0),
    last: () => makeLocator(() => items().slice(-1), hooks),
    nth: (i) => one(i),
    count: async () => items().length,
    isVisible: async () => Boolean(items()[0]?.visible),
    innerText: async () => items()[0]?.el.innerText ?? "",
    getAttribute: async (n) => items()[0]?.el.getAttribute(n) ?? null,
    waitFor: async () => { if (!items()[0]?.visible) throw new Error("timed out waiting for it to be visible"); },
    evaluate: async (fn, arg) => fn(items()[0]?.el, arg),
    click: async () => { hooks.onClick?.(items()[0]); },
    selectOption: async (opt) => { hooks.onSelect?.(items()[0], opt); },
  };
  // Scoped lookups (inside a dialog) use the same screen.
  loc.locator = (k) => makeLocator(() => hooks.screen[k] ?? [], hooks);
  loc.getByText = loc.locator;
  loc.getByLabel = loc.locator;
  return loc;
}
function fakePage(screen, hooks = {}) {
  const h = { ...hooks, screen };
  const at = (k) => makeLocator(() => screen[k] ?? [], h);
  return {
    locator: at, getByText: at, getByLabel: at,
    url: () => "https://example.test/weighing/analytics",
    addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
}

async function run(page, expect, status = "assert") {
  const lines = [];
  const realLog = console.log;
  console.log = (line) => lines.push(String(line));
  let threw = null;
  const entry = { sha: "abc1234", title: "value check", route: "r", status, expect: [expect] };
  try {
    await assertFeaturesPresent(page, { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (error) {
    threw = String(error.message ?? error);
  } finally {
    console.log = realLog;
  }
  return { threw, lines };
}

test("presence is smoke; value operators are the checks that can be wrong", () => {
  for (const smoke of [{ visible: { css: ".x" } }, { count: { css: ".x", min: 1 } }, { absent: { css: ".x" } }, { url: { contains: "/x" } }]) {
    assert.equal(isValueExpect(smoke), false, JSON.stringify(smoke));
  }
  for (const real of [
    { equals: { css: ".t", is: "791" } },
    { compare: { left: { css: ".a" }, right: { css: ".b" } } },
    { stable: { target: { css: ".t" } } },
    { carriesIntoDialog: { open: { css: ".o" }, page: { css: ".p" }, inDialog: { css: ".d" } } },
  ]) assert.equal(isValueExpect(real), true, JSON.stringify(real));
});

test("numberIn reads the first figure, ignoring thousands separators", () => {
  assert.equal(numberIn("Total 1,234 goats"), 1234);
  assert.equal(numberIn("-3.5 kg"), -3.5);
  assert.equal(numberIn("none"), null);
  assert.equal(numberIn(null), null);
});

test("equals holds a string to the letter", async () => {
  assert.match((await run(fakePage({ ".s": [cell("published")] }), { equals: { css: ".s", is: "Published" } })).threw, /should read "Published", reads "published"/);
  assert.equal((await run(fakePage({ ".s": [cell("Published")] }), { equals: { css: ".s", is: "Published" } })).threw, null);
});

test("compare: a total must equal the sum of its visible rows", async () => {
  const expect = { compare: { left: { css: ".total" }, right: { css: ".row", all: "sum" } } };
  assert.equal((await run(fakePage({ ".total": [cell("120")], ".row": [cell("50"), cell("70"), cell("900", false)] }), expect)).threw, null);
  assert.match((await run(fakePage({ ".total": [cell("999")], ".row": [cell("50"), cell("70")] }), expect)).threw, /999 must equal 120/);
  // A row with no figure is unreadable, never a zero.
  assert.match((await run(fakePage({ ".total": [cell("120")], ".row": [cell("50"), cell("—")] }), expect)).threw, /not visible/);
});

test("compare: a count must equal the rows listed, and lte/gte/tolerance/ratio work", async () => {
  const count = { compare: { left: { css: ".n" }, right: { css: ".row", all: "count" } } };
  assert.equal((await run(fakePage({ ".n": [cell("3 items")], ".row": [cell("a"), cell("b"), cell("c")] }), count)).threw, null);
  assert.match((await run(fakePage({ ".n": [cell("7")], ".row": [cell("a"), cell("b"), cell("c")] }), count)).threw, /7 must equal 3/);
  const lte = { compare: { left: { css: ".w" }, right: { css: ".h" }, op: "lte" } };
  assert.match((await run(fakePage({ ".w": [cell("791")], ".h": [cell("715")] }), lte)).threw, /791 must not be more than 715/);
  const pct = { compare: { left: { css: ".p" }, right: { ratio: { part: { css: ".d" }, whole: { css: ".t" } }, times: 100 }, tolerance: 1 } };
  assert.equal((await run(fakePage({ ".p": [cell("64%")], ".d": [cell("160")], ".t": [cell("250")] }), pct)).threw, null);
  assert.match((await run(fakePage({ ".p": [cell("0%")], ".d": [cell("160")], ".t": [cell("250")] }), pct)).threw, /0 must equal 64/);
  // Nothing out of nothing is unjudgeable, not a wrong percentage.
  assert.match((await run(fakePage({ ".p": [cell("0%")], ".d": [cell("0")], ".t": [cell("0")] }), pct)).threw, /not visible/);
});

test("stable: a summary that moves on page two is reported; one that holds passes", async () => {
  let paged = false;
  const screen = { ".next": [cell("Next page")] };
  Object.defineProperty(screen, ".summary", { enumerable: true, get: () => [cell(paged ? "31" : "240")] });
  const expect = { stable: { target: { css: ".summary" }, through: [{ click: { css: ".next" } }], label: "the total" } };
  const moved = await run(fakePage(screen, { onClick: () => { paged = true; } }), expect);
  assert.match(moved.threw, /reads 240, then 31/);
  assert.match(moved.threw, /must describe the whole filter/);
  paged = false;
  const held = await run(fakePage({ ".summary": [cell("240")], ".next": [cell("Next page")] }), expect);
  assert.equal(held.threw, null);
});

// ------------------------------------------------------------------ carriesIntoDialog
function weighingScreen({ drawerFollows }) {
  const state = { period: "01/09/2026 – 25/09/2026", open: false, clicks: [] };
  const screen = {};
  Object.defineProperties(screen, {
    ".period-trigger": { enumerable: true, get: () => [{ visible: true, el: new FakeEl({ text: state.period }) }] },
    ".day-1-aug": { enumerable: true, get: () => [cell("1")] },
    ".download-open": { enumerable: true, get: () => [{ visible: true, el: new FakeEl({ text: "Download weights", attrs: { "aria-haspopup": "dialog" } }) }] },
    "aside.drawer.on": { enumerable: true, get: () => (state.open ? [cell("drawer")] : []) },
    ".drawer-period": {
      enumerable: true,
      get: () => [{ visible: true, el: new FakeInput({ value: drawerFollows ? state.period : "01/09/2026 – 25/09/2026" }) }],
    },
  });
  const hooks = {
    onClick: (item) => {
      const text = item?.el.innerText;
      state.clicks.push(text);
      if (text === "1") state.period = "01/08/2026 – 25/09/2026";
      if (text === "Download weights") state.open = true;
    },
  };
  return { page: fakePage(screen, hooks), state };
}

const adgPeriod = {
  carriesIntoDialog: {
    label: "Period",
    set: [{ click: { css: ".day-1-aug" } }],
    page: { css: ".period-trigger" },
    open: { css: ".download-open" },
    dialog: { css: "aside.drawer.on" },
    inDialog: { css: ".drawer-period" },
  },
};

test("carriesIntoDialog: a drawer that keeps the load-time Period is reported", async () => {
  const { page, state } = weighingScreen({ drawerFollows: false });
  const result = await run(page, adgPeriod);
  assert.match(result.threw, /Period on the page reads "01\/08\/2026 – 25\/09\/2026" but the dialog it opens reads "01\/09\/2026 – 25\/09\/2026"/);
  assert.deepEqual(state.clicks, ["1", "Download weights"], "only the filter and the opener were clicked — nothing inside the drawer");
});

test("carriesIntoDialog: a drawer that follows the changed Period passes", async () => {
  const { page } = weighingScreen({ drawerFollows: true });
  assert.equal((await run(page, adgPeriod)).threw, null);
});

test("carriesIntoDialog: a filter step that changed nothing cannot vouch for the drawer", async () => {
  const { page } = weighingScreen({ drawerFollows: true });
  const noop = structuredClone(adgPeriod);
  noop.carriesIntoDialog.set = [{ click: { css: ".period-trigger" } }];
  assert.match((await run(page, noop)).threw, /did not change what the page shows/);
});

test("carriesIntoDialog: an opener that is not a disclosure is refused before it is clicked", async () => {
  const screen = {
    ".p": [cell("Male")],
    ".go": [{ visible: true, el: new FakeEl({ text: "Download weights" }) }],
  };
  const clicks = [];
  const page = fakePage(screen, { onClick: (item) => clicks.push(item?.el.innerText) });
  const result = await run(page, { carriesIntoDialog: { page: { css: ".p" }, open: { css: ".go" }, inDialog: { css: ".d" } } });
  assert.equal(result.threw, null, "a refused step is a safety skip, not a finding");
  assert.ok(result.lines.some((l) => l.startsWith("feature_assertion_skip=")), "and it is said out loud");
  assert.deepEqual(clicks, [], "the download control was never pressed");
});

test("carriesIntoDialog: a select filter is changed by choosing an option, never by typing or submitting", async () => {
  const pageSelect = new FakeSelect({ options: ["All", "Male", "Female"], selected: 0 });
  let open = false;
  const screen = {};
  Object.defineProperties(screen, {
    ".sex": { enumerable: true, get: () => [{ visible: true, el: pageSelect }] },
    ".open": { enumerable: true, get: () => [{ visible: true, el: new FakeEl({ text: "Filters", attrs: { "aria-expanded": "false" } }) }] },
    '[role="dialog"]': { enumerable: true, get: () => (open ? [cell("d")] : []) },
    ".dsex": { enumerable: true, get: () => [{ visible: true, el: new FakeSelect({ options: ["All", "Male", "Female"], selected: 0 }) }] },
  });
  const page = fakePage(screen, {
    onSelect: (item, opt) => { item.el.selected = item.el.options.indexOf(opt.label ?? opt); },
    onClick: () => { open = true; },
  });
  const result = await run(page, {
    carriesIntoDialog: {
      label: "Sex", compare: "text",
      set: [{ select: { css: ".sex" }, option: "Male" }],
      page: { css: ".sex" }, open: { css: ".open" }, dialog: { css: '[role="dialog"]' }, inDialog: { css: ".dsex" },
    },
  });
  assert.match(result.threw, /Sex on the page reads "Male" but the dialog it opens reads "All"/);
});

test("openerIsDisclosure: write-shaped openers need aria proof; submit never passes", () => {
  assert.equal(openerIsDisclosure({ text: "Filters" }), true);
  assert.equal(openerIsDisclosure({ text: "Download weights" }), false);
  assert.equal(openerIsDisclosure({ text: "Download weights", hasPopup: "dialog" }), true);
  assert.equal(openerIsDisclosure({ text: "Export", expanded: "false" }), true);
  assert.equal(openerIsDisclosure({ text: "Export", hasPopup: "false" }), false);
  assert.equal(openerIsDisclosure({ text: "Filters", type: "submit" }), false);
  assert.ok(WRITE_WORDS.test("Download weights"), "the general step guard still refuses it as a click step");
});

test("sameShownValue compares dates by their digits and plain words case-insensitively", () => {
  assert.equal(sameShownValue("01/08/2026 – 25/09/2026", "01/08/2026 - 25/09/2026"), true);
  assert.equal(sameShownValue("01/08/2026 – 25/09/2026", "01/09/2026 – 25/09/2026"), false);
  assert.equal(sameShownValue("Male", "male"), true);
  assert.equal(sameShownValue("Male", "male", "text"), false);
  assert.equal(sameShownValue(null, "x"), false);
});
