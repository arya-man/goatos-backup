import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { WIDTH_WORDS, auditPins, classesIn, classifyPin, hiddenAtWidth, mediaBlocks, selectorsOf } from "./viewport-pin-audit.mjs";
import { loadFeatureAssertions } from "./feature-assertions.mjs";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "../../../..");
const adminWeb = join(repoRoot, "apps/admin-web");
const css = () => readFileSync(join(adminWeb, "app/mesha-theme.css"), "utf8");

test("the runnable viewport-pinned set is much smaller than the raw manifest suggests", () => {
  // The number everyone quotes — 75 laptop-only / 27 mobile-only — is from the
  // RAW manifest, which includes entries the runner never executes. Among the
  // entries that actually run it is 53 and 13.
  const runnable = loadFeatureAssertions();
  const pinned = runnable.filter((e) => (e.viewports ?? []).length === 1);
  const byWidth = {};
  for (const e of pinned) byWidth[e.viewports[0]] = (byWidth[e.viewports[0]] ?? 0) + 1;
  assert.ok(pinned.length < 100, `the runnable pinned set is ${pinned.length}, not the raw manifest's 102`);
  assert.ok(byWidth.laptop > byWidth.mobile, "most pins are laptop-only");
});

test("media blocks are parsed by the width they apply at", () => {
  const blocks = mediaBlocks("@media(max-width:760px){.a{display:none}} @media print{.b{display:none}} @media (max-width: 600px) { .c { display : none } }");
  assert.equal(blocks.length, 2, "only width queries count");
  assert.deepEqual(blocks.map((b) => b.bound).sort((a, b) => a - b), [600, 760]);
});

test("a query shape the parser does not understand is treated as applying everywhere", () => {
  // The cautious reading. It can only ever hold a widen BACK, never let one through.
  const blocks = mediaBlocks("@media (min-width:900px){.a{display:none}}");
  assert.equal(blocks[0].bound, Number.POSITIVE_INFINITY);
  assert.equal(hiddenAtWidth("@media (min-width:900px){.a{display:none}}", "a", 390).hidden, true);
});

test("an element the stylesheet hides at a width is never widened onto it", () => {
  const sheet = "@media(max-width:760px){.wcols{display:none}}";
  assert.equal(hiddenAtWidth(sheet, "wcols", 390).hidden, true, "hidden on a phone");
  assert.equal(hiddenAtWidth(sheet, "wcols", 1440).hidden, false, "present on a laptop");
  assert.equal(hiddenAtWidth(sheet, "other", 390).hidden, false, "and only that class");
});

test("a check whose own title is about one width is never widened", () => {
  for (const title of [
    "Grouped weighing values fit the card on phones",
    "Daily gain by shed in two ranked park columns",
    "Herd Signals mobile card stack fits the screen (no horizontal page scroll)",
  ]) {
    assert.ok(WIDTH_WORDS.test(title), `"${title}" announces its own width`);
    const verdict = classifyPin({ title, viewports: ["laptop"], expect: [{ visible: { css: ".anything" } }] }, { css: "", root: adminWeb, cache: new Map() });
    assert.equal(verdict.verdict, "by-design");
    assert.ok(verdict.why.includes("pinned on purpose"));
  }
});

test("selectors come from what a check clicks as well as what it asserts", () => {
  const entry = { steps: [{ click: { css: ".tagedit-value" } }], expect: [{ visible: { css: ".tagedit-pop" } }, { count: { css: ".tagedit-option", min: 1 } }] };
  assert.deepEqual(selectorsOf(entry).sort(), [".tagedit-option", ".tagedit-pop", ".tagedit-value"]);
  assert.deepEqual(classesIn(".a .b-c"), ["a", "b-c"]);
});

