import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Pins the fix for the STG incident (2026-08-12): the CEO's oversight filters (module chips,
// capture-date range picker) on /verify rendered for every role that can open the page, including
// RoleVerifier, because /verify is a single role-agnostic component. See
// docs/decisions/role-scoped-ui-is-capability-gated.md.
//
// These are SOURCE-SHAPE assertions, not a rendered-DOM test: the renderer must gate the
// oversight-only chrome on the backend page contract's "oversight_filters" control -- never on a
// hardcoded literal, a role string, or a data shape a hand-crafted payload could satisfy.
const source = readFileSync(
  fileURLToPath(new URL("./verification-review-page.tsx", import.meta.url)),
  "utf8",
);

test("the oversight filters flag is read from the page contract's oversight_filters control", () => {
  assert.match(
    source,
    /const oversightFiltersEnabled = controlEnabled\(pageContract, "oversight_filters", false\)/,
    "oversightFiltersEnabled must come from controlEnabled(pageContract, \"oversight_filters\", ...), not be hardcoded or role-derived",
  );
});

test("the module-chip row is gated on oversightFiltersEnabled", () => {
  assert.match(
    source,
    /\{oversightFiltersEnabled && modules\.length > 1 \? \(/,
    "the module chip row must require oversightFiltersEnabled in addition to having more than one module option",
  );
});

test("the capture-date range picker is gated on oversightFiltersEnabled", () => {
  const filterRow = source.match(/<div className="vr-frow">([\s\S]*?)\{sheds\.length \? \(/);
  assert.ok(filterRow, "expected the vr-frow filter row to precede the shed filter block");
  assert.match(
    filterRow[1],
    /\{oversightFiltersEnabled \? \(\s*<ActionsDateFilter/,
    "ActionsDateFilter must only render when oversightFiltersEnabled is true",
  );
});

// The verifier's original working-queue filters predate the oversight rollout (status chips:
// commit fe06be1ed, the screen's first commit; shed filter: commit 89b16c0fa, the same commit
// that introduced this screen). They must stay UNGATED so a verifier's queue is unaffected.
test("status chips and the shed filter are not gated behind oversightFiltersEnabled", () => {
  const statusBlock = source.match(/\{statuses\.length \? \(([\s\S]*?)\) : null\}\s*\n\s*<div className="vr-secthd">/);
  assert.ok(statusBlock, "expected the status chip block ahead of the results table heading");
  assert.ok(
    !/oversightFiltersEnabled/.test(statusBlock[1]),
    "status chips predate the oversight rollout and must render for every role, including RoleVerifier",
  );

  const shedFormMatch = source.match(/id="verification-shed"[\s\S]{0,400}/);
  assert.ok(shedFormMatch, "expected the shed <select> in the filter row");
  assert.ok(
    !/oversightFiltersEnabled/.test(shedFormMatch[0]),
    "the shed filter predates the oversight rollout and must render for every role, including RoleVerifier",
  );
});

// No hardcoded filter chip labels: every visible string this screen shows for the oversight
// filters must be backend copy (pageContract), never a literal the frontend invented -- so a
// hand-edited role cannot resurrect the filters by literal text, and a copy change lands from one
// place (the contract), not two.
test("no hardcoded oversight filter chip labels: module/date/shed copy is backend-owned", () => {
  for (const literal of [
    "All modules",
    "To verify",
    "Accepted",
    "Rejected",
  ]) {
    assert.ok(
      !source.includes(`"${literal}"`) && !source.includes(`'${literal}'`) && !source.includes(`>${literal}<`),
      `filter chip label "${literal}" must come from the page contract via copy(pageContract, ...), not be hardcoded`,
    );
  }
});
