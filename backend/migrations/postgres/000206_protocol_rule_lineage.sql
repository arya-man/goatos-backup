-- +goose Up
-- +goose NO TRANSACTION
-- Rule lineage across versions: name a rule by what it IS, and hash what it SAYS.
--
-- protocol_rules.rule_id is a fresh UUID per version -- publishing rewrites every rule row -- so
-- nothing today can answer "is this the same rule the previous version had, saying the same
-- thing?". Without that answer a publish has to assume the whole plan changed, which is why
-- adding a sixth vaccine cancelled and re-minted the five nobody touched.
--
--   identity_key        vaccine_code|dose_code|sequence -- WHICH rule this is, stable across versions
--   content_fingerprint sha256 over the fields that decide what is owed and when
--
-- Both are written by the publisher (Go), never derived in SQL: the fingerprint has to match the
-- application's own hashing byte for byte or carry-over would compare two different things.
--
-- This lives in its own table rather than as two columns on protocol_rules, and that is not
-- incidental. protocol_rules holds SOURCED configuration -- what the plan says, traceable to the
-- spreadsheets the seed pipeline validates. Lineage is derived: the server computes it from the
-- rule it just wrote, no fixture supplies it, and no validator could check it against a sheet.
-- Keeping derived metadata beside sourced configuration would have coupled every future change
-- here to the seed fixture contract for no reason a reader could defend.
--
-- Rows are written only by new publishes. A rule with no lineage row is treated as "content
-- unverified" and never carries over, which falls back to the cancel-and-re-mint behaviour that
-- shipped. Failing safe here means falling back to what shipped, never to an unverified carry-over.

-- seed-fixture-guard:ignore: owner=ravi issue=vaccination-additive-publish reason=brand-new-derived-metadata-table-written-only-by-the-publisher-with-no-source-mapping-fixture-row-or-validator-to-keep-in-step expiry=2026-11-30
-- seed-migration-guard:ignore owner=ravi issue=vaccination-additive-publish reason=brand-new-table-inert-until-the-publisher-populates-it expiry=2026-11-30
CREATE TABLE IF NOT EXISTS public.protocol_rule_lineage (
    tenant_id uuid NOT NULL,
    protocol_version_id uuid NOT NULL,
    rule_id uuid NOT NULL,
    identity_key text NOT NULL,
    content_fingerprint text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (tenant_id, rule_id)
);

-- Carry-over pairs a retired version's rules to the effective version's rules by identity, one
-- version at a time, so the lookup is (tenant, version) -> identity. Not unique: a misauthored
-- plan can carry two rules with the same identity, and publish validation is where that gets
-- rejected, not an index that would fail the migration on existing data.
CREATE INDEX CONCURRENTLY IF NOT EXISTS protocol_rule_lineage_identity_idx
  ON public.protocol_rule_lineage (tenant_id, protocol_version_id, identity_key);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS protocol_rule_lineage_identity_idx;
-- seed-fixture-guard:ignore: owner=ravi issue=vaccination-additive-publish reason=brand-new-derived-metadata-table-written-only-by-the-publisher-with-no-source-mapping-fixture-row-or-validator-to-keep-in-step expiry=2026-11-30
-- seed-migration-guard:ignore owner=ravi issue=vaccination-additive-publish reason=brand-new-table-inert-until-the-publisher-populates-it expiry=2026-11-30
DROP TABLE IF EXISTS public.protocol_rule_lineage;
