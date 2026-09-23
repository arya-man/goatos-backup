// Every case here is pinned in BOTH directions: the shape that must stay quiet, and the real
// failure of the same family that must report. A one-directional test is how "covered" came to
// mean "the sweep visits this page".
//
// Pure functions only: no browser, no network, no node_modules beyond node:test.
import test from "node:test";
import assert from "node:assert/strict";
import {
  blankNonMarkup,
  coverageOf,
  coverageSentence,
  gradeAssertion,
  scanInteractiveSurfaces,
  validateLedger,
} from "./interactive-surfaces.mjs";

const file = (path, text) => ({ path, text });

// ------------------------------------------------------------------ inventory
test("every surface class is found, and each one is keyed separately", () => {
  const found = scanInteractiveSurfaces([
    file("features/a/edit.tsx", '<form action={saveAction} className="cfg-form">'),
    file("features/a/modal.tsx", '<div className="vr-modal on" role="dialog">'),
    file("features/a/drawer.tsx", "<LocalOverlayDrawer title={x} />"),
    file("features/a/cell.tsx", 'className="inline-cell" contentEditable'),
    file("features/a/row.tsx", '<div className="cfg-rowacts">'),
  ]);
  assert.deepEqual(
    [...new Set(found.map((s) => s.kind))].sort(),
    ["edit-form", "inline-editor", "modal", "row-action"],
  );
  assert.equal(new Set(found.map((s) => s.key)).size, found.length, "keys collide");
});

test("two surfaces of the same kind in one file do not collapse into one key", () => {
  const found = scanInteractiveSurfaces([file("features/a/two.tsx", '<form className="x">\n<form className="x">')]);
  assert.equal(found.length, 2);
  assert.equal(new Set(found.map((s) => s.key)).size, 2);
});

test("a surface described in a comment or an import is not a surface, but the real one next to it is", () => {
  const described = scanInteractiveSurfaces([
    file("features/a/doc.tsx", '/**\n * It renders a real <form action={serverAction}> so it can drop in.\n */\nexport const A = 1;'),
    file("features/a/imp.tsx", 'import { InlineCellEditor } from "./inline-cell-editor";'),
    file("features/a/line.tsx", '// <div role="dialog"> is what this used to be\nconst x = 1;'),
  ]);
  assert.deepEqual(described, [], "a comment or an import was counted as a control");

  // ...and the same markup, actually rendered, still counts -- with the right line number.
  const real = scanInteractiveSurfaces([
    file("features/a/real.tsx", '// <div role="dialog"> is what this used to be\nimport x from "y";\n<form className="real-form">'),
  ]);
  assert.equal(real.length, 1);
  assert.equal(real[0].line, 3, "blanking a comment must not move the line numbers");
});

test("blanking keeps a URL inside a string intact", () => {
  assert.match(blankNonMarkup('const u = "https://example.test/x"; // gone'), /https:\/\/example\.test\/x/);
  assert.doesNotMatch(blankNonMarkup('const u = "x"; // gone'), /gone/);
});

test("test fixtures are not counted as product surfaces", () => {
  assert.equal(scanInteractiveSurfaces([file("features/a/a.test.tsx", "<form>")]).length, 0);
});

// ------------------------------------------------------------------ the discrimination gate
test("the four value-less operators are refused outright", () => {
  for (const operator of ["visible", "absent", "count", "url"]) {
    const graded = gradeAssertion({ subject: "the panel", operator, expected: "anything", blankScreenValue: "" });
    assert.equal(graded.ok, false, `${operator} was accepted`);
    assert.match(graded.reasons.join(" "), /cannot state an expected value/);
  }
});

test("an assertion that is still true on a blank screen is refused, and naming a real value fixes it", () => {
  // The 2026 failure verbatim: a heading regex that matches anything, including nothing.
  const blankSafe = gradeAssertion({
    subject: "the Spend share heading",
    operator: "text-matches",
    expected: ".*",
    blankScreenValue: "",
  });
  assert.equal(blankSafe.ok, false);
  assert.match(blankSafe.reasons.join(" "), /goes green on a blank screen/);

  const discriminating = gradeAssertion({
    subject: "the feeds named in the Spend share slices",
    operator: "field-set-equals",
    expected: ["Maize", "Mineral mix", "Soya"],
    blankScreenValue: [],
  });
  assert.equal(discriminating.ok, true, discriminating.reasons.join("; "));
});

test("an assertion that does not even hold for its own expected value is refused", () => {
  const graded = gradeAssertion({
    subject: "the row count",
    operator: "count-equals",
    expected: Number.NaN,
    blankScreenValue: 0,
  });
  assert.equal(graded.ok, false);
});

