# Goat OS

Goat OS is the operating system for goat identity, SOP work, vaccination,
health, breeding, genetics, workforce operations, proof verification, devices,
analytics, and controlled AI assistance.

Plain English:

```text
Every goat gets a passport.
Every job gets assigned to the right person.
Every important action gets proof.
Every risky proof gets verified.
Every official fact goes into one trusted backend.
Dashboards show the truth without reading Sheets or Slack.
```

## Where To Start

- Product phases: `context/product/goat-os-feature-phases.md`
- Phase PRD/TRD drafts: `docs/phases/`
- Two-developer build plan: `context/execution/two-dev-build-plan.md`
- Final architecture: `context/architecture/final-architecture.md`
- Forms/SOP engine: `context/forms/final-forms-sop-engine.md`
- Analytics/infra: `context/analytics/final-analytics-infra.md`
- Agent rules: `AGENTS.md`

The first working loop is:

```text
goat passport
-> vaccination SOP task
-> Android operator proof
-> verifier approval
-> goat timeline update
-> dashboard compliance
```

Existing live dashboard repos remain untouched. Their UI snapshots live in:

```text
apps/admin-web/
apps/investor-web-shadow/
```

Those copies are the only dashboard code to rewire.
