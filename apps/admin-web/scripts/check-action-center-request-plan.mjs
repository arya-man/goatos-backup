import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const { actionCenterRequestPlan } = await import("../features/process-integrity/action-center-request-plan.ts");

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const adminWebRoot = path.resolve(scriptDir, "..");
const actionCenterSource = readFileSync(path.join(adminWebRoot, "features/process-integrity/action-center.tsx"), "utf8");

const allState = actionCenterRequestPlan({
  stateFilter: "all",
  severityFilter: "all",
  requestedBoardPage: { pageSize: 10, offset: 0 },
  parkId: "park-1",
  asOf: "2026-07-11T12:00:00+05:30",
});

assertEqual(allState.actionCenter.workState, undefined, "all-state board must use one unfiltered Action Center request");
assertEqual(allState.actionCenter.limit, 10, "all-state board request must use the requested page size");
assertEqual(allState.actionCenter.offset, 0, "all-state board request must preserve pagination offset");
assertEqual(allState.verificationQueue.limit, 200, "verification queue request must stay bounded");
assertEqual(Object.keys(allState).length, 2, "Action Center request plan must not carry per-lane sample requests");

const filtered = actionCenterRequestPlan({
  stateFilter: "overdue",
  severityFilter: "at_risk",
  requestedBoardPage: { pageSize: 25, offset: 50 },
});

assertEqual(filtered.actionCenter.workState, "overdue", "single-state board must request only the selected work state");
assertEqual(filtered.actionCenter.severity, "at_risk", "severity filter must be sent to the backend");
assertEqual(filtered.actionCenter.limit, 25, "filtered board request must use the requested page size");
assertEqual(filtered.actionCenter.offset, 50, "filtered board request must preserve pagination offset");

assertIncludes(actionCenterSource, "Promise.all([", "Action Center must parallelize independent server requests");
assertNotIncludes(actionCenterSource, "boardSampleStates", "Action Center must not restore per-lane sample request fanout");
assertNotIncludes(actionCenterSource, "boardSamples", "Action Center must not restore per-lane sample request fanout");
assertNotIncludes(actionCenterSource, "boardWorkStates", "Action Center must not derive per-state request fanout from UI lanes");
assertNotIncludes(actionCenterSource, "stateCounts={stateCounts}", "WorkBoard lane counts must describe visible cards, not hidden server rows");

console.log("action-center request plan: ok");

function assertEqual(got, want, message) {
  if (got !== want) {
    throw new Error(`${message}: got ${String(got)}, want ${String(want)}`);
  }
}

function assertIncludes(source, needle, message) {
  if (!source.includes(needle)) {
    throw new Error(message);
  }
}

function assertNotIncludes(source, needle, message) {
  if (source.includes(needle)) {
    throw new Error(message);
  }
}
