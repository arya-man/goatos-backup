#!/usr/bin/env node
// check-admin-web-interaction-patterns.mjs -- the four interaction shapes the maintainer caught
// on the Tasks page on 2026-09-18 and ordered never to ship again (AGENTS.md -> "Admin-web
// interaction patterns"). Each cost a round of "wtf is this" on a feature the console already had
// a right answer for. This is the STATIC half; the runtime half is Chrome on the running branch,
// which is still required for every browser-visible change.
//
// Findings (each has an adversarial self-test fixture below):
//
//   native-date-input           `<input type="date" | "datetime-local" | "time" | "month">`. The
//                               browser's own popover: different chrome from every other field,
//                               locale-driven day/month order that contradicts DD/MM/YYYY. The
//                               console has ONE date field, `components/themed-date-picker.tsx`
//                               (`ThemedDatePicker`), and hour/minute `<select>`s beside it when a
//                               time is needed (`features/leadership-tasks/task-write-forms.tsx`).
//
//   fake-checkbox               `role="checkbox"`, `role="menuitemcheckbox"` or `aria-checked=` on
//                               anything that is not an `<input>`: a coloured square or a `.on`
//                               button standing in for a tick. A choice the user ticks is a real
//                               `<input type="checkbox">` inside a `<label>`; style the box, do
//                               not fake the control (`components/people-dropdown.tsx`).
//
//   revalidate-in-returning-action
//                               in a `"use server"` module, a function that BOTH returns an object
//                               (`return { ... }`) AND calls `revalidatePath` / `revalidateTag`. The
//                               caller is a client island that applies the returned row in place
//                               (a drawer, a status menu, a composer); the revalidate then re-fetches
//                               the whole route on top of it -- the page flickers and the click costs
//                               a full server render for nothing. Either the action returns the row
//                               and the client publishes it to the feature's row store, or the action
//                               is a form post that `redirect()`s. Not both.
//                               (`features/leadership-tasks/actions.ts` -> changeLeadershipTaskStatusInPlaceAction)
//
//   native-dialog               `window.confirm(` / `window.alert(` / `window.prompt(` (or bare
//                               `confirm(` etc.): the browser's "127.0.0.1 says" box in the middle
//                               of the console. A confirm is an in-place control -- two buttons
//                               where the action was -- or the console's own modal
//                               (`features/leadership-tasks/task-status-menu.tsx`, cancel).
//
//   view-toggle-navigation      a `<Link>` / `<a>` whose href carries a `view=` / `_view=` query
//                               param: Board <-> List, Table <-> Cards -- two renderings of rows the
//                               page already holds, swapped by re-running the route. A view toggle is
//                               client state that pushes/replaces the URL through the local-overlay helpers
//                               (`features/leadership-tasks/task-view-switch.tsx`). Data-changing
//                               tabs (scope, status filter) still navigate; only a presentation
//                               switch is a finding.
//
// The DRAWER shape (a card click that navigates to open a same-page overlay) is owned by
// check-admin-web-local-overlays.mjs, which now also pins the Tasks drawer host as required wiring.
//
// Escape hatch: `interaction-guard:ignore: <reason>` on the same line or the line above. It is a
// reviewer-facing justification, never a rubber stamp.
//
// Whole-tree + count ratchet (docs/observability/GUARDRAIL_RATCHET.md): the tree carried debt
// before this guard existed (native date inputs in older forms, returning actions that also
// revalidate), frozen per (file, rule) -> COUNT in tools/admin-web-interaction-patterns/baseline.json.
// A file fails the moment its count for a rule exceeds the baseline; a count BELOW the baseline
// also fails ("stale-high") so fixed debt comes off the books in the same change
// (`--update-baseline`). Growing the baseline to land a new finding is not accepted.
//
// Known blind spots, stated so a green run is not mistaken for proof: a checkbox faked with a
// plain `<button className="on">` and no ARIA at all (invisible to a static scan -- Chrome sees it);
// a view toggle whose param is not called `view`; an action that revalidates through a helper
// function the scan cannot see into; and a `<Calendar>`-looking component that is a native input
// under the hood. The maintainer's Chrome is the last line.

import { existsSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/admin-web";
const BASELINE = "tools/admin-web-interaction-patterns/baseline.json";
const IGNORE_MARK = "interaction-guard:ignore";

function isSource(rel) {
  return (
    rel.startsWith(`${ROOT}/`) &&
    /\.(ts|tsx)$/.test(rel) &&
    !/\.(test|spec|stories)\.(ts|tsx|mjs)$/.test(rel) &&
    !/\.d\.ts$/.test(rel) &&
    !rel.includes("/node_modules/") &&
    !rel.includes("/.next/") &&
    !rel.includes("/public/") &&
    !rel.includes("/lib/api/generated/")
  );
}

function walk(dir, acc = []) {
  let entries = [];
  try {
    entries = readdirSync(dir);
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo", "public", "generated"].includes(entry)) continue;
      walk(full, acc);
      continue;
    }
    const rel = relative(repo, full);
    if (isSource(rel)) acc.push(rel);
  }
  return acc;
}

function lineOf(text, index) {
  return text.slice(0, index).split("\n").length;
}

function ignoredAt(lines, lineNo) {
  const here = lines[lineNo - 1] ?? "";
  const above = lines[lineNo - 2] ?? "";
  return here.includes(IGNORE_MARK) || above.includes(IGNORE_MARK);
}

// ---- rules ---------------------------------------------------------------------------------

