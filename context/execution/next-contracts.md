# Next Execution Artifacts

Status: authoritative next build work.

Architecture review is done. The next work is contracts and schema, then code.

## Artifacts To Draft

```text
1. Form DSL grammar
   fields
   rules
   data sources
   repeat_for_each_goat
   server-authoritative gates
   proof policies

2. AnalyticsEvent envelope
   bitemporal timestamps
   idempotency
   schema version
   proof refs
   verification state
   replay metadata

3. Decision record schema
   decision_type
   decision_state
   decided_by
   policy_version
   source evidence IDs
   model version
   confidence
   reviewer ID
   needs_review path

4. Operational schema
   identity
   locations
   workforce/roster
   workforce skills/scopes/shifts/absence
   backfill candidate selection and reassignment events
   tasks
   sop/form versions
   media
   verification
   vaccination
   inventory seed
   legacy import
   outbox

5. Native runner contract
   schema delivery
   offline cache
   draft state
   proof capture
   batch submit
   retry/idempotency

6. Submit transaction spec
   app-api request
   server validation
   domain event creation
   verification creation
   outbox emission

7. Workforce backfill contract
   skill match
   park/shed/cohort scope match
   current load and shift conflict checks
   candidate ordering
   reassignment event
   new-assignee notification
   park-head notification
   no-eligible-backup escalation
   due_at escalation for unmarked absence

8. One real vaccination SOP
   authored in Goat OS DSL
   rendered on Android
   submitted offline/online
   verified
   exported to analytics
```

## Non-Negotiables

```text
No app touches DB directly.
No arbitrary JS in form rules.
No AI queries raw Postgres.
No AI directly mutates canonical truth.
No AI proposal becomes approved without deterministic validation or human approval where required.
No dashboard scans raw BigQuery facts.
No raw telemetry writes canonical state.
No task generation without idempotency key.
No proof video through API proxy.
No staging shortcuts that break migration/load rehearsal.
```
