# Shifting Destination Latency - 2026-09-16

## Scope

Reduce latency for `GET /app/counts/shifting/destinations`, the mobile-facing shifting and birth-placement destination catalog.

The change is backend-only. It preserves the existing response shape, including empty parks, empty real partitions, legacy partition-alias suppression, per-pen head counts, per-pen resident stages, configured destination stages, birth placement metadata, and management-stage vocabulary.

## Change

- Rewrote the destination catalog SQL so active parks, active sheds, active partitions, canonical sheds, and live resident aggregates are each computed once for the tenant.
- Replaced two per-destination correlated goat scans with one resident aggregate grouped by `(tenant_id, shed_id, normalized partition label)`.
- Kept the existing route contract and repository grouping code unchanged outside the SQL shape.

## OCI Before / After

Database target: OCI staging clone through `127.0.0.1:15432`.

Tenant: `00000000-0000-4000-8000-000000000001`.

Catalog returned by both versions: 2 parks, 120 destination rows.

Command shape:

```text
GOATOS_LIVE_OCI_BENCH=1 GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001 go test -tags liveoci ./internal/counts/adapters/postgres -run TestLiveOCIShiftingDestinationCatalogLatency -count=1 -v
```

| Version | n | min | p50 | p90 | p95 | max | avg |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `origin/main` (`397114d1d`) | 25 | 585.00 ms | 607.06 ms | 679.62 ms | 685.34 ms | 696.45 ms | 621.26 ms |
| patched branch | 25 | 64.95 ms | 91.22 ms | 158.77 ms | 166.26 ms | 176.32 ms | 103.28 ms |

Improvement: p50 84.97%, p90 76.64%, average 83.38%.

## Chrome E2E

Chrome was driven by Playwright against an isolated local API on `127.0.0.1:18117`, backed by the same OCI database and authenticated with a tenant-scoped operator bearer token already present in OCI grants.

Checks:

- HTTP 200 for the initial browser navigation and every same-origin fetch.
- Parsed JSON in Chrome.
- Verified 2 parks, 120 destination rows, 17 management stages.
- Verified `operational_location_display`, `destination_stage`, `destination_stage_reason`, and `birth_placement` contract fields.
- Verified no observed outage strings: `Admin-web contract unavailable`, `backend_down`, `The board could not be loaded`, `Weights could not be loaded`.
- Visual artifact validated: `.e2e-artifacts/shifting-destinations/chrome-shifting-destinations-e2e.png`.

Chrome samples:

| n | min | p50 | p90 | p95 | max | avg |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 10 | 117.60 ms | 123.00 ms | 208.30 ms | 329.40 ms | 329.40 ms | 158.49 ms |

## Tests

- `cd backend && go test ./internal/counts/adapters/postgres -run 'TestShiftingDestinationCatalog|TestGoatShiftingFacts' -count=1`
- `cd backend && go test ./internal/counts/app -run 'TestShiftingDestinations' -count=1`
- `cd backend && go test ./internal/counts/adapters/http -run 'Test.*Shifting.*Destination|Test.*BirthPlacement|Test.*WriteHandler.*Shift' -count=1`
- `cd backend && go test -tags liveoci ./internal/counts/adapters/postgres -run TestLiveOCIShiftingDestinationCatalogLatency -count=1`

## Current State

- Branch: `fix/shifting-destination-latency-clean-20260916`
- Base: `origin/main` at `397114d1d06baddb50dffc7d2c2f9df1d0497b7b`
- Deployment: not deployed
