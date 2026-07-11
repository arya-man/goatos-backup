-- +goose Up
-- Backend-owned daily vaccination capacity config for the shed-wise session-splitting planner. The cap
-- counts VACCINATIONS (obligation cells), not animals: one goat receiving FMD + HS = 2 vaccinations.
-- Scope 'tenant' = one daily cap for the whole tenant (overridable per center/shed later). The planner
-- splits a shed's due cells across consecutive days at max_per_day; work that cannot fit within
-- (max_buffer_days + 1) days is flagged for manager review. Frontend renders these values; it never
-- hardcodes the cap.
CREATE TABLE vaccination_capacity_config (
  tenant_id       uuid PRIMARY KEY REFERENCES tenants(tenant_id),
  max_per_day     int  NOT NULL,
  capacity_scope  text NOT NULL,
  max_buffer_days int  NOT NULL,
  overflow_policy text NOT NULL,
  row_version     int  NOT NULL DEFAULT 1,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_capacity_config_max_per_day_check CHECK (max_per_day >= 1),
  CONSTRAINT vaccination_capacity_config_scope_check CHECK (capacity_scope IN ('tenant', 'center', 'shed')),
  CONSTRAINT vaccination_capacity_config_buffer_check CHECK (max_buffer_days >= 0),
  CONSTRAINT vaccination_capacity_config_overflow_check CHECK (overflow_policy IN ('split_within_safe_window_then_mark_needs_review'))
);

-- Maintainer-set tenant default (2026-07-11): 100 vaccinations/day, tenant scope, 3 buffer days,
-- split-within-safe-window-then-mark-needs-review. Seeded for every existing tenant; tenants created
-- later fall back to the same defaults in code until a row is authored.
INSERT INTO vaccination_capacity_config (tenant_id, max_per_day, capacity_scope, max_buffer_days, overflow_policy)
SELECT tenant_id, 100, 'tenant', 3, 'split_within_safe_window_then_mark_needs_review'
FROM tenants
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS vaccination_capacity_config;
