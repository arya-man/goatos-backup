// Chart template guards (Ravi 2026-09-27, /sales/sold "Month by month" in dark and the same on many
// pages): a hovered bar painted a big grey column, the tooltip was clipped and showed the raw a11y
// text, every bar carried a value label, bars were fat and neon, axis labels were two-line, and the
// chart wrapper had grown Mesha overrides the template does not have. Every chart must be the
// template's `Chart` + `useChart` (components/minimal/chart, byte-for-byte), with palette colours.
//
// Rules (all P0, wired into scripts/check-design-system.mjs with a self-test case each):
//   chart-wrapper-verbatim  components/minimal/chart/{chart.tsx,use-chart.ts,styles.css} are the
//                           template's exact bytes (sha256 pinned below).
//   chart-data-labels       no `dataLabels: { enabled: true }` / bar `total: { enabled: true }`
//                           in OUR chart code: a figure on every bar is not the template; the
//                           tooltip carries it. (Template sections under components/minimal/ keep
//                           their own options verbatim.)
//   chart-states-override   no `states:` in chart options; useChart's hover/active states stand.
//   chart-bypasses-usechart ApexCharts is imported only by the template wrapper, and every file
//                           that renders <Chart> builds its options with useChart.
//   chart-raw-colour        chart `colors` / `fillColor` are never a raw var(--…), hex or
//                           color-mix() literal: they come from components/app/chart-colors
//                           (theme palette channels), which ApexCharts can do colour maths on.
//   chart-tooltip-css       no stylesheet or sx outside the template chart styles targets
//                           `.apexcharts-*` (custom tooltip/legend CSS clipped and restyled it).
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";

export const CHART_TEMPLATE_CHECKS = {
  "chart-wrapper-verbatim": "components/minimal/chart/chart.tsx, use-chart.ts and styles.css must be the MUI Minimal template's exact bytes (no Mesha overrides: stable-options, var() resolution, tooltip shifting, merged-option tweaks, legend CSS)",
  "chart-data-labels": "ApexCharts data labels (dataLabels.enabled / bar total labels) are off, as in the template's useChart; put the figure in the tooltip and the legend totals, never on every bar",
  "chart-states-override": "chart options must not override `states` (hover/active): useChart's base states stand",
  "chart-bypasses-usechart": "ApexCharts is used only through components/minimal/chart: import Chart + useChart and pass useChart(...) options (never react-apexcharts/apexcharts directly, never a <Chart> without useChart)",
  "chart-raw-colour": "chart colours come from components/app/chart-colors (theme palette channels via chartColor/chartRamp), never a raw var(--…), hex or color-mix() literal in `colors`/`fillColor`",
  "chart-tooltip-css": "only the template chart styles (components/minimal/chart/styles.css) may style .apexcharts-* parts; no page CSS or sx repositions or restyles the tooltip",
};

// sha256 of the template files (~/mesha/mui/Minimal_TypeScript_v7.7.0/next-ts/src/components/chart).
export const CHART_WRAPPER_SHA256 = {
  "components/minimal/chart/chart.tsx": "2859c6002d6f0fdb5539326f9db71b2d6af78b2b537b6621d102b3f6bf63333b",
  "components/minimal/chart/use-chart.ts": "1a2026d3ac8ef50f693c4791786c0c80f3b0f9a0486fa1ddc4ab6468a2264e63",
  "components/minimal/chart/styles.css": "52a15864b4b3e5f7baef6dcedd79621064f035cca9e8122d25342e9da90dfc1f",
};

