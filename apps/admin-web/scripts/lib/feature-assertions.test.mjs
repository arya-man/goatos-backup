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
  assert.match(source, /missing\.slice\(0, 4\)\.map\(say\)/);

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
      const named = expect.visible ?? expect.absent ?? expect.count ?? expect.url;
      assert.ok(named, `${entry.sha}: an expect with nothing to check`);
    }
  }
});