const NATIVE_DATE_INPUT = /<input\b[^>]*?\btype\s*=\s*["'](?:date|datetime-local|time|month)["']/g;
// role / aria-checked on a tag that is not <input ...>. Match the opening tag, then reject inputs.
const CHECKED_ATTR_TAG = /<([a-zA-Z][\w.]*)\b[^>]*?(?:\brole\s*=\s*["'](?:checkbox|menuitemcheckbox)["']|\baria-checked\s*=)[^>]*>/g;
const REVALIDATE = /\brevalidate(?:Path|Tag)\s*\(/g;
const OBJECT_RETURN = /\breturn\s*\{/;
const FUNCTION_HEAD = /(?:^|\n)\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z0-9_$]+)\s*\(/g;
const NATIVE_DIALOG = /(?:\bwindow\.|(?<![\w.$]))(?:confirm|alert|prompt)\(/g;
const VIEW_HREF_TAG = /<(?:Link|a)\b[^>]*?\bhref\s*=\s*(?:["'`][^"'`]*?[?&_]view=[^"'`]*["'`]|\{[^}]*?[?&_]view=[^}]*\})/g;

function scanNativeDateInputs(rel, text, lines) {
  const out = [];
  for (const m of text.matchAll(NATIVE_DATE_INPUT)) {
    const line = lineOf(text, m.index);
    if (ignoredAt(lines, line)) continue;
    out.push({ file: rel, line, rule: "native-date-input", detail: "native date/time input; use ThemedDatePicker (+ hour/minute selects) -- the console's one date field" });
  }
  return out;
}

function scanFakeCheckboxes(rel, text, lines) {
  const out = [];
  for (const m of text.matchAll(CHECKED_ATTR_TAG)) {
    if (m[1] === "input") continue;
    const line = lineOf(text, m.index);
    if (ignoredAt(lines, line)) continue;
    out.push({ file: rel, line, rule: "fake-checkbox", detail: `<${m[1]}> carries checkbox ARIA; a tick is a real <input type="checkbox"> inside a <label>` });
  }
  return out;
}

// Per-function: split the module at every function head and look inside each body. A body is
// the text up to the next head (good enough for the flat `actions.ts` shape these files have).
function scanReturningRevalidate(rel, text, lines) {
  if (!/["']use server["']/.test(text.slice(0, 400))) return [];
  const heads = [...text.matchAll(FUNCTION_HEAD)];
  const out = [];
  for (let i = 0; i < heads.length; i += 1) {
    const start = heads[i].index;
    const end = i + 1 < heads.length ? heads[i + 1].index : text.length;
    const body = text.slice(start, end);
    if (!OBJECT_RETURN.test(body)) continue;
    for (const m of body.matchAll(REVALIDATE)) {
      const line = lineOf(text, start + m.index);
      if (ignoredAt(lines, line)) continue;
      out.push({ file: rel, line, rule: "revalidate-in-returning-action", detail: `${heads[i][1]}() returns a result object AND revalidates the route; return the row for the client to apply in place, or redirect() -- not both` });
    }
  }
  return out;
}

function scanNativeDialogs(rel, text, lines) {
  const out = [];
  for (const m of text.matchAll(NATIVE_DIALOG)) {
    const line = lineOf(text, m.index);
    if (ignoredAt(lines, line)) continue;
    // A method named confirm on our own object (`dialog.confirm(`) is excluded by the lookbehind.
    out.push({ file: rel, line, rule: "native-dialog", detail: "browser dialog (confirm/alert/prompt); use an in-place confirm or the console's own modal" });
  }
  return out;
}

function scanViewToggleNavigation(rel, text, lines) {
  const out = [];
  for (const m of text.matchAll(VIEW_HREF_TAG)) {
    const line = lineOf(text, m.index);
    if (ignoredAt(lines, line)) continue;
    out.push({ file: rel, line, rule: "view-toggle-navigation", detail: "a view= link re-runs the route to swap two renderings of the same rows; make it client state that pushLocalOverlayUrl()s" });
  }
  return out;
}

function scanText(rel, text) {
  const lines = text.split("\n");
  return [
    ...scanNativeDateInputs(rel, text, lines),
    ...scanFakeCheckboxes(rel, text, lines),
    ...scanReturningRevalidate(rel, text, lines),
    ...scanViewToggleNavigation(rel, text, lines),
    ...scanNativeDialogs(rel, text, lines),
  ];
}

function scanFile(rel) {
  return scanText(rel, readFileSync(join(repo, rel), "utf8"));
}

// The reference implementations must keep the shape they are cited for above. If one of these
// stops holding its invariant the guard's own advice is wrong, so this fails loudly.
const REQUIRED_WIRING = [
  ["apps/admin-web/components/themed-date-picker.tsx", ["export function ThemedDatePicker"]],
  ["apps/admin-web/components/people-dropdown.tsx", ['type="checkbox"', "<label"]],
  ["apps/admin-web/features/leadership-tasks/task-view-switch.tsx", ["preventDefault", "LocalOverlayUrl("]],
  ["apps/admin-web/features/leadership-tasks/task-row-store.ts", ["export function publishTaskRow"]],
];

function wiringFindings() {
  const out = [];
  for (const [file, needles] of REQUIRED_WIRING) {
    let text = "";
    try {
      text = readFileSync(join(repo, file), "utf8");
    } catch {
      out.push(`${file}: reference implementation is missing`);
      continue;
    }
    for (const needle of needles) if (!text.includes(needle)) out.push(`${file}: reference invariant is missing: ${needle}`);
  }
  // The in-place status action must not have grown a revalidate back.
  const actions = readFileSync(join(repo, "apps/admin-web/features/leadership-tasks/actions.ts"), "utf8");
  const head = actions.indexOf("export async function changeLeadershipTaskStatusInPlaceAction");
  if (head < 0) out.push("apps/admin-web/features/leadership-tasks/actions.ts: changeLeadershipTaskStatusInPlaceAction is missing");
  else {
    const next = actions.indexOf("\nexport ", head + 10);
    const body = actions.slice(head, next < 0 ? undefined : next);
    if (/revalidate(?:Path|Tag)\s*\(/.test(body)) out.push("apps/admin-web/features/leadership-tasks/actions.ts: changeLeadershipTaskStatusInPlaceAction revalidates the route again (the drawer applies the row in place; this is the flicker)");
  }
  return out;
}

// ---- baseline ratchet -----------------------------------------------------------------------

function countByKey(findings) {
  const counts = new Map();
  for (const f of findings) {
    const key = `${f.file}|${f.rule}`;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function loadBaseline() {
  if (!existsSync(join(repo, BASELINE))) return new Map();
  const parsed = JSON.parse(readFileSync(join(repo, BASELINE), "utf8"));
  return new Map(Object.entries(parsed.entries ?? {}));
}

function writeBaseline(counts) {
  const entries = Object.fromEntries([...counts.entries()].sort(([a], [b]) => a.localeCompare(b)));
  writeFileSync(
    join(repo, BASELINE),
    `${JSON.stringify({ _comment: "Count of known admin-web interaction-pattern findings per file|rule. Shrink-only: regenerate with `node tools/agent-hooks/check-admin-web-interaction-patterns.mjs --update-baseline` after FIXING debt; never grow it to land a new finding.", entries }, null, 2)}\n`,
  );
}

// ---- self-test ------------------------------------------------------------------------------

function selfTest() {
  const cases = [
    // native-date-input
    ['<input type="date" name="d" />', ["native-date-input"]],
    ["<input\n  name=\"d\"\n  type='datetime-local'\n/>", ["native-date-input"]],
    ['<input type="time" />', ["native-date-input"]],
    ['<input type="text" name="d" />', []],
    ['<ThemedDatePicker value={d} />', []],
    // ignore mark honoured
    ['{/* interaction-guard:ignore: legacy clock screen, tracked */}\n<input type="time" />', []],
    // fake-checkbox
    ['<button role="menuitemcheckbox" aria-checked={on} className="opt">A</button>', ["fake-checkbox"]],
    ['<span role="checkbox" aria-checked="true" className="box" />', ["fake-checkbox"]],
    ['<div\n  aria-checked={on}\n  onClick={toggle}\n/>', ["fake-checkbox"]],
    ['<input type="checkbox" checked={on} aria-checked={on} />', []],
    ['<label><input type="checkbox" checked={on} onChange={t} /><span className="box" /></label>', []],
    // revalidate-in-returning-action
    ['"use server";\nexport async function saveAction(f) {\n  await api();\n  revalidatePath("/x");\n  return { ok: true };\n}', ["revalidate-in-returning-action"]],
    ['"use server";\nexport async function postAction(f) {\n  await api();\n  revalidatePath("/x");\n  redirect("/x");\n}', []],
    ['"use server";\nexport async function loadAction(id) {\n  const t = await api(id);\n  return { ok: true, task: t };\n}', []],
    // two functions: only the returning one counts
    ['"use server";\nexport async function a() {\n  revalidateTag("t");\n  redirect("/");\n}\nexport async function b() {\n  return { ok: false };\n}', []],
    ['"use server";\nexport async function a() {\n  redirect("/");\n}\nexport async function b() {\n  revalidateTag("t");\n  return { ok: false };\n}', ["revalidate-in-returning-action"]],
    // not a server module: no finding
    ['export async function saveAction(f) {\n  revalidatePath("/x");\n  return { ok: true };\n}', []],
    // native-dialog
    ['if (!window.confirm(copy)) return;', ["native-dialog"]],
    ["alert('saved')", ["native-dialog"]],
    ['const ok = confirm("Sure?");', ["native-dialog"]],
    ['await dialog.confirm({ title })', []],
    ['const confirmingCancel = true; setConfirmingCancel(false)', []],
    // view-toggle-navigation
    ['<Link href="/tasks?scope=x&view=list">List</Link>', ["view-toggle-navigation"]],
    ['<a href={`/tasks?t_view=${next}`}>Board</a>', ["view-toggle-navigation"]],
    ['<Link href={tasksHref({ scope })}>Team</Link>', []],
    ['<Link href="/tasks?filter=overdue">Overdue</Link>', []],
  ];
  const failures = [];
  cases.forEach(([src, expected], i) => {
    const got = scanText(`fixture-${i}.tsx`, src).map((f) => f.rule);
    if (JSON.stringify(got) !== JSON.stringify(expected)) failures.push(`case ${i}: expected ${JSON.stringify(expected)} got ${JSON.stringify(got)} for ${JSON.stringify(src)}`);
  });
  // The ratchet: a second finding in an already-baselined file reads as an increase.
  const counts = countByKey([{ file: "f", rule: "native-date-input" }, { file: "f", rule: "native-date-input" }]);
  if (counts.get("f|native-date-input") !== 2) failures.push("count ratchet did not count per file|rule");
  const wiring = wiringFindings();
  if (wiring.length) failures.push(...wiring.map((w) => `wiring: ${w}`));
  if (failures.length) {
    console.error("admin-web-interaction-patterns self-test: FAIL");
    for (const f of failures) console.error(`  ${f}`);
    return 1;
  }
  console.log(`admin-web-interaction-patterns self-test: PASS (${cases.length} cases)`);
  return 0;
}

// ---- main -----------------------------------------------------------------------------------

function main() {
  const args = process.argv.slice(2);
  if (args.includes("--self-test")) return selfTest();
  const files = walk(join(repo, ROOT));
  const findings = files.flatMap((rel) => scanFile(rel));
  const counts = countByKey(findings);

  if (args.includes("--list")) {
    for (const f of findings) console.log(`${f.file}:${f.line} [${f.rule}] ${f.detail}`);
    console.log(`admin-web-interaction-patterns: ${findings.length} finding(s) listed (baseline not consulted)`);
    return 0;
  }

  if (args.includes("--update-baseline")) {
    const old = loadBaseline();
    for (const [key, n] of counts) {
      const was = old.get(key) ?? 0;
      if (n > was) console.log(`  +${was === 0 ? "NEW" : "MORE"} ${key} ${was} -> ${n}`);
    }
    for (const [key, was] of old) {
      const n = counts.get(key) ?? 0;
      if (n < was) console.log(`  -${n === 0 ? "GONE" : "LESS"} ${key} ${was} -> ${n}`);
    }
    writeBaseline(counts);
    console.log(`admin-web-interaction-patterns: baseline written (${counts.size} file|rule entries, ${findings.length} findings)`);
    return 0;
  }

  const wiring = wiringFindings();
  const baseline = loadBaseline();
  const increased = [];
  const decreased = [];
  for (const [key, n] of counts) {
    const was = baseline.get(key) ?? 0;
    if (n > was) increased.push({ key, was, n });
  }
  for (const [key, was] of baseline) {
    const n = counts.get(key) ?? 0;
    if (n < was) decreased.push({ key, was, n });
  }
  if (wiring.length) {
    console.error("admin-web-interaction-patterns: FAIL -- reference implementation drifted:");
    for (const w of wiring) console.error(`  ${w}`);
  }
  if (increased.length) {
    console.error("admin-web-interaction-patterns: FAIL -- new findings (docs/agent-rules/ui-frontend.md 'Admin-web interaction patterns'):");
    for (const { key, was, n } of increased) {
      console.error(`  ${key}: ${was} -> ${n}`);
      for (const f of findings.filter((x) => `${x.file}|${x.rule}` === key)) console.error(`    ${f.file}:${f.line} [${f.rule}] ${f.detail}`);
    }
    console.error("Use ThemedDatePicker, a real <input type=\"checkbox\">, return-the-row OR redirect (never revalidate on top of a returned row), and client-state view toggles; or justify with `interaction-guard:ignore: <reason>`.");
  }
  if (decreased.length) {
    console.error("admin-web-interaction-patterns: FAIL -- baseline is stale-high (debt was fixed; take it off the books in the same change with --update-baseline):");
    for (const { key, was, n } of decreased) console.error(`  ${key}: ${was} -> ${n}`);
  }
  if (wiring.length || increased.length || decreased.length) return 1;
  console.log(`admin-web-interaction-patterns: PASS (${files.length} files, ${findings.length} baselined findings across ${counts.size} file|rule entries, zero new)`);
  return 0;
}

process.exit(main());
