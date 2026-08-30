import assert from "node:assert/strict";
import test from "node:test";

import { personName } from "./version-format.ts";

test("publisher names render only when the backend value is a human-readable string", () => {
  assert.equal(personName(" CEO Ops "), "CEO Ops");
  assert.equal(personName("90000000-0000-4000-8000-000000000001"), null);
  assert.equal(personName(""), null);
  assert.equal(personName(null), null);
  assert.equal(personName({ display_name: "CEO Ops" }), null);
});
