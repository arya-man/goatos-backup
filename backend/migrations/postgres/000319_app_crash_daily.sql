-- +goose Up
-- App-wide Firebase export summary. Never attribute these rows to a tenant.
CREATE TABLE IF NOT EXISTS analytics.app_crash_daily (
 source_app_id text NOT NULL,
 event_date date NOT NULL,
 app_version text NOT NULL,
 total_users bigint NOT NULL CHECK (total_users >= 0),
 total_sessions bigint NOT NULL CHECK (total_sessions >= 0),
 crashed_users bigint NOT NULL CHECK (crashed_users BETWEEN 0 AND total_users),
 crashed_sessions bigint NOT NULL CHECK (crashed_sessions BETWEEN 0 AND total_sessions),
 crash_free_users_pct double precision,
 crash_free_sessions_pct double precision,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (source_app_id,event_date,app_version)
);

-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'goatos_grafana_ro') THEN
  GRANT USAGE ON SCHEMA analytics TO goatos_grafana_ro;
  GRANT SELECT ON analytics.app_crash_daily TO goatos_grafana_ro;
 END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS analytics.app_crash_daily;
