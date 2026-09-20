# PostgreSQL bind-contract audit — 2026-09-21

## Scope and method

This is a read-only audit of the current backend and the previous month of Git history. It covers pgx `Query`, `QueryRow`, `Exec`, and `Batch.Queue` calls, with particular attention to generated/pruned SQL, variadic argument slices, and pgx execution-mode options. It does not claim every listed risk is a current defect; the inventory identifies code shapes where SQL and argument construction can drift and therefore need the shared guard or executable PostgreSQL coverage.

Current-tree reconnaissance found approximately 5,545 method-call text matches before excluding unrelated methods, tests, and HTTP `URL.Query` calls. A focused construction scan found 27 Go files that build `[]any` argument lists, 19 files that construct SQL dynamically, 10 files in both groups, and 19 files containing `Batch.Queue` calls. Those counts are an audit aid, not a stable CI contract.

## Confirmed incidents in the last month

### 1. Weighing section pruning dropped the last placeholder

- Fix: `9102698cc2fb7b7c437611f4ab4a6f2c2d6218d3` (the equivalent branch-only fix was `3c5294a76`).
- Introducer: `856d6e6fb` added the weight-band parameter.
- Failure: the caller always supplied 31 values, but section pruning removed the only `$31` reference for four section shapes. pgx rejected those queries with `expected 30 arguments, got 31`.
- Current evidence: `backend/internal/weighing/adapters/postgres/weight_demographics.go:470-490` anchors all inputs in `_param_types`; the actual 31-value call is at `:1360-1372`.
- Escape route: the earlier unit guard tested pruning with toy SQL, while the real PostgreSQL test did not enumerate all section shapes. The first tactical guard also hard-coded `31`, so it described the present query instead of deriving the contract.

### 2. Feed workbook batch retained `$7` after the caller shrank to six values

- Fix: `dde3cc1f6c763e82816f4b3543a4b3a5421dabd2` (duplicate branch commit `cea6be0d2`).
- Failure: a queued `UPDATE` still used `$7` after `head_count` was removed and the `Batch.Queue` call supplied six values. Batch preprocessing failed with PostgreSQL `42P18`, aborting the insert alongside the update.
- Current evidence: `backend/cmd/seed-feed-ration/main.go:865-888` now uses `$6` and passes six values; the adjacent insert is another six-value queued statement at `:894-908`.
- Escape route: arithmetic and generator unit tests never executed the batch. The fix added an integration test that exercises insert, reconcile, no-op, and ownership cases against PostgreSQL.

### 3. Obligation clinical-conflict SQL received unused values

- Fix: `81bb247c98fe7409e2663b877dbd4c32a299ddff` (duplicate branch commit `6141f77ab`).
- Failure: SQL referenced four parameters while the call supplied seven. PostgreSQL could not infer types for values with no placeholder, so every caller failed with `42P18` before the read ran.
- Additional lesson: the same change passed `pgx.QueryExecModeSimpleProtocol` to multi-statement test seeds. `QueryExecMode*` and `QueryResultFormats` values are pgx control arguments, not PostgreSQL bind values, and a guard must exclude them from the bind count.
- Current evidence: `backend/internal/obligation/adapters/postgres/drive_date_override_clinical_spacing.go:294-346`; simple-protocol examples are in the adjacent integration test.

### 4. Leadership Tasks bound a user that the tenant-wide branch did not read

- Fix: `81d3f7163c61a90c6d8615cb44d97a30961ef88c`.
- Failure: the tenant-wide `team_progress` shape used only `$1` but always received tenant and user. The unfiltered page failed with `expected 1 arguments, got 2`; a filtered shape masked the bug because a later placeholder happened to consume the extra slot.
- Current evidence: `backend/internal/leadershiptasks/adapters/postgres/repository.go:130-168` conditionally appends the user and derives all subsequent placeholders with `bindArg`; the paired batch calls are at `:178-182`.
- Escape route: the existing test discarded the returned error and did not execute the unfiltered production shape.

These four incidents cover both mismatch directions: SQL can require more values than the caller supplies, or callers can supply values that one generated query shape no longer references. They also show why “maximum placeholder equals argument count” is necessary but insufficient unless it is applied to the final generated SQL for every reachable shape.

## Contract the guard must enforce

For each final executable statement and its actual argument list:

1. Extract positional placeholders from PostgreSQL lexical tokens, ignoring single-quoted strings, escaped strings, quoted identifiers, dollar-quoted bodies, line comments, and block comments.
2. Require the used placeholder set to be contiguous from `$1` through `$N`; repeated placeholders are valid.
3. Require `N` to equal the number of bind values after pgx control arguments are removed.
4. Reject both missing values and unused/surplus values.
5. Validate each `Batch.Queue` statement independently; one malformed queued statement can prevent the whole batch from executing.
6. Validate the final SQL after optional filters, section pruning, concatenation, replacement, or builder expansion—not a copied fixture or a pre-transformation template.
7. Enumerate every meaningful branch of production builders. Tests must include the empty/unfiltered shape because that is where surplus arguments have repeatedly hidden.
8. Treat `pgx.QueryExecMode*` and `pgx.QueryResultFormats` as options, not binds. Preserve their order and semantics while counting only PostgreSQL values.
9. Resolve direct literals, constants, and simple composed values statically. Require an approved runtime `BoundQuery`/validator plus branch tests where a static check cannot prove a dynamically constructed query.

The guard's adversarial self-tests should include: missing final argument, surplus argument, a placeholder gap, repeated placeholders, conditional removal of the highest placeholder, a filtered shape that accidentally masks a surplus value, independently invalid batch entries, fake `$n` text in every quoted/comment form, and pgx control arguments before real binds.

