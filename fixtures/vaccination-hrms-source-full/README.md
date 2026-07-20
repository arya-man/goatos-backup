# Vaccination + HRMS source fixture

This directory is the canonical, deterministic local/dev source bundle for the
full vaccination seed. It contains 1,311 synthetic animal identities, their
sanitized vaccination history, 116 shed ownership rows, and a synthetic HRMS
roster with complete Preventive Care Manager, Backup Manager, and Park Head
coverage for CBE and CPT.

The private source is never committed. The generator removes payroll, salary,
bank, IFSC, tax, advance, DOJ, and payment data; replaces staff and animal
identities; preserves only fields consumed by the seed or needed to validate
relationships; and records every deterministic correction in
`corrections.json`. The fixture contains no email or phone because the reviewed
source files contain no such columns and the current `workforce_members` model
does not store them.

Run the DB-free contract before any reset or seed:

```bash
make vaccination-hrms-source-audit \
  SOURCE=fixtures/vaccination-hrms-source-full
make vaccination-hrms-seed-fixture-guard
```

Run the canonical clean-slate seed only through:

```bash
make seed-vaccination-source-full
```

The seed contract is deliberately fail-closed:

- All 13,110 vaccination cells are immutable source truth. The fixture keeps all
  3,836 dated administrations, 6,383 `Pending` cells, and 2,891 `NA`/blank
  cells exactly as supplied; no correction creates a new `NA`, `Pending`, or
  vaccination date.
- Trusted DOB determines kid/adult stage as of `manifest.json:data_as_of`.
  Valid K1/K2 distinctions remain; only K-tagged animals past the configured
  20-completed-week finish cutoff become Adult. A missing DOB stays null and
  retains the reviewed source stage fallback.
- Contradictory mock DOB, breed/species, death/sale, copied vaccination-row
  metadata, and maternal links are repaired around vaccination truth and counted
  in `corrections.json`. Goat/sheep-specific vaccine conflict is a hard stop.
- Goat/vaccination identities are one-to-one and every mother reference left in
  the fixture resolves to another fixture animal.
- Every source shed has a reviewed manager and backup, and both centers have PC
  Manager, Backup Manager, and Park Head seats.
- Vaccination proof is one live in-app-camera clip per goat (maximum five); a
  clip may cover same-handling vaccines for that goat only. Shed completion is
  acknowledgement, never a shed/drive proof video.

Intentional contract changes must regenerate the data/hashes and update the
source audit, strict validator, adversarial self-tests, source-data and
source-date runbooks, anti-patterns, and `goatos-build` skill in the same change.
See `docs/runbooks/source-seed-data-validation.md` for the full intake contract.

To regenerate from the maintainer-only private source:

```bash
node tools/dev/build-vaccination-hrms-fixture.mjs \
  --source /absolute/path/to/private/vgoats-seed \
  --out fixtures/vaccination-hrms-source-full
```
