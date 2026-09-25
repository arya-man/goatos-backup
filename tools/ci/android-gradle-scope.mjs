#!/usr/bin/env node
// android-gradle-scope.mjs — which Gradle tasks does THIS diff need at landing?
//
// The landing gate used to run `:app:compileStgReleaseKotlin :app:testStgReleaseUnitTest
// :app:lintStgRelease` with --max-workers=1 for every Android diff (PR #404: 1814 s of a
// 43-min landing). The 20-min land-main cap (docs/progress/ci-deploy-speedup.md) scopes it:
//
//   compile  :app:compileStgReleaseKotlin, always (compiles every module :app depends on)
//   unit     :app:testStgReleaseUnitTest, always; plus :<m>:testReleaseUnitTest for every
//            CHANGED library module and every library module that DEPENDS on one
//            (transitively), when that module has src/test
//   lint     :app:lintStgRelease only when app/**, core-designsystem, any res/ dir, or
//            build logic changed; otherwise :<m>:lintRelease for each changed library module
//
// Build-logic changes (root/any *.gradle.kts, gradle.properties, gradle/**, buildSrc/**,
// settings) are build-wide: every module is "changed". `--full` (MODE=all / the nightly
// full-suite workflow) returns every module's unit + lint plus :app's.
//
// Usage: git diff --name-only BASE...HEAD | node tools/ci/android-gradle-scope.mjs [--full]
//        node tools/ci/android-gradle-scope.mjs --self-test
// Prints the task list on one line (space separated) and a `# reason` line on stderr.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const defaultRoot = path.resolve(here, "../../apps/goatos-android");
const PREFIX = "apps/goatos-android/";
const NOT_LIBRARIES = new Set([":app", ":benchmark", ":journeys"]);

