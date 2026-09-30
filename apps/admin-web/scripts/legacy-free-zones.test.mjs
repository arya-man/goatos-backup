// Self-test + live run of the legacy-free-zones guard (scripts/lib/legacy-free-zones.mjs).
import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { legacyFreeZoneFindings, legacyZoneFindingsFor, readZones } from "./lib/legacy-free-zones.mjs";

const appDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");

test("a legacy-free zone file refuses every legacy construct", () => {
  const selectors = new Set(["card", "hd", "btn", "qcard"]);
  const src = [
    'import { X } from "lucide-react";',
    'import "./panel.css";',
    'import styles from "./chrome.module.css";',
    '<div className="card"><div className="hd" /></div>',
    '<Box className={cx("qcard", on && "btn")} />',
    '<div style={{ padding: 12 }} />',
    '<button onClick={go}>Go</button>',
    '<input value={v} />',
    '<input type="text" name="deadline_hour" />',
    "<select\n  value={v}>",
    '<textarea />',
    '<table><tr><td>1</td></tr></table>',
    'const c = "#ff0000"; const d = "rgba(0,0,0,.5)";',
    '<style>{`.x{color:red}`}</style>',
    '<GlobalStyles styles={`.y{margin:0}`} />',
  ].join("\n");
  const found = legacyZoneFindingsFor("features/x.tsx", src, selectors).map((f) => f.snippet);
  for (const what of ['"card"', '"hd"', '"qcard"', '"btn"', "style=", "<button>", "<input>", "<select>", "<textarea>", "<table>", "<tr>", "<td>", "lucide", "stylesheet import", "hex colour", "rgb()"]) {
    assert.ok(found.some((s) => s.includes(what)), `expected a finding for ${what}; got ${found.join(" | ")}`);
  }
  assert.equal(found.filter((s) => s.includes("stylesheet import")).length, 2);
  assert.equal(found.filter((s) => s.includes("embedded stylesheet")).length, 2);
});

test("template-clean code, comments, a hidden file input and a hidden form field pass", () => {
  const src = [
    "// className=\"card\" in a comment, <button> too, style={{}}",
    "/* #ff0000 */",
    '<Card sx={{ p: 3 }}><CardHeader title="t" /></Card>',
    '<Button component="label">Upload<input hidden type="file" onChange={f} /></Button>',
    '<input type="hidden" name="task_id" value={id} />',
    '<input ref={keyRef} type="hidden" name="idempotency_key" />',
    '<Box className="studio-anchor" data-x="#1" />',
    'const url = "https://example.com/#section";',
  ].join("\n");
  assert.deepEqual(legacyZoneFindingsFor("features/y.tsx", src, new Set(["card"])), []);
});

test("a zone that no longer exists is refused", () => {
  const root = mkdtempSync(join(tmpdir(), "lfz-"));
  mkdirSync(join(root, "scripts"), { recursive: true });
  writeFileSync(join(root, "scripts/legacy-free-zones.json"), JSON.stringify({ zones: ["features/gone/"] }));
  const found = legacyFreeZoneFindings(root);
  assert.equal(found.length, 1);
  assert.match(found[0].snippet, /does not exist/);
});

test("reads legacy selectors from the stylesheets and flags a zone file using one", () => {
  const root = mkdtempSync(join(tmpdir(), "lfz-"));
  const put = (rel, text) => {
    mkdirSync(join(root, dirname(rel)), { recursive: true });
    writeFileSync(join(root, rel), text);
  };
  put("app/mesha-theme.css", ".qcard .qhead{padding:1.5rem;transition:all .5s}\n");
  put("features/sops/a.tsx", '<div className="qhead" />\n');
  put("features/sops/b.tsx", '<Card sx={{ p: 1 }} />\n');
  put("scripts/legacy-free-zones.json", JSON.stringify({ zones: ["features/sops/"] }));
  const found = legacyFreeZoneFindings(root);
  assert.deepEqual(found.map((f) => f.file), ["features/sops/a.tsx"]);
});

test("every listed zone in the app is legacy-free", () => {
  assert.ok(Array.isArray(readZones(appDir)));
  const found = legacyFreeZoneFindings(appDir);
  assert.deepEqual(found.map((f) => `${f.file}:${f.line} ${f.snippet}`), []);
});
