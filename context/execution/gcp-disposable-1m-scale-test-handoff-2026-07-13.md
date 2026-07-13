# Disposable GCP 1M Scale Rehearsal Handoff

Date: 2026-07-13 (Asia/Kolkata)

Status: setup and implementation handoff for a non-certifying `goatos-dev` VM
rehearsal. This document does not claim that a GCP run has happened, that the
open projection defects are fixed, or that Goat OS is 1M-certified.

## Outcome

Build a disposable, current-SHA scale runner in Google Cloud that:

1. creates an isolated Compute Engine VM in `goatos-dev`, `asia-south1`;
2. loads production-shaped synthetic data at one-million-animal cardinality;
3. runs correctness, query-plan, API-latency, projector-throughput, retry, and
   resource-pressure gates;
4. uploads a complete evidence bundle before teardown;
5. deletes the VM and every attached disk automatically; and
6. publishes a bounded, exact-SHA report through the existing
   `/scale-audit-e2e-report/` GitHub Pages category and commits its summary to
   `main`.

The runner is rehearsal proof infrastructure. It does not replace the
production code changes required for Calendar history materialization, bounded
incremental projectors, historical `as_of` snapshots, or bounded pagination.
It also does not replace the `goatos-stg-1m-benchmark-v1` Cloud SQL run required
by `docs/protocol-engine/high-scale-kernel-validation-plan.md`. A passing VM
verdict is behavior/performance evidence only, never staging, Cloud SQL, or
full-chain certification.

## Organization and Environment Lock

This test belongs only to Mesha / VGoats.

```text
Google account:       ravi@mesha.sg
Google organization:  vgoats.com
Google project:       goatos-dev
Region:               asia-south1
Repository:           vgoats/goatos
Allowed target only:  goatos-dev
```

Before any resource creation, the controller must print and verify:

```bash
gcloud auth list --filter=status:ACTIVE --format='value(account)'
gcloud config get-value project
gcloud projects get-ancestors goatos-dev
gcloud organizations list --filter='displayName=vgoats.com'
git remote get-url origin
git rev-parse HEAD
git status --short
```

Fail closed unless the account is `ravi@mesha.sg`, the project is
`goatos-dev`, the project belongs to the `vgoats.com` organization, the remote
is `vgoats/goatos`, and the tested tree is clean. Do not run this test against
staging or production databases. Do not import real animals, people, media, or
other PII.

Allow only one scale run at a time for this project. The controller workflow
must use a non-canceling concurrency group so a second dispatch cannot create a
second VM while the first is still running. Configure billing alerts for the
project as a warning layer, but do not treat a budget alert as cleanup or a hard
spend cap.

## Correct Mental Model

There are three separate deliverables:

| Deliverable | Purpose | Can it close a defect alone? |
| --- | --- | --- |
| Static guard/lint | Rejects known code shapes such as N+1 fanout or `COUNT(*) OVER()` before a bounded page | No |
| Production implementation | Materializes/scopes the read model and removes the unsafe runtime path | Yes, after proof |
| Disposable GCP 1M rehearsal | Exercises the current implementation against one-million-row synthetic data and prevents unsupported certification claims | No; it produces rehearsal evidence only |
| `goatos-stg-1m-benchmark-v1` Cloud SQL run | Exercises the committed staging shape, Cloud Run pools, connection reserves, alerts, and full evidence contract | Only after every required threshold and review gate passes |

Required order:

```text
implement -> static guards -> targeted tests -> disposable VM rehearsal
  -> goatos-stg Cloud SQL certification -> report -> review -> main
```

A green rehearsal with an unfixed code path is not closure. A code fix without
the required staging evidence is also not closure. The VM rehearsal may fail
fast before the more expensive staging run, but it can never substitute for it.

## Current Scale-Guard Debt: `47 -> 23` Is Not on Main

At handoff creation, the exact current-`main` command reports:

```text
make scale-guard
scale-guard: RATCHET PASS — NOT SCALE CERTIFIED
(47 time-bounded known offenders across 20 rule/file groups; zero new)
```

The historical `integration/scale-wave` branch has tip `819f91b3` with the
message `47->23 offenders (24 genuinely cleared)`. That commit and the branch's
remediation commits are not ancestors of current `main`. Therefore:

