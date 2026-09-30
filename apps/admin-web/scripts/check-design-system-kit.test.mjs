// Tests for the MUI Minimal kit/token ratchet in design:guard (scripts/lib/design-kit-ratchet.mjs).
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import {
  cssDeclFindings,
  elementFindings,
  evaluateRatchet,
  fixedOverlayFindings,
  jsxStyleFindings,
  kitStoryFindings,
  menuSurfaceFindings,
  tapTargetFindings,
} from "./lib/design-kit-ratchet.mjs";

const here = dirname(fileURLToPath(import.meta.url));

test("CSS: raw px/radius/shadow/font-size are flagged, tokens pass", () => {
  assert.deepEqual(cssDeclFindings(".a{padding:12px;border-radius:10px;box-shadow:0 4px 8px #000;font-size:13px}"), ["raw-px", "raw-radius", "raw-shadow", "raw-font-size"]);
  assert.deepEqual(cssDeclFindings(".a{padding:var(--sp-2);border-radius:var(--r-md);box-shadow:var(--shadow-card);font-size:var(--fs-body2)}"), []);
  // hairlines, 50% rounds and focus rings are not layout values
  assert.deepEqual(cssDeclFindings(".a{border-radius:50%;margin:1px;box-shadow:var(--shadow-card),inset 0 0 0 1px var(--line)}"), []);
});

test("TSX: inline styles and Tailwind arbitrary values are flagged", () => {
  assert.deepEqual(jsxStyleFindings('<div style={{ fontSize: 13, borderRadius: "8px", padding: "12px" }} />').sort(), ["raw-font-size", "raw-px", "raw-radius"]);
  // J1B P2-1 (FIXJ7): numbers on borderRadius / padding / margin / gap are the theme scale (shape x n,
  // spacing x n), the values css-var-token moves var(--r-*) / var(--sp-*) reads onto.
  assert.deepEqual(jsxStyleFindings('<Box sx={{ borderRadius: 1.5, gap: 3, padding: 2, marginTop: 4 }} />'), []);
  assert.deepEqual(jsxStyleFindings('<div className="text-[13px] rounded-[10px] p-[12px]" />').sort(), ["raw-font-size", "raw-px", "raw-radius"]);
  assert.deepEqual(jsxStyleFindings('<div style={{ padding: "var(--sp-2)", gap: 2 }} />'), []);
});

test("raw elements are flagged in feature code but not in the kit that wraps them", () => {
  const code = '<table><button role="tablist" /></table><span role="tooltip" className="chip" />';
  assert.deepEqual(elementFindings(code, "features/x/y.tsx").sort(), ["raw-button", "raw-chip", "raw-table", "raw-tabs", "raw-tooltip"]);
  assert.deepEqual(elementFindings(code, "components/kit/card.tsx"), []);
  assert.deepEqual(elementFindings('<span className="wf-chip" />', "features/x.tsx"), []);
  assert.deepEqual(elementFindings('<input value={v} />', "features/x.tsx"), ["raw-input"]);
  assert.deepEqual(elementFindings('<input type="checkbox" />', "features/x.tsx"), ["raw-checkbox"]);
  assert.deepEqual(elementFindings('<input type="hidden" />', "features/x.tsx"), []);
  assert.deepEqual(elementFindings('<div className="alert warn" />', "features/x.tsx"), ["raw-alert"]);
  assert.deepEqual(elementFindings('<span className="av" />', "features/x.tsx"), ["raw-avatar"]);
});

test("menus, listboxes and popovers must wear the kit dropdown surface", () => {
  const f = "features/x/y.tsx";
  // role=menu / role=listbox on a hand-rolled panel, even when role sits on its own line
  assert.equal(menuSurfaceFindings('<div className="parkmenu" role="menu">\n<a />\n</div>', f).length, 1);
  assert.equal(menuSurfaceFindings('<ul\n  id={id}\n  role="listbox"\n  style={{ top: 0 }}\n>\n<li /></ul>', f).length, 1);
  // a class that names a menu or popover, with no role
  assert.equal(menuSurfaceFindings('<div className="lt-fdrop-pop">x</div>', f).length, 1);
  assert.equal(menuSurfaceFindings('<div className={`avmenu ${open ? "on" : ""}`}>x</div>', f).length, 1);
  // the same panels as template dropdowns pass
  assert.equal(menuSurfaceFindings('<CustomPopover open={o} anchorEl={a} onClose={c}>\n<MenuList>\n<MenuItem />\n</MenuList>\n</CustomPopover>', f).length, 0);
  assert.equal(menuSurfaceFindings('<MenuList\n  role="listbox"\n  id={id}\n>\n<MenuItem /></MenuList>', f).length, 0);
  assert.equal(menuSurfaceFindings('<DropdownPaper className="lt-fdrop-pop" role="group">x</DropdownPaper>', f).length, 0);
  assert.equal(menuSurfaceFindings('<Menu open role="menu" className="proc-columns-pop">x</Menu>', f).length, 0);
  // one finding per tag even when both role and class match; titles/rows inside are not panels
  assert.equal(menuSurfaceFindings('<div className="avmenu" role="listbox">\n<b className="role-menu-title" /></div>', f).length, 1);
  assert.equal(menuSurfaceFindings('<span className="lt-menu-col" />', f).length, 0);
  // a list nested inside a template dropdown is on it (until the dropdown closes); a tooltip is not a menu
  assert.equal(menuSurfaceFindings('<CustomPopover open={o}>\n<div className="avq" />\n<div className="list" role="listbox">x</div></CustomPopover>', f).length, 0);
  assert.equal(menuSurfaceFindings('<CustomPopover open={o}>x</CustomPopover>\n<div className="list" role="listbox">x</div>', f).length, 1);
  assert.equal(menuSurfaceFindings('<span className="info-pop" role="tooltip">i</span>', f).length, 0);
  assert.equal(menuSurfaceFindings('<div className="wb ltd-statusmenu">x</div>', f).length, 0);
  // the kit owns the primitive
  assert.equal(menuSurfaceFindings('<ul role="listbox" className="kit-menu" />', "components/kit/example.tsx").length, 0);
});

