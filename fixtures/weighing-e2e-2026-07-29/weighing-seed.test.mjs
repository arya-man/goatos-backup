import test from "node:test";
import { validateWeighingFixtureFile } from "../../tools/dev/validate-weighing-e2e-fixture.mjs";

test("weighing E2E seed fixture covers the v1 regression lane", () => {
  validateWeighingFixtureFile(new URL("./weighing-seed.json", import.meta.url));
});
