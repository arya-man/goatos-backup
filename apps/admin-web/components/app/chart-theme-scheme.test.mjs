// guard: chart-theme-scheme (N4, TR-2). Chart colours are resolved through useChartTheme(): until a
// component has hydrated and MUI knows the colour scheme, chartColor answers with the scheme-following
// CSS variable, so the server paint (and a late hydration) never shows light-only greens in dark.
// A chart adapter that resolves colours from a bare useTheme() brings the defect back.
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";

const appDir = new URL("../..", import.meta.url).pathname;

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const abs = join(dir, name);
    if (name === "node_modules" || name === "minimal") continue;
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.tsx$/.test(name) && !/\.stories\.tsx$/.test(name)) out.push(abs);
  }
  return out;
}

export function chartThemeFindings(src) {
  const resolves = /\b(chartColor|chartRamp|seriesColor|chartColors)\(\s*theme\b/.test(src);
  return resolves && /\buseTheme\(\)/.test(src) ? ["resolves chart colours from useTheme() instead of useChartTheme()"] : [];
}

test("chart colour callers read the theme through useChartTheme", () => {
  const offenders = [];
  for (const abs of [...walk(join(appDir, "components")), ...walk(join(appDir, "features"))]) {
    for (const f of chartThemeFindings(readFileSync(abs, "utf8"))) offenders.push(`${abs.slice(appDir.length)}: ${f}`);
  }
  assert.deepEqual(offenders, []);
});

test("self-test: a bare useTheme() chart caller is caught, useChartTheme passes", () => {
  assert.equal(chartThemeFindings("const theme = useTheme(); const c = chartColor(theme, 'primary');").length, 1);
  assert.equal(chartThemeFindings("const theme = useChartTheme(); const c = chartColor(theme, 'primary');").length, 0);
});

test("useChartTheme answers with CSS variables until hydrated and the scheme is known", () => {
  const src = readFileSync(join(appDir, "components/app/chart-colors.ts"), "utf8");
  assert.match(src, /useSyncExternalStore\(noSubscribe, \(\) => true, \(\) => false\)/, "per-component hydration snapshot");
  assert.match(src, /return hydrated && colorScheme \? theme : /);
  assert.match(src, /chartSchemeUnresolved\) \{[\s\S]{0,200}return `var\(--palette-\$\{channel\}-/);
});

test("ssr-scheme: the audit scans legend dots, also in the server paint", () => {
  const audit = readFileSync(join(appDir, "scripts/r2-visual-audit.mjs"), "utf8");
  assert.match(audit, /CHART_SCHEME_SCOPE = "\.apexcharts-canvas, \.minimal__chart__legends__item__dot/);
  assert.match(audit, /sctx\.route\(\/\\\/_next\\\/static\\\/chunks\\\/\/, \(r\) => r\.abort\(\)\)/);
});
