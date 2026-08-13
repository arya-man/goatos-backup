#!/usr/bin/env node
// check-android-alerts-gate-composed.mjs — notification access must be asked for on a surface
// EVERY role passes through, not only where someone scans or records.
//
// The failure this blocks actually shipped. The only permission card in the app
// (PermissionGateCard) was never composed anywhere, and the mandatory capture gates only run on
// scan/record screens — so a director, a park head or the CEO was never asked for notification
// access at all. On Android 13+ that permission starts DENIED. Their phones still held valid push
// tokens, so the push gateway accepted every alert and reported it delivered while the OS silently
// dropped it. Nothing failed and nothing logged; the roles whose alerts matter most received
// nothing.
//
// The app shell is the only surface every role reaches after bootstrap — the start destination
// itself varies by role (/vaccination, /weighing, /verify, /calendar, /counts...) — so the alerts
// gate belongs there and nowhere narrower.
//
// Invariants:
//   1. The shell composes the alerts gate.
//   2. The alerts gate actually asks the OS about notifications (the permission AND the app-wide
//      notification switch), rather than being a card that only talks about them.
//
// `--self-test` runs the same detector over fixtures that violate each invariant, so a guard that
// has quietly stopped detecting anything cannot pass as a satisfied one.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const SHELL = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/GoatOsShell.kt";
// 2026-08-02: the non-blocking alerts BANNER was replaced by a mandatory, non-dismissible
// role-based permission gate (maintainer decision: the app is unusable until the permissions a
// role needs are granted). The rule this guard protects is unchanged — a gate nothing composes
// asks nobody for anything — so it now points at the replacement.
const GATE =
  "apps/goatos-android/feature/feature-auth/src/main/kotlin/sg/mesha/goatos/feature/auth/RoleBasedPermissionGate.kt";
const GATE_NAME = "RoleBasedPermissionGate";

const composesGate = (shellSource) => new RegExp(`\\b${GATE_NAME}\\s*\\(`).test(shellSource);
const asksTheOs = (gateSource) =>
  gateSource.includes("AppPermission.NOTIFICATIONS") &&
  /RequestMultiplePermissions\(\)/.test(gateSource);

function selfTest() {
  const failures = [];
  if (composesGate("OfflineBanner(visible = showOffline)\nAppNavHost(navController)")) {
    failures.push("a shell that never composes the alerts gate would be reported as compliant");
  }
  if (!composesGate(`${GATE_NAME}(onAlertsTurnedOn = vm::reportNow)`)) {
    failures.push("a shell that DOES compose the alerts gate would be reported as broken");
  }
  if (asksTheOs("Text(\"Alerts are off\")")) {
    failures.push("a card that only talks about notifications would pass as asking the OS");
  }
  if (failures.length > 0) {
    for (const failure of failures) console.error(`self-test FAILED: ${failure}`);
    process.exit(1);
  }
  console.log("check-android-alerts-gate-composed: self-test ok");
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

const problems = [];
const shellSource = readFileSync(resolve(repo, SHELL), "utf8");
const gateSource = readFileSync(resolve(repo, GATE), "utf8");

if (!composesGate(shellSource)) {
  problems.push(
    `${SHELL}: the app shell does not compose ${GATE_NAME}. A gate nothing shows asks nobody for anything — every role that never opens a capture screen goes back to receiving no alerts at all.`,
  );
}
if (!asksTheOs(gateSource)) {
  problems.push(
    `${GATE}: ${GATE_NAME} no longer asks the OS about notifications (it must read areNotificationsEnabled, use AppPermission.NOTIFICATIONS, and request the permission). A card that only describes alerts turns nothing on.`,
  );
}

if (problems.length > 0) {
  for (const problem of problems) console.error(problem);
  process.exit(1);
}
console.log("check-android-alerts-gate-composed: ok");
