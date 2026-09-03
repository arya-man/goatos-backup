import assert from "node:assert/strict";
import test from "node:test";
import { copy, optionLabel, optionTone } from "./admin-ui-contract.ts";

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

test("process integrity enum skew renders a neutral label instead of crashing the route", () => {
  const page = {
    ...pageWithCopy({}, "protocol-adherence"),
    option_groups: [
      {
        id: "work_state_filter_chips",
        options: [{ key: "scheduled", label: "Scheduled", title: "", tone: "info", enabled: true, disabled_reason: "" }],
      },
    ],
  };

  assert.equal(optionLabel(page, "work_state_filter_chips", "waiting_on_director_review"), "Waiting On Director Review");
  assert.equal(optionTone(page, "work_state_filter_chips", "waiting_on_director_review"), "mut");
});

test("fixed missing copy keys still fail loudly", () => {
  assert.throws(
    () => copy(pageWithCopy({}, "protocol-adherence"), "label.this_key_is_required"),
    /missing copy key label\.this_key_is_required/,
  );
});
