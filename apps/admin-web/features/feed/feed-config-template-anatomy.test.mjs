// Guard: feed-config-template-anatomy (TR1 /feed/config). Feed Config writes sit on template anatomy:
// every editor opens the template quick-edit Dialog (never an inline form growing in a table cell or
// card header), declared feeds are soft Chips with a delete, adds are template Buttons, and the
// session plan edit is the CardHeader action, not a lone pencil row under the header.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const editor = readFileSync(new URL("./feed-config-editor.tsx", import.meta.url), "utf8");
const page = readFileSync(new URL("./feed-config.tsx", import.meta.url), "utf8");

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