- do not claim `47 -> 23` is already merged;
- do not merge/cherry-pick the stale wave wholesale because `main` has since
  changed Calendar, projections, migrations, SOP, mobile and verification code;
- reconcile each root fix onto current `main`, preserving newer behavior and
  rerunning its current tests and migration gates;
- record the before/after rule, file and count matrix; deleting baseline lines
  without removing the real finding is not a fix; and
- treat 23 as an intermediate burn-down result, not the certification target.

This remediation is Phase 2 work before the VM rehearsal and staging proof. The
VM runner records and validates the resulting guard count but cannot reduce it
by itself. Full certification requires the committed staging Cloud SQL profile,
zero unresolved P0/P1 request-path offenders, and an explicit owner, issue,
expiry and boundedness proof for any remaining lower-risk exception.

## Recommended Disposable Resources

### Projection/read-path run

```text
Machine:       n2-standard-8 (8 vCPU, 32 GiB)
Boot/data disk: 100 GiB pd-balanced, auto-delete
Maximum life:  2 hours
Expected life: 45-75 minutes including setup
Budget:        USD 1.00 per run
```

### Full projector/kernel run

```text
Machine:       n2-standard-16 (16 vCPU, 64 GiB)
Boot/data disk: 150 GiB pd-balanced, auto-delete
Maximum life:  2 hours
Expected life: 45-90 minutes including setup
Budget:        USD 2.00 per run
```

Use an ordinary on-demand VM for a reproducible rehearsal. Spot VMs may be used
for exploratory runs only because preemption makes a failed run ambiguous. Do
not create a temporary Cloud SQL instance for this runner: PostgreSQL inside
the disposable VM is cheaper, faster to provision, and matches the repository's
Docker-based test shape. The separate certifying run uses the committed
`goatos-stg-1m-benchmark-v1` Cloud SQL profile; this VM workflow never closes
that boundary.

The controller must attach these labels:

```text
app=goatos
purpose=scale-rehearsal
environment=dev
ephemeral=true
commit_sha=<full SHA>
run_id=<run ID>
owner=ravi-mesha-sg
```

Do not reserve a static IP, create snapshots, or preserve disks.

## Controller and Cleanup Design

Run creation and deletion from a controller outside the test VM, preferably a
manually dispatched GitHub Actions workflow with Workload Identity Federation.
The controller owns the lifecycle even if the guest crashes.

Required lifecycle:

```text
verify account/org/project/repo/SHA
  -> create disposable VM with two-hour maximum duration
  -> wait for boot readiness
  -> run exact-SHA test bundle
  -> upload artifacts and terminal result to GCS/GitHub Actions
  -> verify artifact checksums and terminal result
  -> delete VM with --delete-disks=all in an unconditional cleanup step
  -> verify VM absent, disks absent, static IP absent
```

The controller must execute cleanup under an `always()`/shell `trap`, not only
after a passing test. Also configure a two-hour maximum run duration with
instance termination action `DELETE`. The hard timeout is the orphan-cost
safety net; it is not the normal cleanup mechanism.

Stopping a VM is insufficient because stopped instances can retain billable
disks and IP resources. The final cleanup assertion must query Compute Engine
and save proof that no resource with the `run_id` label remains.

## Exact-SHA Input Contract

The run accepts:

```text
commit_sha       full 40-character commit on vgoats/goatos
run_id           GCP1M-YYYYMMDD-HHMMSS-<short SHA>
test_profile     projection_reads | full_kernel
dataset_profile  one_million_skew_v1
```

The VM must obtain code through a SHA-pinned artifact or checkout, then record:

```bash
git rev-parse HEAD
git status --porcelain
git show -s --format='%H %cI %s' HEAD
```

Abort unless `HEAD == commit_sha` and the working tree is clean. Never report
results from a branch name alone.

## Dataset Contract

The fixture must be synthetic, deterministic, tenant-scoped, and production
shaped. It must not be a uniform `generate_series` dataset presented as
certification evidence.

Record exact cardinalities for at least:

