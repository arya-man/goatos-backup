# Herd Signals OCI Database Seed Runbook

## Overview

This runbook explains how to seed the Herd Signals OCI PostgreSQL development database with realistic BLE gateway data, packet captures, and tag-to-animal mappings for local feature testing.

The seed data includes:
- Real HoneyComm BLE gateway registration (MAC `f130d402dcb4` at IP `192.168.0.9`)
- 12,000+ live BLE advertisement packets captured from 20 unique ear tags
- Derived motion snapshots and activity windows (60-second buckets per tag)
- 19 tags mapped to live animals in the Castro shed
- 1 unmapped tag (A0003B) for testing unmapped-state UI flows

All data is marked as test data with `source_system='herd-signals-oci-seed'` and can be safely re-seeded without corrupting staging data.

## Prerequisites

1. **SSH Tunnel to OCI Database**

   The seed script connects via an SSH tunnel to the OCI PostgreSQL VM. Start the tunnel first:

   ```bash
   /Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
   ```

   This opens a persistent forwarded connection on `127.0.0.1:15432`.

2. **OCI Database Environment**

   Ensure the OCI env file is available (it should be):
   ```bash
   cat /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
   ```

3. **Worktree Location**

   This runbook assumes the herd-signals worktree is at:
   ```
   /Users/ravi/goatos-work/herd-signals
   ```

## Running the Seed Script

### Option 1: Shell Wrapper (Recommended)

The shell wrapper handles environment setup, prerequisite checks, and connection verification:

```bash
cd /Users/ravi/goatos-work/herd-signals
bash tools/local/seed-herd-signals-oci.sh
```

Expected output:
```
[INFO] Checking prerequisites...
[INFO] SSH tunnel is active on 127.0.0.1:15432
[INFO] Database connection verified
[INFO] Running seed script...
[INFO] Loading BLE gateway data from: /Users/ravi/mesha/local-data/honeycomm-gateway-capture/
...
Step 1: Inserting HoneyComm gateway f130d402dcb4 at 192.168.0.9
...
[INFO] Seed script completed successfully
```

### Option 2: Direct SQL Execution

If you prefer to run the SQL directly:

```bash
source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
psql "$DATABASE_URL" -f tools/local/seed-herd-signals-oci.sql
```

## What the Seed Script Does

### Step 1: Safety Checks

The script validates:
- Database name is `goatos` (not production)
- Connection is from trusted network (localhost or Tailscale 10.88.0.0/16)

If checks fail, the script stops with an error.

### Step 2: Gateway Registration

Inserts a single HoneyComm BLE reader:
- **Gateway ID**: `honeycomm-gateway-001`
- **BLE MAC**: `f130d402dcb4`
- **Location IP**: `192.168.0.9`
- **Status**: `active`

### Step 3: Packet Data Load

Loads 12,000+ real BLE advertisement packets from CSV:
- **Source**: `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv`
- **Rows loaded**: 12,203 packets (actual count may vary due to CSV parsing)
- **Fields captured**:
  - Tag ID (e.g., `A0002A`) and MAC address
  - Signal strength (RSSI in dBm)
  - Battery voltage (converted to mV)
  - Tag temperature reading (numeric, not goat temperature)
  - Motion count (cumulative counter on the tag)
  - Sensor status flags
  - Packet timestamp

### Step 4: Tag Latest Snapshots

Derives per-tag snapshot from the latest packet:
- One row per unique tag (20 total)
- Fields: latest RSSI, battery state, motion count, temperature, signal quality
- `mapping_state` initialized to `unmapped` (updated in Step 7)
- `movement_state` initialized to `stationary`

### Step 5: Activity Windows

Creates 60-second time buckets per tag:
- Groups packets by tag and minute/bucket
- Computes: motion delta, packet count, avg/min/max RSSI, first/last seen times
- 200 total windows (20 tags × ~10 buckets each)

### Step 6: Tag-to-Animal Mapping

