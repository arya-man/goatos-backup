import assert from "node:assert/strict";
import test from "node:test";
import { copy } from "./admin-ui-contract.ts";

function pageWithCopy(copyValues = {}) {
  return {
    route_id: "vaccination",
    title: "Vaccination",
    subtitle: "",
    copy: copyValues,
    option_groups: [],
    controls: [],
    tables: [],
  };
}

test("vaccination schedule workload labels tolerate stale backend page contracts", () => {
  const page = pageWithCopy();
  assert.equal(copy(page, "schedule.load.scheduled_short"), "sched.");
  assert.equal(copy(page, "schedule.load.deferred_short"), "def.");
  assert.equal(copy(page, "label.overdue"), "overdue");
  assert.equal(copy(page, "label.done"), "done");
});
