# Migration 000021 historical SOP-version audit (F2 residual risk)

## Status

**Open residual risk — audit required per environment before closure.**

## Context

Migration `000021_vaccination_shed_level_video_sop.sql` rewrites `form_dsl` and
`proof_policy` for the `vaccination.drive` / `vaccination.session` SOP
definitions (flipping the published default to shed-level video proof). As
shipped, its `UPDATE` was **not scoped to `status = 'published'`**, so it rewrote
**every** matching `sop_versions` row — including any `draft`/`retired`/
superseded version — mutating published-version history in place.

We deliberately did **not** patch `000021` in place:

- goose will not re-run an already-applied migration, so editing the file cannot
  repair an environment where it has already run.
- On a fresh apply the vaccination SOP has a single `published` version, so the
  unscoped `UPDATE` only touches that one version — the multi-version mutation
  only bites an environment that already held historical versions when `000021`
  ran.

The forward rule is: **a SOP default change must publish a NEW version, never
rewrite existing versions in place.**

## Required audit (run per real environment: local, stg, prod)

Determine whether any non-published vaccination SOP version was mutated by
`000021` (identified by carrying the shed-level proof mode on a non-published
row). Use the read-only Cloud SQL access path in
`docs/runbooks/google-cloud-environments.md`:

```sql
SELECT sv.tenant_id,
       sd.code,
       sv.status,
       count(*) AS affected_versions
FROM sop_versions sv
JOIN sop_definitions sd
  ON sd.tenant_id = sv.tenant_id
 AND sd.sop_id = sv.sop_id
WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
  AND sv.status <> 'published'
  AND sv.proof_policy ->> 'proof_mode' = 'shed_level_video'
GROUP BY sv.tenant_id, sd.code, sv.status
ORDER BY sv.tenant_id, sd.code, sv.status;
```

## Decision by result

- **Zero rows** in every environment → no historical version was mutated; this
  risk is closed for that environment. Record the run (env, date, `0 rows`).
- **One or more rows** → those non-published versions were rewritten in place and
  their original `form_dsl`/`proof_policy` are not recoverable from the row.
  Escalate to the maintainer: decide per version whether to leave it (retired /
  never re-published) or republish a corrected version. Add a forward
  repair/decision migration only if a mutated historical version can still be
  activated.

## Audit log

| Environment | Date (IST) | Affected versions | Action |
|-------------|-----------|-------------------|--------|
| _pending_   | _pending_ | _pending_         | _pending_ |
