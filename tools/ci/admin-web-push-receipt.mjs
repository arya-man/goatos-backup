#!/usr/bin/env node
// admin-web-push-receipt.mjs — the push-gate RECEIPT: what the gate ran for a push, carried to the PR
// and to `make land-main`.
//
// WHY (J1B P0-2 + CI gap 2, 2026-09-30): the skip ledger was local only. It was not committed, not
// pushed, not on the PR, and it only recorded skips taken THROUGH the gate, so a push that never ran
// the gate (2d43dee4a: no visual-gate pass, no ledger row) left no trace at all. Now:
//   1. every push the gate sees writes a receipt: SHA, ref, branch, admin-web input digest, every
//      lane with pass / reused-pass / skip (+ the written reason) / not-applicable, the skip-ledger
//      rows for the SHA, the hook source that ran, and the commits the push covered (sha + stable
//      patch-id, so a rebase keeps the coverage);
//   2. `publish` posts it on the PR as the `goatos/push-gate` commit status once the SHA is on
//      GitHub (success = every lane ran and passed; failure = a lane was skipped, reason shown);
//   3. `check-range` is run by tools/ci/land-main.sh: every commit in origin/main..candidate must be
//      covered by a receipt (by SHA or patch-id) or the landing is REFUSED; the receipts (and every
//      skip reason) are attached to the land-main receipt and to its GitHub status.
//
// Store: $GOATOS_PUSH_RECEIPT_DIR, default <git-common-dir>/goatos-push-gate/receipts
//   <sha>.<ref>.<epoch>.json   the receipt
//   by-commit/<sha>            receipt file names that cover the commit (one per line)
//   by-patch/<patch-id>        same, keyed by `git patch-id --stable`
//
// Usage:
//   write --sha S --ref R [--remote-sha RS] [--base B] [--digest D] [--results FILE] [--kind K]
//         [--ledger FILE] [--applicable 0|1]            -> prints the receipt path
//       results FILE: lane<TAB>status<TAB>seconds<TAB>reason   (status pass|reused-pass|skip|not-applicable)
//       A skip without a >= 12 character reason is REFUSED (exit 1).
//   publish --receipt FILE [--remote URL] [--wait SECONDS]
//   check-range --base B --head H [--out FILE]        -> exit 1 when a commit has no receipt
//   show --sha S
//   --self-test
import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const ZERO = "0".repeat(40);
const MIN_REASON = 12;
const STATUS_CONTEXT = process.env.GOATOS_PUSH_RECEIPT_CONTEXT || "goatos/push-gate";

function git(args, opts = {}) {
  return execFileSync("git", args, { encoding: "utf8", stdio: ["pipe", "pipe", "ignore"], maxBuffer: 1 << 30, ...opts }).trim();
}
function tryGit(args, opts) {
  try { return git(args, opts); } catch { return ""; }
}

export function receiptDir() {
  if (process.env.GOATOS_PUSH_RECEIPT_DIR) return process.env.GOATOS_PUSH_RECEIPT_DIR;
  const common = path.resolve(git(["rev-parse", "--git-common-dir"]));
  return path.join(common, "goatos-push-gate", "receipts");
}

function parseArgs(argv) {
  const out = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a.startsWith("--")) {
      const key = a.slice(2);
      const next = argv[i + 1];
      if (next === undefined || next.startsWith("--")) out[key] = true;
      else { out[key] = next; i++; }
    } else out._.push(a);
  }
  return out;
}

// sha -> patch-id for every non-merge commit with a diff in RANGE (one git pass).
export function patchIds(range) {
  const log = spawnSync("git", ["log", "-p", "--no-color", "--no-merges", "--format=commit %H", range], { encoding: "utf8", maxBuffer: 1 << 30 });
  if (log.status !== 0) return new Map();
  const pid = spawnSync("git", ["patch-id", "--stable"], { input: log.stdout, encoding: "utf8", maxBuffer: 1 << 30 });
  const map = new Map();
  for (const line of (pid.stdout || "").split("\n")) {
    const [p, sha] = line.trim().split(/\s+/);
    if (p && sha) map.set(sha, p);
  }
  return map;
}

