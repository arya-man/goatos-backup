-- +goose Up
-- +goose StatementBegin
-- Org role catalog: tier x vertical x park.
--
-- See context/architecture/org-role-model.md (the derived target model) and
-- context/architecture/staff-org-data.md (the source staff/org data). The real
-- org is a 3-axis matrix: TIER (CEO/CxO -> Director -> Head -> Manager ->
-- Assistant Manager) x VERTICAL (9 business departments) x PARK. A role is
-- (tier, vertical) encoded as a single grantable "role_key" string
-- (e.g. "manager_feed", "director_health"); park stays the existing
-- user_scope_grants scope_type = 'park' dimension -- unchanged, still a hard
-- scope filter (see internal/calendar/adapters/http/handler.go calendarScope
-- for the established park/shed scope-filtering pattern; the permissions
-- package's new ScopeIDsForPermission generalizes it for future modules).
--
-- This table is the config-driven catalog of every grantable role. It
-- replaces the hardcoded CHECK (role IN (...)) enums on user_scope_grants and
-- auth_pending_email_grants (last widened in
-- 000089_pc_director_escalation_role.sql) with a normal FK: adding a new tier
-- or vertical becomes a data change (INSERT into org_tiers/org_verticals/
-- org_role_catalog), not a migration that touches every CHECK constraint that
-- mentions a role.
--
-- Capability (which permissions a role has) is driven by TIER alone, in
-- backend/internal/permissions/permissions_orgrole.go's tierPermissions map
-- -- this table is the Postgres-side mirror of that same tier catalog so the
-- role model is documented and queryable from the DB, not only compiled into
-- the Go binary. backend/internal/permissions/permissions_orgrole_test.go
-- TestOrgTierCatalogParityWithSeed asserts the two stay in sync.
--
-- Small control-plane tables (org_tiers/org_verticals/org_role_catalog,
-- user_scope_grants, auth_pending_email_grants) -- not goat/event/obligation
-- scale, so a same-transaction FK add is safe (db-migration-safety's
-- lock-safety concurrency rules apply to large hot tables, not these).
CREATE TABLE org_tiers (
  tier_code text PRIMARY KEY,
  label text NOT NULL,
  rank smallint NOT NULL,
  CONSTRAINT org_tiers_rank_unique UNIQUE (rank)
);

CREATE TABLE org_verticals (
  vertical_code text PRIMARY KEY,
  label text NOT NULL,
  sort_order smallint NOT NULL,
  CONSTRAINT org_verticals_sort_order_unique UNIQUE (sort_order)
);

CREATE TABLE org_role_catalog (
  role_key text PRIMARY KEY,
  tier_code text NOT NULL REFERENCES org_tiers (tier_code),
  vertical_code text NULL REFERENCES org_verticals (vertical_code),
  is_legacy boolean NOT NULL DEFAULT false,
  label text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT org_role_catalog_vertical_or_legacy_check
    CHECK (vertical_code IS NOT NULL OR is_legacy),
  CONSTRAINT org_role_catalog_role_key_shape_check
    CHECK (role_key ~ '^[a-z][a-z0-9_]*$')
);
CREATE INDEX org_role_catalog_tier_idx ON org_role_catalog (tier_code);
CREATE INDEX org_role_catalog_vertical_idx ON org_role_catalog (vertical_code);

INSERT INTO org_tiers (tier_code, label, rank) VALUES
  ('ceo_cxo', 'CEO / CxO', 1),
  ('director', 'Director', 2),
  ('head', 'Head (Ops-Head)', 3),
  ('manager', 'Manager', 4),
  ('am', 'Assistant Manager', 5);

INSERT INTO org_verticals (vertical_code, label, sort_order) VALUES
  ('procurement', 'Procurement', 1),
  ('preventive_care', 'Preventive Care', 2),
  ('breeding', 'Breeding', 3),
  ('health', 'Health', 4),
  ('growth', 'Growth', 5),
  ('infrastructure', 'Infrastructure', 6),
  ('feed', 'Feed', 7),
  ('milk', 'Milk', 8),
  ('sales', 'Sales', 9);

-- Legacy flat roles (pre-existing; unchanged behavior in permissions.go).
-- Kept in the catalog so user_scope_grants / auth_pending_email_grants can
-- keep referencing them under the new FK with zero data migration.
INSERT INTO org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label) VALUES
  ('admin', 'ceo_cxo', NULL, true, 'Admin (system/product admin, legacy flat)'),
  ('ceo_internal', 'ceo_cxo', NULL, true, 'CEO / CxO (founder/builder cohort, legacy flat)'),
  ('verifier', 'director', NULL, true, 'Verifier (video verification team, cross-vertical, legacy flat)'),
  ('park_head', 'head', NULL, true, 'Park Head (legacy flat, pre org-role-model)'),
  ('pc_director', 'director', 'preventive_care', true, 'Preventive Care Director (legacy flat)'),
  ('operator', 'am', NULL, true, 'Operator (legacy flat ground executor)');

