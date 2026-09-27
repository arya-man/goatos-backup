// guard: herd-signals-template-anatomy. The board shells, Alerts tab, Gateways and Insights tabs are
// template Cards / CardHeaders / Alerts / Grid; the legacy mock markup (card / hd / bd / banner /
// gwcard / gwstats / insight / rowlist / rowitem / pager / btn classes) must not come back.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const LEGACY = /className=["'`][^"'`]*\b(card|hd|bd|banner|gwcard|gwh|gwstats|insight|ih|iv|if|srcl|rowlist|rowitem|rt|rs|pager|pgbtn|btn|grid2|empty|eicon|eact|sp)\b/;

for (const name of ["herd-signals-board.tsx", "herd-signals-gateways.tsx", "herd-signals-insights.tsx"]) {
  test(`${name} renders template anatomy, not legacy mock classes`, () => {
    const src = readFileSync(new URL(`./${name}`, import.meta.url), "utf8");
    const hit = src.split("\n").find((line) => LEGACY.test(line));
    assert.equal(hit, undefined, `legacy class in ${name}: ${hit}`);
    assert.match(src, /from "@mui\/material\/Card"/);
  });
}

test("self-test: the legacy pattern catches the old shells", () => {
  assert.match('<div className="card">', LEGACY);
  assert.match('<div className="gwcard">', LEGACY);
  assert.match('<div className="banner info">', LEGACY);
  assert.doesNotMatch('<Box className="herd-signals-page">', LEGACY);
});

// TR1-#30: the header live control is a template soft Button (updated line in its tooltip) and
// Export an outlined Button; no red-outline LIVE pill, no "Updated … stream open" meta line, no raw
// <button>. Eight KPI tiles sit in two full rows of four (KpiGrid), never 3 + 3 + 2.
test("herd-signals-live-header: live toggle + export are template Buttons, KPI rows have no orphan", () => {
  const bridge = readFileSync(new URL("./herd-signals-stream-bridge.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(bridge, /<button\b|<a\b|className="(livebadge|refreshmeta|herd-signals-livebar|btn)/);
  assert.match(bridge, /<Button\s+variant="soft"[\s\S]*?onClick=\{toggleLive\}/);
  assert.match(bridge, /<Tooltip title=\{`\$\{live \? "Pause live stream" : "Resume live stream"\} · \$\{updatedLine\}`\}/);
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.doesNotMatch(css, /\.livebadge|\.refreshmeta|\.herd-signals-livebar|hs-lp/);
  const grid = readFileSync(new URL("../../components/app/kpi-grid.tsx", import.meta.url), "utf8");
  assert.match(grid, /if \(n % 4 === 0\) return \{ xs: 12, sm: 6, md: 3 \};/);
});