function revList(range) {
  const r = spawnSync("git", ["rev-list", range], { encoding: "utf8", maxBuffer: 1 << 30 });
  if (r.status !== 0) throw new Error(`git rev-list ${range} failed: ${(r.stderr || "").trim()}`);
  return r.stdout.split("\n").map((s) => s.trim()).filter(Boolean);
}

function isCommit(sha) {
  return Boolean(sha) && sha !== ZERO && spawnSync("git", ["cat-file", "-e", `${sha}^{commit}`]).status === 0;
}

// The commits a push of SHA over REMOTE_SHA covers (what the reviewer sees as new on the ref).
export function coverRange({ sha, remoteSha, base }) {
  if (isCommit(remoteSha)) return `${remoteSha}..${sha}`;
  if (base && isCommit(base)) return `${base}..${sha}`;
  const mb = tryGit(["merge-base", sha, "refs/remotes/origin/main"]);
  if (mb) return `${mb}..${sha}`;
  return null; // unrelated history: the tip only
}

export function readResults(file) {
  if (!file || file === true || !fs.existsSync(file)) return [];
  return fs.readFileSync(file, "utf8").split("\n").filter((l) => l.trim()).map((l) => {
    const [lane, status, seconds, ...rest] = l.split("\t");
    return { lane, status, seconds: seconds ? Number(String(seconds).replace(/s$/, "")) || 0 : 0, reason: rest.join(" ").trim() || undefined };
  });
}

export function validateLanes(lanes) {
  const errors = [];
  for (const l of lanes) {
    if (!["pass", "reused-pass", "skip", "not-applicable"].includes(l.status)) errors.push(`lane ${l.lane}: unknown status '${l.status}'`);
    if (l.status === "skip" && (!l.reason || l.reason.length < MIN_REASON)) {
      errors.push(`lane ${l.lane} was SKIPPED without a written reason (GOATOS_SKIP_REASON, >= ${MIN_REASON} chars); the receipt refuses it`);
    }
  }
  return errors;
}

function ledgerRows(ledgerFile, sha) {
  if (!ledgerFile || ledgerFile === true || !fs.existsSync(ledgerFile)) return [];
  return fs.readFileSync(ledgerFile, "utf8").split("\n").filter(Boolean).map((l) => l.split("\t"))
    .filter((f) => f[1] === sha)
    .map(([time, s, branch, flag, lane, user, reason]) => ({ time, sha: s, branch, flag, lane, user, reason }));
}

export function verdict(receipt) {
  if (!receipt.applicable) return { state: "success", text: "admin-web lanes not applicable (no admin-web input changed)" };
  const skipped = receipt.lanes.filter((l) => l.status === "skip");
  const ledgerSkips = (receipt.ledger || []).filter((r) => !skipped.some((l) => r.lane.includes(l.lane)));
  if (skipped.length || ledgerSkips.length) {
    const parts = [...skipped.map((l) => `SKIP ${l.lane}: ${l.reason}`), ...ledgerSkips.map((r) => `SKIP ${r.lane}: ${r.reason}`)];
    return { state: "failure", text: parts.join("; ") };
  }
  return { state: "success", text: `${receipt.lanes.length} lanes passed: ${receipt.lanes.map((l) => l.lane).join(", ")}` };
}

function appendIndex(dir, sub, key, value) {
  const d = path.join(dir, sub);
  fs.mkdirSync(d, { recursive: true });
  fs.appendFileSync(path.join(d, key), `${value}\n`);
}

