-- +goose Up
-- seed-fixture-guard:ignore: forward data cutover for the vaccine-stock director gate; no
-- schema change and no seed-contract table is reshaped.
--
-- Vaccine-stock director gate (maintainer decision 2026-09-02). The inventory_vaccine fridge
-- check is now RECORDED BY PARK OPERATORS and APPROVED BY THE PC DIRECTOR on the module's own
-- stock-verdict route — it is no longer a Verification category, so the tenant verifier's
-- queue must not keep holding stock items nobody will ever judge there.
--
-- 1. Withdraw every still-pending inventory_vaccine verifier item. The withdrawn status is the
--    existing "this item no longer awaits this queue" retire path (the feed reopen shape); a
--    verdict already CAST stays untouched as history. The task rows themselves stay
--    pending_verification and simply wait for the PC Director's verdict instead.
--    verification_items is a hot table: bound the lock wait so a busy verifier queue makes
--    this cutover retry rather than queue behind row locks indefinitely.
SET lock_timeout = '5s';
UPDATE public.verification_items
SET status = 'withdrawn',
    row_version = row_version + 1,
    updated_at = now()
WHERE module = 'pc_care'
  AND category = 'inventory_vaccine'
  AND status = 'pending';

-- 2. Strip pc_director assignees off UNFINISHED per-vaccine stock tasks. The director judges
--    the videos and must not be offered a camera; an already-submitted or completed task keeps
--    its assignee history untouched. The kernel reconciler repeats this on every pass and
--    inserts the park's own vaccination operators as the new assignees.
DELETE FROM public.pc_care_task_assignees a
USING public.pc_care_tasks t, public.workforce_members dm
WHERE t.tenant_id = a.tenant_id
  AND t.task_id = a.task_id
  AND t.category = 'inventory_vaccine'
  AND t.vaccine_label IS NOT NULL
  AND t.status IN ('open', 'rework')
  AND t.work_state IN ('scheduled', 'delayed')
  AND dm.tenant_id = a.tenant_id
  AND dm.user_id = a.operator_user_id
  AND dm.primary_role_hint = 'pc_director';

-- +goose Down
-- Intentionally no-op: the withdrawn verifier items and removed director assignees are a
-- one-way product cutover; re-arming the verifier queue for stock work would contradict the
-- recorded decision, and the reconciler would immediately re-run the assignee cutover anyway.
