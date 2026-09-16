-- +goose Up
-- Reserved migration number: the unpublished calendar prototype was superseded by
-- the database-only configuration in 319. Retain a no-op to keep clean-install
-- numbering contiguous and existing local test migration receipts compatible.
SELECT 1;

-- +goose Down
SELECT 1;
