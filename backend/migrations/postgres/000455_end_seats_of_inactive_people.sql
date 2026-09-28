-- +goose Up
-- seed-fixture-guard:ignore: data repair on workforce_positions for people already marked inactive;
-- no seed contract change -- a seed never creates an inactive person holding seats.
--
-- A PERSON MARKED INACTIVE ON PEOPLE / HRMS HOLDS NO SEAT (maintainer instruction 2026-09-28:
-- "people should go from HRMS to vaccination").
--
-- Deactivating somebody revoked their logins but left every workforce_positions seat active, and
-- the Vaccination operators roster, pen ownership and the drive operator pool all read seats. On
-- STG that kept Darshan Talwar -- inactive since 2026-08-24 -- on the Channapatna vaccination roster
-- and as primary manager of five Channapatna pens. The write path now ends the seats in the same
-- transaction as the status change (workforce.SetOperatorStatus); this repairs the rows written
-- before it did.
--
-- Scope: ONLY seats still active for a member whose own status is not 'active'. Idempotent: a
-- re-run finds nothing. No outbox event is written here: the drive operator pool already skips
-- inactive people (wm.status = 'active'), so no drive can be assigned to them, and on STG the one
-- affected person holds no drive assignment. Pens fall to their backup seat, which ownership already
-- resolves.

SET lock_timeout = '5s';

CREATE TABLE public.workforce_positions_000455_ended (
  tenant_id   uuid NOT NULL,
  position_id uuid NOT NULL,
  old_valid_to timestamptz,
  PRIMARY KEY (tenant_id, position_id)
);

INSERT INTO public.workforce_positions_000455_ended (tenant_id, position_id, old_valid_to)
SELECT wp.tenant_id, wp.position_id, wp.valid_to
FROM workforce_positions wp
JOIN workforce_members wm
  ON wm.tenant_id = wp.tenant_id AND wm.workforce_member_id = wp.workforce_member_id
WHERE wp.status = 'active'
  AND wm.status <> 'active';

UPDATE workforce_positions wp
SET status = 'ended',
    valid_to = GREATEST(LEAST(COALESCE(wp.valid_to, now()), now()), wp.valid_from + interval '1 millisecond'),
    updated_at = now(),
    row_version = wp.row_version + 1
FROM public.workforce_positions_000455_ended e
WHERE e.tenant_id = wp.tenant_id
  AND e.position_id = wp.position_id
  AND wp.status = 'active';

-- +goose Down
SET lock_timeout = '5s';

-- Restores only seats this migration ended and that nobody has since reused (the active-seat
-- unique index would otherwise refuse the second holder).
UPDATE workforce_positions wp
SET status = 'active',
    valid_to = e.old_valid_to,
    updated_at = now(),
    row_version = wp.row_version + 1
FROM public.workforce_positions_000455_ended e
WHERE e.tenant_id = wp.tenant_id
  AND e.position_id = wp.position_id
  AND wp.status = 'ended'
  AND NOT EXISTS (
    SELECT 1 FROM workforce_positions o
    WHERE o.tenant_id = wp.tenant_id AND o.scope_type = wp.scope_type AND o.scope_id = wp.scope_id
      AND o.position_code = wp.position_code AND o.status = 'active'
  );

DROP TABLE public.workforce_positions_000455_ended;
