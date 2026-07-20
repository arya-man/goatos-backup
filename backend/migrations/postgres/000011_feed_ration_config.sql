-- +goose Up
-- Feed ration configuration tables (the authored grid behind daily feed quantity).
--
-- WHY A FORWARD MIGRATION (not an edit to the 000001 baseline):
-- clean-slate databases get 000001 verbatim, but dev/stg databases have already applied and
-- checksum-tracked it. Editing historical SQL would leave those databases silently missing the
-- feed ration schema. Same rule that produced 000002.
--
-- ---------------------------------------------------------------------------
-- THE MODEL
-- ---------------------------------------------------------------------------
-- Feed quantity for one shed row is:
--
--   quantity = head_count x grams_per_head x shed_factor        (split across sessions)
--
-- and the ration lookup key is:
--
--   ration_group = (goats.age_band = 'kid') ? 'Kid' : breed_to_group(goats.breed)
--   tag          = goats.management_stage
--   rate         = (park, ration_group, tag, feed_item) -> grams_per_head
--
-- Two properties of that key drive this schema and are easy to get wrong:
--
--   1. FOR KIDS THE BREED IS IGNORED. The source workbook literally stores the string 'Kid' in
--      its breed column, so every kid of every breed shares one ration group. feed_ration_groups
--      therefore maps ADULT breed labels only; the 'Kid' group is selected by age band upstream
--      and never resolved through the breed map.
--
--   2. RATES ARE PARK-SCOPED. CBE and CPT agree on 75 of 77 group/tag rows and genuinely differ
--      on 2 (Osmanabadi/Non-Pregnant and Malai/Non-Pregnant). A tenant-global rate table would
--      quietly feed one of the two parks the wrong ration, so park_id is part of the natural key
--      of feed_ration_rates rather than an optional override dimension.
--
-- The breed -> ration_group map also carries a real merge: Beetal and Sirohi are two live breeds
-- that share ONE ration group, 'Beetal/Sirohi'. That is why the mapping is a table (data) and not
-- an identity function over goats.breed (code).
--
-- ---------------------------------------------------------------------------
-- CONFIGURED ZERO IS NOT MISSING CONFIGURATION  (the safety-critical rule here)
-- ---------------------------------------------------------------------------
-- A rate of 0 is a REAL, AUTHORED business value: K0 and K1 kids are on milk and are correctly
-- fed 0 g of every solid feed item. A rate that was never authored is a DIFFERENT state, and the
-- two must never collapse, because the consequences are opposite:
--
--   rate = 0        -> feed nothing. Correct. Proceed.
--   rate not found  -> we do not know what to feed this group/tag. BLOCK and surface the gap.
--
-- If "no row" were allowed to read as 0, an unconfigured shed would silently be fed nothing and
-- the operator would see a clean, complete-looking feed sheet. That is a starvation path, not a
-- display bug. So the distinction is structural, not conventional:
--
--   * grams_per_head is NOT NULL and has NO DEFAULT. A row therefore always carries an authored
--     number, and 0 is one of the legal authored numbers.
--   * ABSENCE OF A ROW is the only representation of "not configured". There is no sentinel
--     value, no nullable rate, and no default-to-zero anywhere in this schema.
--   * The read path must treat a zero-row lookup as a BLOCKING gap. It must not COALESCE, must
--     not LEFT JOIN a missing rate to 0, and must not fall back to another park's rate.
--
-- The same rule applies to feed_shed_factors.multiplier and feed_experiment_config.absolute_kg.
--
-- ---------------------------------------------------------------------------
-- NORMALIZATION
-- ---------------------------------------------------------------------------
-- Live goats.management_stage is free text off the source sheet and carries real separator and
-- case variants of the SAME tag: 'ICU- kid' (42 rows) vs 'ICU-Kid' (27), 'F2- Male' (8) vs
-- 'F2-Male' (453). Joining a stored tag to a live stage on raw equality would miss those rows --
-- and a missed rate lookup here is the blocking state described above, so 8 animals would stall
-- a shed's feed sheet for a purely cosmetic difference.
--
-- feed_config_norm() is the single definition of the join key. It is the counts module's
-- countAliasNorm / feedGrainNormSQL (backend/internal/counts/adapters/postgres) widened by one
-- character class: countAliasNorm collapses whitespace runs to '_', which unifies
-- 'Milking Warmup' but NOT 'ICU- kid' vs 'ICU-Kid', because the hyphen survives and leaves
-- 'icu-_kid' <> 'icu-kid'. Folding runs of [whitespace _ -] to a single '_' unifies both while
-- keeping every one of the 31 live tags, 7 ration groups, and 10 feed items distinct (verified
-- against the source grid: zero collisions).
--
-- It is applied on BOTH sides by construction:
--   * stored side  -- the *_key columns are GENERATED ALWAYS ... STORED over the label, so the
--                     database, not the caller, computes them. There is exactly one normalizer
--                     and a writer cannot bypass it.
--   * lookup side  -- callers wrap the raw live value: feed_config_norm(g.management_stage).
--     The function is IMMUTABLE, so that predicate is index-eligible.
--
-- feed_config_norm is idempotent (its output is already lowercase and '_'-joined), so applying it
-- to an already-normalized key is a no-op rather than a second transform.

