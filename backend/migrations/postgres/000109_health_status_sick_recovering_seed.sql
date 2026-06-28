-- +goose Up
-- +goose StatementBegin
-- Keep the status_definitions seed aligned with the goat health CHECK in 000070.
-- Config rule authoring is backend-owned; every health value accepted on goats must be visible
-- as an eligibility option, while defer authoring filters non-hold states at the contract layer.

INSERT INTO status_definitions (
  status_code,
  axis,
  display_name,
  short_label,
  description,
  legacy_label,
  sort_order,
  active,
  expected_duration_days,
  created_at,
  updated_at
) VALUES
  ('sick', 'health', 'Sick', 'Sick', 'Illness health status; vaccination generation can create visible deferred obligations when selected as a defer state.', 'Sick', 70, true, NULL, now(), now()),
  ('recovering', 'health', 'Recovering', 'Recovering', 'Recovery health status after illness/treatment; eligible for targeting but not a default defer state.', 'Recovering', 75, true, NULL, now(), now())
ON CONFLICT (status_code) DO UPDATE
SET axis = EXCLUDED.axis,
    display_name = EXCLUDED.display_name,
    short_label = EXCLUDED.short_label,
    description = EXCLUDED.description,
    legacy_label = EXCLUDED.legacy_label,
    sort_order = EXCLUDED.sort_order,
    active = true,
    updated_at = now();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM status_definitions
WHERE axis = 'health'
  AND status_code IN ('sick', 'recovering');
-- +goose StatementEnd