test("a class drawn by a component that branches on width in JS is left alone", () => {
  // This is the finding that makes blanket widening unsafe: admin-web decides
  // in JavaScript what to render at each width (useIsMobile / useIsDesktop), so
  // the DOM genuinely differs and a widened check could accuse a correct page.
  const cache = new Map();
  cache.set("__files__", [{ file: "/x/task-board-dnd.tsx", text: ".tboard useMediaQuery(DRAG_MEDIA_QUERY)" }]);
  const verdict = classifyPin(
    { title: "Task board renders its lanes", viewports: ["laptop"], expect: [{ visible: { css: ".tboard" } }] },
    { css: "", root: adminWeb, cache },
  );
  assert.equal(verdict.verdict, "viewport-js");
  assert.ok(verdict.why.includes("cannot be settled without opening the page"));
});

test("a class no source mentions is unknown, never assumed safe", () => {
  const cache = new Map();
  cache.set("__files__", []);
  const verdict = classifyPin(
    { title: "Something renders", viewports: ["laptop"], expect: [{ visible: { css: ".nothing-mentions-this" } }] },
    { css: "", root: adminWeb, cache },
  );
  assert.equal(verdict.verdict, "unknown", "failing closed: an unknown class holds the widen back");
});

test("the audit reports a fraction and a reason on every pin it leaves alone", () => {
  const entries = loadFeatureAssertions();
  const result = auditPins(entries, { css: css(), root: adminWeb });
  assert.match(result.fraction, /^\d+\/\d+ pinned checks provably hold at the other width$/);
  assert.equal(result.widenable + result.stillPinned.length, result.entriesPinned, "every pin is accounted for");
  for (const row of result.stillPinned) {
    assert.ok(row.why.length > 40, `a pin kept must say why: ${JSON.stringify(row)}`);
    assert.ok(["by-design", "hidden-by-css", "viewport-js", "unknown"].includes(row.verdict));
  }
});

test("classifyPin itself refuses to widen onto a width that hides the element", () => {
  // The branch above tests hiddenAtWidth in isolation; this pins that classifyPin
  // actually CONSULTS it. Deleting the consultation left the suite green before
  // this test existed.
  const cache = new Map();
  cache.set("__files__", [{ file: "/x/weights.tsx", text: "wcols renders here", viewportJs: false }]);
  const sheet = "@media(max-width:760px){.wcols{display:none}}";
  const verdict = classifyPin(
    { title: "Daily gain by shed is ranked", viewports: ["laptop"], expect: [{ visible: { css: ".wcols" } }] },
    { css: sheet, root: adminWeb, cache },
  );
  assert.equal(verdict.verdict, "hidden-by-css", "it is not on the page at 390, so widening would accuse a correct page");
  assert.ok(verdict.why.includes("760px"), "and the reason names the width that hides it");
  // The mirror: the same class going the other way IS widenable.
  const other = classifyPin(
    { title: "Daily gain by shed is ranked", viewports: ["mobile"], expect: [{ visible: { css: ".wcols" } }] },
    { css: sheet, root: adminWeb, cache },
  );
  assert.equal(other.verdict, "widenable", "a laptop does not apply a max-width:760px rule");
});

test("the audit refuses to count from inputs it did not read", () => {
  // Measured before this existed: with the source root missing it reported
  // 11 of 66 rather than failing, and with an EMPTY stylesheet it reported
  // 34 of 66 — HIGHER than the true 33, because nothing looks hidden when
  // there is no CSS. A missing input made it recommend MORE widening, which
  // ends in a check widened onto a width that does not draw the element.
  const entries = loadFeatureAssertions();
  assert.throws(() => auditPins(entries, { css: css(), root: "/nonexistent" }), /claims to read app\/ and could not/);
  assert.throws(() => auditPins(entries, { css: "", root: adminWeb }), /claims to read the stylesheet and got nothing/);
  assert.throws(() => auditPins(entries, { css: "   ", root: adminWeb }), /got nothing/);
});

test("the audit prints what its fraction was counted from", () => {
  const result = auditPins(loadFeatureAssertions(), { css: css(), root: adminWeb });
  assert.ok(result.read.sourceFiles > 100, `expected the whole component tree, read ${result.read.sourceFiles} files`);
  assert.ok(result.read.sourceCharacters > 1_000_000, "and its contents");
  assert.ok(result.read.mediaBlocks > 50, `and the stylesheet's width rules, found ${result.read.mediaBlocks}`);
});
