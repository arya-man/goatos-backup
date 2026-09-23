import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { VERDICTS, assessSubstance, collectSubstance, contentUnits, gateContentCheck } from "./page-substance.mjs";

// The seven pages every surviving `covered` entry was silent on. Written as
// snapshots so the gate is proved with no browser anywhere in the test.
const snap = (o) => ({ collected: true, rows: 0, cards: 0, cells: 0, chartMarks: 0, controls: 0, headings: 0, figures: 0, textLength: 0, emptyState: [], ...o });

const BLANK_PAGES = {
  "a completely blank page": snap({}),
  "only a heading left": snap({ headings: 1, textLength: 18, controls: 4 }),
  "empty cards with no data": snap({ cards: 0, headings: 2, controls: 6, textLength: 60 }),
  "the chart card removed entirely": snap({ headings: 1, controls: 3, textLength: 40 }),
  "a table that lost every row": snap({ cells: 0, rows: 0, headings: 1, controls: 9, textLength: 120 }),
};

test("every page the old checks were silent on is now refused a verdict", () => {
  for (const [name, page] of Object.entries(BLANK_PAGES)) {
    const assessment = assessSubstance(page);
    assert.equal(assessment.verdict, VERDICTS.BLANK, `${name} must not read as a page that was judged`);
    const gate = gateContentCheck(assessment);
    assert.equal(gate.judge, false, `${name} must not be judged`);
    assert.ok(gate.finding && gate.finding.length > 40, `${name} must carry a sentence saying why`);
    assert.ok(!("notAttempted" in gate), `${name} is a finding about the page, not a shrug`);
  }
});

test("chrome alone is never content, however much of it there is", () => {
  // The exact shape that fooled the old sweep: a full shell, no data.
  const shell = snap({ headings: 3, controls: 40, textLength: 900, figures: 12 });
  assert.equal(contentUnits(shell), 0, "nav, buttons and headings are chrome");
  assert.equal(assessSubstance(shell).verdict, VERDICTS.BLANK);
});

test("a page that says it has nothing is not attempted, and never a pass", () => {
  const page = snap({ headings: 1, controls: 5, emptyState: ["No animals have been weighed in this window yet"] });
  const assessment = assessSubstance(page);
  assert.equal(assessment.verdict, VERDICTS.EMPTY_STATE);
  const gate = gateContentCheck(assessment);
  assert.equal(gate.judge, false, "nothing was judged");
  assert.ok(gate.notAttempted.includes("never put to the test"), "and it says so in words");
  assert.ok(!gate.finding, "a correct empty state is not an accusation");
});

test("a real page is judged, and the gate does not fire on it", () => {
  // NO FALSE POSITIVES: each of these is a correct page and must pass the gate.
  const real = {
    "a table of animals": snap({ rows: 20, cells: 120, headings: 2, controls: 30, figures: 300, textLength: 2000 }),
    "a page of cards": snap({ cards: 8, headings: 2, controls: 12, figures: 40, textLength: 400 }),
    "a chart-only analytics tab": snap({ chartMarks: 46, headings: 1, controls: 8, figures: 22, textLength: 180 }),
    "a small three-row table": snap({ rows: 3, cells: 9, headings: 1, controls: 4, figures: 12, textLength: 90 }),
    "one card and two chart marks": snap({ cards: 1, chartMarks: 2, headings: 1 }),
  };
  for (const [name, page] of Object.entries(real)) {
    const assessment = assessSubstance(page);
    assert.equal(assessment.verdict, VERDICTS.SUBSTANTIAL, `${name} is a real page and must be judged`);
    assert.equal(gateContentCheck(assessment).judge, true, name);
  }
});

test("an empty state beside real rows does not suppress the judgement", () => {
  // A page can carry a "no results for this filter" note in one card while the
  // rest of it is drawn. That is a substantial page.
  const page = snap({ rows: 14, cells: 70, emptyState: ["No results for this filter"] });
  assert.equal(assessSubstance(page).verdict, VERDICTS.SUBSTANTIAL);
});

