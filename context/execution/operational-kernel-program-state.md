# Operational Kernel Program State

Status: foundation only; implementation integration PR not created.

This is the persistent resume point for the whole-ledger and operational-task-
kernel program. Coordinators update it only on the single integration branch.
Worker and review agents never edit it.

## Authority and topology

- Repository: `vgoats/goatos`
- External topology: exactly one draft integration PR against `main`
- Internal topology: isolated implementation branches and read-only judges;
  reviewed commits flow into the one integration branch
- Review topology: every root batch receives an independent read-only
  `gpt-5.6-sol` `xhigh` judge (or stronger successor) plus cumulative
  integration-diff review; the builder never self-approves
- Ordinary/direct-main landing is forbidden for program implementation
- Final landing: `make land-integration-pr PR=<number>` after F0 builds and
  proves that target

## Foundation revalidation

- Fresh-main architecture/landing review: `d64e38790f010cd05b1c4f2df833a81024dce02c`
- Migration tail at that review: `000141`
- Stable ledger: 137 IDs; 135 open, 2 closed
- `WEIGH-001..006`: all six remain open; migration `000141` closes none
- Unrelated open GitHub PRs or future main movement are not program input until
  their commits actually land on `main`

## Start gate

The first coordinator fetches fresh `origin/main`, records the new base below,
re-adjudicates the selected IDs and migration tail, creates the sole draft PR,
and begins F0. No later batch can claim closure until F0 supplies machine-
readable proof/review receipts, honest guard registration, closed CI bypasses,
and the exact-head integration-PR landing gate.

## Live checkpoint

- Program base SHA: not started
- Integration branch: not started
- Integration head SHA: not started
- Draft PR: not started
- Latest main-sync merge: not started
- Accepted batches/commit ranges: none
- Migration reservations: none
- Next ready batch: F0 foundation enforcement
- Parallel candidates after F0 dependency review: N0, S0, selected disjoint D0
  and M0 source corrections
- Blocked-with-evidence rows: none recorded at foundation time
- Latest gate receipts: none
- Latest reconciled tally: 135 open / 2 closed

## Update law

After each accepted root batch, record its stable IDs, commit range, proof packet,
independent-review disposition, migration use, rollback class, changed
dependencies, new integration head, and next-ready batches. After merging fresh
main, record the merge SHA and conflict decisions, rerun affected earlier gates,
and refresh cumulative review. Never reconstruct this state from chat memory.

If a genuine medical, security, tenant-isolation, ownership, or external-
authority blocker remains, keep only that adapter/cutover fail-closed, record the
evidence here, and continue every independent ready batch.
