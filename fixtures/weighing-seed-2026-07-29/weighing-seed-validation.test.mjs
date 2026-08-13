import test from "node:test";
import { validateWeighingFixtureFile } from "../../tools/dev/validate-weighing-fixture.mjs";

// NOTE: this is a SCHEMA VALIDATOR over a seeded JSON fixture, not an
// end-to-end test. It proves the fixture file's shape/coverage fields are
// present; it does not exercise the weighing backend, mobile app, or any
// production code path. See ./README.md "What this fixture is NOT".
test("weighing seed fixture schema covers the v1 regression scenario list", () => {
  validateWeighingFixtureFile(new URL("./weighing-seed.json", import.meta.url));
});