test("the gate is an emptiness gate and says what it cannot see", () => {
  // STATED LIMIT, asserted so nobody reads the gate as more than it is: a page
  // whose every figure is WRONG is substantial, and this gate passes it. Two of
  // the seven blank-page scenarios are of that kind and need an expected value,
  // not an emptiness check.
  const everyNumberWrong = snap({ rows: 20, cells: 120, figures: 300, textLength: 2000 });
  const absurdBars = snap({ chartMarks: 46, figures: 22, textLength: 180 });
  for (const page of [everyNumberWrong, absurdBars]) {
    assert.equal(assessSubstance(page).verdict, VERDICTS.SUBSTANTIAL,
      "a page full of wrong numbers is still a page that drew something");
    assert.equal(gateContentCheck(assessSubstance(page)).judge, true);
  }
});

test("the threshold is a shape, not a knob to turn when something fires", () => {
  // Two content units is below the floor and must stay refused; raising the
  // floor to silence a finding would put real pages back into BLANK.
  assert.equal(assessSubstance(snap({ rows: 2 })).verdict, VERDICTS.BLANK);
  assert.equal(assessSubstance(snap({ rows: 3 })).verdict, VERDICTS.SUBSTANTIAL);
  assert.equal(assessSubstance(snap({ rows: 3 }), { minContentUnits: 10 }).verdict, VERDICTS.BLANK,
    "and the floor is explicit, so a caller that moves it is visible in the diff");
});

// ---------------------------------------------------------------- the real capture decision
//
// The gate is only worth having if the sweep actually consults it, and a branch
// buried inside a loop that needs a live site cannot be held to account (§8).
// judgeLandedPage IS that branch, so these run against the real one.
import { judgeLandedPage } from "./flicker-capture.mjs";

test("a page that loaded and drew nothing is never filmed or called clean", () => {
  const verdict = judgeLandedPage({ landedOn: "/tasks", snapshot: snap({ headings: 1, controls: 2, textLength: 22 }) });
  assert.equal(verdict.film, false, "filming it would produce identical frames and a clean verdict");
  assert.equal(verdict.blankPage, true, "and a page that drew nothing is a finding about the page");
  assert.ok(verdict.parked.length > 40, "with a sentence");
});

test("a page that says it is empty is not filmed, and is not an accusation either", () => {
  const verdict = judgeLandedPage({ landedOn: "/tasks", snapshot: snap({ headings: 1, emptyState: ["Nothing to show yet"] }) });
  assert.equal(verdict.film, false);
  assert.ok(!verdict.blankPage, "a correct empty state is not a finding");
  assert.match(verdict.parked, /never put to the test/);
});

test("a sign-in redirect is still parked for its own reason", () => {
  const verdict = judgeLandedPage({ landedOn: "/login", snapshot: snap({ rows: 20, cells: 80 }) });
  assert.equal(verdict.film, false);
  assert.match(verdict.parked, /signed-in session/);
});

test("a real page is filmed", () => {
  const verdict = judgeLandedPage({ landedOn: "/tasks", snapshot: snap({ rows: 18, cells: 90, figures: 200 }) });
  assert.equal(verdict.film, true, "NO FALSE POSITIVES: a page with content must still be swept");
  assert.equal(verdict.substance, VERDICTS.SUBSTANTIAL);
});

