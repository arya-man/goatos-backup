-- +goose Up
-- +goose NO TRANSACTION

-- SUBSTRING SEARCH on animal identifiers, indexable.
--
-- Maintainer decision 2026-08-20: the sale picker's "Find a tag" box must match ANYWHERE
-- in the identifier, not just the start. An operator reading a tag off an animal is
-- routinely working from the middle three digits or the last four -- that is how the
-- number is legible on a real ear tag -- so a prefix-only search finds nothing and reads
-- as a broken box.
--
-- WHY AN INDEX AND NOT JUST A LEADING-WILDCARD LIKE. `identifier_value ILIKE '%123%'` is
-- the non-SARGable predicate AGENTS.md bans outright: it cannot use the ordinary btree on
-- the column and degrades to a scan of every identifier the scope admits. The recorded
-- alternative is a pg_trgm GIN index, which DOES serve a leading wildcard, and that is
-- what this adds. Same shape as procurement_vendors_search_trgm_idx (000156).
--
-- CONCURRENTLY + NO TRANSACTION because goat_identifiers is a populated hot table -- one
-- row per identifier per animal -- and a plain CREATE INDEX would hold a write lock
-- against the herd's identity table for the duration.
--
-- gin_trgm_ops needs a text operator class; identifier_value is already text, so no
-- expression wrapper is required. The search itself lowercases both sides, and trigram
-- matching is case-insensitive for ILIKE, so no separate normalized column is needed.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identifiers_value_trgm_idx
    ON public.goat_identifiers USING gin (identifier_value public.gin_trgm_ops);

-- The picker also searches the animal's display id, on the same substring rule.
CREATE INDEX CONCURRENTLY IF NOT EXISTS goats_display_id_trgm_idx
    ON public.goats USING gin (display_id public.gin_trgm_ops);

-- +goose Down
-- +goose NO TRANSACTION

DROP INDEX CONCURRENTLY IF EXISTS public.goats_display_id_trgm_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.goat_identifiers_value_trgm_idx;

-- pg_trgm is deliberately NOT dropped: it is database-wide and 000156 depends on it.