- animals by tenant, park, shed, stage, sex, lifecycle and health state;
- obligations by state, protocol/rule/dose, due business date and scope;
- completion states: recorded, accepted, rejected and reversed;
- batches, SOP tasks, ownership/workforce mappings and locations;
- outbox events, retries, processed-event identities and DLQ rows;
- Process Integrity projection rows and summaries;
- Calendar open rows, completed-history rows and date-marker rows;
- Vaccination shed, execution and operations projection rows;
- skew: large/small parks, hot/cold sheds, common/rare vaccines, recent/old
  history, and concentrated overdue/deferred work.

The report must distinguish:

```text
animals/facts loaded
projection rows loaded
rows produced by real projectors
rows inserted directly as read-model fixtures
```

Directly seeded projection rows can prove serving-read mechanics, but they
cannot certify projection build throughput, invalidation coverage, or the
end-to-end CQRS chain.

## Test Phases

### Phase 0: preflight

- verify GCP and GitHub organization boundaries;
- verify exact SHA and clean checkout;
- capture VM type, CPU model, RAM, disk type/size and image digest;
- capture Docker, Go, Node and PostgreSQL versions;
- reject missing pass/fail budgets.

### Phase 1: database bootstrap

- start PostgreSQL 16 with explicit, recorded settings;
- apply migrations from the tested SHA to an empty database;
- load the deterministic fixture;
- run `VACUUM (ANALYZE)`/`ANALYZE` before plans or latency measurements;
- record load time, migration time, database size, table sizes and index sizes.

### Phase 2: static and contract gates

Run the current-SHA repository guards, including migration validation, sqlc
plans, API-client drift, scale guard and the targeted `COUNT(*) OVER()` before
bounded-page rule when it lands.

Save a machine-readable scale-guard matrix containing rule, file, line, count,
severity, owner, issue, expiry and disposition. Fail if the current-SHA count
regresses, if a baseline entry is removed without the corresponding code finding
disappearing, or if a P0/P1 request-path finding remains. The historical
`47 -> 23` branch is input for reconciliation only; it is not acceptable proof
for the tested SHA.

Static success is recorded separately and must never be labelled 1M proof.

### Phase 3: real query plans

For every required hot path, save:

```sql
EXPLAIN (ANALYZE, BUFFERS, WAL, SETTINGS, FORMAT JSON)
```

Plans must be captured after realistic data loading and `ANALYZE`, without
`enable_seqscan=off`. A sequential scan over a tiny lookup table is not an
automatic failure. A broad scan, sort, aggregation, spill, or excessive
rows-removed count on a million-cardinality hot table is a failure unless an
approved plan budget explicitly allows it.

At minimum cover:

1. Control Tower;
2. Action Center list;
3. Action Center counts;
4. Protocol Adherence;
5. Calendar open work;
6. Calendar completed history;
7. Calendar date markers;
8. Vaccination execution;
9. Vaccination operations;
10. Vaccination shed summary;
11. SOP active-version lookup and list/detail request plan;
12. historical `as_of` snapshot lookup.

### Phase 4: HTTP latency and response budgets

Start the API built from the tested SHA and run warmup plus measured requests.
Record p50, p90, p95, p99, maximum, error rate, response bytes, concurrency and
sample count for every endpoint.

Hard serving ceilings:

```text
p90 <= 300 ms
p95 <= 500 ms
p99 <= 1000 ms
Action Center counts p90 <= 250 ms
HTTP error rate = 0 for the supported scenario
```

The report must fail when any required hot path is still classified as
`canonical_1300_nonempty_latency_only`. A 1,300-row result may be published as a
diagnostic, but never as certification.

### Phase 5: projector and event-chain scale

Measure initial bootstrap separately from steady-state updates.

Initial bootstrap must record source rows read, projection rows written,
elapsed time, throughput, peak memory, temp-file bytes, WAL bytes and retries.

Steady-state proof must change one bounded business scope and demonstrate:

- only the affected tenant/park/shed/date shard becomes dirty;
- the durable dirty-scope claim uses a bounded lease and retry budget;
- no normal cycle rebuilds or copies the whole tenant;
- unchanged shards retain their serving versions;
- the affected shard atomically publishes a new serving version;
- queue lag, projection age and source watermark return to green;
- a duplicate/out-of-order wake-up is idempotent;
- a worker crash reclaims the lease and completes without duplicate results;
- DLQ/error state is observable;
- full recompute remains an explicit bootstrap/repair path only.

