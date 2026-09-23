import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { WRITE_WORDS, loadFeatureAssertions } from "./feature-assertions.mjs";

test("feature assertion steps refuse anything that writes", () => {
  for (const word of ["Save", "Approve", "Reject", "Delete", "Submit", "Upload", "Download", "Assign", "Publish", "Mark reached", "Create task"]) {
    assert.ok(WRITE_WORDS.test(word), word);
  }
  for (const word of ["Stock", "List", "Flow", "Overdue", "Filters", "Status-wise"]) assert.ok(!WRITE_WORDS.test(word), word);
});

test("feature assertions manifest loads and every runnable entry names a route and an expectation", () => {
  for (const entry of loadFeatureAssertions()) {
    assert.ok(entry.route, entry.sha);
    assert.ok(Array.isArray(entry.expect) && entry.expect.length > 0, entry.sha);
    for (const step of entry.steps ?? []) assert.ok(!WRITE_WORDS.test(step.click?.text ?? step.click?.css ?? ""), `${entry.sha} step writes`);
  }
});

test("smoke runs feature assertions on every route after overlays", () => {
  const smoke = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");
  assert.match(smoke, /await check\(\(\) => assertFeaturesPresent\(page,/);
});

test("the triaged PR350 entries assert the screen, not the sidebar, and are still present", () => {
  const entries = new Map(loadFeatureAssertions().map((entry) => [entry.sha, entry]));

  // 798497220 failed on laptop only because getByText("Mortality").first() resolved to the
  // collapsed Counts -> Mortality nav leaf. Scoped targets keep it on the analytics screen.
  const health = entries.get("798497220");
  assert.ok(health, "798497220 must not be deleted");
  assert.equal(health.route, "health-analytics");
  assert.ok(health.expect.every((e) => e.visible?.css), "health-analytics expects must be scoped CSS, not bare text");
  assert.ok(health.expect.some((e) => e.visible.css === ".feed-tabbar"));
  for (const label of ["Mortality", "Diagnosis engine"]) {
    assert.ok(
      health.expect.some((e) => e.visible.css === `.feed-tabbar :has-text("${label}")`),
      `${label} must be asserted inside .feed-tabbar`,
    );
  }
  assert.ok(health.expect.some((e) => e.visible.css === '.kpi-row :has-text("Open cases now")'));
  assert.equal(health.triage, "bad-assertion");

  // acbb15186 is the first clicking entry on its route; it lost the race with hydration.
  const weighing = entries.get("acbb15186");
  assert.ok(weighing, "acbb15186 must not be deleted");
  assert.equal(weighing.route, "weighing-sop-flow");
  assert.ok(weighing.expect.some((e) => e.visible?.css === '[data-testid="studio-view-list"][aria-selected="true"]'));
  assert.ok(weighing.expect.some((e) => e.visible?.css === ".sop-weighing .qlist"));
  assert.equal(weighing.triage, "harness-flake");
  for (const entry of [health, weighing]) assert.ok((entry.reason ?? "").length > 40, `${entry.sha} needs its triage reason`);
});

test("a clicking entry is replayed once before it is reported missing", () => {
  const source = readFileSync(new URL("./feature-assertions.mjs", import.meta.url), "utf8");
  assert.match(source, /const attempt = async \(\) => \{/);
  assert.match(source, /if \(miss && entry\.steps\?\.length && reload\) miss = await attempt\(\);/);
});

// A multi-expect entry named only its title, so "Feed Config has an add feed item control"
// was reported when the Add feed item button was on the page several times over and only the
// "Feed items" heading was gone. The sentence must name the part that actually failed.
test("a missing feature names the expectation that failed, not just the entry title", () => {
  const source = readFileSync(new URL("./feature-assertions.mjs", import.meta.url), "utf8");
  assert.match(source, /const say = \(m\) => \{/);
  assert.match(source, /\^\(\?:not visible\|should not appear\): \(\.\+\)\$/);
  // Features that have gone are still named by `say`; figures that disagree carry their own
  // sentence and are listed separately, so the two kinds never get merged into one label.
  assert.match(source, /gone\.slice\(0, 4\)\.map\(say\)/);
  assert.match(source, /figures do not agree/);

  // The shape checkExpect actually produces for a missing "visible" target.
  const say = (m) => {
    const what = String(m.miss?.what ?? "");
    const quoted = what.match(/^(?:not visible|should not appear): (.+)$/);
    if (quoted) return `${m.entry.title} — "${quoted[1]}" is not on the page`;
    if (what) return `${m.entry.title} — ${what}`;
    return m.entry.title;
  };
  assert.equal(
    say({ entry: { title: "Feed Config has an add feed item control" }, miss: { what: "not visible: Feed items" } }),
    'Feed Config has an add feed item control — "Feed items" is not on the page',
  );
  assert.equal(
    say({ entry: { title: "x" }, miss: { what: "expected at least 1 of .qlist, found 0" } }),
    "x — expected at least 1 of .qlist, found 0",
  );
});

test("every asserted feature entry states which expectation it is checking", () => {
  for (const entry of loadFeatureAssertions()) {
    for (const expect of entry.expect ?? []) {
      // The value forms say what a figure SHOULD BE rather than that something is present.
      const named = expect.visible ?? expect.absent ?? expect.count ?? expect.url
        ?? expect.reconcile ?? expect.value ?? expect.equals ?? expect.matches;
      assert.ok(named, `${entry.sha}: an expect with nothing to check`);
    }
  }
});

test("a step written as a sentence is a check to finish, not a broken feature", () => {
  const source = readFileSync(new URL("./feature-assertions.mjs", import.meta.url), "utf8");
  // A prose step reaches locatorFor with neither css nor text, so it used to throw an
  // error that read like a product failure and was counted as one on every single run.
  assert.match(source, /NEEDS_STEP_PREFIX/);
  assert.match(source, /typeof target === "string" \|\| \(!target\.css && !target\.text\)/);
  // It must be separated BEFORE the missing-feature bucket.
  const needsAt = source.indexOf("message.startsWith(NEEDS_STEP_PREFIX)");
  const missingAt = source.indexOf('if (entry.status !== "data-dependent") missing.push(');
  assert.ok(needsAt > 0 && needsAt < missingAt, "unwritten steps must be split off before the missing bucket");
  // And it must land on the wording that says a check needs review, not one that says
  // the farm's screen is broken.
  assert.match(source, /assertion\(s\) need review/);
});

test("the manifest still carries the unwritten steps this split is for", () => {
  // These are the entries that reported a present feature as missing on 2026-09-22.
  // When someone finishes writing them as { css } / { text }, this list shrinks —
  // it is here so the shrink is deliberate and visible, not silent.
  const prose = [];
  for (const entry of loadFeatureAssertions()) {
    for (const step of entry.steps ?? []) {
      if (typeof step.click === "string") prose.push(entry.sha);
    }
  }
  assert.ok(prose.includes("5c504076c"), "the Tasks attachment picker entry");
  assert.ok(prose.includes("75d30c3ef"), "the Tasks scope entry");
});

// ---------------------------------------------------------------------------------------------
// Noise. A check that fires on a correct page is worse than no check.
//
// 57 assertions demanded that an ordinary product word never appear anywhere on a page —
// `absent "weighed"` on a livestock app, `absent "Doing"` as a substring on the tasks board.
// They are now scoped to the part of the screen the commit actually changed, so they still go
// red when the fix is reverted and no longer fire on ordinary content.
// ---------------------------------------------------------------------------------------------

// Words that are ordinary farm or product vocabulary. A whole-page substring match on any of
// these is a false positive waiting to happen.
const EVERYDAY_WORDS = [
  "weighed", "Doing", "Missing", "At risk", "Readings", "Age group", "Estimated battery life",
  "Feed by shed", "Cost to buy", "Add feed type", "Recent leads", "All evidence", "Open photo",
  "Penalty", "Reassign", "Other exits", "True", "Integration notes", "Verification latency",
];

test("no check demands that an ordinary product word be absent from a whole page", () => {
  const offenders = [];
  for (const entry of loadFeatureAssertions()) {
    for (const expectation of entry.expect ?? []) {
      const text = expectation.absent?.text;
      if (!text) continue;
      if (EVERYDAY_WORDS.some((word) => word.toLowerCase() === text.toLowerCase())) {
        offenders.push(`${entry.sha} ${entry.route}: absent "${text}" over the whole page`);
      }
    }
  }
  assert.deepEqual(offenders, [], `these fire on a correct page:\n${offenders.join("\n")}`);
});

test("a rewritten negative says what it used to be, so the change is auditable", () => {
  let scoped = 0;
  for (const entry of loadFeatureAssertions()) {
    for (const expectation of entry.expect ?? []) {
      if (!expectation.absentWas) continue;
      scoped += 1;
      assert.ok(expectation.absent?.css, `${entry.sha}: a rewritten negative must be scoped to an element`);
      assert.ok(expectation.absent.css.includes(expectation.absentWas), `${entry.sha}: the scoped form must still look for the same words`);
      assert.ok(expectation.absent.css.length > expectation.absentWas.length + 4, `${entry.sha}: "${expectation.absent.css}" is not actually scoped`);
    }
  }
  assert.ok(scoped >= 20, `expected the noisy negatives to have been scoped, found ${scoped}`);
});

test("no string is asserted absent by more than two checks on one route", () => {
  // 'Integration notes' was asserted by seven checks on weighing-analytics: one product change
  // would have posted seven findings into the alert channel.
  const seen = new Map();
  for (const entry of loadFeatureAssertions()) {
    for (const expectation of entry.expect ?? []) {
      const text = expectation.absent?.text ?? expectation.absentWas;
      if (!text) continue;
      const key = `${entry.route}|${text}`;
      seen.set(key, (seen.get(key) ?? 0) + 1);
    }
  }
  // Error sentinels are allowed to repeat: every page may refuse to render "undefined".
  const SENTINELS = new Set(["undefined", "null", "NaN", "Invalid Date", "Hydration failed", "Something went wrong"]);
  const loud = [...seen].filter(([key, n]) => n > 2 && !SENTINELS.has(key.split("|")[1]));
  assert.deepEqual(loud, [], `one product change would post ${loud[0]?.[1]} findings for ${loud[0]?.[0]}`);
});

test("every runnable check asserts something", () => {
  // An entry with an empty expect array asserts literally nothing and was still counted as
  // covering its commit.
  for (const entry of loadFeatureAssertions()) {
    assert.ok((entry.expect ?? []).length > 0, `${entry.sha} ${entry.title} asserts nothing but is runnable`);
  }
});
