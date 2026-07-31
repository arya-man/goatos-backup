#!/usr/bin/env node

// check-weighing-kernel-phase2-guard.mjs — the WEIGHING PHASE 2 time-driven
// kernel invariants. Weighing shipped Phase 1 with NO durable work item and NO
// cadence at all: publishing a campaign produced a plan nothing swept, so nothing
// could roll forward, go delayed, or escalate, and Calendar/Control Tower had no
// weighing process state. This guard keeps that hole closed.
//
// Owning docs:
//   docs/features/weighing/TRD.md (sections 7, 13, 16)
//   context/architecture/operational-kernel.md
//   docs/decisions/operational-kernel-5k-50k-scale-envelope.md
//
// Failure modes (each has a pure finding function, an in-process self-test, and a
// real-process exit-code self-test):
//
//   1. publish-without-work-items — the weighing publish transaction
//      (PublishCampaign) must materialize durable work items IN THAT transaction.
//      A published campaign with no work items is a plan the kernel cannot see.
//   2. unbounded-sweeper — every weighing work-item claim must be keyset-chunked
//      with FOR UPDATE SKIP LOCKED and a LIMIT, and must never use OFFSET. A
//      polling full scan is the banned shape.
//   3. hour-arithmetic — weighing kernel time comparisons are Asia/Kolkata
//      BUSINESS DAYS. No `time.Hour` offsets, no `now() - interval '.. hours'`,
//      no `Add(...Hour)`. Sub-day precision on weighing work is a defect.
//   4. cadence-not-registered — the weighing cadence must be registered inside the
//      EXISTING backend/cmd/kernel-worker cadence classes. A new worker binary, a
//      Cloud Scheduler cron, or a scheduled Cloud Run Job is not the topology.
//   5. hardcoded-recipients — day-start/rolled-forward/delayed recipients must
//      resolve from role grants / assignment rows through the recipient resolver.
//      A literal email, phone, FCM token, or person name in the cadence handler is
//      seed-time routing and is banned.
//
// Blind spots (native Grep/Read must still catch these): a work-item write added
// in a file other than the weighing postgres adapter; a cadence registered through
// a variable indirection the literal constructor check cannot see; a recipient
// list built in a helper package this guard does not scan.