This phase must cover Vaccination shed, execution and operations, Process
Integrity (CT/AC/PA), and Calendar. A shed-only incremental implementation does
not close the full projector defect.

### Phase 6: contract truth

Prove historical `as_of` without manually replacing the live serving snapshot:

1. build the normal current serving state;
2. request a supported past `as_of` through the public API;
3. show that an immutable matching historical snapshot is read;
4. verify the live serving pointer and current reads remain unchanged;
5. verify unsupported historical ranges return the documented contract error,
   not an accidental projection-unavailable `503`.

If immutable history is not implemented, remove/disable the advertised
capability and record this phase as failed. Test scaffolding is not permission
to claim the capability exists.

### Phase 7: resource and cost capture

Sample at least every five seconds:

- VM CPU, load and memory;
- swap usage;
- disk used, IOPS, throughput and latency;
- PostgreSQL connections, locks, temp bytes, buffer hit ratio and WAL growth;
- API RSS/CPU and goroutine count;
- worker concurrency, queue depth, retries and projection lag.

Fail if the VM OOM-kills, uses swap during the measured interval, exceeds 80%
disk utilization, or loses the API/database process. Record estimated cost
from machine uptime immediately; append actual Cloud Billing evidence when it
becomes available.

### Phase 8: evidence upload and teardown

Upload the evidence bundle, verify checksums, write the terminal verdict, and
only then delete the VM and disks. Cleanup runs even if an earlier phase fails.

## Required Evidence Bundle

Every run must produce this bounded structure:

```text
gcp-1m/<run_id>/
  run-metadata.json
  verdict.json
  summary.md
  summary.html
  commands.tsv
  versions.txt
  gcp-resource-before.json
  gcp-resource-cleanup.json
  dataset-cardinality.json
  database-sizes.json
  load-timings.json
  latency/current-sha.json
  latency/request-path-evidence.json
  plans/<hot-path>.json
  projectors/bootstrap.json
  projectors/incremental.json
  projectors/recovery.json
  resources/samples.csv.gz
  logs/test.log.gz
  sha256sums.txt
```

Do not upload secrets, tokens, connection strings, real goat identifiers, real
people, proof media, or PII. Redact command output before packaging.

`run-metadata.json` must contain:

```text
run_id, started_at, finished_at, commit_sha, branch_label,
repository, gcp_project, gcp_region, gcp_zone, machine_type,
cpu_count, memory_gib, disk_type, disk_gib, image_digest,
test_profile, dataset_profile, command_digest, runner_version
```

`verdict.json` must contain one row per required phase/path:

```text
id, status (passed|failed|not_run|not_implemented), threshold,
observed, unit, evidence_path, failure_reason
```

An omitted required row is a failed report. There is no implicit pass.

## Finding-to-Proof Matrix

| Finding | Actual implementation required before proof | Required GCP evidence |
| --- | --- | --- |
| Calendar history/date markers | Projector-owned history and marker tables; indexed request reads | 1M plans and latency for completed history and markers; zero canonical hot-table request reads |
| Whole-tenant scheduled rebuilds | Durable dirty scopes and bounded per-shard workers across all projection families | One-scope mutation, bounded rows touched, queue/retry/recovery metrics, no normal full-tenant rebuild |
| Historical `as_of` | Immutable historical snapshots or removal of advertised support | Past public-API query succeeds from stored snapshot without changing live serving state |
| Execution `COUNT(*) OVER()` | `LIMIT + 1`/`has_more`, or a separately materialized exact total | Plan shows page-bounded work as matching cardinality grows |
| Incomplete CI scale boundary | Current-SHA 1M evidence for every declared hot path and projector throughput | No required path remains 1,300-only; report names every excluded boundary |
| Scale-guard `47 -> 23` wave | Reconcile each stale-branch fix onto current `main`; do not only edit the baseline | Current-SHA guard matrix, zero unresolved P0/P1 request-path offenders, and evidence for every retained bounded exception |

## Report and Main-Branch Publication

Use the existing GitHub Pages category:

```text
https://vgoats.github.io/goatos/scale-audit-e2e-report/
```

