import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  coverageGaps,
  elementKey,
  findFailureString,
  observedChange,
  planActivation,
  renderMarkdown,
  requiresBeyondValue,
  summarize,
} from "./tasks-click-matrix.mjs";

const el = (over = {}) => ({
  tag: "a",
  type: "",
  role: "",
  draggable: "",
  disabled: false,
  hidden: false,
  formValid: null,
  className: "",
  name: "x",
  width: 40,
  height: 40,
  ...over,
});

test("the dead +N chip: no observable change is exactly an empty diff", () => {
  const before = { href: "/tasks", expanded: "", popovers: 0, textHash: 1, requests: 3 };
  assert.deepEqual(observedChange(before, { ...before }), []);
  assert.deepEqual(observedChange(before, { ...before, popovers: 1 }), ["popovers"]);
});

test("typing into the toolbar search must show more than its own value", () => {
  const search = el({ tag: "input", type: "search" });
  assert.equal(requiresBeyondValue(search), true);
  assert.equal(requiresBeyondValue(el({ tag: "input", type: "search", role: "combobox" })), false);
  assert.equal(requiresBeyondValue(el({ tag: "textarea" })), false);
  const before = { inputs: "", href: "/tasks" };
  assert.deepEqual(observedChange(before, { inputs: "fence", href: "/tasks" }, ["inputs"]), []);
  assert.deepEqual(observedChange(before, { inputs: "fence", href: "/tasks?t_q=fence" }, ["inputs"]), ["href"]);
});

test("planActivation: drag is asserted absent at phone width and exercised at desktop", () => {
  const card = el({ draggable: "true", className: "ltb-card" });
  assert.equal(planActivation(card, 390).how, "drag-absent");
  assert.equal(planActivation(card, 1440).how, "drag");
  assert.equal(planActivation(card, 760).how, "drag-absent");
  assert.equal(planActivation(card, 761).how, "drag");
});

test("planActivation: a submit on a VALID form is guarded, on an INVALID form it is clicked", () => {
  assert.equal(planActivation(el({ tag: "button", type: "submit", formValid: true }), 1440).how, "write-guard");
  assert.equal(planActivation(el({ tag: "button", type: "submit", formValid: false }), 1440).how, "submit-invalid");
  assert.equal(planActivation(el({ tag: "button", type: "button", formValid: true }), 1440).how, "click");
});

test("planActivation: hidden and disabled come first, form fields by type", () => {
  assert.equal(planActivation(el({ hidden: true, disabled: true }), 1440).how, "hidden");
  assert.equal(planActivation(el({ disabled: true }), 1440).how, "disabled");
  assert.equal(planActivation(el({ tag: "select" }), 1440).how, "select");
  assert.equal(planActivation(el({ tag: "input", type: "date" }), 1440).how, "date");
  assert.equal(planActivation(el({ tag: "input", type: "checkbox" }), 1440).how, "toggle");
  assert.equal(planActivation(el({ tag: "input", type: "file" }), 1440).how, "file");
  assert.equal(planActivation(el({ tag: "input", type: "search" }), 1440).how, "type");
  assert.equal(planActivation(el({ tag: "textarea" }), 1440).how, "type");
  assert.equal(planActivation(el({ tag: "li", role: "option" }), 1440).how, "click");
});

test("elementKey ignores state classes and row numbers so the same control matches across stages", () => {
  const a = elementKey(el({ tag: "a", className: "ltb-card is-selected", name: "Open task #10: Feed" }));
  const b = elementKey(el({ tag: "a", className: "ltb-card", name: "Open task #375: Feed" }));
  assert.equal(a, b);
  assert.notEqual(a, elementKey(el({ tag: "a", className: "ltb-colmore", name: "See every task" })));
  assert.equal(elementKey(el({ className: "btn sm p" })), elementKey(el({ className: "btn sm" })));
  assert.equal(elementKey(el({ tag: "button", name: "Deadline" })), elementKey(el({ tag: "button", name: "Deadlineany" })));
  assert.notEqual(elementKey(el({ tag: "button", name: "Deadline" })), elementKey(el({ tag: "button", name: "Raised" })));
});

