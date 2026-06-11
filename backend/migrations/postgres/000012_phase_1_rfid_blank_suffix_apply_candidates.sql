-- +goose Up
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS legacy_import_rows_rfid_apply_candidates_idx
  ON legacy_import_rows (import_run_id, row_number, legacy_row_id)
  WHERE processing_state = 'pending'
     OR (
       processing_state = 'needs_review'
       AND normalized_payload @> '{"processing_reasons":["blank_old_tag_suffix"]}'::jsonb
     );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS legacy_import_rows_rfid_apply_candidates_idx;
-- +goose StatementEnd
