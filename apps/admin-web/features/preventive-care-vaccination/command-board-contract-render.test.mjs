import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const viewSource = readFileSync(new URL("./command-board-view.tsx", import.meta.url), "utf8");
const contractSource = readFileSync(
  new URL("../../lib/admin-ui-contract.ts", import.meta.url),
  "utf8",
);

test("every targeted animal is accounted for by a rendered KPI bucket", () => {
  // The five buckets are a disjoint, EXHAUSTIVE partition of kpis.targets. While the fifth was
  // unrendered a leader saw "Total 100" above tiles summing to 97 and could not tell a projection
  // bug from animals that genuinely closed with no dose.
  assert.match(viewSource, /view\.kpis\.closedWithoutDose/);
  assert.match(viewSource, /command_board\.kpi\.closed_without_dose/);
  assert.match(viewSource, /command_board\.kpi\.closed_without_dose_dl/);
  assert.match(contractSource, /"command_board\.kpi\.closed_without_dose":/);
  assert.match(contractSource, /"command_board\.kpi\.closed_without_dose_dl":/);
});

test("a truncated drive catalogue is disclosed instead of reading as the whole programme", () => {
  // driveOptions is bounded, so a drive past the bound is otherwise indistinguishable from a drive
  // that was never planned. The contract requires clients to surface that more drives exist.
  assert.match(viewSource, /board\.driveOptionsTruncated/);
  assert.match(viewSource, /command_board\.filter\.drives_truncated/);
  assert.match(contractSource, /"command_board\.filter\.drives_truncated":/);
});

test("command-board payload types are derived from the generated contract client", () => {
  // Hand-written local mirrors of the response schema are why TypeScript stayed green while two
  // REQUIRED contract fields were silently dropped before reaching the render. Deriving makes the
  // next dropped field a compile error.
  assert.match(viewSource, /import type \{ AppApiComponents \} from "@goatos\/api-client"/);
  assert.match(viewSource, /AppApiComponents\["schemas"\]\["VaccinationCommandBoardResponse"\]/);
  for (const local of ["CommandBoardKpis", "CohortCell", "ShedDoseCell", "QueueRow"]) {
    assert.doesNotMatch(
      viewSource,
      new RegExp(`interface ${local} \\{`),
      `${local} must be derived from the generated client, not hand-declared`,
    );
  }
});

test("shed dose matrix renders park context under each pen", () => {
  // The red/blue Vaccine x Pen Status table is the one a park head acts on. A pen label without
  // its park is not enough on the live tenant because same-looking operational pens exist across
  // farms, and the screenshot regression was exactly that missing context.
  assert.match(viewSource, /parkName: cell\.parkName \?\? null/);
  assert.match(viewSource, /row\.parkName \? <span className="cbm-rowh-note">\{row\.parkName\}<\/span> : null/);
});
