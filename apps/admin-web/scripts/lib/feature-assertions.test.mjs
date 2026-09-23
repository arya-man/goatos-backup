import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { compareReadings } from "./reading-comparison.mjs";
import test from "node:test";
import { WRITE_WORDS, assertFeaturesPresent, isValueExpect, loadFeatureAssertions, numberIn, reloadCoverage } from "./feature-assertions.mjs";
import { resolveRoutes } from "./smoke-route-catalogue.mjs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), "../../../..");
const fullManifest = () => JSON.parse(readFileSync(join(repoRoot, "tools/dashboard-automation/feature-assertions.json"), "utf8"));
const realCoverage = () => reloadCoverage({
  routes: resolveRoutes(repoRoot).all,
  viewports: ["laptop", "mobile"],
  runnable: loadFeatureAssertions(),
  manifest: fullManifest(),
});

// ---------------------------------------------------------------- a page that can be empty
//
// Enough of a page for the assertion machinery to run against, so the three outcomes can be
// proved without a browser: what is on the screen is exactly what this says is on it.
const makeLocator = (items) => ({
  first: () => makeLocator(items.slice(0, 1)),
  nth: (i) => makeLocator(items.slice(i, i + 1)),
  count: async () => items.length,
  isVisible: async () => Boolean(items[0]?.visible),
  innerText: async () => items[0]?.text ?? "",
  getAttribute: async () => null,
  click: async () => {},
  evaluate: async () => {},
  waitFor: async () => { if (!items[0]?.visible) throw new Error("timed out waiting for it to be visible"); },
});
const fakePage = (onScreen = {}) => ({
  locator: (key) => makeLocator(onScreen[key] ?? []),
  getByText: (key) => makeLocator(onScreen[key] ?? []),
  url: () => "https://example.test/vaccination/plan",
  addStyleTag: async () => {},
  screenshot: async () => {},
  waitForLoadState: async () => {},
});
const shown = (text = "x") => [{ visible: true, text }];

/** Runs the assertions against a fake screen and returns every line it printed, plus the throw. */
async function runAgainst(page, entries) {
  const lines = [];
  const realLog = console.log;
  console.log = (line) => lines.push(String(line));
  let threw = null;
  try {
    await assertFeaturesPresent(page, {
      routeName: "vaccination-plan-edit", viewportLabel: "laptop", screenshotDir: "/tmp", entries,
    });
  } catch (error) {
    threw = String(error.message ?? error);
  } finally {
    console.log = realLog;
  }
  return { lines, threw, said: (prefix) => lines.filter((l) => l.startsWith(prefix)) };
}

const planEntry = (over = {}) => ({
  sha: "d00d1e", title: "A published plan version cannot be edited", route: "vaccination-plan-edit",
  viewports: ["laptop"], status: "data-dependent", expect: [{ visible: { css: ".plan-version-lock" } }], ...over,
});

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
  assert.match(source, /if \(result\?\.miss && entry\.steps\?\.length && reload\) result = await attempt\(\);/);
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
      const named = expect.visible ?? expect.absent ?? expect.count ?? expect.url ?? expect.equals ?? expect.compare ?? expect.stable;
      assert.ok(named, `${entry.sha}: an expect with nothing to check`);
      // A shape the ENGINE does not implement would be silently skipped by
      // checkExpect and read as a pass. This guard caught `stable` the moment it
      // was added, which is what it is for; it must keep catching the next one.
      const engine = readFileSync(join(repoRoot, "apps/admin-web/scripts/lib/feature-assertions.mjs"), "utf8");
      for (const shape of Object.keys(expect)) {
        assert.ok(engine.includes(`expect.${shape}`), `${entry.sha}: nothing in the engine reads "${shape}", so this expectation would be skipped and read as a pass`);
      }
    }
  }
});