import { readFileSync, existsSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { join, resolve } from "node:path";
import { tmpdir } from "node:os";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repo = process.env.WEIGHING_KERNEL_GUARD_TEST_REPO
  ? resolve(process.env.WEIGHING_KERNEL_GUARD_TEST_REPO)
  : resolve(import.meta.dirname, "../..");
const SELF_PATH = fileURLToPath(import.meta.url);

const KERNEL_FILE = "backend/internal/weighing/adapters/postgres/kernel.go";
const PUBLISH_FILE = "backend/internal/weighing/adapters/postgres/repository.go";
const WORKER_FILE = "backend/cmd/kernel-worker/main.go";
const NOTIFY_FILE = "backend/internal/notificationbridge/weighing_lifecycle_notify_consumer.go";

// ---------------------------------------------------------------------------
// mode 1: publish must create work items in the publish transaction
// ---------------------------------------------------------------------------
export function findingsForPublishSource(rel, source) {
  const findings = [];
  const start = source.indexOf("func (r *Repository) PublishCampaign(");
  if (start < 0) return findings; // not the publish file
  // Bound the scan to the PublishCampaign body: the next top-level func.
  const rest = source.slice(start + 1);
  const nextFunc = rest.indexOf("\nfunc ");
  const body = nextFunc < 0 ? rest : rest.slice(0, nextFunc);
  if (!/createWorkItemsForPublishTx\s*\(\s*ctx\s*,\s*tx\b/.test(body)) {
    findings.push({
      rule: "publish-without-work-items",
      message: `${rel}: PublishCampaign does not call createWorkItemsForPublishTx(ctx, tx, ...) — a published weighing campaign must materialize its durable work items IN the publish transaction, or the kernel has nothing to sweep`,
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// mode 2 + 3: the sweeper must be bounded/keyset and business-day only
// ---------------------------------------------------------------------------
export function findingsForKernelSource(rel, source) {
  const findings = [];

  // Every claim over weighing_work_items must lock with SKIP LOCKED and bound with
  // LIMIT. We look at each `FROM weighing_work_items` claim block.
  const claimBlocks = source.split(/FROM\s+weighing_work_items/i).slice(1);
  let claimIndex = 0;
  for (const rawBlock of claimBlocks) {
    claimIndex += 1;
    const block = rawBlock.slice(0, 1200);
    const isClaim = /FOR\s+UPDATE/i.test(block) || /\bORDER BY\b[\s\S]{0,200}\bLIMIT\b/i.test(block);
    if (!isClaim) continue; // an aggregate read, not a claim
    if (!/FOR\s+UPDATE(\s+OF\s+\w+)?\s+SKIP\s+LOCKED/i.test(block)) {
      findings.push({
        rule: "unbounded-sweeper",
        message: `${rel}: weighing_work_items claim #${claimIndex} locks rows without FOR UPDATE SKIP LOCKED — copy the keyset-chunked claim used by the obligation/idempotency sweepers`,
      });
    }
    if (!/\bLIMIT\b/i.test(block)) {
      findings.push({
        rule: "unbounded-sweeper",
        message: `${rel}: weighing_work_items claim #${claimIndex} has no LIMIT — an unbounded worker tick is a polling full scan`,
      });
    }
    if (!/\bwork_item_id\s*>\s*\$\d/.test(block)) {
      findings.push({
        rule: "unbounded-sweeper",
        message: `${rel}: weighing_work_items claim #${claimIndex} has no keyset cursor predicate (work_item_id > $n) — the claim loop must provably advance`,
      });
    }
  }
  if (/\bOFFSET\s+\$?\d/i.test(source)) {
    findings.push({
      rule: "unbounded-sweeper",
      message: `${rel}: uses OFFSET pagination — the weighing kernel claim must be keyset/cursor based`,
    });
  }

  for (const f of businessDayFindings(rel, source)) findings.push(f);
  return findings;
}

// businessDayFindings bans sub-day time arithmetic. Weighing work has business-DAY
// grain; an hour offset is a defect in the making (a fixture or a query that
// passes or fails depending on the time of day it runs).
export function businessDayFindings(rel, source) {
  const findings = [];
  const goHour = /\.Add\(\s*-?\s*\d*\s*\*?\s*time\.(Hour|Minute)\b/;
  if (goHour.test(source)) {
    findings.push({
      rule: "hour-arithmetic",
      message: `${rel}: uses sub-day time arithmetic (Add(... time.Hour/Minute)) — weighing work items have Asia/Kolkata BUSINESS DAY grain; anchor to biztime.BusinessDate / biztime.BusinessDayStart instead`,
    });
  }
  if (/now\(\)\s*[-+]\s*interval\s*'[^']*\b(hour|minute|second)/i.test(source)) {
    findings.push({
      rule: "hour-arithmetic",
      message: `${rel}: uses SQL now() +/- an hour/minute interval for a weighing business comparison — compare business DATES (::date) resolved from biztime instead`,
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// mode 4: the cadence must live in the existing kernel worker
// ---------------------------------------------------------------------------
export function findingsForWorkerSource(rel, source) {
  const findings = [];
  const ctor = "kernelstages.NewWeighingKernelStage(";
  const idx = source.indexOf(ctor);
  if (idx < 0) {
    findings.push({
      rule: "cadence-not-registered",
      message: `${rel}: the weighing kernel stage is not registered — the weighing cadence must run inside the EXISTING consolidated kernel worker, not a new binary or a Cloud Scheduler cron`,
    });
    return findings;
  }
  const preceding = source.slice(0, idx);
  if (preceding.lastIndexOf("supervisor.RegisterCadence") < 0) {
    findings.push({
      rule: "cadence-not-registered",
      message: `${rel}: the weighing kernel stage is constructed but not attached to a supervisor cadence — a stage that is never scheduled surfaces nothing`,
    });
  }
  return findings;
}

// ---------------------------------------------------------------------------
// mode 5: cadence recipients come from grants/assignment rows only
// ---------------------------------------------------------------------------
export function findingsForNotifySource(rel, source) {
  const findings = [];
  const start = source.indexOf("handleWorkItemCadence");
  if (start < 0) {
    findings.push({
      rule: "hardcoded-recipients",
      message: `${rel}: no handleWorkItemCadence handler — the Phase 2 day-start/rolled-forward/delayed cadences must be consumed by the EXISTING weighing lifecycle consumer, not a parallel one`,
    });
    return findings;
  }
  const body = source.slice(start);
  if (!/ResolveMemberRecipients\s*\(/.test(body) || !/leadershipRecipients\s*\(|ResolvePositionRecipients\s*\(/.test(body)) {
    findings.push({
      rule: "hardcoded-recipients",
      message: `${rel}: handleWorkItemCadence does not resolve recipients through ResolveMemberRecipients (assignment-row operator) and the role-grant leadership resolver`,
    });
  }
  const literalEmail = /"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"/;
  const literalPhone = /"\+?\d{10,15}"/;
  const literalToken = /"(?:fcm|token|device)[-_][A-Za-z0-9._-]{6,}"/i;
  for (const [re, what] of [[literalEmail, "an email address"], [literalPhone, "a phone number"], [literalToken, "an FCM/device token"]]) {
    if (re.test(body)) {
      findings.push({
        rule: "hardcoded-recipients",
        message: `${rel}: handleWorkItemCadence contains ${what} literal — cadence recipients must resolve from active role grants and assignment rows, never seed-time/hardcoded routing`,
      });
    }
  }
  return findings;
}

const CHECKS = [
  [PUBLISH_FILE, findingsForPublishSource],
  [KERNEL_FILE, findingsForKernelSource],
  [WORKER_FILE, findingsForWorkerSource],
  [NOTIFY_FILE, findingsForNotifySource],
];

function run() {
  const problems = [];
  let scanned = 0;
  for (const [rel, fn] of CHECKS) {
    const path = resolve(repo, rel);
    if (!existsSync(path)) {
      problems.push(`[missing-file] required weighing Phase 2 file not found: ${rel}`);
      continue;
    }
    scanned += 1;
    for (const f of fn(rel, readFileSync(path, "utf8"))) {
      problems.push(`[${f.rule}] ${f.message}`);
    }
  }
  if (problems.length > 0) {
    console.error("weighing-kernel-phase2 guard failed:");
    for (const p of problems) console.error(`- ${p}`);
    process.exit(1);
  }
  console.log(`weighing-kernel-phase2 guard: ok (${scanned} files, 5 failure modes)`);
}

// ---------------------------------------------------------------------------
// self-tests
// ---------------------------------------------------------------------------
const GOOD_PUBLISH = `package postgres

func (r *Repository) PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error) {
	if _, err := r.createWorkItemsForPublishTx(ctx, tx, tenantID, campaignID); err != nil {
		return domain.Campaign{}, err
	}
	return c, tx.Commit(ctx)
}

func (r *Repository) Other() {}
`;

const GOOD_KERNEL = `package postgres

const q = \`
WITH claimed AS (
  SELECT work_item_id
  FROM weighing_work_items
  WHERE tenant_id = $1::uuid
    AND due_business_date < $2::date
    AND work_item_id > $3::uuid
  ORDER BY work_item_id
  LIMIT $4
  FOR UPDATE SKIP LOCKED
)
UPDATE weighing_work_items SET due_business_date = $2::date\`

func date(t time.Time) string { return biztime.BusinessDate(t) }
`;

const GOOD_WORKER = `package main

func run() {
	supervisor.RegisterCadence("operational", 5*time.Minute,
		kernelstages.NewWeighingKernelStage(deps, tenantID),
	)
}
`;

const GOOD_NOTIFY = `package notificationbridge

func (c *WeighingLifecycleEventConsumer) handleWorkItemCadence(ctx context.Context, event eventbus.Event) error {
	devices, err := c.recipients.ResolveMemberRecipients(ctx, tenantID, payload.OperatorID)
	leadership, err := c.leadershipRecipients(ctx, tenantID)
	return nil
}
`;

function selfTest() {
  const expectClean = (label, fn, rel, src) => {
    const got = fn(rel, src);
    if (got.length !== 0) throw new Error(`self-test failed: false positive [${label}]: ${JSON.stringify(got)}`);
  };
  const expectRule = (label, fn, rel, src, rule) => {
    const got = fn(rel, src);
    if (!got.some((f) => f.rule === rule)) {
      throw new Error(`self-test failed: [${label}] did not flag ${rule}. got: ${JSON.stringify(got)}`);
    }
  };

  expectClean("clean publish", findingsForPublishSource, PUBLISH_FILE, GOOD_PUBLISH);
  expectRule("publish missing work items", findingsForPublishSource, PUBLISH_FILE,
    GOOD_PUBLISH.replace(/if _, err := r\.createWorkItemsForPublishTx[\s\S]*?\n\t\}\n/, ""), "publish-without-work-items");
  // ADVERSARIAL SIBLING: the call exists in the FILE but in a different function,
  // so a naive whole-file grep would pass. The publish body is what must have it.
  expectRule("publish work items in a sibling func only", findingsForPublishSource, PUBLISH_FILE, `package postgres

func (r *Repository) PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error) {
	return c, tx.Commit(ctx)
}

func (r *Repository) SomethingElse(ctx context.Context) error {
	_, err := r.createWorkItemsForPublishTx(ctx, tx, tenantID, campaignID)
	return err
}
`, "publish-without-work-items");

  expectClean("clean kernel", findingsForKernelSource, KERNEL_FILE, GOOD_KERNEL);
  expectRule("claim without SKIP LOCKED", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL.replace("FOR UPDATE SKIP LOCKED", "FOR UPDATE"), "unbounded-sweeper");
  expectRule("claim without LIMIT", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL.replace("  LIMIT $4\n", ""), "unbounded-sweeper");
  expectRule("claim without keyset cursor", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL.replace("    AND work_item_id > $3::uuid\n", ""), "unbounded-sweeper");
  expectRule("offset pagination", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL.replace("  LIMIT $4", "  LIMIT $4 OFFSET $5"), "unbounded-sweeper");
  expectRule("go hour arithmetic", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL + "\nvar x = time.Now().Add(-2 * time.Hour)\n", "hour-arithmetic");
  expectRule("sql hour interval", findingsForKernelSource, KERNEL_FILE,
    GOOD_KERNEL + "\nconst bad = `WHERE due_at < now() - interval '12 hours'`\n", "hour-arithmetic");

  expectClean("clean worker", findingsForWorkerSource, WORKER_FILE, GOOD_WORKER);
  expectRule("stage not registered", findingsForWorkerSource, WORKER_FILE, "package main\nfunc run() {}\n", "cadence-not-registered");
  expectRule("stage built but not scheduled", findingsForWorkerSource, WORKER_FILE,
    "package main\nfunc run() {\n\tstage := kernelstages.NewWeighingKernelStage(deps, tenantID)\n\t_ = stage\n}\n", "cadence-not-registered");

  expectClean("clean notify", findingsForNotifySource, NOTIFY_FILE, GOOD_NOTIFY);
  expectRule("no cadence handler", findingsForNotifySource, NOTIFY_FILE, "package notificationbridge\n", "hardcoded-recipients");
  expectRule("hardcoded email recipient", findingsForNotifySource, NOTIFY_FILE,
    GOOD_NOTIFY.replace("payload.OperatorID", `"someone@mesha.sg"`), "hardcoded-recipients");
  expectRule("hardcoded fcm token", findingsForNotifySource, NOTIFY_FILE,
    GOOD_NOTIFY + `\nvar t = "fcm-abc123def456"\n`, "hardcoded-recipients");

  console.log("weighing-kernel-phase2 guard: self-test passed (5/5 failure modes, incl. adversarial sibling-function fixture)");
}

function runOneExitCodeCase(label, files, expectFailure) {
  const dir = mkdtempSync(join(tmpdir(), "weighing-kernel-phase2-guard-"));
  try {
    for (const [rel, source] of Object.entries(files)) {
      const path = join(dir, rel);
      mkdirSync(resolve(path, ".."), { recursive: true });
      writeFileSync(path, source);
    }
    const result = spawnSync(process.execPath, [SELF_PATH], {
      env: { ...process.env, WEIGHING_KERNEL_GUARD_TEST_REPO: dir },
      encoding: "utf8",
    });
    const failed = result.status !== 0;
    if (failed !== expectFailure) {
      throw new Error(
        `exit-code self-test failed [${label}]: expected exit ${expectFailure ? "non-zero" : "0"}, got ${result.status}\nstdout: ${result.stdout}\nstderr: ${result.stderr}`,
      );
    }
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function selfTestExitCodes() {
  const clean = {
    [PUBLISH_FILE]: GOOD_PUBLISH,
    [KERNEL_FILE]: GOOD_KERNEL,
    [WORKER_FILE]: GOOD_WORKER,
    [NOTIFY_FILE]: GOOD_NOTIFY,
  };
  runOneExitCodeCase("clean tree", clean, false);
  runOneExitCodeCase("mode 1: publish-without-work-items",
    { ...clean, [PUBLISH_FILE]: GOOD_PUBLISH.replace(/if _, err := r\.createWorkItemsForPublishTx[\s\S]*?\n\t\}\n/, "") }, true);
  runOneExitCodeCase("mode 2: unbounded-sweeper",
    { ...clean, [KERNEL_FILE]: GOOD_KERNEL.replace("FOR UPDATE SKIP LOCKED", "FOR UPDATE") }, true);
  runOneExitCodeCase("mode 3: hour-arithmetic",
    { ...clean, [KERNEL_FILE]: GOOD_KERNEL + "\nvar x = time.Now().Add(-2 * time.Hour)\n" }, true);
  runOneExitCodeCase("mode 4: cadence-not-registered",
    { ...clean, [WORKER_FILE]: "package main\nfunc run() {}\n" }, true);
  runOneExitCodeCase("mode 5: hardcoded-recipients",
    { ...clean, [NOTIFY_FILE]: GOOD_NOTIFY.replace("payload.OperatorID", `"someone@mesha.sg"`) }, true);
  runOneExitCodeCase("missing required file", { [KERNEL_FILE]: GOOD_KERNEL }, true);
  console.log("weighing-kernel-phase2 guard: exit-code self-test passed (clean=0, 6/6 violations=non-zero)");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  selfTestExitCodes();
} else {
  run();
}
