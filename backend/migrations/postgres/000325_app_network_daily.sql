-- +goose Up
-- App-wide Firebase Performance summaries; no tenant attribution.
CREATE TABLE analytics.app_network_daily (
 source_app_id text NOT NULL,
 event_date date NOT NULL,
 app_version text NOT NULL,
 route_pattern text NOT NULL,
 http_method text NOT NULL CHECK (http_method IN ('GET','HEAD','POST','PUT','PATCH','DELETE','OPTIONS','CONNECT','TRACE','OTHER')),
 response_class text NOT NULL CHECK (response_class IN ('1xx','2xx','3xx','4xx','5xx','unknown')),
 total_requests bigint NOT NULL CHECK (total_requests > 0),
 p50_ms double precision NOT NULL CHECK (p50_ms >= 0),
 p95_ms double precision NOT NULL CHECK (p95_ms >= p50_ms),
 p99_ms double precision NOT NULL CHECK (p99_ms >= p95_ms),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source_app_id,event_date,app_version,route_pattern,http_method,response_class)
);
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'goatos_grafana_ro') THEN
  GRANT USAGE ON SCHEMA analytics TO goatos_grafana_ro;
  GRANT SELECT ON analytics.app_network_daily TO goatos_grafana_ro;
 END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
DROP TABLE analytics.app_network_daily;
