#!/usr/bin/env node
// check-mock-css-parity — a class the implementation RENDERS must actually be STYLED.
//
// WHY THIS EXISTS
// ---------------
// Herd Signals was built mock-first: mock/herd-signals-mock.html is the approved design and the
// React components were ported from it, reusing its class names. But porting the MARKUP without
// porting the CSS is silent. `.grid2` is the worked example: the Gateways and Insights tabs both
// render <div className="grid2">, the mock defines
//     .grid2{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:14px}
// and the app theme defined NOTHING. No error, no warning, no failing test — the tabs just
// stacked as full-width blocks and looked nothing like the design. It took a maintainer opening
// both screens side by side to catch it, twice.
//
// The failure mode is structural, not a one-off: a component says `className="x"`, the app CSS has
// no rule for `x`, and the page renders unstyled but not broken. TypeScript cannot see it, eslint
// cannot see it, and every test still passes.
//
// THE RULE
// --------
// If a component renders a class, AND the module's mock defines that class, THEN the app stylesheet
// must define it too. Porting markup from a mock without porting its CSS is the defect this blocks.
//
// WHAT IT DOES NOT CLAIM
// ----------------------
// - It does not compare property VALUES. A rule that exists but differs from the mock (wrong
//   padding, wrong colour) passes here; that is what visual regression review is for.
// - It only considers classes the MOCK defines. A class that exists solely in the app (Tailwind
//   utilities, shared primitives like .card/.tag/.btn defined elsewhere in the theme) is ignored,
//   because the mock is not the authority on those.
// - Class names built by string concatenation or template interpolation are invisible to it.
//   Static className literals and the static parts of template literals are covered.
//
// USAGE
//   node tools/agent-hooks/check-mock-css-parity.mjs            # check every registered module
//   node tools/agent-hooks/check-mock-css-parity.mjs --self-test
import { readFileSync, existsSync } from "node:fs";
import { readdirSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");

// Modules built mock-first. Add an entry when a new screen is ported from a mock.
const MODULES = [
  {
    name: "herd-signals",
    componentsDir: "apps/admin-web/features/herd-signals",
    mock: "mock/herd-signals-mock.html",
    stylesheets: ["apps/admin-web/app/mesha-theme.css"],
  },
];

/** Class names appearing in className="..." / className={`...`} literals. */
function classesUsedIn(source) {
  const found = new Set();
  const attr = /className\s*=\s*(?:"([^"]*)"|'([^']*)'|\{`([^`]*)`\}|\{"([^"]*)"\}|\{'([^']*)'\})/g;
  let m;
  while ((m = attr.exec(source)) !== null) {
    const raw = m[1] ?? m[2] ?? m[3] ?? m[4] ?? m[5] ?? "";
    // Drop ${...} interpolations; keep the static tokens around them.
    for (const token of raw.replace(/\$\{[^}]*\}/g, " ").split(/\s+/)) {
      const cleaned = token.trim();
      if (cleaned && /^[a-zA-Z][\w-]*$/.test(cleaned)) found.add(cleaned);
    }
  }
  return found;
}

/** Class names that appear as selectors (.foo{...}) in a stylesheet or a mock's <style>. */
function classesDefinedIn(source) {
  const found = new Set();
  const selector = /\.([a-zA-Z][\w-]*)(?=[^{}]*\{)/g;
  let m;
  while ((m = selector.exec(source)) !== null) found.add(m[1]);
  return found;
}

function walkTsx(dir, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) walkTsx(full, out);
    else if (/\.(tsx|jsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

function checkModule(mod) {
  const findings = [];
  const mockPath = join(repoRoot, mod.mock);
  if (!existsSync(mockPath)) return { findings, scanned: 0 };

  const mockClasses = classesDefinedIn(readFileSync(mockPath, "utf8"));
  const styled = new Set();
  for (const sheet of mod.stylesheets) {
    const p = join(repoRoot, sheet);
    if (existsSync(p)) for (const c of classesDefinedIn(readFileSync(p, "utf8"))) styled.add(c);
  }

  const files = walkTsx(join(repoRoot, mod.componentsDir));
  for (const file of files) {
    const used = classesUsedIn(readFileSync(file, "utf8"));
    for (const cls of used) {
      if (!mockClasses.has(cls)) continue; // mock is not the authority on this class
      if (styled.has(cls)) continue; // styled somewhere in the app theme
      findings.push({
        file: file.replace(`${repoRoot}/`, ""),
        cls,
        mock: mod.mock,
      });
    }
  }
  return { findings, scanned: files.length };
}

function selfTest() {
  const usedOk = classesUsedIn('<div className="grid2 card" />');
  const usedTpl = classesUsedIn("<div className={`kpi ${tone}`} />");
  const defined = classesDefinedIn(".grid2{display:grid}\n.kpi .val{font-weight:700}");
  const problems = [];
  if (!usedOk.has("grid2") || !usedOk.has("card")) problems.push("static className not parsed");
  if (!usedTpl.has("kpi")) problems.push("template-literal static part not parsed");
  if (usedTpl.has("tone")) problems.push("interpolation leaked into class list");
  if (!defined.has("grid2") || !defined.has("kpi") || !defined.has("val")) problems.push("selector parse failed");
  if (problems.length) {
    console.error(`check-mock-css-parity self-test: FAIL\n- ${problems.join("\n- ")}`);
    process.exit(1);
  }
  console.log("check-mock-css-parity self-test: PASS");
  process.exit(0);
}

if (process.argv.includes("--self-test")) selfTest();

let total = 0;
let scanned = 0;
for (const mod of MODULES) {
  const { findings, scanned: n } = checkModule(mod);
  scanned += n;
  for (const f of findings) {
    if (total === 0) console.error("mock-css-parity: a rendered class has no rule in the app stylesheet");
    console.error(
      `- ${f.file}: class "${f.cls}" is defined in ${f.mock} but has NO rule in the app stylesheet — port the mock's CSS, do not ship the markup alone`,
    );
    total += 1;
  }
}

if (total > 0) {
  console.error(
    "\nPorting markup from a mock without porting its CSS renders unstyled but not broken: no type error, no test failure, just a screen that does not match the design.",
  );
  process.exit(1);
}
console.log(`mock-css-parity: ok (${scanned} component file(s) scanned against ${MODULES.length} mock(s))`);
