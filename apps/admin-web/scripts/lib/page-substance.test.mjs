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

test("a settings form is content, a filter bar over an empty table is not", () => {
  assert.equal(assessSubstance(snap({ formFields: 6, headings: 1, controls: 8 })).verdict, VERDICTS.SUBSTANTIAL);
  // controls (filters outside a <form>) never count, so the lost-rows table stays blank.
  assert.equal(assessSubstance(snap({ controls: 9, headings: 1 })).verdict, VERDICTS.BLANK);
  assert.match(collectSubstance.toString(), /form input/, "only fields inside a form are counted");
});

test("the route sweep consults the gate before its detectors and fails a blank page", () => {
  const runner = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");
  assert.match(runner, /import \{[^}]*collectSubstance[^}]*\} from "\.\/lib\/page-substance\.mjs"/);
  assert.match(runner, /page\.evaluate\(collectSubstance\)/, "the sweep must take the snapshot");
  assert.match(runner, /const substanceGate = gateContentCheck\(substanceAssessment\);/);
  assert.match(runner, /if \(substanceGate\.finding\) throw new Error/, "a blank page is a route failure");
  assert.match(runner, /route_not_judged=/, "empty-state / unreadable pages are named, never silently passed");
  const gateAt = runner.indexOf("const substanceGate =");
  const firstCheck = runner.indexOf("assertRegressionPatterns(page");
  assert.ok(gateAt > 0 && firstCheck > 0 && gateAt < firstCheck, "the gate must run before the first detector");
});

test("stat tiles count as content and a named *-empty-state reads as an empty state", () => {
  // Farm value is a page of .kpi tiles (no table, no .card); Tasks' filtered-out view says so in
  // .lt-empty-state. Both were reported as blank pages by the production sweep.
  const source = collectSubstance.toString();
  assert.match(source, /\.kpi/, "the stat tile is a content unit");
  assert.match(source, /-empty-state/, "a page-named empty state is an empty state");
});
