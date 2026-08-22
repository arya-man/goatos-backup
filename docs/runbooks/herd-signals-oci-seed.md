# Herd Signals OCI Dev Seed

Seeds the OCI dev PostgreSQL database with a real HoneyComm BLE gateway capture
so the Herd Signals feature can be exercised against live-shaped telemetry.

> This runbook lives under `docs/runbooks/` and matches `*seed*` deliberately:
> `tools/agent-hooks/check-seed-migration-coupling.mjs` only credits a seed
> companion at `docs/runbooks/*seed*`, `backend/cmd/seed-*`, a seed/projection
> test, or `tools/dev/seed-closeout.sh`. A runbook parked anywhere else is
> invisible to the guard, which is why `make guardrails` went red when this file
> sat at `docs/HERD_SIGNALS_OCI_SEED_RUNBOOK.md`.

## The rule this seed obeys

**SQL loads facts. Go replays them. Nothing hand-derives read-model state.**

| Table | Kind | Written by |
|---|---|---|
| `herd_signal_gateways` | external fact | stage 1 (SQL) |
| `herd_signal_packets` | external fact | stage 1 (SQL) |
| `goat_identifiers` (BLE tags) | external fact | stage 1 (SQL) |
| `herd_signal_tag_latest` | **derived** | stage 2 (real ingest service) |
| `herd_signal_activity_windows` | **derived** | stage 2 (real ingest service) |

The seed used to `INSERT` into the bottom two directly, re-deriving movement and
pattern state in SQL. That is a seeded stand-in for derived state, which
`AGENTS.md` forbids, and it drifted exactly as predicted: it invented a
`stationary` movement state that is not in the vocabulary at all, and it labelled
readings that were twelve hours old `not_moving` where the rule says `stale`. A
seed that computes the answer itself will always agree with itself and never with
the shipped classifier.

If you add a column to `herd_signal_tag_latest` or `herd_signal_activity_windows`,
the fix belongs in the ingest service, not here.

## Prerequisites

1. **SSH tunnel to the OCI VM** (the seed refuses to run without it):

   ```bash
   /Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel
   ```

   This listens on `127.0.0.1:15432`.

2. **Credentials.** Either export `DATABASE_URL` yourself, or leave it unset and
   let the wrapper source the local env file:

   ```bash
   source /Users/ravi/mesha/local-data/goatos-stg-to-oci/oci-goatos-db.env
   ```

   That file is machine-local and `chmod 600`. Never copy it into the repo.

3. **Migrations 000190, 000191 and 000192 applied.** 000192 creates
   `herd_signal_packets_dedup_uidx`; without it the seed's `ON CONFLICT DO
   NOTHING` has no unique index to conflict against and a re-run silently doubles
   the packet table. Check:

   ```bash
   psql "$DATABASE_URL" -qAt -c \
     "SELECT indexname FROM pg_indexes WHERE tablename='herd_signal_packets'"
   ```

   `herd_signal_packets_dedup_uidx` must be present.

4. **The capture CSV** at
   `/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv`.
   The gateway is still appending to this file. See "Freezing the capture" below.

## Running it

```bash
cd /Users/ravi/goatos-work/herd-signals
bash tools/local/seed-herd-signals-oci.sh
```

Flags:

| Flag | Effect |
|---|---|
| `--csv PATH` | use a specific capture file (use a frozen copy for anything reproducible) |
| `--facts-only` | stage 1 only — load gateway/packets/identifiers, compute nothing |
| `--replay-only` | stage 2 only — recompute the derived read models from the capture |

### Stage 1 — external facts (`tools/local/seed-herd-signals-oci.sql`)

Loads the gateway, the captured packets, and 19 tag-to-animal `goat_identifiers`
rows. Tag `A0003B` is deliberately left unmapped so the unmapped-tag path stays
exercised.

### Stage 2 — derived read models (`backend/cmd/seed-herd-signals-oci`)

Replays the same capture through `app.Service.IngestPackets`, so
`herd_signal_tag_latest` and `herd_signal_activity_windows` are produced by the
shipped classifier and the shipped bucketing — the same code the live gateway
endpoint runs.

## The refuse-to-run guard

`tools/local` seeds must never touch production or the shared local `:5433`
stack. The wrapper honours an explicitly exported `DATABASE_URL` (it does *not*
overwrite it from the env file — a guard that can never fire is not a guard) and
then refuses anything that is not the OCI tunnel:

```
$ DATABASE_URL='postgres://postgres:x@127.0.0.1:5433/goatos' \
    bash tools/local/seed-herd-signals-oci.sh
