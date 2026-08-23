-- +goose Up
-- +goose NO TRANSACTION
-- Rule lineage across versions: name a rule by what it IS, and hash what it SAYS.
--
-- protocol_rules.rule_id is a fresh UUID per version -- publishing rewrites every rule row --
-- so nothing today can answer "is this the same rule the previous version had, saying the same
-- thing?". Without that answer a publish has to assume the whole plan changed, which is why
-- adding a sixth vaccine cancels and re-mints the five nobody touched.
--
--   identity_key        vaccine_code|dose_code|sequence -- WHICH rule this is, stable across versions
--   content_fingerprint sha256 over the fields that decide what is owed and when
--
-- Both are written by the publisher (Go), never derived in SQL: the fingerprint has to match the
-- application's own hashing byte for byte or carry-over would compare two different things.
--
-- Columns are nullable. Rows written before this migration keep NULL and are treated as
-- "content unverified", which falls back to the previous cancel-and-re-mint behaviour. Failing
-- safe here means falling back to what shipped, never to an unverified carry-over.
SET lock_timeout = '3s';

-- seed-migration-guard:ignore owner=ravi issue=vaccination-additive-publish reason=no-seed-impact-nullable-columns-inert-until-the-publisher-populates-them expiry=2026-11-30
ALTER TABLE protocol_rules
  ADD COLUMN IF NOT EXISTS identity_key text,
  ADD COLUMN IF NOT EXISTS content_fingerprint text;

RESET lock_timeout;

-- Carry-over pairs a retired version's rules to the effective version's rules by identity_key,
-- one version at a time, so the lookup is (tenant, version) -> identity. Not unique: a
-- misauthored plan can carry two rows with the same identity key, and publish validation is
-- where that gets rejected, not an index that would fail the migration on existing data.
CREATE INDEX CONCURRENTLY IF NOT EXISTS protocol_rules_identity_lookup_idx
  ON protocol_rules (tenant_id, protocol_version_id, identity_key)
  WHERE identity_key IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS protocol_rules_identity_lookup_idx;

SET lock_timeout = '3s';
-- seed-migration-guard:ignore owner=ravi issue=vaccination-additive-publish reason=no-seed-impact-nullable-columns-inert-until-the-publisher-populates-them expiry=2026-11-30
ALTER TABLE protocol_rules
  DROP COLUMN IF EXISTS content_fingerprint,
  DROP COLUMN IF EXISTS identity_key;
RESET lock_timeout;
