# Mobile Anti-Patterns: Proof Flow Migrations

This skill documents anti-patterns discovered during proof-flow canonical migrations (2026-08-16).

## 1. Cosmetic Canonical Migration

**Pattern**: Build canonical EvidenceSlot, then unpack back to raw strings.

**Impact**: Migration defeats itself. Telemetry, retry logic, supersedence don't work on raw strings.

**Fix**: Use slot.identity directly, never re-derive groupKey() from raw params.

---

## 2. Self-Fulfilling Test

**Pattern**: Test re-implements production logic, asserts only its own output.

**Impact**: Regressions go undetected. Test passes when production is broken.

**Fix**: Import production functions, test them directly. Mutation check: break production, test must fail.

---

## 3. Proof Media Invariant Violation

**Pattern**: Raw upload bypass exists alongside processed pipeline.

**Impact**: Media inconsistent. Some compressed+overlaid, some raw.

**Fix**: ONE pipeline entry point. Terminal PROCESSING_FAILED_AWAITING_RETRY, no raw fallback.

---

## 4. Ratchet-to-Zero Guard Regression

**Pattern**: CI guard counting anti-patterns is disabled to merge non-canonical code.

**Impact**: Guard ratchets backwards. Future code re-introduces same pattern.

**Fix**: Enforce monotonic ratchet with max_count + explanation requirement.

---

## 5. Analytics Parameter Gap

**Pattern**: Proof events fire without identifying flow/task/action.

**Impact**: Failed proof indistinguishable. Funnel queries fail: "milk proofs failed at Park A" → bare events.

**Fix**: All events carry identifying context: parkId/taskId/workflowId + stepCode/actionId + reason.

---

## Verification Checklist

- [ ] Canonical routing (slot, not raw uploads)
- [ ] Slot consistency (same slotId for all operations)
- [ ] Tests authentic (import real functions)
- [ ] Media invariant (ONE pipeline, terminal failures)
- [ ] Analytics params (flowId, actionId, reason on all events)
- [ ] Guard integrity (ratchet monotonic)
- [ ] Live status observation (not stale in-memory state)
