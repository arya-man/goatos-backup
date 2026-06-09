-- name: CreateLegacyImportRun :one
INSERT INTO legacy_import_runs (
  tenant_id,
  source_name,
  source_system,
  source_dataset,
  source_file_ref,
  source_file_hash,
  policy_version,
  dry_run,
  completed_at,
  status,
  row_count,
  created_goat_count,
  updated_goat_count,
  conflict_count,
  error_count,
  started_by
) VALUES (
  @tenant_id,
  @source_name,
  @source_system,
  @source_dataset,
  sqlc.narg('source_file_ref')::text,
  @source_file_hash,
  @policy_version,
  @dry_run,
  CASE WHEN @status::text = 'completed' THEN now() ELSE NULL END,
  @status,
  @row_count,
  0,
  0,
  0,
  @error_count,
  sqlc.narg('started_by')::uuid
)
RETURNING import_run_id::text AS import_run_id;

-- name: CompleteLegacyImportRun :exec
UPDATE legacy_import_runs
SET
  completed_at = now(),
  status = 'completed',
  row_count = @row_count,
  created_goat_count = 0,
  updated_goat_count = 0,
  conflict_count = 0,
  error_count = @error_count
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;

-- name: FailLegacyImportRun :exec
UPDATE legacy_import_runs
SET
  completed_at = now(),
  status = 'failed',
  row_count = @row_count,
  created_goat_count = 0,
  updated_goat_count = 0,
  conflict_count = 0,
  error_count = @error_count
WHERE tenant_id = @tenant_id
  AND import_run_id = @import_run_id;

-- name: InsertLegacyImportRow :one
INSERT INTO legacy_import_rows (
  import_run_id,
  tenant_id,
  source_system,
  source_dataset,
  source_record_id,
  source_row_key,
  source_key_recipe_version,
  source_row_version_hash,
  hash_recipe_version,
  row_number,
  raw_payload,
  normalized_payload,
  processing_state,
  error_reason,
  matched_goat_id
) VALUES (
  @import_run_id,
  @tenant_id,
  @source_system,
  @source_dataset,
  sqlc.narg('source_record_id')::text,
  @source_row_key,
  @source_key_recipe_version,
  @source_row_version_hash,
  @hash_recipe_version,
  @row_number,
  @raw_payload,
  @normalized_payload,
  @processing_state,
  sqlc.narg('error_reason')::text,
  NULL
)
ON CONFLICT (tenant_id, source_system, source_dataset, source_row_key, source_row_version_hash) DO NOTHING
RETURNING legacy_row_id::text AS legacy_row_id;
