-- +goose Up
-- WHO GETS WHICH ALERT IS CONFIG, PER DESIGNATION (maintainer decision 2026-09-08).
--
-- Every leadership-facing push -- the daily low-stock alert, the overdue-load alert, the
-- proof-pending / approved / rework copies to directors, the weighing lifecycle notices, the
-- 20:30 vaccination checkpoint -- carried its audience as a literal position code in Go
-- ("ceo_internal", "growth_director", ...). Changing who is told meant a code change and a
-- release, and the farm could not say "the Park Head should also hear this" without a
-- developer.
--
-- This table is that sentence, keyed by DESIGNATION (the job title in designation_catalog),
-- never by person: an alert is addressed to a desk, and whoever holds the desk hears it. The
-- per-person device resolution stays where it is (workforce ResolvePositionRecipients: active
-- seats + active role grants + reachable devices); this table only decides WHICH desks are
-- asked.
--
-- ABSENCE MEANS THE CATALOG DEFAULT. The Go catalog (notificationaudience/domain) declares
-- every configurable alert with the audience the code carried until now, so deploying this
-- changes nobody's phone. A row is written only when an admin customises an alert; deleting
-- the row is "back to default". An EMPTY array is a real answer -- "nobody" -- and is kept
-- distinct from "not customised" precisely because the two must not be confused: one is a
-- decision, the other is the absence of one.
--
-- The alert key is validated by the application against the catalog rather than by a CHECK
-- here: the catalog grows with every new alert, and a closed SQL enum would turn each addition
-- into a migration for a table no seed path writes.
CREATE TABLE IF NOT EXISTS public.notification_alert_audiences (
  tenant_id         uuid NOT NULL REFERENCES tenants (tenant_id),
  alert_key         text NOT NULL,
  -- The designation codes that receive this alert, each a designation_catalog row. Validated
  -- against the catalog in the write transaction; stored as an array because the whole
  -- audience is replaced as one decision and read as one row per (tenant, alert).
  designation_codes text[] NOT NULL DEFAULT '{}',
  updated_at        timestamptz NOT NULL DEFAULT now(),
  updated_by        uuid,
  row_version       integer NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id, alert_key),
  CONSTRAINT notification_alert_audiences_alert_key_check
    CHECK (alert_key ~ '^[a-z_]+\.[a-z_]+$'),
  CONSTRAINT notification_alert_audiences_codes_distinct_check
    CHECK (public.goatos_text_array_is_distinct(designation_codes)),
  CONSTRAINT notification_alert_audiences_row_version_check
    CHECK (row_version >= 1)
);

COMMENT ON TABLE public.notification_alert_audiences IS
  'Per-tenant, per-alert designation audience (maintainer decision 2026-09-08). A row is a customised audience; no row means the Go catalog default (notificationaudience/domain). An empty array means nobody is told, deliberately.';
COMMENT ON COLUMN public.notification_alert_audiences.designation_codes IS
  'designation_catalog codes that receive the alert. Resolved to devices at send time through the workforce position/grant resolver; never a person id.';

-- +goose Down
DROP TABLE IF EXISTS public.notification_alert_audiences;
