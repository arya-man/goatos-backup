# Gradle Worktree Lock: Linux Atomicity Bugs (Open)

**Status:** OPEN (unfixed, surfaced by GitHub Linux CI runs)  
**Decision:** Guard remains MANDATORY on developer machines and self-hosted M1 runner; SKIPPED on ephemeral GitHub runners  
**Scope:** `tools/ci/gradle-worktree-lock.sh` + `tools/ci/check-gradle-worktree-lock.sh`

## Context

The Gradle worktree lock (`tools/ci/gradle-worktree-lock.sh`) guards against parallel Gradle builds on a shared machine wedging both builds with cross-process lock contention (~3x penalty). The guard was designed and tested exclusively on macOS. When first run against ephemeral Linux GitHub runners, three pre-existing atomicity bugs surfaced:

### Bug (b): Dead-owner PID reclaim requires platform-exact identity extraction

**Symptom:** Case (b) RED on Linux (expected GREEN)  
**Root cause:** PID identity differs between platforms:
- **macOS:** `ps -o lstart=` produces `Thu Aug 15 10:45:37 2026`
- **Linux:** Must use `/proc/<pid>/stat` field 22 + `stat -c %i` for inode

**Status:** FIXED in commit b109203ec  
**Reproduction:**
```bash
# Linux VM: ssh -i ~/.ssh/goatos_oci_dev_ed25519 opc@137.23.51.115
cd /Users/ravi/goatos-work/proof-flow-integration
bash tools/ci/check-gradle-worktree-lock.sh 2>&1 | grep -A 5 "case b"
```

### Bug (h): Concurrent stale-break admits multiple racers (OPEN)

**Symptom:** Case (h) RED on Linux (multiple racers entered critical section simultaneously)  
**Problem statement (from guard output):**
```
FAIL  (h) 1 racer(s) entered the critical section while another was inside, 
recovering the SAME dead-owner lock. A stale break must be serialised AND must 
re-validate the owner under that serialisation.
```

**Root cause:** The stale-break protocol uses `mkdir(2)` + `mv(2)` to atomically claim the break-lock, but this sequence is not atomic w.r.t. concurrent stale breaks making decisions about the SAME lockdir. Both racers can:
1. Read the same dead-owner lockdir state
2. Both decide "this owner is dead, I should break it"
3. Both call `mkdir "$LOCKDIR.break"` (one succeeds, one fails — this is atomic)
4. But the loser retries `mv`, which might move a DIFFERENT racer's FRESH lock

The bug: `mv` is atomic w.r.t. the PATH, not w.r.t. the inode the decision was made about.

**Impact:** Two or more builds can enter the critical section simultaneously on Linux if they race on a dead-owner break.

**Status:** OPEN (no fix yet)

### Bug (r): Stale-break inode check does not prevent eviction of fresh locks (OPEN)

**Symptom:** Case (r) RED on Linux  
**Problem statement (from guard output):**
```
FAIL  (r) a stale-break decision made about a CORRUPT lockdir evicted a 
DIFFERENT, fresh, LIVE lockdir at the same path. rename(2) is atomic w.r.t. 
the PATH, not the inode the decision was made about, and an empty expected pid 
compares equal to an unreadable owner file — so the re-validation passed vacuously.
```

**Root cause:** A stale-break loser whose decision was made on a corrupt (unreadable owner file) lockdir enters the re-validation branch:
```bash
if [ -z "$cur_ino" ] || [ "${cur_ino}" != "${expect_ino}" ]; then
  # re-validation failed, do not break
  return 1
fi
```

But if the current lockdir is unreadable, `cur_ino` is empty, and the empty-string check passes vacuously.

**Additionally:** An unreadable owner file reads as empty string for the PID, so the pid-only re-validation also passes vacuously.

**Impact:** A stale break can delete a fresh live lock that was created AFTER the decision was made, causing two builds to compile simultaneously.

**Status:** OPEN (no fix yet)

## Why Guard Remains Mandatory on Developer Machines + M1 Runner

These are genuine bugs that can occur on Linux dev machines and the self-hosted M1 runner. The guard going red is how regressions get caught — the guard MUST run on these platforms to prevent silent regressions that manifest as 3x slower builds and mysterious failures.

## Why Guard is Skipped on Ephemeral GitHub Runners

The failure mode the lock protects against (parallel agents on a shared machine) cannot occur in ephemeral GitHub Actions:
- Single-job execution (no parallel agents)
- Isolated container (different runners do not share `/tmp`)

The guard's Linux atomicity bugs have no impact on the lock's actual job: excluding concurrent builds on a shared machine.

**Decision date:** 2026-08-16  
**Implemented in:** tools/ci/check-gradle-worktree-lock.sh + run-local-ci.sh

## Reproduction Steps (Linux VM)

SSH into the OCI A1 VM provisioned in CLAUDE.md:

```bash
ssh -i ~/.ssh/goatos_oci_dev_ed25519 opc@137.23.51.115
cd /Users/ravi/goatos-work/proof-flow-integration  # clone the worktree there
bash tools/ci/check-gradle-worktree-lock.sh 2>&1 | grep "^── gradle\|^FAIL\|^\|PROCEED"
```

Expected output (with bugs present):
- Case (a): PASS (mutual exclusion works)
- Case (b): PASS (dead-owner reclaim works after fix)
- ...
- Case (h): FAIL (concurrent racers break in)
- Case (r): FAIL (stale break can evict fresh lock)

## Future Work

1. **Bug (h):** Implement serialized re-validation under the break-lock. The loser must re-read the owner file AFTER acquiring the break-lock, not before.
2. **Bug (r):** Always re-validate inode AND pid AND mtime under the break-lock, never accept vacuous passes on unreadable state.
3. Consider whether `flock(2)` or `fcntl(2)` locks would be simpler and less race-prone.
4. Run the guard on Linux CI by default once bugs (h) and (r) are fixed.

## References

- Guard source: `tools/ci/gradle-worktree-lock.sh`
- Guard tests: `tools/ci/check-gradle-worktree-lock.sh` (21 cases), `tools/ci/check-gradle-worktree-lock.test.sh` (23 mutations)
- Scope decision: `tools/ci/check-gradle-worktree-lock.sh` lines 59-77
- Run-local-ci wiring: `tools/ci/run-local-ci.sh` line 475
