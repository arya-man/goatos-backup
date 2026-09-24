-- A vaccination completion that becomes an administration (status 'accepted' with verified_at)
-- moves the second-wave floor for its goat. The persistence guard proves every vaccination write
-- under `FOR SHARE OF goats`, so the acceptance must conflict with that lock on the same goat in
-- the same transaction. Otherwise a completion committed concurrently with a reconcile can leave
-- a second-wave row committed below its new floor.
--
-- A bare `FOR UPDATE` is not enough: the guard runs SERIALIZABLE, and a serializable reader that
-- merely WAITS on a lock keeps its old snapshot and proves against stale history. The acceptance
-- therefore writes a new goat tuple (a value-preserving `row_version = row_version`, which leaves
-- optimistic-concurrency values and the herd-register column trigger untouched). The waiting
-- `FOR SHARE` then fails with 40001 and the guard's retry re-proves against the committed dose.
--
-- Acceptance happens on several paths (verify, batch verify, submission fan-out, direct insert),
-- so the lock is taken here, once, for every one of them.

-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.vaccination_completion_accept_lock_goat_trg()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF NEW.status = 'accepted' AND NEW.verified_at IS NOT NULL AND (
       TG_OP = 'INSERT'
       OR OLD.status IS DISTINCT FROM 'accepted'
       OR OLD.verified_at IS NULL
       OR OLD.goat_id IS DISTINCT FROM NEW.goat_id
       OR OLD.administered_at IS DISTINCT FROM NEW.administered_at
     ) THEN
    UPDATE public.goats g
    SET row_version = g.row_version
    WHERE g.tenant_id = NEW.tenant_id AND g.goat_id = NEW.goat_id
      AND g.merged_into_goat_id IS NULL;
  END IF;
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS vaccination_completion_accept_lock_goat ON public.vaccination_completions;
CREATE TRIGGER vaccination_completion_accept_lock_goat
  BEFORE INSERT OR UPDATE OF status, verified_at, goat_id, administered_at
  ON public.vaccination_completions
  FOR EACH ROW EXECUTE FUNCTION public.vaccination_completion_accept_lock_goat_trg();

-- +goose Down

DROP TRIGGER IF EXISTS vaccination_completion_accept_lock_goat ON public.vaccination_completions;
DROP FUNCTION IF EXISTS public.vaccination_completion_accept_lock_goat_trg();
