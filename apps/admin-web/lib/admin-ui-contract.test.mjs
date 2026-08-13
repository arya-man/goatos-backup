import assert from "node:assert/strict";
import test from "node:test";
import { copy } from "./admin-ui-contract.ts";

function pageWithCopy(copyValues = {}, routeId = "vaccination") {
  return {
    route_id: routeId,
    title: routeId,
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

test("control tower drawer labels tolerate stale backend page contracts", () => {
  const page = pageWithCopy({}, "control-tower");
  assert.equal(copy(page, "label.scope"), "Scope");
  assert.equal(copy(page, "label.detail"), "Detail");
  assert.equal(copy(page, "label.evidence"), "Evidence");
  assert.equal(copy(page, "drawer.alert.aria"), "Control Tower alert");
  assert.equal(copy(page, "drawer.alert.close_label"), "Close Control Tower alert");
  assert.equal(copy(page, "drawer.alert.guidance"), "Use the linked work surfaces to resolve the underlying process gap.");
});
