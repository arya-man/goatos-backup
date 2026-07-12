# MAIN STABILIZATION STATUS — 2026-07-13 (push freeze in effect)

**PUSH FREEZE ACTIVE.** main's `make guardrails` / `validate-migrations` is RED (see
BLOCKED below). Do NOT push code/migrations to main until it is green again. One
owner stabilizes main first; everyone else holds in worktrees. This note is the
coordination artifact — the only intentional push while frozen.

## GREEN — landed on main
- **Verification vertical** — backend module (`backend/internal/verification/**`,
  migrations `000172_verification_module` + `000173_verification_outbox_idempotency`),
  OpenAPI `/verification/queue` + `/verification/items/{id}/verdict`, permissions/
  routes/bootstrap wiring, durable outbox events (`verification.item.pending`,
  `verification.verdict.approved`, `verification.verdict.rework`). Reviewed SOUND.
- **Verifier mobile app (1b)** — `apps/goatos-android/feature/feature-verify`, queue/
  detail VMs, Room v5, `VERIFICATION_VERDICT` outbox.
- **admin-web approve/reject (1c)** — `/verification` review screen (Rework/Re-assign
  wired to real `/admin/tasks/{id}/rework|assign`).
- **Nav registry** — `moduleNavRegistry` composition (hardcoded operator/leadership
  nav templates removed; nav-composition-guard 0 baselined offenders).
- **Org role model** — 36 composite roles (tier×vertical) + `000178_org_role_catalog`.
- **MOB-010** — 5 Android VMs migrated to `stateIn(WhileSubscribed)`; lifecycle test.
- **Ledger** — reconciled: 0 correctness bugs; C35-002 fixed; C35-009 (scale cert) +
  C35-021 (graph freshness) reclassified as non-bugs; MOB-010 = FIXED.
- **Pipeline unblocks pushed** — `000171_proof_artifacts` NO-TRANSACTION concurrency,
  capacity.go validate-or-reject, TasksRepository offline-first annotations,
  Firebase-auth guard exclusion, e2e smoke-file deletion, duplicate-migration
  renumber (notification → `000179`/`000180`).

## BLOCKED — main is red, needs the OWNER to fix before any further push
- **`validate-migrations` RED** on `000179_vaccination_verification_notification_kernel.sql`
  (Max/FCM lane): it does `ALTER TABLE notification_requests DROP CONSTRAINT ...` on a
  populated hot table to swap the `notification_type` CHECK (add `verification_pending`,
  `rework`). The hot-table-migration guard flags the direct `DROP CONSTRAINT` (and the
  in-txn `VALIDATE CONSTRAINT`). Adding `-- +goose NO TRANSACTION` alone did NOT clear it
  — the `DROP CONSTRAINT` on the hot table needs an explicitly-reviewed no-lock rollout
  (or a guard-sanctioned exemption for this reviewed enum-widening swap). **Owner: the
  Max/FCM notification session.** This is a migration DESIGN fix, not a number/placement
  patch.
- Any residual whole-repo `make guardrails` reds should be re-audited after the above,
  since sub-checks were passing individually.

## OWNERSHIP (to stop cross-lane collisions)
- **Max / FCM notification lane owns:** the notification migrations (`000179`/`000180`),
  FCM push wiring, `firebase-refresh.ts`, and fixing the hot-table `DROP CONSTRAINT`
  above. It must NOT reuse migration numbers already taken (latest is `000180`).
- **Claude (verification+nav+org-roles) owns:** everything under GREEN above (already
  landed) + two clean, gate-passing branches HELD (not pushed) pending main going green:
  - `fix/verifier-app-contract-wire` @ `9525eafa` — 1b android DTOs reconciled to the
    landed OpenAPI contract (compile + guards green).
  - `fix/verification-web-wire-and-qa` — 1c admin-web: swap hand-typed types → generated
    client + a real feature-import fix (`verification/page.tsx` must import via the
    feature public entrypoint) + visual QA. (In progress in a worktree.)
- **Migration-number allocation needs ONE owner** across lanes — the duplicate `000171`/
  `000172` collision (proof/verification vs notification) is what broke main; whoever
  cuts the next migration must take the next free number after `000180`.

## STABILIZATION PLAN (single owner, in order)
1. Fix/rollback the `000179` hot-table `DROP CONSTRAINT` (Max/FCM lane).
2. `make guardrails` + `make validate-migrations` LOCAL green on the exact SHA.
3. ONLY after green: land the two held Claude branches (1b/1c wire) — each re-verified
   diff-clean.
4. Resume normal bug/feature closures once main is stable.

**No "land mine, flag theirs" while main is red. No more one-by-one absorbing of other
lanes. Stabilize main first.**
