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

import { readFileSync, readdirSync } from "node:fs";
import { relative, resolve } from "node:path";

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

  // Drawer access must be SHELL-OWNED, not opt-in per screen.
  //
  // The regression this blocks: `LocalDrawerOpener` used to be provided unconditionally, and
  // each screen decided whether to draw a hamburger for it. Only two ever did, so every other
  // L0 root (Counts, Alerts, You) shipped with no way to switch modules and Vaccination's own
  // root tab showed a Back arrow. The opener is now the shell's single derived answer to "does
  // this destination have a drawer", so it MUST be gated on the same exact L0 membership the
  // bottom bar uses — otherwise a hosted child would render root chrome again.
  const chromeFn = functionSlice(shell, "GoatOsShellChrome");
  const providesOpener = /LocalDrawerOpener\s+provides\s+(\w+)/.exec(chromeFn);
  if (!providesOpener) {
    findings.push("GoatOsShellChrome must provide `LocalDrawerOpener` so every L0 root gets drawer access");
  } else {
    const gate = new RegExp(
      `(?:val\\s+${providesOpener[1]}[^\\n]*\\n?[^\\n]*)if\\s*\\(\\s*hasDrawer\\s*&&\\s*isTopLevel\\s*\\)`,
    );
    if (!gate.test(chromeFn)) {
      findings.push(
        "the drawer opener must be gated on `hasDrawer && isTopLevel` (null off L0), " +
          "so a hosted child can never inherit the module drawer",
      );
    }
  }

  const requiredTestEvidence = [
    ["hosted child chrome coverage", /Routes\.CALENDAR_DRIVE[\s\S]*isTopLevelRoute/],
    ["prefix-collision coverage", /isTopLevelRoute\("\$\{Routes\.VACCINATION\}\/drive"/],
    ["blank-target route coverage", /calendarTargetRoute\(null\)/],
    ["dedicated-child assertion", /assertEquals\(Routes\.CALENDAR_DRIVE,\s*calendarTargetRoute/],
    ["L0 drawer-availability coverage", /drawerAvailable|hasDrawerAffordance/],
  ];
  for (const [label, pattern] of requiredTestEvidence) {
    if (!pattern.test(test)) findings.push(`TopLevelChromeTest is missing ${label}`);
  }

  return findings;
}

/**
 * The hamburger must have exactly ONE implementation, in the shared header.
 *
 * A feature that hand-rolls its own menu button is free to forget it (which is how this
 * regressed), free to skip the accessibility label, and free to show it on a drill. Routing
 * every screen through `MeshaScreenHeader` makes the affordance derive from backend-composed
 * L0 membership instead of from whatever the screen author remembered.
 */
export function findingsForFeatureHeaders(files) {
  const findings = [];
  for (const { path: rel, source: raw } of files) {
    // Scan CODE only. A KDoc/comment that names the symbol while explaining why the screen no
    // longer uses it is exactly the documentation we want, and must not trip the guard.
    const source = raw.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/[^\n]*/g, "");
    if (/MeshaIcons\.Menu\b/.test(source)) {
      findings.push(
        `${rel} draws its own drawer/menu button (MeshaIcons.Menu); ` +
          "render `MeshaScreenHeader` instead and let the shell decide the leading affordance",
      );
    }
    if (/LocalDrawerOpener\b/.test(source)) {
      findings.push(
        `${rel} reads LocalDrawerOpener directly; ` +
          "drawer access is shell-owned — use `MeshaScreenHeader`",
      );
    }
  }
  return findings;
}

function featureSources() {
  const root = resolve(repo, "apps/goatos-android/feature");
  const out = [];
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = resolve(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === "build") continue;
        walk(full);
      } else if (entry.name.endsWith(".kt")) {
        out.push({ path: relative(repo, full), source: readFileSync(full, "utf8") });
      }
    }
  };
  walk(root);
  return out;
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
      @Composable
      fun GoatOsShellChrome(navState: NavState) {
        val drawerOpener: (() -> Unit)? =
          if (hasDrawer && isTopLevel) {
            { scope.launch { drawerState.open() } }
          } else {
            null
          }
        CompositionLocalProvider(LocalDrawerOpener provides drawerOpener) { content() }
      }
      internal fun isTopLevelRoute(currentRoute: String?, topLevelRoutes: Collection<String>): Boolean =
        currentRoute != null && currentRoute in topLevelRoutes
    `,
    test: `
      assertFalse(Routes.CALENDAR_DRIVE, isTopLevelRoute(Routes.CALENDAR_DRIVE, roots))
      assertFalse(isTopLevelRoute("\${Routes.VACCINATION}/drive", roots))
      assertEquals(Routes.CALENDAR_DRIVE, calendarTargetRoute(null))
      assertTrue(drawerAvailable(NavChrome.EXPANDED, "/counts", countsRoots))
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

  // The exact regression that shipped: the opener provided unconditionally, so drawer access
  // became a per-screen decision and a hosted drill could open the module drawer.
  const ungatedOpener = {
    ...good,
    shell: good.shell.replace(
      /val drawerOpener[\s\S]*?\n          }\n/,
      "val drawerOpener: (() -> Unit)? = { scope.launch { drawerState.open() } }\n",
    ),
  };
  if (!findingsForSources(ungatedOpener).some((item) => item.includes("hasDrawer && isTopLevel"))) {
    throw new Error("self-test did not reject an ungated drawer opener");
  }

  const noOpener = {
    ...good,
    shell: good.shell.replace(/CompositionLocalProvider\(LocalDrawerOpener[^\n]*\n/, "content()\n"),
  };
  if (!findingsForSources(noOpener).some((item) => item.includes("must provide `LocalDrawerOpener`"))) {
    throw new Error("self-test did not reject a shell that never provides the drawer opener");
  }

  // Adversarial sibling: a feature module hand-rolling its own hamburger is how a screen
  // silently opts out of (or wrongly into) drawer access.
  const handRolled = findingsForFeatureHeaders([
    { path: "feature/feature-x/XScreen.kt", source: "HeaderIconButton(icon = MeshaIcons.Menu)" },
    { path: "feature/feature-y/YScreen.kt", source: "val open = LocalDrawerOpener.current" },
  ]);
  if (handRolled.length !== 2) {
    throw new Error("self-test did not reject hand-rolled feature drawer affordances");
  }
  if (findingsForFeatureHeaders([{ path: "feature/ok/OkScreen.kt", source: "MeshaScreenHeader(title = t)" }]).length) {
    throw new Error("self-test rejected a compliant shared-header screen");
  }

  console.log("android-navigation-stack self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = [
  ...findingsForSources({
    host: read(paths.host),
    shell: read(paths.shell),
    test: read(paths.test),
  }),
  ...findingsForFeatureHeaders(featureSources()),
];
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