test("coverageGaps: hidden-here-activated-there passes; hidden everywhere fails", () => {
  const results = [
    { key: "button:Close filters", how: "hidden", status: "pass", viewport: "1440x900", element: "Close filters" },
    { key: "button:Close filters", how: "click", status: "pass", viewport: "390x844", element: "Close filters" },
    { key: "span:+5", how: "hidden", status: "pass", viewport: "1440x900", element: "+5" },
    { key: "a:Prev", how: "hidden", status: "pass", viewport: "1440x900", element: "Prev" },
    { key: "a:Prev", how: "disabled", status: "pass", viewport: "390x844", element: "Prev" },
  ];
  const gaps = coverageGaps(results).map((entry) => entry.key);
  assert.deepEqual(gaps, ["span:+5", "a:Prev"]);
});

test("planActivation: behind a scrim is asserted covered only on an overlay stage", () => {
  const card = el({ covered: true });
  assert.equal(planActivation(card, 1440, true).how, "covered");
  assert.equal(planActivation(card, 1440, false).how, "click");
  assert.equal(planActivation(el({ tag: "input", type: "datetime-local" }), 1440).how, "date");
});

test("planActivation: a page-rendered overlay (the ?task= drawer) covers without a stage opener", () => {
  const chip = el({ covered: true, overlay: true });
  assert.equal(planActivation(chip, 1440, false).how, "covered");
  assert.equal(planActivation(el({ covered: true, overlay: false }), 1440, false).how, "click");
  // The scrim itself is the overlay's own control and is never "covered".
  assert.equal(planActivation(el({ tag: "button", covered: false, overlay: true, className: "scrim on ltd-scrim" }), 1440, false).how, "click");
});

test("planActivation: the already-selected tab is a no-op by design, not a dead control", () => {
  assert.equal(planActivation(el({ tag: "button", role: "tab", selected: true }), 1440).how, "current");
  assert.equal(planActivation(el({ tag: "button", role: "tab", selected: false }), 1440).how, "click");
  // A selected OPTION is still clicked: in the people picker a second click clears the pick.
  assert.equal(planActivation(el({ tag: "button", role: "option", selected: true }), 1440).how, "click");
});

test("coverageGaps: a `current` tab does not count as activated on its own", () => {
  const results = [
    { key: "button[tab]:All", how: "hidden", status: "pass", viewport: "390x844" },
    { key: "button[tab]:All", how: "current", status: "pass", viewport: "1440x900" },
  ];
  assert.deepEqual(coverageGaps(results).map((e) => e.key), ["button[tab]:All"]);
});

test("coverageGaps: covered and sampled do not count as activated", () => {
  const results = [
    { key: "a:x", how: "hidden", status: "pass", viewport: "390x844" },
    { key: "a:x", how: "covered", status: "pass", viewport: "1440x900" },
    { key: "a:y", how: "hidden", status: "pass", viewport: "390x844" },
    { key: "a:y", how: "sampled", status: "pass", viewport: "1440x900" },
  ];
  assert.deepEqual(coverageGaps(results).map((e) => e.key), ["a:x", "a:y"]);
});

test("failure strings are found anywhere in the body text", () => {
  assert.equal(findFailureString("ok page"), null);
  assert.equal(findFailureString("... This screen failed to render ..."), "This screen failed to render");
  assert.equal(findFailureString("x missing copy key y"), "missing copy key");
  assert.equal(findFailureString("contract unavailable"), "contract unavailable");
});

test("summarize counts per viewport and renderMarkdown carries the matrix columns", () => {
  const results = [
    { viewport: "1440x900", stage: "board", element: "a", how: "click", status: "pass", detail: "changed: href" },
    { viewport: "1440x900", stage: "board", element: "b|c", how: "click", status: "fail", detail: "nothing" },
    { viewport: "390x844", stage: "board", element: "a", how: "click", status: "pass", detail: "" },
  ];
  assert.deepEqual(summarize(results), {
    "1440x900": { pass: 1, fail: 1, total: 2 },
    "390x844": { pass: 1, fail: 0, total: 1 },
  });
  const md = renderMarkdown({ runId: "R", appBaseUrl: "http://x", results });
  assert.match(md, /\| Status \| Viewport \| Stage \| Element \| Action \| Observed \|/);
  assert.match(md, /\| fail \| 1440x900 \| board \| b\\\|c \| click \| nothing \|/);
  assert.match(md, /1440x900: 1 pass \/ 1 fail \(2\)/);
});