test("fixed overlays must portal", () => {
  assert.equal(fixedOverlayFindings('<div style={{ position: "fixed" }} />', "features/a.tsx").length, 1);
  assert.equal(fixedOverlayFindings('<BodyPortal><div style={{ position: "fixed" }} /></BodyPortal>', "features/a.tsx").length, 0);
});

test("phone tap targets under 44px inside a phone media query", () => {
  const css = ".btn{min-height:32px}\n@media (max-width:600px){\n.btn{min-height:32px}\n.iconbtn{height:44px}\n}\n.btn{height:30px}\n";
  const hits = tapTargetFindings(css);
  assert.equal(hits.length, 1);
  assert.equal(hits[0].line, 3);
});

test("kit component without a story is flagged", () => {
  const root = mkdtempSync(join(tmpdir(), "kit-story-"));
  mkdirSync(join(root, "components/kit"), { recursive: true });
  mkdirSync(join(root, "stories/kit"), { recursive: true });
  writeFileSync(join(root, "components/kit/a.tsx"), "export function Alpha() { return null; }\n");
  writeFileSync(join(root, "components/kit/b.tsx"), "export function Beta() { return null; }\n");
  writeFileSync(join(root, "stories/kit/A.stories.tsx"), 'import { Alpha } from "@/components/kit";\nexport const S = { render: () => <Alpha /> };\n');
  const hits = kitStoryFindings(root);
  rmSync(root, { recursive: true, force: true });
  assert.deepEqual(hits.map((h) => h.file), ["components/kit/b.tsx"]);
});

test("ratchet: new file fails at 1, legacy file fails only above its allowance, never raised", () => {
  const f = (check, file) => ({ check, file, line: 1, snippet: "x" });
  const allow = { "raw-px|old.css": { allowed: 2, reason: "legacy" } };
  let r = evaluateRatchet([f("raw-px", "old.css"), f("raw-px", "old.css")], allow, { seed: false });
  assert.equal(r.over.length, 0);
  r = evaluateRatchet([f("raw-px", "old.css"), f("raw-px", "old.css"), f("raw-px", "old.css")], allow, { seed: false });
  assert.equal(r.over.length, 1);
  assert.equal(r.nextBaseline["raw-px|old.css"].allowed, 2, "update-baseline must never raise an allowance");
  r = evaluateRatchet([f("raw-px", "new.css")], allow, { seed: false });
  assert.equal(r.over[0].key, "raw-px|new.css");
  assert.equal(r.nextBaseline["raw-px|new.css"], undefined, "update-baseline must not silently waive a new file");
  r = evaluateRatchet([f("raw-px", "old.css")], allow, { seed: false });
  assert.equal(r.shrinkable.length, 1);
  assert.equal(r.nextBaseline["raw-px|old.css"].allowed, 1);
});

test("design:guard self-test covers every check", () => {
  const out = spawnSync(process.execPath, [join(here, "check-design-system.mjs"), "--self-test"], { encoding: "utf8" });
  assert.equal(out.status, 0, out.stderr + out.stdout);
});

test("every ratchet allowance carries a reason", async () => {
  const { readFileSync } = await import("node:fs");
  const waivers = JSON.parse(readFileSync(join(here, "check-design-system-waivers/design-system-waivers.json"), "utf8"));
  for (const [key, entry] of Object.entries(waivers.ratchet ?? {})) {
    assert.ok(entry.reason && entry.reason.length > 20, `ratchet ${key} has no reason`);
    assert.ok(Number.isInteger(entry.allowed) && entry.allowed > 0, `ratchet ${key} allowance must be a positive count`);
  }
});
