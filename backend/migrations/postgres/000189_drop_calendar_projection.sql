-- +goose Up
/*
U7 completion (operational-kernel-5k-50k-scale-envelope ADR): retire the Calendar screen's
projection read-model entirely. Calendar now serves canonical indexed SQL directly
(backend/internal/calendar/adapters/postgres/canonical_read.go's calendarCanonicalEventsCTE /
calendarCanonicalListSQL / calendarCanonicalDetailSQL), and the reminder/escalation/nudge/snooze
sweeps (repository.go, reminder_cadence.go) now read the same canonical reconstruction instead of
calendar_event_projections. Migration 000187 deliberately kept calendar_event_projections as a
"surviving projection... disproportionately hard [to reconstruct canonically]" -- this migration is
the unit that actually does that reconstruction and retires it.

Dropped:
  - calendar_event_projections          (the upcoming/live projection row store)
  - calendar_projection_state            (its freshness/coverage watermark)
  - calendar_history_projection_rows     (the separate completed-history projection)
  - calendar_history_date_markers        (its month-grid marker rollup)
  - calendar_history_projection_state    (its freshness/coverage watermark)

Also corrects an incomplete prior step: migration 000186 (U5, "repoint the two foreign keys that
currently reference calendar_event_projections") dropped constraints named
notification_requests_event_fk / calendar_snoozes_event_fk -- but migration 000106 had already
renamed those to notification_requests_event_identity_fk / calendar_snoozes_event_identity_fk (and
repointed them at a THIRD table, calendar_event_identities, populated only by an
INSERT/UPDATE-of-identity-columns trigger on calendar_event_projections). Migration 000186's DROP
CONSTRAINT IF EXISTS therefore silently no-opped against the old, already-renamed name, and the real
identity FKs stayed live and enforced. This migration finishes that repoint properly: it drops the
REAL, currently-active FK constraints, then drops calendar_event_identities itself (dropping
calendar_event_projections already drops its identity trigger for free; the trigger FUNCTION is
dropped explicitly since a function is not owned by any one table). Once these constraints are gone,
notification_requests.calendar_event_id / calendar_snoozes.calendar_event_id are plain,
application-validated text columns (every writer already re-derives/re-selects its target from the
canonical reconstruction before inserting -- see SendNudge/Snooze/queueDueReminder/queueEscalation),
exactly as the ADR's recovery-section fallback language anticipated ("if a calendar_event_id does not
cleanly map 1:1 to a canonical id ... drop the projection FK and enforce the relationship in app code
instead").

Lock safety: every statement here is catalog-only (DROP CONSTRAINT / DROP TRIGGER-via-DROP TABLE /
DROP FUNCTION / DROP TABLE) -- a brief ACCESS EXCLUSIVE lock with no table rewrite or scan, on tables
that are either being dropped in the same migration or losing only a constraint definition.

No seed-path impact: this migration does not add, rename, or change the shape of any column a seed
command writes -- it only removes derived/projection tables and a referential-integrity constraint
whose target was itself a derived identity-shadow table, not seed source-of-truth.
Seed-migration-guard:ignore markers are placed on each DROP CONSTRAINT below.

Also drops calendar_prune_closed_vaccination_projection(uuid, timestamptz, int) (migration 000106): a
standalone plpgsql function operating directly on calendar_event_projections that was ALREADY
orphaned before this migration -- the Go repository's PruneClosedVaccinationProjection method (now
also removed) never called it, instead inlining equivalent SQL directly
(calendarPruneClosedVaccinationProjectionSQL). Nothing in the codebase invokes this function; it is
dropped here rather than left behind referencing a table that no longer exists.

All current rows in every dropped table are test data (docs/decisions/
operational-kernel-5k-50k-scale-envelope.md: "All data in the current environment is test data.").
Recoverable via git tag kernel-split-workers-v1 (structure only; rows are not restored).
*/

ALTER TABLE notification_requests DROP CONSTRAINT IF EXISTS notification_requests_event_identity_fk; -- seed-migration-guard:ignore owner=ravi issue=calendar-canonical-5k50k reason=fk-drop-no-seed-shape-change expiry=2027-01-01

ALTER TABLE calendar_snoozes DROP CONSTRAINT IF EXISTS calendar_snoozes_event_identity_fk; -- seed-migration-guard:ignore owner=ravi issue=calendar-canonical-5k50k reason=fk-drop-no-seed-shape-change expiry=2027-01-01

DROP FUNCTION IF EXISTS calendar_prune_closed_vaccination_projection(uuid, timestamptz, int);

DROP TABLE IF EXISTS calendar_event_projections CASCADE;

DROP FUNCTION IF EXISTS calendar_event_projection_identity_trg();

DROP TABLE IF EXISTS calendar_event_identities CASCADE;

DROP TABLE IF EXISTS calendar_projection_state CASCADE;

DROP TABLE IF EXISTS calendar_history_projection_rows CASCADE;

DROP TABLE IF EXISTS calendar_history_date_markers CASCADE;

DROP TABLE IF EXISTS calendar_history_projection_state CASCADE;

-- +goose Down
-- Irreversible in place: these five retired read-models (plus the calendar_event_identities
-- identity shadow table and its trigger/function) are derived from canonical data by projector code
-- that is also removed. Restore the full pre-cutover topology from git tag kernel-split-workers-v1 if
-- a rollback is required -- including re-adding notification_requests_event_identity_fk /
-- calendar_snoozes_event_identity_fk against a restored calendar_event_identities, per the ADR's
-- recovery section. No-op here, matching the precedent set by migrations 000187/000188.
SELECT 1;
