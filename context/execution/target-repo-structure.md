# Goat OS Target Repo Structure

The current `<mesha-workspace>` folder is a workspace: cloned repos, source docs, context docs, and reference code. It is not the final Goat OS repo layout.

This document defines the fresh repo structure to create and where the architecture/context/docs should live.

## Final Decision

Create one main build repo:

```text
<mesha-workspace>/goatos/
```

`goatos/` becomes the canonical product repo for:

- backend
- admin/dashboard web
- operator Android app
- contracts
- infra
- analytics definitions
- load tests
- agent context
- architecture docs

The current `context/` should move into:

```text
<mesha-workspace>/goatos/context/
```

After that move, do not keep two active `context/` folders.

## What Stays Outside Goatos

These existing repos stay outside as reference/source material until intentionally migrated:

```text
<mesha-workspace>/dashboard/
  current live CEO/admin dashboard source
  do not edit for Goat OS rewiring
  take a fresh snapshot/clone into goatos/apps/admin-web and change only that copy

<mesha-workspace>/vgoats-dashboard/
  current live investor/reduced dashboard source
  do not edit for Goat OS rewiring
  take a fresh snapshot/clone into goatos/apps/investor-web-shadow for validation

<mesha-workspace>/procurement_app/
  reference mobile app
  copy camera/upload/team ideas into goatos/apps/operator-mobile

<mesha-workspace>/slack-automation-scripts/
  reference legacy Slack/SOP/Sheets workflows
  use for migration discovery only
  do not extend canonical workflows here

<mesha-workspace>/website/
  out of scope for Goat OS core
  business-context reference only
```

## Target Goatos Repo

```text
goatos/
  AGENTS.md
  CLAUDE.md
  SKILLS.md
  README.md
  Makefile
  .gitignore
  .agents/
    skills/
      goatos-build/
        SKILL.md
        references/
  .agent/
    scope.json
  .claude/
    settings.json
    skills/
      goatos-build -> ../../.agents/skills/goatos-build
  .github/
    workflows/
      ci.yml
      deploy-dev.yml
      deploy-stg.yml
      deploy-prod.yml

  context/
    README.md
    architecture/
      final-architecture.md
      final-frontend-mobile-backend-architecture.md
    forms/
      final-forms-sop-engine.md
    analytics/
      final-analytics-infra.md
    agents/
      ai-agent-context-and-protocols.md
      skills/
        README.md
        goatos-build/
          SKILL.md
          references/
            repo-structure.md
            architecture.md
            forms-sop.md
            contracts-events.md
            frontend-mobile.md
            analytics-infra.md
            execution-plan.md
            existing-repos.md
            security-ops.md
    execution/
      two-dev-build-plan.md
      env-load-test-and-doc-hygiene.md
      next-contracts.md
      target-repo-structure.md
    # no archive/ under context; stale planning history stays outside goatos

  contracts/
    openapi/
      app-api.yaml
      analytics-api.yaml
      admin-api.yaml
      public-api.yaml
    jsonschema/
      form-dsl.schema.json
      form-submission.schema.json
      domain-event-envelope.schema.json
      decision-record.schema.json
      dlq-repair.schema.json
    proto/
      telemetry/
        device_observation.proto
    examples/
      forms/
      submissions/
      events/

  backend/
    go.mod
    cmd/
      goatos-api/
      outbox-relay/
      sweeper/
      device-gateway/
    internal/
      identity/
      locations/
      permissions/
      workforce/
      tasks/
      sop/
      forms/
      media/
      verification/
      vaccination/
      health/
      breeding/
      genetics/
      growth/
      feed/
      devices/
      analytics_export/
      legacy_import/
    platform/
      auth/
      config/
      db/
      logging/
      observability/
      outbox/
      pubsub/
      scheduler/
      secrets/
      storage/
      notifications/
    migrations/
      postgres/
    pkg/
      apierrors/
      idempotency/
      timeutil/
    tests/
      contract/
      integration/

  apps/
    admin-web/
      # initialized from a fresh dashboard snapshot/clone; old dashboard repo untouched
      app/
      components/
      features/
      lib/
      tests/
    investor-web-shadow/
      # temporary validation copy initialized from vgoats-dashboard; old repo untouched
      app/
      components/
      features/
      lib/
      tests/
    operator-mobile/
      android/
      ios/
      src/
        app/
        features/
          tasks/
          forms/
          media/
          goats/
          sync/
          devices/
          profile/
        shared/
          api/
          ui/
          storage/
          config/

  packages/
    api-client/
    analytics-client/
    auth-client/
    forms-dsl/
    mobile-forms-runner/
    media-client/
    device-client/
    rbac/
    ui/
    config/

  analytics/
    dbt/
      models/
      tests/
    cube/
      model/
    tinybird/
      datasources/
      pipes/
    metabase/
      README.md

  infra/
    envs/
      dev/
      stg/
      prod/
    modules/
      cloud-run/
      cloud-sql/
      pubsub/
      gcs/
      bigquery/
      iam/
      secret-manager/
    scripts/
      bootstrap-dev.sh
      bootstrap-stg.sh
      bootstrap-prod.sh

  load-tests/
    k6/
      operator-sync.js
      vaccination-campaign.js
      media-proof.js
      idempotency-storm.js
      outbox-relay.js
      dashboard-analytics.js
      device-telemetry.js
      sweeper.js
    data/
      synthetic/
    reports/

  tools/
    agent-hooks/
      check-write-scope.sh
      check-boundaries.sh
      check-contract-drift.sh
      format-touched.sh
    generators/
      synthetic-data/
      openapi-client/
      jsonschema-fixtures/
    migration/
      xlsx-sample-import/
      slack-form-discovery/

  docs/
    runbooks/
      rotate-slack-tokens.md
      gate-dashboards.md
      restore-postgres.md
      replay-outbox.md
      dlq-repair.md
    decisions/
      README.md
```

