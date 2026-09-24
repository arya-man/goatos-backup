#!/usr/bin/env node
// Ask Mesha accuracy regression. Every golden question (eval/golden.json) is asked through the real
// /ceo-ai/ask path and graded against truth SQL computed LIVE, read-only, at eval time; where an
// admin-web screen shows the same number, the screen's backend read is fetched too (3-way check).
//
//   ASK_MESHA_BENCH_TOKEN=... node tools/ask-mesha-agent/eval/run.mjs [options]
//     --subset <n|tag|id[,id]>  first n items, or items with that tag/id (default: all)
//     --concurrency <n>         parallel asks (default 2)
//     --budget-usd <x>          stop asking once measured spend passes x (default 6)
//     --truth-only              run truth SQL + UI reads only; no agent calls, $0 (check goldens)
//     --no-ui                   skip the UI API leg even when a token is set
//     --url <base>              agent base (default ASK_MESHA_URL or http://127.0.0.1:8787)
// Env: ASK_MESHA_STATE_DIR (default ~/.ask-mesha-agent; holds .pgenv, evals/, events), PGENV_FILE (override .pgenv),
//      ASK_MESHA_EVAL_BEARER + ASK_MESHA_EVAL_TENANT (optional UI leg; never stored or logged),
//      GOATOS_STG_API (default https://api.goatos.mesha.sg).
// Exit 1 when any graded item fails (or truth SQL errors); 0 when all pass. Skipped (budget) items don't fail the run.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";
import { buildReferenceSql, referencePath, validateReadSql } from "../lib.mjs";
import { createEvents } from "../events.mjs";
import { dateMacros, extractPath, fillMacros, gradeAnswer, numberPresent, threeWay, expectedValues } from "./grade.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const REPO = path.resolve(HERE, "../../..");
const STATE = process.env.ASK_MESHA_STATE_DIR || path.join(os.homedir(), ".ask-mesha-agent");

function parseArgs(argv) {
  const o = { subset: null, concurrency: 2, budget: 6, truthOnly: false, ui: true, url: process.env.ASK_MESHA_URL || "http://127.0.0.1:8787", golden: path.join(HERE, "golden.json") };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const val = () => argv[++i];
    if (a === "--subset") o.subset = val();
    else if (a === "--concurrency") o.concurrency = Math.max(1, Number(val()) || 1);
    else if (a === "--budget-usd") o.budget = Number(val());
    else if (a === "--truth-only") o.truthOnly = true;
    else if (a === "--no-ui") o.ui = false;
    else if (a === "--url") o.url = val();
    else if (a === "--golden") o.golden = path.resolve(val());
    else if (a === "-h" || a === "--help") { console.log(fs.readFileSync(fileURLToPath(import.meta.url), "utf8").split("\n").slice(1, 18).join("\n")); process.exit(0); }
    else throw new Error(`unknown option ${a}`);
  }
  if (!Number.isFinite(o.budget) || o.budget <= 0) throw new Error("--budget-usd must be > 0");
  return o;
}

export function selectItems(items, subset) {
  if (!subset) return items;
  if (/^\d+$/.test(subset)) return items.slice(0, Number(subset));
  const keys = subset.split(",").map((s) => s.trim()).filter(Boolean);
  return items.filter((it) => keys.some((k) => it.id === k || (it.tags || []).includes(k)));
}

