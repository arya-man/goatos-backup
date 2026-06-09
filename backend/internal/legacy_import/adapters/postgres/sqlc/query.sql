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

-- name: GetLegacyImportRunForApply :one
SELECT
  import_run_id::text AS import_run_id,
  tenant_id::text AS tenant_id,
  source_system,
  source_dataset,
  policy_version,
  dry_run,
  status,
  row_count,
  created_goat_count,
  updated_goat_count,
  conflict_count,
  error_count
FROM legacy_import_runs
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;

-- name: ListPendingLegacyImportRowsForApply :many
SELECT
  legacy_row_id::text AS legacy_row_id,
  row_number,
  source_system,
  source_dataset,
  source_record_id,
  source_row_key,
  source_key_recipe_version,
  source_row_version_hash,
  hash_recipe_version,
  raw_payload,
  normalized_payload,
  processing_state,
  error_reason
FROM legacy_import_rows
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id
  AND processing_state = 'pending'
  AND (
    sqlc.narg('cursor_row_number')::int IS NULL
    OR (row_number, legacy_row_id) > (sqlc.narg('cursor_row_number')::int, sqlc.narg('cursor_legacy_row_id')::uuid)
  )
ORDER BY row_number, legacy_row_id
LIMIT @limit_count;
