#!/usr/bin/env node
// grafana-provisioning-guard
//
// Grafana deploy/smoke moved to vgoats/mesha-ops
// (docs/decisions/grafana-owned-by-mesha-ops.md), but infra/grafana/{dashboards,
// provisioning} still live here and infra/envs/stg/observability.tf reads them.
// This guard keeps those files honest:
//   1. every dashboard JSON parses and has a uid + title;
//   2. observability.tf still wires provisioning/datasources, provisioning/dashboards
//      and the dashboards fileset (else a dashboard edit here silently goes nowhere);
//   3. no STG deploy script/config reads infra/grafana (deploy authority is mesha-ops).
// Blind spots: does not validate Grafana panel semantics or datasource uids.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const DEPLOY_FILES = [
  "cloudbuild.stg.yaml",
  "tools/deploy/stg-clouddeploy-task.sh",
  "tools/deploy/stg-clouddeploy-release.sh",
  "tools/deploy/stg-cloudbuild-release.sh",
];
const TF = "infra/envs/stg/observability.tf";
const TF_NEEDLES = [
  "grafana/provisioning/datasources/datasources.yaml",
  "grafana/provisioning/dashboards/dashboards.yaml",
  "grafana/dashboards",
];

export function check(root) {
  const errs = [];
  const dashDir = path.join(root, "infra/grafana/dashboards");
  const dashes = fs.existsSync(dashDir) ? fs.readdirSync(dashDir).filter((f) => f.endsWith(".json")) : [];
  for (const f of dashes) {
    try {
      const j = JSON.parse(fs.readFileSync(path.join(dashDir, f), "utf8"));
      if (!j.uid || !j.title) errs.push(`${f}: dashboard missing uid/title`);
    } catch (e) {
      errs.push(`${f}: invalid JSON (${e.message})`);
    }
  }
  if (dashes.length > 0) {
    const tfPath = path.join(root, TF);
    const tf = fs.existsSync(tfPath) ? fs.readFileSync(tfPath, "utf8") : "";
    for (const n of TF_NEEDLES) if (!tf.includes(n)) errs.push(`${TF} no longer references ${n}`);
  }
  for (const f of DEPLOY_FILES) {
    const p = path.join(root, f);
    if (fs.existsSync(p) && /infra\/grafana\//.test(fs.readFileSync(p, "utf8"))) {
      errs.push(`${f} reads infra/grafana (Grafana deploy is owned by vgoats/mesha-ops)`);
    }
  }
  return errs;
}

function fixture(mut) {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), "grafana-guard-"));
  const w = (p, s) => { fs.mkdirSync(path.dirname(path.join(d, p)), { recursive: true }); fs.writeFileSync(path.join(d, p), s); };
  w("infra/grafana/dashboards/a.json", JSON.stringify({ uid: "a", title: "A" }));
  w(TF, TF_NEEDLES.map((n) => `source = "../../${n}"`).join("\n"));
  w("cloudbuild.stg.yaml", "steps: []\n");
  mut?.(w);
  return d;
}

if (process.argv.includes("--self-test")) {
  const cases = [
    ["clean", null, 0],
    ["bad json", (w) => w("infra/grafana/dashboards/b.json", "{"), 1],
    ["missing uid", (w) => w("infra/grafana/dashboards/b.json", "{\"title\":\"x\"}"), 1],
    ["tf unwired", (w) => w(TF, "nothing"), 3],
    ["deploy reads grafana", (w) => w("cloudbuild.stg.yaml", "cp infra/grafana/dashboards x"), 1],
  ];
  let bad = 0;
  for (const [name, mut, want] of cases) {
    const got = check(fixture(mut)).length;
    if (got !== want) { console.error(`self-test ${name}: want ${want} errors, got ${got}`); bad++; }
  }
  if (bad) process.exit(1);
  console.log("grafana-provisioning-guard self-test: ok");
} else {
  const errs = check(process.cwd());
  if (errs.length) { for (const e of errs) console.error(`grafana-provisioning-guard: ${e}`); process.exit(1); }
  console.log("grafana-provisioning-guard: ok");
}