## Current high-risk inventory

### Priority 0 — already failed in production or review

- `backend/internal/weighing/adapters/postgres/weight_demographics.go:1360-1372`: pruned SQL plus an unconditional long argument list. Replace the source-regex/hard-coded-31 tactical guard with validation of the production-built query and actual derived argument list for every section set.
- `backend/cmd/seed-feed-ration/main.go:865-908`: paired `Batch.Queue` statements with manually numbered values. Cover all remaining queue sites in this command, not only the repaired experiment reconcile statement.
- `backend/internal/obligation/adapters/postgres/drive_date_override_clinical_spacing.go:294-346`: keep executable coverage and use it as the surplus-argument regression case.
- `backend/internal/leadershiptasks/adapters/postgres/repository.go:130-182`: scope, filters, keysets, aggregate SQL, and two independent batch argument lists. Enumerate scope x empty/filter/cursor shapes and validate both batch entries.

### Priority 1 — dynamic SQL and dynamic argument lists

- Configuration writers: `backend/internal/configuration/adapters/postgres/import_store.go:280-302` and `:656-686`; `stores_reference.go:175-228`; plus `stores_animals.go`, `stores_catalogue.go`, and `stores_places.go`. These combine computed placeholder numbers, optional columns, and nested `append` expressions.
- Work lists: `backend/internal/penroutines/adapters/postgres/repository.go:225-236` and `backend/internal/penvisits/adapters/postgres/repository.go:177-188` build optional keyset predicates from argument length.
- Procurement: `backend/internal/procurement/adapters/postgres/feed_purchase_repository.go:84-122` and `vendor_repository.go:117-236` reuse dynamically built arguments across rows and totals queries.
- Sales: `backend/internal/sales/adapters/postgres/overview_repository.go` and `pipeline_repository.go:74-151,493-514` build filters used by multiple list/count projections.
- Toxin: `backend/internal/toxin/adapters/postgres/repository.go:173-197` conditionally adds status and cursor binds.
- Animal purchase: `backend/internal/animalpurchase/adapters/postgres/repository.go:615-653` derives optional filters and keyset placeholders from slice lengths.

### Priority 2 — batch-heavy surfaces

- Seeder/import commands: `backend/cmd/seed-feed-ration/main.go`, `seed-vaccination-real/main.go`, `seed-roster-real/main.go`, `seed-shed-positions/main.go`, `seed-position-duties/main.go`, `import-feed-purchases/main.go`, and `import-feed-external-consumption/main.go`.
- Product repositories: counts breakdown and mortality batches, health analytics and write batches, herd-signals batches, protocol rule-dimension batches, leadership-task hydration batches, and platform audit recording.
- Each queued statement needs an individual contract. A successful `SendBatch` call site inspection does not prove its heterogeneous entries have matching binds.

### Priority 3 — special pgx argument shapes and shared query prefixes

- Vaccination execution uses `pgx.QueryExecModeExec`, `QueryExecModeCacheStatement`, and `pgx.QueryResultFormats` before real values (for example `backend/internal/vaccinationexecution/adapters/postgres/execution_combined.go:208`).
- Process Integrity uses `append([]any{pgx.QueryExecModeExec}, args...)` and different row/count argument prefixes (`backend/internal/processintegrity/adapters/postgres/repository.go:283,326,368`). Its existing `TestQueryArgsShapeMatchesRowsAndCountQueries` at `repository_integration_test.go:853-880` is a useful local precedent, but it still compares against hard-coded argument-count constants. The shared validator should derive the invariant while retaining its deliberate rows-versus-counts prefix checks.
- Natural SQL (`backend/internal/ceoai/app/natural_sql.go`) is a separately constrained execution surface. Do not infer static safety from generated text; preserve its SQL guard and validate bind shape at its final execution boundary.

## Prioritized migration plan

1. Land the shared PostgreSQL-aware lexer/validator and adversarial self-tests first. Keep it independent of business packages.
2. Add a checked-in AST/source guard that proves direct literal/constant calls and blocks newly introduced unresolved dynamic calls unless they use the approved validated path. Establish a reviewed legacy baseline so enforcement is a ratchet and cannot grow.
3. Convert the four confirmed incident paths to the derived contract. Weighing must validate every production section variant; Feed must validate every repaired queue entry; Obligation must prove surplus args fail; Leadership Tasks must cover unfiltered and filtered scope variants.
4. Convert Priority 1 dynamic builders. Return SQL and arguments as one `BoundQuery` value so they cannot be edited independently; validate immediately before execution.
5. Convert Priority 2 batches, validating at queue construction rather than only after `SendBatch`.
6. Convert special pgx-option and shared-prefix surfaces with explicit tests proving control arguments are excluded and query-specific prefixes remain intentional.
7. Prefer `pgx.StrictNamedArgs` for suitable handwritten queries and sqlc for stable static queries. Keep positional binds where generated clauses genuinely benefit from them, but require the shared bound-query path.
8. Wire the static guard and its self-test into the unconditional backend/local-CI and `make land-main` path. Keep real PostgreSQL integration tests for dynamic branch execution; they are defense in depth, not the only guard.

## Review and skill requirements

Any review that changes handwritten PostgreSQL SQL or its arguments should classify the call as sqlc-generated, `pgx.StrictNamedArgs`, validated `BoundQuery`, or a narrow documented exemption with executable PostgreSQL coverage. Reviewers should require proof of the final query shape, including empty/unfiltered and batch variants, and should reject a primary invariant that hard-codes today's maximum placeholder. The same check must run again through the final landing receipt so a later rebase or edit cannot bypass it.