[seed-herd-signals-oci] target host=127.0.0.1 port=5433 db=goatos
[seed-herd-signals-oci] ERROR: REFUSING TO RUN: port '5433' is not the OCI tunnel
port (15432). Port 5433 is the shared local stack and 5432 is a direct/production
socket; this seed targets the OCI dev database only.
```

Non-loopback hosts and any database other than `goatos` are refused the same way.
A second, server-side guard inside the SQL re-checks `current_database()` and
`inet_client_addr()`.

## Freezing the capture (required for any reproducible run)

The gateway appends to `decoded_ear_tags.csv` continuously, so row counts move
between runs (11,144 → 13,134 → 13,276 → 14,460 were all observed within one
afternoon). Anything that compares two runs must use a frozen copy — on a
persistent path, never `/tmp`, which macOS purges:

```bash
mkdir -p /Users/ravi/goatos-work/herd-signals-proof
cp /Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv \
   /Users/ravi/goatos-work/herd-signals-proof/decoded_ear_tags.frozen.csv

bash tools/local/seed-herd-signals-oci.sh --facts-only \
  --csv /Users/ravi/goatos-work/herd-signals-proof/decoded_ear_tags.frozen.csv
```

Run it twice; the counts below must be identical both times.

## Verifying

```bash
psql "$DATABASE_URL" -c "
SELECT 'packets' AS fact, count(*)::text AS n FROM herd_signal_packets
UNION ALL SELECT 'distinct_tags',    count(DISTINCT tag_id)::text FROM herd_signal_packets
UNION ALL SELECT 'gateways',         count(*)::text FROM herd_signal_gateways
UNION ALL SELECT 'seed_identifiers', count(*)::text FROM goat_identifiers
         WHERE source_system = 'herd-signals-oci-seed'
UNION ALL SELECT 'packets_in_future', count(*)::text FROM herd_signal_packets
         WHERE received_at > now()
ORDER BY 1;"
```

`packets_in_future` must be `0`. A non-zero value means the capture was loaded
with the wrong timezone anchor — see below.

## Timezone contract

Do not "simplify" the offsets in the packets INSERT.

- `received_at` in the CSV is **Asia/Kolkata wall clock**, so it is anchored
  `'+05:30'`. Anchoring it `'+00'` — as an earlier version of this seed did —
  moves every packet 5h30m into the future and makes every staleness and
  movement classification wrong.
- `packet_time` is the **same instant** on the gateway's own misconfigured clock,
  which runs at UTC+08:00, so it is anchored `'+08:00'`. Verified on the capture:
  `2026-08-22T15:52:08+05:30` == `2026-08-22 18:22:08.183+08:00` ==
  `2026-08-22T10:22:08Z`.

## Rebuild registration (seed closeout)

`herd_signal_tag_latest` and `herd_signal_activity_windows` are projection tables,
so per `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` they must be
rebuildable from canonical data through the closeout, not only creatable by a
one-off script. `tools/dev/seed-closeout.sh` registers that rebuild:

```bash
GOATOS_HERD_SIGNALS_CAPTURE_CSV=/path/to/decoded_ear_tags.csv \
  tools/dev/seed-closeout.sh --dry-run
```

The rebuild runs the stage-2 replayer, never hand-written SQL. It is skipped when
the capture is not present on the machine (Herd Signals is a local-capture dev
feature today), and that skip is printed, not silent.

Partitioning and retention for these two tables are recorded in the Herd Signals
system-design document alongside the rest of the scale envelope; this runbook
covers the rebuild path only and does not restate the retention decision.

## Troubleshooting

**`REFUSING TO RUN: ...`** — working as designed. Point `DATABASE_URL` at
`127.0.0.1:15432/goatos`, or unset it and let the wrapper source the env file.

**`SSH tunnel is not listening`** — run
`/Users/ravi/mesha/tools/local/oci-goatos-a1-dev.sh tunnel`.

**`SAFETY: suspected non-OCI connection from ...`** — the server saw a client
address that is neither loopback nor Tailscale `10.88/16`. You are not on the
tunnel.

**Packet count doubles on re-run** — `herd_signal_packets_dedup_uidx` is missing.
Apply migration 000192.

**`movement_state` is `unknown` everywhere** — stage 2 has not run. Run
`bash tools/local/seed-herd-signals-oci.sh --replay-only`.

## Related

- `tools/local/seed-herd-signals-oci.sql` — stage 1, external facts
- `backend/cmd/seed-herd-signals-oci/main.go` — stage 2, real ingest replay
- `tools/dev/seed-closeout.sh` — projection rebuild registration
- `backend/migrations/postgres/000190..000192` — schema
- `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` — projection obligations
- `docs/runbooks/initial-seed-migration-coupling.md` — why the guard exists
