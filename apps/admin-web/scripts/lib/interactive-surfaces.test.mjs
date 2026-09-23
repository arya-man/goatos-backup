// Every case here is pinned in BOTH directions: the shape that must stay quiet, and the real
// failure of the same family that must report. A one-directional test is how "covered" came to
// mean "the sweep visits this page".
//
// Pure functions only: no browser, no network, no node_modules beyond node:test.
import test from "node:test";
import assert from "node:assert/strict";
import {
  blankNonMarkup,
  blankValueFor,
  deriveExpected,
  coverageOf,
  coverageSentence,
  gradeAssertion,
  routeOfPageFile,
  routesOwningFiles,
  rootLabels,
  scanInteractiveSurfaces,
  validateLedger,
  VALUE_OPERATORS,
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

test("the blank reading is derived, so omitting it is fine and mis-stating it is not", () => {
  // Superseded the old "must declare it" rule after judge finding B1: an author who may choose
  // BOTH sides of the comparison can always make it look discriminating, so the gate picks one.
  assert.equal(gradeAssertion({ subject: "the title", operator: "text-equals", expected: "Access" }).ok, true);
  const mis = gradeAssertion({ subject: "the title", operator: "text-equals", expected: "Access", blankScreenValue: "Access-ish" });
  assert.equal(mis.ok, false);
  assert.match(mis.reasons.join(" "), /does not get to pick this side/);
});

// ------------------------------------------------------------------ the ledger
const SURFACES = scanInteractiveSurfaces([
  file("features/a/modal.tsx", '<div className="vr-modal" role="dialog">'),
]);
const KEY = SURFACES[0].key;
const ROUTES = ["/people"];
const PRINCIPAL = "the verifier signed in with verification.review";
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
  const { problems } = validateLedger([], { entries: [{ routes: ROUTES, key: "gone", status: "not-checked", notCheckedReason: "x" }] });
  assert.match(problems.join(" "), /no longer exists in the source/);
});

test("not-checked is allowed only with a reason, and counts zero either way", () => {
  const withReason = validateLedger(SURFACES, {
    entries: [{ routes: ROUTES, key: KEY, status: "not-checked", notCheckedReason: "labels come from data" }],
  });
  assert.deepEqual(withReason.problems, []);
  assert.equal(withReason.coverage.covered, 0);

  const silent = validateLedger(SURFACES, { entries: [{ routes: ROUTES, key: KEY, status: "not-checked" }] });
  assert.match(silent.problems.join(" "), /silence is not a verdict/);
});

