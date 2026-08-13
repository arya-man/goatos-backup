#!/usr/bin/env node
// check-android-verifier-detail-scope.mjs — verifier queue rows must open detail with the
// exact queue scope that produced the row.
//
// Regression blocked: Weighing queue rows showed "1 video", but detail opened without the
// selected status/date/missed scope and observed a different Room cache slice. The detail screen
// then rendered "No video attached to this item" even though the backend and queue row carried
// media. The queue-to-detail route must pass category, action mode, park, shed, status, business
// date, and missed-only together.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const APP_NAV_HOST = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt";
const DETAIL_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/VerifyDetailViewModel.kt";

function queueOpenItemRoute(source) {
  const marker = "is VerifyQueueEvent.OpenItem";
  const start = source.indexOf(marker);
  if (start < 0) return "";
  const nextComposable = source.indexOf("composable(", start + marker.length);
  return source.slice(start, nextComposable < 0 ? undefined : nextComposable);
}

function routePassesScope(block) {
  return [
    "itemId = event.itemId",
    "category = event.category",
    "actionMode = state.isActionQueue",
    "parkId = state.selectedParkId",
    "shedId = state.selectedShedId",
    "status = state.selectedStatus",
    "businessDate = state.selectedBusinessDate",
    "missed = state.missedOnly",
  ].every((needle) => block.includes(needle));
}

function detailLatchesSelectedGroup(source) {
  return (
    source.includes("val matching = answered.items.filter { it.verificationGroupKey() == itemId }") &&
    source.includes("matching.isEmpty()") &&
    source.includes("previous.isNotEmpty()") &&
    source.includes("!_flags.value.isDecisionResolving") &&
    source.includes("!_flags.value.autoCloseAfterDecision") &&
    source.includes("previous")
  );
}

function selfTest() {
  const failures = [];
  const badRoute = `
    is VerifyQueueEvent.OpenItem ->
      navController.navigate(Routes.verifyDetailRoute(
        itemId = event.itemId,
        category = event.category,
        actionMode = state.isActionQueue,
        parkId = state.selectedParkId,
        shedId = state.selectedShedId,
      ))
    composable("next") {}
  `;
  const goodRoute = `
    is VerifyQueueEvent.OpenItem ->
      navController.navigate(Routes.verifyDetailRoute(
        itemId = event.itemId,
        category = event.category,
        actionMode = state.isActionQueue,
        parkId = state.selectedParkId,
        shedId = state.selectedShedId,
        status = state.selectedStatus,
        businessDate = state.selectedBusinessDate,
        missed = state.missedOnly,
      ))
    composable("next") {}
  `;
  if (routePassesScope(queueOpenItemRoute(badRoute))) {
    failures.push("a verifier route that drops status/date/missed scope would pass");
  }
  if (!routePassesScope(queueOpenItemRoute(goodRoute))) {
    failures.push("a verifier route that carries the full queue scope would fail");
  }
  if (detailLatchesSelectedGroup("answered.items.filter { it.verificationGroupKey() == itemId }")) {
    failures.push("detail without the selected-group latch would pass");
  }
  if (
    !detailLatchesSelectedGroup(
      "val matching = answered.items.filter { it.verificationGroupKey() == itemId }\n" +
        "if (matching.isEmpty() && previous.isNotEmpty() && !_flags.value.isDecisionResolving && !_flags.value.autoCloseAfterDecision) previous else matching",
    )
  ) {
    failures.push("detail with the selected-group latch would fail");
  }
  if (failures.length > 0) {
    for (const failure of failures) console.error(`self-test FAILED: ${failure}`);
    process.exit(1);
  }
  console.log("check-android-verifier-detail-scope: self-test ok");
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

const problems = [];
const appNavHost = readFileSync(resolve(repo, APP_NAV_HOST), "utf8");
const detailVm = readFileSync(resolve(repo, DETAIL_VM), "utf8");

if (!routePassesScope(queueOpenItemRoute(appNavHost))) {
  problems.push(
    `${APP_NAV_HOST}: verifier queue OpenItem must pass status, businessDate, and missed along with category/action/park/shed. Otherwise detail can observe a different cache scope and hide real videos.`,
  );
}
if (!detailLatchesSelectedGroup(detailVm)) {
  problems.push(
    `${DETAIL_VM}: verifier detail must keep the previously opened group during non-decision refreshes that return a page without that group. Otherwise a refresh can replace a real video with the empty state.`,
  );
}

if (problems.length > 0) {
  for (const problem of problems) console.error(problem);
  process.exit(1);
}
console.log("check-android-verifier-detail-scope: ok");
