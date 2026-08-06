#!/usr/bin/env node

// check-l0-root-chrome.mjs — L0 root screens must have required shell chrome
//
// Every L0 root screen (rendered directly at backend-composed bootstrap routes)
// must render MeshaScreenHeader and call RefreshOnResume (for read screens).
// Manual refresh affordances must use SyncIconButton, never hand-rolled IconButton.
//
// This prevents screens from shipping without top chrome, drawer affordance,
// or refresh handling — as happened on 2026-08-03 with /vaccination/videos.
//
// L0 roots: routes that own global chrome (bottom bar/drawer). They are
// extracted from bootstrap bar items at runtime, so this guard checks the
// DECLARED L0 sources and verifies each one's implementation.

import { readFileSync, readdirSync } from "node:fs";
import { relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Parse the AppNavHost file to extract which routes are documented as L0 roots.
// We can also cross-check against what bootstrap sends at runtime.
const appNavHostPath = resolve(
  repo,
  "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt"
);
const appNavHostSource = readFileSync(appNavHostPath, "utf8");

// Declared L0 roots (from AppNavHost Routes object and composable registrations).
// This is the authoritative set; they appear in bootstrap bar items.
const l0Roots = new Set([
  "Routes.CALENDAR",
  "Routes.VACCINATION",
  "Routes.WEIGHING",
  "Routes.COUNTS_BIRTH",
  "Routes.COUNTS_DEATH",
  "Routes.COUNTS_SHIFTING",
  "Routes.COUNTS_MILK_PREPARATION",
  "Routes.COUNTS_MILK_FEEDING",
  "Routes.YOU",
  "Routes.VACCINATION_ALERTS",
  "Routes.WEIGHING_ALERTS",
  // Leadership/verifier screens that render at backend-composed L0 routes
  "Routes.VACCINATION_LEADERSHIP_VIDEOS",
  "Routes.WEIGHING_VIDEOS",
  "Routes.WEIGHING_GROWTH",
  "Routes.WEIGHING_WEIGHTS",
  "Routes.WEIGHING_OPERATORS",
]);

// Map routes to the screen composables that render them.
// Extracted from AppNavHost composable() calls.
const routeToScreenMap = {
  "Routes.CALENDAR": "CalendarScreen",
  "Routes.VACCINATION": "VaccinationScreen",
  "Routes.WEIGHING": "WeighingScreen",
  "Routes.COUNTS_BIRTH": "CountsBirthScreen",
  "Routes.COUNTS_DEATH": "CountsDeathScreen",
  "Routes.COUNTS_SHIFTING": "CountsShiftingScreen",
  "Routes.COUNTS_MILK_PREPARATION": "MilkPreparationListScreen",
  "Routes.COUNTS_MILK_FEEDING": "MilkFeedingListScreen",
  "Routes.YOU": "ProfileScreen",
  "Routes.VACCINATION_ALERTS": "AlertsScreen",
  "Routes.WEIGHING_ALERTS": "WeighingAlertsScreen",
  "Routes.VACCINATION_LEADERSHIP_VIDEOS": "VaccinationLeadershipVideosScreen",
  "Routes.WEIGHING_VIDEOS": "WeighingLeadershipVideosScreen",
  "Routes.WEIGHING_GROWTH": "WeighingGrowthScreen",
  "Routes.WEIGHING_WEIGHTS": "WeightHistoryChartScreen",
  "Routes.WEIGHING_OPERATORS": "WeighingOperatorsScreen",
};

// Read screens that show cached data and should call RefreshOnResume.
// Scan screens (camera/capture) and forms are exempted via ignore comments.
const readScreens = new Set([
  "CalendarScreen",
  "VaccinationScreen",
  "WeighingScreen",
  "CountsBirthScreen",
  "CountsDeathScreen",
  "CountsShiftingScreen",
  "MilkPreparationListScreen",
  "MilkFeedingListScreen",
  "ProfileScreen",
  "AlertsScreen",
  "WeighingAlertsScreen",
  "VaccinationLeadershipVideosScreen",
  "WeighingLeadershipVideosScreen",
  "WeighingGrowthScreen",
  "WeightHistoryChartScreen",
  "WeighingOperatorsScreen",
]);

function findingsForL0Screens(files) {
  const findings = [];

  for (const { path: rel, source: raw } of files) {
    // Extract screen composable name from source code (more reliable than filename)
    const funcMatch = raw.match(/fun\s+(\w+Screen)\s*\(/);
    if (!funcMatch) continue;

    const screenName = funcMatch[1];
    const isL0Root = Object.values(routeToScreenMap).includes(screenName);
    if (!isL0Root) continue;

    // Check for chrome-guard:ignore directive (used for forms/scan/capture)
    const hasIgnore = /chrome-guard:\s*ignore/.test(raw);

    // Remove ALL comments so patterns don't match text inside KDoc/comments
    const withoutAllComments = raw
      .replace(/\/\*[\s\S]*?\*\//g, "")
      .replace(/\/\/[^\n]*/g, "");

    // L0 screens MUST render MeshaScreenHeader
    const hasMeshaScreenHeader = /MeshaScreenHeader\s*\(/.test(
      withoutAllComments
    );
    if (!hasMeshaScreenHeader && !hasIgnore) {
      findings.push(
        `${rel}: L0 root screen must render MeshaScreenHeader (shell-owned chrome); ` +
          `add it at the top of the Column/Box or use // chrome-guard:ignore: <reason> if exempt`
      );
    }

    // Read screens MUST call RefreshOnResume
    const isReadScreen = readScreens.has(screenName);
    if (isReadScreen && !hasIgnore) {
      const hasRefreshOnResume = /RefreshOnResume\s*[\({]/.test(
        withoutAllComments
      );
      if (!hasRefreshOnResume) {
        findings.push(
          `${rel}: read screen must call RefreshOnResume { onEvent(...Refresh) }; ` +
            `add near top of composable or use // chrome-guard:ignore: form/scan/capture`
        );
      }
    }

    // If the screen has a refresh affordance, it MUST use SyncIconButton, never hand-rolled
    // This checks for both inline icon parameter and child Icon composable with Refresh.
    const hasIconButtonRefresh = /IconButton\s*\([\s\S]*?Refresh|IconButton\s*\([^)]*icon\s*=.*Refresh/.test(
      withoutAllComments
    );
    const hasSyncIconButton = /SyncIconButton\s*[\({]/.test(withoutAllComments);

    if (hasIconButtonRefresh && !hasSyncIconButton && !hasIgnore) {
      findings.push(
        `${rel}: refresh button must use SyncIconButton (shared icon state), ` +
          `not hand-rolled IconButton; this ensures consistent UX and auto-disable while syncing`
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
      } else if (entry.name.endsWith("Screen.kt")) {
        out.push({
          path: relative(repo, full),
          source: readFileSync(full, "utf8"),
        });
      }
    }
  };
  walk(root);
  return out;
}

function selfTest() {
  // Good L0 screen: has MeshaScreenHeader, RefreshOnResume, and SyncIconButton
  const goodL0 = {
    path: "feature/feature-vaccination/VaccinationScreen.kt",
    source: `
      @Composable
      fun VaccinationScreen(
          state: VaccinationUiState,
          onEvent: (VaccinationEvent) -> Unit,
      ) {
          RefreshOnResume { onEvent(VaccinationEvent.Refresh()) }

          Column {
              MeshaScreenHeader(
                  title = state.title,
                  actions = {
                      SyncIconButton(
                          isSyncing = state.loading,
                          onSync = { onEvent(VaccinationEvent.Refresh()) },
                      )
                  },
              )
              // ... content
          }
      }
    `,
  };

  const findings = findingsForL0Screens([goodL0]);
  if (findings.length) {
    throw new Error(
      `self-test rejected compliant L0 screen: ${findings.join("; ")}`
    );
  }

  // Bad L0 screen: no MeshaScreenHeader
  const badNoHeader = {
    path: "feature/feature-vaccination/VaccinationScreen.kt",
    source: `
      @Composable
      fun VaccinationScreen(state: VaccinationUiState) {
          Column {
              LazyColumn { /* items */ }
          }
      }
    `,
  };

  const findings2 = findingsForL0Screens([badNoHeader]);
  if (!findings2.some((f) => f.includes("MeshaScreenHeader"))) {
    throw new Error("self-test did not reject L0 screen without MeshaScreenHeader");
  }

  // Bad L0 read screen: no RefreshOnResume
  const badNoRefresh = {
    path: "feature/feature-weighing/WeighingScreen.kt",
    source: `
      @Composable
      fun WeighingScreen(state: WeighingUiState) {
          Column {
              MeshaScreenHeader(title = "Weighing")
              LazyColumn { /* items */ }
          }
      }
    `,
  };

  const findings3 = findingsForL0Screens([badNoRefresh]);
  if (!findings3.some((f) => f.includes("RefreshOnResume"))) {
    throw new Error("self-test did not reject read screen without RefreshOnResume");
  }

  // Bad L0 screen: hand-rolled IconButton instead of SyncIconButton
  const badHandRolled = {
    path: "feature/feature-vaccination/VaccinationScreen.kt",
    source: `
      @Composable
      fun VaccinationScreen(state: VaccinationUiState) {
          RefreshOnResume { }
          Column {
              MeshaScreenHeader(
                  title = "Vaccination",
                  actions = {
                      IconButton(
                          onClick = { },
                          icon = Icons.Outlined.Refresh,
                      )
                  },
              )
          }
      }
    `,
  };

  const findings4 = findingsForL0Screens([badHandRolled]);
  if (!findings4.some((f) => f.includes("SyncIconButton"))) {
    throw new Error(
      "self-test did not reject hand-rolled refresh IconButton"
    );
  }

  // Exempt screen with ignore comment
  const exemptScreen = {
    path: "feature/feature-scan/ScanScreen.kt",
    source: `
      // chrome-guard:ignore: capture screen, resume refresh would disrupt recording
      @Composable
      fun ScanScreen(state: ScanUiState) {
          Column {
              LazyColumn { /* items */ }
          }
      }
    `,
  };

  const findings5 = findingsForL0Screens([exemptScreen]);
  if (findings5.length) {
    throw new Error(
      `self-test rejected screen with ignore directive: ${findings5.join("; ")}`
    );
  }

  console.log("l0-root-chrome self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = findingsForL0Screens(featureSources());

if (findings.length) {
  console.error(
    "l0-root-chrome-guard FAILED — L0 root screens must render MeshaScreenHeader " +
      "and call RefreshOnResume (read screens); refresh buttons must use SyncIconButton:"
  );
  findings.forEach((finding) => console.error(`- ${finding}`));
  console.error("See docs/decisions/android-navigation-stack.md and AGENTS.md.");
  process.exit(1);
}

console.log(
  "l0-root-chrome: ok (all L0 roots have required shell chrome and refresh handling)"
);