test("covered without a run receipt is not coverage; with one it is", () => {
  const base = { routes: ROUTES, key: KEY, principal: PRINCIPAL, status: "covered", viewports: ["1440", "390"], assertions: [goodAssertion] };
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
      { routes: ROUTES, key: KEY,
        principal: PRINCIPAL, status: "stated-not-executed",
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
  const entry = { routes: ROUTES, key: KEY,
    principal: PRINCIPAL, status: "stated-not-executed",
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
      { routes: ROUTES, key: KEY,
        principal: PRINCIPAL, status: "covered",
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
  const { problems } = validateLedger(SURFACES, { entries: [{ routes: ROUTES, key: KEY, status: "smoke-only" }] });
  assert.match(problems.join(" "), /use "covered", "stated-not-executed" or "not-checked"/);
});

// ------------------------------------------------------------------ which routes own a surface
test("a route owns every surface its page reaches, through barrels and through relative paths", () => {
  const owners = routesOwningFiles([
    file("app/(admin)/people/page.tsx", 'import { PeoplePage } from "@/features/people";'),
    file("features/people/index.ts", 'export { PeoplePage } from "./people-board";'),
    file("features/people/people-board.tsx", 'import { PersonAccessModal } from "./person-access-modal";\n<form className="card">'),
    file("features/people/person-access-modal.tsx", '<div role="dialog">'),
    file("features/unreachable/orphan.tsx", '<div role="dialog">'),
  ]);
  assert.deepEqual(owners.get("features/people/person-access-modal.tsx"), ["/people"]);
  assert.equal(owners.get("features/unreachable/orphan.tsx"), undefined, "an orphan claimed a route");
});

test("dropping the import drops the route, so a stale claim cannot survive", () => {
  const withImport = routesOwningFiles([
    file("app/(admin)/x/page.tsx", 'import { A } from "./a";'),
    file("app/(admin)/x/a.tsx", '<form className="f">'),
  ]);
  assert.deepEqual(withImport.get("app/(admin)/x/a.tsx"), ["/x"]);
  const without = routesOwningFiles([
    file("app/(admin)/x/page.tsx", "const A = () => null;"),
    file("app/(admin)/x/a.tsx", '<form className="f">'),
  ]);
  assert.equal(without.get("app/(admin)/x/a.tsx"), undefined);
});

test("a route group is not part of the route, and a dynamic segment is a placeholder", () => {
  assert.equal(routeOfPageFile("app/(admin)/weighing/sops/page.tsx"), "/weighing/sops");
  assert.equal(routeOfPageFile("app/(admin)/vaccination/sheds/[shed_id]/page.tsx"), "/vaccination/sheds/placeholder");
});

test("a surface with no owning route and no reason fails the gate", () => {
  const entry = { routes: ROUTES, key: KEY,
    principal: PRINCIPAL, status: "stated-not-executed",
    viewports: ["1440", "390"],
    notExecutedReason: "lane disabled",
    assertions: [goodAssertion],
    routes: [],
  };
  assert.match(validateLedger(SURFACES, { entries: [entry] }).problems.join(" "), /names no route that renders it/);
  const named = { ...entry, routeGapReason: "nothing in the app imports this file" };
  assert.deepEqual(validateLedger(SURFACES, { entries: [named] }).problems, []);
});

// ------------------------------------------------------------------ provenance (judge finding B1)
const SOURCE = new Map([
  ["features/a/panel.tsx", '<div role="dialog">\n<button aria-label="Close">X</button>\n<label>Designation</label>'],
]);
const readSource = (p) => SOURCE.get(p);
const PANEL_PROVENANCE = { kind: "source", path: "features/a/panel.tsx", line: 1, extractor: "labels-near" };

test("an expected value nobody measured is refused, however well-formed", () => {
  // The judge's own example, verbatim.
  const fabricated = { subject: "a thing nobody looked at", operator: "number-equals", expected: 7, blankScreenValue: 0 };
  assert.equal(gradeAssertion(fabricated).ok, true, "grading without a reader is the ungated path");
  const graded = gradeAssertion(fabricated, readSource);
  assert.equal(graded.ok, false);
  assert.match(graded.reasons.join(" "), /carries no provenance/);
});

test("provenance that does not re-derive the stated value is refused", () => {
  const graded = gradeAssertion(
    { subject: "the panel", operator: "field-set-equals", expected: ["Something else"], blankScreenValue: [], provenance: PANEL_PROVENANCE },
    readSource,
  );
  assert.equal(graded.ok, false);
  assert.match(graded.reasons.join(" "), /the source does not produce/);
});

test("a value re-derived from the source it names is accepted", () => {
  const expected = deriveExpected(PANEL_PROVENANCE, readSource).value;
  assert.ok(expected.includes("Close") && expected.includes("Designation"), `derived ${JSON.stringify(expected)}`);
  const graded = gradeAssertion(
    { subject: "the panel's controls", operator: "field-set-equals", expected, blankScreenValue: [], provenance: PANEL_PROVENANCE },
    readSource,
  );
  assert.equal(graded.ok, true, graded.reasons.join("; "));
});

test("the author does not get to pick the blank side of the comparison", () => {
  const expected = deriveExpected(PANEL_PROVENANCE, readSource).value;
  const tilted = { subject: "the panel", operator: "field-set-equals", expected, blankScreenValue: ["Close"], provenance: PANEL_PROVENANCE };
  assert.match(gradeAssertion(tilted, readSource).reasons.join(" "), /does not get to pick this side/);
  assert.deepEqual(blankValueFor("field-set-equals"), []);
  assert.equal(blankValueFor("count-equals"), 0);
  assert.equal(blankValueFor("text-equals"), "");
  assert.equal(blankValueFor("enabled-equals"), false);
});

test("provenance pointing at a file that is not in the app, or at an extractor nobody runs, is refused", () => {
  assert.match(
    deriveExpected({ kind: "source", path: "features/a/gone.tsx", line: 1, extractor: "labels-near" }, readSource).error,
    /not a file in the app/,
  );
  assert.match(deriveExpected({ kind: "source", path: "features/a/panel.tsx", line: 1, extractor: "vibes" }, readSource).error, /not one this gate can run/);
  assert.match(deriveExpected({ kind: "trust me" }, readSource).error, /cannot be re-derived/);
});

// ---------------------------------------------- role-agnostic pages (judge finding B-1)
test("an exact set scraped from a component is wrong for a narrower principal; the own-label superset is not", () => {
  const couldRender = ["Close", "Delete person", "Designation", "Scope"];
  const narrowerPrincipalSees = ["Close", "Designation", "Scope"];
  // This is the defect: the page is correct, the person simply cannot delete.
  assert.equal(VALUE_OPERATORS["field-set-equals"](narrowerPrincipalSees, couldRender), false);
  assert.equal(VALUE_OPERATORS["field-set-contains-all"](narrowerPrincipalSees, ["Close"]), true);
  // ...and it still cannot pass on a blank screen, which is the whole point of the gate.
  assert.equal(VALUE_OPERATORS["field-set-contains-all"]([], ["Close"]), false);
  assert.equal(VALUE_OPERATORS["field-set-contains-all"](["Close"], []), false, "an empty expectation asserts nothing");
});

test("an expectation that does not say whose screen it describes is refused", () => {
  const entry = {
    routes: ROUTES,
    key: KEY,
    status: "stated-not-executed",
    viewports: ["1440", "390"],
    notExecutedReason: "lane disabled",
    assertions: [goodAssertion],
  };
  assert.match(validateLedger(SURFACES, { entries: [entry] }).problems.join(" "), /without saying WHOSE screen/);
  const named = { ...entry, principal: "any principal whose contract lets them open it" };
  assert.deepEqual(validateLedger(SURFACES, { entries: [named] }).problems, []);
});

test("only the surface's own opening tag is read, never a child control or a wire field name", () => {
  const text = [
    '<aside className="drawer" role="dialog" aria-label="Access" data-testid="access-drawer">',
    '  <input name="row_version" />',
    '  {canDelete ? <button aria-label="Delete person">x</button> : null}',
    "</aside>",
  ].join("\n");
  const labels = rootLabels(text, 0);
  assert.deepEqual(labels, ["Access"]);
  assert.ok(!labels.includes("Delete person"), "a permission-gated child leaked into the expectation");
  assert.ok(!labels.includes("row_version"), "a wire field name leaked into what a person reads");
  assert.ok(!labels.includes("access-drawer"), "a test id leaked into what a person reads");
});

test("a copy key on the surface's own label is read, because an unresolved contract renders the key", () => {
  assert.deepEqual(rootLabels('<div role="dialog" aria-label={t("filter.drawer.title")}>', 0), ["filter.drawer.title"]);
});

// ---------------------------------------------- router-convention files and unfollowable imports
test("the router's own files own their route, though no import names them", () => {
  const owners = routesOwningFiles([
    file("app/(admin)/people/page.tsx", "export default function P() { return null; }"),
    file("app/(admin)/people/loading.tsx", '<form className="card" aria-busy="true">'),
    file("app/(admin)/people/error.tsx", '<div role="alert">'),
  ]);
  // Next.js renders these while the page streams or when it throws. They are the empty and error
  // states, not dead code, and before this they read as "no page imports this file".
  assert.deepEqual(owners.get("app/(admin)/people/loading.tsx"), ["/people"]);
  assert.deepEqual(owners.get("app/(admin)/people/error.tsx"), ["/people"]);
});

test("a dynamic import the graph cannot follow is reported, not silently treated as no edge", () => {
  const quiet = routesOwningFiles([file("app/(admin)/x/page.tsx", 'const m = await import("./a");')]);
  assert.deepEqual(quiet.unresolvableImportSites, []);
  const loud = routesOwningFiles([file("app/(admin)/x/page.tsx", "const m = await import(whichever);")]);
  assert.deepEqual(loud.unresolvableImportSites, ["app/(admin)/x/page.tsx"]);
});

test("a literal dynamic import IS followed, so a lazily loaded panel still names its route", () => {
  const owners = routesOwningFiles([
    file("app/(admin)/n/page.tsx", 'const P = lazy(() => import("./panel"));'),
    file("app/(admin)/n/panel.tsx", '<div role="dialog" aria-label="Notifications">'),
  ]);
  assert.deepEqual(owners.get("app/(admin)/n/panel.tsx"), ["/n"]);
});
