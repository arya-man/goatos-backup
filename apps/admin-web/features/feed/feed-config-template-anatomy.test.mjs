// Guard: feed-config-template-anatomy (TR1 /feed/config). Feed Config writes sit on template anatomy:
// every editor opens the template quick-edit Dialog (never an inline form growing in a table cell or
// card header), declared feeds are soft Chips with a delete, adds are template Buttons, and the
// session plan edit is the CardHeader action, not a lone pencil row under the header.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const editor = readFileSync(new URL("./feed-config-editor.tsx", import.meta.url), "utf8");
const page = readFileSync(new URL("./feed-config.tsx", import.meta.url), "utf8");
const gridTable = readFileSync(new URL("./ration-grid-table.tsx", import.meta.url), "utf8");
const rateValue = readFileSync(new URL("./feed-rate-optimistic.tsx", import.meta.url), "utf8");

/** Legacy markup a template page must not carry (REVIEW-36 O47/O48). */
export function legacyViolations(src) {
  const out = [];
  for (const [re, what] of [
    [/feed-table|feed-scroll/, "legacy .feed-table / .feed-scroll class"],
    [/className="fld"/, "legacy .fld wrapper"],
    [/<label\b/, "raw <label> (use TextField label)"],
    [/className="(?:small|muted)[^"]*"/, "legacy small / muted text class"],
    [/style=\{\{/, "inline style (use sx)"],
    [/<TableHead>/, "bare TableHead (use TableHeadCustom)"],
  ]) if (re.test(src.replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, ""))) out.push(what);
  for (const m of src.matchAll(/<Alert\b[\s\S]*?<\/Alert>/g)) {
    if (/<Table\b/.test(m[0])) out.push("table inside an Alert (alerts hold text only)");
    if (/<(?:ul|ol|li|List)\b|component="(?:ul|ol)"/.test(m[0])) out.push("list inside an Alert (alerts hold text only)");
  }
  if (/className="tag\b/.test(src)) out.push("legacy .tag chip");
  return out;
}

export function shellViolations(src) {
  const out = [];
  const shell = src.slice(src.indexOf("function FeedConfigFormShell("), src.indexOf("function Outcome("));
  if (!/<Dialog[\s>]/.test(shell)) out.push("FeedConfigFormShell does not open a Dialog");
  if (!/<DialogActions>/.test(shell)) out.push("FeedConfigFormShell has no DialogActions");
  if (/if \(!idem\.open\)[\s\S]*?return[\s\S]*?<form/.test(shell)) out.push("inline form in place of the closed control");
  if (/className="tag"/.test(src)) out.push("legacy .tag chip");
  if (/\b(Pencil|Trash2|Plus)\b/.test(src.split("\n").find((l) => l.includes('from "lucide-react"')) ?? "")) out.push("lucide action icons instead of template Iconify");
  return out;
}

test("feed-config-template-anatomy: self-test", () => {
  assert.deepEqual(shellViolations(`function FeedConfigFormShell() { return <Dialog open><DialogActions></DialogActions></Dialog> }\nfunction Outcome() {}`), []);
  const bad = `import { Pencil } from "lucide-react";\nfunction FeedConfigFormShell() { if (!idem.open) { return x } return <form></form> }\nfunction Outcome() {}\n<span className="tag">a</span>`;
  const v = shellViolations(bad);
  assert.ok(v.includes("FeedConfigFormShell does not open a Dialog"));
  assert.ok(v.includes("legacy .tag chip"));
  assert.ok(v.includes("lucide action icons instead of template Iconify"));
});

test("feed-config-template-anatomy: legacy self-test", () => {
  assert.deepEqual(legacyViolations(`<Table sx={{}}><TableHeadCustom /></Table>`), []);
  assert.ok(legacyViolations(`<Alert>t<Box component="ul"><li>x</li></Box></Alert>`).includes("list inside an Alert (alerts hold text only)"));
  assert.ok(legacyViolations(`<span className="tag t-ok">x</span>`).includes("legacy .tag chip"));
  const v = legacyViolations(`<Alert><Table className="feed-table" /></Alert>\n<div className="fld" style={{ a: 1 }}><label>x</label></div>\n<span className="small muted">y</span>`);
  for (const what of ["legacy .feed-table / .feed-scroll class", "legacy .fld wrapper", "raw <label> (use TextField label)", "legacy small / muted text class", "inline style (use sx)", "table inside an Alert (alerts hold text only)"]) assert.ok(v.includes(what), what);
});

test("feed-config-template-anatomy: page and editors carry no legacy markup", () => {
  assert.deepEqual(legacyViolations(page), []);
  assert.deepEqual(legacyViolations(editor), []);
  assert.deepEqual(legacyViolations(gridTable), []);
  assert.deepEqual(legacyViolations(rateValue), []);
});

test("feed-config-template-anatomy: editors open the template dialog", () => {
  assert.deepEqual(shellViolations(editor), []);
});

test("feed-config-template-anatomy: session feeds are chips + an Add button; plan edit is the CardHeader action", () => {
  const cell = editor.slice(editor.indexOf("export function SessionFeedsCell("));
  assert.match(cell, /trigger="chip"/);
  assert.match(cell, /trigger="button"/);
  const header = page.slice(page.indexOf('title={copy(pageContract, "section.session_template.title")}'));
  const action = header.slice(0, header.indexOf("/>\n"));
  assert.match(action, /action=\{[\s\S]*<SessionPlanEditor/, "SessionPlanEditor must be the session CardHeader action");
});
