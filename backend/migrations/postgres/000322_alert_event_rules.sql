-- +goose Up
-- USER-DEFINED EVENT ALERTS (maintainer decision 2026-09-16, same day as 000320).
--
-- "If a birth happens I want to add it as an alert, and anything like that in future" -- so an
-- alert is no longer only a code-defined rule. The code keeps a CATALOG of durable business
-- events it knows how to read for a park-day (alerts/domain.EventKinds: birth recorded, death
-- recorded, animal sold, animal added, shifting raised/approved, feed purchase recorded); the
-- people HRMS ticks for alerts.configure compose alerts from that catalog on the Configure
-- drawer. One row here is one such composition: the farm's own wording, the kind it watches, a
-- severity and a switch. A row naming a kind the catalog no longer carries is ignored on read
-- and refused on write, so config can never invent a reader.
--
-- No seed: the farm starts with no event alerts and adds what it wants to see.
CREATE TABLE IF NOT EXISTS alert_event_rules (
    alert_event_rule_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (tenant_id),
    label       text NOT NULL,
    event_kind  text NOT NULL,
    severity    text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    created_by  uuid,
    updated_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT alert_event_rules_label_check CHECK (btrim(label) <> '' AND length(label) <= 80),
    CONSTRAINT alert_event_rules_kind_check CHECK (btrim(event_kind) <> ''),
    CONSTRAINT alert_event_rules_severity_check CHECK (severity IN ('critical', 'warning'))
);

CREATE INDEX IF NOT EXISTS alert_event_rules_tenant_idx ON alert_event_rules (tenant_id, created_at);

COMMENT ON TABLE alert_event_rules IS
  'User-composed Alerts page rules (maintainer decision 2026-09-16): one row = "tell me when <catalog event kind> happens", with the farm''s label, a severity and a switch. The kinds and their park-day readers are code (alerts/domain.EventKinds); composing an alert from one is config. Written only through /alerts/config/events by holders of alerts.configure.';

-- +goose Down
DROP TABLE IF EXISTS alert_event_rules;
