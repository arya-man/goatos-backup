#!/usr/bin/env node

// check-android-navigation-stack.mjs — prevents structural Android drill
// destinations from inheriting L0 navigation chrome.
//
// The invariant is intentionally small and global:
//   * only an exact backend bootstrap root route owns the bottom bar/drawer;
//   * a Calendar drill fallback uses its dedicated hosted child route;
//   * regression tests cover exact roots, prefix collisions, hosted children,
//     and null/generic Calendar targets.
//
// This guard always scans the authoritative shell, host, and test files. They
// are tiny, and a global scan prevents a diff-base problem from making a
// navigation regression look green.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const paths = {
  host: "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt",
  shell: "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/GoatOsShell.kt",
  test: "apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/ui/TopLevelChromeTest.kt",
};

function read(rel) {
  return readFileSync(resolve(repo, rel), "utf8");
}

function functionSlice(source, name) {
  const start = source.indexOf(`fun ${name}`);
  if (start < 0) return "";
  const next = source.indexOf("\nfun ", start + 4);
  const nextInternal = source.indexOf("\ninternal fun ", start + 4);
  const nextPrivate = source.indexOf("\nprivate fun ", start + 4);
  const ends = [next, nextInternal, nextPrivate].filter((value) => value > start);
  return source.slice(start, ends.length ? Math.min(...ends) : source.length);
}

export function findingsForSources({ host, shell, test }) {
  const findings = [];
  const chrome = functionSlice(shell, "isTopLevelRoute");
  const calendarTarget = functionSlice(host, "calendarTargetRoute");

  if (!/currentRoute\s*!=\s*null\s*&&\s*currentRoute\s+in\s+topLevelRoutes/.test(chrome)) {
    findings.push(
      "top-level chrome must use exact route membership: " +
        "`currentRoute != null && currentRoute in topLevelRoutes`",
    );
  }
  if (/\b(?:startsWith|contains|substringBefore|removePrefix)\s*\(/.test(chrome)) {
    findings.push(
      "top-level chrome must not use path-prefix/substring matching; child routes cannot inherit L0 chrome",
    );
  }
  if (!/const\s+val\s+CALENDAR_DRIVE\s*=\s*"\/calendar\/drive"/.test(host)) {
    findings.push("Calendar must declare a dedicated hosted child route (`Routes.CALENDAR_DRIVE`)");
  }
  if (!/target\.isNullOrBlank\(\)\)\s+return\s+Routes\.CALENDAR_DRIVE/.test(calendarTarget)) {
    findings.push("blank Calendar targets must fall back to `Routes.CALENDAR_DRIVE`, never an L0 route");
  }
  if (!/else\s+Routes\.CALENDAR_DRIVE/.test(calendarTarget)) {
    findings.push("generic Calendar targets must fall back to `Routes.CALENDAR_DRIVE`, never an L0 route");
  }
  if (/return\s+Routes\.(?:CALENDAR|VACCINATION|LEADERSHIP|ALERTS|YOU)\b/.test(calendarTarget)) {
    findings.push("Calendar drill routing returns a known L0 route; use a dedicated hosted child destination");
  }

  const requiredTestEvidence = [
    ["hosted child chrome coverage", /Routes\.CALENDAR_DRIVE[\s\S]*isTopLevelRoute/],
    ["prefix-collision coverage", /isTopLevelRoute\("\$\{Routes\.VACCINATION\}\/drive"/],
    ["blank-target route coverage", /calendarTargetRoute\(null\)/],
    ["dedicated-child assertion", /assertEquals\(Routes\.CALENDAR_DRIVE,\s*calendarTargetRoute/],
  ];
  for (const [label, pattern] of requiredTestEvidence) {
    if (!pattern.test(test)) findings.push(`TopLevelChromeTest is missing ${label}`);
  }

  return findings;
}

function selfTest() {
  const good = {
    host: `
      object Routes { const val CALENDAR_DRIVE = "/calendar/drive" }
      internal fun calendarTargetRoute(target: String?): String {
        if (target.isNullOrBlank()) return Routes.CALENDAR_DRIVE
        val shedId = shedIdFromTarget(target)
        return if (shedId != null) Routes.scanRoute(shedId) else Routes.CALENDAR_DRIVE
      }
    `,
    shell: `
      internal fun isTopLevelRoute(currentRoute: String?, topLevelRoutes: Collection<String>): Boolean =
        currentRoute != null && currentRoute in topLevelRoutes
    `,
    test: `
      assertFalse(Routes.CALENDAR_DRIVE, isTopLevelRoute(Routes.CALENDAR_DRIVE, roots))
      assertFalse(isTopLevelRoute("\${Routes.VACCINATION}/drive", roots))
      assertEquals(Routes.CALENDAR_DRIVE, calendarTargetRoute(null))
    `,
  };
  if (findingsForSources(good).length) {
    throw new Error(`self-test rejected compliant fixture: ${findingsForSources(good).join("; ")}`);
  }

  const prefixBug = {
    ...good,
    shell: `
      internal fun isTopLevelRoute(currentRoute: String?, topLevelRoutes: Collection<String>) =
        topLevelRoutes.any { currentRoute?.startsWith(it) == true }
    `,
  };
  if (!findingsForSources(prefixBug).some((item) => item.includes("exact route membership"))) {
    throw new Error("self-test did not reject prefix-based chrome inheritance");
  }

  const rootFallbackBug = {
    ...good,
    host: good.host.replace(
      "if (target.isNullOrBlank()) return Routes.CALENDAR_DRIVE",
      "if (target.isNullOrBlank()) return Routes.VACCINATION",
    ),
  };
  if (!findingsForSources(rootFallbackBug).some((item) => item.includes("blank Calendar targets"))) {
    throw new Error("self-test did not reject an L0 Calendar fallback");
  }

  console.log("android-navigation-stack self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = findingsForSources({
  host: read(paths.host),
  shell: read(paths.shell),
  test: read(paths.test),
});
if (findings.length) {
  console.error(
    "android-navigation-stack-guard FAILED — structural drills must be hosted children " +
      "with Up/Back and no L0 bottom bar/drawer:",
  );
  findings.forEach((finding) => console.error(`- ${finding}`));
  console.error("See docs/decisions/android-navigation-stack.md.");
  process.exit(1);
}

console.log("android-navigation-stack: ok (exact L0 chrome + hosted Calendar drill regression coverage)");
