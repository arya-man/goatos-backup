-- +goose Up
-- Maintainer decision 2026-07-31: the note an operator writes when raising a shifting
-- request must reach the verifier reviewing that movement's evidence, not only the park
-- head approving it.
--
-- This is a GENERIC producer field, not a shifting column. verification_items is the shared
-- queue behind every vertical's evidence review, and "free text the raiser wrote that the
-- reviewer should read" is not specific to shifting -- birth, death, feed, and health
-- producers can populate it the same way. It is deliberately NOT overloaded onto
-- subject_label: that field is system-composed identity ("Shed move · 12 animals") and
-- concatenating operator prose into it would make the two indistinguishable to every
-- renderer and to anyone reading the queue later.
--
-- Nullable, no default, no backfill: absent means the producer supplied no note, which is
-- the truth for every item enqueued before this migration. ADD COLUMN with no default is
-- metadata-only in Postgres 11+, so no table rewrite.
ALTER TABLE public.verification_items
    ADD COLUMN IF NOT EXISTS subject_note text;

COMMENT ON COLUMN public.verification_items.subject_note IS
    'Optional free-text note from whoever raised the underlying work, shown to the verifier during evidence review. Distinct from subject_label, which is system-composed identity. NULL means the producer supplied no note.';

-- +goose Down
ALTER TABLE public.verification_items
    DROP COLUMN IF EXISTS subject_note;
