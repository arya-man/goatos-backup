-- Track whether a goat identifier is backed by a BLE telemetry-capable ear tag.
--
-- This belongs on goat_identifiers, not goats: an animal can carry two tags and
-- only one of those identifiers may emit BLE measurements. NULL means unknown
-- for legacy/imported rows, true means telemetry-capable, false means explicitly
-- a plain/non-telemetry identifier.

-- +goose Up
ALTER TABLE public.goat_identifiers
  ADD COLUMN IF NOT EXISTS smart_tag_capable boolean;

-- +goose Down
ALTER TABLE public.goat_identifiers
  DROP COLUMN IF EXISTS smart_tag_capable;
