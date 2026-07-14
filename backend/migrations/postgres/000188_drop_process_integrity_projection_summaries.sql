-- +goose Up
/*
U7 follow-up: 000187 dropped process_integrity_projection_rows/_state but missed the
process_integrity_projection_summaries table (from 000176). It has no live reader (the
process-integrity screen serves canonical indexed SQL); only uncalled dead code
referenced it. Drop it to complete the retirement. Recoverable via tag
kernel-split-workers-v1.
*/
DROP TABLE IF EXISTS process_integrity_projection_summaries CASCADE;

-- +goose Down
-- Irreversible in place; restore from git tag kernel-split-workers-v1 if needed. No-op.
SELECT 1;