Maps 19 tags to live goats in the Castro shed:
- **Shed**: Castro (ID `62241795-628e-58ef-9591-aa384fb0f0f7`)
- **Mapped tags**: All except A0003B (19 total)
- **Mapping mechanism**:
  - Creates `goat_identifiers` rows with:
    - `identifier_type = 'animal_identifier_1'`
    - `normalized_value = lower(tag_id)` (e.g., `a0002a`)
    - `smart_tag_capable = true`
    - `status = 'active'`
  - Goat identifiers are paired with animals by creation order

- **Unmapped Tag**: A0003B (intentionally excluded)
  - Remains in `herd_signal_tag_latest` with `mapping_state='unmapped'`
  - No corresponding `goat_identifiers` row
  - Exercises "unmapped tag detected" UI state

### Step 7: Verification Queries

The script runs 9 verification checks:
1. Total packet count loaded
2. Distinct tags in packet table
3. Gateway registration details
4. Sample tag latest snapshots
5. Activity window bucket count and distribution
6. Mapped tags with goat associations
7. The unmapped tag (A0003B)
8. Tag-to-goat lookup resolution (both mapped and unmapped)

## Verifying the Seed

After running the script, verify the data is present:

### Count packets
```sql
SELECT COUNT(*) FROM herd_signal_packets WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
-- Expected: ~12,203
```

### List all tags
```sql
SELECT DISTINCT tag_id FROM herd_signal_tag_latest WHERE tenant_id = '00000000-0000-4000-8000-000000000001' ORDER BY tag_id;
-- Expected: 20 tags (A0002A through A00041, excluding A00032, A00037, A00039)
```

### Verify mapping state
```sql
SELECT mapping_state, COUNT(*) as count
FROM herd_signal_tag_latest
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
GROUP BY mapping_state;
-- Expected:
--   mapped    | 19
--   unmapped  | 1
```

### Tag-to-goat resolution query
This query mimics the backend mapping lookup:

```sql
SELECT
  tl.tag_id,
  tl.tag_mac,
  COALESCE(gi.goat_id::text, 'UNMAPPED') as goat_id,
  COALESCE(g.display_id, 'N/A') as goat_tag,
  tl.mapping_state
FROM herd_signal_tag_latest tl
  LEFT JOIN goat_identifiers gi ON (
    tl.tenant_id = gi.tenant_id
    AND lower(tl.tag_id) = lower(gi.normalized_value)
    AND gi.status = 'active'
    AND gi.smart_tag_capable = true
  )
  LEFT JOIN goats g ON gi.goat_id = g.goat_id
WHERE tl.tenant_id = '00000000-0000-4000-8000-000000000001'
ORDER BY tl.tag_id;
```

Expected output shows:
- 19 rows with goat IDs (e.g., G-003492, G-003500, etc.)
- 1 row (A0003B) with `UNMAPPED` as goat_id

## Re-running the Seed

The seed script is **idempotent**: it can be safely re-run multiple times.

- Packet data uses `INSERT ... ON CONFLICT DO NOTHING` to avoid duplicates
- Gateway and tag_latest use `ON CONFLICT ... DO UPDATE` to refresh snapshot
- Activity windows use `ON CONFLICT DO NOTHING` (immutable historical buckets)
- Tag-to-animal mappings use `ON CONFLICT DO NOTHING` on normalized value

To refresh the seed (clear and reload):

```bash
source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
psql "$DATABASE_URL" << 'EOF'
DELETE FROM herd_signal_activity_windows WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
DELETE FROM herd_signal_tag_latest WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
DELETE FROM herd_signal_packets WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
DELETE FROM herd_signal_gateways WHERE tenant_id = '00000000-0000-4000-8000-000000000001';
DELETE FROM goat_identifiers
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
    AND source_system = 'herd-signals-oci-seed';
EOF

bash tools/local/seed-herd-signals-oci.sh
```

## Shed and Animal Details

### Castro Shed

