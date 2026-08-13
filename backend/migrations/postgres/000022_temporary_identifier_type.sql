-- +goose Up
-- Temporary vs permanent animal identifiers.
--
-- Kids are often born before a permanent RFID is available: the operator tags the newborn with a
-- temporary/provisional tag and swaps in the real RFID later. This adds `temporary_tag` as a third
-- identifier_type alongside the two RFID slots (animal_identifier_1 / animal_identifier_2).
--
-- A temporary_tag may be the goat's PRIMARY identity within its own type (primary-per-goat is
-- unique per identifier_type), so a goat carrying only a temporary tag has NO active
-- animal_identifier_1 and therefore still counts as an untagged kid in the herd projection until it
-- is promoted -- which is the correct semantics: it still needs a permanent tag.
--
-- Uniqueness is unchanged: goat_identifiers_lifetime_value_unique (tenant_id, normalized_value)
-- stays in force, so a temporary value is claimed for the tenant's lifetime even after it is
-- retired on promotion. A physical temporary tag is a real, single-use object, so that is the
-- intended behaviour.
--
-- Lock-safe: each CHECK is dropped and re-added NOT VALID, then validated (a full-table validate
-- scan under SHARE UPDATE EXCLUSIVE rather than an ACCESS EXCLUSIVE rewrite). Additive only.

ALTER TABLE public.goat_identifiers DROP CONSTRAINT goat_identifiers_type_check;
ALTER TABLE public.goat_identifiers
  ADD CONSTRAINT goat_identifiers_type_check
  CHECK (identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text, 'temporary_tag'::text]))
  NOT VALID;
ALTER TABLE public.goat_identifiers VALIDATE CONSTRAINT goat_identifiers_type_check;

ALTER TABLE public.identifier_policies DROP CONSTRAINT identifier_policies_identifier_type_check;
ALTER TABLE public.identifier_policies
  ADD CONSTRAINT identifier_policies_identifier_type_check
  CHECK (identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text, 'temporary_tag'::text]))
  NOT VALID;
ALTER TABLE public.identifier_policies VALIDATE CONSTRAINT identifier_policies_identifier_type_check;

-- The policy row governing temporary_tag behaviour. Mirrors animal_identifier_1 (a primary,
-- globally-unique, scoped, reject-on-invalid identifier) so the same normalizer and uniqueness
-- rules apply. This is static catalog config seeded by the migration itself, exactly as the two
-- RFID-slot policies were in the clean-slate baseline.
INSERT INTO public.identifier_policies
  (policy_version, identifier_type, default_scope_type, scope_required, active_uniqueness,
   auto_link_allowed, primary_allowed, unknown_scope_action, missing_or_conflicting_scope_action,
   normalizer_version, format_validator_version, invalid_value_action, created_at, approved_by)
VALUES
  ('phase1-identifier-v1', 'temporary_tag', 'global', true, 'global',
   false, true, 'reject', 'reject',
   'identifier_normalizer_v1', NULL, 'reject', now(), NULL)
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM public.identifier_policies WHERE identifier_type = 'temporary_tag';

ALTER TABLE public.identifier_policies DROP CONSTRAINT identifier_policies_identifier_type_check;
ALTER TABLE public.identifier_policies
  ADD CONSTRAINT identifier_policies_identifier_type_check
  CHECK (identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text]));

ALTER TABLE public.goat_identifiers DROP CONSTRAINT goat_identifiers_type_check;
ALTER TABLE public.goat_identifiers
  ADD CONSTRAINT goat_identifiers_type_check
  CHECK (identifier_type = ANY (ARRAY['animal_identifier_1'::text, 'animal_identifier_2'::text]));
