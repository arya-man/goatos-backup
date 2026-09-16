# Vaccination policy precedence — source trace

2026-09-16. Read-only trace against `origin/main` `e06d27bf600b4d5d7c46d178d9341a5f6c942a6a`, joined with the actual staging values in feature-config-value-readback.txt. No clinical changes or DB writes. These findings resolve configuration ownership and selection logic; they are not evidence that every existing staging task uses the latest version or installed Android build.

## Shot cap: the authored capacity override wins

The previously noted plan2 versus table3 is an explicit override contract, not an unexplained discrepancy:

1. `backend/internal/obligation/app/drive_planner_config.go:95–96` reads a positive `rule_dsl.drive_policy.max_shots_per_animal_per_drive` into the planner. Publishedv9 supplies2.
2. `backend/internal/kernelstages/obligation_sweeper.go:182–194` constructs the real vaccinationexecution capacity repository. At`:234–246` it reads the tenant capacity once per sweep and obtains its non-null max-shots override.
3. `kernelstages/obligation_sweeper.go:461–463` applies `ApplyCapacityShotCapOverride` **after** the DSL resolver. `obligation/app/drive_planner_config.go:101–113` explicitly sets the planner cap to a positive authored capacity value; nil/<=0 preserves the DSL/default.
4. Therefore, with the observed real table value3, the current source’s ordinary kernel sweep uses **3 shots per animal per drive**, overriding the publishedv9 DSL’s2. Capacity read failure records a batch-sweep error rather than silently applying an invented override.
5. Cross-protocol combo alignment uses the strictest participating planner/default bound (`drive_planner_config.go:150–166`); default is3 (`obligation/domain/types.go:371`). This does not reduce the observed effective3 merely because an overridden raw DSL value was2.

A separate concept remains: maximum distinct vaccine products in a shed-visit combo is hardcoded3 in `obligation/app/combo_session.go:8–18`, with chunking/validation at`:41–64`. Do not collapse this distinct-product combo bound and the per-animal cap into an undifferentiated field just because both currently equal3. Compatibility rules also apply separately; cap3 is not authorization to combine arbitrary products.

Source tests `obligation/app/capacity_shot_cap_override_test.go:11–40` explicitly cover nil-preserves, authored-override-wins and invalid-override-ignored. This investigation read those tests and implementation; it did not run Go tests or trigger a live sweep.

## Proof mode: execution follows the task’s pinned SOP version

Published protocol planv9 has `shed_level_video`; published vaccination.drive SOPv1 has `per_goat_video`. The execution paths traced select **the task’s SOP**, not the raw protocol proof_policy:

- Sweep config selects the protocol version’s SOPVersionID, falling back to configured SOP only when absent (`kernelstages/obligation_sweeper.go:451–454`). Per-rule config starts with that SOPVersionID (`obligation/app/sweeper.go:2324–2333`) and task creation carries the resolved reference (`:2427`). This preserves the possibility of rule-specific SOP references; do not infer all tasks use one current published version.
- Backend vaccination completion summary joins `sop_tasks.sop_version_id` to `sop_versions` and reads **sv.proof_policy**, not protocol_versions.proof_policy (`vaccination/adapters/postgres/repository.go:1882–1896`). Explicit proof_mode wins; absent mode derives shed_level_video from subject_scope=shed, otherwise per_goat_video. Min/max evidence counts are likewise read from that SOP policy with fallbacks.
- Android task detail uses `sopVersion?.toProofPolicy()` (`apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/TasksRepository.kt:160–167`). The parser reads explicit proof_mode first and derives shed mode from subject_scope only if absent (`forms/ProofPolicy.kt:82–103`). Capture persists this resolved mode (`capture/CaptureRepository.kt:919`); it does not independently choose the protocol plan’s mode.
- Protocol publication does retain row-level proof policy, inheriting version-level policy if omitted (`backend/internal/protocol/app/publish.go:1924–1926`,`:1950`). That persistence path alone does not override the task’s SOP execution contract.

Thus **a task pinned to the observed vaccination.drive SOPv1 resolves per-goat video** along the traced backend/Android task path, even though the current protocol plan document displays shed-level video. Existing tasks pinned to another SOP version may resolve differently. The remaining specific evidence needed to name the effective mode for any real task is its `sop_tasks.sop_version_id` and the served task-detail `sopVersion.proof_policy`, not another broad configuration audit. No individual task or installed phone response was queried in this trace, so “all current staging tasks require per-goat video” would overclaim.

## Product implication

Display distinct owner/version and effective source: capacity override for per-animal planning; task-pinned SOP for execution proof; protocol DSL for clinical scheduling/eligibility and its persisted proof metadata. A new shared authoring surface must either update the authoritative setting/reference through its existing publish contract or clearly show that it is editing a different layer. The prototype should not advertise the plan’s proof setting as the effective phone mode without task-reference resolution.

This source trace supersedes the earlier audits’ broad unresolved-precedence wording. Remaining limits are deployment/version/task selection and runtime observation, rather than unknown code precedence.
