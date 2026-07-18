#!/usr/bin/env node

import { existsSync, readFileSync, readdirSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

export function findings({ files, jobs, worker, analytics, deploy }) {
  const problems = [];
  const allTerraform = files.join("\n");

  if (/resource\s+"google_cloud_scheduler_job"/.test(allTerraform)) {
    problems.push("stg Terraform must declare zero Cloud Scheduler jobs");
  }
  if (/retire_legacy_stage_jobs/.test(allTerraform)) {
    problems.push("stg Terraform must not retain the transitional retirement flag");
  }
  for (const retired of [
    "legacy_stage_jobs",
    "google_cloud_run_v2_job\" \"kernel",
    "partition_maintainer",
  ]) {
    if (jobs.includes(retired)) problems.push(`stg cloud_run_jobs.tf still contains ${retired}`);
  }
  if (!/name\s*=\s*"GOATOS_WORKER_STAGES_ENABLED"\s+value\s*=\s*"true"/.test(worker)) {
    problems.push("stg kernel worker stages must be unconditionally enabled");
  }
  if (!/resource\s+"google_cloud_run_v2_job"\s+"analytics_rollup"/.test(analytics)) {
    problems.push("stg analytics rollup must remain available as a manual Cloud Run Job");
  }
  if (!deploy.includes("KERNEL_WORKER_SERVICE")) {
    problems.push("Cloud Deploy runner must name the kernel worker service");
  }
  if (!/services update "\$KERNEL_WORKER_SERVICE"/.test(deploy)) {
    problems.push("Cloud Deploy runner must update the kernel worker image");
  }
  if (!/service_image "\$KERNEL_WORKER_SERVICE"/.test(deploy)) {
    problems.push("Cloud Deploy runner must verify the kernel worker image");
  }
  return problems;
}

function selfTest() {
  const good = {
    files: ["resource \"google_cloud_run_v2_job\" \"migrate\" {}"],
    jobs: "resource \"google_cloud_run_v2_job\" \"migrate\" {}",
    worker: 'name = "GOATOS_WORKER_STAGES_ENABLED"\n        value = "true"',
    analytics: 'resource "google_cloud_run_v2_job" "analytics_rollup" {}',
    deploy: 'KERNEL_WORKER_SERVICE=x\ngcloud run services update "$KERNEL_WORKER_SERVICE"\nservice_image "$KERNEL_WORKER_SERVICE"',
  };
  if (findings(good).length) throw new Error(`clean fixture failed: ${findings(good)}`);

  const bad = {
    ...good,
    files: ['resource "google_cloud_scheduler_job" "legacy" {}\nretire_legacy_stage_jobs=true'],
    jobs: "legacy_stage_jobs partition_maintainer",
    worker: 'name = "GOATOS_WORKER_STAGES_ENABLED"\nvalue = "false"',
    analytics: "",
    deploy: "",
  };
  const got = findings(bad);
  if (got.length < 9) throw new Error(`unsafe fixture was not fully rejected: ${got}`);
  console.log("stg-disposable-topology guard: self-test passed");
}

function run() {
  const envDir = resolve(repo, "infra/envs/stg");
  const required = [
    resolve(envDir, "cloud_run_jobs.tf"),
    resolve(envDir, "cloud_run_worker.tf"),
    resolve(envDir, "analytics_rollup.tf"),
    resolve(repo, "tools/deploy/stg-clouddeploy-task.sh"),
  ];
  for (const path of required) {
    if (!existsSync(path)) {
      console.error(`stg-disposable-topology guard: missing ${path}`);
      process.exit(1);
    }
  }
  const terraform = readdirSync(envDir)
    .filter((name) => name.endsWith(".tf"))
    .sort()
    .map((name) => readFileSync(resolve(envDir, name), "utf8"));
  const problems = findings({
    files: terraform,
    jobs: readFileSync(required[0], "utf8"),
    worker: readFileSync(required[1], "utf8"),
    analytics: readFileSync(required[2], "utf8"),
    deploy: readFileSync(required[3], "utf8"),
  });
  if (problems.length) {
    console.error("stg-disposable-topology guard: FAIL");
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log("stg-disposable-topology guard: zero schedulers, one kernel worker, manual analytics, release skew protected");
}

if (process.argv.includes("--self-test")) selfTest();
else run();