export function readGraph(root) {
  const settings = fs.readFileSync(path.join(root, "settings.gradle.kts"), "utf8");
  const modules = [...settings.matchAll(/include\("([^"]+)"\)/g)].map((m) => m[1]);
  const deps = new Map();
  const hasTests = new Map();
  for (const m of modules) {
    const dir = path.join(root, ...m.split(":").filter(Boolean));
    let text = "";
    try { text = fs.readFileSync(path.join(dir, "build.gradle.kts"), "utf8"); } catch { /* no build file */ }
    deps.set(m, new Set([...text.matchAll(/project\("([^"]+)"\)/g)].map((x) => x[1])));
    hasTests.set(m, fs.existsSync(path.join(dir, "src", "test")));
  }
  return { modules, deps, hasTests };
}

function moduleDir(m) { return m.split(":").filter(Boolean).join("/") + "/"; }

export function isBuildLogic(rel) {
  return /(^|\/)[^/]*\.gradle\.kts$/.test(rel) || rel === "gradle.properties" ||
    rel.startsWith("gradle/") || rel.startsWith("buildSrc/") || rel.startsWith("build-logic/");
}

export function scope(changedFiles, graph, { full = false } = {}) {
  const { modules, deps, hasTests } = graph;
  const libs = modules.filter((m) => !NOT_LIBRARIES.has(m));
  const rels = changedFiles.filter((f) => f.startsWith(PREFIX)).map((f) => f.slice(PREFIX.length));
  const buildWide = full || rels.some(isBuildLogic);
  // Longest module dir wins (core/core-ui/ vs core/).
  const byDir = [...modules].sort((a, b) => moduleDir(b).length - moduleDir(a).length);
  const changed = new Set();
  if (buildWide) modules.forEach((m) => changed.add(m));
  for (const r of rels) {
    const m = byDir.find((x) => r.startsWith(moduleDir(x)));
    if (m) changed.add(m);
  }
  // Reverse-dependency closure over library modules.
  const affected = new Set(changed);
  let grew = true;
  while (grew) {
    grew = false;
    for (const m of libs) {
      if (affected.has(m)) continue;
      for (const d of deps.get(m) || []) if (affected.has(d)) { affected.add(m); grew = true; break; }
    }
  }
  const tasks = [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest"];
  for (const m of libs) if (affected.has(m) && hasTests.get(m)) tasks.push(`${m}:testReleaseUnitTest`);
  const appLint = buildWide || changed.has(":app") || changed.has(":core:core-designsystem") ||
    rels.some((r) => /(^|\/)src\/[^/]+\/res\//.test(r));
  if (appLint) tasks.push(":app:lintStgRelease");
  const lintLibs = full ? libs : libs.filter((m) => changed.has(m));
  if (full || !appLint) for (const m of lintLibs) tasks.push(`${m}:lintRelease`);
  return {
    tasks,
    reason: `android scope: ${full ? "FULL" : buildWide ? "build-wide" : `changed=[${[...changed].join(",") || "none"}]`} ` +
      `affected-libs=${[...affected].filter((m) => !NOT_LIBRARIES.has(m)).length} appLint=${appLint}`,
  };
}

function selfTest() {
  const g = {
    modules: [":app", ":benchmark", ":core:core-model", ":core:core-ui", ":core:core-designsystem", ":feature:feature-a", ":feature:feature-b"],
    deps: new Map([
      [":app", new Set([":core:core-model", ":core:core-ui", ":feature:feature-a", ":feature:feature-b"])],
      [":benchmark", new Set()],
      [":core:core-model", new Set()],
      [":core:core-ui", new Set([":core:core-model", ":core:core-designsystem"])],
      [":core:core-designsystem", new Set()],
      [":feature:feature-a", new Set([":core:core-ui"])],
      [":feature:feature-b", new Set([":core:core-model"])],
    ]),
    hasTests: new Map([[":core:core-model", true], [":core:core-ui", false], [":feature:feature-a", true], [":feature:feature-b", true], [":core:core-designsystem", false]]),
  };
  const P = (s) => PREFIX + s;
  const cases = [
    ["no android diff -> app compile+unit only", ["backend/x.go"], {}, [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest"]],
    ["feature code -> its tests + its lint, no app lint", [P("feature/feature-a/src/main/kotlin/A.kt")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":feature:feature-a:testReleaseUnitTest", ":feature:feature-a:lintRelease"]],
    ["core-model -> dependents' tests (transitive), lint core-model only", [P("core/core-model/src/main/kotlin/M.kt")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":core:core-model:testReleaseUnitTest", ":feature:feature-a:testReleaseUnitTest", ":feature:feature-b:testReleaseUnitTest", ":core:core-model:lintRelease"]],
    ["app code -> app lint", [P("app/src/main/kotlin/X.kt")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":app:lintStgRelease"]],
    ["resources -> app lint", [P("feature/feature-b/src/main/res/values/strings.xml")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":feature:feature-b:testReleaseUnitTest", ":app:lintStgRelease"]],
    ["design system -> app lint + dependents' tests", [P("core/core-designsystem/src/main/kotlin/T.kt")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":feature:feature-a:testReleaseUnitTest", ":app:lintStgRelease"]],
    ["build logic -> every test + app lint", [P("gradle/libs.versions.toml")], {},
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":core:core-model:testReleaseUnitTest", ":feature:feature-a:testReleaseUnitTest", ":feature:feature-b:testReleaseUnitTest", ":app:lintStgRelease"]],
    ["full -> every test + every lint", [], { full: true },
      [":app:compileStgReleaseKotlin", ":app:testStgReleaseUnitTest", ":core:core-model:testReleaseUnitTest", ":feature:feature-a:testReleaseUnitTest", ":feature:feature-b:testReleaseUnitTest", ":app:lintStgRelease",
        ":core:core-model:lintRelease", ":core:core-ui:lintRelease", ":core:core-designsystem:lintRelease", ":feature:feature-a:lintRelease", ":feature:feature-b:lintRelease"]],
  ];
  let bad = 0;
  for (const [name, files, opts, want] of cases) {
    const got = scope(files, g, opts).tasks;
    const ok = JSON.stringify(got) === JSON.stringify(want);
    if (!ok) { bad++; console.error(`FAIL ${name}\n  want ${want.join(" ")}\n  got  ${got.join(" ")}`); }
    else console.log(`ok   ${name}`);
  }
  // The real graph must parse and include :app plus library modules.
  const real = readGraph(defaultRoot);
  if (!real.modules.includes(":app") || real.modules.length < 10) { bad++; console.error("FAIL real settings.gradle.kts did not parse"); }
  if (bad) { console.error(`android-gradle-scope self-test: ${bad} failure(s)`); process.exit(1); }
  console.log("android-gradle-scope self-test: passed");
}

const args = process.argv.slice(2);
if (args.includes("--self-test")) {
  selfTest();
} else {
  const input = fs.readFileSync(0, "utf8").split("\n").map((s) => s.trim()).filter(Boolean);
  const { tasks, reason } = scope(input, readGraph(defaultRoot), { full: args.includes("--full") });
  console.error(reason);
  console.log(tasks.join(" "));
}