CREATE FUNCTION feed_config_norm(value text) RETURNS text
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    RETURNS NULL ON NULL INPUT
    AS $$
      SELECT lower(regexp_replace(btrim(value), '[\s_-]+', '_', 'g'))
    $$;

COMMENT ON FUNCTION feed_config_norm(text) IS
  'Canonical feed-config join key: trim, casefold, collapse runs of whitespace/underscore/hyphen to a single underscore. Widened twin of counts.countAliasNorm; must be applied to BOTH the stored label and the live lookup value.';


-- ---------------------------------------------------------------------------
-- 1. feed_ration_groups -- breed label -> ration group
-- ---------------------------------------------------------------------------
-- Adult breeds only. Carries the Beetal + Sirohi -> 'Beetal/Sirohi' merge, which is exactly why
-- this is data rather than an identity mapping over goats.breed. Kids bypass this table
-- entirely (see the FOR KIDS THE BREED IS IGNORED note above).
CREATE TABLE feed_ration_groups (
    ration_group_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
    breed_label         text NOT NULL,
    breed_key           text GENERATED ALWAYS AS (feed_config_norm(breed_label)) STORED,
    ration_group_label  text NOT NULL,
    ration_group_key    text GENERATED ALWAYS AS (feed_config_norm(ration_group_label)) STORED,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_ration_groups_breed_label_not_blank CHECK (btrim(breed_label) <> ''),
    CONSTRAINT feed_ration_groups_group_label_not_blank CHECK (btrim(ration_group_label) <> '')
);

CREATE UNIQUE INDEX feed_ration_groups_natural_key_uidx
    ON feed_ration_groups (tenant_id, breed_key);

CREATE INDEX feed_ration_groups_group_idx
    ON feed_ration_groups (tenant_id, ration_group_key);

COMMENT ON TABLE feed_ration_groups IS
  'Maps a live goats.breed label to its ration group. Adult breeds only -- kids resolve to the fixed ''Kid'' group by age band and never consult this table. Carries the Beetal/Sirohi merge.';


