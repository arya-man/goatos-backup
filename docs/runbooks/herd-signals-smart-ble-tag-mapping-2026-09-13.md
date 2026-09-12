# Herd Signals smart BLE tag mapping - 2026-09-13

Source: Ravi's WhatsApp photo screenshots captured on 2026-09-13 around 02:19-02:22 IST. The BLE tag code is read from the yellow smart tag face. The RFID values are read from the adjacent message bubbles in the same screenshots.

Staging verification: checked against real `goatos-stg` Cloud SQL on 2026-09-13. All 23 RFID values existed as active `goat_identifiers`. The four BLE tags with two RFID values each resolved both RFIDs to the same goat. Across the 19 BLE groups, there were 19 distinct goats.

Mapping model:

- Existing RFID identifiers stay as `animal_identifier_1` / `animal_identifier_2`.
- The smart BLE tag is added as a separate nullable identifier row with `identifier_type = 'smart_ble_tag'` and `smart_tag_capable = true`.
- A row with two RFID values means one animal already has both RFID slots; the BLE tag becomes that same animal's third physical identifier.
- The BLE tag id and BLE MAC are both inserted as smart-tag-capable identifiers so live packets resolve whether the gateway reports `tag_id` or `tag_mac`.

| BLE tag | BLE MAC | RFID 1 | RFID 2 |
|---|---|---|---|
| A00041 | F0:C9:90:A0:00:41 | 901007000504387 | 901007000506078 |
| A00031 | F0:C9:90:A0:00:31 | 901007000504394 | 901007000506033 |
| A0002C | F0:C9:90:A0:00:2C | 901007000504141 | 901007000506071 |
| A0002F | F0:C9:90:A0:00:2F | 901007000506051 | 901007000504192 |
| A0002D | F0:C9:90:A0:00:2D | 901007000504154 |  |
| A00034 | F0:C9:90:A0:00:34 | 901007000504386 |  |
| A00030 | F0:C9:90:A0:00:30 | 901007000504072 |  |
| A00038 | F0:C9:90:A0:00:38 | 901007000504106 |  |
| A0002B | F0:C9:90:A0:00:2B | 901007000504067 |  |
| A00040 | F0:C9:90:A0:00:40 | 901007000504111 |  |
| A00035 | F0:C9:90:A0:00:35 | 901007000504197 |  |
| A0003A | F0:C9:90:A0:00:3A | 901007000504194 |  |
| A0003C | F0:C9:90:A0:00:3C | 901007000505115 |  |
| A0002E | F0:C9:90:A0:00:2E | 901007000504342 |  |
| A0003E | F0:C9:90:A0:00:3E | 901007000505093 |  |
| A0002A | F0:C9:90:A0:00:2A | 901007000504378 |  |
| A0003F | F0:C9:90:A0:00:3F | 901007000505106 |  |
| A00033 | F0:C9:90:A0:00:33 | 901007000506132 |  |
| A00036 | F0:C9:90:A0:00:36 | 901007000504155 |  |

Migration safety checks in `000298_smart_ble_tag_mapping_seed.sql`:

1. Every RFID must resolve to exactly one active RFID identifier.
2. Every BLE group must resolve to exactly one goat.
3. The 19 BLE groups must resolve to 19 distinct goats.
4. Existing BLE tag id or MAC identifiers claimed by another goat abort the migration.
5. `herd_signal_tag_latest` is updated only as denormalized cache after the authoritative `goat_identifiers` rows are written.
