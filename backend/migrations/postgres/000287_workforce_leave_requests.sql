-- +goose Up
--
-- seed-fixture-guard:ignore: operational/approval-class HR tables plus one role-catalog row
-- and an additive per-person access repair; no vaccination seed-data contract, no read-model
-- projection, no change to workforce_members. Leave rows are born only from live phone
-- requests, so the seed-migration coupling rule classifies them as operational/audit tables
-- with no seed command to update (docs/runbooks/initial-seed-migration-coupling.md).
--
-- LEAVE REQUESTS (maintainer decisions 2026-09-10, docs/features/leave-requests/plan.md):
--
--   1. The operator raises leave from the phone Clock In / Out screen: days + reason.
--   2. TWO approvers, and BOTH must accept -- the operator's Park Head and a holder of
--      the new `hr` role. Either one rejecting ends the request.
--   3. `hr` is a new role, granted PER PERSON, carrying approvals only.
--   4. Whom a request routes to is CEO-only HRMS config (the flags below).
--   5. Approvers act in the existing Approvals module; the operator sees the request's
--      history beside their past clockings; a pending request can be withdrawn.
--
-- RECONCILIATION WITH THE PRE-EXISTING ROSTER LEAVE. `workforce_absences` (baseline) is the
-- canonical "this person is absent" fact and stays so; `/admin/roster/leave` applies and
-- approves it with ONE approver on roster.manage and then resolves backup coverage. This
-- table is the APPROVAL WORKFLOW in front of that fact: on final approval the service writes
-- one `workforce_absences` row (status 'approved', no replacement) in the same transaction
-- and links it through absence_id, so every existing approved-leave read sees it. Coverage
-- is deliberately NOT resolved here -- the roster rewrite is the maintainer's next step.
--
-- Grain proof (projection-review): one row per REQUEST; the two approver slots are fixed
-- columns rather than a child table because decision 2 fixes them at exactly two, so a
-- request-to-decision join cannot fan out. The phone history read is keyed
-- (tenant_id, workforce_member_id) and the approver queue (tenant_id, status, park_id);
-- both are covered by the indexes below and page by keyset (raised_at, leave_request_id).

CREATE TABLE workforce_leave_requests (
    leave_request_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL,
    workforce_member_id  uuid NOT NULL REFERENCES workforce_members (workforce_member_id),
    -- The requester's park at raise time (workforce_members.primary_location_id). Snapshotted
    -- so a later roster move cannot re-route a request already sitting in a park head's
    -- queue; NULL when the person has no park (the park-head slot is then not required).
    park_id              uuid,
    starts_on            date NOT NULL,
    ends_on              date NOT NULL,
    reason               text NOT NULL,
    status               text NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn')),
    -- Routing snapshot from workforce_leave_approval_config at raise time (decision 4).
    park_head_required   boolean NOT NULL DEFAULT true,
    hr_required          boolean NOT NULL DEFAULT true,
    park_head_decision   text CHECK (park_head_decision IN ('approved', 'rejected')),
    park_head_decided_by uuid,
    park_head_decided_at timestamptz,
    park_head_note       text,
    hr_decision          text CHECK (hr_decision IN ('approved', 'rejected')),
    hr_decided_by        uuid,
    hr_decided_at        timestamptz,
    hr_note              text,
    -- Final outcome instant (approved / rejected / withdrawn).
    decided_at           timestamptz,
    -- The mirror row written into workforce_absences on final approval.
    absence_id           uuid,
    raised_by_user_id    uuid NOT NULL,
    raised_at            timestamptz NOT NULL DEFAULT now(),
    idempotency_key      text NOT NULL,
    request_fingerprint  text NOT NULL,
    row_version          integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workforce_leave_requests_window_check CHECK (ends_on >= starts_on),
    CONSTRAINT workforce_leave_requests_reason_check CHECK (length(btrim(reason)) BETWEEN 1 AND 2000),
    CONSTRAINT workforce_leave_requests_routing_check CHECK (park_head_required OR hr_required),
    CONSTRAINT workforce_leave_requests_approved_absence_check
        CHECK (status <> 'approved' OR absence_id IS NOT NULL),
    CONSTRAINT workforce_leave_requests_final_decided_check
        CHECK ((status = 'pending') = (decided_at IS NULL))
);

CREATE UNIQUE INDEX workforce_leave_requests_idem_uq
    ON workforce_leave_requests (tenant_id, idempotency_key);

-- The requester's own history: newest first.
CREATE INDEX workforce_leave_requests_member_idx
    ON workforce_leave_requests (tenant_id, workforce_member_id, starts_on DESC, leave_request_id DESC);

-- The approver queues: a park head reads their park's open requests, HR reads every open
-- request. Partial on pending so decided history never widens the queue scan.
CREATE INDEX workforce_leave_requests_pending_park_idx
    ON workforce_leave_requests (tenant_id, park_id, raised_at DESC, leave_request_id DESC)
    WHERE status = 'pending';