Do not create an unrelated root card. Extend the existing scale/performance
report generator and Pages workflow so it consumes the exact-SHA GCP artifact.

The published page must show:

- exact commit SHA and run ID;
- GCP project/region and synthetic dataset profile;
- rehearsal scope (`projection_reads` or `full_kernel`), visibly labeled
  `non_certifying`;
- overall `PASSED`, `FAILED`, `NOT RUN`, or `NOT IMPLEMENTED` status;
- endpoint latency table;
- query-plan verdict table;
- projector bootstrap/incremental/recovery table;
- peak resource usage and estimated/actual cost;
- cleanup proof;
- links to raw immutable artifacts and checksums;
- every known exclusion and unresolved finding.

After an independently reviewed successful rehearsal, commit a bounded result
summary and machine-readable verdict under:

```text
context/execution/scale-runs/<run_id>/summary.md
context/execution/scale-runs/<run_id>/verdict.json
```

Do not commit database dumps, raw unbounded logs, media, secrets, or enormous
plan bundles to Git. Store those as compressed immutable artifacts and link them
by checksum.

The summary and Pages report must state that `PASSED` means the VM rehearsal
passed. It must not use `certified`, `staging passed`, `Cloud SQL passed`, or
`production ready`; only the separate `goatos-stg-1m-benchmark-v1` evidence can
make those claims.

Before pushing the result commit to `main`:

1. fetch/rebase onto current `origin/main`;
2. verify `run-metadata.commit_sha` equals the code SHA that was tested (the
   later report-only commit will naturally have a different SHA);
3. verify all required verdict rows exist;
4. verify raw artifact checksums;
5. verify cleanup proof shows zero remaining run resources;
6. run report-generation tests and relevant repository guardrails;
7. obtain independent scale-focused counter-review;
8. push using the repository's guarded `git mesha-push HEAD:main` workflow;
9. verify the commit exists in `origin/main`;
10. verify both the root Pages card and detail page after deployment.

A failed run must also be documented. Push its bounded summary with `FAILED`
and the remediation link when it provides useful current-SHA evidence; never
delete or relabel a failed result to make the report green.

## Setup Deliverables

The setup implementation is complete only when the repository contains:

- a manually dispatched GCP scale-run workflow/controller;
- Workload Identity Federation with least-privilege permissions;
- a SHA-pinned runner image or bundle;
- deterministic one-million skew fixture generation;
- plan and latency collectors for all required paths;
- projector throughput/recovery collectors;
- resource/cost sampler;
- artifact redaction and checksum packaging;
- unconditional cleanup plus two-hour deletion safety net;
- artifact-to-Pages report ingestion;
- unit/policy tests that reject missing evidence, stale SHAs, 1,300-only hot
  paths, missing cleanup proof, and unsupported certification claims.

## Operator Checklist

```text
[ ] Active account is ravi@mesha.sg
[ ] Project is goatos-dev under vgoats.com
[ ] Tested SHA is full, clean and current
[ ] Pass/fail budgets are recorded before execution
[ ] Test uses only synthetic data
[ ] VM has max two-hour lifetime and auto-deleting disks
[ ] All required phases ran or are explicitly failed/not implemented
[ ] Evidence uploaded and checksums verified
[ ] VM, disks and IP resources are absent after cleanup
[ ] Bounded report summary reviewed independently
[ ] Summary/verdict committed to main
[ ] Existing scale-audit Pages report shows exact SHA and verdict
```

## References

- `docs/runbooks/performance-gates.md`
- `docs/runbooks/google-cloud-environments.md`
- `docs/runbooks/github-workflows.md`
- `docs/protocol-engine/high-scale-kernel-validation-plan.md`
- `docs/decisions/high-scale-dashboard-projections.md`
- `docs/decisions/scale-anti-patterns.md`
- `context/execution/api-projection-performance-handoff-2026-07-13.md`
- `load-tests/AGENTS.md`
- [Compute Engine pricing](https://cloud.google.com/products/compute/pricing/general-purpose)
- [Persistent Disk pricing](https://cloud.google.com/compute/disks-image-pricing)
- [Delete a Compute Engine instance](https://docs.cloud.google.com/compute/docs/instances/deleting-instance)