test("a snapshot that could not be taken is NOT CHECKED, never an accusation", () => {
  // This test used to assert `blankPage: true` for a failed snapshot, and that
  // was the wrong direction: page.evaluate failing — a blocked script, a
  // detached frame, a navigation mid-evaluate — read exactly like a page that
  // drew nothing, and that is a FINDING against the page. A correct screen
  // would have been reported broken because the harness stumbled.
  for (const snapshot of [null, undefined, {}, { rows: 5 }]) {
    const verdict = judgeLandedPage({ landedOn: "/tasks", snapshot });
    assert.equal(verdict.film, false, "it is still not filmed");
    assert.ok(!verdict.blankPage, `a snapshot with no proof it ran must not accuse the page: ${JSON.stringify(snapshot)}`);
    assert.match(verdict.parked, /could not be run here/);
  }
  // And a snapshot that DID run and found nothing is still a finding.
  const drewNothing = judgeLandedPage({ landedOn: "/tasks", snapshot: snap({ headings: 1, controls: 2 }) });
  assert.equal(drewNothing.blankPage, true, "a page that demonstrably drew nothing is still reported");
});

test("only a snapshot that proves it ran is judged", () => {
  assert.equal(assessSubstance(null).verdict, VERDICTS.UNREADABLE);
  assert.equal(assessSubstance({}).verdict, VERDICTS.UNREADABLE);
  // A half-filled object is not proof either: the marker is what the collector
  // sets, so a stray object cannot pass itself off as a reading.
  assert.equal(assessSubstance({ rows: 20, cells: 80 }).verdict, VERDICTS.UNREADABLE);
  assert.equal(assessSubstance(snap({ rows: 20, cells: 80 })).verdict, VERDICTS.SUBSTANTIAL);
  assert.equal(gateContentCheck(assessSubstance(null)).judge, false);
  assert.ok(gateContentCheck(assessSubstance(null)).notAttempted, "not checked");
  assert.ok(!gateContentCheck(assessSubstance(null)).finding, "never a finding");
});

test("the route sweep itself refuses to judge a page that drew nothing", () => {
  // §8 again: the gate is only worth having if the sweep consults it, and the
  // consultation sits in a loop that needs a live site. Removing it left every
  // other test green, so it is pinned here at the source.
  const runner = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");
  assert.match(runner, /import \{[^}]*collectSubstance[^}]*\} from "\.\/lib\/page-substance\.mjs"/,
    "the sweep must import the gate");
  assert.match(runner, /const substanceGate = gateContentCheck\(substanceAssessment\);/, "and consult it");
  assert.match(runner, /if \(!substanceGate\.judge\) \{/,
    "and act on it — every per-route check below is a defect finder that goes silent on a blank page");
  assert.match(runner, /route_not_judged=/, "and say which pages it refused to judge");
  // The gate must sit BEFORE the checks it protects, or it protects nothing.
  const gateAt = runner.indexOf("const substanceGate =");
  const firstCheck = runner.indexOf("assertRegressionPatterns(page");
  assert.ok(gateAt > 0 && firstCheck > 0 && gateAt < firstCheck,
    "the gate must run before the first detector, not after it");
});

test("the collector proves it ran, and every caller relies on that rather than on its own catch", () => {
  // The marker is the whole defence: without it a snapshot that never happened
  // and a page that drew nothing are the same value. collectSubstance runs
  // inside the browser, so it is pinned at its source.
  const source = collectSubstance.toString();
  assert.match(source, /collected:\s*true/, "the collector must stamp proof that it ran");

  // Belt and braces on the callers: whatever they hand over on failure — null,
  // undefined or {} — the marker check refuses it. That is deliberate, so a
  // caller changing its catch value cannot reopen the hole.
  for (const handedOver of [null, undefined, {}]) {
    assert.equal(assessSubstance(handedOver).verdict, VERDICTS.UNREADABLE, `${JSON.stringify(handedOver)} must be unreadable`);
  }
  const capture = readFileSync(new URL("./flicker-capture.mjs", import.meta.url), "utf8");
  const sweep = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");
  for (const [name, text] of [["the capture path", capture], ["the route sweep", sweep]]) {
    assert.match(text, /page\.evaluate\(collectSubstance\)/, `${name} must take the snapshot`);
  }
});
