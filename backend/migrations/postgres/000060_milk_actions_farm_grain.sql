-- +goose Up
-- Maintainer decision 2026-07-29: Milk Preparation is one farm-day Action and Milk Feeding is a
-- separate set of four farm-session Actions. The shed-grain rows created by 000056/000057 remain
-- immutable history and are retired; current operator contracts never expose a shed.

ALTER TABLE milk_preparation_completions
  DROP CONSTRAINT milk_preparation_completions_shed_required_check;

DROP INDEX milk_preparation_completions_shed_day_uidx;

ALTER TABLE milk_preparation_completions
  ADD CONSTRAINT milk_preparation_completions_active_farm_grain_check
  CHECK (status = 'retired' OR shed_id IS NULL) NOT VALID;
ALTER TABLE milk_preparation_completions
  VALIDATE CONSTRAINT milk_preparation_completions_active_farm_grain_check;

CREATE UNIQUE INDEX milk_preparation_completions_farm_day_uidx
  ON milk_preparation_completions (tenant_id, park_id, preparation_date)
  WHERE status <> 'retired';

DROP INDEX milk_preparation_completions_serving_idx;
CREATE INDEX milk_preparation_completions_serving_idx
  ON milk_preparation_completions (tenant_id, preparation_date, park_id, status);

ALTER TABLE milk_feeding_tasks
  DROP CONSTRAINT milk_feeding_tasks_status_check;
ALTER TABLE milk_feeding_tasks
  ADD CONSTRAINT milk_feeding_tasks_status_check
  CHECK (status IN ('not_submitted', 'pending_verification', 'completed', 'rework', 'retired')) NOT VALID;

UPDATE milk_feeding_tasks SET status = 'retired', updated_at = now();

ALTER TABLE milk_feeding_tasks
  VALIDATE CONSTRAINT milk_feeding_tasks_status_check;
ALTER TABLE milk_feeding_tasks
  ALTER COLUMN shed_id DROP NOT NULL;
ALTER TABLE milk_feeding_tasks
  DROP CONSTRAINT milk_feeding_tasks_grain_uq;
ALTER TABLE milk_feeding_tasks
  ADD CONSTRAINT milk_feeding_tasks_active_farm_grain_check
  CHECK (status = 'retired' OR shed_id IS NULL) NOT VALID;
ALTER TABLE milk_feeding_tasks
  VALIDATE CONSTRAINT milk_feeding_tasks_active_farm_grain_check;

CREATE UNIQUE INDEX milk_feeding_tasks_farm_session_uidx
  ON milk_feeding_tasks (tenant_id, park_id, feeding_date, session_no)
  WHERE status <> 'retired';

DROP INDEX milk_feeding_tasks_worklist_idx;
CREATE INDEX milk_feeding_tasks_worklist_idx
  ON milk_feeding_tasks (tenant_id, feeding_date, status, park_id, session_no);

CREATE TABLE milk_feeding_farm_watchlist (
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  consecutive_yes smallint NOT NULL DEFAULT 0 CHECK (consecutive_yes BETWEEN 0 AND 1),
  remarks text,
  added_date date NOT NULL,
  added_session smallint NOT NULL CHECK (added_session BETWEEN 1 AND 4),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, park_id, goat_id),
  CONSTRAINT milk_feeding_farm_watchlist_park_fk FOREIGN KEY (tenant_id, park_id)
    REFERENCES locations(tenant_id, location_id),
  CONSTRAINT milk_feeding_farm_watchlist_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES goats(tenant_id, goat_id)
);

WITH eligible_farms AS (
  SELECT g.tenant_id, g.park_id, count(*)::integer AS head_count
  FROM goats g
  JOIN locations p ON p.tenant_id = g.tenant_id AND p.location_id = g.park_id
    AND p.location_type = 'park' AND p.status = 'active'
  WHERE g.merged_into_goat_id IS NULL AND g.lifecycle_status = 'alive'
    AND upper(regexp_replace(trim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g'))
      IN ('K1', 'K2', 'K3', 'ICUKID', 'QUARANTINEMILKKID')
  GROUP BY g.tenant_id, g.park_id
), sessions(session_no, due_time) AS (
  VALUES (1, time '08:00'), (2, time '12:00'), (3, time '16:00'), (4, time '21:00')
), business_day AS (
  SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date AS feeding_date
)
INSERT INTO milk_feeding_tasks (tenant_id, park_id, shed_id, feeding_date, session_no, due_at, head_count)
SELECT f.tenant_id, f.park_id, NULL, d.feeding_date, s.session_no,
       (d.feeding_date + s.due_time) AT TIME ZONE 'Asia/Kolkata', f.head_count
FROM eligible_farms f CROSS JOIN sessions s CROSS JOIN business_day d
ON CONFLICT DO NOTHING;

-- +goose Down
-- Forward-only grain correction: reactivating shed rows would merge obsolete and current state.
SELECT 1;
