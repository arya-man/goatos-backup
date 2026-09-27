// Guard: tasks-one-tab-strip (TR1-#41). /tasks has ONE tab strip (the card's status tabs); the scope
// (For me / Raised by me / Team progress) is the toolbar's first select and the board / list switch
// sits with the page action, so nothing clips at 390 and no second strip stacks above the card.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const page = readFileSync(new URL("./leadership-tasks-page.tsx", import.meta.url), "utf8");
const filters = readFileSync(new URL("./leadership-tasks-filters.tsx", import.meta.url), "utf8");
const loading = readFileSync(new URL("../../app/(admin)/tasks/loading.tsx", import.meta.url), "utf8");

test("tasks-one-tab-strip: scope is a toolbar select, no page-level tabs", () => {
  assert.doesNotMatch(page, /<TemplateTabs\b|<UrlTabs\b/, "no scope tab strip on the page");
  assert.match(page, /scope=\{\s*scopes\.length/, "the scope reaches the toolbar");
  assert.match(filters, /<LinkSelect label=\{scope\.label\}/, "scope renders as the toolbar select");
  assert.match(page, /actions=\{\s*<>\s*<TaskViewToggle/, "view switch sits with the page action");
  assert.doesNotMatch(loading, /variant="pill"/, "skeleton has no scope pill strip");
});
