-- name: GetApprovedLegacyImportPolicy :one
SELECT
  policy_version,
  source_system,
  source_dataset,
  identifier_policy_version,
  source_key_recipe,
  source_key_recipe_version,
  hash_recipe,
  hash_recipe_version,
  normalizer_version
FROM legacy_import_policies
WHERE policy_version = @policy_version
  AND status = 'approved';

-- name: HasLegacyImportRowWithDifferentHash :one
SELECT EXISTS (
  SELECT 1
  FROM legacy_import_rows
  WHERE tenant_id = @tenant_id
    AND source_system = @source_system
    AND source_dataset = @source_dataset
    AND source_row_key = @source_row_key
    AND source_row_version_hash <> @source_row_version_hash
)::bool;
