-- +goose Up
-- seed-fixture-guard:ignore: additive verify-duty rows for a module on already-seeded databases; cmd/seed-position-duties already emits the same row on a fresh seed (it derives the verifier's modules from verificationcatalog.All()).
--
-- THE VERIFIER MUST SEE ROUTINE CHECKS (found in the 2026-09-17 phone + browser proof run).
-- A verifier's queue and her per-module Verify tab are composed from her position's
-- position_module_duties 'verify' rows. The Routines module registers verification category
-- pen_routine under navigation module pen_routines, but existing databases only gain a duty
-- row when someone re-runs cmd/seed-position-duties -- so on the live data (video_verifier:
-- aas_health, counts, feed.direction, milk, pc.vaccination, pc_care, weighing) every routine
-- check submitted for review would sit in a queue no verifier can open, and the pending push
-- would resolve zero recipients. This gives every position that already verifies at least one
-- module the pen_routines verify duty too. ADDITIVE ONLY: a position that already holds it, or
-- one an admin deactivated, is left as it is.
CREATE TABLE IF NOT EXISTS public.position_module_duties_pen_routines_backfill (
  id uuid PRIMARY KEY
);

WITH inserted AS (
  INSERT INTO public.position_module_duties (tenant_id, position_code, module_code, duty_type, status)
  SELECT DISTINCT d.tenant_id, d.position_code, 'pen_routines', 'verify', 'active'
  FROM public.position_module_duties d
  WHERE d.duty_type = 'verify'
    AND d.status = 'active'
    AND d.module_code <> 'pen_routines'
    AND NOT EXISTS (
      SELECT 1 FROM public.position_module_duties x
      WHERE x.tenant_id = d.tenant_id
        AND x.position_code = d.position_code
        AND x.module_code = 'pen_routines'
        AND x.duty_type = 'verify'
    )
  RETURNING id
)
INSERT INTO public.position_module_duties_pen_routines_backfill (id)
SELECT id FROM inserted
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.position_module_duties p
USING public.position_module_duties_pen_routines_backfill b
WHERE p.id = b.id;
DROP TABLE IF EXISTS public.position_module_duties_pen_routines_backfill;