-- New tier x vertical composite roles -- the org-role-model.md target shape.
-- role_key = "<tier_code>_<vertical_code>", matching
-- permissions.RoleKey(tier, vertical) in Go. CEO/CxO is intentionally absent
-- here: it stays the flat 'ceo_internal' role above (founder/builder
-- visibility invariant, AGENTS.md) rather than getting a per-vertical key.
INSERT INTO org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label) VALUES
  ('director_procurement', 'director', 'procurement', false, 'Director -- Procurement'),
  ('director_preventive_care', 'director', 'preventive_care', false, 'Director -- Preventive Care'),
  ('director_breeding', 'director', 'breeding', false, 'Director -- Breeding'),
  ('director_health', 'director', 'health', false, 'Director -- Health'),
  ('director_growth', 'director', 'growth', false, 'Director -- Growth'),
  ('director_infrastructure', 'director', 'infrastructure', false, 'Director -- Infrastructure'),
  ('director_feed', 'director', 'feed', false, 'Director -- Feed'),
  ('director_milk', 'director', 'milk', false, 'Director -- Milk'),
  ('director_sales', 'director', 'sales', false, 'Director -- Sales'),
  ('head_procurement', 'head', 'procurement', false, 'Head (Ops-Head) -- Procurement'),
  ('head_preventive_care', 'head', 'preventive_care', false, 'Head (Ops-Head) -- Preventive Care'),
  ('head_breeding', 'head', 'breeding', false, 'Head (Ops-Head) -- Breeding'),
  ('head_health', 'head', 'health', false, 'Head (Ops-Head) -- Health'),
  ('head_growth', 'head', 'growth', false, 'Head (Ops-Head) -- Growth'),
  ('head_infrastructure', 'head', 'infrastructure', false, 'Head (Ops-Head) -- Infrastructure'),
  ('head_feed', 'head', 'feed', false, 'Head (Ops-Head) -- Feed'),
  ('head_milk', 'head', 'milk', false, 'Head (Ops-Head) -- Milk'),
  ('head_sales', 'head', 'sales', false, 'Head (Ops-Head) -- Sales'),
  ('manager_procurement', 'manager', 'procurement', false, 'Manager -- Procurement'),
  ('manager_preventive_care', 'manager', 'preventive_care', false, 'Manager -- Preventive Care'),
  ('manager_breeding', 'manager', 'breeding', false, 'Manager -- Breeding'),
  ('manager_health', 'manager', 'health', false, 'Manager -- Health'),
  ('manager_growth', 'manager', 'growth', false, 'Manager -- Growth'),
  ('manager_infrastructure', 'manager', 'infrastructure', false, 'Manager -- Infrastructure'),
  ('manager_feed', 'manager', 'feed', false, 'Manager -- Feed'),
  ('manager_milk', 'manager', 'milk', false, 'Manager -- Milk'),
  ('manager_sales', 'manager', 'sales', false, 'Manager -- Sales'),
  ('am_procurement', 'am', 'procurement', false, 'Assistant Manager -- Procurement'),
  ('am_preventive_care', 'am', 'preventive_care', false, 'Assistant Manager -- Preventive Care'),
  ('am_breeding', 'am', 'breeding', false, 'Assistant Manager -- Breeding'),
  ('am_health', 'am', 'health', false, 'Assistant Manager -- Health'),
  ('am_growth', 'am', 'growth', false, 'Assistant Manager -- Growth'),
  ('am_infrastructure', 'am', 'infrastructure', false, 'Assistant Manager -- Infrastructure'),
  ('am_feed', 'am', 'feed', false, 'Assistant Manager -- Feed'),
  ('am_milk', 'am', 'milk', false, 'Assistant Manager -- Milk'),
  ('am_sales', 'am', 'sales', false, 'Assistant Manager -- Sales');

-- Swap the hardcoded role CHECK-IN enums for a catalog FK on the two
-- authorization-critical tables. workforce_members.primary_role_hint and
-- obligation_escalations.escalated_to_role keep their existing (narrower,
-- advisory) CHECK-IN enums unchanged -- they are HR display hints / escalation
-- routing labels, not RBAC grants, and are out of scope for this migration.
ALTER TABLE user_scope_grants
  DROP CONSTRAINT IF EXISTS user_scope_grants_role_check;
ALTER TABLE user_scope_grants
  ADD CONSTRAINT user_scope_grants_role_fk
  FOREIGN KEY (role) REFERENCES org_role_catalog (role_key);

ALTER TABLE auth_pending_email_grants
  DROP CONSTRAINT IF EXISTS auth_pending_email_grants_role_check;
ALTER TABLE auth_pending_email_grants
  ADD CONSTRAINT auth_pending_email_grants_role_fk
  FOREIGN KEY (role) REFERENCES org_role_catalog (role_key);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE auth_pending_email_grants
  DROP CONSTRAINT IF EXISTS auth_pending_email_grants_role_fk;
ALTER TABLE auth_pending_email_grants
  ADD CONSTRAINT auth_pending_email_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'pc_director', 'operator', 'verifier', 'ceo_internal'));

ALTER TABLE user_scope_grants
  DROP CONSTRAINT IF EXISTS user_scope_grants_role_fk;
ALTER TABLE user_scope_grants
  ADD CONSTRAINT user_scope_grants_role_check
  CHECK (role IN ('admin', 'park_head', 'pc_director', 'operator', 'verifier', 'ceo_internal'));

DROP TABLE IF EXISTS org_role_catalog;
DROP TABLE IF EXISTS org_verticals;
DROP TABLE IF EXISTS org_tiers;
-- +goose StatementEnd