CREATE INDEX workforce_leave_requests_pending_idx
    ON workforce_leave_requests (tenant_id, raised_at DESC, leave_request_id DESC)
    WHERE status = 'pending';

-- The People / HRMS list: every request, newest first, optionally by status.
CREATE INDEX workforce_leave_requests_tenant_idx
    ON workforce_leave_requests (tenant_id, status, raised_at DESC, leave_request_id DESC);

-- Overlap guard support: open (pending/approved) windows per person.
CREATE INDEX workforce_leave_requests_open_window_idx
    ON workforce_leave_requests (tenant_id, workforce_member_id, starts_on, ends_on)
    WHERE status IN ('pending', 'approved');

COMMENT ON TABLE workforce_leave_requests IS
    'Operator leave requests raised from the phone Clock screen; park head + HR both approve (2026-09-10). On approval mirrored into workforce_absences via absence_id.';

-- Routing flags (decision 4): one row per tenant, CEO-only write. An ABSENT row means both
-- approvers are required, so a tenant that never touched the screen runs the maintainer's
-- stated default.
CREATE TABLE workforce_leave_approval_config (
    tenant_id          uuid PRIMARY KEY,
    park_head_required boolean NOT NULL DEFAULT true,
    hr_required        boolean NOT NULL DEFAULT true,
    updated_by         uuid,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    row_version        integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    CONSTRAINT workforce_leave_approval_config_routing_check CHECK (park_head_required OR hr_required)
);

-- The `hr` role (decision 3). A PER-PERSON authority in the counts_approver shape: HR is
-- whoever the maintainer names, never a job the roster derives. It is a director-tier row so
-- the tenant-scoped grant the seeder writes is legal, and is_legacy=true because it belongs to
-- no org vertical -- exactly the row shape 000108 used for counts_approver. It carries leave
-- approval as its first authority; more approval kinds will be added to the role later.
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=per-person-role-row-granted-by-name-via-seed-stg-login-grants-perPersonGrants-no-fixture-seed-writes-it expiry=2026-12-31
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES ('hr', 'director', NULL, true, 'HR', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    label = EXCLUDED.label;

-- HR is also a DESIGNATION (the notification audience config on /people ticks alert audiences
-- per designation, and the leave-raised push defaults to park_head + hr). Same shape as
-- 000247's breeding_director row.
INSERT INTO public.designation_catalog (designation_code, label, grade, sort_order)
VALUES ('hr', 'HR', 'director', 65)
ON CONFLICT (designation_code) DO NOTHING;

-- Per-person access repair (the 000245 shape). Since the 2026-08-24 cutover a person's stored
-- person_module_access rows DECIDE their permissions and menus; the role map in
-- capability_backfill.go is read only by the one-time backfill. Every already-migrated park
-- head and CEO therefore holds no `leave_approvals` row and would 403 on the new approver
-- routes. Write the tick for the roles the mapping gives it to. ADDITIVE ONLY, and only for
-- people the cutover already migrated (someone with no person_access row is still on the role
-- fallback path and must not be moved off it by a single row).
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, s.surface, 'leave_approvals', s.capabilities
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
-- Mirrors permissions.capability_backfill.go: a park head signs from the PHONE only (park
-- heads hold no admin-web bootstrap); HR reads the list and decides on both surfaces; the
-- CEO also configures. Multiple rows per role are folded by the DISTINCT + ON CONFLICT.
JOIN (VALUES
  ('park_head',    'mobile', ARRAY['oversee']::text[]),
  ('hr',           'web',    ARRAY['view', 'oversee']::text[]),
  ('hr',           'mobile', ARRAY['view', 'oversee']::text[]),
  ('ceo_internal', 'web',    ARRAY['view', 'oversee', 'configure']::text[]),
  ('ceo_internal', 'mobile', ARRAY['view', 'oversee', 'configure']::text[])
) AS s(role, surface, capabilities) ON s.role = g.role
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- +goose Down
DELETE FROM public.person_module_access WHERE module_key = 'leave_approvals';
DELETE FROM public.designation_catalog WHERE designation_code = 'hr';
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=per-person-role-row-granted-by-name-via-seed-stg-login-grants-perPersonGrants-no-fixture-seed-writes-it expiry=2026-12-31
DELETE FROM public.user_scope_grants WHERE role = 'hr';
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=per-person-role-row-granted-by-name-via-seed-stg-login-grants-perPersonGrants-no-fixture-seed-writes-it expiry=2026-12-31
DELETE FROM public.auth_pending_email_grants WHERE role = 'hr';
-- seed-migration-guard:ignore owner=manohark issue=leave-requests reason=per-person-role-row-granted-by-name-via-seed-stg-login-grants-perPersonGrants-no-fixture-seed-writes-it expiry=2026-12-31
DELETE FROM public.org_role_catalog WHERE role_key = 'hr';
DROP TABLE IF EXISTS workforce_leave_approval_config;
DROP TABLE IF EXISTS workforce_leave_requests;
