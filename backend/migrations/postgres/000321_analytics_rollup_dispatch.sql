-- +goose Up
-- Durable dispatch leases for the shared kernel-worker cadence; job success
-- remains analytics.rollup_run's final status after every configured adapter.
CREATE TABLE analytics.rollup_dispatch (
 source_date date PRIMARY KEY,
 claimed_at timestamptz NOT NULL,
 lease_until timestamptz NOT NULL
);
-- +goose Down
DROP TABLE analytics.rollup_dispatch;