const TEMPLATE_CHART_DIR = "components/minimal/chart/";
const IMPORTS_CHART = /from\s+["'](?:@\/components\/minimal\/chart|(?:\.\.?\/)+(?:minimal\/)?chart)["']/;
const APEX_IMPORT = /(?:^|\n)\s*import\s+(?!type\b)[^;]*from\s+["'](?:react-apexcharts|apexcharts)["']/;
const DATA_LABELS_ON = /\b(?:dataLabels|total)\s*:\s*\{[^{}]{0,120}?\benabled\s*:\s*true/g;
const STATES = /\bstates\s*:\s*\{/g;
const RAW_COLOUR = /\b(?:colors|fillColor|strokeColor|borderColor)\s*:\s*\[?\s*(?:["'`](?:var\(--|#[0-9a-fA-F]{3,8}\b|color-mix\())/g;
const APEX_CSS = /\.apexcharts-/g;

const lineOf = (text, index) => text.slice(0, index).split("\n").length;
const snippetAt = (text, index) => text.split("\n")[lineOf(text, index) - 1]?.trim() ?? "";
const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " ")).replace(/(^|[^:"'`])\/\/[^\n]*/g, (m, p) => p + " ".repeat(m.length - p.length));

/**
 * @param {string} root admin-web root
 * @param {{ rel: string, abs: string }[]} files every scanned file (code + styles), tests/stories excluded
 * @returns {{ check: string, file: string, line: number, snippet: string }[]}
 */
export function chartTemplateFindings(root, files) {
  const out = [];
  const push = (check, file, line, snippet) => out.push({ check, file, line, snippet });

  for (const [rel, want] of Object.entries(CHART_WRAPPER_SHA256)) {
    const abs = join(root, rel);
    if (!existsSync(abs)) {
      push("chart-wrapper-verbatim", rel, 1, "template chart file is missing");
      continue;
    }
    const got = createHash("sha256").update(readFileSync(abs)).digest("hex");
    if (got !== want) push("chart-wrapper-verbatim", rel, 1, `differs from the template (sha256 ${got.slice(0, 12)}…)`);
  }

  for (const file of files) {
    const rel = file.rel.split("\\").join("/");
    if (rel.startsWith(TEMPLATE_CHART_DIR)) continue;
    const raw = readFileSync(file.abs, "utf8");
    if (rel.endsWith(".css")) {
      for (const m of raw.matchAll(APEX_CSS)) {
        push("chart-tooltip-css", rel, lineOf(raw, m.index), snippetAt(raw, m.index));
        break;
      }
      continue;
    }
    if (!/\.(tsx?|mjs|jsx?)$/.test(rel)) continue;
    const text = stripComments(raw);
    if (APEX_IMPORT.test(text)) {
      const m = APEX_IMPORT.exec(text);
      push("chart-bypasses-usechart", rel, lineOf(text, m.index + 1), snippetAt(raw, m.index + 1));
    }
    for (const m of text.matchAll(/["'`]&?\s*\.apexcharts-/g)) {
      push("chart-tooltip-css", rel, lineOf(text, m.index), snippetAt(raw, m.index));
      break;
    }
    const chartFile = IMPORTS_CHART.test(text) || /\buseChart\s*\(/.test(text);
    if (!chartFile) continue;
    // components/minimal/ is template code, byte-checked against its source by
    // unsourced-minimal-file + review: a template section's own options (AnalyticsConversionRates'
    // bar-end labels) are the reference, not a Mesha override.
    if (rel.startsWith("components/minimal/")) continue;
    if (/<Chart\b/.test(text) && !/\buseChart\s*\(/.test(text)) {
      const i = text.search(/<Chart\b/);
      push("chart-bypasses-usechart", rel, lineOf(text, i), snippetAt(raw, i));
    }
    for (const m of text.matchAll(DATA_LABELS_ON)) push("chart-data-labels", rel, lineOf(text, m.index), snippetAt(raw, m.index));
    for (const m of text.matchAll(STATES)) push("chart-states-override", rel, lineOf(text, m.index), snippetAt(raw, m.index));
    for (const m of text.matchAll(RAW_COLOUR)) push("chart-raw-colour", rel, lineOf(text, m.index), snippetAt(raw, m.index));
  }
  return out;
}

/** Self-test fixtures: one file per rule. */
export const CHART_TEMPLATE_SELFTEST = {
  "components/bad-chart.tsx": [
    'import { Chart, useChart } from "@/components/minimal/chart";',
    "const o = useChart({",
    "  dataLabels: { enabled: true },",
    '  states: { hover: { filter: { type: "none" } } },',
    '  colors: ["var(--brand)"],',
    "});",
  ].join("\n"),
  "components/raw-apex.tsx": 'import ReactApexChart from "react-apexcharts";\nexport const X = ReactApexChart;\n',
  "components/chart.css": ".x .apexcharts-tooltip{position:fixed}\n",
};