test("a step written as a sentence is a check to finish, not a broken feature", () => {
  const source = readFileSync(new URL("./feature-assertions.mjs", import.meta.url), "utf8");
  // A prose step reaches locatorFor with neither css nor text, so it used to throw an
  // error that read like a product failure and was counted as one on every single run.
  assert.match(source, /NEEDS_STEP_PREFIX/);
  assert.match(source, /typeof target === "string" \|\| \(!target\.css && !target\.text\)/);
  // It must be separated BEFORE anything that renders a verdict: before the missing-feature
  // bucket, and before the harness-fault bucket that now catches everything else.
  const needsAt = source.indexOf("message.startsWith(NEEDS_STEP_PREFIX)");
  const stepTargetAt = source.indexOf("message.startsWith(STEP_TARGET_PREFIX)");
  const faultAt = source.indexOf("harnessFaults.push({ entry, why:");
  assert.ok(needsAt > 0 && needsAt < stepTargetAt, "unwritten steps must be split off before the missing bucket");
  assert.ok(stepTargetAt > 0 && stepTargetAt < faultAt, "a control that is not on the page is the product, not the harness");
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

// ---------------------------------------------------------------- the three outcomes
//
// THE PROOF. `vaccination-plan-edit` reported 17 of 17 assertions passed against a screen with
// nothing drawn on it - which is where published-plan-version immutability lives. Every one of
// those greens came from one line: a data-dependent entry whose target was not on the page
// returned "no miss", and no miss was read as a pass. A check that did not run must never
// render a verdict, in either direction.

test("a data-dependent check on an empty screen reports not-attempted, never a pass", async () => {
  const run = await runAgainst(fakePage({}), [planEntry()]);
  assert.equal(run.threw, null, "an empty screen is not an accusation against the product");
  assert.equal(run.said("feature_missing=").length, 0);
  assert.equal(run.said("feature_not_attempted=").length, 1, "it must say it was not checked");
  assert.match(run.said("feature_not_attempted=")[0], /A published plan version cannot be edited/);
  // And the count must not quietly carry it as a pass: nothing was judged here.
  assert.match(run.said("feature_assertions=")[0], /^feature_assertions=vaccination-plan-edit:laptop:0\/0 not_attempted=1/);
});

test("a data-dependent check whose screen HAS its rows still fails when the feature is gone", async () => {
  // The other half of the same bug: once the entry says how to tell the screen has its rows,
  // an absent feature on a populated screen is a finding, not a shrug.
  const entry = planEntry({ dataProbe: { css: ".plan-row" } });
  const run = await runAgainst(fakePage({ ".plan-row": shown("kids, 2 doses") }), [entry]);
  assert.match(String(run.threw), /feature missing/);
  assert.match(String(run.threw), /A published plan version cannot be edited/);
  assert.equal(run.said("feature_not_attempted=").length, 0);
});

test("a data-dependent check that holds is still a plain pass", async () => {
  const entry = planEntry({ dataProbe: { css: ".plan-row" } });
  const run = await runAgainst(fakePage({ ".plan-row": shown(), ".plan-version-lock": shown("Published") }), [entry]);
  assert.equal(run.threw, null);
  assert.match(run.said("feature_assertions=")[0], /:1\/1$/);
});

test("an error from the assertion machinery is a harness fault, never a silent green", async () => {
  // A malformed target used to throw, and the throw was swallowed for data-dependent entries:
  // the run reported the screen as fine while the check had not run at all.
  const entry = planEntry({ expect: [{ visible: { name: "not a target the browser can find" } }] });
  const run = await runAgainst(fakePage({}), [entry]);
  assert.match(String(run.threw), /harness fault/, "the harness must accuse itself, not the farm's screens");
  assert.equal(run.said("feature_missing=").length, 0, "and it must not be reported as a broken screen");
  assert.equal(run.said("feature_assertion_harness_fault=").length, 1);
  assert.match(run.said("feature_assertions=")[0], /harness_faults=1/);
});

test("something that must NOT appear is still a failure on a screen with no rows", async () => {
  // The one thing absence never excuses: if a thing is on the page that must never be there,
  // an empty screen is no defence.
  const entry = planEntry({ expect: [{ absent: { css: ".raw-key" } }] });
  const run = await runAgainst(fakePage({ ".raw-key": shown("plan.version.published") }), [entry]);
  assert.match(String(run.threw), /feature missing/);
});

// ---------------------------------------------------------------- expectations that can be wrong

test("presence is smoke; an exact string and a comparison between two figures can fail", () => {
  for (const smoke of [{ visible: { css: ".x" } }, { count: { css: ".x", min: 1 } }, { absent: { css: ".x" } }, { url: { contains: "/x" } }]) {
    assert.equal(isValueExpect(smoke), false, JSON.stringify(smoke));
  }
  for (const real of [{ equals: { css: ".total", is: "791" } }, { compare: { left: { css: ".a" }, right: { css: ".b" }, op: "lte" } }]) {
    assert.equal(isValueExpect(real), true, JSON.stringify(real));
  }
});

test("a figure that disagrees with the figure it is measured against is reported", async () => {
  const entry = planEntry({
    status: "assert",
    expect: [{ compare: { left: { css: ".weighed" }, right: { css: ".head" }, op: "lte" } }],
  });
  const wrong = await runAgainst(fakePage({ ".weighed": shown("791"), ".head": shown("715") }), [entry]);
  assert.match(String(wrong.threw), /791 must not be more than 715/);
  const right = await runAgainst(fakePage({ ".weighed": shown("700"), ".head": shown("715") }), [entry]);
  assert.equal(right.threw, null);
});

test("an exact string is held to the letter", async () => {
  const entry = planEntry({ status: "assert", expect: [{ equals: { css: ".stage", is: "Published" } }] });
  const wrong = await runAgainst(fakePage({ ".stage": shown("published") }), [entry]);
  assert.match(String(wrong.threw), /should read "Published", reads "published"/);
  const right = await runAgainst(fakePage({ ".stage": shown("Published") }), [entry]);
  assert.equal(right.threw, null);
});

test("a harness fault is still said out loud when there are real findings too", async () => {
  const run = await runAgainst(fakePage({ ".plan-row": shown() }), [
    planEntry({ sha: "aaa111", status: "assert", expect: [{ visible: { css: ".gone" } }] }),
    planEntry({ sha: "bbb222", expect: [{ visible: { name: "not a target" } }] }),
  ]);
  assert.match(String(run.threw), /feature missing/);
  assert.match(String(run.threw), /1 check\(s\) could not be run at all/);
  assert.equal(run.said("feature_assertion_harness_fault=").length, 1);
});


// ---------------------------------------------------------------- reload coverage
//
// Measured against the REAL manifest and the REAL route table, not a fixture: §8 says a
// self-test that only exercises a stub proves nothing about the path that runs.

test("reload coverage is a fraction over every page at every width", () => {
  const c = realCoverage();
  assert.equal(c.pairsExpected, resolveRoutes(repoRoot).all.length * 2,
    "the denominator is every route the sweep opens, at both widths");
  assert.match(c.fraction, /^\d+\/\d+ page\/width pairs carry a reload check$/,
    `coverage must read as a fraction, it reads "${c.fraction}"`);
  assert.ok(!/\b(tasks|herd-register)\b/.test(c.fraction), "the fraction must not degrade into a list of hits");
  // Measured on 2026-09-23: 177 of 292. Asserted as a floor so coverage cannot quietly fall.
  assert.ok(c.pairsWithACheck >= 177, `reload coverage has fallen to ${c.pairsWithACheck}, it was 177`);
  assert.equal(c.pairsWithACheck + c.gaps.length, c.pairsExpected, "every pair is either covered or a named gap");
});

test("every uncovered page names its own reason, and no two share one", () => {
  const c = realCoverage();
  assert.ok(c.gaps.length > 0, "there are gaps; pretending otherwise is the failure this replaced");
  const reasons = new Set();
  for (const gap of c.gaps) {
    assert.ok(gap.route && gap.viewport, "a gap names the page and the width");
    assert.ok(gap.why.includes(gap.route), `a gap's reason must name its own page, "${gap.why}" does not`);
    assert.ok(gap.why.includes(gap.viewport), `a gap's reason must name its own width, "${gap.why}" does not`);
    assert.ok(gap.why.length > 40, `a gap needs a sentence, not a label: "${gap.why}"`);
    reasons.add(gap.why);
  }
  // One reason per gap. A bulk "not covered" collapses this to a handful.
  assert.equal(reasons.size, c.gaps.length, `${c.gaps.length} gaps must carry ${c.gaps.length} distinct reasons, they carry ${reasons.size}`);
});

test("a check aimed at a page the sweep never opens is reported, and an unrouted one is not mistaken for it", () => {
  const c = realCoverage();
  // Measured: every orphan-looking entry was simply unrouted. Counting the two together
  // produced 42 false "can never run" findings, which is exactly the noise §2 forbids.
  assert.equal(c.unreachableEntries.length, 0, "no check currently names a page the sweep does not open");
  assert.ok(c.unroutedEntries.length > 0, "entries with no page at all exist and are reported separately");
  assert.ok(c.unroutedEntries.every((e) => e.why.includes("names no page")), "and say what they are");

  // The check bites: aim one at a page that is not in the table.
  const planted = reloadCoverage({
    routes: [{ name: "tasks" }],
    viewports: ["laptop", "mobile"],
    runnable: [{ sha: "abc", route: "a-page-that-does-not-exist", status: "assert" }],
    manifest: [{ sha: "abc", route: "a-page-that-does-not-exist", status: "assert" }],
  });
  assert.equal(planted.unreachableEntries.length, 1, "a check aimed at a missing page is found");
  assert.match(planted.unreachableEntries[0].why, /can never run/);
});

test("a check written for a width the sweep does not visit is reported, not silently dropped", () => {
  const planted = reloadCoverage({
    routes: [{ name: "tasks" }],
    viewports: ["laptop"],
    runnable: [{ sha: "abc", route: "tasks", status: "assert", viewports: ["mobile"] }],
    manifest: [{ sha: "abc", route: "tasks", status: "assert", viewports: ["mobile"] }],
  });
  assert.equal(planted.unreachableViewports.length, 1);
  assert.match(planted.unreachableViewports[0].why, /mobile width, which this sweep does not visit/);
});

test("a page whose only checks are unrunnable says which statuses and why", () => {
  const planted = reloadCoverage({
    routes: [{ name: "tasks" }],
    viewports: ["laptop"],
    runnable: [],
    manifest: [{ sha: "abc", route: "tasks", status: "screenshot-only" }],
  });
  assert.equal(planted.gaps.length, 1);
  assert.match(planted.gaps[0].why, /screenshot-only — its evidence is a screenshot/);
});

test("a page/width pair with nothing written for it is said out loud, not passed over in silence", async () => {
  const lines = [];
  const log = console.log;
  console.log = (line) => lines.push(String(line));
  try {
    await assertFeaturesPresent({}, { routeName: "tasks", viewportLabel: "laptop", entries: [] });
  } finally {
    console.log = log;
  }
  assert.ok(lines.some((l) => l.startsWith("feature_assertions_none=tasks:laptop:")),
    `an empty page/width pair must report that it was not checked, it logged ${JSON.stringify(lines)}`);
});


test("the sweep runner actually emits the coverage, and the receipt carries it", () => {
  // THE DEFECT: reloadCoverage was imported by exactly one file — its own test.
  // The arithmetic was right, the 115 named reasons were written, and no sweep
  // emitted them, no receipt carried them, nothing reached Slack. A number
  // nobody can see is not a measurement.
  const runner = readFileSync(join(repoRoot, "apps/admin-web/scripts/smoke-visual-live.mjs"), "utf8");
  assert.match(runner, /import \{[^}]*reloadCoverage[^}]*\} from "\.\/lib\/feature-assertions\.mjs"/,
    "the sweep runner must import the coverage function");
  assert.match(runner, /reloadCoverage\(\{/, "and call it");
  // Match the SUCCESS assignment specifically. A looser pattern was satisfied by
  // the error branch, so deleting the real one left the check green.
  assert.match(runner, /browserEvidence\.reload_coverage = coverage;/,
    "and put the computed coverage in the receipt, not only the error fallback");
  assert.match(runner, /reload_coverage=\$\{coverage\.fraction\}/, "and print the fraction");
  assert.match(runner, /reload_coverage_gap=/, "and print every gap's reason, which is the actual product of the work");
  // A failure to compute it must say "not checked", never vanish.
  assert.match(runner, /fraction: "not checked"/, "a coverage block that could not be built says so");
});


// ---------------------------------------------------------------- comparing VALUES
//
// The engine could only ever read one cell, which is why every invariant this
// product states about itself was inexpressible and 0 of 928 assertions compared
// a value. These are the shapes that catch a page whose figures are wrong while
// everything still renders - the class the blank-page gate cannot see.

const vCell = (text, visible = true) => ({ text, visible });
const vMakeLocator = (items) => ({
  first: () => vMakeLocator(items.slice(0, 1)),
  nth: (i) => vMakeLocator(items.slice(i, i + 1)),
  count: async () => items.length,
  isVisible: async () => Boolean(items[0]?.visible),
  innerText: async () => items[0]?.text ?? "",
  getAttribute: async () => null,
  click: async () => {},
  evaluate: async () => {},
  waitFor: async () => { if (!items[0]?.visible) throw new Error("not visible"); },
});
const vFakePage = (screen, onClick) => ({
  locator: (k) => vMakeLocator(screen[k] ?? []),
  getByText: (k) => vMakeLocator(screen[k] ?? []),
  url: () => "https://example.test/x",
  addStyleTag: async () => {}, screenshot: async () => {},
  waitForLoadState: async () => {},
  __click: onClick,
});

const vRun = async (screen, expects, steps = []) => {
  const entry = { sha: "s", title: "t", route: "r", status: "assert", expect: expects, steps };
  const errors = [];
  const log = console.log; console.log = () => {};
  try {
    await assertFeaturesPresent(vFakePage(screen), { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { errors.push(e.message); } finally { console.log = log; }
  return errors;
};

test("a total that disagrees with the sum of its rows is reported", async () => {
  const right = await vRun({ ".total": [vCell("120")], ".row": [vCell("50"), vCell("70")] },
    [{ compare: { left: { css: ".total" }, right: { css: ".row", all: "sum" } } }]);
  assert.deepEqual(right, [], "50 + 70 = 120 agrees");
  const wrong = await vRun({ ".total": [vCell("999")], ".row": [vCell("50"), vCell("70")] },
    [{ compare: { left: { css: ".total" }, right: { css: ".row", all: "sum" } } }]);
  assert.equal(wrong.length, 1, "999 does not");
  assert.match(wrong[0], /999 must equal 120/);
});

test("a count that disagrees with the rows listed is reported", async () => {
  const ok = await vRun({ ".tabcount": [vCell("3 items")], ".row": [vCell("a"), vCell("b"), vCell("c")] },
    [{ compare: { left: { css: ".tabcount" }, right: { css: ".row", all: "count" } } }]);
  assert.deepEqual(ok, []);
  const bad = await vRun({ ".tabcount": [vCell("7 items")], ".row": [vCell("a"), vCell("b"), vCell("c")] },
    [{ compare: { left: { css: ".tabcount" }, right: { css: ".row", all: "count" } } }]);
  assert.equal(bad.length, 1);
  assert.match(bad[0], /7 must equal 3/);
});

test("a row with no figure in it stops the sum instead of counting as zero", async () => {
  // Treating a blank row as 0 is how a sum quietly drifts under the total it is
  // checked against, and the page gets accused for the harness's arithmetic.
  const errors = await vRun({ ".total": [vCell("120")], ".row": [vCell("50"), vCell("—")] },
    [{ compare: { left: { css: ".total" }, right: { css: ".row", all: "sum" } } }]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /is not on the page/, "it reports as unreadable, never as a disagreement");
});

test("hidden rows are not summed or counted", async () => {
  const errors = await vRun({ ".total": [vCell("120")], ".row": [vCell("50"), vCell("70"), vCell("900", false)] },
    [{ compare: { left: { css: ".total" }, right: { css: ".row", all: "sum" } } }]);
  assert.deepEqual(errors, [], "a row that is not on the screen is not part of what the screen claims");
});

test("a summary that changes when the page does is reported", async () => {
  let clicked = false;
  const screen = { ".summary": [vCell("240")], ".next": [vCell("Next page")] };
  const page = vFakePage(screen);
  // Re-point the locator so the second read sees page two's (wrong) summary.
  page.locator = (k) => {
    if (k === ".summary" && clicked) return vMakeLocator([vCell("31")]);
    if (k === ".next") {
      const nx = { ...vMakeLocator([vCell("Next page")]), click: async () => { clicked = true; } };
      nx.first = () => nx;
      return nx;
    }
    return vMakeLocator(screen[k] ?? []);
  };
  const entry = { sha: "s", title: "t", route: "r", status: "assert",
    expect: [{ stable: { target: { css: ".summary" }, through: [{ click: { css: ".next" } }], label: "the total" } }] };
  const log = console.log; console.log = () => {};
  let message = null;
  try {
    await assertFeaturesPresent(page, { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { message = e.message; } finally { console.log = log; }
  assert.ok(message, "a summary computed from the rows on screen must be reported");
  assert.match(message, /reads 240, then 31/);
  assert.match(message, /must describe the whole filter/);
});

test("all three new shapes count as references that can fail", () => {
  assert.equal(isValueExpect({ compare: { left: {}, right: {} } }), true);
  assert.equal(isValueExpect({ stable: { target: {} } }), true);
  assert.equal(isValueExpect({ visible: {} }), false, "presence is still smoke");
  assert.equal(isValueExpect({ count: { css: ".x", min: 1 } }), false, "and so is a floor of one");
});

test("a percentage is checked against the numbers it is derived from", async () => {
  // The vaccination drive ring, its percentage and its "N of M" sentence are one
  // backend-owned number rendered three ways; they must agree on one page.
  const screen = { ".rtx": [vCell("64%")], ".done": [vCell("160")], ".total": [vCell("250")] };
  const agreeing = await vRun(screen, [{
    compare: { left: { css: ".rtx" }, right: { ratio: { part: { css: ".done" }, whole: { css: ".total" } }, times: 100 }, tolerance: 1 },
  }]);
  assert.deepEqual(agreeing, [], "160 of 250 is 64%");

  const ringHeldBack = await vRun({ ...screen, ".rtx": [vCell("0%")] }, [{
    compare: { left: { css: ".rtx" }, right: { ratio: { part: { css: ".done" }, whole: { css: ".total" } }, times: 100 }, tolerance: 1 },
  }]);
  assert.equal(ringHeldBack.length, 1, "a ring held at 0 while the work is done is the recorded defect");
  assert.match(ringHeldBack[0], /0 must equal 64/);
});

test("nothing out of nothing is not zero per cent, it is not a question", async () => {
  // A drive with no animals must not be accused of a wrong percentage.
  const errors = await vRun({ ".rtx": [vCell("0%")], ".done": [vCell("0")], ".total": [vCell("0")] }, [{
    compare: { left: { css: ".rtx" }, right: { ratio: { part: { css: ".done" }, whole: { css: ".total" } }, times: 100 } },
  }]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /is not on the page/, "it reads as unjudgeable, never as a disagreement");
});

test("a rounded percentage is not a disagreement", async () => {
  // 1 of 3 renders as 33%, not 33.333. A tolerance of one point is the rounding,
  // not a threshold raised to silence a finding.
  const errors = await vRun({ ".rtx": [vCell("33%")], ".done": [vCell("1")], ".total": [vCell("3")] }, [{
    compare: { left: { css: ".rtx" }, right: { ratio: { part: { css: ".done" }, whole: { css: ".total" } }, times: 100 }, tolerance: 1 },
  }]);
  assert.deepEqual(errors, [], "rounding must not fire");
});

// ---------------------------------------------------------------- the invariants themselves
//
// The engine tests above prove the machinery. These run the REAL entries out of
// the manifest, so a wrong selector or a mis-written entry is caught here rather
// than on a live sweep — §8: a check nothing exercises is a check nobody has.

const invariantEntries = () => loadFeatureAssertions().filter((e) => e.group === "invariant");

test("the repo now carries assertions that can compare a value", () => {
  // It carried none: 0 of 928. A presence check holds on a page whose every
  // figure is wrong, which is the whole reason the coverage ledger read 87%.
  const entries = invariantEntries();
  assert.ok(entries.length > 0, "at least one invariant is asserted");
  for (const entry of entries) {
    assert.ok((entry.expect ?? []).some(isValueExpect), `${entry.sha} must carry a reference that can fail`);
    assert.ok(entry.evidence?.length > 40, `${entry.sha} must cite the rule it enforces, not someone's judgement`);
  }
});

test("a summary computed from the rows on screen is reported by the real entry", async () => {
  // The banned anti-pattern, played out: page one says 240, page two says 31,
  // because the summary was computed from the rows currently rendered.
  const entry = invariantEntries()[0];
  let turned = false;
  const next = () => {
    const n = { ...vMakeLocator([vCell("Next")]), click: async () => { turned = true; } };
    n.first = () => n;
    return n;
  };
  const page = {
    locator: (k) => {
      if (k === ".kpi .val") return vMakeLocator(turned ? [vCell("31"), vCell("9")] : [vCell("240"), vCell("9")]);
      if (k === entry.dataProbe.css) return next();
      return vMakeLocator([]);
    },
    getByText: () => vMakeLocator([]),
    url: () => "https://example.test/counts/milk-preparation",
    addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
  const log = console.log; console.log = () => {};
  let message = null;
  try {
    await assertFeaturesPresent(page, { routeName: entry.route, viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { message = e.message; } finally { console.log = log; }
  assert.ok(turned, "the entry turns the page");
  assert.ok(message, "and reports the summary that moved");
  assert.match(message, /249, then 40/, "249 on page one, 40 on page two");
  assert.match(message, /must describe the whole filter/);
});

test("a summary that holds across the page turn is a pass", async () => {
  const entry = invariantEntries()[0];
  const next = () => { const n = { ...vMakeLocator([vCell("Next")]), click: async () => {} }; n.first = () => n; return n; };
  const page = {
    locator: (k) => {
      if (k === ".kpi .val") return vMakeLocator([vCell("240"), vCell("9")]);
      if (k === entry.dataProbe.css) return next();
      return vMakeLocator([]);
    },
    getByText: () => vMakeLocator([]),
    url: () => "https://example.test/x",
    addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
  const log = console.log; console.log = () => {};
  let message = null;
  try {
    await assertFeaturesPresent(page, { routeName: entry.route, viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { message = e.message; } finally { console.log = log; }
  assert.equal(message, null, "NO FALSE POSITIVES: a correct page must not be accused");
});

test("a page with only one page of results is not attempted, never a pass", async () => {
  // The probe IS the click target, so a selector that matches nothing — a
  // single page of results, or a selector that turns out to be wrong — degrades
  // to not-attempted rather than accusing a page that is fine.
  const entry = invariantEntries()[0];
  const page = {
    locator: () => vMakeLocator([]),
    getByText: () => vMakeLocator([]),
    url: () => "https://example.test/x",
    addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
  const lines = [];
  const log = console.log; console.log = (l) => lines.push(String(l));
  let message = null;
  try {
    await assertFeaturesPresent(page, { routeName: entry.route, viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { message = e.message; } finally { console.log = log; }
  assert.equal(message, null, "it must not be a finding");
  assert.ok(lines.some((l) => l.startsWith("feature_not_attempted=")), "it must say it was not attempted");
  assert.ok(!lines.some((l) => /feature_assertions=.*:1\/1/.test(l)), "and must never be counted as a pass");
});

// ---------------------------------------------------------------- the shared comparison
//
// `expect.stable` and builder B's promotion rule ask one question — did these
// two readings of one target agree? — and differ only in the conditions the
// pair was taken under, and therefore in what a disagreement MEANS.

test("stable asks the shared primitive rather than re-implementing it", () => {
  const engine = readFileSync(join(repoRoot, "apps/admin-web/scripts/lib/feature-assertions.mjs"), "utf8");
  assert.match(engine, /import \{ compareReadings \} from "\.\/reading-comparison\.mjs"/,
    "one comparison, in one place");
  assert.match(engine, /compareReadings\(before\.values, after\.values, \{/, "and the stable branch calls it");
  // And no second implementation left behind.
  assert.ok(!/before\.number === after\.number/.test(engine),
    "the hand-rolled comparison must be gone, or the two can drift");
});

test("the two vocabularies stay apart", () => {
  // Pinned so neither side drifts into the other's wording: they describe
  // different facts about the page and must keep saying so.
  const moved = compareReadings([240], [31], { conditions: "deliberate-action", label: "the total" });
  const varies = compareReadings([240], [31], { conditions: "same", label: "the total" });
  assert.match(moved.verdict, /it moved when it should not/);
  assert.ok(!/something the page varies/.test(moved.verdict), "a deliberate-action verdict never borrows the other wording");
  assert.match(varies.verdict, /something the page varies, not something it owes/);
  assert.ok(!/it moved when it should not/.test(varies.verdict), "and vice versa");
});

test("a figure that was never drawn is refused at both readings, for every reducer", async () => {
  // Builder B's finding, and it was live here: two readings of a figure that was
  // never drawn are EQUAL, and equal is the pass condition. Measured before the
  // fix — `count` passed on a page that rendered nothing at both readings, while
  // `sum` and a single cell already refused it.
  for (const all of ["sum", "count", undefined]) {
    const next = () => { const n = { ...vMakeLocator([vCell("Next")]), click: async () => {} }; n.first = () => n; return n; };
    const page = {
      locator: (k) => (k === ".nx" ? next() : vMakeLocator([])),
      getByText: () => vMakeLocator([]),
      url: () => "x", addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
    };
    const entry = { sha: "s", title: "t", route: "r", status: "assert",
      expect: [{ stable: { target: { css: ".kpi .val", ...(all ? { all } : {}) }, through: [{ click: { css: ".nx" } }], label: "the figure" } }] };
    const log = console.log; console.log = () => {};
    let message = null;
    try {
      await assertFeaturesPresent(page, { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
    } catch (e) { message = e.message; } finally { console.log = log; }
    assert.ok(message, `reducer ${all ?? "none"}: a page that drew nothing must not pass`);
  }
});

test("an empty reading is turned into an absent one before the primitive sees it", () => {
  // Stated because it is a property of THIS caller, not of the primitive: the
  // shared comparison refuses null and undefined, and two EMPTY readings still
  // agree with each other inside it. Reported upward; guarded here meanwhile.
  assert.equal(compareReadings([], [], { conditions: "deliberate-action", all: "sum" }).agreed, true,
    "the primitive agrees on two empty readings today");
  assert.equal(compareReadings(null, null, { conditions: "deliberate-action", all: "sum" }).agreed, false,
    "and refuses two absent ones");
});

test("rows that are present but all hidden are an absent reading, not a zero", async () => {
  // The `!total` guard covers "nothing matched". This is the other way to get an
  // empty reading: the elements are in the page and none of them is on screen.
  const next = () => { const n = { ...vMakeLocator([vCell("Next")]), click: async () => {} }; n.first = () => n; return n; };
  const hidden = [vCell("50", false), vCell("70", false)];
  const page = {
    locator: (k) => (k === ".nx" ? next() : vMakeLocator(hidden)),
    getByText: () => vMakeLocator([]),
    url: () => "x", addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
  for (const all of ["sum", "count"]) {
    const entry = { sha: "s", title: "t", route: "r", status: "assert",
      expect: [{ stable: { target: { css: ".kpi .val", all }, through: [{ click: { css: ".nx" } }], label: "the figure" } }] };
    const log = console.log; console.log = () => {};
    let message = null;
    try {
      await assertFeaturesPresent(page, { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
    } catch (e) { message = e.message; } finally { console.log = log; }
    assert.ok(message, `reducer ${all}: rows that are all hidden must not read as zero`);
  }
});

test("the stable finding speaks the deliberate-action vocabulary and names its rule", async () => {
  // Pins BOTH halves at the engine, not just at the primitive: the shared
  // sentence says what happened, the clause after it says which rule it breaks.
  let turned = false;
  const next = () => { const n = { ...vMakeLocator([vCell("Next")]), click: async () => { turned = true; } }; n.first = () => n; return n; };
  const page = {
    locator: (k) => {
      if (k === ".nx") return next();
      return vMakeLocator(turned ? [vCell("31")] : [vCell("240")]);
    },
    getByText: () => vMakeLocator([]),
    url: () => "x", addStyleTag: async () => {}, screenshot: async () => {}, waitForLoadState: async () => {},
  };
  const entry = { sha: "s", title: "t", route: "r", status: "assert",
    expect: [{ stable: { target: { css: ".kpi .val", all: "sum" }, through: [{ click: { css: ".nx" } }], label: "the total" } }] };
  const log = console.log; console.log = () => {};
  let message = null;
  try {
    await assertFeaturesPresent(page, { routeName: "r", viewportLabel: "laptop", screenshotDir: "/tmp", entries: [entry] });
  } catch (e) { message = e.message; } finally { console.log = log; }
  assert.ok(message, "it reports");
  assert.match(message, /it moved when it should not/, "the shared sentence, in this caller's vocabulary");
  assert.ok(!/something the page varies/.test(message), "never the other caller's vocabulary");
  assert.match(message, /must describe the whole filter/, "and the rule it breaks, or the finding is not actionable");
});
