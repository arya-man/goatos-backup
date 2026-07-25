import assert from "node:assert/strict";
import test from "node:test";

import { ParkScopeAmbiguousError } from "../../lib/api/park-scope.ts";
import { loadVaccinationOperatorsScreen } from "./vaccination-operators-scope.ts";

// BUG-019 (completion). The screen's park scope is BACKEND-owned. Three states must
// hold, and the previous partial fix only produced a thrown error for the first:
//   (a) an actor whose scope spans several parks gets a usable, backend-driven
//       park CHOICE, not a dead end;
//   (b) an actor whose scope already resolves to one park needs zero clicks;
//   (c) once a park is chosen, EVERY downstream read is scoped to it;
//   (d) no park is ever silently auto-picked for a multi-park actor.

const PARK_A = "20000000-0000-4000-8000-00000000000a";
const PARK_B = "20000000-0000-4000-8000-00000000000b";

function fakeApi({ config, ambiguousParks, capThrows }) {
  const calls = { configParkIds: [], positionScopeIds: [] };
  return {
    calls,
    async getVaccinationOperatorAssignmentConfig(parkId) {
      calls.configParkIds.push(parkId);
      if (ambiguousParks && !parkId) {
        throw new ParkScopeAmbiguousError("pick a park", ambiguousParks);
      }
      return { data: { ...config, parkId: parkId ?? config.parkId } };
    },
    async listStaffPositions(params) {
      calls.positionScopeIds.push(params.scope_id);
      return { data: { items: [{ position_id: "p1", workforce_member_id: "w1", is_backup_slot: false }] } };
    },
    async getVaccinationCapacityConfig() {
      if (capThrows) throw new Error("capacity config unavailable");
      return { data: { maxPerDay: 200, maxShotsPerAnimalPerDrive: null, rowVersion: 3 } };
    },
    async listStaffLeave() {
      return { data: { items: [] } };
    },
  };
}

const CONFIG = { parkId: PARK_A, activeOperatorsPerDay: 1, defaultOperatorId: "w1", rowVersion: 3, shifts: [] };

test("(b) a single-park actor needs no selection and no park_id round trip", async () => {
  const api = fakeApi({ config: CONFIG });
  const result = await loadVaccinationOperatorsScreen(api);
  assert.equal(result.state, "ready");
  assert.equal(result.parkId, PARK_A);
  assert.deepEqual(api.calls.configParkIds, [undefined], "the first config read must let the backend resolve scope");
  assert.deepEqual(api.calls.positionScopeIds, [PARK_A], "the roster read must carry the resolved park");
});

test("(a)+(d) a multi-park actor gets backend-owned park options, and nothing is auto-picked", async () => {
  const parks = [
    { parkId: PARK_A, code: "CPT", name: "Channapatna" },
    { parkId: PARK_B, code: "CBE", name: "Coimbatore" },
  ];
  const api = fakeApi({ config: CONFIG, ambiguousParks: parks });
  const result = await loadVaccinationOperatorsScreen(api);
  assert.equal(result.state, "needs_park_selection", "an ambiguous scope must be a usable choice, not a thrown dead end");
  assert.deepEqual(result.parks, parks, "the selectable parks must come from the backend verbatim");
  assert.deepEqual(api.calls.positionScopeIds, [], "no downstream read may run before a park is chosen");
});

test("(c) after a park is chosen every downstream read is scoped to it", async () => {
  const parks = [
    { parkId: PARK_A, code: "CPT", name: "Channapatna" },
    { parkId: PARK_B, code: "CBE", name: "Coimbatore" },
  ];
  const api = fakeApi({ config: CONFIG, ambiguousParks: parks });
  const result = await loadVaccinationOperatorsScreen(api, PARK_B);
  assert.equal(result.state, "ready");
  assert.equal(result.parkId, PARK_B);
  assert.deepEqual(api.calls.configParkIds, [PARK_B]);
  assert.deepEqual(api.calls.positionScopeIds, [PARK_B], "the roster (and the KPI/preview/dropdown it feeds) must be park-scoped");
});

test("a failed capacity-config load surfaces an error and does NOT fake 200/rowVersion-0", async () => {
  const api = fakeApi({ config: CONFIG, capThrows: true });
  const result = await loadVaccinationOperatorsScreen(api);
  assert.equal(result.state, "ready", "the roster still renders; only cap editing is gated");
  assert.ok(result.capConfigError, "capConfigError must be set when the capacity config fails to load");
  assert.match(result.capConfigError, /capacity config/i);
});

test("a successful capacity-config load leaves capConfigError null and passes the real rowVersion", async () => {
  const api = fakeApi({ config: CONFIG });
  const result = await loadVaccinationOperatorsScreen(api);
  assert.equal(result.capConfigError, null);
  assert.equal(result.capRowVersion, 3, "the real backend rowVersion must flow through, not a fabricated 0");
});