Historical planning files stay outside this repo:

```text
docs/archive/planning-history/
```

Agents should not read that folder unless a human explicitly asks for
historical comparison.

## Why One Main Repo

Use one main repo because:

- two developers need shared contracts and generated clients
- backend, dashboard, mobile, forms, analytics, and load tests must evolve together
- context should live beside code
- CI can validate contracts, migrations, backend, frontend, and mobile together
- modular monolith discipline does not require many repos

This is not microservices and not microfrontends. It is one product repo with strict internal modules.

## What Each Top-Level Folder Owns

```text
context/
  source of truth for architecture and AI-agent context

contracts/
  OpenAPI, JSON Schema, proto, examples
  generates clients and validates payloads

backend/
  Go modular monolith and worker binaries
  owns canonical Postgres writes

apps/admin-web/
  role-aware CEO/admin/investor dashboard and command center

apps/operator-mobile/
  Android field operator app
  task-first, offline-capable, form DSL runner

packages/
  shared TypeScript packages and generated clients

analytics/
  dbt transforms/tests, Cube semantic model, Tinybird pipes

infra/
  dev/stg/prod environment definitions
  IAM, Cloud Run, Cloud SQL, Pub/Sub, GCS, BigQuery, Secret Manager

load-tests/
  k6 scenarios, synthetic data, reports

tools/
  generators and migration utilities

docs/
  operational runbooks and decision logs
```

## Agent Skill Bundle Rule

`.agents/skills/goatos-build/` is the canonical source for the committed Goat OS
agent skill bundle inside the build repo.

`.claude/skills/goatos-build` is a symlink or generated copy pointing to the
same skill. Do not hand-maintain two skill copies.

`SKILL.md` owns the reference index. `references/*.md` owns scoped playbooks and
links back to canonical context docs. Do not duplicate full architecture facts
inside root `CLAUDE.md`, root `AGENTS.md`, or per-module agent files.

`.agent/scope.json` declares task write scope. `tools/agent-hooks/*` is the
shared enforcement layer used by Claude hooks, Codex workflows, CI, and humans.
Hooks are convenience; CI is the hard gate.

## Agent Files In Goatos

Every major area gets an `AGENTS.md`. `CLAUDE.md` points to `AGENTS.md`.

```text
goatos/AGENTS.md
goatos/CLAUDE.md

goatos/backend/AGENTS.md
goatos/backend/internal/identity/AGENTS.md
goatos/backend/internal/tasks/AGENTS.md
goatos/backend/internal/forms/AGENTS.md
goatos/backend/internal/verification/AGENTS.md
goatos/backend/internal/vaccination/AGENTS.md

goatos/apps/admin-web/AGENTS.md
goatos/apps/operator-mobile/AGENTS.md

goatos/contracts/AGENTS.md
goatos/analytics/AGENTS.md
goatos/infra/AGENTS.md
goatos/load-tests/AGENTS.md
```

These files should stay short and point back to `goatos/context/README.md`.

## How To Move From Current Workspace

Step 1: Initialize safe meta-repo for current docs.

```text
cd <mesha-workspace>
git init
git add .gitignore AGENTS.md CLAUDE.md context/ archive/README.md
git commit -m "Lock Goat OS architecture context"
```

Step 2: Create main repo.

```text
mkdir <mesha-workspace>/goatos
cd <mesha-workspace>/goatos
git init
```

Step 3: Move context.

```text
mv context <mesha-workspace>/goatos/context
```

Do not move `docs/archive/planning-history/` into `goatos/context/`.
It is superseded planning history and should not be part of normal agent reads.

Step 4: Update root pointers.

```text
<mesha-workspace>/AGENTS.md
  points to <mesha-workspace>/goatos/context/README.md
```

Step 5: Scaffold folders.

```text
contracts/
backend/
apps/admin-web/
apps/investor-web-shadow/
apps/operator-mobile/
packages/
analytics/
infra/
load-tests/
tools/
docs/
```

Step 6: Copy code deliberately.

```text
fresh dashboard snapshot/clone -> goatos/apps/admin-web
fresh vgoats-dashboard snapshot/clone -> goatos/apps/investor-web-shadow
procurement camera/upload ideas -> goatos/apps/operator-mobile
slack workflow knowledge -> goatos/tools/migration/slack-form-discovery
```

Do not edit the current live dashboard repos. Do not import other old repos
wholesale.

## What Not To Do

- Do not keep building inside `context` forever.
- Do not keep two active context folders.
- Do not absorb nested Git repos by accident.
- Do not treat `website/` as Goat OS core.
- Do not turn current `dashboard/` and `vgoats-dashboard/` into permanent separate products.
- Do not keep direct BigQuery/Firestore/GCS access in app code.
- Do not start with microservices or microfrontends.

## Immediate Start

Create `goatos/`, move `context/`, then create:

```text
contracts/jsonschema/domain-event-envelope.schema.json
contracts/jsonschema/decision-record.schema.json
contracts/jsonschema/form-dsl.schema.json
contracts/openapi/app-api.yaml
backend/
apps/admin-web/
apps/operator-mobile/
```

The first end-to-end build remains:

```text
vaccination task -> Android form -> proof upload -> server validation -> verification -> typed event -> outbox -> dashboard
```
