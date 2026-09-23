# Guard weakening ledger

Every change that makes a guard protect **less** is recorded here, in the same
diff that makes it. Tightening a guard, shrinking a ratchet baseline, or deleting
an exemption needs nothing — only loosening is loud.

Enforced by `tools/agent-hooks/check-guard-weakening.mjs`
(`make guard-weakening-guard`), which is diff-scoped and runs in `run_common`.

## Why

Measured on 2026-09-23 by planting the violation each guard exists to catch and
running it. Five ways to switch a guard off while CI stayed green:

| what an agent does | what CI said before this ledger existed |
| --- | --- |
| delete the guard script, its manifest entry, its Makefile target and its CI step | `guardrail-registration guard: 140 guards registered ... all registered`. All 136 surviving guards were then run against that tree — not one noticed. |
| flip `requiredInCI: true` → `false` and drop the `step` line | green |
| replace a real `selfTest` with `selfTestExemptReason: "n/a"` | green |
| add new debt, then `ratchet-guard.py --regenerate` | green on the next run; the warning was printed once, in the author's own terminal |
| add `<guard>:ignore: owner=x issue=none scope=whatever expiry=2020-01-01` next to a real violation | green — 10 of the 11 guards that accept such a marker validate its format only, never the date |

## How to record one

Add one line per finding. The finding id is printed by the guard when it blocks:

```
GUARD-WEAKENING-ACK: <finding id> owner=<name> reason=<one line, why this is right>
```

An ACK clears only the finding whose id it names. There is no blanket waiver, and
the ACK is an added line in the diff, so it is reviewed with the change.

## What counts as weakening

- a guard script deleted, or renamed out of `tools/{agent-hooks,ci}/check-*`
- a guard self-test harness (`*.test.sh`) deleted
- a guard removed from `tools/ci/guardrail-manifest.json`
- `requiredInCI` downgraded from true
- `selfTest` removed (including when it is replaced by `selfTestExemptReason`)
- `ciStep` removed
- a ratchet baseline that admits more debt than before — a new `file|kind` key, a
  grown count, or an appended line in a line-shaped allowlist
- a baseline or allowlist file deleted
- a named numeric threshold in a guard script moved the looser way: a ceiling
  (`MAX_*`, `*_LIMIT`, `*_BUDGET`) raised, or a floor (`MIN_*`, `*_FLOOR`) lowered
- a `step "..."` invocation removed from `tools/ci/run-local-ci.sh`, or a
  `-guard` target no longer invoked from the `Makefile`
- a new guard-silencing marker (`<guard>:ignore:`, `exception:exempt`,
  `telemetry:exempt`) added to product source. Generic lint silencers are not
  listed: they switch off no guard here, and flagging them would fire on correct
  code.
- `tools/ci/guard-inputs.json` losing declared inputs

## Entries

<!-- Newest first. One line per ACKed finding, with the date. -->

_No weakenings recorded yet._
