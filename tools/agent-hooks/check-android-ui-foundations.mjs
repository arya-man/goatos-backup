#!/usr/bin/env node
// Android UI-foundation regression guard.
//
// This complements Android lint and screenshot tests with checks for Goat OS architecture
// invariants that generic tools cannot infer: backend-composed start navigation, durable scan
// restoration, exclusive camera presentation/release, and minimum custom touch targets.

import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const root = process.cwd();
const sourceRoot = process.env.GOATOS_GUARD_SOURCE_ROOT || root;
const productionRoots = ["apps/goatos-android"];

function walk(dir, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "build") walk(full, out);
    } else if (entry.name.endsWith(".kt") && full.includes(`${path.sep}src${path.sep}main${path.sep}`)) {
      out.push(full);
    }
  }
  return out;
}

function lineNumber(text, index) {
  return text.slice(0, index).split("\n").length;
}

function callBodies(text, functionName) {
  const bodies = [];
  const start = new RegExp(`\\b${functionName}\\s*\\(`, "g");
  for (const match of text.matchAll(start)) {
    const open = text.indexOf("(", match.index);
    let depth = 1;
    let quote = null;
    let escaped = false;
    for (let index = open + 1; index < text.length; index += 1) {
      const char = text[index];
      if (quote !== null) {
        if (escaped) escaped = false;
        else if (char === "\\") escaped = true;
        else if (char === quote) quote = null;
        continue;
      }
      if (char === '"') {
        quote = char;
      } else if (char === "(") {
        depth += 1;
      } else if (char === ")") {
        depth -= 1;
        if (depth === 0) {
          bodies.push({ index: match.index, body: text.slice(open + 1, index) });
          break;
        }
      }
    }
  }
  return bodies;
}

