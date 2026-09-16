-- +goose Up
-- ALERT RULES ARE CONFIG, NOT CODE (maintainer decision 2026-09-16).
--
-- The Alerts page below the Work Board lists what is OFF today: a pen whose feed sheet moved
-- against yesterday with no shifting recorded to explain it, a feed that runs out inside a few
-- days, and whatever rule is added next. WHICH rules run and at WHAT threshold is decided on the
-- page's Configure drawer by the people HRMS ticks for it (alerts.configure), never by a release.
--
-- One row per (tenant, rule). The rule CATALOG -- what a rule means, how it is detected, its
-- default threshold -- stays in code (alerts/domain.Rules): a row here only says "on or off" and
-- "at this number" for a rule the code already knows. A row for an unknown rule_key is ignored on
-- read and refused on write, so config can never invent a detector.
--
-- NO SEED: a tenant with no row for a rule runs that rule at its catalog default, enabled. The
-- defaults are the thresholds the farm already used (feed low stock at 5 days is the Stock tab's
-- red card), so deploying this migration changes nothing on screen. Absence and "deliberately set
-- to the default" therefore read the same, which is fine: the audit of WHO changed a rule is the
-- updated_by/updated_at on the row that exists once anyone touches it.
CREATE TABLE IF NOT EXISTS alert_rule_config (
    tenant_id   uuid    NOT NULL REFERENCES tenants (tenant_id),
    rule_key    text    NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    -- The one numeric knob a rule carries (days of stock, minimum head-count change). Its unit is
    -- named by the rule catalog, not stored here.
    threshold   integer NOT NULL,
    updated_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, rule_key),
    CONSTRAINT alert_rule_config_rule_key_check CHECK (btrim(rule_key) <> ''),
    CONSTRAINT alert_rule_config_threshold_check CHECK (threshold >= 0)
);

COMMENT ON TABLE alert_rule_config IS
  'Per-tenant switch and threshold for each Alerts page rule (maintainer decision 2026-09-16). The rule catalog and detectors live in code (alerts/domain.Rules); a missing row means the catalog default, enabled. Written only through PUT /alerts/config/{rule_key} by holders of alerts.configure.';
COMMENT ON COLUMN alert_rule_config.threshold IS
  'The rule''s one numeric knob, in the unit the catalog names for that rule (days for feed_low_stock, head count for pen_feed_quantity_change).';

-- +goose Down
DROP TABLE IF EXISTS alert_rule_config;