-- ---------------------------------------------------------------------------
-- 2. feed_shed_tags -- tenant-scoped tag vocabulary
-- ---------------------------------------------------------------------------
-- The authored vocabulary the ration grid is indexed by, matched against live
-- goats.management_stage. applies_to records whether a tag belongs to the kid course or the
-- adult course; the two sets are disjoint in the source grid (17 kid, 14 adult, 31 total).
CREATE TABLE feed_shed_tags (
    shed_tag_id    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      uuid NOT NULL REFERENCES tenants (tenant_id),
    shed_tag_label text NOT NULL,
    shed_tag_key   text GENERATED ALWAYS AS (feed_config_norm(shed_tag_label)) STORED,
    applies_to     text NOT NULL,
    display_order  integer NOT NULL DEFAULT 0,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_shed_tags_label_not_blank CHECK (btrim(shed_tag_label) <> ''),
    CONSTRAINT feed_shed_tags_applies_to_check CHECK (applies_to = ANY (ARRAY['adult'::text, 'kid'::text])),
    CONSTRAINT feed_shed_tags_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_shed_tags_natural_key_uidx
    ON feed_shed_tags (tenant_id, shed_tag_key);

COMMENT ON TABLE feed_shed_tags IS
  'Authored shed-tag vocabulary the ration grid is indexed by, matched against live goats.management_stage via feed_config_norm. applies_to splits the kid course from the adult course.';


-- ---------------------------------------------------------------------------
-- 3. feed_item_catalog -- the feed items themselves
-- ---------------------------------------------------------------------------
-- Nutritional/handling attributes are NULLABLE on purpose: unlike a ration rate, an unknown
-- energy value does not silently under-feed an animal, it just means an energy rollup cannot be
-- computed for that item. That is a reportable gap, not a feeding decision.
CREATE TABLE feed_item_catalog (
    feed_item_id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    feed_item_label    text NOT NULL,
    feed_item_key      text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    energy_kcal_per_kg numeric(10, 3),
    dry_matter_factor  numeric(6, 4),
    wastage_factor     numeric(6, 4),
    display_order      integer NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'active',
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_item_catalog_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_item_catalog_energy_check CHECK (energy_kcal_per_kg IS NULL OR energy_kcal_per_kg >= 0),
    CONSTRAINT feed_item_catalog_dry_matter_check CHECK (dry_matter_factor IS NULL OR (dry_matter_factor > 0 AND dry_matter_factor <= 1)),
    CONSTRAINT feed_item_catalog_wastage_check CHECK (wastage_factor IS NULL OR (wastage_factor >= 0 AND wastage_factor < 1)),
    CONSTRAINT feed_item_catalog_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_item_catalog_natural_key_uidx
    ON feed_item_catalog (tenant_id, feed_item_key);

COMMENT ON TABLE feed_item_catalog IS
  'Feed items and their nutritional/handling attributes. Attributes are nullable because a missing energy value blocks only a rollup, never a feeding decision -- unlike feed_ration_rates.grams_per_head, which is NOT NULL.';


-- ---------------------------------------------------------------------------
-- 4. feed_ration_rates -- THE EDITABLE GRID
-- ---------------------------------------------------------------------------
-- (tenant, park, ration_group, shed_tag, feed_item) -> grams per head per day.
--
-- Effective-dated: a rate change CLOSES the current row (valid_to = the day the change takes
-- effect) and OPENS a new one, so "what were we feeding Osmanabadi/Pregnant at CBE last March"
-- stays answerable. An in-place UPDATE of grams_per_head would destroy that audit trail and is
-- not the intended edit path.
--
-- grams_per_head is NOT NULL with NO DEFAULT: 0 is authored ("K0 kids are on milk"), and absence
-- of a row is the ONLY encoding of "not configured", which the read path must treat as blocking.
-- See the CONFIGURED ZERO block at the top of this migration.
CREATE TABLE feed_ration_rates (
    ration_rate_id     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id            uuid NOT NULL,
    ration_group_label text NOT NULL,
    ration_group_key   text GENERATED ALWAYS AS (feed_config_norm(ration_group_label)) STORED,
    shed_tag_label     text NOT NULL,
    shed_tag_key       text GENERATED ALWAYS AS (feed_config_norm(shed_tag_label)) STORED,
    feed_item_label    text NOT NULL,
    feed_item_key      text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    grams_per_head     numeric(12, 3) NOT NULL,
    valid_from         date NOT NULL DEFAULT CURRENT_DATE,
    valid_to           date,
    source_system      text NOT NULL DEFAULT 'manual',
    created_by         uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_ration_rates_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_ration_rates_group_label_not_blank CHECK (btrim(ration_group_label) <> ''),
    CONSTRAINT feed_ration_rates_tag_label_not_blank CHECK (btrim(shed_tag_label) <> ''),
    CONSTRAINT feed_ration_rates_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    -- >= 0, NOT > 0: zero is a legitimate authored rate (milk-fed kids).
    CONSTRAINT feed_ration_rates_grams_check CHECK (grams_per_head >= 0),
    CONSTRAINT feed_ration_rates_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

-- Natural key including valid_from: one authored rate per key per effective date.
CREATE UNIQUE INDEX feed_ration_rates_natural_key_uidx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, feed_item_key, valid_from);

-- At most ONE open (current) rate per key. Without this, two open rows would make the lookup
-- ambiguous and a feed sheet non-deterministic.
CREATE UNIQUE INDEX feed_ration_rates_open_row_uidx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, feed_item_key)
    WHERE valid_to IS NULL;

-- Hot read: resolve every feed item's rate for one (park, ration_group, shed_tag) in one indexed
-- scan of the currently-open rows. This is the access pattern the daily feed sheet runs per shed.
CREATE INDEX feed_ration_rates_current_lookup_idx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key)
    INCLUDE (feed_item_key, grams_per_head)
    WHERE valid_to IS NULL;

