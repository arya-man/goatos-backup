-- +goose Up
--
-- seed-fixture-guard:ignore: operational/audit-class attendance tables, no
-- vaccination seed-data contract. Both tables below are brand-new creations;
-- the only roster token is `REFERENCES workforce_members` -- a foreign key to
-- the roster, not a change to it. Nothing here is seeded: clock rows are born
-- exclusively from live phone punches, so the seed-migration coupling rule
-- classifies these as operational/audit tables with no seed command to update
-- (docs/runbooks/initial-seed-migration-coupling.md, "operational/audit/event"
-- class).
--
-- CLOCK IN / CLOCK OUT (maintainer decisions 2026-08-27/28, recorded in
-- docs/features/clock-in-out/plan.md):
--
--   D1  offline punches are ALLOWED and flagged; the mock-location check runs
--       client-side at tap AND server-side at drain.
--   D2  EVERYONE with an app login clocks in.
--   D3  location is recorded + distance-flagged; no hard geofence in V1.
--   D4  ONE in/out pair per IST business day; a forgotten clock-out
--       auto-closes at midnight IST with NO invented hours.
--
-- Two tables, one write transaction:
--
--   workforce_clock_events   append-only, one row per punch (in or out), the
--                            full honest capture: location, device, integrity.
--                            This IS the audit trail; rows are never updated.
--   workforce_clock_entries  the person x business-day pairing read model the
--                            screens serve from, maintained in the SAME
--                            transaction as the event insert (atomic
--                            transition + owned read model rule).
--
-- Grain proof (projection-review): producer grain = one event row per punch;
-- consumer grain = one entry row per (tenant_id, workforce_member_id,
-- business_date) -- enforced by workforce_clock_entries_day_uq below, so the
-- event->entry join is 1:1 by construction and worked_minutes ranges over
-- exactly one entry row. No join fan-out is possible.

CREATE TABLE workforce_clock_events (
    clock_event_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL,
    workforce_member_id uuid NOT NULL REFERENCES workforce_members (workforce_member_id),
    user_id             uuid NOT NULL,
    event_type          text NOT NULL CHECK (event_type IN ('clock_in', 'clock_out')),
    -- IST business day, derived SERVER-side from recorded_at for an online
    -- punch; for an offline-queued punch (network_type = 'offline_queued') it
    -- anchors on the device captured_at instead, with clock_skew_ms making the
    -- gap auditable (maintainer decision D1).
    business_date       date NOT NULL,
    captured_at         timestamptz NOT NULL,
    recorded_at         timestamptz NOT NULL DEFAULT now(),
    clock_skew_ms       bigint,
    -- location: a punch with no GPS is ACCEPTED and flagged, never refused --
    -- GPS genuinely fails indoors and refusing would block honest people.
    location_status     text NOT NULL CHECK (location_status IN ('captured', 'permission_missing', 'unavailable')),
    latitude            double precision,
    longitude           double precision,
    gps_accuracy_m      double precision,
    address             text,
    -- integrity: only PROVEN dishonesty refuses. A refused punch writes NO row
    -- here (the write is rejected 422 mock_location_detected before insert);
    -- these columns record what the accepted punch reported.
    mock_location       boolean NOT NULL DEFAULT false,
    mock_provider_packages text[],
    developer_options_enabled boolean,
    -- device snapshot at punch time; canonical device row is workforce_member_devices.
    device_id           uuid,
    app_install_id      text,
    app_version         text,
    app_version_code    text,
    build_type          text,
    os_version          text,
    sdk_version         text,
    device_model        text,
    network_type        text NOT NULL DEFAULT 'online' CHECK (network_type IN ('online', 'offline_queued')),
    battery_pct         smallint CHECK (battery_pct IS NULL OR (battery_pct >= 0 AND battery_pct <= 100)),
    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX workforce_clock_events_day_idx
    ON workforce_clock_events (tenant_id, business_date, workforce_member_id);

CREATE TABLE workforce_clock_entries (
    clock_entry_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL,
    workforce_member_id uuid NOT NULL REFERENCES workforce_members (workforce_member_id),
    business_date       date NOT NULL,
    clock_in_event_id   uuid NOT NULL REFERENCES workforce_clock_events (clock_event_id),
    clock_out_event_id  uuid REFERENCES workforce_clock_events (clock_event_id),
    -- Effective punch instants (server recorded_at for online punches, device
    -- captured_at for offline-queued ones -- the same anchor business_date
    -- uses, so hours and day always agree).
    clock_in_at         timestamptz NOT NULL,
    clock_out_at        timestamptz,
    -- Backend-owned working-hours truth. NULL while open, and NULL FOREVER on
    -- an auto_closed entry -- an end time is never invented (decision D4).
    worked_minutes      integer CHECK (worked_minutes IS NULL OR worked_minutes >= 0),
    status              text NOT NULL CHECK (status IN ('open', 'closed', 'auto_closed')),
    -- Denormalized flags for list rendering; source of truth stays on events.
    offline_punch       boolean NOT NULL DEFAULT false,
    location_missing    boolean NOT NULL DEFAULT false,
    row_version         bigint NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    -- ONE pair per person per business day (decision D4). This is also the
    -- 1:1 grain lock the presence/list reads join on.
    CONSTRAINT workforce_clock_entries_day_uq UNIQUE (tenant_id, workforce_member_id, business_date)
);

-- Admin list + presence board read: whole-roster day scan, keyset over member.
CREATE INDEX workforce_clock_entries_day_idx
    ON workforce_clock_entries (tenant_id, business_date, workforce_member_id);
-- Auto-close sweeper claim: open entries older than the current business day.
CREATE INDEX workforce_clock_entries_open_idx
    ON workforce_clock_entries (tenant_id, business_date)
    WHERE status = 'open';

-- +goose Down
DROP TABLE IF EXISTS workforce_clock_entries;
DROP TABLE IF EXISTS workforce_clock_events;