export function writeReceipt(opts) {
  const sha = opts.sha;
  if (!isCommit(sha)) throw new Error(`--sha ${sha} is not a commit`);
  const lanes = readResults(opts.results);
  const errors = validateLanes(lanes);
  if (errors.length) { const e = new Error(errors.join("\n")); e.refused = true; throw e; }
  const range = coverRange({ sha, remoteSha: opts["remote-sha"], base: opts.base });
  const commits = range ? revList(range) : [sha];
  if (!commits.includes(sha)) commits.unshift(sha);
  const pids = range ? patchIds(range) : new Map();
  const applicable = opts.applicable === undefined ? lanes.some((l) => l.status !== "not-applicable") : String(opts.applicable) === "1";
  const receipt = {
    version: 1,
    kind: opts.kind || "pre-push",
    sha,
    ref: opts.ref || null,
    remote_sha: opts["remote-sha"] || null,
    remote: opts.remote && opts.remote !== true ? String(opts.remote).replace(/x-access-token:[^@]+@/, "") : null,
    branch: tryGit(["rev-parse", "--abbrev-ref", "HEAD"]) || null,
    digest: opts.digest || null,
    created_at: new Date().toISOString(),
    user: process.env.USER || os.userInfo().username,
    host: os.hostname(),
    hook_source: process.env.GOATOS_PUSH_HOOK_SOURCE || null,
    applicable,
    lanes,
    ledger: ledgerRows(opts.ledger, sha),
    range,
    covers: commits.map((c) => ({ sha: c, patch_id: pids.get(c) || null })),
  };
  receipt.verdict = verdict(receipt);
  const dir = receiptDir();
  fs.mkdirSync(dir, { recursive: true });
  const refSlug = String(receipt.ref || "none").replace(/^refs\/heads\//, "").replace(/[^A-Za-z0-9._-]+/g, "_");
  const name = `${sha}.${refSlug}.${Date.now()}.json`;
  fs.writeFileSync(path.join(dir, name), `${JSON.stringify(receipt, null, 2)}\n`);
  for (const c of receipt.covers) {
    appendIndex(dir, "by-commit", c.sha, name);
    if (c.patch_id) appendIndex(dir, "by-patch", c.patch_id, name);
  }
  return { file: path.join(dir, name), receipt };
}

function readIndex(dir, sub, key) {
  const f = path.join(dir, sub, key);
  if (!fs.existsSync(f)) return [];
  return fs.readFileSync(f, "utf8").split("\n").filter(Boolean);
}

export function checkRange({ base, head }) {
  const dir = receiptDir();
  const range = `${base}..${head}`;
  const commits = revList(range);
  const pids = patchIds(range);
  const rows = [];
  const missing = [];
  const receipts = new Map();
  for (const c of commits) {
    let names = readIndex(dir, "by-commit", c);
    let via = "sha";
    if (!names.length && pids.get(c)) { names = readIndex(dir, "by-patch", pids.get(c)); via = "patch-id"; }
    if (!names.length) { missing.push(c); continue; }
    const name = names[names.length - 1];
    if (!receipts.has(name)) {
      try { receipts.set(name, JSON.parse(fs.readFileSync(path.join(dir, name), "utf8"))); } catch { receipts.set(name, null); }
    }
    rows.push({ sha: c, via, receipt: name });
  }
  const skips = [];
  for (const [name, r] of receipts) {
    if (!r) continue;
    for (const l of r.lanes || []) if (l.status === "skip") skips.push({ receipt: name, sha: r.sha, lane: l.lane, reason: l.reason });
    for (const x of r.ledger || []) skips.push({ receipt: name, sha: r.sha, lane: x.lane, reason: x.reason, flag: x.flag });
  }
  return { base, head, dir, commits: rows, missing, receipts: [...receipts.keys()], skips };
}

async function publish(opts) {
  const file = opts.receipt;
  const r = JSON.parse(fs.readFileSync(file, "utf8"));
  const remote = (opts.remote && opts.remote !== true) ? opts.remote : "origin";
  const wait = Number(opts.wait || 240);
  const ref = r.ref;
  const deadline = Date.now() + wait * 1000;
  let seen = false;
  while (Date.now() < deadline) {
    const out = tryGit(["ls-remote", remote, ref]);
    if (out.split(/\s+/)[0] === r.sha) { seen = true; break; }
    await new Promise((res) => setTimeout(res, 4000));
  }
  if (!seen) { console.log(`push-receipt: ${r.sha.slice(0, 12)} never appeared on ${ref} within ${wait}s (dry-run or refused push); status not posted`); return 0; }
  const slug = (tryGit(["remote", "get-url", "origin"]).match(/github\.com[:/](.+?)(\.git)?$/) || [])[1] || "vgoats/goatos";
  const v = r.verdict || verdict(r);
  const desc = v.text.length > 140 ? `${v.text.slice(0, 137)}...` : v.text;
  const res = spawnSync("gh", ["api", "-X", "POST", `repos/${slug}/statuses/${r.sha}`, "-f", `state=${v.state}`, "-f", `context=${STATUS_CONTEXT}`, "-f", `description=${desc}`], { encoding: "utf8" });
  if (res.status !== 0) { console.log(`push-receipt: could not post ${STATUS_CONTEXT} on ${r.sha.slice(0, 12)}: ${(res.stderr || res.error || "").toString().trim()}`); return 1; }
  console.log(`push-receipt: posted ${STATUS_CONTEXT}=${v.state} on ${r.sha.slice(0, 12)} (${ref}): ${desc}`);
  return 0;
}

function selfTest() {
  const assert = (c, m) => { if (!c) { console.error(`self-test FAIL: ${m}`); process.exit(1); } };
  assert(validateLanes([{ lane: "visual-gate", status: "skip" }]).length === 1, "skip without reason must be refused");
  assert(validateLanes([{ lane: "visual-gate", status: "skip", reason: "short" }]).length === 1, "short reason must be refused");
  assert(validateLanes([{ lane: "visual-gate", status: "skip", reason: "local API down for the outage" }]).length === 0, "reasoned skip allowed");
  assert(validateLanes([{ lane: "x", status: "maybe" }]).length === 1, "unknown status refused");
  const skip = verdict({ applicable: true, lanes: [{ lane: "design-guard", status: "pass" }, { lane: "visual-gate", status: "skip", reason: "local API down for the outage" }] });
  assert(skip.state === "failure" && /SKIP visual-gate: local API down/.test(skip.text), "a skip is a failure status that names the reason");
  const ok = verdict({ applicable: true, lanes: [{ lane: "design-guard", status: "pass" }, { lane: "visual-gate", status: "reused-pass" }] });
  assert(ok.state === "success", "all lanes passed = success");
  assert(verdict({ applicable: false, lanes: [] }).state === "success", "not applicable = success");
  console.log("admin-web-push-receipt self-test: OK");
}

const args = parseArgs(process.argv.slice(2));
const cmd = args._[0] || (args["self-test"] ? "self-test" : "");
try {
  if (cmd === "self-test") selfTest();
  else if (cmd === "write") {
    const { file, receipt } = writeReceipt(args);
    console.error(`admin-web-push-receipt: ${receipt.sha.slice(0, 12)} ${receipt.ref || ""} -> ${receipt.verdict.state}: ${receipt.verdict.text} (covers ${receipt.covers.length} commit(s))`);
    console.log(file);
  } else if (cmd === "publish") process.exit(await publish(args));
  else if (cmd === "check-range") {
    if (!args.base || !args.head) throw new Error("check-range needs --base and --head");
    const res = checkRange({ base: args.base, head: args.head });
    if (args.out && args.out !== true) fs.writeFileSync(args.out, `${JSON.stringify(res, null, 2)}\n`);
    if (res.missing.length) {
      console.error(`!! push-gate receipts: ${res.missing.length} of ${res.missing.length + res.commits.length} commit(s) in ${args.base.slice(0, 12)}..${args.head.slice(0, 12)} have NO gate receipt (never pushed through the push gate, or pushed with --no-verify / from a machine without it):`);
      for (const m of res.missing.slice(0, 25)) console.error(`!!   ${m.slice(0, 12)} ${tryGit(["log", "-1", "--format=%s", m]).slice(0, 90)}`);
      if (res.missing.length > 25) console.error(`!!   ... and ${res.missing.length - 25} more`);
      console.error("!! Fix: run the lanes on the candidate and record a receipt for the range: tools/ci/admin-web-push-gate.sh --certify <base>");
      process.exit(1);
    }
    console.log(`push-gate receipts: all ${res.commits.length} commit(s) covered by ${res.receipts.length} receipt(s); ${res.skips.length} skipped lane(s)`);
    for (const s of res.skips) console.log(`!!   SKIP ${s.lane} @ ${String(s.sha).slice(0, 12)}: ${s.reason}`);
  } else if (cmd === "show") {
    const dir = receiptDir();
    for (const n of readIndex(dir, "by-commit", args.sha)) console.log(fs.readFileSync(path.join(dir, n), "utf8"));
  } else {
    console.error("usage: admin-web-push-receipt.mjs write|publish|check-range|show|self-test ...");
    process.exit(2);
  }
} catch (e) {
  console.error(`!! admin-web-push-receipt: ${e.refused ? "REFUSED — " : ""}${e.message}`);
  process.exit(1);
}
