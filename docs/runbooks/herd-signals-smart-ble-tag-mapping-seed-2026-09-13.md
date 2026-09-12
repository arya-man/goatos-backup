# Herd Signals smart BLE tag mapping seed closeout

This companion note exists for the seed-migration guard around migration `000298_smart_ble_tag_mapping_seed.sql`.

The migration changes the canonical `goat_identifiers` table by adding `smart_ble_tag` as a third physical identifier type and seeding 19 farm-captured BLE mappings. It does not change the vaccination/HRMS source fixture contract: the 23 captured RFID values remain existing `animal_identifier_1` / `animal_identifier_2` rows, while each BLE tag id and BLE MAC is inserted as a separate smart-tag-capable identifier owned by Herd Signals.

Closeout checks after applying the migration:

- `goat_identifiers` contains 38 active `smart_ble_tag` rows with `source_system = 'herd_signals'` and `source_record_id LIKE '2026-09-13:%'`.
- Those 38 rows cover 19 distinct goats.
- The 23 captured RFID values still exist as active RFID identifiers and are not rewritten into smart BLE rows.
- `herd_signal_tag_latest` is only denormalized after the identifier rows are written, so live mapping state follows the authoritative identifiers.