-- As-of read: the same lookup for a historical business date (audit / back-dated recompute).
CREATE INDEX feed_ration_rates_asof_lookup_idx
    ON feed_ration_rates (tenant_id, park_id, ration_group_key, shed_tag_key, valid_from DESC);

COMMENT ON TABLE feed_ration_rates IS
  'The editable ration grid: (tenant, park, ration_group, shed_tag, feed_item) -> grams per head per day. Park-scoped because CBE and CPT genuinely differ. Effective-dated so a rate change is auditable rather than destructive.';
COMMENT ON COLUMN feed_ration_rates.grams_per_head IS
  'Authored grams per head per day. NOT NULL with no default: 0 means "authored as zero" (milk-fed K0/K1 kids) and a MISSING ROW means "not configured" -- a blocking gap the read path must surface, never COALESCE to 0.';
COMMENT ON COLUMN feed_ration_rates.valid_to IS
  'NULL = currently in force. A rate change closes this row and inserts a new one; it does not UPDATE grams_per_head in place.';


-- ---------------------------------------------------------------------------
-- 5. feed_shed_factors -- per-shed multiplier
-- ---------------------------------------------------------------------------
-- The third term of head_count x grams_per_head x shed_factor. Effective-dated for the same
-- audit reason as the rates. Default 1.0 applies to a row being INSERTED without an explicit
-- multiplier -- it is NOT a fallback for a missing row. A shed with no factor row is treated as
-- 1.0 by the read path, which is safe (it cannot zero out a ration); a factor of 0 must be
-- authored explicitly.
CREATE TABLE feed_shed_factors (
    shed_factor_id  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id         uuid NOT NULL,
    shed_id         uuid NOT NULL,
    feed_item_label text NOT NULL,
    feed_item_key   text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    multiplier      numeric(8, 4) NOT NULL DEFAULT 1.0,
    valid_from      date NOT NULL DEFAULT CURRENT_DATE,
    valid_to        date,
    created_by      uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_shed_factors_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_shed_factors_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_shed_factors_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_shed_factors_multiplier_check CHECK (multiplier >= 0),
    CONSTRAINT feed_shed_factors_window_check CHECK (valid_to IS NULL OR valid_to > valid_from)
);

CREATE UNIQUE INDEX feed_shed_factors_natural_key_uidx
    ON feed_shed_factors (tenant_id, park_id, shed_id, feed_item_key, valid_from);

CREATE UNIQUE INDEX feed_shed_factors_open_row_uidx
    ON feed_shed_factors (tenant_id, park_id, shed_id, feed_item_key)
    WHERE valid_to IS NULL;

