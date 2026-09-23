import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import path from "node:path";
import {
  ADMIN_WEB_STYLESHEETS,
  checkCompositingHazards,
  compositingSummary,
  findCompositedPinnedElements,
  leafRules,
} from "./compositing-checks.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../..");

// ---------------------------------------------------------------------------
// The thing it is for
// ---------------------------------------------------------------------------
test("flags an element that is pinned and blurs what is behind it", () => {
  const found = findCompositedPinnedElements(".bar{position:sticky;top:0;backdrop-filter:blur(8px)}", "x.css");
  assert.equal(found.length, 1);
  assert.equal(found[0].selector, ".bar");
  assert.equal(found[0].risk, "sticky");
  assert.match(found[0].what, /redraw the blur on every frame/);
});

test("a plain filter counts too, not only backdrop-filter", () => {
  assert.equal(findCompositedPinnedElements(".bar{position:sticky;filter:blur(2px)}").length, 1);
  assert.equal(findCompositedPinnedElements(".bar{position:sticky;-webkit-backdrop-filter:blur(2px)}").length, 1);
});

// ---------------------------------------------------------------------------
// The thing it must not do: fire on a filter that is not pinned to anything
// ---------------------------------------------------------------------------
test("does not flag a filter on an element that is not pinned", () => {
  assert.deepEqual(findCompositedPinnedElements(".loading{position:absolute;inset:0;backdrop-filter:blur(1px)}"), []);
  assert.deepEqual(findCompositedPinnedElements(".card{backdrop-filter:blur(4px)}"), []);
  assert.deepEqual(findCompositedPinnedElements(".card{position:relative;filter:saturate(1.2)}"), []);
});

test("does not flag a pinned element whose filter is turned off", () => {
  assert.deepEqual(findCompositedPinnedElements(".bar{position:sticky;backdrop-filter:none}"), []);
  // Last declaration in the block wins, exactly as the cascade does inside one block.
  assert.deepEqual(findCompositedPinnedElements(".bar{position:sticky;backdrop-filter:blur(8px);backdrop-filter:none}"), []);
  // And the other way round, where the blur is the one that wins.
  assert.equal(findCompositedPinnedElements(".bar{position:sticky;backdrop-filter:none;backdrop-filter:blur(8px)}").length, 1);
});

test("a fixed overlay is reported separately, not counted as a scrolling hazard", () => {
  const found = findCompositedPinnedElements(".veil{position:fixed;inset:0;backdrop-filter:blur(3px)}");
  assert.equal(found[0].risk, "fixed");
  assert.match(found[0].what, /only tears if what is underneath keeps moving/);
});

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------
test("reads rules inside a media query and keeps the context", () => {
  const found = findCompositedPinnedElements("@media(max-width:760px){.bar{position:sticky;backdrop-filter:blur(2px)}}");
  assert.equal(found.length, 1);
  assert.deepEqual(found[0].context, ["@media(max-width:760px)"]);
});

test("a comment never moves a reported line number", () => {
  const css = ["/* one", " * two", " * three */", ".bar{position:sticky;backdrop-filter:blur(8px)}"].join("\n");
  assert.equal(findCompositedPinnedElements(css)[0].line, 4);
});

test("a commented-out rule is not a rule", () => {
  assert.deepEqual(findCompositedPinnedElements("/* .bar{position:sticky;backdrop-filter:blur(8px)} */"), []);
});

test("only leaf rules are considered, so a wrapper is never mistaken for a declaration", () => {
  const rules = leafRules("@supports(backdrop-filter:blur(1px)){.a{color:red}}");
  assert.deepEqual(rules.map((r) => r.selector), [".a"]);
});

// ---------------------------------------------------------------------------
// Against the stylesheet that is actually shipped
// ---------------------------------------------------------------------------
test("finds every pinned-and-blurred element on this checkout, and nothing else", () => {
  const result = checkCompositingHazards(repo);
  assert.deepEqual(result.scanned, [...ADMIN_WEB_STYLESHEETS]);
  const selectors = result.scrolling.map((f) => f.selector).sort();
  // The three the filmed bug is about: the top bar, the back bar, the task filter bar.
  assert.deepEqual(selectors, [".lt-page .lt-fbar", ".navback", ".top"]);
  assert.equal(result.ok, false);
  // The two fixed overlays are known, reported, and deliberately not a failure.
  assert.deepEqual(result.overlays.map((f) => f.selector).sort(), [".schedule-drawer-backdrop", ".veil"]);
  // The negative control from the same stylesheet: a filter with no pinning at all.
  assert.ok(!result.findings.some((f) => f.selector === ".vr-results-loading"),
    "an absolutely positioned blur must not be reported");
});

test("an element switched off at phone width is flagged but not called a phone problem", () => {
  const css = [
    ".bar{position:sticky;top:0;backdrop-filter:blur(8px)}",
    "@media(max-width:640px){.bar{position:static}}",
  ].join("\n");
  const found = findCompositedPinnedElements(css);
  assert.equal(found.length, 1, "the combination is still in the stylesheet and still reported");
  assert.equal(found[0].appliesOnPhone, false);
  assert.equal(found[0].neutralisedAtLine, 2);
});

test("an override that does not apply at phone width does not excuse anything", () => {
  const css = [
    ".bar{position:sticky;top:0;backdrop-filter:blur(8px)}",
    "@media(min-width:900px){.bar{position:static}}",
  ].join("\n");
  assert.equal(findCompositedPinnedElements(css)[0].appliesOnPhone, true);
});

test("turning the blur off at phone width counts as switching it off too", () => {
  const css = [
    ".bar{position:sticky;top:0;backdrop-filter:blur(8px)}",
    "@media(max-width:760px){.bar{backdrop-filter:none}}",
  ].join("\n");
  assert.equal(findCompositedPinnedElements(css)[0].appliesOnPhone, false);
});

test("on this checkout the two bars that stay pinned on a phone are named, and the filter bar is not", () => {
  // The stylesheet makes the task filter bar `position:static` below 640px, so the
  // combination is real on a laptop and gone on a phone. The check has to say so:
  // a phone channel must not be told about a laptop-only combination.
  const result = checkCompositingHazards(repo);
  assert.deepEqual(result.onPhone.map((f) => f.selector).sort(), [".navback", ".top"]);
  const filterBar = result.scrolling.find((f) => f.selector === ".lt-page .lt-fbar");
  assert.equal(filterBar.appliesOnPhone, false);
  assert.ok(filterBar.neutralisedAtLine > filterBar.line, "and it says where it is switched off");
});

test("the summary names the elements without naming a CSS property", () => {
  const summary = compositingSummary(checkCompositingHazards(repo));
  assert.match(summary, /stay pinned while the page scrolls under them/);
  assert.ok(!summary.includes("backdrop-filter"), "the summary is read by people, not by browsers");
});

test("a stylesheet that has moved is not this check's business to fail on", () => {
  const result = checkCompositingHazards(repo, ["apps/admin-web/app/no-such-file.css"]);
  assert.deepEqual(result.scanned, []);
  assert.equal(result.ok, true);
});