- **Location ID**: `62241795-628e-58ef-9591-aa384fb0f0f7`
- **Active goats**: 63
- **Mapped goats (this seed)**: 19 (first 19 by creation date)

**Why Castro?** A single representative shed with adequate active animals for testing. All tags are mapped to the same shed to exercise single-location UI flows without cross-shed complexity.

### Mapped Animals (Sample)

| Tag ID | Goat Display ID | Goat UUID |
|--------|-----------------|-----------|
| A0002A | G-003492 | 18d80581-... |
| A0002B | G-003500 | 4f7987cc-... |
| A0003B | (UNMAPPED) | N/A |
| ... | ... | ... |

Full list available in seed script verification output (query #7).

## Data Location and Refresh

### Source Data

- **CSV packets**: `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv` (11,144 rows)
- **Raw NDJSON**: `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/raw_scan_reports.ndjson`
- **Capture date**: August 22, 2026, ~3 minutes of real gateway reception

These files are snapshots of live HoneyComm gateway captures and can be refreshed with new capture sessions if needed.

### Database Location

- **Host**: OCI Compute VM (Oracle A1 Flex)
- **Instance**: `goatos-remote-dev-a1-4x24`
- **Public IP**: `137.23.51.115` (SSH tunnel via Tailscale)
- **Local tunnel**: `127.0.0.1:15432`
- **Database**: `goatos`
- **Migrations applied**: 000190 (smart_tag_capable column) + 000191 (herd_signal_* tables)

## Troubleshooting

### "SAFETY: Suspected non-OCI connection"

The database connection is from an unexpected IP. Allowed sources:
- `127.0.0.%` (localhost)
- `10.88.0.%` (Tailscale)

If connecting from elsewhere, either:
1. Use the SSH tunnel: `/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel`
2. Or update the safety check in `tools/local/seed-herd-signals-oci.sql` if your network changes

### "SSH tunnel to OCI database is not running"

Start the tunnel:
```bash
/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
```

Keep it running in a separate terminal while seeding.

### "Cannot connect to database"

Verify:
1. Tunnel is running
2. PostgreSQL is up on the VM: `ssh opc@137.23.51.115 'docker ps | grep postgres'`
3. Network connectivity: `nc -z 127.0.0.1 15432`

### "CSV file not found"

Ensure you're running the script from the worktree root, or update paths in `seed-herd-signals-oci.sql` to absolute paths.

### "Column does not exist" / schema mismatch

Verify the OCI database has migrations 000190 and 000191 applied:

```bash
source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
psql "$DATABASE_URL" -c "
  SELECT table_name FROM information_schema.tables
  WHERE table_schema='public' AND table_name LIKE 'herd_signal%'
  ORDER BY table_name;
"
```

Expected output (4 tables):
- herd_signal_activity_windows
- herd_signal_gateways
- herd_signal_packets
- herd_signal_tag_latest

If missing, apply migrations manually or from the goatos repo:
```bash
cd /Users/ravi/goatos-work/herd-signals
# (build and run migrate tool with DATABASE_URL set)
```

## Notes

- **BLE telemetry only**: Sensor readings are from the ear tag hardware, not goat vital signs. Tag temperature is the tag's internal temperature, not body temperature.
- **Bench unit data**: The 20 tags were captured while powered up on a workbench, not attached to live animals. The seed assigns them to animals for testing purposes only.
- **Immutable packets**: Once a packet is loaded, it is never updated or deleted. Activity windows are derived from packets and also immutable per bucket.
- **Snapshot updates**: Tag latest snapshots ARE updated on re-runs to reflect current packet data (via `ON CONFLICT ... DO UPDATE`).
- **Test-data marker**: All seed rows have `source_system='herd-signals-oci-seed'` for easy identification and cleanup.

## Related Documentation

- Backend API design: `backend/internal/herdsignals/`
- Frontend UI implementation: `apps/admin-web/src/features/herd-signals/`
- Migration schemas: `backend/migrations/postgres/000190_*.sql`, `backend/migrations/postgres/000191_*.sql`
