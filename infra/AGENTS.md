# Infra Agent Context

Read first:

- `../context/execution/env-load-test-and-doc-hygiene.md`
- `../context/analytics/final-analytics-infra.md`

Purpose:

- dev/stg/prod infra definitions, IAM, secrets, deploy shape, and load-test
  environment support.

Do:

- Keep dev, stg, and prod isolated.
- Enforce prod read-only agent access through IAM.
- Run heavy load tests only in stg.

Do not:

- Do not share prod DBs, buckets, topics, datasets, or secrets with dev/stg.
- Do not rely on markdown promises for IAM.