// ---- read-only truth SQL (same guard + PGOPTIONS as the agent's runSql) -------------------
function loadPgEnv() {
  const file = process.env.PGENV_FILE || path.join(STATE, ".pgenv");
  if (!fs.existsSync(file)) throw new Error(`missing ${file} (read-only PG* vars)`);
  return Object.fromEntries(fs.readFileSync(file, "utf8").trim().split("\n").filter((l) => l && !l.startsWith("#")).map((l) => {
    const i = l.indexOf("=");
    return [l.slice(0, i).replace(/^export\s+/, ""), l.slice(i + 1).replace(/^['"]|['"]$/g, "")];
  }));
}
function referenceSql(name, opts = {}) {
  const ref = referencePath(REPO, name);
  if (!ref.ok) throw new Error(ref.out);
  const built = buildReferenceSql(fs.readFileSync(ref.file, "utf8"), { name, ...opts });
  if (!built.ok) throw new Error(built.out);
  return built.sql;
}
export function truthSql(truth, macros) {
  const t = fillMacros(truth, macros);
  if (t.reference) return referenceSql(t.reference, { params: t.params, where: t.where, order_by: t.order_by, limit: t.limit });
  // {{ref:file.sql}} inlines a reference (wrapped, no filter) so an item can post-process it.
  return String(t.sql).replace(/\{\{ref:([a-z0-9.-]+)\}\}/g, (_, n) => referenceSql(n));
}
let pgEnv;
function runTruth(sql) {
  const checked = validateReadSql(sql);
  if (!checked.ok) return Promise.reject(new Error(checked.out));
  pgEnv ??= loadPgEnv();
  return new Promise((resolve, reject) => {
    const child = spawn("psql", ["-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-f", "-"], {
      env: { PATH: process.env.PATH, ...pgEnv, PGCONNECT_TIMEOUT: "10", PGOPTIONS: "-c default_transaction_read_only=on -c statement_timeout=90000 -c standard_conforming_strings=on" },
    });
    let out = "", err = "";
    child.stdout.on("data", (c) => (out += c));
    child.stderr.on("data", (c) => (err += c));
    child.on("error", reject);
    child.on("close", (code) => {
      if (code !== 0) return reject(new Error(err.trim().split("\n")[0] || `psql exited ${code}`));
      try { resolve(JSON.parse(out.trim() || "[]")); } catch (e) { reject(new Error(`bad truth output: ${e.message}`)); }
    });
    child.stdin.end(`BEGIN READ ONLY;\nSELECT coalesce(json_agg(t), '[]'::json) FROM (\n${checked.sql}\n) t;\nROLLBACK;\n`);
  });
}

// ---- UI leg: the backend read the admin-web screen calls -------------------------------
function uiConfig(enabled) {
  const bearer = process.env.ASK_MESHA_EVAL_BEARER, tenant = process.env.ASK_MESHA_EVAL_TENANT;
  if (!enabled) return { on: false, note: "UI checks skipped (--no-ui)" };
  if (!bearer || !tenant) return { on: false, note: "UI checks skipped: set ASK_MESHA_EVAL_BEARER (leadership Firebase ID token) + ASK_MESHA_EVAL_TENANT to compare against the admin-web screens" };
  return { on: true, bearer, tenant, base: (process.env.GOATOS_STG_API || "https://api.goatos.mesha.sg").replace(/\/$/, ""), note: null };
}
async function fetchUi(cfg, ui, macros) {
  const q = fillMacros(ui.query || {}, macros);
  const u = new URL(cfg.base + ui.path);
  for (const [k, v] of Object.entries(q)) if (v !== "" && v != null) u.searchParams.set(k, String(v));
  const res = await fetch(u, { headers: { Authorization: `Bearer ${cfg.bearer}`, "X-GoatOS-Tenant-ID": cfg.tenant, Accept: "application/json" }, signal: AbortSignal.timeout(30_000) });
  if (!res.ok) throw new Error(`${ui.path} HTTP ${res.status}`);
  const body = await res.json();
  return Object.fromEntries((ui.extract || []).map((x) => [x.label, extractPath(body, x.path)]));
}

// ---- agent ask (SSE, as bench.mjs) --------------------------------------------------------
async function ask(url, question, maxSeconds) {
  const t0 = Date.now();
  const ctl = new AbortController();
  const hard = setTimeout(() => ctl.abort(), Math.max(maxSeconds * 3, 180) * 1000); // grade slowness, don't hang forever
  let final = null, err = null;
  try {
    const res = await fetch(`${url}/ceo-ai/ask`, {
      method: "POST", signal: ctl.signal,
      headers: { Authorization: `Bearer ${process.env.ASK_MESHA_BENCH_TOKEN}`, "Content-Type": "application/json" },
      body: JSON.stringify({ question, stream: true }),
    });
    if (!(res.headers.get("content-type") || "").includes("text/event-stream")) {
      err = `HTTP ${res.status}: ${(await res.text()).slice(0, 200)}`;
    } else {
      const dec = new TextDecoder(); let buf = "";
      for await (const chunk of res.body) {
        buf += dec.decode(chunk, { stream: true });
        let i;
        while ((i = buf.indexOf("\n\n")) >= 0) {
          const ev = buf.slice(0, i); buf = buf.slice(i + 2);
          const line = ev.split("\n").find((l) => l.startsWith("data:"));
          if (!line) continue;
          let o; try { o = JSON.parse(line.slice(5)); } catch { continue; }
          if (o.type === "final") final = o;
          if (o.type === "error") err = o.message;
        }
      }
    }
  } catch (e) { err = ctl.signal.aborted ? "timed out (hard stop)" : e.message; }
  clearTimeout(hard);
  return { answer: final?.answer ?? "", request_id: final?.request_id ?? null, source: final?.source ?? null, error: err, seconds: +((Date.now() - t0) / 1000).toFixed(1) };
}
// Measured cost lands in the events store in the ask's finally; poll /metrics/recent briefly.
async function costOf(url, requestId) {
  if (!requestId) return null;
  for (let i = 0; i < 6; i++) {
    try {
      const r = await fetch(`${url}/metrics/recent?email=bench@local`, { headers: { Authorization: `Bearer ${process.env.ASK_MESHA_BENCH_TOKEN}` } });
      if (r.ok) {
        const hit = (await r.json()).asks?.find((a) => a.request_id === requestId);
        if (hit && hit.cost_usd != null) return Number(hit.cost_usd);
      }
    } catch {}
    await new Promise((r) => setTimeout(r, 500));
  }
  return null;
}

const EST_COST = 0.15; // per question when the measured cost can't be read (keeps the budget guard fail-closed)

async function main() {
  const opt = parseArgs(process.argv.slice(2));
  const golden = JSON.parse(fs.readFileSync(opt.golden, "utf8"));
  const items = selectItems(golden.items, opt.subset);
  if (!items.length) throw new Error(`no golden items match --subset ${opt.subset}`);
  if (!opt.truthOnly && !process.env.ASK_MESHA_BENCH_TOKEN) throw new Error("ASK_MESHA_BENCH_TOKEN is required (or use --truth-only)");
  const macros = dateMacros();
  const ui = uiConfig(opt.ui);
  const startedAt = new Date();
  let spent = 0;
  let stop = false;
  const results = new Array(items.length);

  async function one(idx) {
    const it = items[idx];
    const r = { id: it.id, tags: it.tags || [], question: it.question, status: "pending", reasons: [], checks: [], ui: [], truth: null };
    const maxSeconds = it.max_seconds ?? golden.defaults?.max_seconds ?? 90;
    try {
      r.truth = it.truth ? await runTruth(truthSql(it.truth, macros)) : [];
    } catch (e) {
      r.status = "error"; r.reasons = [`truth SQL failed: ${e.message.slice(0, 200)}`];
      return (results[idx] = r);
    }
    let uiVals = null;
    if (ui.on && it.ui_api) {
      try { uiVals = await fetchUi(ui, it.ui_api, macros); } catch (e) { r.ui_error = e.message.slice(0, 160); }
    }
    if (opt.truthOnly) {
      const ev = expectedValues(it, r.truth);
      const missing = ev.filter((x) => x.value === undefined).map((x) => x.label);
      r.status = missing.length ? "error" : "truth-ok";
      r.reasons = missing.length ? [`truth row missing for ${missing.join(", ")}`] : [ev.map((x) => `${x.label}=${x.value}`).join(" ")];
      if (uiVals) r.ui = ev.filter((x) => x.label in uiVals).map((x) => threeWay({ label: x.label, truth: x.value, chatOk: true, ui: uiVals[x.label], tol: x.tol, tol_pct: x.tol_pct }));
      return (results[idx] = r);
    }
    if (stop || spent >= opt.budget) { stop = true; r.status = "skipped"; r.reasons = [`budget $${opt.budget} reached`]; return (results[idx] = r); }
    const a = await ask(opt.url, it.question, maxSeconds);
    const cost = await costOf(opt.url, a.request_id);
    r.cost_usd = cost; r.cost_estimated = cost == null;
    spent += cost ?? EST_COST;
    if (spent >= opt.budget) stop = true;
    Object.assign(r, { answer: a.answer, seconds: a.seconds, request_id: a.request_id, source: a.source });
    if (a.error && !a.answer) { r.status = "fail"; r.reasons = [`ask error: ${a.error}`]; return (results[idx] = r); }
    const g = gradeAnswer(it, a.answer, r.truth, { seconds: a.seconds, maxSeconds });
    r.status = g.pass ? "pass" : "fail"; r.reasons = g.reasons; r.checks = g.checks;
    if (uiVals) {
      for (const ev of expectedValues(it, r.truth)) {
        if (!(ev.label in uiVals)) continue;
        const x = (it.ui_api.extract || []).find((e) => e.label === ev.label) || {};
        r.ui.push(threeWay({ label: ev.label, truth: ev.value, ui: uiVals[ev.label], chatOk: numberPresent(a.answer, ev.value, ev), tol: x.tol ?? ev.tol, tol_pct: x.tol_pct ?? ev.tol_pct, answer: a.answer }));
      }
    }
    return (results[idx] = r);
  }

  let next = 0;
  await Promise.all(Array.from({ length: Math.min(opt.concurrency, items.length) }, async () => {
    while (next < items.length) await one(next++);
  }));

  // ---- report ---------------------------------------------------------------------------
  const count = (s) => results.filter((r) => r.status === s).length;
  const summary = {
    started_at: startedAt.toISOString(), finished_at: new Date().toISOString(), mode: opt.truthOnly ? "truth-only" : "full",
    url: opt.truthOnly ? null : opt.url, subset: opt.subset, items: results.length,
    pass: count("pass"), fail: count("fail"), error: count("error"), skipped: count("skipped"), truth_ok: count("truth-ok"),
    cost_usd: +spent.toFixed(4), budget_usd: opt.budget, ui_note: ui.note,
    ui_mismatches: results.flatMap((r) => r.ui.filter((u) => /UI differs/.test(u.verdict)).map((u) => `${r.id}:${u.label}`)),
  };
  summary.accuracy = summary.pass + summary.fail ? +(summary.pass / (summary.pass + summary.fail)).toFixed(3) : null;
  const pad = (s, n) => String(s ?? "").slice(0, n).padEnd(n);
  console.log(`\n${pad("status", 9)} ${pad("id", 30)} ${pad("secs", 6)} ${pad("$", 6)} reason`);
  for (const r of results) {
    console.log(`${pad(r.status.toUpperCase(), 9)} ${pad(r.id, 30)} ${pad(r.seconds ?? "", 6)} ${pad(r.cost_usd != null ? r.cost_usd.toFixed(3) : "", 6)} ${r.reasons.join("; ").slice(0, 220)}`);
    for (const u of r.ui) console.log(`${" ".repeat(10)}UI ${u.label}: chat/SQL ${u.truth} vs UI ${u.ui} -> ${u.verdict}`);
    if (r.ui_error) console.log(`${" ".repeat(10)}UI read failed: ${r.ui_error}`);
  }
  console.log(`\n${summary.mode}: ${summary.pass} pass, ${summary.fail} fail, ${summary.error} error, ${summary.skipped} skipped` +
    `${summary.truth_ok ? `, ${summary.truth_ok} truth-ok` : ""}; accuracy ${summary.accuracy ?? "-"}; spend $${summary.cost_usd} of $${opt.budget}`);
  if (ui.note) console.log(ui.note);
  const withUi = golden.items.filter((i) => i.ui_api).map((i) => i.id);
  if (!ui.on) console.log(`Items with a ui_api mapping (${withUi.length}): ${withUi.join(", ")}`);

  const dir = path.join(STATE, "evals");
  fs.mkdirSync(dir, { recursive: true });
  const file = path.join(dir, `${startedAt.toISOString().replace(/[:.]/g, "-")}.json`);
  fs.writeFileSync(file, JSON.stringify({ summary, results }, null, 2));
  console.log(`report: ${file}`);

  if (!opt.truthOnly) {
    const events = await createEvents({ stateDir: STATE, log: () => {} });
    await events.emit("eval_run", { email: "bench@local" }, {
      pass: summary.pass, fail: summary.fail, error: summary.error, skipped: summary.skipped, items: summary.items,
      accuracy: summary.accuracy, cost_usd: summary.cost_usd, budget_usd: opt.budget, subset: opt.subset,
      failed_ids: results.filter((r) => r.status === "fail" || r.status === "error").map((r) => r.id),
      ui_mismatches: summary.ui_mismatches, report: path.basename(file),
    });
  }
  process.exitCode = summary.fail || summary.error ? 1 : 0;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  main().catch((e) => { console.error(`eval: ${e.message}`); process.exit(2); });
}
