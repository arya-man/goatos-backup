-- +goose Up
-- Milk Preparation question answers and four daily shed-session Milk Feeding Actions replace the
-- legacy Slack/Sheets workflow. All new tables are module-owned and start empty, so ordinary
-- indexes are safe during creation. Existing preparation proof history remains immutable.
ALTER TABLE milk_preparation_proof_attempts
  ADD COLUMN answers jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE milk_feeding_tasks (
  task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  feeding_date date NOT NULL,
  session_no smallint NOT NULL CHECK (session_no BETWEEN 1 AND 4),
  due_at timestamptz NOT NULL,
  head_count integer NOT NULL CHECK (head_count >= 0),
  status text NOT NULL DEFAULT 'not_submitted'
    CHECK (status IN ('not_submitted', 'pending_verification', 'completed', 'rework')),
  current_attempt_no integer NOT NULL DEFAULT 0 CHECK (current_attempt_no >= 0),
  assigned_operator_id uuid,
  submitted_by uuid,
  submitted_at timestamptz,
  verified_by uuid,
  verified_at timestamptz,
  rework_reason text,
  row_version integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT milk_feeding_tasks_park_fk FOREIGN KEY (tenant_id, park_id)
    REFERENCES locations(tenant_id, location_id),
  CONSTRAINT milk_feeding_tasks_shed_fk FOREIGN KEY (tenant_id, shed_id)
    REFERENCES locations(tenant_id, location_id),
  CONSTRAINT milk_feeding_tasks_grain_uq UNIQUE (tenant_id, shed_id, feeding_date, session_no)
);

CREATE INDEX milk_feeding_tasks_worklist_idx
  ON milk_feeding_tasks (tenant_id, feeding_date, status, park_id, shed_id, session_no);

CREATE TABLE milk_feeding_attempts (
  attempt_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL REFERENCES milk_feeding_tasks(task_id),
  attempt_no integer NOT NULL CHECK (attempt_no > 0),
  answers jsonb NOT NULL,
  proof_refs jsonb NOT NULL,
  submitted_by uuid NOT NULL,
  submitted_at timestamptz NOT NULL,
  idempotency_key text NOT NULL,
  request_fingerprint text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT milk_feeding_attempts_attempt_uq UNIQUE (tenant_id, task_id, attempt_no),
  CONSTRAINT milk_feeding_attempts_idempotency_uq UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX milk_feeding_attempts_task_idx
  ON milk_feeding_attempts (tenant_id, task_id, attempt_no DESC);

CREATE TABLE milk_feeding_watchlist (
  tenant_id uuid NOT NULL,
  shed_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  consecutive_yes smallint NOT NULL DEFAULT 0 CHECK (consecutive_yes BETWEEN 0 AND 1),
  remarks text,
  added_date date NOT NULL,
  added_session smallint NOT NULL CHECK (added_session BETWEEN 1 AND 4),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, shed_id, goat_id),
  CONSTRAINT milk_feeding_watchlist_shed_fk FOREIGN KEY (tenant_id, shed_id)
    REFERENCES locations(tenant_id, location_id),
  CONSTRAINT milk_feeding_watchlist_goat_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES goats(tenant_id, goat_id)
);

CREATE INDEX milk_feeding_watchlist_shed_idx
  ON milk_feeding_watchlist (tenant_id, shed_id, goat_id);

-- Seed today's durable Actions. The five-minute kernel stage uses the same set-based shape for
-- every following India business date. Stage spellings are normalized because the imported herd
-- currently contains both `ICU- kid` and `ICU-Kid`.
-- projection-review: membership=goats, unique on (tenant_id, goat_id), filtered to live unmerged
-- animals whose normalized management_stage is a milk cohort; group_key=(tenant_id, park_id,
-- shed_id) which is exactly the grain milk_feeding_tasks is keyed at, so one shed-session is one
-- row; join_cardinality=no join on the counted side -- head_count is a plain count over goats and
-- the sessions VALUES list is a 4-row cross product that multiplies SESSIONS, never animals;
-- pagination=none, this is a one-shot set-based seed of today's rows; scope=park+shed, taken from
-- the goat's own canonical FKs and never from a caller
WITH eligible_sheds AS (
  SELECT g.tenant_id, g.park_id, g.shed_id, count(*)::integer AS head_count
  FROM goats g
  WHERE g.merged_into_goat_id IS NULL
    AND g.lifecycle_status = 'alive'
    AND upper(regexp_replace(trim(coalesce(g.management_stage, '')), '[^A-Za-z0-9]+', '', 'g'))
      IN ('K1', 'K2', 'K3', 'ICUKID', 'QUARANTINEMILKKID')
    AND g.park_id IS NOT NULL AND g.shed_id IS NOT NULL
  GROUP BY g.tenant_id, g.park_id, g.shed_id
), sessions(session_no, due_time) AS (
  VALUES (1, time '08:00'), (2, time '12:00'), (3, time '16:00'), (4, time '21:00')
), business_day AS (
  SELECT (now() AT TIME ZONE 'Asia/Kolkata')::date AS feeding_date
)
INSERT INTO milk_feeding_tasks (
  tenant_id, park_id, shed_id, feeding_date, session_no, due_at, head_count
)
SELECT e.tenant_id, e.park_id, e.shed_id, d.feeding_date, s.session_no,
       (d.feeding_date + s.due_time) AT TIME ZONE 'Asia/Kolkata', e.head_count
FROM eligible_sheds e CROSS JOIN sessions s CROSS JOIN business_day d
ON CONFLICT (tenant_id, shed_id, feeding_date, session_no) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS milk_feeding_watchlist;
DROP TABLE IF EXISTS milk_feeding_attempts;
DROP TABLE IF EXISTS milk_feeding_tasks;
ALTER TABLE milk_preparation_proof_attempts DROP COLUMN IF EXISTS answers;
