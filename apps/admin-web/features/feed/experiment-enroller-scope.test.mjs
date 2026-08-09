import assert from "node:assert/strict";
import test from "node:test";

import { experimentEnrollerScopeKey } from "./experiment-enroller-scope.ts";

test("experiment enroller gets a new React identity when park scope changes", () => {
  const parkA = [{ id: "park-a" }];
  const parkB = [{ id: "park-b" }];
  const allParks = [{ id: "park-a" }, { id: "park-b" }];

  assert.notEqual(experimentEnrollerScopeKey(parkA), experimentEnrollerScopeKey(parkB));
  assert.notEqual(experimentEnrollerScopeKey(parkA), experimentEnrollerScopeKey(allParks));
  assert.equal(experimentEnrollerScopeKey(parkA), experimentEnrollerScopeKey([{ id: "park-a" }]));
});
