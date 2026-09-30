import assert from "node:assert/strict";
import test from "node:test";

import { legacyPeopleTabRedirect } from "./people-routes.ts";

test("an old /people tab link lands on its own HRMS page, keeping every other parameter", () => {
  assert.equal(legacyPeopleTabRedirect({ tab: "clock" }), "/people/clock");
  assert.equal(
    legacyPeopleTabRedirect({ tab: "clock", clocking: "e1", date: "2026-09-30" }),
    "/people/clock?clocking=e1&date=2026-09-30",
  );
  assert.equal(legacyPeopleTabRedirect({ tab: "vaccination", park: "p1", scope_mode: "company" }), "/people/vaccination?park=p1&scope_mode=company");
  assert.equal(legacyPeopleTabRedirect({ tab: "notifications" }), "/people/notifications");
});

test("the directory itself, and tabs that never had a page, stay on /people", () => {
  assert.equal(legacyPeopleTabRedirect({}), null);
  assert.equal(legacyPeopleTabRedirect({ tab: "all" }), null);
  assert.equal(legacyPeopleTabRedirect({ tab: "weighing" }), null);
  assert.equal(legacyPeopleTabRedirect({ person: "p9" }), null);
});
