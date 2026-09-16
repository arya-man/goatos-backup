import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(import.meta.url);
// Exercise the real server page and contract helpers with deterministic API responses.
// Copy comes from the production backend, so missing copy keys fail the render.
const backend = readFileSync(path.join(root, "../../backend/internal/adminui/app/service.go"), "utf8");
const copySection = backend.slice(backend.indexOf('case "alerts":'), backend.indexOf('case "work-board":', backend.indexOf('case "alerts":')));
const copy = Object.fromEntries([...copySection.matchAll(/"([^"\n]+)":\s*("(?:[^"\\]|\\.)*")/g)].map((m) => [m[1], JSON.parse(m[2])]));
const contract = {
  route_id: "alerts", copy,
  controls: [{ id: "configure_alerts", label: "Configure alerts", enabled: false }],
  option_groups: [
    { id: "alerts_parks", options: [{ key: "park", label: "Park" }] },
    { id: "alert_severities", options: [{ key: "critical", label: "Critical" }] },
  ],
  tables: [{ id: "alerts", title: "Alerts", columns: [] }],
};
async function render(data, searchParams = {}, pageContract = contract) {
  const modules = new Map();
  const link = ({ children, href }) => React.createElement("a", { href }, children);
  const mocks = {
    "next/navigation": { redirect: () => { throw new Error("unexpected redirect"); } },
    "@/components/no-prefetch-link": { default: link },
    "@/components/local-overlay-link": { LocalOverlayLink: link },
    "@/components/ui-primitives": { Tag: ({ children }) => React.createElement("span", null, children) },
    "@/lib/api/alerts-server": { listAlerts: async () => data, getAlertRuleConfig: async () => { throw new Error("viewer must not read config"); } },
    "@/lib/api/server": { firstAuthRequiredError: () => undefined },
    "@/lib/auth/session-cookie": { INTERNAL_LOGIN_PATH: "/login" },
    "./alerts-configure": { AlertsConfigure: () => null },
  };
  function load(filename) {
    if (!path.extname(filename)) filename += existsSync(`${filename}.tsx`) ? ".tsx" : ".ts";
    if (modules.has(filename)) return modules.get(filename).exports;
    const loaded = { exports: {} };
    modules.set(filename, loaded);
    const source = ts.transpileModule(readFileSync(filename, "utf8"), {
      compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
    }).outputText;
    vm.runInThisContext(`(function(require,module,exports){${source}\n})`, { filename })(
      (name) => mocks[name] ? { __esModule: true, ...mocks[name] }
        : name.startsWith("@/") ? load(path.join(root, name.slice(2)))
          : name.startsWith(".") ? load(path.resolve(path.dirname(filename), name)) : require(name),
      loaded, loaded.exports,
    );
    return loaded.exports;
  }
  const { AlertsPage } = load(path.join(root, "features/alerts/alerts-page.tsx"));
  return renderToStaticMarkup(await AlertsPage({ searchParams, pageContract }));
}
const clean = { rows: [], total: 0, critical: 0, rules_run: ["pen_feed_quantity_change"] };
for (const [name, data, params, expected] of [
  ["clean", clean, {}, "Every enabled rule ran clean"],
  ["degraded", { ...clean, rules_run: [], degraded: ["pen_feed_quantity_change"] }, {}, "Some checks were not completed"],
  ["skipped", { ...clean, rules_run: [], skipped: [{ key: "pen_feed_quantity_change", label: "Feed", reason: "Missing sheet" }] }, {}, "Some checks were not completed"],
  ["filtered", { ...clean, rows: [{ severity: "warning", rule_key: "r", rule_label: "Rule" }], total: 1 }, { severity: "critical" }, "No alerts match this severity"],
  ["disabled", { ...clean, rules_run: [] }, {}, "No alert rules are switched on"],
]) {
  test(`page renders ${name} empty state`, async () => {
    const html = await render({ ok: true, data }, params);
    assert.ok(html.includes(expected), html);
    if (name !== "clean") assert.doesNotMatch(html, /Every enabled rule ran clean/);
  });
}
test("failed park read never renders an all-clear", async () => {
  const html = await render({ ok: false, error: { code: "alerts_unavailable" } });
  assert.match(html, /Alerts could not be loaded/);
  assert.doesNotMatch(html, /data-testid="alerts-empty"|Every enabled rule ran clean/);
});
test("previous backend copy contract still renders incomplete checks", async () => {
  const oldCopy = { ...copy };
  delete oldCopy["state.empty.incomplete"];
  delete oldCopy["state.empty.filtered"];
  const html = await render({ ok: true, data: { ...clean, degraded: ["r"] } }, {}, { ...contract, copy: oldCopy });
  assert.match(html, /Some checks were not completed/);
});