test("not declaring the blank-screen reading is itself a refusal", () => {
  const graded = gradeAssertion({ subject: "the title", operator: "text-equals", expected: "Access" });
  assert.equal(graded.ok, false);
  assert.match(graded.reasons.join(" "), /blank screen/);
});

// ------------------------------------------------------------------ the ledger
const SURFACES = scanInteractiveSurfaces([
  file("features/a/modal.tsx", '<div className="vr-modal" role="dialog">'),
]);
const KEY = SURFACES[0].key;
const goodAssertion = {
  subject: "the controls on the Access panel",
  operator: "field-set-equals",
  expected: ["Close", "Designation", "Scope"],
  blankScreenValue: [],
};

test("a surface the ledger says nothing about fails the gate", () => {
  const { problems } = validateLedger(SURFACES, { entries: [] });
  assert.equal(problems.length, 1);
  assert.match(problems[0], /the ledger makes no decision about it/);
});

test("a ledger entry whose surface has left the tree fails the gate", () => {
  const { problems } = validateLedger([], { entries: [{ key: "gone", status: "not-checked", notCheckedReason: "x" }] });
  assert.match(problems.join(" "), /no longer exists in the source/);
});

test("not-checked is allowed only with a reason, and counts zero either way", () => {
  const withReason = validateLedger(SURFACES, {
    entries: [{ key: KEY, status: "not-checked", notCheckedReason: "labels come from data" }],
  });
  assert.deepEqual(withReason.problems, []);
  assert.equal(withReason.coverage.covered, 0);

  const silent = validateLedger(SURFACES, { entries: [{ key: KEY, status: "not-checked" }] });
  assert.match(silent.problems.join(" "), /silence is not a verdict/);
});

test("covered without a run receipt is not coverage; with one it is", () => {
  const base = { key: KEY, status: "covered", viewports: ["1440", "390"], assertions: [goodAssertion] };
  const noReceipt = validateLedger(SURFACES, { entries: [base] });
  assert.match(noReceipt.problems.join(" "), /names no run receipt/);
  assert.equal(noReceipt.coverage.covered, 0);

  const withReceipt = validateLedger(SURFACES, {
    entries: [{ ...base, receipt: { runId: "sweep-2026-09-24", path: "scratchpad/sweep/receipt.json" } }],
  });
  assert.deepEqual(withReceipt.problems, []);
  assert.equal(withReceipt.coverage.covered, 1);
});

test("a stated assertion never counts toward coverage, however good it is", () => {
  const { problems, coverage } = validateLedger(SURFACES, {
    entries: [
      {
        key: KEY,
        status: "stated-not-executed",
        viewports: ["1440", "390"],
        notExecutedReason: "the browser lane is disabled",
        assertions: [goodAssertion],
      },
    ],
  });
  assert.deepEqual(problems, []);
  assert.equal(coverage.covered, 0);
  assert.equal(coverage.stated, 1);
});

test("one viewport is not both viewports unless the gap is named", () => {
  const entry = {
    key: KEY,
    status: "stated-not-executed",
    notExecutedReason: "lane disabled",
    assertions: [goodAssertion],
    viewports: ["1440"],
  };
  assert.match(validateLedger(SURFACES, { entries: [entry] }).problems.join(" "), /never runs at 390px/);
  const named = { ...entry, viewportGapReason: "this sheet only renders at <=760px" };
  assert.deepEqual(validateLedger(SURFACES, { entries: [named] }).problems, []);
});

test("a blank-screen-proof assertion drags its whole entry out of coverage", () => {
  const { problems, coverage } = validateLedger(SURFACES, {
    entries: [
      {
        key: KEY,
        status: "covered",
        viewports: ["1440", "390"],
        receipt: { runId: "r", path: "p" },
        assertions: [{ subject: "the heading", operator: "text-matches", expected: ".*", blankScreenValue: "" }],
      },
    ],
  });
  assert.match(problems.join(" "), /goes green on a blank screen/);
  assert.equal(coverage.covered, 0);
});

test("the coverage sentence reports proven and stated as separate numbers", () => {
  const sentence = coverageSentence(coverageOf(SURFACES, [], [KEY]));
  assert.match(sentence, /PROVEN[^]*0\/1 \(0\.0%\)/);
  assert.match(sentence, /never executed: 1\/1/);
});

test("an unknown status is refused rather than ignored", () => {
  const { problems } = validateLedger(SURFACES, { entries: [{ key: KEY, status: "smoke-only" }] });
  assert.match(problems.join(" "), /use "covered", "stated-not-executed" or "not-checked"/);
});
