#!/usr/bin/env node
// Mandatory frontend fidelity gate. The ONLY UI/UX source of truth is the mock at
// goatos/mock/goatos-dashboard-mock.html. Old admin UI is gone — screens must be PORTED from the mock,
// not reused/recolored. This check FAILS on the mechanical signals of "recolored old admin UI":
//   1. emoji / dingbat glyphs used as icons (the mock uses one SVG icon set)
//   2. old admin cyan/slate palette hex literals (the mock uses Mesha CSS tokens)
//   3. imports of the legacy components/admin-primitives module inside in-scope screens
// Run before every git mesha-push of frontend work:  npm run check:mock-fidelity
import { readdirSync, readFileSync, statSync, existsSync } from "node:fs";
import { join } from "node:path";

// In-scope screens that must already be ported from the mock (current review scope).
const SCAN_PATHS = [
  "components/mesha-shell.tsx",
  "features/control-tower",
  "features/vaccination",
  "features/config",
  "features/goat-passport",
  "app/(admin)/page.tsx",
  "app/(admin)/layout.tsx",
  "app/(admin)/vaccination",
  "app/(admin)/config",
  "app/(admin)/goats",
  "app/login",
];

// Old admin cyan/slate palette — banned in in-scope screens (use var(--brand/--line/--panel/...)).
const BANNED_HEX = [
  "#14f1d9", "#334155", "#1A1D24", "#11151C", "#0f1115", "#10141b", "#1d1214",
  "#7f1d1d", "#8899AA", "#93a4b8", "#aab7c4", "#c7d1dc", "#64748b", "#10141B",
];
// Emoji/dingbat glyphs used as icons (the mock uses SVG icons). Typographic →/✓/✗/·/— stay allowed.
const BANNED_GLYPHS = ["💉", "⚠", "✎", "⇄", "✕", "⚙", "📋", "🎥", "🔔", "🩺", "📦", "🧪", "🐐", "🔬"];
const EMOJI_RANGE = /[\u{1F000}-\u{1FAFF}]/u;
const ADMIN_PRIMITIVES = /from\s+["']@\/components\/admin-primitives["']/;

function walk(p, out) {
  if (!existsSync(p)) return;
  const st = statSync(p);
  if (st.isDirectory()) {
    for (const entry of readdirSync(p)) walk(join(p, entry), out);
  } else if (/\.(tsx|ts)$/.test(p)) {
    out.push(p);
  }
}

const files = [];
for (const sp of SCAN_PATHS) walk(sp, files);

const findings = [];
for (const file of files) {
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, i) => {
    const n = i + 1;
    for (const hex of BANNED_HEX) {
      if (line.includes(hex)) findings.push(`${file}:${n}  old-admin palette hex ${hex} — use Mesha token var(--…)`);
    }
    for (const g of BANNED_GLYPHS) {
      if (line.includes(g)) findings.push(`${file}:${n}  emoji/dingbat icon "${g}" — use the mock's SVG/lucide icon`);
    }
    if (EMOJI_RANGE.test(line)) findings.push(`${file}:${n}  emoji glyph — use the mock's SVG/lucide icon`);
    if (ADMIN_PRIMITIVES.test(line)) findings.push(`${file}:${n}  imports legacy components/admin-primitives — port this screen from the mock instead`);
  });
}

if (findings.length > 0) {
  console.error("✖ mock-fidelity check FAILED — old admin UI must not be reused/recolored. Port from the mock:");
  console.error("  reference: goatos/mock/goatos-dashboard-mock.html\n");
  for (const f of findings) console.error("  " + f);
  console.error(`\n${findings.length} issue(s). Rebuild the screen from the mock structure — do not recolor old admin components.`);
  process.exit(1);
}

console.log("✓ mock-fidelity check passed — no old-admin UI reuse detected in in-scope screens.");