function genericFindings(rel, text) {
  const findings = [];

  // A custom clickable whose own declared size/height/width is below Android's 48dp minimum.
  // Inner decorative icons are legal because they do not own the clickable modifier.
  const undersized = /\.(size|height|width)\(\s*(\d+(?:\.\d+)?)\.dp\s*\)([\s\S]{0,220}?)\.clickable\s*\(/g;
  for (const match of text.matchAll(undersized)) {
    const dp = Number(match[2]);
    const chain = match[0];
    if (dp < 48 && !chain.includes("minimumInteractiveComponentSize") && !match[3].includes("Modifier")) {
      findings.push({
        rel,
        line: lineNumber(text, match.index ?? 0),
        message: `custom clickable declares ${dp}dp ${match[1]}; interactive targets must be at least 48dp`,
      });
    }
  }

  // Text links/chips often look acceptable while exposing only the glyph bounds to touch and
  // accessibility services. Require the Material minimum-target modifier on the same Text call.
  for (const call of callBodies(text, "Text")) {
    if (/\.clickable\s*\(/.test(call.body) && !/\.minimumInteractiveComponentSize\s*\(\s*\)/.test(call.body)) {
      findings.push({
        rel,
        line: lineNumber(text, call.index),
        message: "clickable Text must apply minimumInteractiveComponentSize()",
      });
    }
  }

  if (/contentDescription\s*:\s*String\s*=\s*"Button"/.test(text)) {
    findings.push({ rel, line: 1, message: "generic accessibility label 'Button' is forbidden" });
  }
  if (/import\s+kotlinx\.coroutines\.GlobalScope\b/.test(text)) {
    findings.push({ rel, line: 1, message: "GlobalScope is forbidden; bind coroutines to an owned lifecycle" });
  }
  if (/import\s+kotlinx\.coroutines\.runBlocking\b/.test(text)) {
    findings.push({ rel, line: 1, message: "runBlocking is forbidden in production Android code" });
  }
  if (/\.collectAsState\s*\(/.test(text)) {
    findings.push({ rel, line: 1, message: "use collectAsStateWithLifecycle() for UI Flow collection" });
  }
  return findings;
}

function requirePatterns(rel, text, requirements) {
  const findings = [];
  for (const [pattern, message] of requirements) {
    if (!pattern.test(text)) findings.push({ rel, line: 1, message });
  }
  return findings;
}

function scaffoldInsetFindings(rel, text) {
  return requirePatterns(rel, text, [
    [
      /\.padding\(padding\)[\s\S]{0,700}\.consumeWindowInsets\(padding\)/,
      "Scaffold innerPadding must be consumed before composing child routes so system bars are not applied twice",
    ],
  ]);
}

function rfidDispatchFindings(rel, text) {
  const findings = requirePatterns(rel, text, [
    [/override\s+fun\s+dispatchKeyEvent\s*\(/, "RFID keyboard-wedge input must be intercepted before Compose view dispatch"],
    [/dispatchRfidFirst\s*\(/, "RFID dispatch order must remain independently regression-testable"],
  ]);
  if (/override\s+fun\s+onKey(?:Down|Up)\s*\(/.test(text)) {
    findings.push({
      rel,
      line: 1,
      message: "MainActivity must not rely on onKeyDown/onKeyUp for RFID Enter; Compose may consume the terminator first",
    });
  }
  return findings;
}

function architectureFindings(read = (rel) => fs.readFileSync(path.join(sourceRoot, rel), "utf8")) {
  const findings = [];
  const shell = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/GoatOsShell.kt";
  findings.push(...requirePatterns(shell, read(shell), [
    [/startDestination\s*=\s*startDestinationFor\(navState\)/, "app start destination must be composed from backend-visible navigation"],
  ]));
  findings.push(...scaffoldInsetFindings(shell, read(shell)));

  const mainActivity = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/MainActivity.kt";
  findings.push(...rfidDispatchFindings(mainActivity, read(mainActivity)));

  const scanVm = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt";
  findings.push(...requirePatterns(scanVm, read(scanVm), [
    [/observeScannedTags\(/, "scan completion must restore from durable Room capture rows"],
    [/persistedScanDone[\s\S]*_localDone/, "rendered scan completion must merge persisted and in-process state"],
  ]));

  const formSpec = "apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/forms/FormSpec.kt";
  findings.push(...requirePatterns(formSpec, read(formSpec), [
    [/"select",\s*"single_select"\s*->\s*SELECT/, "backend select fields must map to a supported renderer"],
    [/"date_time",\s*"datetime",\s*"timestamp"\s*->\s*DATE_TIME/, "backend date-time fields must map to a supported renderer"],
  ]));

  const submitVm = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/SubmitViewModel.kt";
  findings.push(...requirePatterns(submitVm, read(submitVm), [
    [/FormFieldType\.BOOLEAN\s*->\s*\(answer as\? JsonPrimitive\)\?\.booleanOrNull\s*!=\s*null/, "explicit false must count as an answered required boolean"],
    [/FormRuleType\.BLOCK_SUBMISSION_IF[\s\S]{0,180}conditionMatches\(\)/, "backend block-submission rules must be evaluated before enqueue"],
    [/FormRuleType\.REQUIRED_IF[\s\S]{0,180}conditionMatches\(\)/, "backend conditional-required rules must be evaluated before enqueue"],
    [/it\.goatId\?\.takeIf\(String::isNotBlank\)\s*\?:\s*it\.tag/, "resolved vaccination scans must submit canonical goat ids rather than RFID text"],
  ]));

  const launcher = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/VideoCaptureLauncher.kt";
  findings.push(...requirePatterns(launcher, read(launcher), [
    [/Dialog\s*\(/, "camera proof capture must use an exclusive full-screen dialog"],
    [/dismissOnBackPress\s*=\s*false/, "camera dialog back handling must remain explicit"],
    [/dismissOnClickOutside\s*=\s*false/, "camera dialog must not expose an outside-dismiss path"],
    [/usePlatformDefaultWidth\s*=\s*false/, "camera dialog must occupy the full window"],
    [/decorFitsSystemWindows\s*=\s*false/, "camera dialog must own edge-to-edge insets"],
  ]));

  const recorder = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/InAppVideoRecorder.kt";
  findings.push(...requirePatterns(recorder, read(recorder), [
    [/bindToLifecycle\(/, "CameraX use cases must be lifecycle-bound"],
    [/onRelease\s*=\s*\{[\s\S]*cameraSession\.release\(\)/, "AndroidView release must unbind CameraX"],
    [/onDispose\s*\{[\s\S]*cameraSession\.release\(\)/, "composition disposal must unbind CameraX"],
    [/minimumInteractiveComponentSize\(\)/, "camera controls must enforce Material minimum interaction size"],
    [/contentDescription\s*=\s*actionDescription/, "record control must expose a meaningful action description"],
    [/role\s*=\s*Role\.Button/, "custom record control must expose button semantics"],
  ]));

  const benchmark = "apps/goatos-android/benchmark/src/main/kotlin/sg/mesha/goatos/benchmark/GoatOsBenchmark.kt";
  findings.push(...requirePatterns(benchmark, read(benchmark), [
    [/StartupTimingMetric\s*\(/, "Macrobenchmark must measure cold-start timing"],
    [/FrameTimingMetric\s*\(/, "Macrobenchmark must measure frame timing"],
    [/BaselineProfileRule\s*\(/, "a Baseline Profile generator must cover the primary path"],
  ]));

  const appBuild = "apps/goatos-android/app/build.gradle.kts";
  findings.push(...requirePatterns(appBuild, read(appBuild), [
    [/debugImplementation\(libs\.leakcanary\.android\)/, "debug builds must include LeakCanary"],
  ]));
  const debugManifest = "apps/goatos-android/app/src/debug/AndroidManifest.xml";
  findings.push(...requirePatterns(debugManifest, read(debugManifest), [
    [/LeakCanaryBridgeInstaller/, "debug manifest must install the LeakCanary reporting bridge"],
  ]));

  const composeMetricBuilds = [
    "apps/goatos-android/core/core-ui/build.gradle.kts",
    ...["auth", "calendar", "leadership", "profile", "record", "scan", "sheds", "submit", "timetable", "verify"]
      .map((name) => `apps/goatos-android/feature/feature-${name}/build.gradle.kts`),
  ];
  for (const buildFile of composeMetricBuilds) {
    findings.push(...requirePatterns(buildFile, read(buildFile), [
      [/metricsDestination\s*=/, "Compose compiler metrics must be enabled"],
      [/reportsDestination\s*=/, "Compose compiler reports must be enabled"],
    ]));
  }
  return findings;
}

function runSelfTest() {
  const bad = `
    Box(Modifier.size(38.dp).clip(CircleShape).clickable(onClick = onClose))
    Text("Retry", modifier = Modifier.clickable(onClick = retry))
    fun Thing(contentDescription: String = "Button") = Unit
    import kotlinx.coroutines.GlobalScope
    state.collectAsState()
  `;
  const good = `
    Box(Modifier.size(48.dp).clip(CircleShape).clickable(onClick = onClose))
    Text("Retry", modifier = Modifier.minimumInteractiveComponentSize().clickable(onClick = retry))
    Icon(thing, contentDescription = "Refresh")
  `;
  const badFindings = genericFindings("Bad.kt", bad);
  const goodFindings = genericFindings("Good.kt", good);
  const badInsets = scaffoldInsetFindings("BadShell.kt", "Column(Modifier.padding(padding)) { content() }");
  const goodInsets = scaffoldInsetFindings(
    "GoodShell.kt",
    "Column(Modifier.padding(padding).consumeWindowInsets(padding)) { content() }",
  );
  const badRfid = rfidDispatchFindings(
    "BadMainActivity.kt",
    "override fun onKeyDown(keyCode: Int, event: KeyEvent) = reader.onKeyEvent(event)",
  );
  const goodRfid = rfidDispatchFindings(
    "GoodMainActivity.kt",
    "override fun dispatchKeyEvent(event: KeyEvent) = dispatchRfidFirst({ reader.onKeyEvent(event) }, { super.dispatchKeyEvent(event) })",
  );
  if (
    badFindings.length !== 5 || goodFindings.length !== 0 ||
    badInsets.length !== 1 || goodInsets.length !== 0 ||
    badRfid.length !== 3 || goodRfid.length !== 0
  ) {
    throw new Error(
      `self-test failed: bad=${JSON.stringify(badFindings)} good=${JSON.stringify(goodFindings)} ` +
        `badInsets=${JSON.stringify(badInsets)} goodInsets=${JSON.stringify(goodInsets)} ` +
        `badRfid=${JSON.stringify(badRfid)} goodRfid=${JSON.stringify(goodRfid)}`,
    );
  }
  console.log("android-ui-foundations guard self-test passed");
}

if (process.argv.includes("--self-test")) {
  runSelfTest();
  process.exit(0);
}

const files = productionRoots.flatMap((dir) => walk(path.join(sourceRoot, dir)));
const findings = files.flatMap((file) => {
  const rel = path.relative(sourceRoot, file);
  return genericFindings(rel, fs.readFileSync(file, "utf8"));
});
findings.push(...architectureFindings());

if (findings.length > 0) {
  console.error("android-ui-foundations guard FAILED:");
  for (const finding of findings) console.error(`  ${finding.rel}:${finding.line}: ${finding.message}`);
  process.exit(1);
}
console.log(`android-ui-foundations guard passed (${files.length} production Kotlin files)`);
