import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { WRITE_WORDS, assertFeaturesPresent, isValueExpect, loadFeatureAssertions, reloadCoverage } from "./feature-assertions.mjs";
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
      const named = expect.visible ?? expect.absent ?? expect.count ?? expect.url ?? expect.equals ?? expect.compare;
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