CREATE INDEX feed_shed_factors_current_lookup_idx
    ON feed_shed_factors (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, multiplier)
    WHERE valid_to IS NULL;

COMMENT ON TABLE feed_shed_factors IS
  'Per-shed, per-feed-item multiplier -- the shed_factor term of head_count x grams_per_head x shed_factor. Effective-dated. A missing row reads as 1.0 (safe); a 0 multiplier must be authored explicitly.';


-- ---------------------------------------------------------------------------
-- 6. feed_session_templates -- how the daily quantity is split across sessions
-- ---------------------------------------------------------------------------
-- The daily quantity computed above is divided across the park's feeding sessions by
-- split_fraction. The fractions for one park are expected to sum to 1.0; that is a cross-row
-- invariant a table CHECK cannot express, so it is enforced by the writer (the seed command
-- validates it) and must be re-validated by any future session-template editor.
CREATE TABLE feed_session_templates (
    session_template_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id             uuid NOT NULL,
    session_no          integer NOT NULL,
    session_label       text NOT NULL,
    split_fraction      numeric(6, 4) NOT NULL,
    display_order       integer NOT NULL DEFAULT 0,
    status              text NOT NULL DEFAULT 'active',
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_session_templates_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_session_templates_session_no_check CHECK (session_no >= 1),
    CONSTRAINT feed_session_templates_label_not_blank CHECK (btrim(session_label) <> ''),
    CONSTRAINT feed_session_templates_split_check CHECK (split_fraction > 0 AND split_fraction <= 1),
    CONSTRAINT feed_session_templates_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_session_templates_natural_key_uidx
    ON feed_session_templates (tenant_id, park_id, session_no);

CREATE INDEX feed_session_templates_park_order_idx
    ON feed_session_templates (tenant_id, park_id, display_order, session_no);

COMMENT ON TABLE feed_session_templates IS
  'Per-park feeding-session ordering and the fraction of the daily quantity each session carries. split_fraction across a park''s active sessions is expected to sum to 1.0 -- a cross-row invariant enforced by the writer, not by a CHECK.';


-- ---------------------------------------------------------------------------
-- 7. feed_conversions -- inter-feed substitution matrix
-- ---------------------------------------------------------------------------
-- ratio = how many kg of to_item replace 1 kg of from_item. Directional: A->B and B->A are two
-- rows and are NOT required to be reciprocal, because a substitution is a nutritional judgement
-- rather than arithmetic.
CREATE TABLE feed_conversions (
    conversion_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL REFERENCES tenants (tenant_id),
    from_item_label text NOT NULL,
    from_item_key   text GENERATED ALWAYS AS (feed_config_norm(from_item_label)) STORED,
    to_item_label   text NOT NULL,
    to_item_key     text GENERATED ALWAYS AS (feed_config_norm(to_item_label)) STORED,
    ratio           numeric(12, 6) NOT NULL,
    notes           text,
    status          text NOT NULL DEFAULT 'active',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_conversions_from_label_not_blank CHECK (btrim(from_item_label) <> ''),
    CONSTRAINT feed_conversions_to_label_not_blank CHECK (btrim(to_item_label) <> ''),
    -- A substitution must actually substitute: ratio 0 would silently drop the feed.
    CONSTRAINT feed_conversions_ratio_check CHECK (ratio > 0),
    CONSTRAINT feed_conversions_not_self CHECK (feed_config_norm(from_item_label) <> feed_config_norm(to_item_label)),
    CONSTRAINT feed_conversions_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_conversions_natural_key_uidx
    ON feed_conversions (tenant_id, from_item_key, to_item_key);

CREATE INDEX feed_conversions_from_idx
    ON feed_conversions (tenant_id, from_item_key)
    WHERE status = 'active';

COMMENT ON TABLE feed_conversions IS
  'Directional inter-feed substitution matrix: kg of to_item that replace 1 kg of from_item. A->B and B->A are separate rows and need not be reciprocal.';


-- ---------------------------------------------------------------------------
-- 8. feed_experiment_config -- hand-entered experiment sheds
-- ---------------------------------------------------------------------------
-- Experiment sheds are NOT computed from the ration grid. An operator hand-enters ABSOLUTE kg
-- per feed item for the whole shed, so absolute_kg is a shed total, NOT a per-head rate, and
-- head_count is informational only -- it must never be multiplied into absolute_kg. That is the
-- single most important distinction between this table and feed_ration_rates.
--
-- Grain is (tenant, park, shed, feed_item). head_count and experiment_category describe the SHED
-- and are therefore repeated across that shed's item rows; a writer must keep them consistent
-- for a given shed (no single-column CHECK can express a cross-row invariant).
--
-- absolute_kg follows the same zero-vs-missing rule as grams_per_head: NOT NULL, no default,
-- 0 means "authored as zero" and a missing row means "not configured" (blocking).
CREATE TABLE feed_experiment_config (
    experiment_config_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            uuid NOT NULL REFERENCES tenants (tenant_id),
    park_id              uuid NOT NULL,
    shed_id              uuid NOT NULL,
    feed_item_label      text NOT NULL,
    feed_item_key        text GENERATED ALWAYS AS (feed_config_norm(feed_item_label)) STORED,
    absolute_kg          numeric(12, 3) NOT NULL,
    head_count           integer,
    experiment_category  text NOT NULL,
    notes                text,
    status               text NOT NULL DEFAULT 'active',
    created_by           uuid,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT feed_experiment_config_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_experiment_config_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES locations (tenant_id, location_id),
    CONSTRAINT feed_experiment_config_item_label_not_blank CHECK (btrim(feed_item_label) <> ''),
    CONSTRAINT feed_experiment_config_category_not_blank CHECK (btrim(experiment_category) <> ''),
    CONSTRAINT feed_experiment_config_absolute_kg_check CHECK (absolute_kg >= 0),
    CONSTRAINT feed_experiment_config_head_count_check CHECK (head_count IS NULL OR head_count >= 0),
    CONSTRAINT feed_experiment_config_status_check CHECK (status = ANY (ARRAY['active'::text, 'retired'::text]))
);

CREATE UNIQUE INDEX feed_experiment_config_natural_key_uidx
    ON feed_experiment_config (tenant_id, park_id, shed_id, feed_item_key);

CREATE INDEX feed_experiment_config_shed_lookup_idx
    ON feed_experiment_config (tenant_id, park_id, shed_id)
    INCLUDE (feed_item_key, absolute_kg)
    WHERE status = 'active';

COMMENT ON TABLE feed_experiment_config IS
  'Hand-entered experiment sheds. absolute_kg is a SHED TOTAL in kg, never a per-head rate; head_count is informational and must not be multiplied into it.';
COMMENT ON COLUMN feed_experiment_config.absolute_kg IS
  'Absolute kg for the whole shed. NOT NULL with no default: 0 means "authored as zero", a missing row means "not configured" and must block rather than read as 0.';
COMMENT ON COLUMN feed_experiment_config.head_count IS
  'Informational shed head count. NOT a multiplier -- absolute_kg is already the shed total.';


-- +goose Down
-- Reversible: drop the eight tables (and their indexes/constraints, which are owned by the
-- tables) in reverse dependency order, then the normalizer they all depend on. Nothing outside
-- this migration references feed_config_norm, so the DROP is safe without CASCADE.
DROP TABLE IF EXISTS feed_experiment_config;
DROP TABLE IF EXISTS feed_conversions;
DROP TABLE IF EXISTS feed_session_templates;
DROP TABLE IF EXISTS feed_shed_factors;
DROP TABLE IF EXISTS feed_ration_rates;
DROP TABLE IF EXISTS feed_item_catalog;
DROP TABLE IF EXISTS feed_shed_tags;
DROP TABLE IF EXISTS feed_ration_groups;

DROP FUNCTION IF EXISTS feed_config_norm(text);
