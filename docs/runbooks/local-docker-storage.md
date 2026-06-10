# Local Docker Storage Runbook

This runbook keeps daily Goat OS development from filling a Mac with Docker
data again.

## Local Development Model

Daily Goat OS coding runs locally with Docker Postgres, tests, and small
synthetic data. GCP is not required for normal coding.

Cloud databases come later and have separate purposes:

- `goatos-dev` Cloud SQL is for explicit cloud rehearsal, not everyday coding.
- `goatos-stg` Cloud SQL later holds the persistent 1M-goat benchmark dataset.
- Later Goat OS cloud regions default to `asia-south1` in Mumbai. Do not default
  Goat OS infrastructure to US regions.

Local 1M tests are temporary only:

```text
create temporary Docker volume
run the benchmark/test
export the summary/report
delete the temporary volume
reclaim Docker Desktop VM disk space if macOS still shows the space as used
```

Do not keep the 1M benchmark permanently inside local Docker.

## Docker Storage Basics

Docker storage has several layers:

- **Containers** are runnable instances. Stopped containers can still hold logs
  and writable-layer data.
- **Images** are downloaded or built filesystem layers used to create
  containers.
- **Volumes** are persistent data stores. Postgres data usually lives here.
- **Build cache** is Docker's machine-wide cache for image builds.
- **Docker Desktop VM disk** is the host-side macOS disk image, usually
  `Docker.raw` or an equivalent VM disk image, that contains Docker's Linux
  filesystem.

## macOS Docker.raw Gotcha

On Docker Desktop for Mac, deleting containers, images, volumes, or build cache
frees space inside Docker's Linux VM. The macOS host-side `Docker.raw` or VM
disk image may not shrink immediately.

Cleanup has two layers:

1. Delete safe Docker resources inside Docker.
2. Reclaim or shrink Docker Desktop VM disk space from Docker Desktop settings
   or Docker Desktop's reclaim/reset workflow.

Volume cleanup alone may not immediately return space to macOS Finder or System
Settings. If the Mac disk remains full after cleanup, inspect Docker Desktop's
disk usage and reclaim options before assuming the Docker cleanup failed.

## Safe Vs Risky Cleanup

Usually safe:

- stopped containers after checking they are not needed;
- clearly temporary Goat OS test volumes;
- old build cache only when the operator understands it is machine-wide.

Risky:

- named database volumes with current work;
- anything not obviously Goat OS-owned;
- arbitrary anonymous volumes without inspection;
- blind `docker volume prune`.

## Naming Contract

Permanent local dev DB volume:

```text
goatos_dev_pg_data
```

Temporary test or benchmark volumes must use only these prefixes:

```text
goatos_tmp_
goatos_test_
goatos_bench_tmp_
```

Future local 1M test tooling must create temporary volumes with one of those
prefixes. Permanent volumes must never use those temp prefixes.

Cleanup scripts must skip:

- `goatos_dev_pg_data`;
- any unclassified volume;
- any non-Goat-OS-looking volume.

## Global Cleanup Honesty

Docker build cache and dangling images are machine-wide, not Goat-OS-scoped.
Goat OS cleanup scripts may report global build cache and image usage, but they
must not silently prune global build cache or images under a "Goat OS only"
label.

If global pruning is offered later, it must be clearly marked machine-wide and
operator-confirmed.

## Commands

Read-only storage report:

```bash
make docker-storage-report
```

Dry-run cleanup for classified Goat OS temp volumes:

```bash
make docker-cleanup-goatos-dry-run
```

Safe execute cleanup for classified Goat OS temp volumes only:

```bash
make docker-cleanup-goatos-execute
```

The execute target still skips `goatos_dev_pg_data`, unclassified volumes,
images, containers, and build cache.
