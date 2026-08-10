# Partition Operational Location STG Handoff

Date: 2026-08-10
Branch: `feature/partition-operational-location-mapping`

## Rollback Anchor

Before deploying the partition operational-location migration to STG, a manual
Cloud SQL backup was created for `goatos-stg-core-db`.

```text
Project:     goatos-stg
Instance:    goatos-stg-core-db
Backup ID:   1786348300210
Status:      SUCCESSFUL
Type:        ON_DEMAND
Started:     2026-08-10T07:51:40.210Z
Ended:       2026-08-10T07:53:01.317Z
Description: pre-partition-oploc-42ae40f874ce-20260810T075135Z
```

If STG migration produces incorrect live goat placement, restore from this
backup or clone it for forensic comparison before attempting manual repair.

## Migration Scope

This PR adds the durable partition-to-location bridge:

```text
shed_partitions.operational_location_id
```

It creates/reuses active `location_type='pen'` children under their parent shed,
maps active `shed_partitions` rows to those pens, and updates live partitioned
goats so:

```text
goats.current_location_id = exact pen location
goats.shed_id             = physical parent shed rollup
goats.park_id             = parent park rollup
```

Undivided/lumpsum sheds remain unchanged:

```text
goats.current_location_id = shed location
goats.shed_id             = same shed location
```

Terminal history, accepted proofs, submitted observations, and immutable event
payloads are not rewritten by this migration.
