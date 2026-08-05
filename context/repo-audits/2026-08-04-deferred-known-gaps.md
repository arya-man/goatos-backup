# Deferred known gaps — 2026-08-04 device E2E session

Gaps found during the 4-device vaccination E2E that are **accepted and deferred
by the maintainer**, not defects to re-open. Each entry states what is broken,
why it is safe to defer, and what closing it will require.

Agent reports are not evidence here: every claim below was reproduced against
the live throwaway DB (`:15546`) or on a physical device.

---

## GAP-1 — `park_head` cannot receive notifications (deferred by maintainer)

**Status:** OPEN, deliberately deferred. Do not re-open as a code defect.

**What happens:** every vaccination push (`rework`, `verification_pending`,
`verification_approved`) resolves its recipients and silently returns zero rows
for `park_head`. Nobody in that role is ever notified.

**Root cause — data, not code.** Two independent reasons, both about seeding:

1. `workforce_positions` and `position_module_duties` are EMPTY in this tenant,
   so the position-based recipient lookup matches nothing.
2. `park_head` is not in the leadership `user_scope_grants` allowlist, so the
   grant-based fallback (added 2026-08-04 for the verifier, see
   `internal/workforce/adapters/postgres/roster_repository.go`) does not cover
   it either.

The recipient-resolution CODE is correct and was fixed this session for the two
roles that do exist: the operator now receives `rework` (previously suppressed
by `legacyHandledVaccination`, which assumed a legacy fan-out that never fires
for the per-animal `vaccination_goat` grain), and the verifier now receives
`verification_pending` via the grants fallback.

**Why deferring is safe today:** this deployment has no park-head staffed. The
role exists in the org model (`context/architecture` org chart: Founders → COO →
Director → Park Head → Manager → Assistant) but no human occupies it, so no
notification is being missed by a real person.

**What closing it requires:**
- seed real `workforce_positions` + `position_module_duties` rows for the
  park-head seat, OR add `park_head` to the grant allowlist with a decision
  recorded about which duty types it should receive;
- then re-verify: reject a proof and confirm a `notification_requests` row is
  addressed to the park-head member with a live device token.

**How to confirm it is still open:**

```sql
SELECT count(*) FROM workforce_positions;          -- 0 today
SELECT count(*) FROM position_module_duties;       -- 0 today
SELECT DISTINCT context->>'role' FROM notification_requests; -- no park_head
```

**Do NOT "fix" this by:** hardcoding a park-head recipient, or widening the
recipient query to fan out to every member of a park. The first invents an
ownership the roster does not record (see the permanent no-invented-ownership
rule in `goatos-shed-positions-seed-defect`), and the second turns one
rejection into a notification storm.
