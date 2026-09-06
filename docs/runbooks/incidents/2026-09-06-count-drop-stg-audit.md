# 2026-09-06 Counts Drop STG Audit

## Summary

Manju reported that the live herd count looked wrong because he expected only
one death and no birth since the prior count. The count was correct on the real
`goatos-stg` Cloud SQL database. The missing explanation was six animals marked
as sold before the one death.

## Required Data Source

For this class of incident, `goatos-stg` means the real staging/production-facing
database, not the OCI testing clone and not local Docker Postgres.

Use:

```text
Project: goatos-stg
Cloud SQL: goatos-stg:asia-south1:goatos-stg-core-db
Database: goatos
Tenant: 00000000-0000-4000-8000-000000000001
```

If `gcloud`, ADC, Secret Manager, or Cloud SQL Auth Proxy auth has expired, use
browser reauthentication first. Do not silently substitute OCI or a service
account unless Ravi explicitly asks for that fallback.

## Result

Audit window:

```text
Start: 2026-09-03 00:00:00 IST
End:   2026-09-07 00:00:00 IST
```

Counts Breakdown defaults to `goats.lifecycle_status = 'alive'` and excludes
merged goats.

```text
Current live count:                 1611
Live animals created in window:       +2
Animals exited in window:             -7
Derived live count at Sep 3 start:   1616
Net change:                            -5
```

This reconciles exactly:

```text
1616 + 2 - 7 = 1611
```

## What Happened

Approved count requests in the window:

```text
death:    1 approved
shifting: 6 approved
birth:    0 approved
```

Identity events in the window included:

```text
goat.created: 2
goat.exited:  7
```

The seven exits were:

```text
6 sold on 2026-09-04 19:43:55 IST
1 died on 2026-09-05 14:58:23 IST
```

Sold animals:

```text
G-002780 RFID 901007000504507, tag2 CJB-657
G-002781 RFID 901007000504504, tag2 CJB-637
G-002784 RFID 901007000504455, tag2 CJB-692
G-002827 RFID 901007000504237, tag2 901007000505228
G-002853 RFID 901007000504303, tag2 CBE-1594
G-002862 RFID 901007000504256, tag2 901007000506005
```

Death:

```text
G-003664 RFID TEMP-CBE-CASTRO3-067, died on 2026-09-05 14:58:23 IST
```

Created live animals:

```text
G-005344 RFID CJB-1931, created on 2026-09-03 17:35:09 IST
G-005345 RFID 901007000506029, created on 2026-09-03 17:38:38 IST
```

## Operator Lesson

Do not answer a count discrepancy from the dashboard chart alone. Reconcile the
live count from the real `goatos-stg` database using created/exited rows and
then inspect `goat_identity_events` and `counts_approval_requests`. The graph is
monthly context; it is not the incident proof.

Do not call `goats.display_id` an RFID. RFID/tag answers must use
`goat_identifiers.identifier_value` for `animal_identifier_1` and
`animal_identifier_2`.
