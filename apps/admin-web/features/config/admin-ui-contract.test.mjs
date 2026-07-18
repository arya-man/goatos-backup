import { test } from "node:test";
import assert from "node:assert";
import { copy, optionGroup } from "../../lib/admin-ui-contract.ts";
import { shouldShowConfigRulePager } from "./config-pagination.ts";

function configPage(overrides = {}) {
  return {
    route_id: "config",
    copy: {},
    option_groups: [],
    tables: [],
    controls: [],
    ...overrides,
  };
}

test("config authoring copy fails closed when backend contract omits key", () => {
  assert.throws(
    () => copy(configPage(), "modal.rule_editor.table.course_type"),
    /missing copy key modal\.rule_editor\.table\.course_type/,
  );
});

test("config authoring option groups fail closed when backend contract omits taxonomy", () => {
  assert.throws(
    () => optionGroup(configPage(), "vaccine_course_types"),
    /missing option group vaccine_course_types/,
  );
});

test("config protocol rule pager hides for single-page rule lists", () => {
  assert.strictEqual(shouldShowConfigRulePager(0, 5), false);
  assert.strictEqual(shouldShowConfigRulePager(1, 5), false);
  assert.strictEqual(shouldShowConfigRulePager(5, 5), false);
  assert.strictEqual(shouldShowConfigRulePager(6, 5), true);
});
