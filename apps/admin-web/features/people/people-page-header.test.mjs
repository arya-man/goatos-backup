import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: people-header-action-every-desk (J3 P1-1). The /people header rendered "Add person" only on
// the All People desk, so leaving it collapsed the header row and the tab strip jumped ~60px under
// the thumb at 390. The action renders on every desk (opening the drawer in place on All People,
// navigating there with the drawer open elsewhere).
const source = readFileSync(new URL("./people-page.tsx", import.meta.url), "utf8");

test("guard: people-header-action-every-desk - the PageHeader action does not depend on the active desk", () => {
  const at = source.indexOf("<PageHeader");
  assert.ok(at >= 0, "people-page renders a PageHeader");
  const actions = source.slice(at).match(/\n\s*actions=\{(.*)\}\s*\n/)?.[1] ?? "";
  assert.match(actions, /<PeopleAddButton/, "the header action is Add person");
  assert.doesNotMatch(actions, /^\s*active\s*[!=]==?[^?]*\?/, "Add person must not render conditionally on the active desk");
  assert.doesNotMatch(actions, /:\s*(undefined|null)\s*$/, "no desk may drop the header action");
});
